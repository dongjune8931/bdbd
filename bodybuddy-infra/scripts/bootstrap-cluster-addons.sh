#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
ARGOCD_NAMESPACE="${ARGOCD_NAMESPACE:-bodybuddy-system}"
KARPENTER_NAMESPACE="${KARPENTER_NAMESPACE:-karpenter}"
KARPENTER_VERSION="${KARPENTER_VERSION:-1.12.1}"

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"

cluster_name="$(jq -r '.eks_cluster_name.value' <<<"$tf_outputs")"
cluster_endpoint="$(jq -r '.eks_cluster_endpoint.value' <<<"$tf_outputs")"
oidc_provider_arn="$(jq -r '.eks_oidc_provider_arn.value' <<<"$tf_outputs")"
vpc_id="$(jq -r '.vpc_id.value' <<<"$tf_outputs")"
karpenter_controller_role_arn="$(jq -r '.karpenter_controller_iam_role_arn.value' <<<"$tf_outputs")"
karpenter_queue_url="$(jq -r '.karpenter_interruption_queue_url.value' <<<"$tf_outputs")"
karpenter_queue_name="${karpenter_queue_url##*/}"
alb_controller_role_arn="$(jq -r '.aws_load_balancer_controller_irsa_role_arn.value' <<<"$tf_outputs")"

echo "Updating kubeconfig for $cluster_name"
aws eks update-kubeconfig \
  --name "$cluster_name" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE"

echo "Ensuring namespaces exist"
kubectl create namespace "$ARGOCD_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "$KARPENTER_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

echo "Installing ArgoCD"
helm repo add argo https://argoproj.github.io/argo-helm >/dev/null 2>&1 || true
helm repo update argo >/dev/null
helm upgrade --install argocd argo/argo-cd \
  -n "$ARGOCD_NAMESPACE" \
  --wait \
  --timeout 10m

echo "Installing Karpenter $KARPENTER_VERSION"
helm upgrade --install karpenter-crd oci://public.ecr.aws/karpenter/karpenter-crd \
  --version "$KARPENTER_VERSION" \
  -n "$KARPENTER_NAMESPACE" \
  --wait \
  --timeout 10m

helm upgrade --install karpenter oci://public.ecr.aws/karpenter/karpenter \
  --version "$KARPENTER_VERSION" \
  -n "$KARPENTER_NAMESPACE" \
  --set replicas=1 \
  --set "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=$karpenter_controller_role_arn" \
  --set "settings.clusterName=$cluster_name" \
  --set "settings.clusterEndpoint=$cluster_endpoint" \
  --set "settings.interruptionQueue=$karpenter_queue_name" \
  --wait \
  --timeout 10m

echo "Installing AWS Load Balancer Controller"
helm repo add eks https://aws.github.io/eks-charts >/dev/null 2>&1 || true
helm repo update eks >/dev/null
helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set "clusterName=$cluster_name" \
  --set "region=$AWS_REGION" \
  --set "vpcId=$vpc_id" \
  --set serviceAccount.create=true \
  --set serviceAccount.name=aws-load-balancer-controller \
  --set "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=$alb_controller_role_arn" \
  --wait \
  --timeout 10m

echo "Applying ArgoCD app-of-apps"
kubectl apply -f "$ROOT_DIR/bodybuddy-infra/argocd/apps/project.yaml"
kubectl apply -f "$ROOT_DIR/bodybuddy-infra/argocd/app-of-apps.yaml"

echo "Bootstrap complete."
