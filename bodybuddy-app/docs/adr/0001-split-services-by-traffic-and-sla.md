# 0001. 서비스 분리를 트래픽 특성과 SLA 기준으로 한다

## Status

Accepted

## Context

BodyBuddy는 사용자 인증, 업로드 요청, 점수 계산, 랭킹 조회, 알림 발송을 포함한다. 기능만 보면 하나의 API 서버로도 만들 수 있지만, 이 프로젝트의 목적은 단순 기능 구현보다 EKS 위에서 작은 MSA를 운영하며 배포, 관측성, 확장성, 복구 전략을 검증하는 것이다.

서비스 분리를 도메인 명사만 보고 하면 과하게 잘게 쪼개질 수 있다. 반대로 하나의 서버로 묶으면 트래픽 특성과 운영 요구가 다른 컴포넌트를 같은 방식으로만 다루게 된다.

## Decision

서비스를 다음 다섯 개로 분리한다.

| Service | Type | Main responsibility | Runtime expectation |
|---|---|---|---|
| `user-service` | HTTP API | 인증, 프로필, 업로드 요청 생성 | 빠른 응답, 사용자-facing |
| `score-service` | HTTP API | 캐릭터 상태, 랭킹 조회, 점수 업데이트 | read-heavy, HPA 대상 |
| `analysis-worker` | Worker | SQS 소비, 재시도, inference orchestration | 지연 허용, retry 가능 |
| `inference-service` | Internal API | S3 이미지 로드, EasyOCR 필드 추출 | 내부 의존성, GPU 전용 워크로드 |
| `notification-worker` | Worker | 알림 이벤트 소비 | 지연 허용, retry 가능 |

분리 기준은 "비즈니스 도메인 이름"보다 **트래픽 특성과 SLA**를 우선한다.

## Alternatives Considered

### 단일 API 서버

장점:

- 구현이 가장 단순하다.
- 로컬 개발과 배포가 쉽다.

단점:

- 비동기 worker와 HTTP API의 운영 요구가 섞인다.
- Spot 노드와 on-demand 노드 분리 시연이 약해진다.
- 장애나 부하가 한 프로세스에 집중된다.

### 더 많은 서비스 분리

예를 들어 auth, upload, ranking, character, notification을 모두 별도 서비스로 분리할 수 있다.

장점:

- 경계가 더 세밀하다.
- 일부 기능을 독립적으로 확장할 수 있다.

단점:

- 현재 규모에서는 운영 복잡도가 더 크다.
- 서비스 간 통신과 배포 단위가 너무 많아져 핵심 인프라 학습 포인트가 흐려진다.

## Consequences

좋은 점:

- API와 worker를 다른 NodePool에 배치할 수 있다.
- GPU inference를 별도 NodePool에 배치할 수 있다.
- `score-service`에만 HPA를 붙여 read-heavy 부하 실험을 만들 수 있다.
- `analysis-worker`는 SQS 재시도와 graceful shutdown을 별도로 검증할 수 있다.

비용:

- Helm chart, 배포, 로그, 메트릭 대상이 다섯 개로 늘어난다.
- 로컬 실행과 환경변수 관리가 단일 서버보다 복잡하다.

## Validation

- Go 단위 테스트와 Helm 렌더링으로 다섯 번째 서비스의 인터페이스와 배포 구성을 검증했다.
- 로컬 CPU runtime에서 S3 이미지 다운로드와 실제 EasyOCR 호출을 확인했으며, EKS GPU 실측은 별도 드릴로 남겼다.
- worker 노드 drain 중 `analysis-worker`가 메시지를 re-queue하고 새 worker가 다시 처리하는 로그를 확보했다.
- `score-service`에 HPA를 붙인 뒤 ranking read 부하에서 `p95 417.59ms -> 35.65ms` 개선을 확인했다.
