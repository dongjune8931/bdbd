# Release Engineering with Argo Rollouts

## Goal

`score-service` 배포를 단순 rolling update에서 끝내지 않고, **실제 사용자 요청 메트릭을 기준으로 승격/중단을 결정하는 progressive delivery** 단계로 확장한다.

이 설계의 목적은 다음 세 가지다.

1. 새 버전 배포 중 `5xx`가 증가하면 자동으로 승격을 멈춘다.
2. 랭킹 조회 지연이 기준치를 넘으면 배포를 계속 밀지 않는다.
3. GitOps 기반 운영(ArgoCD) 위에서 릴리즈 안전장치를 추가해 운영 역량을 보여준다.

## Why `score-service`

`score-service`는 다음 이유로 첫 롤아웃 대상에 적합하다.

- 사용자-facing read API(`/api/v1/ranking`)가 있어 HTTP 메트릭으로 품질을 검증하기 쉽다.
- 이미 HPA와 OpenTelemetry, Prometheus 메트릭이 연결돼 있어 분석 지표를 붙이기 좋다.
- 분석 워커가 내부적으로 의존하는 서비스라서, 장애가 나면 downstream 영향 설명도 가능하다.

## Design

### Controller

- Argo Rollouts Helm chart를 `bodybuddy-system` namespace에 GitOps로 설치
- ArgoCD App-of-Apps에 child Application으로 등록

### Rollout Strategy

- 대상: `score-service`
- 전략: `canary`
- 단계:
  1. `25%` canary
  2. `30s` pause
  3. Prometheus 분석
  4. `50%` canary
  5. `30s` pause
  6. Prometheus 분석
  7. `100%` 승격

### Replica Choice

가중치가 의미 있게 보이도록 기본 replica 수를 `4`로 올렸다.

- `25%` 단계: 대략 `1 canary / 3 stable`
- `50%` 단계: 대략 `2 canary / 2 stable`

서비스 메시나 ingress traffic splitting 없이도, Argo Rollouts의 기본 canary 스케일링만으로 **소규모 dev 환경에서 가시적인 progressive delivery 흐름**을 시연할 수 있게 했다.

## Analysis Gate

Prometheus 기반 `AnalysisTemplate` 두 개를 배포 게이트로 사용한다.

### 1. Error Rate

- metric: `bodybuddy_http_requests_total`
- path: `/api/v1/ranking`
- 조건: `5xx rate <= 1%`

의도:

- 새 버전 적용 직후 랭킹 API 오류율이 급증하면 배포를 중단한다.

### 2. P95 Duration

- metric: `bodybuddy_http_request_duration_seconds_bucket`
- path: `/api/v1/ranking`
- 조건: `p95 <= 300ms`

의도:

- 기능은 살아 있어도 지연이 악화되면 승격을 멈춘다.
- 단순 성공 여부가 아니라 **성능 회귀까지 배포 판단 기준에 포함**한다.

## HPA Compatibility

기존 HPA는 `Deployment`를 대상으로 했지만, Rollout 전환 후에는 `argoproj.io/v1alpha1 Rollout`을 대상으로 스케일해야 한다.

그래서 차트에서 다음 분기 처리를 넣었다.

- `rollout.enabled=false` → 기존 `Deployment`
- `rollout.enabled=true` → `Rollout` + HPA target 전환

즉, 롤아웃 실험을 끄면 일반 배포 패턴으로도 되돌릴 수 있다.

## Expected Demo Flow

1. 새 `score-service` 이미지를 배포한다.
2. Argo Rollouts가 canary ReplicaSet을 먼저 올린다.
3. `/api/v1/ranking` 실트래픽 메트릭을 Prometheus에서 읽는다.
4. 오류율과 p95가 기준 이내면 다음 step으로 승격한다.
5. 기준을 넘으면 rollout이 중단되고 원인 분석 대상으로 남는다.

## Talking Points

- "배포 성공"을 단순 Pod Running이 아니라 **실사용 메트릭 통과**로 정의했다.
- HPA, Prometheus, GitOps, progressive delivery를 한 흐름으로 연결했다.
- 작은 dev 환경이지만 `score-service`를 통해 운영 배포 기준을 코드로 명시했다.

## Evidence to Capture Later

- Argo Rollouts UI 또는 `kubectl argo rollouts get rollout score-service -n bodybuddy`
- step별 canary/stable replica 변화
- AnalysisRun 결과
- Grafana에서 `/api/v1/ranking` error rate / p95 변화
- 문제 버전 배포 시 pause/abort 되는 캡처
