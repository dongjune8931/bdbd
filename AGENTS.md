# 바디버디 — 클라우드 인프라 사이드 프로젝트 스펙

> 이 문서는 Codex(이하 "에이전트")가 프로젝트 작업을 수행할 때 참조하는 **Source of Truth**다.
> 결정이 바뀌면 이 문서를 먼저 갱신한 뒤 작업한다. 이 문서와 충돌하는 즉석 결정은 만들지 않는다.

---

## 0. 에이전트 운영 지침 (먼저 읽을 것)

### 0.1 원칙

1. **이 문서가 우선한다.** 본인의 일반 지식이 이 문서와 충돌하면 이 문서를 따른다.
2. **결정된 사항을 다시 논쟁하지 않는다.** § 12 "명시적 컷"에 들어간 것은 추가 제안 금지.
3. **추측 금지.** 사용자 환경(AWS 계정 ID, 도메인, 리전 등) 모를 때는 플레이스홀더(`<ACCOUNT_ID>`, `<DOMAIN>`)를 쓰고 사용자에게 보완 요청.
4. **비용을 의식한다.** § 14 비용 가이드 위반하는 리소스 생성하지 않는다. 의심되면 사용자에게 확인.
5. **작은 단위로 작업한다.** 한 번에 여러 모듈 동시 작성 X. Phase 단위로 끊는다.
6. **검증 가능한 산출물을 만든다.** "작동한다"는 추측이 아니라 명령어 또는 출력으로 증명한다.

### 0.2 작업 시작 시 체크리스트

- [ ] 어떤 Phase 작업인지 명확한가? (§ 11 참조)
- [ ] 해당 Phase의 acceptance criteria를 알고 있는가?
- [ ] 코드 작성이라면 § 5–10 컨벤션을 확인했는가?
- [ ] 비용 영향이 있는 리소스를 만드는가? § 14 확인.

### 0.3 작업 완료 시 산출물

- [ ] 코드 또는 설정 파일
- [ ] 실행/검증 방법 (명령어 또는 단계)
- [ ] 이 Phase의 acceptance criteria 충족 증거
- [ ] 다음 Phase로 넘어가기 전 주의사항 또는 미해결 이슈

---

## 1. 프로젝트 개요

### 1.1 도메인
운동을 처음 시작하는 사람을 위한 게임화 헬스 서비스. 인바디 결과지 사진을 업로드하면, 결과를 점수로 환산해 사용자의 캐릭터를 키운다. 다른 사용자들과 점수 기반 랭킹 경쟁이 있고, 알림으로 사용자 참여를 유도한다.

### 1.2 학습 목표 (이게 진짜 목표)
이 프로젝트의 **목표는 비즈니스 기능 완성이 아니라 인프라/클라우드 엔지니어 역량 어필**이다. 다음 영역에서 면접 talking point를 만든다:

- IaC (Terraform) 모듈 설계
- EKS 위의 MSA 운영
- CI/CD + GitOps (ArgoCD)
- 관측성 3축 (Metrics, Logs, Traces) + 비용 가시화
- Karpenter + Spot 기반 확장성/비용 최적화
- DR (RDS PITR, S3 자동 복구, GitOps 클러스터 복구)
- 보안 (IRSA, Secrets, 암호화)
- 비동기 패턴 (SQS, DLQ, 멱등성)

### 1.3 비목표 (시간 쓰지 말 것)
- OCR 정확도 (Mock 사용, § 2.4 참조)
- 비즈니스 로직 깊이 (점수 환산 공식은 단순 룰 기반)
- 프론트엔드 (필요시 최소한의 정적 페이지, 또는 API만 노출)
- 사용자 경험 디자인
- 운영 안정성 (테스트 배포 목적, 24/7 운영 안 함)

### 1.4 비용 제약
AWS 크레딧 $1000 보유. 6~7주 동안 $300~400 사용을 상정한다. 미사용 시 `terraform destroy`로 환경 내리는 것을 기본 운영 패턴으로 한다.

---

## 2. 아키텍처

### 2.1 서비스 분리 (4개)

| 서비스 | 책임 | 동기/비동기 | NodePool |
|---|---|---|---|
| `user-service` | JWT 인증, 프로필 CRUD, S3 presigned URL 발급 | Sync (HTTP) | critical (on-demand) |
| `score-service` | 캐릭터 상태, 랭킹 조회/갱신, 점수 히스토리 | Sync (HTTP, read-heavy) | critical (on-demand) |
| `analysis-worker` | SQS 컨슈머, Mock OCR + 점수 환산, score-service 호출 | Async | batch (spot) |
| `notification-worker` | SQS 컨슈머, SES 이메일/푸시 디스패치 | Async | batch (spot) |

**분리 근거**: 도메인이 아니라 **트래픽 특성과 SLA**로 분리. API는 응답 시간 SLA → on-demand. 워커는 처리 지연 허용 → spot.

### 2.2 데이터 흐름

```
[클라이언트]
    │
    │ ① POST /uploads (인바디 사진)
    ▼
[user-service] ─② presigned URL 발급─▶ [클라이언트]
                                            │
                                            │ ③ PUT 이미지 (직접 S3 업로드)
                                            ▼
                                         [S3 (이미지 버킷)]
                                            │
                                            │ ④ ObjectCreated 이벤트
                                            ▼
                                         [SQS: analysis-queue]
                                            │
                                            │ ⑤ 컨슈머 pull
                                            ▼
                                  [analysis-worker]
                                       │     │
                          ⑥ Mock OCR  │     │ ⑦ 점수 환산 결과
                          (2~5초 지연) │     ▼
                                       │  [score-service]
                                       │     │ ⑧ Redis ZADD (랭킹), RDS write (캐릭터)
                                       │     ▼
                                       │  [SQS: notification-queue]
                                       │     │ ⑨ 컨슈머 pull
                                       ▼     ▼
                            [notification-worker]
                                       │
                                       ▼
                                  [SES] → [사용자 이메일]
```

### 2.3 데이터 저장소
- **RDS PostgreSQL**: 사용자, 캐릭터 상태, 점수 히스토리, 인바디 메타데이터
- **ElastiCache Redis**: 랭킹 (Sorted Set), 캐릭터 상태 캐시
- **S3**: 인바디 원본 이미지

### 2.4 OCR 처리 정책
프로덕션 모드의 외부 OCR 호출 대신 **Mock 사용**. 워커는 다음과 같이 동작:

1. SQS에서 메시지 수신
2. S3에서 이미지 메타데이터 조회 (정상 동작 검증용)
3. 2~5초의 의도된 sleep (실제 OCR API 지연 시뮬레이션)
4. 룰 기반 또는 의사난수 점수 생성 (사용자 ID 기반 시드로 재현성 확보)
5. score-service에 결과 전달

**이렇게 한 이유**: 비동기 워크플로 자체가 본 프로젝트의 학습 포인트. OCR 정확도 튜닝은 ML 영역이며, 인프라 어필에 기여하지 않는다. 워커 인터페이스는 외부 OCR 호출 패턴(타임아웃, 재시도, 멱등성)을 그대로 따른다.

---

## 3. 기술 스택

### 3.1 언어/프레임워크 (모든 서비스 동일)
- **Go 1.25** (`golang:1.25-alpine` 빌드 베이스)
- HTTP: **Gin** (`github.com/gin-gonic/gin`)
- DB: **pgx/v5** (`github.com/jackc/pgx/v5`), 선택적으로 sqlc
- Cache: **go-redis/v9** (`github.com/redis/go-redis/v9`)
- AWS: **aws-sdk-go-v2** (`github.com/aws/aws-sdk-go-v2/...`) — v1 금지
- 로깅: **slog** (표준 라이브러리)
- 환경변수: **envconfig** (`github.com/kelseyhightower/envconfig`)
- 관측성: `go.opentelemetry.io/otel/...`, `github.com/prometheus/client_golang`

### 3.2 컨테이너
- 빌드: `golang:1.25-alpine`
- 런타임: `gcr.io/distroless/static-debian12:nonroot`
- 환경: `CGO_ENABLED=0`, `-ldflags="-s -w"`
- 목표 이미지 크기: 10~25MB
- 멀티스테이지 빌드 필수, `.dockerignore` 필수

### 3.3 인프라 (AWS, ap-northeast-2 단일 리전)
- 컴퓨팅: EKS 1.30+, Karpenter v1.x
- DB: RDS PostgreSQL 15+ (Multi-AZ)
- 캐시: ElastiCache Redis 7+ (단일 노드 OK, 데모용)
- 스토리지: S3 (Versioning + Object Lock Governance, SSE-KMS)
- 메시징: SQS Standard + DLQ
- 메일: SES
- 네트워크: VPC (public/private subnet), ALB, **S3 Gateway VPC Endpoint**, Route53
- 레지스트리: ECR
- 자동 복구: EventBridge + Lambda

### 3.4 K8s 도구
- 패키징: Helm 3
- GitOps: ArgoCD (App-of-Apps 패턴)
- 오토스케일링: Karpenter (Cluster Autoscaler 사용 안 함), HPA, 선택적 KEDA (SQS depth 기반 스케일)
- 인그레스: AWS Load Balancer Controller (ALB Ingress)
- Pod 권한: IRSA (`eks.amazonaws.com/role-arn` 어노테이션)
- Secrets: External Secrets Operator + AWS Secrets Manager

### 3.5 관측성
- Metrics: kube-prometheus-stack (Prometheus + Grafana + Alertmanager)
- Logs: CloudWatch Logs + Grafana CloudWatch 데이터소스 (Loki 사용 안 함)
- Traces: OpenTelemetry Collector + Tempo (또는 AWS X-Ray) — **critical path 1개만 계측**
  - critical path = upload → analysis-worker → score-service
- 비용: KubeCost

### 3.6 CI/CD
- 빌드: GitHub Actions
- 이미지 레지스트리: ECR (per-service 레포)
- 배포: ArgoCD가 ECR 이미지 태그 변경 감지 → 자동 sync
- IaC 적용: Terraform Cloud 없이 GitHub Actions에서 `terraform plan` (PR) / `terraform apply` (main, 수동 승인)

### 3.7 부하 테스트
- 도구: k6
- 시나리오: ① 업로드 부하 (analysis-worker 스케일 검증), ② 랭킹 조회 부하 (score-service 캐싱 검증)

---

## 4. 저장소 구조

### 4.1 레포지토리 분리
2개 레포로 운영:
1. **`bodybuddy-app`** — 4개 Go 서비스 + Helm chart + GitHub Actions (앱 CI)
2. **`bodybuddy-infra`** — Terraform 모듈 + ArgoCD App 매니페스트 + Lambda 코드

이렇게 분리한 이유: ArgoCD가 infra 레포에서 매니페스트 변경을 추적하면 깔끔. 앱 변경과 인프라 변경의 PR 라이프사이클이 다르다.

### 4.2 `bodybuddy-app` 구조 (monorepo)

```
bodybuddy-app/
├── cmd/
│   ├── user-service/main.go
│   ├── score-service/main.go
│   ├── analysis-worker/main.go
│   └── notification-worker/main.go
├── internal/
│   ├── auth/          # JWT, 미들웨어
│   ├── config/        # envconfig 래퍼
│   ├── db/            # pgx 풀, 마이그레이션
│   ├── cache/         # redis 래퍼
│   ├── queue/         # SQS 컨슈머/프로듀서, 멱등성 유틸
│   ├── observability/ # OTel/Prometheus 셋업
│   ├── http/          # 공통 미들웨어, 헬스체크
│   └── domain/        # 점수 환산, 랭킹 로직 등 비즈니스
├── deploy/
│   ├── helm/
│   │   ├── user-service/
│   │   ├── score-service/
│   │   ├── analysis-worker/
│   │   └── notification-worker/
│   └── docker/        # 공통 Dockerfile 템플릿
├── test/
│   ├── load/          # k6 스크립트
│   └── integration/   # 통합 테스트
├── docs/
│   ├── adr/           # ADR 마크다운
│   ├── architecture.md
│   └── runbook.md
├── .github/workflows/
│   ├── ci.yaml        # 테스트 + 빌드 + ECR push
│   └── lint.yaml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 4.3 `bodybuddy-infra` 구조

```
bodybuddy-infra/
├── terraform/
│   ├── envs/
│   │   └── dev/
│   │       ├── main.tf
│   │       ├── variables.tf
│   │       ├── outputs.tf
│   │       └── backend.tf       # S3 + DynamoDB
│   ├── modules/
│   │   ├── vpc/
│   │   ├── eks/
│   │   ├── karpenter/
│   │   ├── rds/
│   │   ├── elasticache/
│   │   ├── s3/
│   │   ├── sqs/
│   │   ├── ecr/
│   │   ├── iam-irsa/
│   │   ├── lambda-s3-recovery/
│   │   └── observability/       # kube-prometheus-stack, KubeCost, OTel collector
│   └── shared/
│       └── locals.tf
├── argocd/
│   ├── apps/                    # App-of-Apps 매니페스트
│   ├── app-of-apps.yaml
│   └── README.md
├── lambda/
│   └── s3-auto-recovery/
│       ├── main.go              # Go Lambda
│       ├── go.mod
│       └── deploy.sh
├── runbooks/
│   ├── rds-pitr-restore.md
│   ├── s3-mass-delete-recovery.md
│   └── gitops-cluster-recovery.md
├── reports/
│   ├── rto-rpo-matrix.md
│   ├── load-test-report.md
│   └── cost-analysis.md
└── README.md
```

---

## 5. 코딩 컨벤션 (Go)

### 5.1 일반
- `gofmt` + `goimports` + `golangci-lint` 통과 필수
- 패키지 이름 짧고 소문자, 약어 사용 자제
- 외부 노출은 `internal/` 밖에 두지 않는다 (라이브러리화하지 않음)
- 에러는 wrap (`fmt.Errorf("doing X: %w", err)`)
- `panic` 사용 금지 (init 외)
- `context.Context`를 첫 인자로 전달 (HTTP handler, DB call, AWS call 등)

### 5.2 로깅 (slog)
- 구조화 JSON 출력 (`slog.NewJSONHandler`)
- 표준 필드: `service`, `trace_id`, `span_id`, `user_id` (가능한 경우)
- 레벨: `Debug` / `Info` / `Warn` / `Error`
- 비밀 데이터 로깅 절대 금지 (JWT, 비밀번호 해시 등)

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger.With("service", "user-service"))
```

### 5.3 HTTP 서버 (Gin)
- 핸들러는 얇게. 비즈니스는 `internal/domain`으로.
- 미들웨어 순서: recovery → request id → otelhttp → logger → auth
- 헬스체크: `/healthz` (liveness), `/readyz` (readiness, DB ping 포함)
- `/metrics` Prometheus 엔드포인트 노출

### 5.4 Graceful Shutdown (필수)
모든 서비스에 다음 패턴 적용:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
// ... start server in goroutine ...
<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

**값 110초인 이유**: Spot interruption notice가 120초 전에 발생. 10초 여유 두고 in-flight 메시지 처리 + 신규 수신 중단.

### 5.5 SQS 컨슈머 패턴
- visibility timeout = 평균 처리 시간 × 3
- 멱등성: SQS 메시지 ID 또는 비즈니스 키를 dedup 키로 저장 (Redis SETNX 또는 DB unique constraint)
- DLQ: maxReceiveCount = 3
- 처리 중에는 visibility extend (long-running task)
- 종료 신호 받으면 신규 메시지 수신 중단, in-flight만 마무리

### 5.6 데이터베이스 (pgx)
- 트랜잭션은 `pgx.Tx` 명시적으로 전달
- 컨넥션 풀: `pgxpool.New`, 최대 컨넥션은 인스턴스 코어 수 × 2~4
- 마이그레이션: `golang-migrate` 또는 `goose`, 마이그레이션 파일은 `migrations/`
- 쿼리 타임아웃: `ctx`로 5초 기본

### 5.7 테스트
- 단위 테스트: 비즈니스 로직만 충분히. 80% 커버리지 목표 안 둠.
- 통합 테스트: testcontainers-go로 Postgres + Redis 컨테이너
- 라이브 AWS 호출 테스트는 옵트인 (CI 비용 절감)

---

## 6. IaC 컨벤션 (Terraform)

### 6.1 버전 고정
- Terraform: ~> 1.9
- AWS Provider: ~> 5.60
- 모든 모듈에 `required_providers` 블록

### 6.2 State 백엔드
- S3 버킷 (`bodybuddy-tfstate-<account_id>`)
- DynamoDB 테이블 (`bodybuddy-tflock`)
- 버킷 versioning + SSE-KMS 활성화
- **부트스트랩 단계에서 수동 생성**, 나머지는 코드화

### 6.3 모듈 작성 규칙
- 각 모듈은 `main.tf` / `variables.tf` / `outputs.tf` / `versions.tf` 4개 기본
- 변수에는 `description`, `type` 필수, `default`는 dev에서만
- 출력에는 `description` 필수
- 모듈 내부에서 `provider` 블록 정의 금지 (호출자에 위임)
- 검증된 외부 모듈 적극 활용 (terraform-aws-modules/vpc/aws, eks/aws 등)

### 6.4 명명 규칙
- 리소스 이름: `bodybuddy-<env>-<purpose>` (kebab-case)
- 태그: `Environment`, `Project=bodybuddy`, `ManagedBy=terraform`, `Owner=<github_user>` 필수

### 6.5 비용 의식 (dev 환경)
- RDS 인스턴스: `db.t4g.micro` (Multi-AZ는 DR 검증 시간만 ON)
- ElastiCache: `cache.t4g.micro`, 단일 노드
- NAT는 가능하면 단일 NAT instance, S3 Gateway Endpoint 필수
- EKS 노드는 Karpenter로 관리 (수동 ASG 금지)

### 6.6 적용 워크플로
1. 브랜치 → `terraform plan` (PR에 출력 코멘트)
2. PR 리뷰 → main 머지
3. main 머지 시 `terraform apply` (GitHub Actions, **수동 승인 단계** 포함)
4. `destroy`도 PR + 승인 절차 동일 (실수 방지)

---

## 7. K8s / Helm 컨벤션

### 7.1 네임스페이스
- `bodybuddy` (애플리케이션)
- `bodybuddy-system` (관측성, ArgoCD 등 시스템 컴포넌트)
- `karpenter` (Karpenter 자체)

### 7.2 라벨 (모든 리소스)
- `app.kubernetes.io/name: <service>`
- `app.kubernetes.io/part-of: bodybuddy`
- `app.kubernetes.io/component: api|worker`
- `app.kubernetes.io/version: <git-sha>`

### 7.3 Helm chart 구조 (서비스별 동일 템플릿)
```
helm/<service>/
├── Chart.yaml
├── values.yaml              # 기본값 (dev)
├── values.prod.yaml         # 향후 환경 추가시
└── templates/
    ├── deployment.yaml
    ├── service.yaml
    ├── ingress.yaml         # API 서비스만
    ├── hpa.yaml
    ├── pdb.yaml
    ├── serviceaccount.yaml  # IRSA 어노테이션
    ├── servicemonitor.yaml  # Prometheus scrape
    └── _helpers.tpl
```

### 7.4 리소스 설정
- `requests` 항상 설정 (CPU, Memory). HPA 동작과 노드 스케줄링에 필수.
- `limits`는 Memory만 설정 권장 (CPU limit은 throttling 이슈 주의)
- `readinessProbe` 필수 (`/readyz`), `livenessProbe`는 신중히
- `terminationGracePeriodSeconds: 120` (Spot interruption 대응)

### 7.5 PDB (Pod Disruption Budget)
모든 서비스에 PDB 설정. API는 `minAvailable: 1`, 워커는 `maxUnavailable: 1`.

### 7.6 HPA
- API: CPU 70% 임계
- 워커: SQS queue depth 기반 (KEDA `aws-sqs-queue` ScaledObject) 또는 CPU 백업

### 7.7 NodePool (Karpenter)

```yaml
# critical-pool: API용, on-demand 우선
nodeClassRef: default
requirements:
  - key: karpenter.sh/capacity-type
    operator: In
    values: ["on-demand"]
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["t3.medium", "t3.large", "m6i.large"]
limits:
  cpu: "16"
disruption:
  consolidationPolicy: WhenEmpty
  consolidateAfter: 30s

# batch-pool: 워커용, spot 우선
nodeClassRef: default
requirements:
  - key: karpenter.sh/capacity-type
    operator: In
    values: ["spot", "on-demand"]   # spot 부족 시 폴백
  - key: node.kubernetes.io/instance-type
    operator: In
    values: ["t3.medium", "t3.large", "m6i.large", "m6a.large"]
limits:
  cpu: "32"
disruption:
  consolidationPolicy: WhenEmptyOrUnderutilized
  consolidateAfter: 1m
```

워커 Pod에 `nodeSelector` 또는 `affinity`로 `batch-pool` 매칭.

---

## 8. CI/CD 컨벤션

### 8.1 GitHub Actions (app 레포)
- `ci.yaml`: PR/main 푸시 시 → lint → 단위 테스트 → 변경된 서비스만 Docker 빌드 → ECR push (main만)
- 이미지 태그: `<service>:<git-sha-short>` + `<service>:latest`
- Branch protection: main은 직접 푸시 금지, PR 필수

### 8.2 GitHub Actions (infra 레포)
- `terraform-plan.yaml`: PR 시 plan 실행, PR에 코멘트
- `terraform-apply.yaml`: main 머지 시 apply (수동 승인 게이트)

### 8.3 ArgoCD 배포 흐름
1. app 레포 main 머지 → GHA가 ECR에 새 이미지 push (`<sha>` 태그)
2. infra 레포의 Helm values 파일에서 이미지 태그 업데이트 (PR 또는 ArgoCD Image Updater)
3. ArgoCD가 infra 레포 변경 감지 → 자동 sync → K8s 클러스터에 배포

이미지 Updater 안 쓰고 PR 방식 권장 (감사 흔적 + 면접 talking point: "왜 자동 업데이트 안 했나" → "변경 가시성, 롤백 단순성").

---

## 9. 보안 컨벤션

### 9.1 권한 (IRSA)
- AWS API 호출하는 모든 Pod는 IRSA 사용. AccessKey 환경변수 금지.
- ServiceAccount 마다 별도 IAM Role, 최소 권한 정책.
- Role 명명: `bodybuddy-<env>-<service>-irsa`

예: `analysis-worker`는 SQS 특정 큐만 receive/delete, S3 특정 prefix만 read.

### 9.2 Secrets
- AWS Secrets Manager에 저장 (DB 비번, JWT 서명 키, 외부 API 키)
- External Secrets Operator로 K8s Secret으로 동기화
- 코드에 평문 비밀 절대 금지, Git pre-commit hook으로 gitleaks 검사

### 9.3 저장 데이터 암호화
- RDS: SSE 활성화 (KMS 매니지드 키)
- S3: SSE-KMS (인바디 이미지)
- EBS (Karpenter 노드 볼륨): 암호화 활성화

### 9.4 네트워크
- Public subnet에는 ALB만, 모든 워크로드는 private subnet
- Pod 간 통신은 NetworkPolicy 기본 deny + 명시적 allow

### 9.5 Pod Security
- Pod Security Standards: `restricted` (네임스페이스 라벨)
- 컨테이너: non-root, readOnlyRootFilesystem, drop ALL capabilities

---

## 10. 관측성 컨벤션

### 10.1 메트릭 명명
- Prometheus 표준: `<namespace>_<subsystem>_<name>_<unit>`
- 예: `bodybuddy_http_requests_total`, `bodybuddy_sqs_messages_processed_total`, `bodybuddy_score_update_duration_seconds`
- 라벨에 고카디널리티 데이터 금지 (user_id, request_id 등)

### 10.2 로그 포맷
- JSON, slog 핸들러 사용
- 표준 필드: `time`, `level`, `msg`, `service`, `trace_id`, `span_id`
- 비즈니스 식별자: `user_id` (해시 OK), `request_id`

### 10.3 트레이싱 범위
- **계측 대상**: user-service의 업로드 엔드포인트 → SQS → analysis-worker → score-service
- 그 외 엔드포인트는 metrics + logs만
- SDK: `otelhttp` 미들웨어 + `otelaws` 미들웨어로 자동
- Exporter: OTLP gRPC → OpenTelemetry Collector → Tempo

### 10.4 Grafana 대시보드 (만들 것)
1. **서비스별 RED 대시보드** (Rate, Errors, Duration) — Prometheus 데이터소스
2. **SQS 큐 상태 대시보드** — depth, 처리율, DLQ 메시지 수
3. **인프라 비용 대시보드** — KubeCost
4. **DR 시연 대시보드** — RTO/RPO 시각화 (참고: 다른 팀 발표물 패턴)

### 10.5 알림 규칙 (Alertmanager)
- 워커 SQS DLQ에 메시지 도착 → 즉시 알림
- API 5xx 비율 5분간 1% 이상 → 알림
- RDS 연결 풀 95% 이상 → 경고
- 알림 채널: Slack 또는 이메일 (개인 프로젝트라 둘 중 편한 거)

---

## 11. 작업 단위 (Phases)

> 시간 단위가 아닌 **논리적 단위**로 끊은 작업 그룹. 순서는 제안일 뿐 의존성 없는 단위는 병행 가능.

### Phase 1: 로컬 MVP (AWS 비용 0)
**목적**: 4개 서비스가 docker-compose로 통신하는 최소 형태 구축.

**작업**
- Go 모듈 초기화, 디렉토리 구조 (§ 4.2)
- 4개 서비스 골격 (헬스체크, 환경변수 로딩, 로깅, 메트릭 엔드포인트)
- docker-compose: postgres, redis, localstack(SQS, S3), 4개 서비스
- 각 서비스 Dockerfile (멀티스테이지, distroless)
- DB 마이그레이션 (사용자, 캐릭터, 점수 히스토리 테이블)
- 업로드 → SQS → 워커 → score 업데이트 → 알림 SQS → 알림 워커 흐름 작동

**Acceptance criteria**
- [ ] `docker-compose up` 한 번에 전체 기동
- [ ] curl로 업로드 → 5초 내 score 변경 확인 (DB 또는 API 조회)
- [ ] 4개 서비스 모두 `/healthz`, `/readyz`, `/metrics` 응답
- [ ] 컨테이너 이미지 25MB 이하

### Phase 2: IaC 기반 구축
**목적**: AWS에 EKS 클러스터 + 데이터 스토어가 떠 있는 상태.

**작업**
- Terraform state 백엔드 부트스트랩 (S3 + DynamoDB, 수동 1회)
- 모듈 작성: vpc, eks, karpenter(설치), rds, elasticache, s3, sqs, ecr, iam-irsa
- `envs/dev/main.tf`에서 모듈 호출
- `kubectl get nodes` 성공, RDS/Redis 접근 가능 확인

**Acceptance criteria**
- [ ] `terraform apply` 5분 이내 완료 (EKS 자체는 15~20분 걸림)
- [ ] EKS 노드(Karpenter 관리) 1개 이상 Ready
- [ ] RDS 마이그레이션 실행 가능 (bastion 또는 임시 Pod에서)
- [ ] S3 Gateway VPC Endpoint 라우팅 확인
- [ ] `terraform destroy`로 깨끗하게 내려감

### Phase 3: 컨테이너화 + 배포 파이프라인
**목적**: 코드 푸시 → 자동 빌드 → ECR push → 수동 배포까지 동작.

**작업**
- GitHub Actions ci.yaml (테스트, 빌드, ECR push)
- ECR 레포 4개 (Terraform으로 생성)
- 수동 `kubectl apply`로 첫 배포 (4개 서비스)
- ALB Ingress Controller 설치, 도메인 또는 ALB DNS로 접근
- IRSA 적용 (워커 Pod이 SQS/S3 접근)

**Acceptance criteria**
- [ ] PR → 빌드 통과, main → 이미지가 ECR에 푸시
- [ ] 4개 서비스가 EKS에서 Running, ALB로 user-service API 접근
- [ ] 워커 Pod이 IRSA로 SQS 메시지 수신 (AccessKey 없이)
- [ ] 실제 S3 → SQS → 워커 → score 업데이트 흐름 작동

### Phase 4: GitOps (ArgoCD)
**목적**: `kubectl apply` 직접 사용 중단, ArgoCD가 모든 배포를 관리.

**작업**
- ArgoCD 설치 (Helm)
- App-of-Apps 매니페스트 작성 (`bodybuddy-infra/argocd/`)
- 각 서비스의 Helm chart 작성 (§ 7.3 구조)
- ArgoCD가 infra 레포 트래킹, 자동 sync 활성화

**Acceptance criteria**
- [ ] ArgoCD UI에서 4개 App 모두 Synced & Healthy
- [ ] Helm values 변경 PR → 머지 → 5분 내 자동 반영
- [ ] **시연**: `kubectl delete deployment user-service` → ArgoCD가 1~2분 내 복구
- [ ] 시연 결과 measurement 기록 (Grafana 또는 캡처)

### Phase 5: 관측성 셋업
**목적**: 메트릭, 로그, 트레이싱, 비용 가시화 완성.

**작업**
- kube-prometheus-stack 설치 (Helm)
- 서비스마다 ServiceMonitor 작성
- Grafana 데이터소스: Prometheus, CloudWatch Logs
- OpenTelemetry Collector 설치, Tempo 설치
- 4개 서비스에 otelhttp / otelaws 미들웨어 추가 (critical path만 트레이스 export)
- KubeCost 설치
- Grafana 대시보드 4종 만들기 (§ 10.4)
- Alertmanager 규칙 작성 (§ 10.5)

**Acceptance criteria**
- [ ] 모든 서비스의 RED 메트릭 Grafana에서 확인
- [ ] 업로드 트레이스가 Tempo에서 4 service 구간 모두 보임
- [ ] CloudWatch Logs를 Grafana에서 조회 가능
- [ ] KubeCost가 네임스페이스별 비용 표시
- [ ] DLQ 메시지 도착 시 알림 트리거 (강제 실패 후 검증)

### Phase 6: Karpenter + Spot 본격화
**목적**: NodePool 2개로 운영, Spot interruption 대응 검증.

**작업**
- critical-pool, batch-pool NodePool 작성 (§ 7.7)
- 워커 Pod에 `nodeSelector` 적용
- `terminationGracePeriodSeconds: 120` 모든 Pod
- SQS visibility timeout + 멱등성 검증 (이미 § 5.5에 있어야 함)
- AWS Fault Injection Simulator로 Spot interruption 강제 발생
- KubeCost로 Spot 비율 및 절감액 측정

**Acceptance criteria**
- [ ] 워커 Pod이 batch-pool(Spot 노드)에서만 실행
- [ ] Spot interruption 강제 발생 → graceful shutdown 로그 확인 → SQS 메시지 다른 Pod에서 재처리 → 멱등성 동작 (중복 처리 없음)
- [ ] KubeCost에서 Spot 사용 절감액 캡처 (예: 월 $X 절감)
- [ ] 시연 결과를 `reports/`에 정리

### Phase 7: DR 셋업 및 드릴
**목적**: 백업 + 자동 복구 + 측정 가능한 RTO/RPO.

**작업**

**7-A. RDS PITR 드릴**
- automated backup 7일 보관 활성화 (Terraform)
- 임의 데이터 손실 시뮬레이션 (특정 테이블 truncate)
- PITR로 복원 → 새 인스턴스 생성 → 데이터 검증 → 시간 측정
- 런북 작성 (`runbooks/rds-pitr-restore.md`)

**7-B. S3 자동 복구**
- S3 Versioning + Object Lock (Governance 모드) 활성화
- Lambda 함수 작성 (Go): EventBridge 이벤트 수신 → 임계값(5분 내 N개 이상 삭제) 확인 → 버전 히스토리에서 복원 → SNS 알림
- EventBridge 규칙 (S3 DeleteObject 이벤트)
- CloudWatch 알람 (Lambda 실행 시간, 실패 횟수)
- 시연: 의도적 대량 삭제 → 자동 복원 확인

**7-C. RTO/RPO 매트릭스**
- `reports/rto-rpo-matrix.md` 작성:

| 장애 유형 | RTO 목표 | RTO 실측 | RPO 목표 | RPO 실측 | 자동화 |
|---|---|---|---|---|---|
| Pod 다운 | 30초 | - | 0 | 0 | 자동 (K8s) |
| Node 다운 (Spot interruption) | 2분 | - | 0 | 0 | 자동 (Karpenter + SQS retry) |
| Deployment 강제 삭제 | 5분 | - | 0 | 0 | 자동 (ArgoCD) |
| RDS 데이터 손실 | 15분 | - | 5분 | - | 반자동 (PITR) |
| S3 대량 삭제 | 2분 | - | 0 (Versioning) | 0 | 자동 (Lambda) |

**Acceptance criteria**
- [ ] RDS PITR 복원 1회 실시, 측정값 기록
- [ ] S3 자동 복구 시연 영상 또는 캡처
- [ ] RTO/RPO 매트릭스 표 전체 값 채워짐 (실측 포함)
- [ ] 3개 런북 완성

### Phase 8: 부하 테스트 + 튜닝
**목적**: 측정 가능한 성능 데이터, 최적화 스토리.

**작업**
- k6 스크립트 2종 작성:
  - `upload-burst.js`: 5분 동안 1k→10k RPS로 업로드 증가
  - `ranking-read.js`: 랭킹 조회 read-heavy 부하
- 1차 부하: 현 상태 측정
- 병목 식별 (예: score-service의 랭킹 조회 DB 직접 hit)
- 개선 적용 (예: Redis 캐싱 강화, RDS connection pool 조정)
- 2차 부하: 개선 효과 측정
- HPA + Karpenter가 실제로 노드/Pod 늘리는 것 캡처
- `reports/load-test-report.md` 작성

**Acceptance criteria**
- [ ] 1차/2차 부하 결과 비교표 (p50, p95, p99, error rate)
- [ ] HPA 동작 그래프 (Grafana 캡처)
- [ ] Karpenter 노드 확장 그래프
- [ ] 1개 이상의 의미 있는 개선 적용 + 효과 측정

### Phase 9: 문서화
**목적**: 면접 talking point가 될 산출물 완성.

**작업**
- ADR 5~7편 작성 (§ 15 후보 중)
- 아키텍처 다이어그램 (Excalidraw 또는 draw.io, SVG로 export)
- README.md 정리 (실행법, 아키텍처 요약, 트레이드오프, 배운 점)
- 블로그 3편 초안:
  - "EKS 위에 MSA 올린 회고 (4 서비스, GitOps, 관측성)"
  - "Karpenter + Spot으로 사이드 프로젝트 인프라 비용 X% 절감기"
  - "단일 리전에서 진지하게 DR 하기 (S3 자동 복구 + RDS PITR)"
- 데모 영상 (3~5분): 부하 거는 영상 + DR 시연

**Acceptance criteria**
- [ ] ADR 5편 이상, 각 ADR이 맥락/결정/트레이드오프/결과 구조
- [ ] README가 처음 보는 사람도 30초 안에 프로젝트 이해 가능
- [ ] 블로그 1편이라도 공개 (velog/Medium/Notion)
- [ ] 데모 영상 YouTube 또는 Notion에 업로드

---

## 12. 명시적 컷 (Out of Scope)

다음 항목은 **추가 제안하지 않는다.** 면접에서 "왜 안 했어요?"에 답할 준비를 한다.

| 컷 항목 | 답변 (요약) |
|---|---|
| 멀티 리전 | 비용·복잡도 대비 학습 가치 낮음, 단일 리전에서 의미 있는 DR로 대체 |
| Service Mesh (Istio/Linkerd) | 4 서비스 규모에 과잉, mTLS 필요 시 ALB TLS + IRSA로 충분 |
| Velero | GitOps + RDS PITR + S3 Versioning 3축으로 백업 완성, 추가 가치 약함 |
| Loki | 운영 부담 절감 위해 CloudWatch Logs로 충분, 트래픽 커지면 검토 |
| Trivy 이미지 스캔 | future work, README에 명시 |
| 카오스 엔지니어링 도구 (Chaos Mesh 등) | Spot interruption 자체가 실제 카오스 |
| AI 옵저버빌리티 (Phoenix 등) | AI 기능 없음, 도구만 까는 건 안티패턴 |
| Textract 등 실제 OCR | 인프라 학습에 집중, Mock으로 워크플로 시연 |
| Cluster Autoscaler | Karpenter로 단일화 |
| 자체 호스팅 Kafka | SQS로 비동기 패턴 학습 충분 |
| GraphQL | 비즈니스 가치 없음, REST로 충분 |
| 프론트엔드 | API만으로 시연 가능 (k6, curl) |

---

## 13. Deliverables 체크리스트

### 코드/인프라
- [ ] `bodybuddy-app` 레포 (4 Go 서비스)
- [ ] `bodybuddy-infra` 레포 (Terraform + ArgoCD + Lambda)
- [ ] Helm chart 4종
- [ ] GitHub Actions 워크플로 (app, infra)

### 문서
- [ ] 아키텍처 다이어그램 (SVG/PNG)
- [ ] ADR 5~7편
- [ ] README × 2 (app, infra)
- [ ] 런북 3편 (RDS PITR, S3 복구, GitOps 복구)

### 측정 결과 (reports/)
- [ ] RTO/RPO 매트릭스
- [ ] 부하 테스트 리포트 (1차/2차 비교)
- [ ] 비용 분석 리포트 (Before/After Spot + VPC Endpoint)
- [ ] Karpenter Spot 시연 증거 (로그/캡처)
- [ ] S3 자동 복구 시연 증거
- [ ] GitOps 복구 시연 증거

### 공개 자료
- [ ] 블로그 3편 (최소 1편 공개)
- [ ] 데모 영상 (3~5분)

---

## 14. 비용 가이드

### 14.1 예산
- 전체: $1000 크레딧 중 $300~400 사용 목표
- 작업 시간만 켜두는 운영 패턴 가정

### 14.2 월 추정 (작업 시간 기준)
| 항목 | 추정 |
|---|---|
| EKS 컨트롤플레인 | $30~40 (24/7 시 $72) |
| 워커 노드 (Spot 비중↑) | $40~60 |
| NAT Gateway + 트래픽 | $20~30 (S3 Endpoint 적용 후) |
| RDS db.t4g.micro Multi-AZ | $25~30 |
| ElastiCache cache.t4g.micro | $12~15 |
| ALB | $20 + 트래픽 |
| 기타 (S3, SQS, CloudWatch, Lambda) | $10~20 |
| **합계** | **$170~220 / 월** |

부하 테스트 주: +$50 추정.

### 14.3 비용 위험 신호 (에이전트 주의)
다음 작업을 제안받으면 사용자 확인 필수:
- NAT Gateway 추가 생성 (단일 NAT 권장)
- RDS Multi-AZ를 24/7 켜두기 (DR 드릴 외에는 단일 AZ로 충분)
- 추가 EKS 클러스터 생성
- 멀티 리전 리소스 (대원칙 위반, § 12 참조)
- t3.medium 이상의 인스턴스 타입 (Karpenter limits 위반)
- CloudWatch Logs 보관 기간 30일 초과
- KMS Customer Managed Key 다수 생성 (KMS는 키당 $1/월)

### 14.4 비용 절감 도구 (이미 채택)
- Spot for workers
- S3 Gateway VPC Endpoint (NAT 우회)
- KubeCost로 가시화
- `terraform destroy` 운영 패턴

---

## 15. ADR 주제 목록 (작성 후보)

5~7편을 깊이 있게 작성. 나머지는 README에 한두 줄로.

1. **ADR-001**: EKS 채택 (vs ECS Fargate)
2. **ADR-002**: MSA 경계를 트래픽 특성으로 분리 (vs 도메인 기준)
3. **ADR-003**: SQS 채택 (vs self-hosted Kafka, MSK)
4. **ADR-004**: Karpenter 채택 (vs Cluster Autoscaler)
5. **ADR-005**: OCR는 Mock으로 (vs Textract)
6. **ADR-006**: CloudWatch Logs 채택 (vs Loki)
7. **ADR-007**: Distroless 런타임 (vs Alpine)
8. **ADR-008**: GitOps (ArgoCD) 채택 (vs `kubectl apply`)
9. **ADR-009**: 단일 리전 + S3 자동 복구 (vs 멀티 리전 Warm Standby)
10. **ADR-010**: VPC Gateway Endpoint 도입 근거
11. **ADR-011**: OpenTelemetry 부분 계측 (critical path only)
12. **ADR-012**: Velero 제외 결정

### ADR 템플릿
```markdown
# ADR-XXX: <결정 제목>

## 상태
Accepted / Proposed / Deprecated

## 맥락 (Context)
- 어떤 문제를 풀고 있나
- 어떤 제약이 있나

## 결정 (Decision)
- 무엇을 골랐나 (구체적)

## 검토한 대안 (Alternatives Considered)
- 대안 1, 대안 2 — 왜 채택하지 않았나

## 트레이드오프 (Consequences)
- 좋은 점
- 나쁜 점
- 위험

## 후속 작업
- 이 결정이 도입한 추가 작업
```

---

## 16. 위험 요소 및 완화

| 위험 | 영향 | 완화 |
|---|---|---|
| EKS 첫 셋업 시간 (IRSA, 네트워킹) | Phase 2 지연 | terraform-aws-modules/eks 활용, 학습 시간 확보 |
| Karpenter 정책 튜닝 잡일 | Phase 6 지연 | 보수적 시작 → 부하 테스트 후 튜닝 |
| Spot 자연 발생 안 함 | 시연 부족 | AWS FIS로 강제 트리거 |
| ArgoCD App-of-Apps 패턴 학습 | Phase 4 지연 | 검증된 예시 활용, 단순 구조 우선 |
| Lambda + EventBridge 디버깅 | Phase 7 지연 | LocalStack 또는 SAM으로 사전 검증 |
| 부하 테스트에서 트래픽 비용 폭증 | 크레딧 소진 | VPC Endpoint 먼저 적용, k6 시나리오 짧게 |
| Terraform state 충돌 | 인프라 깨짐 | S3 + DynamoDB locking 처음부터 |

---

## 17. 자주 묻는 질문 (에이전트 셀프 체크)

**Q. 사용자가 "이거 빨리 하려면 X 매니지드 서비스 쓰자"고 하면?**
A. § 12 컷 항목인지 먼저 확인. 비용 영향(§ 14.3) 확인. 필요시 트레이드오프 정리해서 사용자에게 결정 요청.

**Q. 코드 작성을 어디까지 해야 하나?**
A. 본 프로젝트는 사용자의 포트폴리오. 핵심 로직(graceful shutdown, SQS 멱등성, Redis 랭킹, Karpenter NodePool, Terraform 모듈 경계)은 사용자가 직접 작성하도록 유도하고, 에이전트는 패턴/예시/리뷰로 보조. 보일러플레이트와 잡일은 적극 작성 OK.

**Q. Phase를 건너뛰고 진행해도 되나?**
A. 안 됨. Phase 1(로컬)을 거치지 않고 Phase 2(AWS)부터 시작하면 디버깅이 AWS 비용으로 직결됨. 의존성 있는 Phase는 순서 지킨다.

**Q. 새 도구/서비스 도입 제안을 어떻게 판단하나?**
A. (1) § 1.2 학습 목표에 기여하나? (2) § 12 컷 항목인가? (3) § 14 비용 가이드 위반인가? (4) 면접에서 "왜 도입?" 답 가능한가? 4개 다 통과해야 제안.

**Q. 사용자가 명확하지 않은 지시를 하면?**
A. 추측하지 말고 명확화 질문. 단, 본 문서에 정의된 컨벤션은 재확인하지 않는다.

---

## 변경 이력

| 날짜 | 변경자 | 내용 |
|---|---|---|
| YYYY-MM-DD | initial | 초기 작성 |
