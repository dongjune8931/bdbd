## 요약
- Phase 6 준비를 위해 API와 worker 워크로드의 스케줄링 대상을 Karpenter 풀별로 명시했습니다.
- API 서비스는 `critical-pool`, worker 서비스는 `batch-pool`로 고정해 이후 Spot interruption 시연의 전제를 맞췄습니다.

## 작업 내역
- `user-service` Helm values에 `nodeSelector.workload-type: critical` 추가
- `score-service` Helm values에 `nodeSelector.workload-type: critical` 추가
- `analysis-worker` Helm values에 `nodeSelector.workload-type: batch` 추가
- `notification-worker` Helm values에 `nodeSelector.workload-type: batch` 추가

## 기대 효과
- API와 worker가 서로 다른 Karpenter NodePool에 스케줄되도록 의도를 코드로 고정할 수 있습니다.
- Phase 6에서 worker만 Spot/batch 노드에 올린 뒤 interruption, drain, 재처리, 멱등성 시연을 진행하기 쉬워집니다.

## 검증 포인트
- 머지 후 ArgoCD sync 완료
- `kubectl get pods -n bodybuddy -o wide`로 worker가 batch 노드에, API가 critical 노드에 올라가는지 확인
- `kubectl get nodes --show-labels`로 `workload-type` 라벨이 기대와 일치하는지 확인

## 참고
- 이번 PR은 스케줄링 의도를 명시하는 1차 변경입니다.
- 실제 Spot interruption 시연은 후속 Phase 6 작업에서 진행합니다.
