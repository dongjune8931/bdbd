# Terraform Portfolio Notes

## 1. 문서 목적

이 문서는 `bodybuddy-infra`의 Terraform 설계를 포트폴리오와 면접 준비 관점에서 정리한 문서다.  
단순히 "무엇을 만들었는가"만 적는 문서가 아니라, 아래 질문에 답할 수 있도록 만드는 것이 목적이다.

- 왜 Terraform으로 분리했는가?
- 왜 이 모듈 구조를 선택했는가?
- 왜 EKS + Karpenter + RDS + Redis + S3 + SQS 조합인가?
- 왜 이런 비용/보안/운영 트레이드오프를 택했는가?
- 실제로 어디까지 검증했는가?

이 문서는 이후 면접 직전 복습 자료, 블로그 초안, README 보강 자료로도 재사용할 수 있게 작성한다.

---

## 2. 현재 상태 요약

### 2.1 현재 구현 범위

현재 `bodybuddy-infra`에는 다음 Terraform 구성 요소가 구현되어 있다.

- `envs/dev`
- `modules/vpc`
- `modules/eks`
- `modules/karpenter`
- `modules/rds`
- `modules/elasticache`
- `modules/s3`
- `modules/sqs`
- `modules/ecr`
- `modules/iam-irsa` 골격

### 2.2 현재까지 끝난 검증

다음 검증은 이미 수행했다.

- `terraform init -backend=false`
- `terraform validate`
- 실제 AWS backend 준비
- 실제 remote backend 기준 `terraform init`
- 실제 AWS 계정 기준 `terraform plan`
- 실제 AWS 계정 기준 `terraform apply`
- `kubectl get nodes`
- `kubectl get pods -A`
- RDS 상태 확인
- ElastiCache 상태 확인

### 2.3 현재 시점의 정확한 상태

현재는 **설계와 코드 작성 단계를 넘어서, 실제 AWS에 dev 인프라를 생성하고 핵심 상태까지 확인한 상태**다.  
즉, 아이디어 수준이나 `plan` 검증 수준이 아니라 **실제 EKS + PostgreSQL + Redis 기반 인프라가 떠 있는 상태**까지 왔다.

실제 `plan` 결과 기준:

- 실행 일시 기준: `2026-05-12`
- 결과: `Plan: 106 to add, 0 to change, 0 to destroy`

이는 적어도 다음 의미를 가진다.

- 모듈 간 참조 구조가 실제 AWS provider 기준으로 해석된다
- backend와 state locking이 정상 동작한다
- 네이밍/태그/리소스 wiring이 유효하다
- EKS, VPC, RDS, Redis, S3, SQS, ECR, Karpenter가 한 스택으로 합쳐진다

실제 `apply` 및 상태 확인 기준 추가 사실:

- EKS 클러스터 생성 성공
- bootstrap managed node group에서 노드 1개 `Ready`
- `kube-system`의 `aws-node`, `kube-proxy`, `coredns` 정상 `Running`
- RDS PostgreSQL 인스턴스 `available`
- ElastiCache Redis replication group `available`

즉, 현재 Terraform은 "생성 가능한 코드"가 아니라 **생성과 최소 운영 상태까지 확인된 코드**다.

---

## 3. 프로젝트 맥락과 Terraform의 역할

### 3.1 이 프로젝트의 진짜 목표

이 프로젝트의 진짜 목표는 앱 기능 완성이 아니라 **클라우드/인프라 엔지니어링 역량을 보여주는 것**이다.

따라서 Terraform은 단순한 "리소스 생성 도구"가 아니라 아래 역량을 드러내는 핵심 수단이다.

- 인프라를 코드로 설계하는 능력
- 모듈 경계를 나누는 능력
- 상태(State)를 운영하는 능력
- 비용과 운영 복잡도를 균형 있게 통제하는 능력
- dev 환경과 미래 확장 방향을 동시에 고려하는 능력

### 3.2 왜 Terraform을 썼는가

Terraform을 사용한 이유는 다음과 같다.

1. AWS 인프라를 선언적으로 관리하기 쉽다.
2. EKS, VPC, RDS, SQS, S3 등 AWS 자원을 한 흐름으로 연결하기 좋다.
3. 변경 이력과 리뷰가 가능하다.
4. `plan`과 `apply`를 분리해서 실수 방지를 할 수 있다.
5. 사이드 프로젝트에서도 "운영형 인프라 프로세스"를 보여주기 좋다.

즉, 이 프로젝트에서 Terraform은 편의 도구가 아니라 **포트폴리오 메시지의 일부**다.

---

## 4. 왜 레포를 분리했는가

현재 구조는 아래 두 레포로 나뉜다.

- `bodybuddy-app`
- `bodybuddy-infra`

이 분리는 단순 취향이 아니라 운영 관점의 분리다.

### 4.1 분리 이유

1. 애플리케이션과 인프라는 변경 주기가 다르다.
2. 앱 PR과 인프라 PR의 리뷰 기준이 다르다.
3. ArgoCD/GitOps를 도입할 때 인프라 레포가 배포 기준점이 되기 좋다.
4. "코드 저장소"와 "플랫폼 정의 저장소"를 나누면 설명이 명확해진다.

### 4.2 면접에서 말할 포인트

"앱과 인프라를 분리한 이유는 단순히 보기 좋게 하기 위해서가 아니라, 변경 lifecycle과 리뷰 관점을 분리하기 위해서였습니다. 특히 ArgoCD 기반 GitOps를 염두에 두고 infra repo를 배포 source of truth로 두는 구성을 택했습니다."

---

## 5. Terraform 디렉토리 구조 설계

현재 Terraform 구조는 다음 철학을 따른다.

```text
terraform/
├── envs/dev
├── modules/vpc
├── modules/eks
├── modules/karpenter
├── modules/rds
├── modules/elasticache
├── modules/s3
├── modules/sqs
├── modules/ecr
└── shared
```

### 5.1 `envs/dev`를 둔 이유

`envs/dev`는 실제 환경 조립 지점이다.

- 모듈 구현과 환경별 값 주입을 분리할 수 있다
- 이후 `staging`, `prod`를 추가할 때 재사용성이 좋아진다
- dev 전용 비용 절감 설정을 모듈 코드와 분리할 수 있다

즉, 모듈은 "무엇을 만들지", env는 "어떻게 조합할지"를 담당한다.

### 5.2 `modules/*`로 나눈 이유

모듈 경계는 AWS 서비스별로 나눴다.

- `vpc`: 네트워크 기반
- `eks`: 클러스터
- `karpenter`: 노드 자동화
- `rds`: 트랜잭션 저장소
- `elasticache`: 랭킹/캐시
- `s3`: 이미지 저장
- `sqs`: 비동기 메시징
- `ecr`: 서비스 이미지 저장소

이렇게 나눈 이유는 재사용성만이 아니라 **책임 경계를 선명하게 하기 위해서**다.

예를 들어:

- VPC 변경은 ECR과 무관하다
- Karpenter 정책은 Redis 모듈과 무관하다
- SQS 설계는 EKS 생성 로직과 직접 섞일 필요가 없다

즉, "AWS 서비스별 모듈 분리"는 이 프로젝트에서 가장 설명력이 좋은 방식이다.

---

## 6. Backend(State) 설계

### 6.1 왜 remote backend가 필요한가

Terraform에서 state는 인프라의 현재 상태를 저장하는 핵심 데이터다.  
로컬 `terraform.tfstate`만 사용하면 다음 문제가 있다.

- 노트북 분실/변경 시 상태 유실
- 협업 시 충돌 가능
- 잘못된 동시 apply 위험
- 상태 복구 및 이력 관리 취약

따라서 remote backend를 먼저 설계했다.

### 6.2 현재 backend 구성

- S3 bucket: `bodybuddy-tfstate-902371998304`
- DynamoDB table: `bodybuddy-tflock`

### 6.3 왜 S3 + DynamoDB 조합인가

이 조합은 AWS에서 Terraform state 운영의 가장 표준적인 패턴 중 하나다.

- S3: state 파일 저장
- DynamoDB: lock 관리

즉, 여러 실행자가 동시에 `apply`를 시도해도 lock으로 보호할 수 있다.

### 6.4 왜 bootstrap은 수동인가

backend 자신을 backend로 만들 수는 없다.  
따라서 state bucket과 lock table은 **초기 1회 수동 생성**이 필요하다.

이 부분은 오히려 포트폴리오 포인트가 된다.

면접에서 말할 수 있는 핵심은 이거다.

"Terraform으로 모든 걸 만들 수는 있지만, Terraform state를 저장할 backend는 그보다 먼저 존재해야 해서 bootstrap 자원은 의도적으로 수동 1회 생성하는 패턴을 택했습니다."

### 6.5 현재 백엔드 품질 평가

현재 기준으로 backend는 잘 구성되어 있다.

- 실제 AWS 계정에서 생성 완료
- versioning 활성화
- server-side encryption 활성화
- 실제 `terraform init` 및 `plan`에 사용됨

엄밀히 말하면 스펙상 `SSE-KMS`가 가장 정석이지만, 현재는 `AES256`으로 충분히 실용적인 수준이다.  
이후 강화 포인트로 "backend encryption을 KMS로 상향"을 말할 수 있다.

---

## 7. 네트워크 설계

### 7.1 현재 VPC 설계

현재 dev 환경 기본값은 다음과 같다.

- Region: `ap-northeast-2`
- VPC CIDR: `10.20.0.0/16`
- Public subnets: `10.20.0.0/24`, `10.20.1.0/24`
- Private subnets: `10.20.10.0/24`, `10.20.11.0/24`
- AZ: `ap-northeast-2a`, `ap-northeast-2c`

### 7.2 왜 public/private subnet을 나눴는가

이 프로젝트는 "모든 워크로드는 private subnet, 외부 진입점은 제한"이라는 설계를 따르고 있다.

이 분리의 이유는 다음과 같다.

1. EKS worker, RDS, Redis를 외부에 직접 노출하지 않기 위해
2. ALB 같은 진입점만 public에 두기 위해
3. 보안 경계를 네트워크 레벨에서도 분리하기 위해
4. 이후 IRSA, NetworkPolicy, private endpoint 흐름과 잘 맞추기 위해

### 7.3 왜 S3 Gateway Endpoint를 넣었는가

VPC 모듈에는 S3 Gateway Endpoint를 추가했다.

이 선택의 이유는 매우 중요하다.

1. 워커가 S3에 접근할 때 NAT를 통한 불필요한 비용을 줄일 수 있다.
2. S3는 이 프로젝트에서 업로드 이미지 저장소라 접근 빈도가 높다.
3. "비용 최적화를 의식한 네트워크 설계"라는 메시지를 줄 수 있다.

이건 단순 튜닝이 아니라 설계 포인트다.

면접에서 말할 포인트:

"이미지 업로드 워크플로가 S3 중심이라, NAT 경유 비용을 줄이기 위해 S3 Gateway VPC Endpoint를 초기에 포함시켰습니다."

### 7.4 왜 single NAT인가

dev 환경에서는 `single_nat_gateway = true`다.

이건 가용성보다 비용을 우선한 선택이다.

- 장점: 비용 절감
- 단점: NAT 단일 장애점

하지만 이 프로젝트의 dev 환경 목표는 24/7 production HA가 아니라, **학습과 시연용 운영 환경**이다.  
따라서 이 선택은 현실적이다.

---

## 8. EKS 설계

### 8.1 왜 EKS를 선택했는가

이 프로젝트는 단순 EC2 배포보다 EKS 자체가 중요한 포트폴리오 포인트다.

이유는 다음과 같다.

1. MSA 운영이라는 주제를 보여주기 좋다.
2. GitOps, IRSA, Karpenter, HPA 같은 운영 패턴과 자연스럽게 연결된다.
3. "앱을 올렸다"보다 "플랫폼을 설계했다"는 메시지가 강해진다.

### 8.2 현재 EKS 모듈의 핵심 설계

현재 EKS 모듈에는 다음이 들어간다.

- EKS cluster
- IRSA 활성화
- control plane log 일부 활성화
- EKS addon: `coredns`, `kube-proxy`, `vpc-cni`
- bootstrap managed node group

### 8.3 왜 bootstrap managed node group을 두었는가

Karpenter를 쓰더라도 초기에는 최소 1개의 node가 필요하다.  
그 이유는 Karpenter controller 자체도 pod로 올라가야 하기 때문이다.

즉, bootstrap node group은 "영구 운영용 노드"가 아니라 **클러스터 초기 자립을 위한 최소 부트스트랩 장치**다.

현재 설정:

- instance type: `t3.medium`
- desired: `1`
- min: `1`
- max: `2`
- capacity type: `ON_DEMAND`

왜 이렇게 했는가:

1. 컨트롤러 컴포넌트가 안정적으로 먼저 떠야 한다
2. 초기에는 spot 의존보다 안정성이 중요하다
3. dev 비용을 감안해 최소 크기로 유지한다

### 8.4 왜 public/private endpoint를 둘 다 열었는가

현재 dev는:

- public access: `true`
- private access: `true`

이건 dev 편의성과 운영 방향의 균형이다.

- public access를 열면 로컬에서 바로 접근하기 쉽다
- private access도 같이 켜두면 이후 운영 패턴과 단절되지 않는다

즉, production-hardening보다 **개발 생산성과 학습 흐름**을 우선한 설정이다.

---

## 9. Karpenter 설계

### 9.1 왜 Cluster Autoscaler가 아니라 Karpenter인가

스펙상 명시적으로 Karpenter를 사용한다.  
이는 단순 도구 취향이 아니라 프로젝트의 핵심 학습 목표와 연결된다.

이유:

1. Spot 활용에 유리하다
2. 노드 provisioning이 더 유연하다
3. 비용 최적화 이야기와 연결하기 좋다
4. "EKS 운영" 포인트를 더 강하게 만든다

### 9.2 현재 Karpenter 모듈에서 만든 것

현재 Terraform은 Karpenter 자체 Helm 설치까지는 아직 아니지만, **AWS 측 선행 자원**은 설계했다.

- controller IAM role
- controller policy
- node IAM role
- node instance profile
- interruption queue
- EventBridge rule/target

즉, Karpenter가 AWS 자원을 안전하게 다루기 위한 기반을 먼저 구축한 상태다.

### 9.3 왜 interruption queue를 만들었는가

Spot은 싸지만 중단될 수 있다.  
따라서 Spot을 쓰려면 interruption event를 처리하는 경로를 먼저 가져가야 한다.

지금 설계는 다음 이벤트를 Karpenter queue로 보낸다.

- Spot interruption warning
- Instance rebalance recommendation
- Instance state change
- AWS Health event

이건 단순 자동화가 아니라, 나중에 Phase 6에서 할 시연의 기반이다.

면접 포인트:

"Spot을 단순히 싸서 쓰는 게 아니라, interruption event까지 인지하도록 선행 자원을 Terraform으로 먼저 정의했습니다."

### 9.4 왜 node role을 별도로 만들었는가

Karpenter가 띄운 노드는 결국 EKS worker node 역할을 해야 한다.  
따라서 아래 권한이 필요하다.

- `AmazonEKSWorkerNodePolicy`
- `AmazonEKS_CNI_Policy`
- `AmazonEC2ContainerRegistryReadOnly`
- `AmazonSSMManagedInstanceCore`

이 구분은 "Karpenter controller 권한"과 "Karpenter가 띄운 노드 권한"을 분리한 것으로, 최소 권한과 책임 분리에 부합한다.

---

## 10. 데이터 계층 설계

### 10.1 왜 RDS와 Redis를 둘 다 썼는가

이 프로젝트는 "랭킹 + 캐릭터 상태 + 히스토리"를 가진다.  
따라서 영속성과 빠른 조회를 분리하는 것이 설계상 자연스럽다.

- RDS PostgreSQL: 시스템의 source of truth
- Redis: 랭킹과 캐시

이 분리는 도메인 요구와 운영 요구가 잘 만나는 지점이다.

### 10.2 RDS 설계

현재 RDS 기본값:

- engine: PostgreSQL
- engine version: `16`
- class: `db.t4g.micro`
- storage: `gp3`
- allocated: `20GiB`
- max autoscaled: `100GiB`
- backup retention: `7`
- multi-AZ: `false` in dev
- deletion protection: `false` in dev
- password: `manage_master_user_password = true`

#### 왜 이런 선택을 했는가

1. `db.t4g.micro`
   - dev 비용 통제를 위해 작은 인스턴스를 선택
   - Graviton 계열로 가성비를 노림

2. `manage_master_user_password = true`
   - DB 비밀번호를 코드나 수동 메모에 두지 않기 위해
   - AWS Secrets Manager 연동 포인트를 만든다

3. `multi_az = false`
   - dev 비용을 우선
   - DR drill 시점에만 Multi-AZ 또는 복구 중심 전략을 강조

4. `publicly_accessible = false`
   - DB는 외부에 직접 노출하지 않는 것이 원칙

#### 네트워크 관점

RDS는 subnet group + SG 조합으로 private 접근만 허용한다.

- VPC CIDR에서 접근 허용
- EKS cluster security group 기반 접근도 추가

이 구조는 추후 app pod나 bastion/ephemeral pod 기반 migration 흐름으로 자연스럽게 이어진다.

### 10.3 Redis 설계

현재 Redis 기본값:

- engine: Redis OSS
- version: `7.1`
- node type: `cache.t4g.micro`
- num_cache_clusters: `1`
- automatic_failover: `false`
- multi_az: `false`
- at-rest encryption: `true`
- transit encryption: `true`

#### 왜 이렇게 했는가

1. dev 환경에서는 단일 노드로 충분하다
2. 랭킹/캐시라는 특성상 RDS보다 낮은 HA 수준을 허용할 수 있다
3. 그러나 암호화는 dev라도 유지해 "보안 관점"을 놓치지 않았다

즉, 비용은 절약하되 보안과 구조는 production-friendly한 방향을 유지했다.

---

## 11. 스토리지 설계

### 11.1 S3의 역할

S3는 인바디 원본 이미지 저장소다.  
이 프로젝트에서는 업로드 워크플로의 시작점이자, 이후 DR 시연의 핵심 대상이기도 하다.

### 11.2 현재 S3 설계

현재 모듈은 아래를 포함한다.

- bucket 생성
- versioning 활성화
- object lock 활성화
- governance 모드 기본 보존
- server-side encryption 활성화

### 11.3 왜 object lock까지 넣었는가

이건 단순 저장 버킷이 아니라 **나중에 S3 대량 삭제 복구 시나리오**까지 고려한 설계다.

즉, S3는 단순 파일 저장이 아니라:

- 업로드 시작점
- 이벤트 소스
- DR 시연 대상

세 가지 역할을 동시에 가진다.

### 11.4 KMS 대신 현재 AES256을 쓰는 이유

스펙상 이상형은 SSE-KMS다.  
현재는 `kms_key_arn`이 없으면 `AES256`으로 암호화한다.

이건 다음 트레이드오프다.

- 장점: 초기 복잡도 감소
- 단점: 스펙 최종형과는 차이

즉, 현재는 "단계적 구현" 관점에서 설계했고, 이후 KMS key 모듈을 붙이며 강화할 수 있다.

---

## 12. 비동기 메시징 설계

### 12.1 왜 SQS인가

이 프로젝트는 비동기 패턴 학습이 핵심 목표 중 하나다.  
특히 analysis-worker와 notification-worker는 비동기 특성이 강하다.

SQS를 선택한 이유:

1. 운영 부담이 낮다
2. DLQ 패턴을 보여주기 좋다
3. 멱등성과 재시도 설계를 설명하기 좋다
4. Kafka보다 현재 프로젝트 범위에 적절하다

### 12.2 현재 SQS 설계

현재는 두 개의 본 큐와 두 개의 DLQ를 만든다.

- `analysis-queue`
- `analysis-queue-dlq`
- `notification-queue`
- `notification-queue-dlq`

현재 기본값:

- `analysis` visibility timeout: `90`
- `notification` visibility timeout: `60`
- `maxReceiveCount`: `3`

### 12.3 왜 큐를 둘로 분리했는가

분석과 알림은 실패 특성과 SLA가 다르다.

- analysis는 OCR mock 지연과 score 반영까지 포함
- notification은 상대적으로 짧은 후처리

둘을 분리하면:

- 재시도 정책을 분리할 수 있다
- DLQ 분석이 쉬워진다
- 관측성과 장애 원인 분리가 좋아진다

---

## 13. ECR 설계

### 13.1 왜 서비스별 ECR repository를 따로 두는가

현재 ECR은 서비스별로 저장소를 만든다.

- `bodybuddy-dev-user-service`
- `bodybuddy-dev-score-service`
- `bodybuddy-dev-analysis-worker`
- `bodybuddy-dev-notification-worker`

이 구조의 장점:

1. 서비스별 이미지 lifecycle 분리
2. 태그/롤백 추적이 쉬움
3. GitHub Actions에서 변경된 서비스만 푸시하기 좋음
4. 나중에 이미지 취약점/보존 정책을 서비스별로 나누기 좋음

### 13.2 왜 scan_on_push를 현재 끈 상태인가

현재는 `scan_on_push = false`다.

이건 명시적인 단계 분리다.

- 지금 목표: Terraform 기반 인프라 구성
- 이후 목표: 보안 스캔/CI 강화

즉, 아직 안 한 것은 실수라기보다 phase 분리다.  
이 점을 README나 면접에서 `future work`로 설명할 수 있다.

---

## 14. 태그와 네이밍 규칙

### 14.1 공통 태그

현재 공통 태그는 다음과 같다.

- `Environment`
- `Project`
- `ManagedBy`
- `Owner`

### 14.2 왜 태그가 중요한가

태그는 단순 장식이 아니라 운영의 기준점이다.

- 비용 분석
- 리소스 검색
- 소유자 식별
- 환경 분리
- 향후 자동화 기준

사이드 프로젝트라도 태그를 일관되게 두면 "운영 마인드"가 느껴진다.

### 14.3 네이밍 규칙

현재 리소스는 `bodybuddy-dev-*` 패턴으로 맞추고 있다.

이 규칙의 장점:

1. 콘솔에서 한눈에 묶인다
2. destroy 후 잔여 리소스 확인이 쉽다
3. env를 나중에 늘릴 때 규칙이 유지된다

---

## 15. 비용 설계 철학

이 프로젝트의 dev 인프라는 "싸게 만들기"가 아니라 **통제 가능하게 만들기**를 목표로 한다.

### 15.1 비용 절감을 위해 적용한 선택

- single NAT
- `db.t4g.micro`
- `cache.t4g.micro`
- bootstrap node 최소화
- Redis 단일 노드
- dev에서 RDS Multi-AZ 비활성화
- destroy 가능한 구조 유지
- S3 Gateway Endpoint로 NAT 경유 비용 절감

### 15.2 왜 이 철학이 중요한가

사이드 프로젝트에서 비싼 인프라를 화려하게 띄우는 것보다,
**같은 목표를 비용을 의식하며 설계했다**는 쪽이 더 좋은 포인트가 된다.

면접 포인트:

"실제로 항상 켜둘 프로덕션 환경이 아니라 학습용 dev 환경이기 때문에, high availability를 무조건 올리는 대신 destroy 가능한 구조와 작은 인스턴스 크기를 택했습니다."

---

## 16. 보안 설계 철학

### 16.1 현재 드러나는 보안 방향

현재 Terraform만 봐도 아래 방향성이 드러난다.

- private subnet 중심 구성
- DB/Redis public 노출 금지
- S3 encryption
- Redis at-rest / transit encryption
- EKS IRSA 활성화
- RDS 비밀번호를 AWS가 관리

### 16.2 왜 "완벽한 보안"보다 "방향성"이 중요한가

현재 Phase 2는 보안 완성 단계가 아니다.  
하지만 중요한 것은 **보안이 나중에 붙는 구조가 아니라 처음부터 반영된 구조인가**다.

이 프로젝트는 그 방향을 이미 갖고 있다.

특히 면접에서 말하기 좋은 포인트:

- Terraform 실행 주체와 런타임 IAM 분리
- IRSA를 위한 OIDC provider 준비
- Secrets를 코드 밖으로 빼는 방향

---

## 17. 왜 이런 트레이드오프를 택했는가

### 17.1 dev와 prod를 구분했다

이 프로젝트는 "모든 것을 production 수준으로 완성"하려 하지 않았다.  
대신 dev에서 설명 가능한 수준까지 맞추고, 이후 phase에서 보강하는 방식을 택했다.

대표 예시:

- RDS Multi-AZ: dev에서는 off
- Redis HA: dev에서는 단일 노드
- S3 encryption: 현재는 AES256 fallback 가능
- EKS endpoint: dev 편의성을 위해 public도 허용

### 17.2 왜 이 판단이 합리적인가

이 프로젝트의 핵심은:

- 운영 포인트를 담는 것
- 실제로 apply/destroy 가능한 것
- 비용을 통제하는 것
- 단계적으로 확장 가능한 것

즉, 현재 설계는 "완벽함"보다 **학습 효율과 설명 가능성**에 최적화돼 있다.

---

## 18. 실제로 해결한 Terraform 이슈

이건 면접에서 꽤 좋은 이야깃거리가 된다.

### 18.1 plan-time unknown 이슈

RDS/Redis 보안 그룹에서 EKS cluster security group을 optional ingress로 연결할 때, 처음엔 plan 시점 unknown 값 때문에 에러가 났다.

문제 원인:

- `for_each` 존재 여부 판단에 apply 이후에만 결정되는 값을 직접 사용함

해결 방식:

- unknown string을 직접 분기 조건에 쓰지 않고
- `enable_cluster_security_group_ingress`라는 **plan 시점에 확정되는 bool 변수**로 분기

이 경험은 다음 포인트로 말할 수 있다.

"Terraform은 값이 unknown이어도 리소스 인자로 넘기는 건 가능하지만, count/for_each의 존재 여부 판단에 unknown이 들어가면 plan이 막히기 때문에 분기 기준을 known boolean으로 분리해 해결했습니다."

이건 단순 버그 픽스가 아니라 **Terraform evaluation model을 이해하고 있다는 신호**다.

---

## 19. 지금 이 설계에서 포트폴리오로 가장 강한 포인트

### 19.1 Remote state 운영 경험

단순 local state가 아니라:

- S3 backend
- DynamoDB locking
- bootstrap 자원 수동 생성

까지 가져간 것은 좋은 포인트다.

### 19.2 EKS와 Karpenter의 역할 분리

클러스터 생성과 노드 lifecycle 자동화를 분리해 설계했다는 점이 좋다.

### 19.3 비용을 의식한 dev 아키텍처

무작정 production 흉내를 내는 대신, 다음을 의식했다.

- 작은 인스턴스
- single NAT
- destroy 중심 운영
- S3 endpoint

### 19.4 데이터 경로 기반 자원 선택

이 프로젝트의 앱 요구와 인프라가 잘 매칭된다.

- 이미지 저장: S3
- 비동기 처리: SQS
- 영속 데이터: PostgreSQL
- 랭킹/캐시: Redis
- 배포 대상: EKS

### 19.5 나중 phase와의 연결성

현재 설계는 이후 phase로 자연스럽게 이어진다.

- IRSA
- ArgoCD
- Observability
- Spot interruption drill
- DR drill

즉, 현재 Phase 2는 "고립된 일회성 환경"이 아니라 뒤 단계의 기반이다.

---

## 20. 면접에서 이렇게 설명하면 좋다

### 20.1 30초 버전

"BodyBuddy 인프라는 Terraform으로 app과 infra 레포를 분리하고, dev 환경에서 VPC, EKS, Karpenter 기반 노드 운영, RDS, Redis, S3, SQS, ECR까지 모듈 단위로 구성했습니다. 특히 S3 backend와 DynamoDB locking으로 Terraform state 운영 구조까지 갖췄고, 비용을 의식해 single NAT, 작은 인스턴스, destroy 중심 운영 패턴을 택했습니다."

### 20.2 1분 버전

"이 프로젝트의 목표가 앱 기능보다 인프라 역량을 보여주는 것이어서, Terraform 구조를 꽤 의도적으로 설계했습니다. 환경 조립 지점은 `envs/dev`로 두고, VPC, EKS, Karpenter, RDS, ElastiCache, S3, SQS, ECR을 서비스별 AWS 책임 경계로 모듈화했습니다. EKS는 bootstrap managed node group으로 먼저 자립하게 하고, 이후 Karpenter가 노드 lifecycle을 가져가도록 설계했습니다. 비용 측면에서는 single NAT, S3 Gateway Endpoint, `db.t4g.micro`, `cache.t4g.micro`를 선택했고, backend도 S3와 DynamoDB lock으로 운영형으로 구성했습니다. 실제 AWS 기준 `terraform plan`까지 검증했습니다."

### 20.3 꼬리 질문 대응

`왜 bootstrap node group이 필요한가?`

- Karpenter controller가 뜨려면 최소 노드가 먼저 필요하기 때문

`왜 RDS Multi-AZ를 dev에서 껐나?`

- 비용 통제 목적, DR은 이후 phase에서 drill 중심으로 검증

`왜 S3 endpoint를 넣었나?`

- 이미지 중심 워크플로에서 NAT 비용 절감 효과가 크기 때문

`왜 state backend를 굳이 remote로 했나?`

- 상태 유실과 동시 apply 충돌 방지, 운영형 Terraform 흐름을 만들기 위해

`왜 Karpenter를 썼나?`

- Spot과 비용 최적화, 더 유연한 node provisioning, 이후 시연 포인트 확보

---

## 21. 아직 남은 것과 솔직하게 말할 부분

현재는 강한 포인트가 많지만, 아직 남은 것도 분명히 있다.

### 21.1 아직 안 끝난 것

- 실제 `terraform apply`
- `kubectl get nodes` 확인
- RDS migration 실제 실행
- Redis/RDS 연결 실제 검증
- IRSA 서비스별 상세 role 구현
- Karpenter Helm/K8s 레벨 설치

### 21.2 면접에서 솔직하게 말할 수 있는 표현

"Terraform 설계와 AWS plan 단계까지는 검증했고, apply 이후 실제 워크로드 배포와 IRSA 세분화, Karpenter 운영 검증은 다음 단계에서 이어서 진행할 예정이었습니다."

이건 약점이 아니라 **단계를 나눠서 구현했다는 신호**가 될 수 있다.

---

## 22. 내가 반드시 기억해야 할 핵심 메시지

이 Terraform 작업의 핵심 메시지는 아래 다섯 줄로 정리할 수 있다.

1. 나는 AWS 인프라를 앱 요구사항에 맞게 모듈로 분리해서 설계했다.
2. 나는 Terraform state 운영까지 포함해 remote backend와 locking을 구성했다.
3. 나는 EKS를 단순 생성한 것이 아니라, bootstrap node + Karpenter 확장 구조로 설계했다.
4. 나는 비용을 의식해서 dev 환경을 작고 destroy-friendly하게 만들었다.
5. 나는 이후 GitOps, IRSA, Observability, Spot, DR까지 이어질 기반을 먼저 깔았다.

---

## 23. apply 이후 이 문서에 추가할 것

실제 `terraform apply`가 끝나면 아래 내용을 이 문서에 추가하면 좋다.

- 실제 apply 소요 시간
- 생성된 노드 수
- `kubectl get nodes` 결과
- RDS endpoint / Redis endpoint 확인 결과
- 예상 비용과 실제 초기 비용 체감
- destroy 결과와 잔여 리소스 점검 기록

그 시점부터는 이 문서가 단순 설계 문서가 아니라 **실행 경험까지 담긴 포트폴리오 문서**가 된다.
