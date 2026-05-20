# BodyBuddy Monitoring and Performance Scorecard Plan

이 문서는 BodyBuddy를 `기본 Grafana 화면` 수준에서 끝내지 않고, 서비스 특성에 맞는 커스텀 관측성과 성능 개선 성적표로 확장하기 위한 실행 계획이다.

핵심 목표는 세 가지다.

1. 업로드, 분석, 점수 반영, 알림이라는 서비스 고유 흐름을 직접 측정한다.
2. `before / after` 비교가 가능한 수치를 남긴다.
3. 발표 장표에서 바로 사용할 수 있는 대시보드와 캡처 포인트를 미리 정의한다.

## 목표 화면

최종적으로는 아래 4종을 남긴다.

1. 서비스 운영 대시보드
2. 업로드 비동기 파이프라인 대시보드
3. DR 전용 대시보드
4. 성능 개선 성적표 장표

추가로 분산 추적에서는 `OpenTelemetry + Tempo + Grafana` 기준으로 `upload -> analysis-worker -> score-service` 크리티컬 패스를 캡처한다.

## 서비스 특화 메트릭 설계

기본 kube-prometheus-stack 메트릭만으로는 BodyBuddy 흐름을 설명하기 어렵다. 아래 메트릭은 서비스 코드에서 직접 노출하는 것을 목표로 한다.

### user-service

| 메트릭 | 타입 | 의미 | 권장 라벨 |
|---|---|---|---|
| `bodybuddy_upload_requests_total` | Counter | 업로드 API 요청 수 | `result=accepted|rejected` |
| `bodybuddy_presigned_url_issued_total` | Counter | Presigned URL 발급 수 | 없음 |
| `bodybuddy_upload_metadata_writes_total` | Counter | 업로드 메타데이터 INSERT 수 | `result=success|error` |
| `bodybuddy_upload_request_duration_seconds` | Histogram | 업로드 요청 처리 시간 | `endpoint="/uploads"` |

### analysis-worker

| 메트릭 | 타입 | 의미 | 권장 라벨 |
|---|---|---|---|
| `bodybuddy_analysis_jobs_received_total` | Counter | analysis queue에서 받은 메시지 수 | 없음 |
| `bodybuddy_analysis_jobs_processed_total` | Counter | 처리 완료 수 | `result=success|retry|failure` |
| `bodybuddy_analysis_job_duration_seconds` | Histogram | 한 건 처리 시간 | 없음 |
| `bodybuddy_mock_ocr_duration_seconds` | Histogram | mock OCR sleep 포함 OCR 단계 시간 | 없음 |
| `bodybuddy_analysis_retries_total` | Counter | 재시도 횟수 | `reason=context_cancelled|downstream_error|visibility_timeout` |
| `bodybuddy_analysis_idempotency_skips_total` | Counter | 멱등성으로 중복 처리 스킵한 횟수 | 없음 |

### score-service

| 메트릭 | 타입 | 의미 | 권장 라벨 |
|---|---|---|---|
| `bodybuddy_score_updates_total` | Counter | 점수 반영 요청 수 | `result=success|error` |
| `bodybuddy_score_update_duration_seconds` | Histogram | 점수 반영 API 처리 시간 | `endpoint="/internal/score"` |
| `bodybuddy_ranking_reads_total` | Counter | 랭킹 조회 수 | `result=hit|miss` |
| `bodybuddy_ranking_read_duration_seconds` | Histogram | 랭킹 조회 시간 | `endpoint="/api/v1/ranking"` |
| `bodybuddy_redis_ranking_updates_total` | Counter | Redis ZADD 갱신 횟수 | `result=success|error` |
| `bodybuddy_level_up_events_total` | Counter | 레벨업 판정 수 | 없음 |

### notification-worker

| 메트릭 | 타입 | 의미 | 권장 라벨 |
|---|---|---|---|
| `bodybuddy_notification_jobs_received_total` | Counter | notification queue 수신 메시지 수 | 없음 |
| `bodybuddy_notification_sent_total` | Counter | 메일 발송 결과 | `result=success|failure` |
| `bodybuddy_notification_duration_seconds` | Histogram | 알림 처리 시간 | 없음 |

### DR / 운영

| 메트릭 | 타입 | 의미 |
|---|---|---|
| `S3AutoRecoveryTriggered` | CloudWatch custom metric | 자동 복구 트리거 횟수 |
| `S3AutoRecoveryRecoveredObjects` | CloudWatch custom metric | 복구 객체 수 |
| `S3AutoRecoveryFailures` | CloudWatch custom metric | 복구 실패 수 |
| `S3AutoRecoveryDurationMilliseconds` | CloudWatch custom metric | Lambda 처리 시간 |

## 대시보드 1: Services Overview

가장 기본적인 운영 대시보드다. API 서비스와 워커의 전반적인 건강 상태를 보여준다.

### 패널 구성

1. `HTTP Request Rate`
2. `HTTP Error Rate`
3. `HTTP p95 Latency`
4. `Go Goroutines`
5. `CPU Usage by Service`
6. `Memory Working Set by Service`
7. `Ready Replicas`

### PromQL 초안

```promql
sum(rate(bodybuddy_http_requests_total[5m])) by (service)
```

```promql
sum(rate(bodybuddy_http_requests_total{status_code=~"5.."}[5m])) by (service)
/ sum(rate(bodybuddy_http_requests_total[5m])) by (service)
```

```promql
histogram_quantile(
  0.95,
  sum(rate(bodybuddy_http_request_duration_seconds_bucket[5m])) by (le, service)
)
```

```promql
go_goroutines{namespace="bodybuddy"}
```

```promql
sum(rate(container_cpu_usage_seconds_total{namespace="bodybuddy", container!="", container!="POD"}[5m])) by (pod)
```

```promql
container_memory_working_set_bytes{namespace="bodybuddy", container!="", container!="POD"}
```

```promql
kube_deployment_status_replicas_ready{namespace="bodybuddy"}
```

### 캡처 포인트

- score-service scale-out 전 steady state
- ranking 부하 중 score-service CPU 상승
- HPA 반응 후 replicas 증가

## 대시보드 2: Async Upload Pipeline

이게 BodyBuddy 고유 대시보드다. 업로드 이후 분석, 점수 반영, 알림까지 한 장에서 보이게 만드는 게 목표다.

### 패널 구성

1. `Upload Requests Accepted`
2. `Presigned URLs Issued`
3. `Analysis Queue Depth`
4. `Analysis Jobs Processed`
5. `Mock OCR Duration p95`
6. `Analysis Job Duration p95`
7. `Score Updates`
8. `Notification Queue Depth`
9. `Notifications Sent`
10. `DLQ Messages`

### PromQL / CloudWatch 초안

```promql
sum(rate(bodybuddy_upload_requests_total{result="accepted"}[5m]))
```

```promql
sum(rate(bodybuddy_presigned_url_issued_total[5m]))
```

큐 depth는 두 방법 중 하나로 간다.

1. CloudWatch datasource 사용
2. exporter 도입 후 Prometheus scrape

현재 구조 기준으로는 CloudWatch datasource가 더 빠르다.

CloudWatch metric:
- `AWS/SQS > ApproximateNumberOfMessagesVisible`
- dimension: `QueueName=bodybuddy-dev-analysis-queue`

```promql
sum(rate(bodybuddy_analysis_jobs_processed_total{result="success"}[5m]))
```

```promql
histogram_quantile(
  0.95,
  sum(rate(bodybuddy_mock_ocr_duration_seconds_bucket[5m])) by (le)
)
```

```promql
histogram_quantile(
  0.95,
  sum(rate(bodybuddy_analysis_job_duration_seconds_bucket[5m])) by (le)
)
```

```promql
sum(rate(bodybuddy_score_updates_total{result="success"}[5m]))
```

```promql
sum(rate(bodybuddy_notification_sent_total{result="success"}[5m]))
```

DLQ는 CloudWatch에서 본다.

- `AWS/SQS > ApproximateNumberOfMessagesVisible`
- dimension: `QueueName=bodybuddy-dev-analysis-queue-dlq`
- dimension: `QueueName=bodybuddy-dev-notification-queue-dlq`

### 캡처 포인트

- 업로드 burst 중 analysis queue depth가 증가했다가 worker 처리로 내려오는 장면
- analysis-worker 재시도/멱등성 지표
- 알림 queue가 쌓이지 않고 소비되는 모습

## 대시보드 3: DR Overview

이미 도입한 대시보드다. 여기에 발표용 관점을 더 명확히 부여한다.

### 패널 구성

1. `자동 복구 트리거`
2. `복구된 객체`
3. `복구 실패`
4. `Lambda 실행 시간`
5. `Lambda 실행 시간 추이`
6. `Lambda 호출 / 오류`

### 발표 포인트

- 삭제 이벤트가 실제로 감지됐는가
- 자동 복구가 몇 건 실행됐는가
- 실패 없이 복구됐는가
- 복구 처리 시간은 어느 수준인가

## 대시보드 4: Cost and Scaling

운영 비용 최적화와 확장성을 함께 보여주는 장표다.

### 패널 구성

1. `Namespace Cost`
2. `Spot vs On-Demand Usage`
3. `score-service Replicas`
4. `Node Count by Pool`
5. `CPU Target vs Actual (HPA)`
6. `Karpenter Provisioning Timeline`

### 발표 포인트

- critical은 on-demand, batch는 spot에 배치
- ranking 부하 시 API 서비스만 scale-out
- worker workload는 batch node pool에서 처리
- 비용과 안정성의 균형을 의도적으로 설계

## 성능 개선 성적표 장표

샘플 발표 자료처럼 `before / after`를 한 장으로 보여주는 핵심 장표다.

### 추천 제목

- `측정 기반 성능 개선 결과`
- `Before / After Performance Scorecard`

### 표 구조

| 항목 | 개선 전 | 개선 후 | 변화 |
|---|---:|---:|---:|
| Ranking Read p50 | 21.24ms | 15.75ms | 5.49ms 개선 |
| Ranking Read p95 | 417.59ms | 35.65ms | 381.94ms 개선 |
| Ranking Read Throughput | 377 req/s | 572 req/s | 195 req/s 증가 |
| Error Rate | 0% | 0% | 유지 |
| score-service Replicas | 1 | 4 | scale-out 확인 |
| HPA CPU Peak | 213%/50% | 4 replicas로 흡수 | 자동 확장 |

### 같이 넣을 캡처

1. k6 1차 결과
2. k6 2차 결과
3. `kubectl get hpa -w` 캡처
4. `kubectl get pods -w`에서 score-service 1 -> 4 증가 캡처

## 분산 추적 장표

Jaeger 느낌의 장표를 만들고 싶다면, 실제 구현물 기준으로는 `OpenTelemetry + Tempo`라고 설명하는 것이 맞다.

### 추천 제목

- `업로드 크리티컬 패스 Trace`
- `OpenTelemetry 기반 분산 추적`

### 보여줄 Trace

`user-service -> analysis-worker -> score-service`

### 장표에서 강조할 것

1. 사용자 요청이 어디서 시작되는가
2. 비동기 분석 구간이 어떻게 이어지는가
3. mock OCR 지연이 어디서 발생하는가
4. score-service까지의 전체 흐름이 추적 가능한가

### Trace 캡처 포인트

- user-service span
- queue consume 이후 analysis-worker span
- mock OCR 단계가 duration을 차지하는 장면
- score-service 내부 score update span

## 내일 테스트 체크리스트

### A. Async pipeline 대시보드용

- upload burst 실행
- analysis queue visible messages 증가 캡처
- analysis-worker 처리율 캡처
- score update count 증가 캡처
- notification sent count 캡처

### B. 성능 개선 scorecard용

- 1차 ranking-read 결과 저장
- 2차 ranking-read 결과 저장
- HPA watch 캡처
- score-service replicas 증가 캡처

### C. DR 대시보드용

- S3 객체 삭제 수행
- SES 이메일 수신 캡처
- CloudWatch metric datapoint 캡처
- Grafana DR dashboard 반영 캡처

### D. Trace 장표용

- 업로드 1건 실행
- Tempo/Grafana trace에서 `user-service -> analysis-worker -> score-service` 흐름 캡처

## 우선 구현 순서

1. `score-service`와 `analysis-worker`에 커스텀 Prometheus 메트릭 추가
2. Async Upload Pipeline 대시보드 작성
3. ranking-read 개선 전/후 scorecard 장표 작성
4. Tempo trace 캡처 확보
5. DR dashboard 캡처 보강

## 발표용 한 줄 요약

- 기본 대시보드만 본 것이 아니라, 업로드와 분석 파이프라인에 맞는 커스텀 메트릭을 직접 설계했다.
- 메트릭과 트레이스로 병목을 확인하고, HPA와 캐시 전략이 실제로 효과를 냈는지 수치로 검증했다.
- DR 역시 복구 여부를 추상적으로 설명하지 않고, 메일 알림과 Grafana 대시보드로 측정 가능한 형태로 남겼다.
