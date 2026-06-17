#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
APP_DIR="$ROOT_DIR/bodybuddy-app"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"
TMP_DIR="$ROOT_DIR/tmp/bodybuddy-images"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
IMAGE_TAG="${IMAGE_TAG:-dev-$(date +%Y%m%d%H%M%S)}"
GOOS_TARGET="${GOOS_TARGET:-linux}"
GOARCH_TARGET="${GOARCH_TARGET:-amd64}"

services=("user-service" "score-service" "analysis-worker" "notification-worker")

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require aws
require docker
require go
require jq
require terraform

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"

registry="$(jq -r '.ecr_repository_urls.value["user-service"]' <<<"$tf_outputs" | cut -d/ -f1)"

echo "Logging in to ECR registry $registry"
aws ecr get-login-password \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" |
  docker login --username AWS --password-stdin "$registry" >/dev/null

cert_file="${CA_CERT_FILE:-}"
if [[ -z "$cert_file" ]]; then
  for candidate in /etc/ssl/cert.pem /etc/ssl/certs/ca-certificates.crt; do
    if [[ -f "$candidate" ]]; then
      cert_file="$candidate"
      break
    fi
  done
fi

if [[ -z "$cert_file" || ! -f "$cert_file" ]]; then
  echo "could not find a CA bundle; set CA_CERT_FILE=/path/to/certs.pem" >&2
  exit 1
fi

runtime_dockerfile="$ROOT_DIR/tmp/bodybuddy-runtime.Dockerfile"
mkdir -p "$TMP_DIR"
cat >"$runtime_dockerfile" <<'DOCKERFILE'
FROM scratch
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY service /service
USER 65532:65532
ENTRYPOINT ["/service"]
DOCKERFILE

for service in "${services[@]}"; do
  repository_url="$(jq -r --arg service "$service" '.ecr_repository_urls.value[$service]' <<<"$tf_outputs")"
  context_dir="$TMP_DIR/$service"
  rm -rf "$context_dir"
  mkdir -p "$context_dir"

  echo "Building $service for $GOOS_TARGET/$GOARCH_TARGET"
  cp "$cert_file" "$context_dir/ca-certificates.crt"
  (
    cd "$APP_DIR"
    CGO_ENABLED=0 GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" \
      go build -ldflags="-s -w" -o "$context_dir/service" "./cmd/$service/"
  )

  echo "Pushing $repository_url:$IMAGE_TAG and :latest"
  docker buildx build \
    --platform "$GOOS_TARGET/$GOARCH_TARGET" \
    -f "$runtime_dockerfile" \
    -t "$repository_url:$IMAGE_TAG" \
    -t "$repository_url:latest" \
    --push \
    "$context_dir"
done

echo "Built and pushed all images with tag: $IMAGE_TAG"

