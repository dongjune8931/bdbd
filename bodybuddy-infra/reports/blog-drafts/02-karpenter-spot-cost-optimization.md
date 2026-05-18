# Karpenter와 Spot으로 worker 비용을 줄이며 배운 것

## 한 줄 요약

Worker를 spot 노드에 올리는 것은 단순 비용 절감이 아니라, 애플리케이션이 interruption을 견딜 수 있는 구조인지 검증하는 일이다.

## 왜 Karpenter를 썼나

Cluster Autoscaler 대신 Karpenter를 선택한 이유는 명확했다.

- pending Pod를 기준으로 빠르게 노드를 만든다.
- NodePool별로 workload 특성을 분리하기 쉽다.
- spot과 on-demand를 정책으로 나눌 수 있다.
- 불필요한 노드를 consolidation으로 줄일 수 있다.

BodyBuddy에서는 NodePool을 두 개로 나눴다.

| NodePool | 대상 | Capacity | 이유 |
|---|---|---|---|
| `critical-pool` | API 서비스 | on-demand | 사용자 요청 경로 |
| `batch-pool` | worker 서비스 | spot | 큐 기반, 재시도 가능 |

## 핵심은 nodeSelector였다

Karpenter가 노드를 만들어도 Pod가 아무 데나 뜨면 의미가 없다. 그래서 Helm values에서 workload별 nodeSelector를 고정했다.

```yaml
nodeSelector:
  workload-type: batch
```

Worker는 `batch-pool`에만, API는 `critical-pool`에만 배치되도록 했다.

## interruption 드릴

실제 spot interruption notice를 기다리는 대신, worker가 올라간 spot 노드를 drain해서 같은 종료 흐름을 재현했다.

중요한 로그는 이 구간이었다.

```text
context cancelled during mock OCR sleep, re-queuing message
shutdown signal received, stopping polling loop
all in-flight messages processed
analysis-worker stopped
```

이 로그는 네 가지를 보여준다.

- worker가 실제로 메시지를 처리 중이었다.
- 종료 신호를 받자 신규 polling을 멈췄다.
- 처리 중이던 메시지를 re-queue했다.
- 새 worker가 다시 메시지를 처리할 수 있게 만들었다.

## SQS가 안전망이었다

Spot은 언제든 끊길 수 있다. 그래서 worker는 다음 전제를 만족해야 한다.

- 메시지 처리는 멱등적이어야 한다.
- visibility timeout이 처리 시간보다 충분히 길어야 한다.
- shutdown 중에는 새 메시지를 받지 않아야 한다.
- 처리 중 중단되면 메시지를 잃지 않아야 한다.

이번 드릴에서는 drain 중 중단된 메시지가 새 spot 노드의 worker에서 다시 처리됐고, 최종 score가 반영됐다.

## 비용 최적화 이야기의 핵심

단순히 "spot을 썼다"보다 중요한 말은 이것이다.

> 사용자 요청 경로는 on-demand에 두고, 재시도 가능한 비동기 worker만 spot에 배치했다. 그리고 실제 interruption 상황에서 메시지 유실 없이 복구되는 것을 로그와 결과값으로 확인했다.

이렇게 말하면 비용 절감과 안정성 검증이 같이 설명된다.

## 남은 개선

- AWS Fault Injection Simulator로 실제 interruption 이벤트까지 자동화
- KubeCost에서 spot 절감액 캡처 추가
- Worker를 SQS queue depth 기준으로 스케일하는 KEDA 적용 검토
