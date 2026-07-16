# GPU Inference Workload MVP

## 목적

BodyBuddy의 업로드 비동기 파이프라인에서 OCR/비전 추론 역할을 `analysis-worker` 안에 섞어 두지 않고, 별도 `inference-service`로 분리해 GPU 워크로드 운영 포인트를 드러낸다.

핵심은 정확도 높은 모델이 아니라 다음 세 가지다.

- CPU 기반 API/worker와 GPU 추론 워크로드를 분리 스케줄링
- 추론 지연과 실패를 별도 메트릭과 대시보드로 관측
- `analysis-worker`가 remote inference 실패 시 mock fallback으로 파이프라인을 계속 진행

## 추가한 구성

| 영역 | 추가 내용 |
|---|---|
| Application | `inference-service` (Go gateway) + EasyOCR/PyTorch runtime |
| Scheduling | `gpu-pool` NodePool, `gpu` EC2NodeClass |
| GPU Runtime | EasyOCR 1.7.2 + PyTorch CUDA, NVIDIA device plugin |
| GPU Metrics | DCGM exporter + Grafana dashboard |
| Pipeline Safety | `analysis-worker -> inference-service` 호출 실패 시 mock fallback |

## 흐름

```text
user-service -> presigned PUT -> S3 ObjectCreated -> SQS analysis queue
  -> analysis-worker
    -> inference-service (internal HTTP)
      -> S3 GetObject -> EasyOCR GPU runtime -> field parser
    -> score-service
    -> notification-worker
```

## 왜 분리했는가

`analysis-worker`가 직접 OCR mock을 수행하면 비동기 파이프라인은 보이지만, GPU 워크로드 운영이라는 인프라 포인트는 약해진다.

별도 `inference-service`로 분리하면 다음 설명이 가능해진다.

- 왜 GPU 노드는 on-demand로 두고 batch worker와 분리했는지
- 왜 device plugin과 exporter가 필요했는지
- 왜 inference latency와 GPU utilization을 따로 봐야 하는지
- 왜 worker에는 fallback 경로를 둬야 하는지

## 현재 범위

- 사전 학습된 EasyOCR 모델로 이미지 텍스트를 실제 추론
- Go gateway가 S3 원본을 읽고 OCR 결과에서 체중·골격근량·체지방률·BMI를 구조화
- EKS에서는 CUDA runtime sidecar가 `nvidia.com/gpu: 1`을 요청하고 GPU 전용 NodePool에 배치
- 로컬 Compose는 동일 API를 CPU 모델로 검증하고, remote inference 실패 시 worker의 deterministic mock으로 fallback
- 평상시 replica 0, 드릴 시 `values.gpu-drill.yaml`로 1개만 기동하며 NodePool은 `g4dn.xlarge`로 제한
- 모델 학습·정확도 튜닝은 제외하고 **GPU 추론 워크로드 운영과 관측성**에 집중

## 검증 포인트

1. `inference-service` Pod이 `workload-type=gpu` 노드에만 스케줄되는지
2. `nvidia-device-plugin`과 `dcgm-exporter`가 GPU 노드에서 정상 실행되는지
3. Grafana `BodyBuddy GPU Inference Overview` 대시보드에서 다음이 보이는지
   - `Inference Ready Replicas`
   - `GPU Utilization`
   - `GPU Memory Used`
   - `Inference p95`
4. S3 이미지가 실제 OCR 결과와 점수로 변환되는지
5. `analysis-worker` remote inference 실패 시 fallback으로 score 반영이 계속되는지

## 비용 안전장치

- ArgoCD 기본 상태에서는 `inference-service` replica를 0으로 유지한다.
- GPU 증거 수집 직전에만 `values.gpu-drill.yaml`을 적용해 replica를 1로 올린다.
- NodePool은 `g4dn.xlarge` on-demand만 허용해 더 비싼 GPU 타입으로 대체되는 것을 막는다.
- 드릴 종료 후 replica를 0으로 되돌리면 Pod가 사라지고, 5분 후 빈 GPU 노드가 Karpenter consolidation 대상이 된다.

## 드릴 환경

2026-07-16 서울 리전 dev 환경에서 약 1시간 동안만 GPU 드릴을 수행했다.

| 항목 | 값 |
|---|---|
| EKS | Kubernetes 1.33 |
| GPU NodePool | Karpenter `gpu-pool`, on-demand only |
| GPU instance | `g4dn.xlarge`, NVIDIA Tesla T4 1장 |
| OCR runtime | EasyOCR 1.7.2, Korean/English, CUDA |
| inference replica | 드릴 중 1, 평상시 0 |
| worker | `analysis-worker`, SQS consumer |
| observability | Prometheus, Grafana, DCGM exporter, Tempo |

사전 점검에서 계정의 On-Demand G/VT quota가 `0 vCPU`임을 발견해 EKS 생성 전에 `4 vCPU` 증설을 요청했다. 승인을 확인한 뒤에만 Terraform을 적용해 GPU 없이 EKS/NAT/RDS 비용만 발생하는 상황을 피했다.

## EKS 실측 결과

### 1. GPU 배치와 실제 CUDA 실행

- Karpenter가 `gpu-pool` NodeClaim을 생성한 뒤 `g4dn.xlarge` 노드가 약 3분 37초 만에 Initialized 상태가 됐다.
- `inference-service`는 `workload-type=gpu` taint/toleration과 `nvidia.com/gpu: 1` 요청을 통해 GPU 노드에만 배치됐다.
- runtime의 `nvidia-smi`에서 Tesla T4, CUDA 13.0, 15,360 MiB 메모리를 확인했다.
- EasyOCR startup 로그에서 `accelerator=Tesla T4`, `model=easyocr-1.7.2-ko-en`을 확인했다.
- 최초 CUDA OCR 이미지 pull은 약 95.7초가 걸렸다. 약 3.66GB 모델 이미지가 GPU cold start에서 무시할 수 없는 지연 요소임을 확인했다.

### 2. 실제 OCR end-to-end 처리

S3 presigned PUT으로 올린 실제 테스트 이미지를 `S3 ObjectCreated -> SQS -> analysis-worker -> inference-service -> score-service` 경로로 처리했다.

| 항목 | 실측 |
|---|---:|
| 실험 시작 | 2026-07-16 14:45:57 KST |
| upload id | `32079cfb-443f-4105-ae0e-e706a72925ff` |
| OCR model duration | 435 ms |
| inference-service span | 850.6 ms |
| analysis-worker 전체 처리 | 916.6 ms |
| 계산 점수 | 74 |
| analysis queue | visible 0 / in-flight 0 |
| analysis DLQ | 0 messages |

실제 OCR 텍스트가 `Skeletal | Muscle | Mass 34.8 kg`처럼 여러 줄로 분리돼 최초 파싱은 실패했다. 최대 3개 인접 OCR line을 결합해 라벨과 값을 탐색하도록 parser를 보완하고 실제 fragment를 단위 테스트로 고정한 뒤, 동일 GPU 경로에서 성공을 확인했다.

### 3. GPU 관측성

짧은 구간에 GPU 추론 45건을 추가 실행해 idle 상태가 아닌 실제 부하 시계열을 만들었다.

| 지표 | 실측 |
|---|---:|
| 총 성공 추론 | 46건 |
| 실패 추론 | 0건 |
| 최대 GPU utilization | 81% |
| 최대 framebuffer memory used | 1,284 MiB |

Grafana `BodyBuddy GPU Inference Overview`에서 DCGM GPU 사용률·메모리와 애플리케이션 inference 요청·지연을 같은 시간축으로 확인했다. 따라서 지연 증가가 GPU 포화인지, 모델 실행 외 구간인지 함께 판단할 수 있다.

### 4. 분산 trace로 지연 분해

Tempo trace `edf64afcecb5e01c6de7979b1a019c83`에서 총 8개 span을 확인했다.

| 구간 | 실측 |
|---|---:|
| `analysis-worker.processMessage` | 916.6 ms |
| worker -> inference HTTP | 858.8 ms |
| `inference-service /internal/v1/inference` | 850.6 ms |
| inference -> OCR runtime HTTP | 704.7 ms |
| worker -> score HTTP | 52.2 ms |
| `score-service /internal/v1/score` | 47.2 ms |
| DB score persistence | 4.9 ms |
| notification enqueue | 42.1 ms |

이 trace에서는 OCR runtime 구간이 critical path 대부분을 차지하고 DB write는 5ms 미만이었다. S3 ObjectCreated 이벤트는 upstream trace context를 전달하지 않으므로 trace root는 `analysis-worker`에서 시작한다. user-service까지 하나의 연속 trace로 보인다고 과장하지 않는다.

### 5. GPU 장애 격리와 fallback

`inference-service`를 1개에서 0개로 내린 뒤 새 업로드를 발생시켰다. worker는 connection refused를 감지한 뒤 `cpu-mock-fallback`으로 전환해 점수 40을 반영했고, 메시지를 성공적으로 ack했다.

| 항목 | 실측 |
|---|---:|
| fallback upload id | `c8a5e691-7be1-4c01-8c46-951fe31770f0` |
| fallback 처리 시간 | 3,199 ms |
| analysis queue | 0 messages |
| analysis DLQ | 0 messages |
| 결과 | score 반영 성공, 메시지 유실 없음 |

GPU 서비스 장애를 숨기는 것이 아니라 로그·메트릭에는 failure/fallback으로 남기되, 비동기 파이프라인 전체 중단과 DLQ 적재는 막는 구조임을 검증했다.

### 6. Scale-to-zero와 비용 회수

- 14:57:30 KST: `inference-service`를 `0/0`으로 축소
- 15:02:48 KST: Karpenter가 NodeClaim을 `Empty`로 판정하고 drain/termination 시작
- 15:02:54 KST: EC2 termination 시작
- 15:09:48 KST: EC2 `terminated`, GPU Node/NodeClaim 제거를 최종 확인
- scale-down 이후 Karpenter 회수 판단까지 5분 18초가 걸려 설정한 `consolidateAfter: 5m`과 일치했다.
- NodeClaim 생성부터 termination 시작까지 GPU 노드는 약 58분 47초 사용됐다. AWS 청구 확정값은 Cost Explorer 반영 후 별도로 확인한다.

## 드릴 중 발견하고 보완한 운영 이슈

| 증상 | 원인 | 조치 |
|---|---|---|
| device plugin이 GPU 노드에 배치되지 않음 | chart 기본 affinity가 NFD label을 요구 | `workload-type=gpu` 명시적 node affinity 적용 |
| OCR runtime이 read-only root filesystem에서 시작 실패 | PyTorch가 쓸 임시 디렉터리 없음 | 보안 설정은 유지하고 `/tmp`에 512Mi `emptyDir` mount |
| 단일 GPU에서 rollout Pending | 기존 Pod이 유일한 GPU를 점유한 상태에서 surge Pod 생성 | inference Deployment를 `Recreate` 전략으로 변경 |
| Object Lock 버킷 PUT이 400/403 | checksum이 필요하고 presigned signature에도 포함돼야 함 | API에서 base64 MD5를 검증하고 `Content-MD5`를 서명·반환 |
| OCR 필드 parser 422 | EasyOCR이 영문 라벨을 여러 line으로 분리 | 최대 3개 인접 line 결합과 회귀 테스트 추가 |

## 증거 캡처 체크리스트

이미지는 `bodybuddy-infra/reports/evidence/10-gpu-inference/`에 아래 이름으로 저장한다.

1. `01-node-placement.png`: GPU NodePool과 inference Pod 배치
2. `02-nvidia-runtime.png`: `nvidia-smi`와 EasyOCR CUDA startup 로그
3. `03-e2e-worker-dlq.png`: GPU OCR 성공 로그와 queue/DLQ 0건
4. `04-gpu-dashboard.png`: GPU utilization, memory, inference requests/latency
5. `05-tempo-trace.png`: 8-span waterfall과 Tesla T4/model attribute
6. `06-fallback.png`: connection failure 이후 mock fallback 성공
7. `07-scale-to-zero.png`: inference `0/0`과 GPU NodeClaim 제거

## 포트폴리오 문장 예시

- OCR 추론 단계를 `analysis-worker`에서 분리해 `inference-service`로 독립 배포하고, GPU 전용 NodePool·device plugin·DCGM exporter를 구성해 추론 워크로드 운영 구조를 설계했습니다.
- `analysis-worker -> inference-service` 호출 경계에 timeout/fallback을 추가해 GPU 추론 장애가 전체 비동기 파이프라인 중단으로 번지지 않도록 설계했습니다.
