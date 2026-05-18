# 04. GitOps (ArgoCD) — Git이 클러스터 상태의 진실

## 개요

이전에는 `helm upgrade --install`로 직접 배포했다면, 04번 작업에서는 **ArgoCD**가 모든 배포를 담당한다. 직접 `kubectl apply`나 `helm upgrade`를 사용하는 것을 중단한다.

04번 작업 완료 상태:
- ArgoCD UI에서 4개 서비스가 모두 Synced & Healthy
- Git에 변경사항 push → 5분 내 자동 배포
- `kubectl delete deployment user-service` → 1~2분 내 ArgoCD가 자동 복원

---

## 1. GitOps란 무엇인가?

### 1.1 전통적 배포 vs GitOps

**전통적 배포:**
```
개발자 → kubectl apply -f deployment.yaml → 클러스터
         또는
개발자 → helm upgrade ... → 클러스터
```
문제:
- 클러스터의 실제 상태와 Git 코드가 다를 수 있음 (누가 kubectl로 직접 수정했다면?)
- 배포 이력이 Git 커밋 이력과 분리됨
- 재해 복구 시 "어떤 상태로 복구해야 하나?"가 불명확

**GitOps:**
```
개발자 → Git push → ArgoCD가 감지 → 클러스터에 자동 sync
```
원칙:
- **Git이 Single Source of Truth**: 클러스터 상태는 항상 Git의 매니페스트와 일치해야 함
- **선언적(Declarative)**: "이 상태이어야 한다"고 선언. ArgoCD가 알아서 맞춤.
- **감사 가능**: 모든 변경이 Git 커밋으로 추적됨

**📌 개념 설명: ArgoCD의 self-heal**
`selfHeal: true` 설정 시, 누군가 kubectl로 직접 Deployment를 수정하면 ArgoCD가 Git 상태로 되돌린다. 이것이 "Git이 진실"이라는 원칙의 구현이다.

---

## 2. App-of-Apps 패턴

### 2.1 패턴이란?

**📌 개념 설명: App-of-Apps**
ArgoCD Application이 다른 ArgoCD Application들을 생성하는 계층 구조다.

```
[app-of-apps.yaml]
    │
    ├── [ArgoCD Application: user-service]     → deploy/helm/user-service/
    ├── [ArgoCD Application: score-service]    → deploy/helm/score-service/
    ├── [ArgoCD Application: analysis-worker]  → deploy/helm/analysis-worker/
    └── [ArgoCD Application: notification-worker] → deploy/helm/notification-worker/
```

이 패턴의 장점:
- 새 서비스 추가 시 `apps/` 디렉토리에 Application 파일 하나만 추가하면 됨
- `app-of-apps.yaml` 하나만 ArgoCD에 등록하면 모든 앱이 관리됨
- 환경별 앱 그룹(dev/staging/prod)을 App-of-Apps로 분리 가능

### 2.2 디렉토리 구조

```
bodybuddy-infra/argocd/
├── app-of-apps.yaml          # ArgoCD에 직접 등록하는 루트
└── apps/
    ├── user-service.yaml
    ├── score-service.yaml
    ├── analysis-worker.yaml
    └── notification-worker.yaml
```

### 2.3 app-of-apps.yaml

```yaml
# bodybuddy-infra/argocd/app-of-apps.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: bodybuddy-apps
  namespace: argocd
spec:
  project: bodybuddy
  source:
    repoURL: https://github.com/username/bodybuddy-infra
    targetRevision: main
    path: argocd/apps          # 이 디렉토리의 모든 yaml을 Application으로 처리
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true     # Git에 없는 Application 자동 삭제
      selfHeal: true  # 직접 수정된 Application을 Git 상태로 복원
```

### 2.4 apps/user-service.yaml

```yaml
# bodybuddy-infra/argocd/apps/user-service.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: user-service
  namespace: argocd
spec:
  project: bodybuddy
  source:
    repoURL: https://github.com/username/bodybuddy-app
    targetRevision: main
    path: deploy/helm/user-service   # Helm chart 경로
    helm:
      valueFiles:
        - values.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: bodybuddy
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

---

## 3. ArgoCD 설치

```bash
# ArgoCD 설치
kubectl create namespace argocd
kubectl apply -n argocd \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# 초기 admin 비밀번호 확인
kubectl get secret argocd-initial-admin-secret -n argocd \
  -o jsonpath="{.data.password}" | base64 -d

# 포트포워딩으로 UI 접근
kubectl port-forward svc/argocd-server -n argocd 8080:443
```

또는 Helm으로 설치 (관측성 작업에서 ArgoCD 자체를 ArgoCD로 관리하는 패턴):
```bash
helm repo add argo https://argoproj.github.io/argo-helm
helm install argocd argo/argo-cd -n argocd --create-namespace
```

### 3.1 app-of-apps 등록

```bash
# 루트 Application 등록 (이 한 번으로 모든 앱 관리 시작)
kubectl apply -f bodybuddy-infra/argocd/app-of-apps.yaml
```

등록 후 ArgoCD가 `argocd/apps/` 디렉토리를 스캔하여 4개 Application을 자동 생성한다.

---

## 4. 트러블슈팅: Image Tag 불일치

**🔥 트러블슈팅 1: Git values.yaml 태그와 실제 배포 태그 불일치**

**증상:**
```
ArgoCD UI: user-service → OutOfSync
이유: Git의 values.yaml에 image.tag=72d3705 인데
     클러스터에 배포된 이미지는 5f6282c
```

**원인:**
이전 수동 배포에서 `helm upgrade --install --set image.tag=5f6282c`로 직접 배포했다. 이 값은 Git의 `values.yaml`에 반영되지 않았다. ArgoCD는 Git을 기준으로 하기 때문에 "Git에는 72d3705인데 클러스터에는 5f6282c가 있다" → OutOfSync로 판단.

**해결:**
```yaml
# deploy/helm/user-service/values.yaml
image:
  tag: "5f6282c"  # 실제 ECR에 있는 최신 이미지 sha로 업데이트
```

Git에 커밋/push → ArgoCD가 Synced로 변경.

**배운 점:** GitOps 전환 후에는 **절대로** 직접 `helm upgrade`나 `kubectl apply`로 배포하면 안 된다. 모든 변경은 Git을 통해야 한다. 이전에 직접 배포한 상태를 Git에 먼저 맞춰야 한다.

---

## 5. 트러블슈팅: ALB Controller 재설치 문제

**🔥 트러블슈팅 2: terraform destroy 후 ALB Controller IRSA Trust Policy 구 OIDC 참조**

**증상:**
```
terraform destroy 후 재apply했더니
ALB Controller Pod이 AWS API 호출 시 에러:
"An error occurred (AccessDenied) when calling the DescribeLoadBalancers operation"
```

**원인:**
`terraform destroy`하면 EKS 클러스터가 삭제된다. EKS 클러스터가 다시 생성되면 **OIDC Provider URL이 바뀐다**.

```
이전 OIDC: oidc.eks.ap-northeast-2.amazonaws.com/id/AAAA1111
새 OIDC:   oidc.eks.ap-northeast-2.amazonaws.com/id/BBBB2222
```

ALB Controller의 IAM Role Trust Policy는 이전 OIDC Provider를 신뢰하도록 설정되어 있었다. 새 OIDC Provider를 신뢰하도록 업데이트하지 않으면 IRSA가 동작하지 않는다.

**해결:**
```bash
# 1. 새 EKS 클러스터의 OIDC Provider ARN 확인
aws eks describe-cluster --name bodybuddy-dev \
  --query "cluster.identity.oidc.issuer" --output text

# 2. IAM Role Trust Policy 업데이트
aws iam update-assume-role-policy \
  --role-name AmazonEKSLoadBalancerControllerRole \
  --policy-document file://new-trust-policy.json

# 3. ALB Controller 재설치
helm upgrade aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=bodybuddy-dev \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller
```

**또는 Terraform으로 관리:**
```hcl
# modules/eks/main.tf
# OIDC Provider ARN을 output으로 노출
output "oidc_provider_arn" {
  value = module.eks.oidc_provider_arn
}

# ALB Controller IRSA도 Terraform으로 관리하면
# destroy/reapply 시 자동으로 새 OIDC ARN으로 업데이트됨
```

**배운 점:** destroy/reapply 시 EKS OIDC ID가 바뀐다. IRSA를 사용하는 모든 IAM Role Trust Policy를 Terraform으로 관리하면 이 문제가 자동으로 해결된다. 수동으로 만든 IAM Role은 destroy/reapply 시 수동으로 업데이트해야 한다.

---

## 6. ArgoCD Self-Heal 시연

### 6.1 시연 시나리오

```bash
# 1. 현재 상태 확인 (Synced & Healthy)
argocd app get user-service
# Status: Synced, Health: Healthy, Replicas: 1

# 2. 강제로 Deployment 삭제
kubectl delete deployment user-service -n bodybuddy

# 3. 즉시 확인 → Pod 없음
kubectl get pods -n bodybuddy -l app.kubernetes.io/name=user-service
# No resources found

# 4. 1~2분 후 확인 → ArgoCD가 자동 복원
kubectl get pods -n bodybuddy -l app.kubernetes.io/name=user-service
# NAME                            READY   STATUS    RESTARTS
# user-service-7d9b8c6f4-xxxxx   1/1     Running   0
```

### 6.2 ArgoCD 동작 원리

```
kubectl delete deployment user-service
    ↓
ArgoCD가 클러스터 상태 변화 감지 (캐시 갱신 주기: 기본 3분, 강제 sync 가능)
    ↓
Git 상태: Deployment가 있어야 함
클러스터 상태: Deployment 없음
    ↓
selfHeal: true → 자동으로 sync 실행
    ↓
Deployment 재생성 → Pod 스케줄링 → Running
```

이 시연이 면접에서 "GitOps로 무엇을 얻었나?"의 답변이다:
> "kubectl delete deployment로 배포를 강제로 삭제했는데 1분 30초 만에 자동 복원됐습니다. 클러스터 상태가 항상 Git 상태와 일치하도록 보장됩니다."

---

## 7. ArgoCD Project 설정

### 7.1 bodybuddy project 생성

```yaml
# bodybuddy-infra/argocd/project.yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: bodybuddy
  namespace: argocd
spec:
  description: "BodyBuddy 애플리케이션"
  sourceRepos:
    - "https://github.com/username/bodybuddy-app"
    - "https://github.com/username/bodybuddy-infra"
  destinations:
    - namespace: bodybuddy
      server: https://kubernetes.default.svc
    - namespace: bodybuddy-system
      server: https://kubernetes.default.svc
  clusterResourceWhitelist:
    - group: ""
      kind: Namespace
```

### 7.2 외부 Helm Chart의 project 설정

**🔥 트러블슈팅 3: 외부 Helm chart는 project 제한에 걸림**

**증상:**
```
KubeCost, Tempo 등 외부 Helm chart를 ArgoCD Application으로 추가하려 하면:
"application references project bodybuddy which does not permit this repository"
```

**원인:**
`bodybuddy` project의 `sourceRepos`에는 bodybuddy GitHub 레포만 허용되어 있다. 외부 Helm chart 저장소 URL은 허용 목록에 없다.

**해결 방법 1 (권장):** 외부 도구는 `project: default` 사용

```yaml
# bodybuddy-infra/argocd/apps/kubecost.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: kubecost
  namespace: argocd
spec:
  project: default    # default project는 모든 레포 허용
  source:
    repoURL: https://kubecost.github.io/cost-analyzer/
    chart: cost-analyzer
    targetRevision: 2.6.5
  destination:
    server: https://kubernetes.default.svc
    namespace: bodybuddy-system
```

**해결 방법 2:** bodybuddy project의 sourceRepos에 외부 레포 추가

```yaml
sourceRepos:
  - "https://github.com/username/bodybuddy-app"
  - "https://kubecost.github.io/cost-analyzer/"
  - "https://grafana.github.io/helm-charts"
  - "*"  # 모든 레포 허용 (보안 약화)
```

`project: default` 방식을 선택한 이유: bodybuddy project에 외부 레포를 추가하면 project의 의미(우리 앱만 관리)가 희석된다. 인프라 도구(KubeCost, Prometheus 등)는 별도 project(또는 default)로 관리하는 것이 깔끔하다.

---

## 8. 이미지 업데이터 전략

### 8.1 자동 업데이터 (ArgoCD Image Updater)

ArgoCD Image Updater는 ECR의 새 이미지 태그를 감지하여 자동으로 values.yaml을 업데이트한다.

```yaml
# 설치 시
metadata:
  annotations:
    argocd-image-updater.argoproj.io/image-list: myapp=902371998304.dkr.ecr.../user-service
    argocd-image-updater.argoproj.io/myapp.update-strategy: latest
```

장점: 완전 자동화, 빠른 배포
단점: 어떤 이미지가 언제 배포됐는지 Git 이력이 없음. 롤백 시 어떤 태그로 돌아가야 하는지 찾기 어려움.

### 8.2 수동 PR 방식 (선택)

```bash
# GitHub Actions ci.yaml에서 빌드 후
# bodybuddy-infra 레포의 values.yaml을 자동으로 PR 생성
SHA=$(git rev-parse --short HEAD)
sed -i "s/tag:.*/tag: ${SHA}/" deploy/helm/user-service/values.yaml
git commit -m "chore: update user-service image to ${SHA}"
git push
# PR 생성 → 리뷰 → 머지 → ArgoCD 자동 sync
```

**수동 PR 방식을 선택한 이유:**
1. **감사 추적**: "언제 어떤 이미지를 배포했나"가 Git 커밋 이력으로 명확히 남음
2. **롤백 단순성**: `git revert <commit>`으로 이전 values.yaml로 되돌리면 끝
3. **면접 talking point**: "자동 업데이트 안 한 이유가 뭔가요?" → "변경 가시성과 롤백 단순성 때문입니다"
4. **human-in-the-loop**: 이미지가 자동으로 프로덕션에 배포되는 것보다 검토 단계가 있는 편이 안전함

---

## 9. destroy/reapply 후 복구 체크리스트

terraform destroy → terraform apply 후 ArgoCD를 다시 쓸 수 있게 되기까지의 단계:

```bash
# 1. kubeconfig 업데이트 (새 클러스터로)
aws eks update-kubeconfig --name bodybuddy-dev --region ap-northeast-2

# 2. kubectl 연결 확인
kubectl get nodes

# 3. ArgoCD 재설치 (새 클러스터에)
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# 4. ArgoCD Pod 준비 대기
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=argocd-server -n argocd --timeout=300s

# 5. app-of-apps 등록
kubectl apply -f bodybuddy-infra/argocd/app-of-apps.yaml

# 6. IRSA Trust Policy 업데이트 (OIDC가 바뀌었으므로)
# ALB Controller, KubeCost, 기타 IRSA 사용 컴포넌트

# 7. ArgoCD UI에서 sync 확인
argocd app sync bodybuddy-apps
argocd app list
```

이 체크리스트 자체가 "GitOps로 클러스터를 처음부터 재구성할 수 있다"는 것을 보여준다. 클러스터가 완전히 날아가도 Git의 매니페스트로 복구할 수 있다.

---

## 핵심 요약

- **GitOps 핵심 원칙**: Git이 클러스터 상태의 Single Source of Truth. 직접 kubectl/helm으로 배포하면 안 됨.
- **App-of-Apps 패턴**: 루트 Application 하나로 모든 서비스 Application을 관리. 새 서비스 추가 시 apps/ 디렉토리에 yaml 파일 하나만 추가.
- **prune + selfHeal**: Git에서 삭제된 리소스를 클러스터에서도 삭제(prune), 직접 수정된 클러스터 상태를 Git으로 복원(selfHeal).
- **OIDC 변경 주의**: terraform destroy/reapply 시 EKS OIDC Provider ID가 바뀜. IRSA Trust Policy를 Terraform으로 관리해야 자동 업데이트.
- **project 설정**: 외부 Helm chart(KubeCost, Tempo 등)는 `project: default` 사용. bodybuddy project는 우리 레포만 허용하여 명확한 범위 유지.
- **수동 PR 이미지 업데이트**: 자동화(Image Updater)보다 감사 추적과 롤백 단순성을 선택. 면접 질문에 명확히 답할 수 있는 결정.
- **self-heal 시연**: `kubectl delete deployment` → 1~2분 내 자동 복원. 이것이 GitOps의 가장 직관적인 데모.
