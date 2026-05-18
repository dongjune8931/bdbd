# GitOps Cluster Recovery Runbook

## 목적

`terraform destroy` 이후 dev 환경을 다시 올리거나, 클러스터 내부 배포 구성이 망가졌을 때 ArgoCD 중심으로 GitOps 상태를 복구하는 절차를 정리한다.

이 문서는 실제 재생성/복구 경험과 `destroy-reapply-recovery.md`의 운영 메모를 바탕으로, "클러스터를 다시 올린 뒤 GitOps를 정상화하는 흐름"에 초점을 맞춘다.

---

## 대상 환경

- 클러스터: `bodybuddy-dev-eks`
- 네임스페이스:
  - `bodybuddy`
  - `bodybuddy-system`
  - `karpenter`
- 핵심 구성요소:
  - ArgoCD
  - Karpenter
  - AWS Load Balancer Controller
  - 앱 4종 (`user-service`, `score-service`, `analysis-worker`, `notification-worker`)
  - observability
  - kubecost

---

## 복구 목표

최종 목표는 아래 상태다.

```bash
kubectl get applications -n bodybuddy-system
```

기대 결과:

- 모든 application이 `Synced`
- 모든 application이 `Healthy`

---

## 권장 복구 순서

### 1. Terraform apply

클러스터와 기본 AWS 리소스를 먼저 복구한다.

```bash
AWS_PROFILE=terraform-bodybuddy terraform -chdir=bodybuddy-infra/terraform/envs/dev apply
```

중점 확인 항목:

- EKS cluster
- RDS
- ElastiCache
- S3
- SQS
- IAM / IRSA

### 2. kubeconfig 갱신

```bash
aws eks update-kubeconfig \
  --name bodybuddy-dev-eks \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

### 3. 클러스터 애드온 부트스트랩

```bash
AWS_PROFILE=terraform-bodybuddy \
  /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/scripts/bootstrap-cluster-addons.sh
```

이 스크립트는 다음을 idempotent하게 수행한다.

- namespace 생성
- ArgoCD 설치
- Karpenter 설치
- AWS Load Balancer Controller 설치
- AppProject / app-of-apps 적용

### 4. 재생성 값 refresh

destroy/reapply 이후 drift가 나는 값들을 Git source에 반영한다.

```bash
AWS_PROFILE=terraform-bodybuddy \
  /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/scripts/refresh-dev-values.sh
```

대상:

- Helm values 이미지 태그
- RDS password
- RDS / Redis endpoint
- Karpenter private subnet ID

### 5. 필요한 변경을 Git source에 반영

중요:

- ArgoCD는 Git이 source of truth다
- `kubectl apply`로 잠깐 고쳐도 Git과 다르면 self-heal이 다시 덮어쓴다

따라서 refresh 결과나 subnet 변경은 반드시 커밋/푸시해 source에 반영해야 한다.

### 6. 애플리케이션 health 확인

```bash
kubectl get applications -n bodybuddy-system
kubectl get pods -n bodybuddy
kubectl get pods -n bodybuddy-system
```

---

## 2026-05-18 재생성 복구에서 확인된 실제 문제

### 1. ECR 레포는 recreate되지만 이미지는 비어 있을 수 있음

destroy 시 `--force`로 ECR 레포를 지우면, apply 후 레포는 다시 생기지만 이미지 태그는 없다.

증상:

- 앱이 새 이미지 태그를 못 찾음
- values refresh 시 최신 non-latest SHA 선택이 의미 없어짐

대응:

1. 최근 main CI run 재실행
2. 이미지 4개를 ECR에 다시 push
3. 그 다음 values refresh

### 2. Karpenter subnet drift

증상:

- `EC2NodeClass`가 이전 subnet ID를 계속 가리킴
- `SubnetsNotFound`
- NodePool `Ready=False`
- 앱 Pod 전부 Pending

실제 원인:

- `refresh-dev-values.sh`의 subnet 치환이 YAML 들여쓰기 패턴과 안 맞아 갱신이 안 됨

대응:

- 스크립트에서 들여쓰기와 정규식을 수정
- Git source에 새 subnet ID를 반영

### 3. bootstrap 노드 pod slot 부족

증상:

- observability DaemonSet 일부 Pending
- `Too many pods`

대응:

- Karpenter가 새 노드를 띄운 뒤 일부 system workload를 재스케줄
- 실제 복구 시에는 KubeCost Pod 재시작으로 bootstrap 노드 slot 확보

### 4. GitOps self-heal의 양면성

좋은 점:

- source에 반영된 변경은 자동으로 복구됨

주의점:

- source에 없는 임시 수정은 곧 되돌아감

운영 원칙:

- "클러스터에서 먼저 고치고 나중에 Git 반영"은 디버깅까지만
- 최종 해법은 항상 Git source 반영

---

## GitOps 복구 드릴 체크리스트

### 최소 체크

1. `kubectl get applications -n bodybuddy-system` 전체 `Synced Healthy`
2. 앱 4개 `1/1 Running`
3. `kubectl get ingress -n bodybuddy` 에서 ALB address 확인
4. `curl /readyz` 확인

### 권장 체크

1. Karpenter `EC2NodeClass`, `NodePool` 모두 `Ready=True`
2. batch / critical 노드 분리 확인
3. observability, kubecost까지 Healthy

---

## 복구 시연으로 쓸 수 있는 장면

이미 확보했거나, 다음에 다시 찍기 좋은 장면:

1. ArgoCD app list 전체 `Synced Healthy`
2. Karpenter가 critical / batch 노드를 각각 띄우는 장면
3. `kubectl delete deployment` 후 ArgoCD가 다시 맞추는 self-heal 장면
4. 재생성 직후 `Progressing/Degraded` 상태에서 source 반영 후 정상화되는 before/after

---

## 후처리

복구가 끝나면 다음을 기록한다.

1. 어떤 리소스가 drift를 일으켰는지
2. 어떤 수정은 Git source에 반영했는지
3. 어떤 수정은 임시 디버깅 조치였는지
4. 다음 destroy/reapply 때 자동화할 수 있는지
