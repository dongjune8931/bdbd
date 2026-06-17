#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_DIR="$ROOT_DIR/bodybuddy-infra/scripts"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"
HELM_DIR="$ROOT_DIR/bodybuddy-app/deploy/helm"
KARPENTER_DIR="$ROOT_DIR/bodybuddy-infra/argocd/karpenter"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
NAMESPACE="${NAMESPACE:-bodybuddy}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-bodybuddy-system}"

RUN_TERRAFORM="${RUN_TERRAFORM:-0}"
RUN_BOOTSTRAP="${RUN_BOOTSTRAP:-$RUN_TERRAFORM}"
BUILD_IMAGES="${BUILD_IMAGES:-0}"
DIRECT_DEPLOY="${DIRECT_DEPLOY:-0}"
DEPLOY_SERVICES="${DEPLOY_SERVICES:-$DIRECT_DEPLOY}"
RUN_MIGRATION="${RUN_MIGRATION:-$DIRECT_DEPLOY}"
RUN_SMOKE="${RUN_SMOKE:-$DIRECT_DEPLOY}"
SUSPEND_ARGOCD_SYNC="${SUSPEND_ARGOCD_SYNC:-$DIRECT_DEPLOY}"

services=("user-service" "score-service" "analysis-worker" "notification-worker")
apps=("bodybuddy-app-of-apps" "user-service" "score-service" "analysis-worker" "notification-worker" "karpenter-capacity")

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require aws
require helm
require jq
require kubectl
require terraform

if [[ "$RUN_TERRAFORM" = "1" ]]; then
  echo "Applying Terraform dev environment"
  AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" apply -auto-approve
else
  echo "Skipping Terraform apply. Set RUN_TERRAFORM=1 to recreate AWS resources."
fi

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"
cluster_name="$(jq -r '.eks_cluster_name.value' <<<"$tf_outputs")"

echo "Updating kubeconfig for $cluster_name"
aws eks update-kubeconfig \
  --name "$cluster_name" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" >/dev/null

if [[ "$RUN_BOOTSTRAP" = "1" ]]; then
  echo "Bootstrapping cluster add-ons"
  AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" "$SCRIPT_DIR/bootstrap-cluster-addons.sh"
else
  echo "Skipping add-on bootstrap. Set RUN_BOOTSTRAP=1 after a fresh Terraform apply."
fi

if [[ "$BUILD_IMAGES" = "1" ]]; then
  echo "Building and pushing service images"
  AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" "$SCRIPT_DIR/dev-build-images.sh"
else
  echo "Skipping image build. Set BUILD_IMAGES=1 to push fresh dev images."
fi

echo "Refreshing dev values from Terraform outputs"
AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" "$SCRIPT_DIR/refresh-dev-values.sh"

echo "Ensuring namespace $NAMESPACE exists"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "Applying Karpenter capacity manifests"
kubectl apply -f "$KARPENTER_DIR"

if [[ "$SUSPEND_ARGOCD_SYNC" = "1" ]]; then
  echo "Suspending ArgoCD auto-sync for direct dev deploy"
  for app in "${apps[@]}"; do
    if kubectl get application "$app" -n "$ARGOCD_NAMESPACE" >/dev/null 2>&1; then
      kubectl patch application "$app" -n "$ARGOCD_NAMESPACE" --type=merge -p '{"spec":{"syncPolicy":null}}' >/dev/null
    fi
  done
fi

if [[ "$DIRECT_DEPLOY" = "1" ]]; then
  echo "Direct deploy mode is enabled. This bypasses normal GitOps convergence and can leave ArgoCD apps OutOfSync."
fi

if [[ "$DEPLOY_SERVICES" = "1" ]]; then
  render_dir="$ROOT_DIR/tmp/rendered-helm-$(date +%Y%m%d%H%M%S)"
  mkdir -p "$render_dir"

  echo "Rendering Helm charts into $render_dir"
  for service in "${services[@]}"; do
    helm template "$service" "$HELM_DIR/$service" -n "$NAMESPACE" --output-dir "$render_dir" >/dev/null
  done

  echo "Applying rendered service manifests to $NAMESPACE"
  kubectl apply -n "$NAMESPACE" -f "$render_dir" --recursive --validate=false

  echo "Restarting deployments to pick up refreshed Secret values"
  for service in "${services[@]}"; do
    kubectl rollout restart "deployment/$service" -n "$NAMESPACE"
  done

  echo "Waiting for deployments"
  for service in "${services[@]}"; do
    kubectl rollout status "deployment/$service" -n "$NAMESPACE" --timeout=240s
  done
else
  echo "Skipping service deploy. Set DIRECT_DEPLOY=1 or DEPLOY_SERVICES=1 to render/apply Helm charts."
fi

if [[ "$RUN_MIGRATION" = "1" ]]; then
  AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" NAMESPACE="$NAMESPACE" "$SCRIPT_DIR/dev-migrate.sh"
else
  echo "Skipping DB migration. Set DIRECT_DEPLOY=1 or RUN_MIGRATION=1 to run it."
fi

if [[ "$RUN_SMOKE" = "1" ]]; then
  AWS_PROFILE="$AWS_PROFILE" AWS_REGION="$AWS_REGION" NAMESPACE="$NAMESPACE" "$SCRIPT_DIR/dev-smoke.sh"
else
  echo "Skipping smoke test. Set DIRECT_DEPLOY=1 or RUN_SMOKE=1 to run it."
fi

echo "Dev environment automation complete."
