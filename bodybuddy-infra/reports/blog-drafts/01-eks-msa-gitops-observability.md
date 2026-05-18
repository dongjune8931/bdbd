# EKS 위에 비동기 MSA를 올리며 배운 것

## 한 줄 요약

작은 사이드 프로젝트라도 EKS, GitOps, 관측성을 실제로 붙이면 "쿠버네티스를 써봤다"가 아니라 "운영 가능한 단위로 설계해봤다"는 이야기를 만들 수 있다.

## 글의 목표

이 글은 BodyBuddy를 만들면서 4개 Go 서비스와 AWS managed service를 EKS 위에 올리고, ArgoCD와 관측성을 붙인 과정을 정리한다. 핵심은 기능 구현이 아니라 운영 관점의 선택이다.

## 문제 설정

BodyBuddy는 인바디 결과 업로드를 점수화해 캐릭터 성장과 랭킹으로 연결하는 서비스다. 기능만 보면 단순하지만, 인프라 학습 관점에서는 좋은 재료가 많다.

- API 요청과 worker 처리를 분리할 수 있다.
- 이미지 업로드, 큐, DB, 캐시가 자연스럽게 등장한다.
- 랭킹 조회처럼 read-heavy 부하를 만들 수 있다.
- 장애 복구와 비용 최적화를 시연하기 쉽다.

## 아키텍처 선택

서비스는 4개로 나눴다.

| Service | 역할 | 운영 특성 |
|---|---|---|
| `user-service` | 인증, 업로드 요청, SQS enqueue | 사용자 요청 경로 |
| `score-service` | 점수 업데이트, 랭킹 조회 | read-heavy API |
| `analysis-worker` | Mock OCR, 점수 계산 | 비동기, 재시도 가능 |
| `notification-worker` | 알림 큐 처리 | 비동기, 지연 허용 |

도메인별 분리보다 트래픽과 SLA를 기준으로 분리했다. API는 on-demand 노드에, worker는 spot 노드에 배치했다.

## GitOps 전환에서 배운 점

처음에는 Helm 명령으로 직접 배포했다. 이 방식은 빠르지만 Git과 클러스터 상태가 어긋나기 쉽다.

ArgoCD로 전환한 뒤에는 다음 규칙을 세웠다.

- 배포 상태의 기준은 Git이다.
- 이미지 태그는 `latest`가 아니라 git SHA다.
- 클러스터에서 직접 바꾼 값은 다시 Git에 반영하지 않으면 drift가 된다.

실제로 `values.yaml`의 이미지 태그와 클러스터 이미지가 달라져 OutOfSync가 발생했고, 그 경험이 GitOps 운영 규칙을 명확히 하는 계기가 됐다.

## 관측성은 어디까지 붙였나

운영 부담을 줄이기 위해 모든 것을 직접 호스팅하지 않았다.

- Metrics: Prometheus, Grafana
- Logs: CloudWatch Logs
- Traces: OpenTelemetry Collector, Tempo
- Cost: KubeCost

로그는 Loki 대신 CloudWatch를 사용했다. 4개 서비스 규모에서 Loki까지 직접 운영하는 것보다 AWS-native 로그 저장소를 쓰는 편이 더 현실적이라고 판단했다.

트레이싱도 전체 엔드포인트가 아니라 upload to analysis to score 경로에 집중했다. 이 경로가 서비스의 비동기 특성을 가장 잘 보여주기 때문이다.

## 검증 결과

최종적으로 다음 상태를 만들었다.

- ArgoCD App들이 `Synced Healthy` 상태로 수렴
- 서비스별 `/healthz`, `/readyz`, `/metrics` 확인
- 업로드 요청 후 SQS, worker, score update 흐름 확인
- Prometheus/Grafana로 HPA와 서비스 상태 관찰
- Tempo로 critical path trace 확인

## 면접에서 말할 포인트

이 프로젝트에서 가장 중요한 이야기는 "EKS에 올렸다"가 아니다.

더 좋은 표현은 이것이다.

> API와 worker를 트래픽 특성에 따라 분리했고, GitOps로 배포 기준을 Git에 고정했으며, metrics/logs/traces/cost를 나눠서 운영 가능한 형태로 관찰했다.

## 남은 개선

- CloudWatch Logs Grafana datasource 캡처 보강
- Alertmanager 알림 채널 최종 연결
- 대시보드 JSON을 코드로 관리
