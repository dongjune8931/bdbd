# 0004. ArgoCD GitOps와 명시적 이미지 태그 업데이트를 사용한다

## Status

Accepted

## Context

EKS 환경에서 직접 `kubectl apply`를 반복하면 실제 클러스터 상태와 Git 상태가 쉽게 어긋난다. 이 프로젝트는 인프라 역량을 보여주는 것이 목표이므로, 배포 결과를 재현 가능하게 만들고 변경 이력을 Git에 남기는 것이 중요하다.

또한 컨테이너 이미지는 `latest`보다 Git SHA 기반 태그가 추적과 롤백에 유리하다.

## Decision

배포는 ArgoCD App-of-Apps 패턴으로 관리한다.

이미지 태그는 Helm values에 명시하고, ECR의 Git SHA 태그를 사용한다.

배포 흐름:

1. 서비스 코드 변경
2. GitHub Actions가 이미지 빌드 및 ECR push
3. Helm values의 image tag를 새 SHA로 갱신
4. ArgoCD가 Git 변경을 감지해 sync

## Alternatives Considered

### 수동 `kubectl apply`

장점:

- 빠르게 적용할 수 있다.

단점:

- Git과 클러스터 상태가 어긋나기 쉽다.
- destroy/reapply 후 복구 흐름을 설명하기 어렵다.
- self-heal 시연이 불가능하다.

### ArgoCD Image Updater

장점:

- 새 이미지 감지와 태그 갱신을 자동화할 수 있다.

단점:

- 개인 포트폴리오 환경에서는 변경 이력이 덜 명시적으로 보일 수 있다.
- 어떤 이미지가 왜 배포됐는지 PR 단위로 설명하기 어렵다.

## Consequences

좋은 점:

- Git이 배포 source of truth가 된다.
- ArgoCD UI에서 sync/health 상태를 명확히 보여줄 수 있다.
- `kubectl delete deployment` 같은 강제 삭제 후에도 self-heal 전략을 설명할 수 있다.

비용:

- 이미지 태그 갱신이 한 단계 더 필요하다.
- destroy/reapply 후 ECR 이미지가 비어 있으면 CI 재실행이 필요하다.

## Validation

- ArgoCD `bodybuddy-app-of-apps` 아래 앱들이 `Synced Healthy` 상태로 수렴했다.
- Helm values 변경 후 ArgoCD가 서비스 배포를 갱신했다.
- destroy/reapply 후 `refresh-dev-values.sh`와 Git 반영을 통해 values drift를 줄였다.
- `metrics-server`와 `score-service` HPA 추가도 GitOps 경로로 반영했다.
