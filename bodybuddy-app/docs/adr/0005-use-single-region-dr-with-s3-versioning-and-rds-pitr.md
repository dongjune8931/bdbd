# 0005. Use Single-Region DR With S3 Versioning and RDS PITR

## Status

Accepted

## Context

BodyBuddy is a portfolio project that needs a serious recovery story without the cost and complexity of a multi-region production architecture. The most important stateful resources are RDS PostgreSQL for user and score data, and S3 for original InBody uploads.

The goal is not to promise zero downtime for a regional outage. The goal is to prove that data loss scenarios are anticipated, measurable, and recoverable with documented runbooks and recorded evidence.

## Decision

We use a single AWS region and implement recovery around the actual state boundaries:

- RDS PostgreSQL uses automated backups with point-in-time recovery.
- S3 uses versioning and delete-marker based recovery.
- S3 delete events are routed through EventBridge to a Go Lambda that removes the latest delete marker when recovery is required.
- Recovery evidence is recorded in reports and runbooks rather than left as an undocumented console exercise.

## Alternatives Considered

Multi-region active-active was rejected because it would add DNS failover, data replication, conflict handling, and operational cost that are disproportionate to the project size.

Velero was rejected because Kubernetes manifests are already recoverable through GitOps, while the real durable state lives in managed AWS services.

Manual-only restore was rejected because it does not demonstrate automation or measurable recovery behavior.

## Consequences

This keeps the recovery design understandable and affordable while still producing strong talking points:

- S3 object deletion can be recovered automatically when version history is intact.
- RDS logical mistakes can be recovered by restoring a new instance to a known timestamp.
- RTO and RPO can be measured and documented for each failure type.

The tradeoff is that a full regional outage is outside scope. This is intentional. The project demonstrates practical single-region resilience, not enterprise multi-region continuity.

## Validation

S3 recovery was tested by deleting a versioned object and confirming the Lambda log emitted `recovered=true` with a measured duration. RDS recovery was tested by inserting and deleting a known row, then restoring a separate DB instance to a timestamp before the deletion and validating the expected data.

Related evidence is recorded in the infrastructure reports and runbooks.
