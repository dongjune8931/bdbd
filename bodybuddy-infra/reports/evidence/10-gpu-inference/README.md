# GPU Inference Drill Evidence

2026-07-16 EKS GPU OCR 드릴의 증거 이미지를 저장한다.

| 파일 | 증명할 내용 |
|---|---|
| `01-node-placement.png` | `g4dn.xlarge` on-demand GPU 노드와 inference Pod 배치 (수집 완료) |
| `02-nvidia-runtime.png` | Tesla T4/CUDA 인식과 EasyOCR model ready (수집 완료) |
| `03-e2e-worker-dlq.png` | GPU OCR 성공, score 반영, queue/DLQ 0건 |
| `04-gpu-dashboard.png` | GPU utilization 81%, memory 1,284 MiB, 추론 시계열 (수집 완료) |
| `05-tempo-trace.png` | worker -> inference -> OCR -> score 지연 분해 (수집 완료) |
| `06-fallback.png` | inference 장애 시 deterministic fallback 성공 |
| `07-scale-to-zero.png` | inference replica 0과 Karpenter GPU 노드 회수 |

핵심 포트폴리오에는 `04`, `05`, `06`, `07`을 우선 사용하고, 나머지는 상세 보고서 증거로 사용한다.
