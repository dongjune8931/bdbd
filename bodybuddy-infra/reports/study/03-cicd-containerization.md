# 03. CI/CD + 컨테이너화 — 코드 Push부터 EKS 배포까지

## 개요

03번 작업의 목표는 **코드를 push하면 자동으로 빌드되어 ECR에 이미지가 올라가고, 그 이미지가 EKS에 배포되는** 파이프라인을 구축하는 것이다.

03번 작업 완료 상태:
- PR 머지 → GitHub Actions가 테스트/빌드/ECR push 자동 실행
- 4개 서비스가 EKS에서 Running 상태
- ALB를 통해 user-service API에 접근 가능
- IRSA로 AccessKey 없이 SQS/S3 접근

---

## 1. GitHub Actions 워크플로 위치 문제

### 1.1 문제 발생

**🔥 트러블슈팅 1: GitHub Actions 인식 실패**

**증상:**
```
bodybuddy-app/.github/workflows/ci.yaml 을 만들었는데
GitHub 레포지토리에서 Actions 탭에 워크플로가 나타나지 않음
```

**원인:**
GitHub Actions 워크플로 파일은 **레포지토리 루트의 `.github/workflows/`** 에 있어야 한다. `bodybuddy-app/`은 로컬 디렉토리 구조일 뿐, GitHub에 올라간 레포의 루트가 아니다.

이 프로젝트는 모노레포 구조다:
```
(git root)/
├── bodybuddy-app/       # Go 서비스 코드
│   └── .github/         # ← GitHub가 인식 못함
├── bodybuddy-infra/     # Terraform 코드
└── .github/             # ← 이 위치여야 함
    └── workflows/
        ├── ci.yaml
        └── terraform-plan.yaml
```

**해결:**
루트 `.github/workflows/`로 워크플로 파일 이동.

**배운 점:** GitHub Actions는 레포지토리 루트 기준으로 `.github/workflows/`를 탐색한다. 서브디렉토리의 `.github/`는 무시된다.

---

## 2. ci.yaml 구성

### 2.1 전체 워크플로 흐름

```yaml
# .github/workflows/ci.yaml
name: CI

on:
  push:
    branches: [main]
    paths:
      - 'bodybuddy-app/**'
  pull_request:
    branches: [main]
    paths:
      - 'bodybuddy-app/**'

jobs:
  lint-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
          working-directory: bodybuddy-app

      - name: Run tests
        working-directory: bodybuddy-app
        run: go test ./...

  build-push:
    needs: lint-test
    if: github.ref == 'refs/heads/main'  # main 브랜치일 때만
    runs-on: ubuntu-latest
    strategy:
      matrix:
        service: [user-service, score-service, analysis-worker, notification-worker]
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials (OIDC)
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::902371998304:role/github-actions-ecr-push
          aws-region: ap-northeast-2

      - name: Login to ECR
        uses: aws-actions/amazon-ecr-login@v2

      - name: Build and push
        working-directory: bodybuddy-app
        run: |
          SHA=$(git rev-parse --short HEAD)
          ECR_URI=902371998304.dkr.ecr.ap-northeast-2.amazonaws.com
          IMAGE=$ECR_URI/bodybuddy-${{ matrix.service }}

          docker build \
            --build-arg SERVICE=${{ matrix.service }} \
            -f deploy/docker/Dockerfile \
            -t $IMAGE:$SHA \
            -t $IMAGE:latest \
            .

          docker push $IMAGE:$SHA
          docker push $IMAGE:latest
```

### 2.2 matrix strategy로 4개 서비스 병렬 빌드

`strategy.matrix`를 사용하면 4개 서비스를 병렬로 빌드할 수 있다. 순차 실행 대비 빌드 시간이 약 4배 빠르다.

---

## 3. 왜 latest 대신 sha 태그를 쓰는가?

### 3.1 이미지 태그 전략

```bash
# 생성되는 이미지 태그
902371998304.dkr.ecr.ap-northeast-2.amazonaws.com/bodybuddy-user-service:a1b2c3d  # sha7
902371998304.dkr.ecr.ap-northeast-2.amazonaws.com/bodybuddy-user-service:latest
```

**latest만 쓰는 경우의 문제:**
- `latest`는 항상 최신 이미지를 가리킨다
- 이전 버전으로 롤백하려면? `latest`를 이전 이미지로 재태그 → 혼란
- "지금 배포된 게 어떤 코드냐?" → 알 수 없음
- `imagePullPolicy: Always`가 아닌 경우 노드에 캐시된 이전 `latest`가 사용될 수 있음

**sha 태그를 쓰는 이유:**
- **재현성**: `a1b2c3d` 태그가 붙은 이미지는 항상 같은 코드
- **롤백**: `helm upgrade --set image.tag=이전_sha` 로 즉시 롤백
- **감사**: 어떤 커밋이 프로덕션에 배포됐는지 Git 이력으로 추적 가능
- **ArgoCD 연동**: values.yaml에 sha를 명시하면 ArgoCD가 어떤 이미지를 써야 하는지 명확하게 알 수 있음

---

## 4. OIDC 기반 AWS 인증

### 4.1 왜 AccessKey를 쓰지 않는가?

**📌 개념 설명: GitHub Actions OIDC 인증**

기존 방식 (위험):
```yaml
env:
  AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
  AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
```
문제: GitHub Secrets에 장기 자격증명 보관 → 유출 시 영구적 권한 손상.

OIDC 방식 (권장):
```yaml
- uses: aws-actions/configure-aws-credentials@v4
  with:
    role-to-assume: arn:aws:iam::902371998304:role/github-actions-ecr-push
    aws-region: ap-northeast-2
```

동작 원리:
1. GitHub Actions가 JWT(OIDC 토큰) 발급
2. AWS IAM에서 GitHub의 OIDC 제공자를 신뢰 (Trust Policy에 명시)
3. `sts:AssumeRoleWithWebIdentity`로 IAM Role을 임시 assume
4. 임시 자격증명(15분~1시간)으로 ECR push

장점:
- 장기 자격증명 없음 → 유출 위험 없음
- IAM Role 정책으로 권한 세밀하게 제어
- 임시 자격증명은 자동 만료

```hcl
# Terraform으로 OIDC Provider 등록
resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

# GitHub Actions용 IAM Role
resource "aws_iam_role" "github_actions" {
  name = "github-actions-ecr-push"

  assume_role_policy = jsonencode({
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:sub" = "repo:username/bodybuddy-app:ref:refs/heads/main"
        }
      }
    }]
  })
}
```

---

## 5. golangci-lint 버전 호환성 문제

**🔥 트러블슈팅 2: golangci-lint Go 버전 불일치**

**증상:**
```
Error: can't load package: package .: build constraints exclude all Go files
golangci-lint v1.60.3 requires Go 1.21 or later, but found go1.25 in go.mod
```

**원인:**
`golangci-lint-action`의 특정 버전(`version: v1.60.3`)이 `go.mod`에 선언된 Go 1.25를 지원하지 않았다. golangci-lint는 Go 컴파일러를 내부적으로 사용하는데, 지원하는 최대 Go 버전이 있다.

**해결:**
```yaml
# 변경 전
- uses: golangci/golangci-lint-action@v6
  with:
    version: v1.60.3

# 변경 후
- uses: golangci/golangci-lint-action@v6
  with:
    version: latest  # 최신 버전으로 자동 사용
```

**배운 점:** 도구 버전을 고정하는 것은 재현성을 위해 좋지만, Go 버전 업그레이드 시 함께 업데이트해야 한다. `latest`는 편리하지만 예상치 못한 동작 변경이 생길 수 있다. 적절한 균형이 필요하다.

---

## 6. Helm Chart 구조

### 6.1 서비스별 Helm Chart

```
deploy/helm/user-service/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── _helpers.tpl         # 공통 helper 함수
    ├── deployment.yaml
    ├── service.yaml
    ├── ingress.yaml         # user-service, score-service만
    ├── serviceaccount.yaml  # IRSA 어노테이션
    ├── secret.yaml          # DB 비밀번호 등 (External Secrets에서 sync)
    ├── hpa.yaml             # Horizontal Pod Autoscaler
    ├── pdb.yaml             # Pod Disruption Budget
    └── servicemonitor.yaml  # Prometheus scrape 설정
```

### 6.2 deployment.yaml 핵심 설정

```yaml
# templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "user-service.fullname" . }}
  labels:
    {{- include "user-service.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "user-service.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "user-service.selectorLabels" . | nindent 8 }}
        app.kubernetes.io/version: {{ .Values.image.tag | quote }}
    spec:
      serviceAccountName: {{ include "user-service.serviceAccountName" . }}
      # Spot 인터럽션 대응
      terminationGracePeriodSeconds: 120
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - containerPort: 8080
          env:
            - name: DB_HOST
              valueFrom:
                secretKeyRef:
                  name: bodybuddy-db-secret
                  key: host
            - name: DB_SSL_MODE
              value: "require"
            - name: REDIS_TLS_ENABLED
              value: "true"
          # Readiness probe 필수
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 30
            periodSeconds: 30
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              memory: "256Mi"  # CPU limit 설정 안 함 (throttling 이슈)
```

**📌 개념 설명: CPU limit을 설정하지 않는 이유**
CPU throttling: 컨테이너가 CPU limit에 도달하면 더 많은 CPU가 물리적으로 가용하더라도 강제로 스로틀된다. 이로 인해 응답 지연이 급격히 증가한다 (특히 Go의 GC가 실행될 때).
- Memory limit: OOM(Out of Memory)은 즉각적인 Pod 재시작으로 이어지므로 반드시 설정
- CPU limit: 설정하지 않고 requests만으로 스케줄링 우선순위를 부여

### 6.3 values.yaml

```yaml
# deploy/helm/user-service/values.yaml
replicaCount: 1

image:
  repository: 902371998304.dkr.ecr.ap-northeast-2.amazonaws.com/bodybuddy-user-service
  tag: latest  # ArgoCD가 sha로 오버라이드
  pullPolicy: IfNotPresent

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::902371998304:role/bodybuddy-dev-user-service-irsa

ingress:
  enabled: true
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip

hpa:
  enabled: true
  minReplicas: 1
  maxReplicas: 5
  targetCPUUtilizationPercentage: 70
```

---

## 7. 트러블슈팅: RDS SSL 문제

**🔥 트러블슈팅 3: RDS SSL 강제 설정으로 인한 /readyz 실패**

**증상:**
```
GET /readyz → 503
{"status":"not ready","error":"db: FATAL: no pg_hba.conf entry for host '10.20.11.5', user 'bodybuddy', database 'bodybuddy', no encryption"}
```

**원인:**
RDS에서 `rds.force_ssl = 1` 파라미터 그룹을 적용했기 때문에 SSL 없는 연결을 거부한다. 앱의 DB 연결 문자열에 `sslmode=disable`이 하드코딩되어 있었다.

```go
// 기존 코드 (잘못됨)
connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
    cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName)
```

**해결:**
`sslmode`를 환경변수로 분리하고 기본값을 `require`로 설정.

```go
// 수정된 코드
connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
    cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)
```

```go
// config 구조체
type Config struct {
    DBSSLMode string `envconfig:"DB_SSL_MODE" default:"require"`
    // ...
}
```

로컬 docker-compose에서는 `DB_SSL_MODE=disable` (로컬 Postgres는 SSL 없음), EKS에서는 기본값 `require` 사용.

**배운 점:** 로컬과 프로덕션의 차이를 환경변수로 추상화하는 것이 중요하다. 하드코딩된 설정은 반드시 문제를 일으킨다.

---

## 8. 트러블슈팅: ElastiCache Redis TLS 문제

**🔥 트러블슈팅 4: ElastiCache TLS 설정 누락**

**증상:**
```
GET /readyz → 503
{"status":"not ready","error":"redis: redis ping: context deadline exceeded"}
```

**원인:**
ElastiCache를 `transit_encryption_enabled = true`로 생성했기 때문에 TLS 연결이 필수다. 하지만 앱의 Redis 클라이언트는 TLS 없이 연결을 시도했다.

```go
// 기존 코드 (TLS 없음)
rdb := redis.NewClient(&redis.Options{
    Addr: cfg.RedisAddr,
})
```

**해결:**
TLS 설정을 환경변수로 제어.

```go
// 수정된 코드
opts := &redis.Options{
    Addr: cfg.RedisAddr,
}

if cfg.RedisTLSEnabled {
    opts.TLSConfig = &tls.Config{
        InsecureSkipVerify: false,  // ElastiCache는 AWS 인증서 사용
        MinVersion:         tls.VersionTLS12,
    }
}

rdb := redis.NewClient(opts)
```

```go
// config 구조체
type Config struct {
    RedisAddr       string `envconfig:"REDIS_ADDR" required:"true"`
    RedisTLSEnabled bool   `envconfig:"REDIS_TLS_ENABLED" default:"false"`
}
```

Helm values.yaml에 환경변수 추가:
```yaml
env:
  - name: REDIS_TLS_ENABLED
    value: "true"
  - name: REDIS_ADDR
    value: "bodybuddy-dev-redis.xxxxx.cache.amazonaws.com:6379"
```

**배운 점:** ElastiCache를 TLS 모드로 생성하면 클라이언트도 반드시 TLS를 사용해야 한다. 로컬 Redis는 TLS 없이 연결되기 때문에 이 차이를 환경변수로 분리해야 한다.

---

## 9. IRSA 설정 상세

### 9.1 analysis-worker IRSA

```yaml
# deploy/helm/analysis-worker/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: analysis-worker
  namespace: bodybuddy
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::902371998304:role/bodybuddy-dev-analysis-worker-irsa
```

이 어노테이션이 있으면 Pod 내부의 AWS SDK가 자동으로 IRSA 토큰을 사용한다. `AWS_WEB_IDENTITY_TOKEN_FILE`과 `AWS_ROLE_ARN` 환경변수가 자동으로 주입된다.

```go
// 코드에서 별도 설정 없이도 IRSA 동작
cfg, err := config.LoadDefaultConfig(ctx)
// → SDK가 자동으로 Web Identity Token 방식으로 credentials 획득
```

### 9.2 IRSA 최소 권한 정책

```json
// analysis-worker IAM Policy
{
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:ChangeMessageVisibility",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:ap-northeast-2:902371998304:bodybuddy-dev-analysis-queue"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:HeadObject"],
      "Resource": "arn:aws:s3:::bodybuddy-dev-images/inbody/*"
    }
  ]
}
```

`analysis-worker`는 analysis-queue에서 메시지를 받고, S3에서 이미지를 읽을 수만 있다. 다른 큐나 S3 버킷, 또는 S3 쓰기 권한은 없다. 최소 권한 원칙(Least Privilege Principle).

---

## 10. ALB Controller 설치

### 10.1 AWS Load Balancer Controller란?

**📌 개념 설명: ALB Ingress Controller**
Kubernetes Ingress 리소스를 생성하면, ALB Controller가 자동으로 AWS ALB를 생성하고 설정한다. Pod IP를 ALB Target Group에 직접 등록한다 (IP Target 모드).

```
클라이언트 → [ALB] → [Kubernetes Service] → [Pod]
                ↑
         ALB Controller가 관리
```

### 10.2 설치 과정

```bash
# 1. IRSA: AmazonEKSLoadBalancerControllerRole 생성
eksctl create iamserviceaccount \
  --cluster=bodybuddy-dev \
  --namespace=kube-system \
  --name=aws-load-balancer-controller \
  --attach-policy-arn=arn:aws:iam::aws:policy/ElasticLoadBalancingFullAccess \
  --approve

# 2. Helm으로 ALB Controller 설치
helm repo add eks https://aws.github.io/eks-charts
helm install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=bodybuddy-dev \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller
```

### 10.3 Ingress 설정

```yaml
# deploy/helm/user-service/templates/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: user-service
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing    # 외부 접근
    alb.ingress.kubernetes.io/target-type: ip            # Pod IP 직접 등록
    alb.ingress.kubernetes.io/healthcheck-path: /healthz
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP": 80}]'
spec:
  rules:
    - http:
        paths:
          - path: /api/v1
            pathType: Prefix
            backend:
              service:
                name: user-service
                port:
                  number: 8080
```

---

## 11. kubectl vs Helm 배포 차이

### 11.1 kubectl apply (직접 배포)

```bash
kubectl apply -f deploy/k8s/user-service-deployment.yaml
```

문제:
- 버전 관리 어려움 (어떤 버전이 배포됐는지 추적 불가)
- 롤백 어려움 (`kubectl rollout undo`는 이미지 태그 기록이 없음)
- 환경별 설정 관리 어려움 (개발/스테이징/프로덕션)

### 11.2 Helm (권장)

```bash
helm upgrade --install user-service deploy/helm/user-service/ \
  --namespace bodybuddy \
  --set image.tag=a1b2c3d \
  --set replicaCount=2
```

장점:
- `helm history user-service`로 배포 이력 확인
- `helm rollback user-service 2`로 이전 버전 롤백
- `values.yaml`로 환경별 설정 분리
- ArgoCD와 통합 용이

---

## 12. 첫 배포 후 흐름 검증

### 12.1 검증 순서

```bash
# 1. Pod 상태 확인
kubectl get pods -n bodybuddy
# NAME                                    READY   STATUS    RESTARTS
# user-service-7d9b8c6f4-xxxxx           1/1     Running   0
# score-service-6f8b7d5c3-xxxxx          1/1     Running   0
# analysis-worker-5c7a9b8d2-xxxxx        1/1     Running   0
# notification-worker-4b6c8a7f1-xxxxx    1/1     Running   0

# 2. Readiness 확인
kubectl exec -it <user-service-pod> -n bodybuddy -- wget -O- localhost:8080/readyz

# 3. 실제 흐름 테스트
# 사용자 등록
curl -X POST http://<alb-dns>/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"test1234"}'

# 로그인 → JWT 획득
TOKEN=$(curl -X POST http://<alb-dns>/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"test1234"}' | jq -r '.token')

# 업로드 presigned URL 요청
curl -X POST http://<alb-dns>/api/v1/uploads \
  -H "Authorization: Bearer $TOKEN"
# → presigned URL 반환

# 5초 후 점수 확인 (analysis-worker가 처리)
curl http://<alb-dns>/api/v1/scores/me \
  -H "Authorization: Bearer $TOKEN"
# → {"score": 74, "level": 1, ...}
```

---

## 핵심 요약

- **GitHub Actions 워크플로 위치**: 레포지토리 루트의 `.github/workflows/`여야 함. 서브디렉토리는 인식 안 됨.
- **OIDC 인증**: 장기 AccessKey 없이 GitHub Actions → AWS IAM Role assume. Trust Policy에서 레포지토리와 브랜치를 명시하여 권한 제한.
- **sha 태그 전략**: `latest` 대신 git short sha로 태그. 재현성, 롤백, 감사 추적이 명확해짐.
- **golangci-lint 버전**: Go 버전 업그레이드 시 lint 도구도 함께 업데이트. `version: latest`로 임시 해결, 이후 버전 고정.
- **DB SSL / Redis TLS**: AWS 관리형 서비스는 암호화가 기본. 앱 클라이언트도 SSL/TLS를 사용해야 함. 환경변수로 on/off 제어해서 로컬과 프로덕션 차이를 추상화.
- **CPU limit 미설정**: Memory limit만 설정. CPU throttling으로 인한 응답 지연 방지.
- **IRSA 최소 권한**: 각 서비스가 꼭 필요한 AWS 리소스에만 접근하도록 별도 IAM Role. AccessKey 환경변수 절대 사용 안 함.
- **terminationGracePeriodSeconds: 120**: Spot 인터럽션 120초 notice 대응. Graceful shutdown이 이 시간 안에 완료되어야 함.
