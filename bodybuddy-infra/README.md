# bodybuddy-infra

Terraform and GitOps assets for BodyBuddy infrastructure.

## Demo Reports

- [Phase 6: Karpenter + Spot Drill](./reports/phase-6-spot-interruption-drill.md)
- [Phase 7: DR Drill - S3 Auto Recovery and RDS PITR](./reports/phase-7-dr-drill.md)
- [RTO / RPO Matrix](./reports/rto-rpo-matrix.md)
- [Phase 7 Evidence Index](./reports/evidence/phase-7-dr/README.md)
- [RDS PITR Restore Runbook](./runbooks/rds-pitr-restore.md)
- [S3 Mass Delete Recovery Runbook](./runbooks/s3-mass-delete-recovery.md)
- [GitOps Cluster Recovery Runbook](./runbooks/gitops-cluster-recovery.md)

앞으로 시연/드릴 문서는 `reports/` 아래에 phase별 문서로 추가하고, 캡처와 원본 증거는 `reports/evidence/` 아래에 연결하는 방식으로 정리한다.

## Phase 2 scope

This repository starts Phase 2 by scaffolding:

- Terraform repository structure
- `terraform/envs/dev` entrypoint
- Shared tags and naming locals
- Initial module implementations for `vpc`, `s3`, `sqs`, and `ecr`
- Module skeletons for `eks`, `karpenter`, `rds`, `elasticache`, and `iam-irsa`

## Important notes

- Replace placeholder values such as `<ACCOUNT_ID>` and `<GITHUB_USER>` before real apply.
- The Terraform backend bucket and lock table are intentionally not created here.
- Backend bootstrap must be done manually once, per the project spec.

## Next slice

The next Phase 2 iteration should implement:

1. EKS cluster wrapper
2. Karpenter bootstrap path
3. RDS and ElastiCache modules
4. `envs/dev` wiring for the remaining modules
