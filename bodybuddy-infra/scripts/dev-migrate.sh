#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TF_DIR="$ROOT_DIR/bodybuddy-infra/terraform/envs/dev"
MIGRATION_FILE="$ROOT_DIR/bodybuddy-app/migrations/000001_init.up.sql"

AWS_PROFILE="${AWS_PROFILE:-terraform-bodybuddy}"
AWS_REGION="${AWS_REGION:-ap-northeast-2}"
NAMESPACE="${NAMESPACE:-bodybuddy}"
DB_USER="${DB_USER:-bodybuddy}"
DB_NAME="${DB_NAME:-bodybuddy}"
CLEANUP="${CLEANUP:-1}"

configmap_name="bodybuddy-migration"
job_name="bodybuddy-migration"

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

echo "Reading Terraform outputs from $TF_DIR"
tf_outputs="$(AWS_PROFILE="$AWS_PROFILE" terraform -chdir="$TF_DIR" output -json)"
cluster_name="$(jq -r '.eks_cluster_name.value' <<<"$tf_outputs")"
rds_endpoint="$(jq -r '.rds_endpoint.value' <<<"$tf_outputs")"
db_host="${rds_endpoint%:*}"

echo "Updating kubeconfig for $cluster_name"
aws eks update-kubeconfig \
  --name "$cluster_name" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" >/dev/null

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

echo "Applying migration ConfigMap"
kubectl create configmap "$configmap_name" \
  -n "$NAMESPACE" \
  --from-file=init.sql="$MIGRATION_FILE" \
  --dry-run=client \
  -o yaml |
  kubectl apply -f - >/dev/null

kubectl delete job "$job_name" -n "$NAMESPACE" --ignore-not-found >/dev/null

echo "Running migration job against $db_host"
kubectl apply -f - >/dev/null <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: $job_name
  namespace: $NAMESPACE
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: bodybuddy-migration
        app.kubernetes.io/part-of: bodybuddy
    spec:
      restartPolicy: Never
      containers:
        - name: psql
          image: postgres:16-alpine
          env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: user-service
                  key: DB_PASSWORD
          command:
            - sh
            - -c
            - |
              set -eu
              exists="\$(psql -h "$db_host" -U "$DB_USER" -d "$DB_NAME" -tAc "SELECT CASE WHEN to_regclass('public.users') IS NULL THEN 'missing' ELSE 'present' END")"
              if [ "\$exists" = "present" ]; then
                echo "schema already exists; skipping migration"
                exit 0
              fi
              psql -v ON_ERROR_STOP=1 \
                -h "$db_host" \
                -U "$DB_USER" \
                -d "$DB_NAME" \
                -f /migrations/init.sql
          volumeMounts:
            - name: migration
              mountPath: /migrations
      volumes:
        - name: migration
          configMap:
            name: $configmap_name
YAML

if ! kubectl wait --for=condition=complete "job/$job_name" -n "$NAMESPACE" --timeout=180s; then
  echo "migration job failed; recent logs:" >&2
  kubectl logs "job/$job_name" -n "$NAMESPACE" >&2 || true
  exit 1
fi

kubectl logs "job/$job_name" -n "$NAMESPACE"

if [[ "$CLEANUP" = "1" ]]; then
  kubectl delete "job/$job_name" "configmap/$configmap_name" -n "$NAMESPACE" --ignore-not-found >/dev/null
fi

echo "Migration check complete."

