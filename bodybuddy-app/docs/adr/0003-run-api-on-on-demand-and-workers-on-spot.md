# 0003. API는 on-demand, worker는 Spot 노드에 배치한다

## Status

Accepted

## Context

BodyBuddy의 워크로드는 사용자 요청을 직접 처리하는 API와, 지연을 허용할 수 있는 worker로 나뉜다.

API는 응답 지연과 가용성이 사용자 경험에 바로 보인다. 반면 `analysis-worker`와 `notification-worker`는 SQS 기반으로 동작하므로 일시 중단이나 재시도가 가능하다. GPU 추론은 별도 capacity와 런타임이 필요하므로 CPU 워크로드와 분리하는 편이 운영 설명력이 높다.

비용 최적화와 운영 시연을 동시에 만족하려면, 모든 Pod를 같은 노드 그룹에 넣는 것보다 중요도에 따라 배치 전략을 다르게 가져가는 편이 낫다.

## Decision

Karpenter NodePool을 세 개로 나눈다.

| NodePool | Capacity | Workloads |
|---|---|---|
| `critical-pool` | on-demand | `user-service`, `score-service` |
| `batch-pool` | spot | `analysis-worker`, `notification-worker` |
| `gpu-pool` | drill-only on-demand `g4dn.xlarge` | `inference-service` |

Helm chart에서 `nodeSelector.workload-type`을 통해 배치를 고정한다. `inference-service`는 추가로 `nvidia.com/gpu` 리소스를 요청한다.

## Alternatives Considered

### 모든 워크로드를 on-demand에 배치

장점:

- interruption 리스크가 작다.
- 운영이 단순하다.

단점:

- 비용 최적화 시연이 약하다.
- worker가 retryable하다는 특성을 활용하지 못한다.

### 모든 워크로드를 Spot에 배치

장점:

- 비용을 더 줄일 수 있다.

단점:

- 사용자-facing API까지 interruption 영향을 받을 수 있다.
- 데모 목적의 dev 환경이라도 장애 격리 설명이 약해진다.

## Consequences

좋은 점:

- 사용자-facing API와 지연 허용 worker의 운영 정책이 분리된다.
- Spot drain 시 worker graceful shutdown과 SQS re-queue를 실제로 보여줄 수 있다.
- GPU inference capacity를 batch worker와 독립적으로 관측하고 조절할 수 있다.
- 비용 최적화와 안정성의 균형을 설명하기 쉽다.

비용:

- NodePool, label, selector, Karpenter 설정이 늘어난다.
- GPU 노드 비용이 높아 상시 운영보다 필요 시 기동 전략이 중요해진다.
- Pod가 Pending일 때 어느 NodePool 문제인지 확인해야 한다.

## Validation

- `analysis-worker`, `notification-worker`는 `workload-type=batch`, `karpenter.sh/capacity-type=spot` 노드에서 실행됐다.
- `user-service`, `score-service`는 `workload-type=critical`, `karpenter.sh/capacity-type=on-demand` 노드에서 실행됐다.
- `inference-service` chart가 `workload-type=gpu`와 `nvidia.com/gpu: 1`을 요구하는지 Helm 렌더링으로 검증했다.
- 기본 replica 0, 드릴 override 1로 GPU 노드의 상시 비용 발생을 차단했다.
- Spot worker 노드 drain 중 worker가 in-flight 메시지를 re-queue하고 종료했다.
- 새 Spot 노드가 생성되고 worker가 재스케줄된 뒤 메시지 처리가 이어졌다.
