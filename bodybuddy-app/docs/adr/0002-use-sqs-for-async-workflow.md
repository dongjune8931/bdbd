# 0002. 비동기 워크플로에 SQS를 사용한다

## Status

Accepted

## Context

인바디 업로드 이후 분석 작업은 즉시 완료될 필요가 없다. 실제 OCR 또는 이미지 분석이 붙는다면 처리 시간이 길어질 수 있고, 외부 API 지연이나 일시 실패도 고려해야 한다.

이 프로젝트에서는 OCR 정확도보다 비동기 처리, retry, DLQ, worker scale-out, interruption 대응을 보여주는 것이 더 중요하다.

## Decision

업로드 분석과 알림 발송 사이에 SQS Standard Queue를 사용한다.

Queues:

- `analysis-queue`: `user-service`가 업로드 작업을 발행하고 `analysis-worker`가 소비한다.
- `notification-queue`: `score-service`가 점수 반영 후 알림 이벤트를 발행하고 `notification-worker`가 소비한다.

각 queue에는 DLQ를 연결하고, worker는 메시지 처리 중 종료 신호를 받으면 신규 polling을 멈추고 in-flight 메시지를 안전하게 처리하거나 재전달되게 한다.

## Alternatives Considered

### Kafka

장점:

- 높은 처리량과 replay에 강하다.
- 이벤트 스트리밍 관점에서 확장성이 좋다.

단점:

- 자체 운영 복잡도가 크다.
- 현재 서비스 규모에서는 학습 포인트가 Kafka 운영으로 과하게 이동한다.
- AWS dev 환경 비용과 운영 부담이 늘어난다.

### 동기 HTTP 호출만 사용

장점:

- 구현이 단순하다.
- 요청 흐름을 추적하기 쉽다.

단점:

- 이미지 분석 지연이 사용자 요청 지연으로 바로 전파된다.
- worker scale-out과 retry 시나리오를 보여주기 어렵다.
- Spot interruption 대응의 의미가 약해진다.

## Consequences

좋은 점:

- worker는 API와 독립적으로 확장될 수 있다.
- Spot 노드에서 worker가 내려가도 메시지 재전달로 복구할 수 있다.
- DLQ, queue depth, visibility timeout을 운영 지표로 삼을 수 있다.

비용:

- 메시지 멱등성 처리가 필요하다.
- 분산 트레이싱에서는 HTTP 호출보다 context propagation을 별도로 챙겨야 한다.

## Validation

- 업로드 요청이 `analysis queued`로 응답하고, `analysis-worker`가 SQS 메시지를 소비했다.
- worker drain 중 처리 중이던 메시지에 대해 `context cancelled during mock OCR sleep, re-queuing message` 로그를 확보했다.
- 새 worker가 re-queued 메시지를 다시 처리하고 최종 score가 반영됐다.
- 큐 기반 구조 덕분에 worker가 Spot 노드에 있어도 사용자-facing API는 critical on-demand 노드에 유지됐다.
