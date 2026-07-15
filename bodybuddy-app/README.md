# bodybuddy-app

Go service and Helm chart workspace for BodyBuddy.

The application side contains five small services that are intentionally split by traffic and reliability characteristics rather than by business noun alone.

## Services

| Service | Type | Responsibility |
|---|---|---|
| `user-service` | HTTP API | Auth, profile, presigned upload URL creation |
| `score-service` | HTTP API | Character state, score history, ranking read/update |
| `analysis-worker` | Worker | SQS consumer, orchestration, score-service callback |
| `inference-service` | Internal API | S3 image loading, EasyOCR field extraction, score input generation |
| `notification-worker` | Worker | SQS consumer for notification events |

## Request Flow

```text
POST /api/v1/uploads
  -> user-service writes upload metadata and returns a presigned S3 PUT URL
  -> client uploads the image directly to S3
  -> S3 ObjectCreated is routed to the analysis SQS queue
  -> analysis-worker consumes the message
  -> analysis-worker calls inference-service
  -> inference-service runs EasyOCR through a PyTorch CPU/CUDA runtime
  -> analysis-worker calls score-service
  -> score-service updates PostgreSQL and Redis ranking
  -> score-service publishes notification event
  -> notification-worker consumes notification event
```

## Local Commands

Build:

```bash
go build ./...
```

Test:

```bash
go test ./...
```

Run load tests against a live cluster port-forward:

```bash
K6_BASE_URL=http://localhost:8080 \
K6_TOKEN=<JWT_TOKEN> \
make load-upload-smoke
```

```bash
K6_SCORE_BASE_URL=http://localhost:8081 \
make load-ranking-smoke
```

## Kubernetes Packaging

Each service has a dedicated Helm chart under `deploy/helm/`.

Important conventions:

- API services use `workload-type=critical`
- Worker services use `workload-type=batch`
- `inference-service` uses `workload-type=gpu`
- ServiceMonitor templates expose metrics to Prometheus
- API services can enable HPA through chart values
- Worker charts set long termination grace periods for interruption handling

## GPU Inference MVP

The upload pipeline now separates inference from queue orchestration:

- `analysis-worker` owns SQS polling, retries, and fallback behavior
- `inference-service` owns S3 image loading, EasyOCR execution, and field parsing
- GPU scheduling is isolated through a dedicated `gpu-pool` and `nvidia.com/gpu` resource requests
- the default replica count is zero; `values.gpu-drill.yaml` enables one on-demand `g4dn.xlarge` node only during evidence drills

This keeps the project aligned with its infrastructure goal: showing how a CPU-based async worker can call, observe, and safely fall back from a separately scheduled GPU workload without turning model training into the project focus.

## Load-Test Scripts

| Script | Target | Purpose |
|---|---|---|
| `test/load/upload-burst.js` | `POST /api/v1/uploads` | Upload API throughput and queue publish stability |
| `test/load/ranking-read.js` | `GET /api/v1/ranking` | Ranking read latency and HPA behavior |

Latest measured result for ranking read:

- Before CPU HPA: `p95=417.59ms`, `377.35 req/s`, error `0%`
- After CPU HPA: `p95=35.65ms`, `572.03 req/s`, error `0%`

See the infrastructure report for full interpretation:

- [Load Test Report](../bodybuddy-infra/reports/load-test-report.md)
