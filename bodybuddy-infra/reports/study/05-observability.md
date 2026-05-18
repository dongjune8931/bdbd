# 05. 관측성 셋업 — Metrics, Logs, Traces 3축 구축

## 개요

관측성(Observability)은 시스템이 내부에서 무슨 일이 일어나고 있는지를 외부에서 알 수 있는 능력이다. 단순히 "서버가 살아있는가"(모니터링)를 넘어, "왜 느린가", "어디서 에러가 나는가", "비용이 얼마인가"를 답할 수 있어야 한다.

05번 작업 완료 상태:
- Prometheus가 4개 서비스의 메트릭 수집
- Grafana에서 서비스별 RED 대시보드 확인
- 업로드 트레이스가 Tempo에서 4개 서비스 구간 모두 보임
- CloudWatch Logs를 Grafana에서 조회 가능
- KubeCost로 네임스페이스별 비용 확인
- DLQ 메시지 도착 시 Alertmanager 알림 발생

---

## 1. 관측성 3축

### 1.1 Metrics (메트릭)

**📌 개념 설명: Metrics**
숫자로 표현되는 시계열 데이터. CPU 사용률, HTTP 요청 수, 에러 율 등.

```
장점: 집계가 쉬움, 알림 설정 용이, 오래 보관해도 비용 낮음
단점: "왜" 느린지는 알 수 없음 (숫자만 있음)

이 프로젝트: Prometheus + Grafana
```

RED 메트릭 패턴:
- **Rate**: 초당 요청 수
- **Errors**: 에러 비율 (5xx 비율)
- **Duration**: 응답 시간 (p50, p95, p99)

### 1.2 Logs (로그)

**📌 개념 설명: Logs**
이벤트 기록. "사용자 A가 2024-01-15 10:23:45에 업로드를 시작했다" 같은 상세 정보.

```
장점: 상세 컨텍스트, 디버깅에 강력
단점: 양이 많으면 비용/저장소 문제, 검색이 느릴 수 있음

이 프로젝트: CloudWatch Logs (Loki 제외)
이유: Loki는 운영 부담이 있음. CloudWatch는 EKS와 자연스럽게 통합됨.
```

### 1.3 Traces (트레이스)

**📌 개념 설명: Traces**
단일 요청이 여러 서비스를 거치는 전체 경로 추적. "이 요청이 user-service → SQS → analysis-worker → score-service 순으로 얼마나 걸렸나"를 한눈에 볼 수 있음.

```
장점: 분산 시스템에서 병목 찾기에 최적
단점: 모든 요청에 계측하면 오버헤드 큼

이 프로젝트: OpenTelemetry Collector + Grafana Tempo
계측 대상: critical path만 (upload → analysis-worker → score-service)
```

---

## 2. kube-prometheus-stack 설치

### 2.1 kube-prometheus-stack이란?

하나의 Helm chart에 다음이 모두 포함된다:
- **Prometheus**: 메트릭 수집기
- **Alertmanager**: 알림 발송 (Slack, 이메일)
- **Grafana**: 시각화 대시보드
- **kube-state-metrics**: Kubernetes 리소스 메트릭 (Pod 상태, Deployment 상태 등)
- **node-exporter**: 노드 수준 메트릭 (CPU, 메모리, 디스크)
- **Prometheus Operator**: CRD로 Prometheus 설정 관리

### 2.2 트러블슈팅: 노드 용량 부족

**🔥 트러블슈팅 1: "0/1 nodes are available: 1 Too many pods"**

**증상:**
```
kube-prometheus-stack 설치 후 Pod들이 Pending 상태
kubectl describe pod prometheus-xxx → "Too many pods"
```

**원인:**
기본 EKS 노드 타입(`t3.medium`)의 최대 Pod 수는 17개다. 기존 시스템 Pod들(kube-dns, kube-proxy, Karpenter, ArgoCD 컴포넌트 등)이 이미 노드를 채우고 있었다.

**해결 1: CoreDNS replicas 줄이기**
```bash
kubectl scale deployment coredns -n kube-system --replicas=1
# 기본 2 → 1로. 데모 환경에서는 단일 CoreDNS로 충분
```

**해결 2: ArgoCD 컴포넌트 스케일다운**
```bash
kubectl scale deployment argocd-repo-server -n argocd --replicas=1
kubectl scale deployment argocd-server -n argocd --replicas=1
```

**해결 3: 근본 해결 - Karpenter로 노드 확장**
kube-prometheus-stack의 resource requests를 설정하여 Karpenter가 새 노드를 프로비저닝하도록 유도.

```yaml
# kube-prometheus-stack values
prometheus:
  prometheusSpec:
    resources:
      requests:
        cpu: "200m"
        memory: "512Mi"
```

**배운 점:** 노드당 Pod 수 제한은 VPC CNI의 ENI당 IP 수에서 온다. `t3.medium`은 3 ENI × 6 IP = 17 Pod. 이 제한을 늘리려면 VPC CNI prefix delegation을 사용하거나 더 큰 인스턴스 타입을 쓴다.

### 2.3 트러블슈팅: CRD 크기 초과

**🔥 트러블슈팅 2: CRD apply 시 "too large" 에러**

**증상:**
```
Error: CustomResourceDefinition "prometheuses.monitoring.coreos.com" is invalid:
metadata.annotations: Too long: must have at most 262144 bytes
```

**원인:**
kube-prometheus-stack의 CRD(CustomResourceDefinition)가 매우 크다. 기본 `kubectl apply`는 마지막 적용 설정을 annotation으로 저장하는데, 이 크기가 Kubernetes의 annotation 크기 제한(256KB)을 초과한다.

**해결:**
```bash
# --server-side: annotation 저장 방식 대신 서버 측 적용 (크기 제한 없음)
kubectl apply --server-side -f crds/
```

또는 Helm 설치 시:
```bash
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  --set "prometheusOperator.admissionWebhooks.certManager.enabled=false" \
  --timeout 10m \
  -n bodybuddy-system
```

### 2.4 트러블슈팅: Admission Webhook TLS 깨짐

**🔥 트러블슈팅 3: Prometheus Operator Webhook TLS 인증서 오류**

**증상:**
```
Error: Internal error occurred: failed calling webhook "vprometheusrule.monitoring.coreos.com":
x509: certificate signed by unknown authority
```

**원인:**
`prometheusOperator.admissionWebhooks`는 Prometheus Rule을 validate하기 위한 webhook이다. 이 webhook이 자체 서명 인증서를 사용하는데, 인증서가 만료되거나 재설치 후 새 인증서가 생성되면서 Kubernetes가 신뢰하지 않는 상황.

**해결:**
```yaml
# kube-prometheus-stack values.yaml
prometheusOperator:
  admissionWebhooks:
    enabled: false       # webhook 비활성화 (validate 없이 적용)
    # 또는
    patch:
      enabled: true      # 자체 서명 인증서 자동 갱신
```

또는 cert-manager를 사용하는 경우:
```yaml
prometheusOperator:
  admissionWebhooks:
    certManager:
      enabled: true
```

---

## 3. ServiceMonitor

### 3.1 ServiceMonitor란?

**📌 개념 설명: ServiceMonitor**
Prometheus가 어떤 서비스의 메트릭을 scrape할지 선언적으로 정의하는 CRD다. Prometheus 설정 파일(`prometheus.yml`)을 직접 편집하는 대신, Kubernetes 리소스로 선언한다.

```yaml
# deploy/helm/user-service/templates/servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: user-service
  namespace: bodybuddy
  labels:
    release: kube-prometheus-stack  # 이 라벨이 있어야 Prometheus가 인식
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: user-service
  endpoints:
    - port: http
      path: /metrics
      interval: 30s       # 30초마다 scrape
      scrapeTimeout: 10s
```

Prometheus는 `serviceMonitorSelector`로 ServiceMonitor를 찾는다:
```yaml
# kube-prometheus-stack values
prometheus:
  prometheusSpec:
    serviceMonitorSelector:
      matchLabels:
        release: kube-prometheus-stack
```

### 3.2 커스텀 메트릭 정의

```go
// internal/observability/metrics.go

var (
    httpRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "bodybuddy_http_requests_total",
            Help: "Total number of HTTP requests",
        },
        []string{"method", "path", "status_code"},
    )

    httpRequestDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "bodybuddy_http_request_duration_seconds",
            Help:    "HTTP request duration",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "path"},
    )

    sqsMessagesProcessed = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "bodybuddy_sqs_messages_processed_total",
            Help: "Total number of SQS messages processed",
        },
        []string{"queue", "status"}, // status: success/failure
    )
)
```

**📌 개념 설명: 고카디널리티 라벨 주의**
라벨에 user_id나 request_id 같은 값을 쓰면 안 된다. 사용자가 100만 명이면 라벨 조합이 100만 개 → Prometheus 메모리 폭발.

```go
// 나쁜 예
httpRequestsTotal.WithLabelValues("GET", "/api/v1/users/me", "200", userID) // ❌

// 좋은 예
httpRequestsTotal.WithLabelValues("GET", "/api/v1/users/:id", "200") // ✅
```

---

## 4. Grafana 대시보드

### 4.1 REST API로 대시보드 생성

ArgoCD로 관리되는 Grafana에 대시보드를 UI에서 직접 만들면 다음 재시작 시 사라질 수 있다. 코드로 관리하는 방법:

**방법 1: ConfigMap으로 대시보드 JSON 관리**
```yaml
# bodybuddy-system 네임스페이스에 ConfigMap 생성
apiVersion: v1
kind: ConfigMap
metadata:
  name: bodybuddy-dashboard
  namespace: bodybuddy-system
  labels:
    grafana_dashboard: "1"  # Grafana가 이 ConfigMap을 자동으로 불러옴
data:
  bodybuddy-overview.json: |
    {
      "title": "BodyBuddy Services Overview",
      ...
    }
```

**방법 2: Grafana REST API**
```bash
# 대시보드 JSON을 API로 push
curl -X POST http://admin:password@grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @dashboard.json
```

### 4.2 BodyBuddy Services Overview 대시보드

```json
{
  "title": "BodyBuddy Services Overview",
  "panels": [
    {
      "title": "HTTP Request Rate",
      "targets": [{
        "expr": "sum(rate(bodybuddy_http_requests_total[5m])) by (service)"
      }]
    },
    {
      "title": "HTTP Error Rate (5xx)",
      "targets": [{
        "expr": "sum(rate(bodybuddy_http_requests_total{status_code=~\"5..\"}[5m])) by (service) / sum(rate(bodybuddy_http_requests_total[5m])) by (service)"
      }]
    },
    {
      "title": "HTTP p95 Latency",
      "targets": [{
        "expr": "histogram_quantile(0.95, sum(rate(bodybuddy_http_request_duration_seconds_bucket[5m])) by (le, service))"
      }]
    },
    {
      "title": "Go Goroutines",
      "targets": [{
        "expr": "go_goroutines{namespace=\"bodybuddy\"}"
      }]
    },
    {
      "title": "SQS Messages Processed",
      "targets": [{
        "expr": "sum(rate(bodybuddy_sqs_messages_processed_total[5m])) by (queue, status)"
      }]
    }
  ]
}
```

---

## 5. PrometheusRule (알림 규칙)

```yaml
# deploy/helm/user-service/templates/prometheusrule.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: bodybuddy-alerts
  namespace: bodybuddy
  labels:
    release: kube-prometheus-stack
spec:
  groups:
    - name: bodybuddy.rules
      rules:
        - alert: PodCrashLooping
          expr: rate(kube_pod_container_status_restarts_total{namespace="bodybuddy"}[15m]) > 0
          for: 5m
          labels:
            severity: critical
          annotations:
            summary: "Pod {{ $labels.pod }} is crash looping"

        - alert: HighMemoryUsage
          expr: |
            container_memory_usage_bytes{namespace="bodybuddy"}
            / container_spec_memory_limit_bytes{namespace="bodybuddy"} > 0.9
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "Memory usage > 90% for {{ $labels.container }}"

        - alert: SQSDLQMessageCount
          expr: aws_sqs_approximate_number_of_messages_visible{queue_name=~".*dlq.*"} > 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "DLQ has messages: {{ $value }} messages in {{ $labels.queue_name }}"

        - alert: HighHTTPErrorRate
          expr: |
            sum(rate(bodybuddy_http_requests_total{status_code=~"5.."}[5m])) by (service)
            / sum(rate(bodybuddy_http_requests_total[5m])) by (service) > 0.01
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "HTTP 5xx error rate > 1% for {{ $labels.service }}"

        - alert: ServiceDown
          expr: up{namespace="bodybuddy"} == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "Service {{ $labels.service }} is down"
```

---

## 6. KubeCost 설치

### 6.1 KubeCost란?

**📌 개념 설명: KubeCost**
Kubernetes 클러스터의 비용을 네임스페이스, Deployment, Pod 단위로 분석하는 도구다. AWS 비용 데이터와 Kubernetes 리소스 사용량을 연결하여 "이 서비스가 한 달에 얼마를 쓰는가"를 알 수 있다.

### 6.2 EBS CSI 드라이버 미설치 문제

**🔥 트러블슈팅 4: KubeCost PVC 바인딩 실패**

**증상:**
```
kubecost PVC 상태: Pending
kubectl describe pvc kubecost-cost-analyzer → "no persistent volumes available"
Events: waiting for first consumer to be created before binding
```

**원인:**
KubeCost가 데이터를 저장하기 위해 PersistentVolumeClaim(PVC)을 생성한다. PVC는 EBS 볼륨을 프로비저닝하는데, 이를 위해 **EBS CSI 드라이버**가 클러스터에 설치되어 있어야 한다. 이 프로젝트에서는 EBS CSI 드라이버를 설치하지 않았다.

**📌 개념 설명: EBS CSI 드라이버**
EBS(Elastic Block Store)를 Kubernetes PersistentVolume으로 사용하기 위한 드라이버. 없으면 EBS 볼륨을 Pod에 마운트할 수 없다.

**해결 1 (빠른 해결): persistentVolume 비활성화**
```yaml
# KubeCost values.yaml
persistentVolume:
  enabled: false  # emptyDir 사용 (Pod 재시작 시 데이터 초기화)
```

데이터가 Pod 재시작 시 초기화되지만, 데모 환경에서는 허용 가능.

**해결 2 (근본 해결): EBS CSI 드라이버 설치**
```bash
# EBS CSI 드라이버 애드온 활성화
aws eks create-addon \
  --cluster-name bodybuddy-dev \
  --addon-name aws-ebs-csi-driver \
  --service-account-role-arn arn:aws:iam::902371998304:role/AmazonEKS_EBS_CSI_DriverRole
```

### 6.3 KubeCost 설치 (ArgoCD Application)

```yaml
# bodybuddy-infra/argocd/apps/kubecost.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kubecost
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://kubecost.github.io/cost-analyzer/
    chart: cost-analyzer
    targetRevision: 2.6.5
    helm:
      values: |
        persistentVolume:
          enabled: false
        prometheus:
          enabled: false  # kube-prometheus-stack의 Prometheus 재사용
          fqdn: http://kube-prometheus-stack-prometheus.bodybuddy-system:9090
  destination:
    server: https://kubernetes.default.svc
    namespace: bodybuddy-system
```

---

## 7. Grafana Tempo (트레이스 저장소)

### 7.1 설치

```yaml
# bodybuddy-infra/argocd/apps/tempo.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: tempo
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://grafana.github.io/helm-charts
    chart: tempo
    targetRevision: 1.10.3
    helm:
      values: |
        tempo:
          storage:
            trace:
              backend: local    # S3도 가능하지만 로컬로 단순화
        persistence:
          enabled: false        # EBS CSI 없으므로 emptyDir
  destination:
    server: https://kubernetes.default.svc
    namespace: bodybuddy-system
```

### 7.2 Grafana에 Tempo 데이터소스 추가

```bash
curl -X POST http://admin:password@grafana:3000/api/datasources \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Tempo",
    "type": "tempo",
    "url": "http://tempo.bodybuddy-system:3100",
    "access": "proxy"
  }'
```

---

## 8. OpenTelemetry Collector

### 8.1 OTel Collector란?

**📌 개념 설명: OpenTelemetry Collector**
트레이스, 메트릭, 로그를 수신하고 변환하여 여러 백엔드로 전달하는 에이전트다. 앱은 OTLP 프로토콜로 Collector에게만 전송하면 된다. Collector가 Tempo, Prometheus, CloudWatch 등 여러 백엔드로 라우팅한다.

```
[앱] --OTLP gRPC--> [OTel Collector] ---> [Tempo (트레이스)]
                                     ---> [Prometheus (메트릭)]
                                     ---> [CloudWatch (로그)]
```

### 8.2 OTel Collector 설치

```yaml
# bodybuddy-infra/argocd/apps/otel-collector.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: otel-collector
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://open-telemetry.github.io/opentelemetry-helm-charts
    chart: opentelemetry-collector
    targetRevision: 0.108.0
    helm:
      values: |
        mode: deployment
        config:
          receivers:
            otlp:
              protocols:
                grpc:
                  endpoint: 0.0.0.0:4317
                http:
                  endpoint: 0.0.0.0:4318
          exporters:
            otlp:
              endpoint: tempo.bodybuddy-system:4317
              tls:
                insecure: true
          service:
            pipelines:
              traces:
                receivers: [otlp]
                exporters: [otlp]
  destination:
    server: https://kubernetes.default.svc
    namespace: bodybuddy-system
```

### 8.3 트러블슈팅: OTel Collector 서비스 이름 오해

**🔥 트러블슈팅 5: Collector 엔드포인트 연결 실패**

**증상:**
```
앱 로그: "failed to export spans: connection refused otel-collector:4317"
```

**원인:**
Helm 릴리즈 이름이 `otel-collector`이면 Kubernetes Service 이름은 `otel-collector-opentelemetry-collector`가 된다 (Helm chart의 fullname template이 `<릴리즈명>-<차트명>` 형식을 사용하기 때문).

**해결:**
```bash
# 실제 Service 이름 확인
kubectl get svc -n bodybuddy-system | grep otel
# otel-collector-opentelemetry-collector   ClusterIP   10.100.x.x   4317/TCP

# 앱 환경변수 수정
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector-opentelemetry-collector.bodybuddy-system:4317
```

**배운 점:** Helm chart의 Service 이름은 chart 내부의 `_helpers.tpl`에 정의된 `fullname` 함수에 따라 결정된다. 릴리즈 이름 + 차트 이름이 합쳐지는 경우가 많다. 설치 후 반드시 `kubectl get svc`로 실제 이름을 확인해야 한다.

---

## 9. 코드 계측 (Instrumentation)

### 9.1 InitTracer()

```go
// internal/observability/tracing.go
func InitTracer(ctx context.Context, serviceName string) (func(), error) {
    endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if endpoint == "" {
        endpoint = "http://otel-collector-opentelemetry-collector.bodybuddy-system:4317"
    }

    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint(endpoint),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, fmt.Errorf("creating OTLP exporter: %w", err)
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String(serviceName),
        )),
        // 샘플링: 모든 요청 (데모용, 프로덕션에서는 100%는 너무 많을 수 있음)
        trace.WithSampler(trace.AlwaysSample()),
    )

    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},  // W3C TraceContext
        propagation.Baggage{},
    ))

    return func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        tp.Shutdown(ctx)
    }, nil
}
```

### 9.2 Gin 미들웨어 계측

```go
// cmd/user-service/main.go
shutdown, err := observability.InitTracer(ctx, "user-service")
if err != nil {
    slog.Error("failed to init tracer", "error", err)
}
defer shutdown()

r := gin.New()
r.Use(otelgin.Middleware("user-service"))  // 자동으로 Span 생성
```

### 9.3 HTTP 클라이언트 계측

```go
// analysis-worker → score-service 호출
httpClient := &http.Client{
    Transport: otelhttp.NewTransport(http.DefaultTransport),
}

req, _ := http.NewRequestWithContext(ctx, "POST", scoreServiceURL, body)
resp, err := httpClient.Do(req)
// → 자동으로 Span 생성, TraceContext 헤더 주입
```

---

## 10. SQS 분산 트레이싱 전파

### 10.1 문제: SQS는 HTTP 헤더가 없다

HTTP 요청은 `traceparent` 헤더로 TraceContext를 전파한다. 하지만 SQS 메시지에는 HTTP 헤더가 없다. SQS를 거치면 트레이스가 끊어진다.

```
[user-service] -- HTTP --> [score-service]  ← 트레이스 연결됨
[user-service] -- SQS --> [analysis-worker] ← 트레이스 끊어짐 (기본)
```

### 10.2 SQS Message Attribute를 활용한 전파

```go
// SQS 메시지 발행 시 TraceContext 주입 (user-service)
type SQSCarrier map[string]string

func (c SQSCarrier) Set(key, val string) {
    c[key] = val
}

func injectTraceContext(ctx context.Context, attrs map[string]types.MessageAttributeValue) {
    carrier := make(SQSCarrier)
    otel.GetTextMapPropagator().Inject(ctx, carrier)

    for k, v := range carrier {
        attrs[k] = types.MessageAttributeValue{
            DataType:    aws.String("String"),
            StringValue: aws.String(v),
        }
    }
}

// 발행 시 사용
messageAttrs := map[string]types.MessageAttributeValue{}
injectTraceContext(ctx, messageAttrs)

_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
    QueueUrl:          aws.String(queueURL),
    MessageBody:       aws.String(body),
    MessageAttributes: messageAttrs,
})
```

```go
// SQS 메시지 수신 시 TraceContext 추출 (analysis-worker)
type SQSReadCarrier map[string]types.MessageAttributeValue

func (c SQSReadCarrier) Get(key string) string {
    if attr, ok := c[key]; ok {
        return aws.ToString(attr.StringValue)
    }
    return ""
}

func (c SQSReadCarrier) Keys() []string {
    keys := make([]string, 0, len(c))
    for k := range c {
        keys = append(keys, k)
    }
    return keys
}

// 수신 시 사용
carrier := SQSReadCarrier(msg.MessageAttributes)
ctx = otel.GetTextMapPropagator().Extract(context.Background(), carrier)
// → 이 ctx로 시작한 Span은 user-service의 Span과 연결됨
```

이렇게 하면 Tempo에서 user-service → SQS 전송 → analysis-worker → score-service 전체 경로가 하나의 Trace로 표시된다.

---

## 11. 추가 트러블슈팅

### 11.1 ALB 504 Gateway Timeout

**🔥 트러블슈팅 6: ALB 504 에러**

**증상:**
```
curl http://<alb-dns>/api/v1/users/me → 504 Gateway Timeout
ALB Target Group Health Check: unhealthy
```

**원인:**
`kubernetes.io/cluster/bodybuddy-dev: owned` 태그가 달린 보안그룹이 두 개 있었다. ALB가 어떤 보안그룹에 규칙을 추가해야 할지 모호해졌다.

보안그룹 1: EKS 클러스터 생성 시 자동 생성 (EKS 관리)
보안그룹 2: Terraform으로 명시적 생성 (하지만 같은 태그 보유)

ALB Controller가 잘못된 보안그룹에 ingress 규칙을 추가하여 ALB → Pod 트래픽이 차단됨.

**해결:**
```bash
# 중복 태그가 달린 보안그룹 확인
aws ec2 describe-security-groups \
  --filters "Name=tag:kubernetes.io/cluster/bodybuddy-dev,Values=owned"

# 중복 보안그룹에서 태그 제거
aws ec2 delete-tags \
  --resources sg-xxxxxxxxxx \
  --tags Key=kubernetes.io/cluster/bodybuddy-dev
```

### 11.2 DB Migration 후 users 테이블 없음

**🔥 트러블슈팅 7: destroy/reapply 후 409 Conflict 오판정**

**증상:**
```
POST /api/v1/auth/register → 409 Conflict
실제로는 새 RDS이고 테이블도 없는데 왜 409?
```

**원인 추적:**
```bash
kubectl logs user-service-xxx -n bodybuddy | grep "register"
# ERROR: relation "users" does not exist (42P01)
```

users 테이블이 없는 상태에서 INSERT를 시도했다. 에러 코드 `42P01` (table not found)을 처리하는 코드가 없었고, 기본 에러 핸들러가 이를 409로 잘못 반환했다.

**원인:** terraform destroy 후 재apply 시 새 RDS가 생성됐는데 마이그레이션을 실행하지 않았다.

**해결:**
```bash
# 마이그레이션 실행 (bastion Pod 활용)
kubectl run migrator --image=migrate/migrate \
  --rm -it --restart=Never \
  -- -path /migrations -database "postgresql://..." up
```

또는 Helm chart에 init container로 마이그레이션 자동화:
```yaml
initContainers:
  - name: migrate
    image: migrate/migrate
    command: ["/bin/sh", "-c"]
    args: ["migrate -path /migrations -database $DATABASE_URL up"]
```

### 11.3 user-service IRSA 누락

**🔥 트러블슈팅 8: EC2 IMDS role not found**

**증상:**
```
user-service 로그: "NoCredentialProviders: no valid providers in chain"
또는: "failed to retrieve credentials: EC2RoleRequestError: no EC2 instance role found"
```

**원인:**
user-service에 IRSA 설정이 없었다. user-service는 S3 presigned URL 발급을 위해 `s3:PutObject` 권한이 필요한데, IAM Role을 연결하지 않았다.

**해결:**
```hcl
# Terraform으로 user-service IRSA 추가
module "user_service_irsa" {
  source = "../../modules/iam-irsa"

  role_name           = "bodybuddy-dev-user-service-irsa"
  service_account     = "user-service"
  namespace           = "bodybuddy"
  oidc_provider_arn   = module.eks.oidc_provider_arn
  oidc_provider       = module.eks.oidc_provider

  policy_json = jsonencode({
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:PutObject", "s3:GetObject"]
      Resource = "${module.s3.image_bucket_arn}/inbody/*"
    }]
  })
}
```

```yaml
# deploy/helm/user-service/values.yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::902371998304:role/bodybuddy-dev-user-service-irsa
```

---

## 핵심 요약

- **관측성 3축**: Metrics(Prometheus, 숫자), Logs(CloudWatch, 이벤트), Traces(OTel+Tempo, 요청 경로). 각각 다른 질문에 답한다.
- **kube-prometheus-stack 설치 장벽 3가지**: 노드 Pod 수 제한 → coredns 스케일다운, CRD 크기 초과 → `--server-side`, Webhook TLS → `admissionWebhooks.enabled=false`.
- **ServiceMonitor**: Prometheus 설정 파일 직접 편집 대신 CRD로 scrape 대상 선언. `release: kube-prometheus-stack` 라벨 필수.
- **KubeCost + EBS CSI**: KubeCost는 PVC가 필요한데 EBS CSI 드라이버 없으면 실패. `persistentVolume.enabled=false`로 emptyDir 우회.
- **OTel Collector Service 이름**: Helm fullname template으로 `<릴리즈명>-opentelemetry-collector` 형식. 실제 이름은 `kubectl get svc`로 확인.
- **SQS 트레이스 전파**: HTTP 헤더 없는 SQS에서 TraceContext를 MessageAttribute로 주입/추출. W3C TraceContext propagator 사용.
- **고카디널리티 라벨 금지**: Prometheus 라벨에 user_id, request_id 등 고유 식별자 금지. Prometheus 메모리 폭발 원인.
- **CPU limit 미설정**: throttling 방지. Memory limit만 설정.
- **destroy/reapply 체크리스트**: IRSA Trust Policy 업데이트, DB 마이그레이션 재실행, ArgoCD 재등록.
