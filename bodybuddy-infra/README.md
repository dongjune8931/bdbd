# bodybuddy-infra

Terraform and GitOps assets for BodyBuddy infrastructure.

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
