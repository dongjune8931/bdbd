# Destroy / Reapply 후 수동 복구 체크리스트

> 목적: `terraform destroy` 후 `terraform apply`로 dev 환경을 다시 올렸을 때, **값 드리프트**와 **Terraform 밖 수동 리소스** 때문에 생기는 복구 작업을 빠르게 재현하기 위한 운영 체크리스트.
>
> 범위: `bodybuddy-dev-*` 단일 dev 환경, ap-northeast-2

---

## 1. 왜 이 문서가 필요한가

이 프로젝트는 아직 다음 항목들이 완전히 코드화되어 있지 않다.

1. **앱 Helm values에 인프라 값이 하드코딩**되어 있다.
   - RDS endpoint
   - Redis endpoint
   - SQS queue URL
   - S3 bucket 이름
   - DB 비밀번호

2. **Terraform이 직접 관리하지 않는 Kubernetes/AWS 수동 리소스**가 있다.
   - ArgoCD 설치 자체
   - AWS Load Balancer Controller 설치
   - ALB Controller role trust policy 재연결
   - `aws eks create-access-entry`로 만든 Karpenter node role access entry
   - `observability-kube-prometh-admission` secret

3. **재생성 시 바뀌는 값**이 있다.
   - EKS cluster endpoint
   - EKS OIDC issuer / provider ARN
   - private subnet ID
   - RDS endpoint
   - Redis endpoint
   - RDS managed master password
   - ALB DNS

즉, destroy/reapply 이후에는 “코드는 그대로인데 환경 값이 달라져서 앱이 죽는” 케이스가 반복된다.

---

## 2. destroy/reapply 시 특히 드리프트하는 값

### 2.1 앱 Helm values에 하드코딩된 값

현재 아래 파일들에는 **환경값이 직접 박혀 있다.**

- `/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app/deploy/helm/user-service/values.yaml`
- `/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app/deploy/helm/score-service/values.yaml`
- `/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app/deploy/helm/analysis-worker/values.yaml`
- `/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-app/deploy/helm/notification-worker/values.yaml`

하드코딩되어 있는 대표 키:

- `config.dbHost`
- `config.redisAddr`
- `config.analysisQueueUrl`
- `config.notificationQueueUrl`
- `config.s3Bucket`
- `secrets.dbPassword`

현재 값 예시:

```yaml
config:
  dbHost: bodybuddy-dev-postgres.chqo0a0kqjjr.ap-northeast-2.rds.amazonaws.com
  redisAddr: master.bodybuddy-dev-redis.ebyc4l.apn2.cache.amazonaws.com:6379
  analysisQueueUrl: https://sqs.ap-northeast-2.amazonaws.com/902371998304/bodybuddy-dev-analysis-queue
  notificationQueueUrl: https://sqs.ap-northeast-2.amazonaws.com/902371998304/bodybuddy-dev-notification-queue
  s3Bucket: bodybuddy-dev-inbody

secrets:
  dbPassword: "..."
```

이 중 **queue URL / bucket 이름은 비교적 안정적**이지만, 아래 값은 destroy/reapply 시 다시 확인해야 한다.

- `dbHost`
- `redisAddr`
- `dbPassword`

### 2.2 Karpenter EC2NodeClass의 subnet ID

현재 아래 파일에는 **private subnet ID가 직접 박혀 있다.**

- `/Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/argocd/karpenter/ec2-node-class.yaml`

예시:

```yaml
subnetSelectorTerms:
  - id: subnet-09336f111c4e59ac7
  - id: subnet-0804981ab77907b7b
```

destroy/reapply 후 VPC가 새로 만들어지면 subnet ID는 바뀐다.  
이 값을 안 바꾸면 Karpenter가 새 노드를 못 띄운다.

### 2.3 EKS OIDC 기반 IRSA trust

destroy/reapply 후 바뀌는 값:

- EKS cluster endpoint
- OIDC issuer
- OIDC provider ARN

특히 ALB Controller처럼 **기존 IAM role을 재사용하는 경우**, trust policy가 이전 OIDC를 가리키고 있으면 WebIdentity 인증이 깨진다.

대표 사례:

- `AmazonEKSLoadBalancerControllerRole`

### 2.4 RDS managed master password

RDS가 `manage_master_user_password = true`로 생성되면, destroy/reapply 후 **새 Secrets Manager secret 값**이 생성된다.

즉 다음이 틀어질 수 있다.

- Secrets Manager의 현재 `AWSCURRENT` 비밀번호
- Helm values의 `secrets.dbPassword`
- K8s Secret의 `DB_PASSWORD`
- 실제 running pod env

이 문제는 이번에 앱 4개가 동시에 `/readyz 503`을 냈던 가장 직접적인 원인이었다.

---

## 3. Terraform apply 직후 반드시 다시 확인할 것

먼저 Terraform output을 다시 본다.

```bash
AWS_PROFILE=terraform-bodybuddy terraform -chdir=bodybuddy-infra/terraform/envs/dev output
```

특히 아래 항목을 본다.

- `eks_cluster_name`
- `eks_cluster_endpoint`
- `eks_oidc_provider_arn`
- `private_subnet_ids`
- `rds_endpoint`
- `rds_master_user_secret_arn`
- `elasticache_primary_endpoint_address`
- `analysis_queue_url`
- `notification_queue_url`
- `s3_bucket_name`

그리고 현재 레포 안에서 실제로 고쳐야 할 파일은 다음이다.

| 값 | 반영 파일 |
|---|---|
| `rds_endpoint` | 4개 service `values.yaml`의 `config.dbHost` |
| `elasticache_primary_endpoint_address` | 4개 service `values.yaml`의 `config.redisAddr` |
| `private_subnet_ids` | `bodybuddy-infra/argocd/karpenter/ec2-node-class.yaml` |
| `rds_master_user_secret_arn`의 실제 password | 4개 service `values.yaml`의 `secrets.dbPassword` |

---

## 4. 실제 복구 순서

아래 순서대로 가는 것이 가장 덜 꼬였다.

### 4.1 Terraform apply

```bash
AWS_PROFILE=terraform-bodybuddy terraform -chdir=bodybuddy-infra/terraform/envs/dev apply
```

### 4.2 kubeconfig 갱신

```bash
aws eks update-kubeconfig \
  --name bodybuddy-dev-eks \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

주의:
- 이 세션에서 EKS endpoint DNS가 자주 흔들렸다.
- `kubectl`이 갑자기 `no such host`를 내면 제일 먼저 이 명령부터 다시 친다.

### 4.3 Terraform output으로 재생성 값 확인

```bash
AWS_PROFILE=terraform-bodybuddy terraform -chdir=bodybuddy-infra/terraform/envs/dev output
```

### 4.4 앱 Helm values 값 갱신

반복 수정을 줄이기 위해 먼저 아래 스크립트를 실행한다.

```bash
AWS_PROFILE=terraform-bodybuddy \
  /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/scripts/refresh-dev-values.sh
```

이 스크립트는 Terraform output, RDS Secrets Manager password, ECR 최신 SHA 태그를 읽어서 다음 파일을 갱신한다.

- `bodybuddy-app/deploy/helm/*/values.yaml`
- `bodybuddy-infra/argocd/karpenter/ec2-node-class.yaml`

스크립트 실행 후에는 반드시 diff를 확인한다.

```bash
git diff -- bodybuddy-app/deploy/helm bodybuddy-infra/argocd/karpenter
```

#### 1) RDS 현재 비밀번호 조회

```bash
aws secretsmanager get-secret-value \
  --secret-id <rds_master_user_secret_arn> \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

여기서 `SecretString.password`를 꺼내서 아래 4개 파일의 `secrets.dbPassword`에 반영한다.

- `bodybuddy-app/deploy/helm/user-service/values.yaml`
- `bodybuddy-app/deploy/helm/score-service/values.yaml`
- `bodybuddy-app/deploy/helm/analysis-worker/values.yaml`
- `bodybuddy-app/deploy/helm/notification-worker/values.yaml`

#### 2) RDS / Redis endpoint 반영

Terraform output을 기준으로 아래도 갱신한다.

- `config.dbHost`
- `config.redisAddr`

### 4.5 Karpenter subnet ID 갱신

`private_subnet_ids` 기준으로 아래 파일 수정:

- `bodybuddy-infra/argocd/karpenter/ec2-node-class.yaml`

갱신할 필드:

```yaml
subnetSelectorTerms:
  - id: <private_subnet_1>
  - id: <private_subnet_2>
```

### 4.6 ArgoCD 재설치

반복 설치를 줄이기 위해 아래 스크립트를 우선 사용한다.

```bash
AWS_PROFILE=terraform-bodybuddy \
  /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/scripts/bootstrap-cluster-addons.sh
```

이 스크립트는 다음을 idempotent하게 수행한다.

- kubeconfig 갱신
- `bodybuddy-system`, `karpenter` namespace 생성
- ArgoCD Helm 설치
- Karpenter CRD/controller 설치
- AWS Load Balancer Controller OIDC trust 갱신
- AWS Load Balancer Controller Helm 설치
- ArgoCD project/app-of-apps 적용

destroy 후에는 ArgoCD CRD 자체가 사라지므로 다시 설치해야 한다.

```bash
kubectl create namespace bodybuddy-system
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update
helm upgrade --install argocd argo/argo-cd -n bodybuddy-system
```

그 다음 app-of-apps 재적용:

```bash
kubectl apply -f /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/argocd/apps/project.yaml
kubectl apply -f /Users/idongjun/Desktop/Project/bdbd_prog/bodybuddy-infra/argocd/app-of-apps.yaml
```

### 4.7 Karpenter 재설치 확인

이번 세션에서 확인된 사실:

- 구버전 Karpenter 차트는 `Provisioner` / `AWSNodeTemplate` CRD를 깔아서 현재 repo의 `NodePool` / `EC2NodeClass`와 API가 맞지 않았다.
- Karpenter는 **v1.3.3 + 새 CRD** 조합으로 다시 올려야 정상 동작했다.

확인할 것:

```bash
kubectl get crd | grep -E 'nodepools|nodeclaims|ec2nodeclasses'
kubectl get nodepool
kubectl get ec2nodeclass
```

### 4.8 Karpenter node access entry 확인

destroy/reapply 후 새 Karpenter node는 EC2가 떠도 EKS에 join 못 할 수 있다.  
이번 세션에서는 아래 명령으로 해결했다.

```bash
aws eks create-access-entry \
  --cluster-name bodybuddy-dev-eks \
  --principal-arn arn:aws:iam::902371998304:role/bodybuddy-dev-eks-karpenter-node \
  --type EC2_LINUX \
  --region ap-northeast-2 \
  --profile terraform-bodybuddy
```

이게 코드화돼 있지 않기 때문에, **새 spot/on-demand 노드가 `Ready`가 안 되면 가장 먼저 의심할 포인트**다.

### 4.9 AWS Load Balancer Controller 재설치

destroy 후에는 ALB Controller가 사라지므로 `user-service` ingress는 `ADDRESS`가 빈 채로 남는다.

확인:

```bash
kubectl get ingress -n bodybuddy
kubectl get pods -n kube-system | grep -i load-balancer
```

주의 포인트:

1. controller Helm install 자체
2. `AmazonEKSLoadBalancerControllerRole` trust policy가 **새 OIDC**를 가리키는지
3. service account annotation이 올바른지

증상:

- `user-service`만 `Progressing`
- ingress `ADDRESS` 비어 있음

### 4.10 observability admission secret 재생성

이번 세션에서는 `observability-kube-prometh-admission` secret이 없어서

- `observability-kube-prometh-operator`
- `prometheus`
- `alertmanager`

가 제대로 안 떴다.

즉 destroy/reapply 후 observability가 `Degraded`이면 다음을 의심한다.

- `observability-kube-prometh-admission` secret 존재 여부
- `cert`, `key`, `ca` 세 키가 다 들어 있는지

---

## 5. destroy/reapply 후 자주 보인 증상과 바로 의심할 원인

### 증상 1. 앱 4개가 동시에 `0/1 Running`, `/readyz 503`

가장 먼저 볼 것:

1. Helm values의 `DB_PASSWORD`
2. Secrets Manager의 현재 RDS master password
3. running pod env가 새 값으로 재시작됐는지

이번 세션의 실제 원인:

- destroy/reapply 후 RDS master password 드리프트

### 증상 2. `user-service`만 `Progressing`

가장 먼저 볼 것:

```bash
kubectl get ingress -n bodybuddy
kubectl get pods -n kube-system | grep -i load-balancer
```

실제 원인 후보:

- AWS Load Balancer Controller 미설치
- controller IRSA trust policy가 이전 OIDC를 가리킴

### 증상 3. Karpenter NodeClaim은 생기는데 node가 `Ready`가 안 됨

가장 먼저 볼 것:

- `aws eks create-access-entry`가 적용됐는지
- `EC2NodeClass` subnet ID가 현재 private subnet과 맞는지

### 증상 4. observability만 `Degraded`

가장 먼저 볼 것:

```bash
kubectl get pods -n bodybuddy-system
kubectl describe pod -n bodybuddy-system -l app.kubernetes.io/name=grafana
kubectl describe pod -n bodybuddy-system -l app.kubernetes.io/name=prometheus-operator
```

실제 원인 후보:

- capacity 부족
- `observability-kube-prometh-admission` secret 부재

---

## 6. 현재 구조에서 “다시 바뀔 수밖에 없는 것”

다음 항목은 현재 구조상 destroy/reapply 시 **다시 맞춰야 할 가능성이 높다.**

1. `dbHost`
2. `redisAddr`
3. `dbPassword`
4. `private_subnet_ids -> EC2NodeClass`
5. EKS OIDC -> ALB Controller trust policy
6. ALB DNS

즉 지금 상태에서는 destroy/reapply를 **완전 무인 복구**라고 보면 안 된다.

---

## 7. 나중에 코드로 줄여야 할 반복 작업

이번 문서는 “현재 운영 기준 복구 방법”이다.  
장기적으로는 아래를 코드화하면 destroy/reapply 후 수동 개입이 크게 줄어든다.

1. **DB 비밀번호를 Helm values에서 제거**
   - External Secrets Operator + Secrets Manager로 전환

2. **RDS/Redis/SQS/S3 값을 Helm values에 하드코딩하지 말고 주입 구조로 변경**
   - infra output을 기반으로 values를 생성하거나
   - ArgoCD app values를 infra 레포에서 템플릿화

3. **Karpenter EC2NodeClass에서 subnet ID 직접 박기 제거**
   - 태그 selector 기반으로 변경

4. **ALB Controller 설치와 trust policy 처리 코드화**
   - Terraform + ArgoCD/Helm으로 일관화

5. **Karpenter node access entry 코드화**
   - `aws_eks_access_entry` 리소스로 옮기기

6. **observability admission secret 원인 제거**
   - chart values 정합성 재검토 또는 secret lifecycle 코드화

---

## 8. 추천 destroy 전/후 운영 순서

### destroy 전

1. ALB가 아직 살아 있으면 먼저 확인
2. 필요한 값/스크린샷/리포트 백업
3. 현재 `terraform output` 저장
4. 필요하면 `kubectl get applications -A`, `kubectl get nodes`, `kubectl get ingress -A` 캡처

### reapply 후

1. `terraform apply`
2. `aws eks update-kubeconfig`
3. `terraform output` 재확인
4. Helm values drift 수정
5. ArgoCD 재설치
6. app-of-apps 재적용
7. Karpenter / node access entry 확인
8. ALB Controller 재설치 확인
9. observability admission secret 확인
10. 앱 readiness / ingress / ArgoCD health 검증

---

## 9. 결론

현재 이 프로젝트에서 destroy/reapply 후 수동 개입이 반복되는 이유는 두 가지다.

1. **재생성 시 바뀌는 값이 app/argocd 레벨에 하드코딩돼 있다.**
2. **일부 운영 컴포넌트가 Terraform/ArgoCD로 완전히 선언되지 않았다.**

따라서 다음에 “환경 다시 올려줘”를 시킬 때는 이 문서를 기준으로 보면 된다.

가장 먼저 체크할 핵심 4개만 다시 요약하면:

1. `DB_PASSWORD` 드리프트
2. `dbHost` / `redisAddr` 드리프트
3. `EC2NodeClass` subnet ID 드리프트
4. ALB Controller / OIDC trust / access entry 같은 Terraform 밖 수동 리소스
