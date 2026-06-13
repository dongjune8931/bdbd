#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"
NOTIFICATION_VALUES="$ROOT_DIR/bodybuddy-app/deploy/helm/notification-worker/values.yaml"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
NAMESPACE="${NAMESPACE:-bodybuddy}"
INGRESS_NAME="${INGRESS_NAME:-user-service}"
SMOKE_PASSWORD="${SMOKE_PASSWORD:-password123}"
SMOKE_WAIT_SECONDS="${SMOKE_WAIT_SECONDS:-8}"
SCORE_CHECK_IMAGE="${SCORE_CHECK_IMAGE:-curlimages/curl:8.8.0}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require aws
require curl
require jq
require kubectl
require terraform

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"
cluster_name="$(jq -r '.eks_cluster_name.value' <<<"$tf_outputs")"
analysis_queue_url="$(jq -r '.analysis_queue_url.value' <<<"$tf_outputs")"
analysis_dlq_url="${ANALYSIS_DLQ_URL:-${analysis_queue_url}-dlq}"
notification_queue_url="$(jq -r '.notification_queue_url.value' <<<"$tf_outputs")"

echo "Updating kubeconfig for $cluster_name"
aws eks update-kubeconfig \
  --name "$cluster_name" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" >/dev/null

echo "Waiting for ALB hostname"
alb_host=""
for _ in $(seq 1 60); do
  alb_host="$(kubectl get ingress "$INGRESS_NAME" -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || true)"
  if [[ -n "$alb_host" ]]; then
    break
  fi
  sleep 5
done

if [[ -z "$alb_host" ]]; then
  echo "ALB hostname is not ready for ingress $NAMESPACE/$INGRESS_NAME" >&2
  exit 1
fi

base_url="http://$alb_host"
echo "ALB: $base_url"

echo "Waiting for /readyz"
for _ in $(seq 1 60); do
  status_code="$(curl -sS -o /dev/null -w '%{http_code}' "$base_url/readyz" || true)"
  if [[ "$status_code" = "200" ]]; then
    break
  fi
  sleep 5
done

if [[ "$status_code" != "200" ]]; then
  echo "/readyz did not become healthy; last status: $status_code" >&2
  exit 1
fi

default_smoke_email="$(sed -n 's/^[[:space:]]*fromEmail:[[:space:]]*//p' "$NOTIFICATION_VALUES" | head -n1 | tr -d "\"'")"
if [[ -z "$default_smoke_email" ]]; then
  default_smoke_email="bodybuddy-smoke-$(date +%Y%m%d%H%M%S)@example.com"
fi

email="${SMOKE_EMAIL:-$default_smoke_email}"
echo "Authenticating smoke user $email"
register_http_response="$(curl -sS \
  -w $'\n%{http_code}' \
  -X POST "$base_url/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$email\",\"password\":\"$SMOKE_PASSWORD\"}")"
register_status="${register_http_response##*$'\n'}"
register_body="${register_http_response%$'\n'*}"

case "$register_status" in
  201)
    auth_response="$register_body"
    ;;
  409)
    echo "Smoke user already exists; logging in"
    auth_response="$(curl -fsS \
      -X POST "$base_url/api/v1/auth/login" \
      -H 'Content-Type: application/json' \
      -d "{\"email\":\"$email\",\"password\":\"$SMOKE_PASSWORD\"}")"
    ;;
  *)
    echo "registration failed with HTTP $register_status: $register_body" >&2
    exit 1
    ;;
esac

token="$(jq -r '.token // empty' <<<"$auth_response")"
user_id="$(jq -r '.user_id // empty' <<<"$auth_response")"

if [[ -z "$token" || -z "$user_id" ]]; then
  echo "registration response did not include token/user_id: $register_response" >&2
  exit 1
fi

echo "Creating upload for user_id=$user_id"
upload_response="$(curl -fsS \
  -X POST "$base_url/api/v1/uploads" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -d '{"filename":"smoke-test.png"}')"
upload_id="$(jq -r '.upload_id // empty' <<<"$upload_response")"

if [[ -z "$upload_id" ]]; then
  echo "upload response did not include upload_id: $upload_response" >&2
  exit 1
fi

echo "Upload queued: $upload_id"
echo "Waiting ${SMOKE_WAIT_SECONDS}s for analysis-worker"
sleep "$SMOKE_WAIT_SECONDS"

score_check_name="score-check-$(date +%s)"
echo "Querying score-service from inside the cluster"
kubectl delete pod "$score_check_name" -n "$NAMESPACE" --ignore-not-found >/dev/null
kubectl run "$score_check_name" \
  -n "$NAMESPACE" \
  --restart=Never \
  --image="$SCORE_CHECK_IMAGE" \
  --command -- sh -c "curl -fsS http://score-service/api/v1/score/$user_id && echo && curl -fsS 'http://score-service/api/v1/ranking?limit=5'" >/dev/null

for _ in $(seq 1 60); do
  phase="$(kubectl get pod "$score_check_name" -n "$NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  case "$phase" in
    Succeeded)
      break
      ;;
    Failed)
      kubectl logs "pod/$score_check_name" -n "$NAMESPACE" >&2 || true
      kubectl delete pod "$score_check_name" -n "$NAMESPACE" --ignore-not-found >/dev/null
      exit 1
      ;;
  esac
  sleep 1
done

if [[ "$phase" != "Succeeded" ]]; then
  echo "score check pod did not complete; final phase: ${phase:-unknown}" >&2
  kubectl logs "pod/$score_check_name" -n "$NAMESPACE" >&2 || true
  kubectl delete pod "$score_check_name" -n "$NAMESPACE" --ignore-not-found >/dev/null
  exit 1
fi

score_output="$(kubectl logs "pod/$score_check_name" -n "$NAMESPACE")"
echo "$score_output"
kubectl delete pod "$score_check_name" -n "$NAMESPACE" --ignore-not-found >/dev/null

queue_attrs() {
  local label="$1"
  local queue_url="$2"
  echo "$label"
  aws sqs get-queue-attributes \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --queue-url "$queue_url" \
    --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed
}

queue_attrs "analysis queue:" "$analysis_queue_url"
queue_attrs "analysis DLQ:" "$analysis_dlq_url"
queue_attrs "notification queue:" "$notification_queue_url"

cat <<EOF

Smoke test complete.

Capture commands:
  kubectl get pods -n $NAMESPACE -o wide
  kubectl get nodepool,nodeclaim -A
  kubectl get deploy,svc,ingress,hpa -n $NAMESPACE
  kubectl logs -n $NAMESPACE deploy/analysis-worker --since=10m
  kubectl logs -n $NAMESPACE deploy/score-service --since=10m

ALB:
  $base_url
EOF
