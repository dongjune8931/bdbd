# BodyBuddy

> 인바디 결과를 점수화해 캐릭터를 키우는 헬스 서비스 — AWS EKS 위에서 비동기 MSA를 운영하며 GitOps, 관측성, Spot 비용 최적화, DR을 실제 증거로 구현한 클라우드 인프라 설계를 목표로 합니다

## 메인 아키텍처

![BodyBuddy Architecture](./bdbd_main_arc.drawio.png)

### 인바디 업로드 비동기 처리 흐름

![Inbody Upload Async Flow](./inbody_upload_diagram.png)

## 기술 스택

| 분류 | 기술 |
|---|---|
| **언어 / 프레임워크** | Go 1.24, Gin, pgx/v5, go-redis/v9, aws-sdk-go-v2 |
| **로깅 / 메트릭 / 트레이싱** | slog (JSON), Prometheus, OpenTelemetry (OTLP), Grafana Tempo |
| **컨테이너** | Docker (멀티스테이지), distroless/static-debian12:nonroot |
| **오케스트레이션** | AWS EKS 1.33, Karpenter v1.x (critical-pool on-demand / batch-pool Spot) |
| **GitOps / CI-CD** | ArgoCD (App-of-Apps), GitHub Actions (OIDC → ECR push) |
| **IaC** | Terraform ~>1.9 (VPC, EKS, Karpenter, RDS, ElastiCache, S3, SQS, Lambda, ECR, IAM/IRSA) |
| **데이터 스토어** | RDS PostgreSQL 15 (PITR, SSE), ElastiCache Redis 7 (TLS, Sorted Set 랭킹) |
| **메시징** | SQS (analysis-queue / notification-queue + DLQ), EventBridge |
| **보안** | IRSA (서비스별 최소 권한), AWS Secrets Manager, SSE-KMS, S3 Object Lock |
| **관측성 스택** | kube-prometheus-stack, OTel Collector, Grafana Tempo, KubeCost |
| **부하 테스트** | k6 (upload-burst / ranking-read 시나리오) |
| **이메일** | AWS SES |

## What This Project Proves

| 영역 | 구현 내용 | 검증 증거 |
|---|---|---|
| MSA on EKS | Go 기반 API 2개와 worker 2개를 EKS에 배포 | ArgoCD 전체 `Synced Healthy`, 서비스별 readiness 확인 |
| GitOps | ArgoCD App-of-Apps로 Helm chart 관리 | Git 변경 후 자동 sync, self-heal 운영 |
| 비동기 처리 | `user-service -> SQS -> analysis-worker -> score-service` 흐름 | 업로드 후 score 반영, worker 로그 |
| Spot 운영 | API는 on-demand, worker는 Spot 노드로 분리 | Spot 노드 drain, re-queue, 새 worker 재처리 |
| DR | S3 자동 복구, RDS PITR, 복구 런북 | Lambda 로그, RDS restore, RTO/RPO 매트릭스 |
| 부하 테스트 | k6 기반 upload/ranking 측정과 HPA 튜닝 | `p95 417.59ms -> 35.65ms`, `1 -> 4 replicas` |

## Architecture

```text
Client
  |
  | POST /api/v1/uploads
  v
user-service
  |
  | SQS message
  v
analysis-worker
  |
  | POST /internal/v1/score
  v
score-service
  |
  | notification event
  v
notification-worker
```

Runtime placement:

| Workload | NodePool | Capacity | Reason |
|---|---|---|---|
| `user-service` | `critical-pool` | on-demand | user-facing API, lower latency sensitivity |
| `score-service` | `critical-pool` | on-demand | ranking/read API, HPA target |
| `analysis-worker` | `batch-pool` | spot | async processing, retryable with SQS |
| `notification-worker` | `batch-pool` | spot | async notification, delay-tolerant |

## Repository Layout

```text
bodybuddy-app/
  cmd/                 # Go service entrypoints
  internal/            # auth, db, cache, queue, domain, observability
  deploy/helm/         # service Helm charts
  test/load/           # k6 load test scripts
  migrations/          # PostgreSQL schema

bodybuddy-infra/
  terraform/           # AWS infrastructure modules and dev entrypoint
  argocd/              # App-of-Apps and application manifests
  runbooks/            # operational recovery procedures
  reports/             # drill reports, RTO/RPO, load test results
```

## Key Results

### Spot Interruption Drill

Worker Pod가 올라간 Spot 노드를 drain해 interruption 흐름을 재현했다.

Observed behavior:

- `analysis-worker`가 처리 중이던 메시지를 중단 시점에 re-queue
- 새 Spot 노드에 worker가 재스케줄
- re-queued 메시지가 새 worker에서 다시 처리
- 최종 score 반영으로 메시지 유실 없이 복구 확인

Core log:

```text
context cancelled during mock OCR sleep, re-queuing message
shutdown signal received, stopping polling loop
all in-flight messages processed
```

### DR Drill

S3와 RDS에 대해 실제 복구 흐름을 실행했다.

| Scenario | Result |
|---|---|
| S3 object delete | Lambda가 delete marker를 제거해 약 `1.247s`에 자동 복구 |
| RDS data loss | PITR restore로 새 DB 인스턴스 `available` 도달 확인 |
| Recovery matrix | RTO/RPO 목표와 실측값을 표로 정리 |

### Load Test and Autoscaling

`score-service` ranking read 부하를 기준으로 HPA 적용 전후를 비교했다.

| Test | Before | After |
|---|---:|---:|
| Throughput | `377.35 req/s` | `572.03 req/s` |
| p95 latency | `417.59ms` | `35.65ms` |
| Error rate | `0%` | `0%` |
| Replicas | `1` | `4` |

## Main Documents

- [Infrastructure README](./bodybuddy-infra/README.md)
- [Application README](./bodybuddy-app/README.md)
- [Architecture Overview](./bodybuddy-app/docs/architecture.md)
- [Architecture Decisions](./bodybuddy-app/docs/adr/README.md)
- [RTO / RPO Matrix](./bodybuddy-infra/reports/rto-rpo-matrix.md)
- [Load Test Report](./bodybuddy-infra/reports/load-test-report.md)
- [Reports Index](./bodybuddy-infra/reports/README.md)
- [RDS PITR Runbook](./bodybuddy-infra/runbooks/rds-pitr-restore.md)
- [S3 Recovery Runbook](./bodybuddy-infra/runbooks/s3-mass-delete-recovery.md)
- [GitOps Recovery Runbook](./bodybuddy-infra/runbooks/gitops-cluster-recovery.md)
- [Demo Video Script](./bodybuddy-infra/reports/demo-video-script.md)

## Local Load Test

Upload burst:

```bash
cd bodybuddy-app
K6_BASE_URL=http://localhost:8080 \
K6_TOKEN=<JWT_TOKEN> \
make load-upload-smoke
```

Ranking read:

```bash
cd bodybuddy-app
K6_SCORE_BASE_URL=http://localhost:8081 \
make load-ranking-smoke
```

## Operational Notes

- Dev infrastructure is designed to be destroyed when not in use.
- After destroy/reapply, run the recovery checklist before expecting GitOps to converge.
- Some values are regenerated by AWS, especially RDS endpoint/password, Redis endpoint, subnet IDs, and EKS OIDC provider.
- `bodybuddy-infra/scripts/refresh-dev-values.sh` exists to reduce manual drift after recreation.
