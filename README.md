# BodyBuddy

> 인바디 결과를 점수화해 캐릭터를 키우는 헬스 서비스 — AWS EKS 위에서 비동기 MSA를 운영하며 GitOps, 관측성, Spot 비용 최적화, DR을 실제 증거로 구현한 클라우드 인프라 설계를 목표로 합니다

## 메인 아키텍처

<img src="./bdbd_main_arc.drawio.png" width="700" />

### 인바디 업로드 비동기 처리 흐름

<img src="./inbody_upload_diagram.png" width="700" />

### 서비스 흐름

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

---

## What This Project Proves

| 영역 | 구현 내용 | 검증 증거 |
|---|---|---|
| MSA on EKS | Go 기반 API 2개와 worker 2개를 EKS에 배포 | ArgoCD 전체 `Synced Healthy`, 서비스별 readiness 확인 |
| GitOps | ArgoCD App-of-Apps로 Helm chart 관리 | Git 변경 후 자동 sync, self-heal 운영 |
| 비동기 처리 | `user-service -> SQS -> analysis-worker -> score-service` 흐름 | 업로드 후 score 반영, worker 로그 |
| Spot 운영 | API는 on-demand, worker는 Spot 노드로 분리 | Spot 노드 drain, re-queue, 새 worker 재처리 |
| DR | S3 자동 복구, RDS PITR, 복구 런북 | Lambda 로그, RDS restore, RTO/RPO 매트릭스 |
| 부하 테스트 | k6 기반 upload/ranking 측정과 HPA 튜닝 | `p95 417.59ms -> 35.65ms`, `1 -> 4 replicas` |

---

## DR

현재 BodyBuddy에 구현한 복구 전략은 세 가지다. Kubernetes와 ArgoCD는 Pod/Deployment 삭제 시 선언형 상태를 기준으로 서비스를 다시 복구하고, batch 워커는 SQS 메시지를 성공 시에만 삭제하도록 구성해 interruption 이후에도 다른 worker가 메시지를 재처리할 수 있다. 여기에 `upload_id` 기반 멱등성 처리를 더해 동일한 분석 결과가 다시 들어와도 중복 반영되지 않게 했다.

| 장애 유형 | 구현 방식 | 현재 보장하는 것 |
|---|---|---|
| Pod / Deployment 삭제 | ReplicaSet + ArgoCD self-heal | 서비스 프로세스 자동 재생성, desired state 복구 |
| Spot / worker 종료 | SIGTERM graceful shutdown + SQS 재노출 | 신규 polling 중단, in-flight 처리 후 종료, 다른 worker 재처리 |
| S3 객체 삭제 | Versioning + Object Lock + EventBridge + Lambda | 최신 delete marker 제거, 이전 버전 자동 복원, 메일/대시보드 기록 |

### Spot 중단 시 Worker 재처리 흐름

```text
                    Spot Node
                       │
          ┌────────────┴────────────┐
          │      analysis-worker    │
          │   · polls SQS           │
          │   · processes message   │
          └────────────┬────────────┘
                       │
              Interruption notice
              (node drain / SIGTERM)
                       │
          ┌────────────┴────────────┐
          │    Graceful shutdown    │
          │  · stop polling         │
          │  · finish in-flight     │
          │  · NO DeleteMessage     │
          └────────────┬────────────┘
                       │
            visibility timeout expires
                       │
          ┌────────────┴────────────┐
          │   Message reappears     │
          │   in analysis-queue     │
          └────────────┬────────────┘
                       │
          ┌────────────┴────────────┐
          │      New worker         │
          │ rescheduled by Karpenter│
          │   on another node       │
          └────────────┬────────────┘
                       │
          ┌────────────┴────────────┐
          │      score-service      │
          │  · idempotent update    │
          │  · upload_id dedup      │
          └─────────────────────────┘
```

Batch worker는 Spot 노드에서 실행하지만, SQS visibility timeout과 멱등성 처리로 interruption 이후에도 메시지 유실 없이 안전하게 재처리할 수 있도록 설계했다.

Core log:

```text
context cancelled during mock OCR sleep, re-queuing message
shutdown signal received, stopping polling loop
all in-flight messages processed
```

### S3 백업 / 복구

<img src="./s3_dr.png" width="700" />

S3 객체 삭제 이벤트는 EventBridge와 Lambda로 연결하고, 최신 delete marker를 제거해 이전 버전을 다시 현재 버전으로 복원한다. 복구 결과는 SES 메일과 Grafana 대시보드로 함께 남겨 운영자가 삭제 감지 시점의 상태와 복구 성공 여부를 바로 확인할 수 있게 했다.

<div align="center">
  <img src="https://github.com/user-attachments/assets/c62702fe-54e7-41b1-b594-50aa24b4d4df" width="48%" alt="자동 복구 알림 메일" />
  &nbsp;
  <img src="https://github.com/user-attachments/assets/18d0561b-629a-4ce6-89b0-c3ebf1ad9eb8" width="48%" alt="DR 대시보드" />
</div>
<p align="center"><em>자동 복구 알림 메일 &nbsp;&nbsp;&nbsp; DR 대시보드</em></p>

메일에는 삭제 비율, 임계값, 삭제 마커 수, 복구 결과를 요약했고, 대시보드에는 자동 복구 호출 수, 복구된 객체 수, 실패 수, 복구 전 삭제 비율을 시각화했다.

### RDS PITR

| Scenario | Result |
|---|---|
| S3 object delete | Lambda가 delete marker를 제거해 약 `1.247s`에 자동 복구 |
| RDS data loss | PITR restore로 새 DB 인스턴스 `available` 도달 확인 |
| Recovery matrix | RTO/RPO 목표와 실측값을 표로 정리 |

---

## 관측성

kube-prometheus-stack으로 Prometheus + Grafana를 올리고, CloudWatch 데이터소스를 연결해 서비스 RED 메트릭과 DR 지표를 단일 화면에서 확인할 수 있도록 구성했다.

<img width="700" alt="01-bodybuddy-service-overview-dashboard" src="https://github.com/user-attachments/assets/5622e182-e644-4dfa-9466-8ccf1b92f7d8" />

### OpenTelemetry Critical Path Trace

OpenTelemetry와 Tempo를 이용해 업로드 요청 이후 `user-service -> analysis-worker -> score-service`로 이어지는 크리티컬 패스를 실제 trace로 검증했다. 비동기 워커 처리 구간과 내부 `HTTP POST`, 그리고 `score-service`의 server span까지 한 trace 안에서 연결되는 것을 확인했다.

#### 1. Node Graph

<!-- TODO: Node graph 캡처를 아래에 추가 -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/07-otel-node-graph.png -->
<img width="995" height="305" alt="스크린샷 2026-05-21 오후 9 05 19" src="https://github.com/user-attachments/assets/9d145f8b-faac-4e38-b6a7-3c200b2dd2fb" />

Node graph는 서비스 간 호출 관계를 한 눈에 보여준다. 업로드 요청이 `user-service`에서 시작되고, `analysis-worker`를 거쳐 `score-service`의 내부 API로 연결되는 전체 흐름을 서비스 단위로 설명할 때 사용한다.

#### 2. Waterfall Trace

<!-- TODO: Waterfall trace 캡처를 아래에 추가 -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/08-otel-trace-waterfall.png -->
<img width="1168" height="382" alt="스크린샷 2026-05-21 오후 9 05 34" src="https://github.com/user-attachments/assets/ff3c4bf8-eb15-4569-9ad3-ff3f0e4c9e58" />

Waterfall 화면에서는 전체 요청 시간 중 어느 구간이 오래 걸렸는지 확인할 수 있다. 현재 구현에서는 `analysis-worker.processMessage` span이 가장 긴 구간으로 나타나며, Mock OCR 지연과 비동기 분석 비용이 trace 상에서 직접 드러난다.

#### 3. Span Details

<!-- TODO: Span details 캡처를 아래에 추가 -->
<img width="1174" height="884" alt="스크린샷 2026-05-21 오후 9 05 54" src="https://github.com/user-attachments/assets/12f701bd-12bb-4fa5-94d1-0a13a69809c9" />
Span detail에서는 `analysis-worker`의 outbound `HTTP POST`와 `score-service /internal/v1/score` server span을 함께 확인할 수 있다. 이를 통해 worker가 실제로 `score-service.bodybuddy.svc.cluster.local`로 내부 호출을 수행했고, 최종 서비스까지 trace가 전파되었음을 증명한다.

### Async Backlog Bottleneck and Recovery

랭킹 조회 path는 Redis hit 비율이 높아 `N=300~1000` 구간에서도 비교적 안정적으로 버텼다. 대신 더 의미 있는 병목은 비동기 분석 파이프라인에서 드러났다. 업로드 burst 동안 `user-service`는 빠르게 요청을 받아들이지만, `analysis-worker`가 1개일 때는 메시지 처리량이 따라가지 못해 queue backlog가 빠르게 증가했다.

이 병목은 `SQS queue depth`, `analysis-worker replica`, `CPU usage`, `job duration`을 함께 관측해 확인했고, 이후 KEDA가 queue depth를 기준으로 worker를 최대 12개까지 자동 확장하도록 수정해 backlog가 실제로 감소하기 시작하는 것을 검증했다.

#### Before: Single Worker Bottleneck

<!-- TODO: before 구간 캡처를 아래에 추가 -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/10-analysis-queue-depth-before.png -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/11-analysis-worker-replicas-before.png -->

업로드 burst(`2026-05-22 01:50:44 ~ 01:53:44 KST`) 동안 `user-service`는 약 8,900건의 업로드 요청을 정상 수락했고 `p95 30.35ms`, `error 0%`를 유지했다. 하지만 같은 시점의 `analysis-worker`는 단일 replica로 고정되어 있었고, 그 결과 `analysis-queue` backlog가 `8,816`건까지 누적됐다.

- Upload accepted: `8,900`
- API p95: `30.35ms`
- Queue backlog: `8,816`
- Worker replicas: `1`

#### After: KEDA-driven Worker Scale-out

<img width="1400" height="300" alt="스크린샷 2026-05-22 오전 3 22 11" src="https://github.com/user-attachments/assets/b5c8b72c-547f-4ffd-95e5-5fb9e19d98c0" />
<img width="689" height="259" alt="스크린샷 2026-05-22 오전 3 20 34" src="https://github.com/user-attachments/assets/19e5c13f-3200-4841-ad4d-a17eabc0d6ae" />


KEDA 설정을 수정한 뒤에는 `analysis-worker`가 queue depth를 external metric으로 정상 인식했고, `1 -> 12 replicas`까지 자동 확장됐다. 그 결과 `analysis-queue` backlog는 `~8,055` 수준에서 계속 감소하기 시작했고, worker CPU 사용량도 같은 시점에 함께 상승해 실제로 새 worker들이 작업을 처리하고 있음을 확인했다.

- Queue depth observed: `~8,055` and decreasing
- Worker replicas: `1 -> 12`
- Worker CPU: scale-out 시점 이후 뚜렷한 상승
- Analysis job duration: `avg ~3.5s`, `p95 ~4.8~5.0s`

핵심 해석은, 병목이 개별 job latency가 아니라 **worker capacity 부족으로 인한 queue backlog**였다는 점이다. Job duration 자체는 큰 폭으로 흔들리지 않았지만, worker 수가 부족해 backlog가 누적됐고, autoscaling으로 처리량을 늘리자 backlog가 감소하기 시작했다.

#### Ranking Read Load Verification

랭킹 조회 경로는 별도 관측 대상이었다. `score-service`의 `GET /api/v1/ranking` trace와 `Ranking Read Duration`, `CPU Usage`, `Ready Replicas` 패널을 함께 관찰해 `N=10 -> 300` 구간에서 latency 증가, CPU 상승, HPA scale-out이 실제로 연결되어 동작함을 검증했다. 이 실험은 dramatic failure를 만들기보다는 **관측성과 오토스케일링 반응을 확인하는 목적**으로 사용했다.

<!-- TODO: ranking 관측 캡처를 아래에 추가 -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/16-ranking-read-duration.png -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/17-score-service-ready-replicas.png -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/18-score-service-cpu-usage.png -->
<!-- 권장 파일: bodybuddy-infra/reports/evidence/05-observability/19-score-service-ranking-trace.png -->

이 섹션은 “부하가 올라가면 지표가 어떻게 반응하는가”를 보여주는 근거로 사용하고, 실제 병목 발견 + 해결 스토리는 위의 `analysis-worker` backlog 사례에 집중한다.

---

## 부하 테스트 및 오토스케일링

이번 프로젝트에서 더 의미 있게 드러난 병목은 `score-service` read path보다 비동기 분석 파이프라인이었다. 업로드 burst 시 API 응답은 빠르게 유지됐지만, 단일 `analysis-worker`가 backlog를 따라가지 못해 queue depth가 빠르게 증가했다. 이후 KEDA가 SQS depth를 external metric으로 읽어 worker를 자동 확장하면서 backlog가 감소하기 시작했다.

| Scenario | Before | After |
|---|---:|---:|
| Upload accepted | `8,900` | `8,900` burst 기준 처리 지속 |
| API p95 | `30.35ms` | burst 동안 안정 유지 |
| Queue backlog | `8,816` | `~8,055`에서 감소 시작 |
| Worker replicas | `1` | `1 -> 12` |
| Analysis job duration | `avg ~3.5s`, `p95 ~4.8~5.0s` | 큰 변화 없이 안정적 |

즉 병목은 개별 job latency가 아니라 **worker capacity 부족**이었고, 해결은 **KEDA 기반 SQS autoscaling**이었다. 랭킹 조회 부하는 별도 관측 실험으로 유지했고, 해당 경로는 Redis hit 비율이 높아 `N=300~1000` 구간에서도 비교적 안정적으로 동작했다.

---

## 비용 최적화

KubeCost로 클러스터 비용을 가시화하고, Karpenter consolidation으로 미사용 노드를 자동 정리해 절감 기회를 식별했다.

<!-- kubecost savings 캡처 -->
![]()

---

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

---

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

<!--
---

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

---

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

---

## Operational Notes

- Dev infrastructure is designed to be destroyed when not in use.
- After destroy/reapply, run the recovery checklist before expecting GitOps to converge.
- Some values are regenerated by AWS, especially RDS endpoint/password, Redis endpoint, subnet IDs, and EKS OIDC provider.
- `bodybuddy-infra/scripts/refresh-dev-values.sh` exists to reduce manual drift after recreation.
-->
