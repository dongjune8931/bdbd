# 01. 로컬 MVP — docker-compose로 전체 시스템 띄우기

## 개요

01번 작업의 목표는 **AWS 비용 0원**으로 4개 마이크로서비스가 서로 통신하는 최소 형태를 로컬에서 구축하는 것이다. 여기서 "최소 형태"란 다음을 의미한다:

- 업로드 → 분석 → 점수 업데이트 → 알림 전송의 전체 데이터 흐름이 작동
- 각 서비스가 헬스체크 엔드포인트를 노출
- docker-compose 한 번으로 전체 시스템 기동

왜 로컬 MVP를 먼저 하는가? AWS에서 디버깅하면 시간이 지날수록 비용이 나간다. 로컬에서 비즈니스 로직과 서비스 간 연동을 완성한 뒤, AWS로 가져가면 인프라 문제에만 집중할 수 있다.

---

## 1. 프로젝트 저장소 구조

### 1.1 두 개의 저장소로 분리하는 이유

프로젝트는 두 개의 Git 저장소로 운영한다.

| 저장소 | 역할 |
|---|---|
| `bodybuddy-app` | 4개 Go 서비스 코드 + Helm chart + GitHub Actions |
| `bodybuddy-infra` | Terraform 모듈 + ArgoCD 매니페스트 + Lambda 코드 |

**분리 이유:**
- ArgoCD는 `bodybuddy-infra` 레포를 추적하며 Kubernetes 클러스터 상태를 동기화한다. 앱 코드와 인프라 코드가 같은 레포에 있으면 ArgoCD 감시 범위가 불필요하게 넓어진다.
- 앱 변경(코드 수정) PR과 인프라 변경(Terraform 수정) PR의 라이프사이클이 다르다. 분리하면 각 팀(혹은 역할)이 독립적으로 작업할 수 있다.
- Git 이력이 명확하게 분리된다. "이 커밋이 인프라 변경인가 앱 변경인가"를 즉시 알 수 있다.

### 1.2 bodybuddy-app 디렉토리 구조

```
bodybuddy-app/
├── cmd/
│   ├── user-service/main.go        # HTTP API 서버 진입점
│   ├── score-service/main.go       # HTTP API 서버 진입점
│   ├── analysis-worker/main.go     # SQS 컨슈머 진입점
│   └── notification-worker/main.go # SQS 컨슈머 진입점
├── internal/
│   ├── auth/          # JWT 발급/검증, 미들웨어
│   ├── config/        # envconfig 래퍼 (환경변수 → 구조체)
│   ├── db/            # pgx 커넥션 풀, 마이그레이션
│   ├── cache/         # Redis 래퍼
│   ├── queue/         # SQS 컨슈머/프로듀서, 멱등성
│   ├── observability/ # OpenTelemetry + Prometheus 셋업
│   ├── http/          # 공통 미들웨어, 헬스체크 핸들러
│   └── domain/        # 점수 환산, 랭킹 비즈니스 로직
├── deploy/
│   ├── helm/          # Helm chart (서비스별)
│   └── docker/        # Dockerfile
├── test/
│   ├── load/          # k6 부하 테스트 스크립트
│   └── integration/   # testcontainers-go 통합 테스트
├── go.mod
├── go.sum
└── Makefile
```

**📌 개념 설명: `internal/` 디렉토리**
Go에서 `internal/` 패키지는 같은 모듈 내에서만 임포트할 수 있다. 즉, 외부 모듈이 이 코드를 라이브러리로 가져다 쓸 수 없다. 마이크로서비스처럼 "공유 라이브러리"를 만들지 않는 경우에 적합한 구조다. 각 서비스는 필요한 `internal` 패키지만 참조한다.

**📌 개념 설명: `cmd/` 디렉토리**
Go 관례상 실행 가능한 바이너리의 진입점(`main.go`)은 `cmd/<프로그램명>/` 에 둔다. 비즈니스 로직은 `internal/`에, 설정 조합과 서버 기동은 `cmd/`에 위치한다. 이렇게 하면 여러 서비스가 같은 `internal` 코드를 공유하면서도 각자 독립적인 실행 파일을 만들 수 있다.

---

## 2. 4개 서비스 설계

### 2.1 서비스 분리 기준: 트래픽 특성과 SLA

서비스 경계를 **도메인**이 아닌 **트래픽 특성과 SLA**로 잘랐다. 이것이 면접에서 설명해야 할 핵심 결정이다.

| 서비스 | 트래픽 특성 | SLA | 배포 위치 |
|---|---|---|---|
| `user-service` | 동기 HTTP, 인증/프로필 CRUD | 응답시간 SLA 있음 | on-demand 노드 |
| `score-service` | 동기 HTTP, 읽기 heavy | 응답시간 SLA 있음 | on-demand 노드 |
| `analysis-worker` | 비동기 SQS 컨슈머 | 처리 지연 허용 | spot 노드 |
| `notification-worker` | 비동기 SQS 컨슈머 | 처리 지연 허용 | spot 노드 |

API는 사용자가 기다리므로 응답시간 SLA가 있다 → 언제든 AWS가 회수할 수 있는 spot 노드에 두면 안 된다.
워커는 메시지를 처리하면 되고 몇 초 지연은 허용된다 → spot 노드에 두어 비용을 70~90% 절감한다.

### 2.2 user-service

**책임:** JWT 인증, 사용자 프로필 CRUD, S3 presigned URL 발급

```
GET  /healthz          # Liveness probe
GET  /readyz           # Readiness probe (DB + Redis ping)
GET  /metrics          # Prometheus metrics

POST /api/v1/auth/register
POST /api/v1/auth/login
GET  /api/v1/users/me
POST /api/v1/uploads   # presigned URL 발급 + analysis-queue에 메시지 발행
```

presigned URL 발급 흐름:
1. 클라이언트가 `POST /api/v1/uploads` 요청
2. user-service가 S3에 `PutObject` presigned URL 생성 (만료 15분)
3. URL + 업로드 ID를 클라이언트에 반환
4. 클라이언트가 S3에 직접 PUT (user-service를 거치지 않음 → 서버 부하 없음)
5. S3 ObjectCreated 이벤트 → SQS analysis-queue로 전달

### 2.3 score-service

**책임:** 캐릭터 상태 관리, 랭킹 조회/갱신, 점수 히스토리

```
GET  /api/v1/scores/me           # 내 점수/캐릭터 조회
GET  /api/v1/scores/ranking      # 전체 랭킹 (Redis ZADD)
POST /api/v1/scores/internal     # analysis-worker에서 점수 업데이트 (내부 API)
```

랭킹은 Redis Sorted Set으로 관리한다. `ZADD ranking <score> <user_id>`, `ZREVRANK ranking <user_id>` 패턴으로 O(log N) 시간에 랭킹을 읽고 쓴다.

### 2.4 analysis-worker

**책임:** SQS 컨슈머, Mock OCR, 점수 환산, score-service 호출

```
루프:
  1. SQS analysis-queue에서 메시지 수신 (Long Polling)
  2. S3에서 이미지 메타데이터 조회
  3. 2~5초 sleep (Mock OCR 지연 시뮬레이션)
  4. 사용자 ID 시드 기반 의사난수 점수 생성
  5. score-service에 점수 업데이트 요청
  6. SQS notification-queue에 알림 메시지 발행
  7. SQS analysis-queue에서 메시지 삭제
```

**📌 개념 설명: 왜 Mock OCR인가?**
이 프로젝트의 목적은 인프라 학습이다. OCR(Optical Character Recognition)은 ML 영역이며, OCR 정확도를 높이는 것은 "AWS에서 MSA 운영하기"라는 학습 목표에 기여하지 않는다. 대신 Mock OCR로 **비동기 워크플로의 구조**(타임아웃, 재시도, 멱등성, DLQ)를 배운다. 실제 프로덕션으로 전환 시에는 Mock을 Textract 호출로 교체하면 된다.

### 2.5 notification-worker

**책임:** SQS notification-queue 컨슈머, SES 이메일 발송

```
루프:
  1. SQS notification-queue에서 메시지 수신
  2. SES로 이메일 발송 (또는 로그로 대체)
  3. SQS에서 메시지 삭제
```

---

## 3. Go 기술 스택

### 3.1 핵심 의존성

```go
// go.mod 주요 의존성
github.com/gin-gonic/gin              // HTTP 프레임워크
github.com/jackc/pgx/v5              // PostgreSQL 드라이버 (pgxpool 포함)
github.com/redis/go-redis/v9          // Redis 클라이언트
github.com/aws/aws-sdk-go-v2/...      // AWS SDK (v1 금지)
github.com/kelseyhightower/envconfig  // 환경변수 → 구조체 매핑
go.opentelemetry.io/otel/...          // OpenTelemetry 계측
github.com/prometheus/client_golang   // Prometheus 메트릭
```

**📌 개념 설명: aws-sdk-go-v2 vs v1**
v2는 context 지원, 모듈 단위 import(필요한 서비스만), 명시적 config 로딩이 특징이다. v1은 전역 state를 사용해 테스트가 어렵다. 이 프로젝트는 v2를 사용한다.

### 3.2 환경변수 관리 (envconfig)

```go
// internal/config/config.go
type Config struct {
    DBHost     string `envconfig:"DB_HOST" required:"true"`
    DBPort     int    `envconfig:"DB_PORT" default:"5432"`
    DBName     string `envconfig:"DB_NAME" required:"true"`
    DBUser     string `envconfig:"DB_USER" required:"true"`
    DBPassword string `envconfig:"DB_PASSWORD" required:"true"`
    DBSSLMode  string `envconfig:"DB_SSL_MODE" default:"disable"`
    RedisAddr  string `envconfig:"REDIS_ADDR" required:"true"`
    // ...
}

func Load() (*Config, error) {
    var cfg Config
    if err := envconfig.Process("", &cfg); err != nil {
        return nil, fmt.Errorf("loading config: %w", err)
    }
    return &cfg, nil
}
```

envconfig의 장점: 환경변수 이름, 타입 변환, 필수값 체크, 기본값을 코드로 문서화할 수 있다. `required:"true"`인 변수가 없으면 프로세스 시작 시 즉시 실패한다.

### 3.3 구조화 로깅 (slog)

```go
// cmd/user-service/main.go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger.With("service", "user-service"))

// 사용
slog.InfoContext(ctx, "user registered",
    "user_id", user.ID,
    "request_id", requestID,
)
```

**📌 개념 설명: 구조화 로깅이란?**
일반 로깅: `log.Printf("user %s registered", userID)` → 문자열로 파싱해야 함
구조화 로깅: JSON 형식으로 필드를 명시 → CloudWatch Logs, Grafana Loki에서 `user_id = "abc123"` 으로 필터링 가능

**절대 로깅 금지:**
- JWT 토큰 (보안)
- 비밀번호 해시 (보안)
- 신용카드 번호 등 PII (개인정보)

---

## 4. docker-compose 구성

### 4.1 전체 구성도

```yaml
# docker-compose.yaml
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: bodybuddy
      POSTGRES_USER: bodybuddy
      POSTGRES_PASSWORD: bodybuddy_local
    ports: ["5432:5432"]

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    ports: ["6379:6379"]

  localstack:
    image: localstack/localstack:3.0
    environment:
      SERVICES: s3,sqs
      DEFAULT_REGION: ap-northeast-2
    ports: ["4566:4566"]

  user-service:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile
      args:
        SERVICE: user-service
    environment:
      DB_HOST: postgres
      REDIS_ADDR: redis:6379
      AWS_ENDPOINT: http://localstack:4566
      # ...

  # score-service, analysis-worker, notification-worker 유사
```

### 4.2 LocalStack이란?

**📌 개념 설명: LocalStack**
AWS 서비스를 로컬에서 에뮬레이션하는 도구다. S3, SQS, SES, Lambda, DynamoDB 등을 실제 AWS 비용 없이 테스트할 수 있다. 엔드포인트는 `http://localhost:4566`이며, AWS SDK에 `endpoint override`를 설정하면 된다.

```go
// LocalStack 연결 (개발 환경)
customResolver := aws.EndpointResolverWithOptionsFunc(
    func(service, region string, options ...interface{}) (aws.Endpoint, error) {
        if os.Getenv("AWS_ENDPOINT") != "" {
            return aws.Endpoint{URL: os.Getenv("AWS_ENDPOINT")}, nil
        }
        return aws.Endpoint{}, &aws.EndpointNotFoundError{}
    },
)
```

프로덕션(EKS)에서는 이 `AWS_ENDPOINT` 변수를 설정하지 않으면 실제 AWS 엔드포인트로 자동 연결된다.

---

## 5. 멀티스테이지 Dockerfile

### 5.1 Dockerfile 구조

```dockerfile
# deploy/docker/Dockerfile
# --- 빌드 스테이지 ---
FROM golang:1.25-alpine AS builder

ARG SERVICE
WORKDIR /app

# 의존성 레이어 캐싱 (소스 변경해도 go.mod/sum이 같으면 캐시 히트)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /app/server \
    ./cmd/${SERVICE}/

# --- 런타임 스테이지 ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
```

### 5.2 왜 distroless인가?

**📌 개념 설명: Distroless 이미지**
Google이 만든 최소화 컨테이너 이미지다. shell(`/bin/sh`), 패키지 매니저, 일반 Linux 유틸리티가 없다.

| 이미지 | 크기 | 공격 표면 |
|---|---|---|
| `ubuntu:22.04` | ~80MB | 매우 큼 (shell, 패키지 등) |
| `alpine:3.20` | ~7MB | 작음 (sh, busybox 포함) |
| `distroless/static-debian12:nonroot` | ~2MB | 매우 작음 (glibc 없음, shell 없음) |

Go는 `CGO_ENABLED=0`으로 빌드하면 정적 바이너리가 되어 glibc 없이도 실행된다. 따라서 distroless와 궁합이 완벽하다.

**보안 측면:** shell이 없으면 `kubectl exec -it <pod> /bin/sh` 로 컨테이너에 직접 접근할 수 없다. 공격자가 컨테이너에 침입해도 할 수 있는 게 없다.

**비용/성능 측면:** 이미지가 작을수록 ECR 저장 비용, 이미지 pull 시간이 줄어든다.

### 5.3 빌드 최적화: `-ldflags="-s -w"`

- `-s`: 심볼 테이블 제거 (디버거용 정보 삭제)
- `-w`: DWARF 디버깅 정보 제거

바이너리 크기가 약 30% 줄어든다. 프로덕션에서 디버거를 붙일 일이 없으므로 항상 적용한다.

### 5.4 .dockerignore

```
# .dockerignore
.git/
.github/
docs/
test/load/
*.md
*.env
deploy/helm/
```

`.dockerignore` 없으면 `.git/` 디렉토리 전체가 빌드 컨텍스트에 포함된다. `.git/`은 크기가 커서 빌드가 느려진다. 또한 `.env` 파일이 실수로 이미지에 들어가면 비밀이 노출된다.

---

## 6. 데이터 흐름: 업로드 → 점수 반영

```
[클라이언트]
    │
    │ ① POST /api/v1/uploads (인바디 사진 업로드 요청)
    ▼
[user-service]
    │ ② S3 PutObject presigned URL 생성
    │ ③ analysis-queue에 메시지 발행 {user_id, upload_id, s3_key}
    │
    │ ④ presigned URL 반환
    ▼
[클라이언트]
    │ ⑤ S3에 직접 PUT (presigned URL 사용)
    ▼
[S3]
    │ (ObjectCreated 이벤트 → 로컬에서는 수동 SQS 메시지 발행으로 대체)
    ▼
[SQS: analysis-queue]
    │
    │ ⑥ analysis-worker가 Long Polling으로 수신
    ▼
[analysis-worker]
    │ ⑦ S3 메타데이터 조회 (정상 업로드 검증)
    │ ⑧ 2~5초 sleep (Mock OCR)
    │ ⑨ 점수 계산 (사용자 ID 기반 의사난수)
    │ ⑩ score-service에 POST /api/v1/scores/internal
    │ ⑪ notification-queue에 메시지 발행
    │ ⑫ analysis-queue에서 메시지 삭제
    ▼
[score-service]
    │ ⑬ RDS characters/score_history 업데이트
    │ ⑭ Redis ZADD ranking <score> <user_id>
    ▼
[SQS: notification-queue]
    │
    │ ⑮ notification-worker가 수신
    ▼
[notification-worker]
    │ ⑯ SES 이메일 발송 (또는 로그 출력)
    ▼
[사용자 이메일]
```

---

## 7. 헬스체크 3종

### 7.1 /healthz (Liveness Probe)

```go
// Liveness: 프로세스가 살아있는가?
r.GET("/healthz", func(c *gin.Context) {
    c.JSON(200, gin.H{"status": "ok"})
})
```

Kubernetes Liveness Probe: 이 엔드포인트가 실패하면 Pod을 재시작한다. 단순히 "프로세스가 살아있는가"만 체크한다. DB 연결 실패는 여기서 확인하지 않는다 (DB가 일시적으로 죽었다고 앱을 재시작하면 안 되기 때문).

### 7.2 /readyz (Readiness Probe)

```go
// Readiness: 트래픽을 받을 준비가 됐는가?
r.GET("/readyz", func(c *gin.Context) {
    ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
    defer cancel()

    // DB ping
    if err := db.Ping(ctx); err != nil {
        c.JSON(503, gin.H{"status": "not ready", "error": "db: " + err.Error()})
        return
    }

    // Redis ping
    if err := redisClient.Ping(ctx).Err(); err != nil {
        c.JSON(503, gin.H{"status": "not ready", "error": "redis: " + err.Error()})
        return
    }

    c.JSON(200, gin.H{"status": "ready"})
})
```

Kubernetes Readiness Probe: 이 엔드포인트가 실패하면 Pod을 Service 엔드포인트에서 제거한다 (트래픽을 보내지 않는다). Pod은 재시작되지 않는다. DB가 일시적으로 죽었을 때 "이 Pod에 트래픽 보내지 마"라고 알리는 용도다.

**📌 개념 설명: Liveness vs Readiness**
- Liveness 실패 → Pod 재시작 (데드락, 무한루프 등)
- Readiness 실패 → 트래픽 차단 (외부 의존성 장애 시 일시적 격리)

### 7.3 /metrics (Prometheus)

```go
// Prometheus metrics 엔드포인트
r.GET("/metrics", gin.WrapH(promhttp.Handler()))
```

Prometheus가 주기적으로 이 URL을 scrape하여 메트릭을 수집한다.

---

## 8. Graceful Shutdown 패턴

### 8.1 왜 필요한가?

Kubernetes에서 Pod이 종료될 때 순서는 다음과 같다:
1. `kubectl delete pod` 또는 스케일다운 → SIGTERM 신호 전송
2. `terminationGracePeriodSeconds` (기본 30초, 이 프로젝트는 120초) 동안 대기
3. 시간 초과 시 SIGKILL 강제 종료

SIGTERM을 받으면 앱이 알아서 종료해야 한다. 그냥 무시하면 진행 중인 요청이 끊기거나, SQS 메시지를 처리하다 중단되어 데이터가 꼬인다.

Spot 인스턴스는 AWS가 2분(120초) 전에 회수 통보(interruption notice)를 보낸다. 이 120초 안에 처리를 마무리하고 깨끗하게 종료해야 한다.

### 8.2 HTTP 서버 (Gin) Graceful Shutdown

```go
func main() {
    // 1. OS 신호 수신 컨텍스트 생성
    ctx, stop := signal.NotifyContext(
        context.Background(),
        syscall.SIGINT,
        syscall.SIGTERM,
    )
    defer stop()

    // 2. HTTP 서버 시작 (별도 goroutine)
    srv := &http.Server{Addr: ":8080", Handler: router}
    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            slog.Error("server error", "error", err)
            os.Exit(1)
        }
    }()

    // 3. 신호 대기
    <-ctx.Done()
    slog.Info("shutdown signal received")

    // 4. 110초 내에 진행 중인 요청 처리 완료
    shutdownCtx, cancel := context.WithTimeout(
        context.Background(),
        110*time.Second,
    )
    defer cancel()

    if err := srv.Shutdown(shutdownCtx); err != nil {
        slog.Error("graceful shutdown failed", "error", err)
    }
    slog.Info("server stopped")
}
```

**왜 110초인가?**
- Spot interruption notice: 120초 전 발생
- 110초 = 120초 - 10초 여유

이 10초 여유는 Kubernetes가 SIGTERM을 보내고 Pod을 Service 엔드포인트에서 제거하는 데 걸리는 시간이다.

### 8.3 SQS 워커 Graceful Shutdown

```go
func (w *Worker) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            slog.Info("worker stopping, waiting for in-flight messages")
            w.wg.Wait() // 처리 중인 메시지가 완료될 때까지 대기
            return
        default:
        }

        // Long polling (최대 20초 대기)
        output, err := w.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
            QueueUrl:            aws.String(w.queueURL),
            MaxNumberOfMessages: 10,
            WaitTimeSeconds:     20,
        })
        if err != nil {
            if ctx.Err() != nil {
                return // 컨텍스트 취소로 인한 에러는 무시
            }
            slog.Error("receive message error", "error", err)
            continue
        }

        for _, msg := range output.Messages {
            w.wg.Add(1)
            go func(m types.Message) {
                defer w.wg.Done()
                w.processMessage(ctx, m)
            }(msg)
        }
    }
}
```

---

## 9. SQS 컨슈머 패턴

### 9.1 Visibility Timeout

**📌 개념 설명: SQS Visibility Timeout**
SQS는 메시지를 "삭제"하기 전까지 메시지가 큐에 남아있다. 컨슈머가 메시지를 받으면 일정 시간(visibility timeout) 동안 다른 컨슈머에게 보이지 않게 된다. 이 시간 안에 처리를 마치고 메시지를 삭제해야 한다. 처리에 실패하면 timeout 후 메시지가 다시 보이게 되어 다른 컨슈머(또는 재시도)가 처리할 수 있다.

**계산 방법:**
```
visibility timeout = 평균 처리 시간 × 3
analysis-worker: Mock OCR 2~5초 + score-service 호출 1초 = 평균 약 5초
→ visibility timeout = 15초 (최소)
→ 여유 있게 30초로 설정
```

Long-running task의 경우 처리 중에 visibility를 연장해야 한다:
```go
// 처리 중 visibility 연장
go func() {
    ticker := time.NewTicker(20 * time.Second) // timeout의 2/3 주기
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            _, _ = w.sqs.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
                QueueUrl:          aws.String(w.queueURL),
                ReceiptHandle:     msg.ReceiptHandle,
                VisibilityTimeout: 30,
            })
        case <-done:
            return
        }
    }
}()
```

### 9.2 멱등성 (Idempotency)

**📌 개념 설명: 멱등성이란?**
같은 작업을 여러 번 실행해도 결과가 동일한 성질이다. SQS는 "at-least-once" delivery를 보장한다. 즉, 동일한 메시지가 두 번 전달될 수 있다. 따라서 컨슈머는 중복 처리를 방어해야 한다.

**구현 방법 1: Redis SETNX**
```go
func (w *Worker) isAlreadyProcessed(ctx context.Context, msgID string) bool {
    // NX: 키가 없을 때만 설정, EX: 24시간 후 자동 삭제
    result, err := w.redis.SetNX(ctx,
        "processed:"+msgID,
        "1",
        24*time.Hour,
    ).Result()
    if err != nil {
        return false // Redis 장애 시 처리 진행 (false positive보다 낫다)
    }
    return !result // result=false이면 이미 처리됨
}
```

**구현 방법 2: DB Unique Constraint**
```sql
-- score_history 테이블에 upload_id unique constraint
ALTER TABLE score_history ADD CONSTRAINT uq_upload_id UNIQUE (upload_id);
```

같은 upload_id로 INSERT 시 unique violation 에러 → 멱등성 보장.

### 9.3 DLQ (Dead Letter Queue)

**📌 개념 설명: DLQ**
처리에 계속 실패하는 메시지를 메인 큐에서 분리하여 별도 큐(DLQ)로 이동시키는 패턴이다. DLQ에 메시지가 쌓이면 알림을 보내 운영자가 확인할 수 있다.

설정:
- `maxReceiveCount: 3` → 3번 실패하면 DLQ로 이동
- DLQ에서 CloudWatch Alarm → Alertmanager → Slack 알림

---

## 10. DB 마이그레이션

### 10.1 테이블 설계

```sql
-- migrations/001_init.sql

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE characters (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    level       INT NOT NULL DEFAULT 1,
    total_score BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id)
);

CREATE TABLE score_history (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    upload_id   VARCHAR(255) UNIQUE NOT NULL, -- 멱등성 키
    score       INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inbody_uploads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    s3_key      VARCHAR(500) NOT NULL,
    status      VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending/processing/done/failed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
```

**upload_id UNIQUE constraint**: 멱등성 보장. analysis-worker가 같은 upload_id로 두 번 score를 저장하려 하면 DB가 거부한다.

### 10.2 마이그레이션 도구 (golang-migrate)

```bash
# 마이그레이션 실행
migrate -path migrations/ -database "postgres://..." up

# 특정 버전으로 롤백
migrate -path migrations/ -database "postgres://..." down 1
```

Docker Compose에서는 서비스가 뜨기 전에 마이그레이션이 먼저 실행되도록 `depends_on` + healthcheck 조합을 사용한다.

---

## 11. Mock OCR 설계

### 11.1 의사난수 점수 생성

```go
func generateMockScore(userID string) int {
    // 사용자 ID를 시드로 사용 → 같은 사용자는 항상 같은 점수 (재현성)
    h := fnv.New64a()
    h.Write([]byte(userID))
    seed := int64(h.Sum64())

    r := rand.New(rand.NewSource(seed + time.Now().UnixNano()))
    // 50~100 사이의 점수
    return 50 + r.Intn(51)
}
```

**재현성을 위해 사용자 ID를 시드에 포함**: 같은 사용자라면 비슷한 점수 범위를 받는다. 완전 랜덤이면 테스트가 어렵다.

**UnixNano 추가**: 같은 사용자가 여러 번 업로드해도 다른 점수를 받는다.

---

## 핵심 요약

- **저장소 2개 분리**: app(코드) vs infra(Terraform+ArgoCD). 라이프사이클이 다르고 ArgoCD가 infra만 추적하면 깔끔하다.
- **서비스 경계는 트래픽 특성**: API(동기, SLA) vs Worker(비동기, 지연 허용). 이 기준이 spot 노드 배치의 근거가 된다.
- **distroless + CGO_ENABLED=0**: 최소 이미지 크기(~10MB), shell 없는 보안, 정적 바이너리 실행.
- **Graceful Shutdown 110초**: Spot interruption 120초 notice에 대응. SIGTERM 수신 후 진행 중인 요청/메시지를 완료하고 종료.
- **SQS 멱등성**: at-least-once delivery 대응. Redis SETNX 또는 DB unique constraint로 중복 처리 방지.
- **Mock OCR**: 인프라 학습이 목적. 비동기 워크플로 패턴(타임아웃, 재시도, 멱등성)이 핵심이지 OCR 정확도가 아니다.
- **헬스체크 3종**: `/healthz`(liveness, 재시작 판단) / `/readyz`(readiness, 트래픽 차단) / `/metrics`(Prometheus scrape).
- **Docker 레이어 캐싱**: `go.mod/sum` 먼저 복사하고 `go mod download` → 소스 복사. 의존성이 바뀌지 않으면 캐시 히트.
