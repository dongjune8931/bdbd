# bodybuddy-infra

Terraform, ArgoCD, runbook, and report assets for the BodyBuddy AWS environment.

This repository area is the operational side of BodyBuddy. It describes how the dev environment is created, how GitOps converges the cluster, and how recovery and load-test evidence is preserved.

## Scope

| Area | Contents |
|---|---|
| Terraform | VPC, EKS, Karpenter, RDS, ElastiCache, S3, SQS, ECR, IAM/IRSA |
| GitOps | ArgoCD App-of-Apps, service applications, observability, cost tooling |
| Recovery | RDS PITR, S3 object recovery, GitOps cluster recovery |
| Chaos drills | One-off LitmusChaos dev drills for measured resilience evidence |
| Reports | RTO/RPO matrix, Spot interruption drill, load-test results, chaos drill results |

## Directory Map

```text
terraform/
  envs/dev/             # dev environment entrypoint
  modules/              # reusable AWS infrastructure modules

argocd/
  app-of-apps.yaml      # root application
  apps/                 # child applications and AppProject
  karpenter/            # NodePool and EC2NodeClass manifests

chaos/
  litmus/               # one-off LitmusChaos dev drill manifests

runbooks/
  destroy-reapply-recovery.md
  gitops-cluster-recovery.md
  litmuschaos-dev-drill.md
  rds-pitr-restore.md
  s3-mass-delete-recovery.md

reports/
  README.md
  09-litmuschaos-dev-drill.md
  rto-rpo-matrix.md
  load-test-report.md
  evidence/
```

## Main Reports

- [RTO / RPO Matrix](./reports/rto-rpo-matrix.md)
- [Load Test Report](./reports/load-test-report.md)
- [LitmusChaos Dev Drill Report](./reports/09-litmuschaos-dev-drill.md)
- [Reports Index](./reports/README.md)

Detailed drill reports and raw screenshots live under `reports/` and `reports/evidence/`.

## Runbooks

- [Destroy / Reapply Recovery](./runbooks/destroy-reapply-recovery.md)
- [LitmusChaos Dev Drill](./runbooks/litmuschaos-dev-drill.md)
- [RDS PITR Restore](./runbooks/rds-pitr-restore.md)
- [S3 Mass Delete Recovery](./runbooks/s3-mass-delete-recovery.md)
- [GitOps Cluster Recovery](./runbooks/gitops-cluster-recovery.md)

## Apply Flow

```bash
AWS_PROFILE=terraform-bodybuddy \
terraform -chdir=bodybuddy-infra/terraform/envs/dev apply
```

After the AWS resources are recreated, bootstrap cluster add-ons:

```bash
AWS_PROFILE=terraform-bodybuddy \
bodybuddy-infra/scripts/bootstrap-cluster-addons.sh
```

Then refresh values that drift after recreation:

```bash
AWS_PROFILE=terraform-bodybuddy \
bodybuddy-infra/scripts/refresh-dev-values.sh
```

## Verification

```bash
kubectl get applications -n bodybuddy-system
kubectl get pods -n bodybuddy
kubectl get nodes --show-labels
kubectl get hpa -n bodybuddy
```

Expected steady state:

- ArgoCD applications are `Synced Healthy`
- API workloads run on `critical-pool / on-demand`
- Worker workloads run on `batch-pool / spot`
- `score-service` HPA is available after metrics-server is synced

## Cost Note

The dev environment is not intended to run all day. When testing is finished, destroy the environment to avoid idle EKS, RDS, ElastiCache, ALB, and node cost.
