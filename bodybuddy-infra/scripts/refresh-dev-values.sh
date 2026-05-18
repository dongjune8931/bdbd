#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"
HELM_DIR="$ROOT_DIR/bodybuddy-app/deploy/helm"
KARPENTER_NODE_CLASS="$ROOT_DIR/bodybuddy-infra/argocd/karpenter/ec2-node-class.yaml"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"

rds_endpoint="$(jq -r '.rds_endpoint.value' <<<"$tf_outputs")"
db_host="${rds_endpoint%:*}"
redis_endpoint="$(jq -r '.elasticache_primary_endpoint_address.value' <<<"$tf_outputs")"
redis_addr="${redis_endpoint}:6379"
analysis_queue_url="$(jq -r '.analysis_queue_url.value' <<<"$tf_outputs")"
notification_queue_url="$(jq -r '.notification_queue_url.value' <<<"$tf_outputs")"
s3_bucket_name="$(jq -r '.s3_bucket_name.value' <<<"$tf_outputs")"
rds_secret_arn="$(jq -r '.rds_master_user_secret_arn.value' <<<"$tf_outputs")"
private_subnet_ids="$(jq -r '.private_subnet_ids.value | join(",")' <<<"$tf_outputs")"

echo "Reading current RDS password from Secrets Manager"
secret_string="$(aws secretsmanager get-secret-value \
  --secret-id "$rds_secret_arn" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" \
  --query SecretString \
  --output text)"
db_password="$(jq -r '.password' <<<"$secret_string")"

latest_ecr_tag() {
  local repository_url="$1"
  local repository_name="${repository_url##*/}"

  aws ecr describe-images \
    --repository-name "$repository_name" \
    --region "$AWS_REGION" \
    --profile "$AWS_PROFILE" \
    --query 'sort_by(imageDetails,& imagePushedAt)[-1].imageTags' \
    --output json |
    jq -r '[.[]? | select(. != "latest")][0] // "latest"'
}

update_values_file() {
  local service="$1"
  local repository_url="$2"
  local image_tag="$3"
  local file="$HELM_DIR/$service/values.yaml"

  echo "Updating $file"
  IMAGE_REPOSITORY="$repository_url" \
    IMAGE_TAG="$image_tag" \
    DB_HOST="$db_host" \
    REDIS_ADDR="$redis_addr" \
    ANALYSIS_QUEUE_URL="$analysis_queue_url" \
    NOTIFICATION_QUEUE_URL="$notification_queue_url" \
    S3_BUCKET="$s3_bucket_name" \
    DB_PASSWORD="$db_password" \
    perl -0pi -e '
      my $password = $ENV{"DB_PASSWORD"};
      $password =~ s/'"'"'/'"'"''"'"'/g;

      s#(^  repository: ).*$#$1$ENV{"IMAGE_REPOSITORY"}#m;
      s#(^  tag: ).*$#$1$ENV{"IMAGE_TAG"}#m;
      s#(^  dbHost: ).*$#$1$ENV{"DB_HOST"}#m;
      s#(^  redisAddr: ).*$#$1$ENV{"REDIS_ADDR"}#m;
      s#(^  analysisQueueUrl: ).*$#$1$ENV{"ANALYSIS_QUEUE_URL"}#m;
      s#(^  notificationQueueUrl: ).*$#$1$ENV{"NOTIFICATION_QUEUE_URL"}#m;
      s#(^  s3Bucket: ).*$#$1$ENV{"S3_BUCKET"}#m;
      s#(^  dbPassword: ).*$#$1'\''$password'\''#m;
    ' "$file"
}

services=("user-service" "score-service" "analysis-worker" "notification-worker")

for service in "${services[@]}"; do
  repository_url="$(jq -r --arg service "$service" '.ecr_repository_urls.value[$service]' <<<"$tf_outputs")"
  image_tag="$(latest_ecr_tag "$repository_url")"
  update_values_file "$service" "$repository_url" "$image_tag"
done

echo "Updating $KARPENTER_NODE_CLASS"
PRIVATE_SUBNET_IDS="$private_subnet_ids" perl -0pi -e '
  my @subnet_ids = split /,/, $ENV{"PRIVATE_SUBNET_IDS"};
  my $replacement = "subnetSelectorTerms:\n" . join("", map { "  - id: $_\n" } @subnet_ids);
  s/subnetSelectorTerms:\n(?:  - id: .+\n)+/$replacement/s;
' "$KARPENTER_NODE_CLASS"

echo "Refresh complete."
