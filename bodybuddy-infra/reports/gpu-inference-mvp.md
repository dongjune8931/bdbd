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

## 로컬 검증 결과

- EasyOCR CPU 이미지 빌드 및 모델 로딩 성공
- LocalStack S3 원본 다운로드와 OCR runtime 호출 성공
- 한글 합성 샘플은 모델 품질 한계로 필드 파싱에 실패했으며, 영문 모델 smoke test와 실제 인바디 샘플 EKS 검증을 분리해 기록한다.
- GPU 실측값은 dev 클러스터를 다시 올린 뒤 별도 드릴에서만 기록하며, 로컬 CPU 결과를 GPU 성능으로 주장하지 않는다.

## 포트폴리오 문장 예시

- OCR 추론 단계를 `analysis-worker`에서 분리해 `inference-service`로 독립 배포하고, GPU 전용 NodePool·device plugin·DCGM exporter를 구성해 추론 워크로드 운영 구조를 설계했습니다.
- `analysis-worker -> inference-service` 호출 경계에 timeout/fallback을 추가해 GPU 추론 장애가 전체 비동기 파이프라인 중단으로 번지지 않도록 설계했습니다.
