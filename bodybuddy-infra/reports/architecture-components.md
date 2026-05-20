# BodyBuddy 아키텍처 전체 컴포넌트 정리

> 이 문서는 아키텍처 다이어그램 제작용 레퍼런스다. 프로젝트에서 실제로 사용하는 모든 구성 요소를 빠짐없이 담았다.

---

## 1. 전체 데이터 흐름

```
[클라이언트 (curl / k6)]
    │
    │ ① POST /api/v1/uploads (인바디 사진 업로드 요청)
    ▼
[ALB (internet-facing, AWS Load Balancer Controller)]
    │
    ▼
[user-service] ─── JWT 인증 ───▶ 업로드 레코드 생성 (RDS)
    │                              │
    │ ② SQS SendMessage            │ presigned URL 발급 → S3 직접 업로드
    │   (W3C TraceContext 전파)     │
    ▼                              ▼
[SQS: analysis-queue]          [S3: bodybuddy-dev-inbody]
    │                              │
    │ ③ Long-poll receive          │ ObjectCreated 이벤트 (EventBridge)
    ▼                              ▼
[analysis-worker]              [EventBridge Rule]
    │                              │
    │ Mock OCR (2~5초 sleep)       │ DeleteObject 이벤트 감지
    │ 점수 계산 (seed-based RNG)    ▼
    │                          [Lambda: s3-auto-recovery]
    │ ④ POST /internal/v1/score    │ delete marker 제거 → 객체 복구
    ▼                              │ CloudWatch metric 발행
[score-service]                    ▼
    │                          [CloudWatch Metrics]
    │ ⑤ RDS: score_history INSERT (idempotency: upload_id UNIQUE)
    │    RDS: character UPDATE (total_score, level)
    │    Redis: ZADD bodybuddy:ranking (실시간 랭킹)
    │
    │ ⑥ SQS SendMessage → notification-queue
    ▼
[SQS: notification-queue]
    │
    │ ⑦ Long-poll receive
    ▼
[notification-worker]
    │
    │ ⑧ RDS에서 user email 조회
    │    SES로 이메일 발송
    ▼
[SES] → [사용자 이메일]
```

---

## 2. AWS 리소스 전체 목록

### 2.1 컴퓨팅

| 리소스 | 이름/설정 | 용도 |
|--------|----------|------|
| EKS Cluster | `bodybuddy-dev-eks`, v1.33 | K8s 컨트롤 플레인 |
| EKS Bootstrap Node Group | t3.medium × 1, on-demand | 초기 시스템 Pod 스케줄링 |
| Karpenter (EC2) | critical-pool: on-demand (t3.medium/large, m6i.large) | API 서비스 노드 |
| Karpenter (EC2) | batch-pool: spot + on-demand fallback (t3.medium/large, m6i.large, m6a.large) | 워커 서비스 노드 |
| Lambda | `bodybuddy-dev-s3-auto-recovery`, Go, arm64, provided.al2023 | S3 삭제 객체 자동 복구 |

### 2.2 네트워킹

| 리소스 | 설정 | 용도 |
|--------|------|------|
| VPC | `10.20.0.0/16` | 전체 네트워크 |
| Public Subnet | `10.20.0.0/24` (ap-northeast-2a), `10.20.1.0/24` (ap-northeast-2c) | ALB, NAT Gateway |
| Private Subnet | `10.20.10.0/24` (ap-northeast-2a), `10.20.11.0/24` (ap-northeast-2c) | EKS 노드, RDS, ElastiCache |
| NAT Gateway | 단일 (비용 최적화) | Private → Internet 아웃바운드 |
| S3 Gateway VPC Endpoint | Private subnet 라우팅 | S3 트래픽 NAT 우회 (비용 절감) |
| ALB | AWS Load Balancer Controller가 생성, internet-facing | user-service 외부 노출 |
| Security Group (ALB) | `bodybuddy-dev-user-service-alb` | ALB 인바운드 제어 |
| Security Group (EKS) | 클러스터 보안 그룹 | 노드 ↔ 컨트롤플레인 통신 |
| Security Group (RDS) | EKS 클러스터 SG에서만 인바운드 | DB 접근 제한 |
| Security Group (ElastiCache) | EKS 클러스터 SG에서만 인바운드 | Redis 접근 제한 |

### 2.3 스토리지 / 데이터베이스

| 리소스 | 이름/설정 | 용도 |
|--------|----------|------|
| RDS PostgreSQL 15 | `bodybuddy-dev-postgres`, db.t4g.micro, gp3, encrypted, backup 7일 | 사용자, 캐릭터, 점수, 업로드 메타 |
| ElastiCache Redis 7 | cache.t4g.micro, 단일 노드, TLS 활성화, at-rest 암호화 | 랭킹 Sorted Set, 캐시 |
| S3 | `bodybuddy-dev-inbody`, Versioning ON, Object Lock (Governance), SSE-KMS, EventBridge ON | 인바디 이미지 저장 |

### 2.4 메시징 / 이벤트

| 리소스 | 이름/설정 | 용도 |
|--------|----------|------|
| SQS | `bodybuddy-dev-analysis-queue` (visibility 90s) | 업로드 → 분석 워커 |
| SQS DLQ | `bodybuddy-dev-analysis-queue-dlq` (maxReceiveCount=3) | 분석 실패 메시지 격리 |
| SQS | `bodybuddy-dev-notification-queue` (visibility 60s) | 점수 완료 → 알림 워커 |
| SQS DLQ | `bodybuddy-dev-notification-queue-dlq` (maxReceiveCount=3) | 알림 실패 메시지 격리 |
| SQS | Karpenter 전용 (Spot interruption notice) | Spot 종료 알림 수신 |
| EventBridge Rule | `bodybuddy-dev-s3-auto-recovery-s3-object-deleted` | S3 DeleteObject → Lambda 트리거 |

### 2.5 보안 / IAM

| 리소스 | 이름 | 용도 |
|--------|------|------|
| IRSA Role | `bodybuddy-dev-user-service-irsa` | user-service: SQS, S3 |
| IRSA Role | `bodybuddy-dev-analysis-worker-irsa` | analysis-worker: SQS receive/delete, S3 read |
| IRSA Role | `bodybuddy-dev-notification-worker-irsa` | notification-worker: SQS receive/delete, SES send |
| IRSA Role | AWS Load Balancer Controller | ALB 생성/관리 |
| IAM Role | Karpenter Controller | EC2, pricing, SSM, instance profile |
| IAM Role | Karpenter Node | EKS 노드 + ECR pull |
| Secrets Manager | RDS master password (자동 관리) | DB 비밀번호 |
| KMS | S3 SSE-KMS, RDS 암호화, EBS 암호화 | 저장 데이터 암호화 |

### 2.6 컨테이너 레지스트리

| 리소스 | 이름 |
|--------|------|
| ECR | `bodybuddy-dev-user-service` |
| ECR | `bodybuddy-dev-score-service` |
| ECR | `bodybuddy-dev-analysis-worker` |
| ECR | `bodybuddy-dev-notification-worker` |

### 2.7 관측성 (AWS 측)

| 리소스 | 용도 |
|--------|------|
| CloudWatch Logs | Lambda 실행 로그 (retention 7일) |
| CloudWatch Metrics | `BodyBuddy/DR > S3AutoRecoveryRecoveredObjects` (Lambda 복구 메트릭) |
| SES | notification-worker 이메일 발송 |

---

## 3. Kubernetes 클러스터 내부 컴포넌트

### 3.1 네임스페이스

| 네임스페이스 | 용도 |
|------------|------|
| `bodybuddy` | 4개 애플리케이션 서비스 |
| `bodybuddy-system` | ArgoCD, Prometheus, Grafana, AlertManager, Tempo, OTel Collector |
| `karpenter` | Karpenter 컨트롤러 |
| `kubecost` | KubeCost |
| `kube-system` | metrics-server, coredns, kube-proxy, vpc-cni |

### 3.2 애플리케이션 서비스 (namespace: bodybuddy)

| 서비스 | 타입 | 포트 | NodePool | 주요 의존성 |
|--------|------|------|----------|------------|
| `user-service` | Deployment (Sync HTTP) | 8080 | critical (on-demand) | RDS, Redis, SQS, S3 |
| `score-service` | Deployment (Sync HTTP) | 8081 | critical (on-demand) | RDS, Redis, SQS |
| `analysis-worker` | Deployment (Async SQS consumer) | 9090 (metrics) | batch (spot) | SQS, S3, score-service HTTP |
| `notification-worker` | Deployment (Async SQS consumer) | 9091 (metrics) | batch (spot) | SQS, RDS, SES |

각 서비스 공통 K8s 리소스:
- **Deployment**: terminationGracePeriodSeconds=120, readiness/liveness probe
- **Service**: ClusterIP
- **ServiceAccount**: IRSA annotation (AWS API 호출 서비스만)
- **PDB**: API → minAvailable=1, Worker → maxUnavailable=1
- **ServiceMonitor**: Prometheus scrape (30s interval, /metrics)
- **Secret**: DB 접속정보, JWT 시크릿 등

user-service 전용:
- **Ingress**: ALB (internet-facing), AWS Load Balancer Controller annotation

score-service 전용:
- **HPA**: minReplicas=1, maxReplicas=4, targetCPU=50%

### 3.3 시스템 컴포넌트 (namespace: bodybuddy-system)

| 컴포넌트 | 버전 | 역할 |
|----------|------|------|
| ArgoCD | - | GitOps 배포 (App-of-Apps 패턴, 자동 sync + self-heal + prune) |
| Prometheus | kube-prometheus-stack v72.6.2 | 메트릭 수집, 7일 보관 |
| Grafana | kube-prometheus-stack 내장 | 대시보드 (Prometheus + Tempo 데이터소스) |
| AlertManager | kube-prometheus-stack 내장 | 알림 (DLQ, 5xx, RDS 연결풀) |
| Tempo | v1.10.3 | 분산 트레이싱 저장 (24h 보관, in-memory) |
| OpenTelemetry Collector | v0.110.0 | OTLP 수신 (gRPC:4317, HTTP:4318) → Tempo export |

### 3.4 시스템 컴포넌트 (기타 namespace)

| 컴포넌트 | 네임스페이스 | 역할 |
|----------|------------|------|
| Karpenter Controller | karpenter | 노드 프로비저닝/종료 |
| KubeCost | kubecost | 비용 분석 (Prometheus 연동) |
| metrics-server | kube-system | HPA를 위한 Metrics API |
| AWS Load Balancer Controller | kube-system | ALB Ingress 관리 |
| CoreDNS | kube-system | K8s 내부 DNS |
| kube-proxy | kube-system | K8s 서비스 네트워킹 |
| vpc-cni | kube-system | AWS VPC CNI (Pod IP 할당) |

### 3.5 Karpenter NodePool 설정

| NodePool | 레이블 | 인스턴스 타입 | 용량 타입 | CPU 제한 | Consolidation |
|----------|--------|-------------|----------|---------|---------------|
| critical-pool | workload-type=critical | t3.medium, t3.large, m6i.large | on-demand | 16 코어 | WhenEmpty (30s) |
| batch-pool | workload-type=batch | t3.medium, t3.large, m6i.large, m6a.large | spot (on-demand fallback) | 32 코어 | WhenEmptyOrUnderutilized (1m) |

EC2NodeClass: AMI AL2023, bodybuddy-dev-eks-karpenter-node role, private subnet에 배치

### 3.6 ArgoCD App-of-Apps 구조

```
app-of-apps.yaml (Root)
├── project.yaml (AppProject: bodybuddy)
├── user-service.yaml
├── score-service.yaml
├── analysis-worker.yaml
├── notification-worker.yaml
├── observability.yaml (kube-prometheus-stack)
├── otel-collector.yaml
├── tempo.yaml
├── kubecost.yaml
├── metrics-server.yaml
└── karpenter-capacity.yaml (NodePool + EC2NodeClass)
```

---

## 4. 애플리케이션 상세

### 4.1 Go 서비스 공통

| 항목 | 값 |
|------|------|
| 언어 | Go 1.24+ |
| HTTP 프레임워크 | Gin |
| DB 드라이버 | pgx/v5 (pgxpool) |
| Redis 클라이언트 | go-redis/v9 |
| AWS SDK | aws-sdk-go-v2 (v1 금지) |
| 로깅 | slog (JSON) |
| 설정 | envconfig |
| 메트릭 | prometheus/client_golang |
| 트레이싱 | OpenTelemetry (otelhttp, OTLP gRPC exporter) |
| 빌드 이미지 | golang:1.24-alpine |
| 런타임 이미지 | gcr.io/distroless/static-debian12:nonroot |
| 바이너리 플래그 | CGO_ENABLED=0, -ldflags="-s -w" |
| 이미지 크기 | < 25MB |

### 4.2 내부 패키지 (internal/)

| 패키지 | 역할 |
|--------|------|
| `auth/` | JWT 생성/검증 (HS256, 24h TTL), Gin 미들웨어 |
| `config/` | envconfig 래퍼, 서비스별 구조체 |
| `db/` | pgxpool 래퍼, Ping 헬스체크 |
| `cache/` | redis.Client 래퍼, TLS 지원, Ping 헬스체크 |
| `queue/` | SQS 클라이언트 (Send, Receive, Delete, ChangeVisibility), SQSCarrier/SQSReadCarrier (W3C TraceContext 전파) |
| `observability/` | Prometheus registry + 메트릭 정의, OTel tracer 초기화 (OTLP gRPC) |
| `http/` | 공통 미들웨어 (RequestID, Logger, Recovery), 헬스체크 핸들러 |
| `domain/user.go` | 유저 CRUD (CreateUser, GetUserByEmail, bcrypt 검증) |
| `domain/score.go` | Mock 점수 계산 (CalculateMockScore: muscle 0-40, fat 0-30, BMI 0-30 → total 0-100) |
| `domain/ranking.go` | 점수 업데이트 트랜잭션 (score_history INSERT + character UPDATE + Redis ZADD), 랭킹 조회 |

### 4.3 DB 스키마 (4 테이블)

| 테이블 | 주요 컬럼 | 특이사항 |
|--------|----------|---------|
| `users` | id (UUID PK), email (UNIQUE), password_hash | gen_random_uuid() |
| `characters` | id, user_id (UNIQUE FK), name, level, total_score | 유저 생성 시 자동 생성 |
| `score_history` | id, user_id, upload_id (UNIQUE), score, score_breakdown (JSONB) | upload_id UNIQUE = SQS 멱등성 키 |
| `inbody_uploads` | id, user_id, s3_key, status, idempotency_key (UNIQUE) | status: pending → completed |

### 4.4 분산 트레이싱 경로 (Critical Path)

```
user-service (POST /uploads)
    │ W3C TraceContext → SQS MessageAttributes
    ▼
analysis-worker (SQS consumer)
    │ Extract trace context → child span
    │ POST /internal/v1/score
    ▼
score-service (내부 API)
```

전파 방식: `otel.GetTextMapPropagator().Inject/Extract` + `SQSCarrier` (SQS MessageAttribute 기반)

### 4.5 Prometheus 커스텀 메트릭

| 메트릭 | 타입 | 라벨 |
|--------|------|------|
| `bodybuddy_http_requests_total` | Counter | method, path, status |
| `bodybuddy_http_request_duration_seconds` | Histogram | method, path |
| `bodybuddy_sqs_messages_processed_total` | Counter | queue, status (success/failure) |
| `bodybuddy_sqs_message_duration_seconds` | Histogram | queue |

### 4.6 AlertManager 규칙

| 알림 | 조건 |
|------|------|
| DLQ 메시지 도착 | SQS DLQ에 메시지 적재 시 즉시 |
| API 5xx 비율 | 5분간 1% 이상 |
| RDS 연결풀 포화 | 95% 이상 |

---

## 5. CI/CD 파이프라인

### 5.1 앱 CI (GitHub Actions: ci.yaml)

```
PR → lint + test (go test ./...)
     │
main merge → Docker build (4 서비스, 매트릭스)
              │ 멀티스테이지: golang:1.24-alpine → distroless:nonroot
              ▼
           ECR push (태그: <git-sha> + latest)
```

### 5.2 배포 흐름 (GitOps)

```
ECR에 새 이미지 push
    ↓
infra 레포 Helm values.yaml에서 image.tag 업데이트 (PR)
    ↓
ArgoCD가 변경 감지 → 자동 sync
    ↓
K8s Deployment rollout
```

### 5.3 인프라 CI (Terraform)

```
PR → terraform plan (plan 결과 PR 코멘트)
main merge → terraform apply (수동 승인 게이트)
```

State backend: S3 + DynamoDB locking

---

## 6. DR (재해 복구) 구성

### 6.1 S3 자동 복구

```
S3 객체 삭제
    ↓ EventBridge (DeleteObject)
Lambda (s3-auto-recovery)
    ↓ delete marker 제거
객체 자동 복원 (RTO 실측: 1.247초)
    ↓
CloudWatch Metric 발행 (S3AutoRecoveryRecoveredObjects)
```

### 6.2 RDS PITR

- 자동 백업 7일 보관
- `restore-db-instance-to-point-in-time`로 복원 인스턴스 생성
- RTO 목표: 15분, RPO 목표: 5분

### 6.3 GitOps 클러스터 복구

- ArgoCD self-heal: Deployment 삭제 시 1~2분 내 자동 복구
- App-of-Apps가 전체 클러스터 상태를 Git 기준으로 유지

### 6.4 Spot Interruption 대응

- Karpenter가 Spot 종료 알림 (SQS) 수신 → 새 노드 프로비저닝
- Pod terminationGracePeriodSeconds=120
- SQS visibility timeout + 멱등성으로 메시지 재처리 보장

---

## 7. 부하 테스트 구성

| 시나리오 | 도구 | 대상 | 핵심 결과 |
|----------|------|------|----------|
| Upload Burst | k6 | POST /api/v1/uploads | 48.55 req/s, p95=38.47ms, 0% error |
| Ranking Read (1차) | k6 | GET /api/v1/ranking | 377 req/s, p95=417.59ms, 0% error |
| Ranking Read (2차, HPA 적용 후) | k6 | GET /api/v1/ranking | 572 req/s, p95=35.65ms, 0% error, HPA 1→4 replicas |

---

## 8. 로컬 개발 환경 (docker-compose)

| 컨테이너 | 이미지 | 포트 | 역할 |
|----------|--------|------|------|
| postgres | postgres:16-alpine | 5432 | 로컬 DB |
| redis | redis:7-alpine | 6379 | 로컬 캐시 |
| localstack | localstack:3 | 4566 | S3, SQS, SES 에뮬레이션 |
| user-service | 로컬 빌드 | 8080 | |
| score-service | 로컬 빌드 | 8081 | |
| analysis-worker | 로컬 빌드 | 9090 | |
| notification-worker | 로컬 빌드 | 9091 | |

---

## 9. Terraform 모듈 구조

```
terraform/
├── envs/dev/
│   ├── main.tf          (모듈 호출: vpc → s3 → lambda → sqs → ecr → eks → karpenter → rds → elasticache → irsa)
│   ├── variables.tf
│   ├── outputs.tf
│   └── backend.tf       (S3 + DynamoDB state backend)
├── modules/
│   ├── vpc/             (VPC, Subnet, NAT, S3 Gateway Endpoint)
│   ├── eks/             (EKS Cluster, Bootstrap Node Group, OIDC Provider)
│   ├── karpenter/       (Controller IAM, Node IAM, SQS)
│   ├── rds/             (PostgreSQL, Subnet Group, Security Group, Secrets Manager)
│   ├── elasticache/     (Redis Replication Group, Subnet Group, Security Group)
│   ├── s3/              (Bucket, Versioning, Object Lock, SSE-KMS, EventBridge)
│   ├── sqs/             (2 Queues + 2 DLQs, Redrive Policy)
│   ├── ecr/             (4 Repositories)
│   ├── iam-irsa/        (Generic IRSA Role + Policy)
│   └── lambda-s3-recovery/ (Lambda Function, EventBridge Rule, CloudWatch Logs, IAM)
└── shared/
    └── locals.tf        (공통 변수: project, name_prefix, tags)
```

---

## 10. 리포지토리 구조 요약

### bodybuddy-app (Go monorepo)
```
cmd/                    4개 서비스 진입점
internal/               10개 내부 패키지
migrations/             DB 마이그레이션 (000001_init.up.sql)
deploy/helm/            4개 Helm chart
deploy/docker/          4개 Dockerfile
test/load/              k6 스크립트 2개
docs/adr/               ADR 7편
docs/architecture.md    아키텍처 설명
.github/workflows/      CI (ci.yaml, lint.yaml)
docker-compose.yml      로컬 개발
Makefile               빌드/테스트/배포 명령
```

### bodybuddy-infra (IaC + GitOps)
```
terraform/              Terraform 모듈 + 환경
argocd/                 App-of-Apps + 앱 매니페스트 + Karpenter config
lambda/                 S3 auto-recovery Lambda (Go)
runbooks/               3개 런북 (RDS PITR, S3 복구, GitOps 복구)
reports/                DR drill, 부하 테스트, RTO/RPO, 비용 분석, study docs
```
