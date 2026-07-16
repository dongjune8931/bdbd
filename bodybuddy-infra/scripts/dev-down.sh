#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
CLUSTER_NAME="${CLUSTER_NAME:-bodybuddy-dev-eks}"
NAMESPACE="${NAMESPACE:-bodybuddy}"
S3_BUCKET="${S3_BUCKET:-bodybuddy-dev-inbody}"
CONFIRM_DESTROY="${CONFIRM_DESTROY:-}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require aws
require jq
require kubectl
require terraform

if [[ "$CONFIRM_DESTROY" != "bodybuddy-dev" ]]; then
  echo "refusing to destroy: set CONFIRM_DESTROY=bodybuddy-dev" >&2
  exit 1
fi

aws_cli=(aws --profile "$AWS_PROFILE" --region "$AWS_REGION")

assert_zero() {
  local label="$1"
  local count="$2"

  if [[ "$count" != "0" ]]; then
    echo "$label resources remain after destroy: $count" >&2
    exit 1
  fi
}

cluster_exists() {
  "${aws_cli[@]}" eks describe-cluster --name "$CLUSTER_NAME" >/dev/null 2>&1
}

wait_for_no_bodybuddy_alb() {
  local attempts=30
  local count

  for ((attempt = 1; attempt <= attempts; attempt++)); do
    count="$("${aws_cli[@]}" elbv2 describe-load-balancers \
      --query 'length(LoadBalancers[?contains(LoadBalancerName, `bodybudd`)])' \
      --output text)"
    if [[ "$count" = "0" ]]; then
      return
    fi
    sleep 10
  done

  echo "BodyBuddy load balancer still exists after 5 minutes" >&2
  exit 1
}

empty_ecr_repositories() {
  local repositories
  local image_ids

  repositories="$("${aws_cli[@]}" ecr describe-repositories \
    --query 'repositories[?starts_with(repositoryName, `bodybuddy-dev-`)].repositoryName' \
    --output text 2>/dev/null || true)"

  for repository in $repositories; do
    image_ids="$("${aws_cli[@]}" ecr list-images \
      --repository-name "$repository" \
      --query 'imageIds' \
      --output json)"
    if [[ "$(jq 'length' <<<"$image_ids")" -gt 0 ]]; then
      echo "Deleting images from $repository"
      "${aws_cli[@]}" ecr batch-delete-image \
        --repository-name "$repository" \
        --image-ids "$image_ids" >/dev/null
    fi
  done
}

empty_versioned_bucket() {
  local versions
  local delete_payload

  if ! "${aws_cli[@]}" s3api head-bucket --bucket "$S3_BUCKET" >/dev/null 2>&1; then
    return
  fi

  while true; do
    versions="$("${aws_cli[@]}" s3api list-object-versions \
      --bucket "$S3_BUCKET" \
      --output json)"
    delete_payload="$(jq -c '{Objects: ([.Versions[]?, .DeleteMarkers[]?] | map({Key, VersionId}) | .[:1000]), Quiet: true}' <<<"$versions")"

    if [[ "$(jq '.Objects | length' <<<"$delete_payload")" -eq 0 ]]; then
      return
    fi

    echo "Deleting S3 object versions from $S3_BUCKET"
    "${aws_cli[@]}" s3api delete-objects \
      --bucket "$S3_BUCKET" \
      --delete "$delete_payload" \
      --bypass-governance-retention >/dev/null
  done
}

if cluster_exists; then
  echo "Preparing Kubernetes-managed resources for deletion"
  "${aws_cli[@]}" eks update-kubeconfig --name "$CLUSTER_NAME" >/dev/null

  kubectl scale deployment/inference-service -n "$NAMESPACE" --replicas=0 >/dev/null 2>&1 || true
  kubectl delete ingress -n "$NAMESPACE" --all --ignore-not-found=true --wait=false
  wait_for_no_bodybuddy_alb

  kubectl delete nodeclaims --all --ignore-not-found=true --wait=true --timeout=300s
fi

empty_ecr_repositories
empty_versioned_bucket

echo "Destroying Terraform dev environment"
AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" \
  terraform -chdir="$TF_DIR" destroy -auto-approve

if [[ -n "$(terraform -chdir="$TF_DIR" state list)" ]]; then
  echo "Terraform state is not empty after destroy" >&2
  exit 1
fi

active_instances="$("${aws_cli[@]}" ec2 describe-instances \
  --filters \
    Name=tag:Project,Values=bodybuddy \
    Name=instance-state-name,Values=pending,running,stopping,stopped,shutting-down \
  --query 'length(Reservations[].Instances[])' \
  --output text)"
assert_zero "active BodyBuddy EC2" "$active_instances"

assert_zero "BodyBuddy EKS" "$("${aws_cli[@]}" eks list-clusters \
  --query 'length(clusters[?contains(@, `bodybuddy`)])' --output text)"
assert_zero "BodyBuddy load balancer" "$("${aws_cli[@]}" elbv2 describe-load-balancers \
  --query 'length(LoadBalancers[?contains(LoadBalancerName, `bodybudd`)])' --output text)"
assert_zero "BodyBuddy NAT Gateway" "$("${aws_cli[@]}" ec2 describe-nat-gateways \
  --filter Name=tag:Project,Values=bodybuddy Name=state,Values=pending,available,deleting,failed \
  --query 'length(NatGateways)' --output text)"
assert_zero "BodyBuddy RDS" "$("${aws_cli[@]}" rds describe-db-instances \
  --query 'length(DBInstances[?contains(DBInstanceIdentifier, `bodybuddy`)])' --output text)"
assert_zero "BodyBuddy Redis" "$("${aws_cli[@]}" elasticache describe-replication-groups \
  --query 'length(ReplicationGroups[?contains(ReplicationGroupId, `bodybuddy`)])' --output text)"
assert_zero "BodyBuddy dev ECR" "$("${aws_cli[@]}" ecr describe-repositories \
  --query 'length(repositories[?starts_with(repositoryName, `bodybuddy-dev-`)])' --output text)"

if "${aws_cli[@]}" s3api head-bucket --bucket "$S3_BUCKET" >/dev/null 2>&1; then
  echo "BodyBuddy dev S3 bucket remains after destroy: $S3_BUCKET" >&2
  exit 1
fi

echo "Dev environment destroy complete; Terraform state and AWS residual checks passed."
