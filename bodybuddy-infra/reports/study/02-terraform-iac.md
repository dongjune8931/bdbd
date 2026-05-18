# 02. IaC 기반 구축 — Terraform으로 AWS 인프라 프로비저닝

## 개요

02번 작업의 목표는 코드로 AWS 인프라를 만들고, `terraform destroy`로 깨끗하게 내릴 수 있는 상태를 만드는 것이다. 단순히 리소스를 만드는 것이 아니라 **모듈 경계 설계**, **State 관리**, **비용 의식적 선택**이 핵심 학습 포인트다.

02번 작업 완료 상태:
- EKS 클러스터가 뜨고 `kubectl get nodes` 성공
- RDS PostgreSQL과 ElastiCache Redis에 접근 가능
- `terraform destroy`로 모든 리소스가 깨끗하게 삭제됨

---

## 1. Terraform State 백엔드 부트스트랩

### 1.1 State란 무엇인가?

**📌 개념 설명: Terraform State**
Terraform은 AWS에 실제로 어떤 리소스가 있는지 추적하기 위해 `terraform.tfstate` 파일을 사용한다. 이 파일에는:
- 어떤 리소스가 생성됐는지
- 각 리소스의 ID, ARN, 설정값
- 리소스 간 의존관계

가 담겨있다. State 없이는 `terraform plan`이 현재 인프라 상태를 알 수 없다.

**로컬 State의 문제점:**
- 팀원 A가 apply하면 State 파일이 A의 로컬에만 있다
- 팀원 B가 apply하면 State 충돌 → 같은 리소스가 두 번 만들어질 수 있다
- 로컬 파일이 삭제되면 Terraform이 기존 리소스를 모른다

**해결책: 원격 State (S3 + DynamoDB)**

### 1.2 왜 수동으로 만드는가?

```
문제: Terraform으로 Terraform State 버킷을 만들 수 없다
이유: State 버킷을 만들기 전에는 State를 저장할 곳이 없다
해결: 최초 1회만 수동으로 AWS CLI로 생성
```

```bash
# 1. S3 버킷 생성 (State 저장)
aws s3api create-bucket \
  --bucket bodybuddy-tfstate-902371998304 \
  --region ap-northeast-2 \
  --create-bucket-configuration LocationConstraint=ap-northeast-2

# 2. 버킷 버저닝 활성화 (State 파일 실수로 덮어써도 복구 가능)
aws s3api put-bucket-versioning \
  --bucket bodybuddy-tfstate-902371998304 \
  --versioning-configuration Status=Enabled

# 3. SSE-KMS 암호화 활성화
aws s3api put-bucket-encryption \
  --bucket bodybuddy-tfstate-902371998304 \
  --server-side-encryption-configuration '...'

# 4. DynamoDB 테이블 생성 (State 잠금)
aws dynamodb create-table \
  --table-name bodybuddy-tflock \
  --attribute-definitions AttributeName=LockID,AttributeType=S \
  --key-schema AttributeName=LockID,KeyType=HASH \
  --billing-mode PAY_PER_REQUEST \
  --region ap-northeast-2
```

**📌 개념 설명: DynamoDB Lock**
두 명이 동시에 `terraform apply`를 실행하면 같은 State 파일을 동시에 수정할 수 있다 → 충돌 발생. DynamoDB Lock은 apply 중 잠금을 걸어서 한 번에 한 명만 apply할 수 있게 한다.

### 1.3 backend.tf 설정

```hcl
# terraform/envs/dev/backend.tf
terraform {
  backend "s3" {
    bucket         = "bodybuddy-tfstate-902371998304"
    key            = "dev/terraform.tfstate"
    region         = "ap-northeast-2"
    dynamodb_table = "bodybuddy-tflock"
    encrypt        = true
  }
}
```

---

## 2. 모듈 구조 설계

### 2.1 모듈이란?

**📌 개념 설명: Terraform 모듈**
재사용 가능한 Terraform 코드 묶음이다. 함수처럼 입력(variables)과 출력(outputs)이 있다. 예를 들어 `rds` 모듈을 만들면:
- 입력: DB 이름, 인스턴스 타입, 서브넷 ID
- 출력: RDS 엔드포인트, 보안그룹 ID

모듈 덕분에 `envs/dev/main.tf`는 "어떤 모듈을 어떤 설정으로 쓸 것인가"만 선언한다. 구현 복잡성은 모듈 내부에 숨겨진다.

### 2.2 모듈 목록과 역할

```
terraform/modules/
├── vpc/            # VPC, 서브넷, 라우팅, NAT, S3 VPC Endpoint
├── eks/            # EKS 클러스터, bootstrap 노드그룹
├── karpenter/      # Karpenter IAM 역할, 인스턴스 프로파일, SQS 인터럽션 큐
├── rds/            # RDS PostgreSQL 인스턴스, 서브넷 그룹, 보안그룹
├── elasticache/    # ElastiCache Redis 클러스터, 서브넷 그룹
├── s3/             # 이미지 버킷 (Versioning, Object Lock, SSE-KMS)
├── sqs/            # analysis-queue, notification-queue, DLQ 2개
├── ecr/            # ECR 레포지토리 4개 (서비스별)
├── iam-irsa/       # ServiceAccount별 IAM Role (IRSA)
└── lambda-s3-recovery/ # DR 자동 복구에서 사용
```

### 2.3 모듈 파일 구조 규칙

각 모듈은 4개 파일로 구성된다:

```
modules/rds/
├── main.tf        # 리소스 정의
├── variables.tf   # 입력 변수 (description, type 필수)
├── outputs.tf     # 출력값 (description 필수)
└── versions.tf    # required_providers (버전 고정)
```

```hcl
# modules/rds/variables.tf 예시
variable "db_name" {
  description = "PostgreSQL 데이터베이스 이름"
  type        = string
}

variable "instance_class" {
  description = "RDS 인스턴스 타입 (예: db.t4g.micro)"
  type        = string
  default     = "db.t4g.micro"  # dev 환경용 기본값
}
```

---

## 3. VPC 설계

### 3.1 서브넷 구조

```
ap-northeast-2 리전
├── VPC: 10.20.0.0/16
│   ├── Public Subnet (AZ-a): 10.20.1.0/24  ← ALB
│   ├── Public Subnet (AZ-c): 10.20.2.0/24  ← ALB
│   ├── Private Subnet (AZ-a): 10.20.11.0/24 ← EKS 노드, RDS, Redis
│   └── Private Subnet (AZ-c): 10.20.12.0/24 ← EKS 노드, RDS, Redis
│
│   ├── NAT Gateway (단일, AZ-a에만)
│   └── S3 Gateway VPC Endpoint
```

**왜 ALB는 Public, 나머지는 Private?**
- ALB: 인터넷에서 접근해야 하므로 Public subnet에 위치
- EKS 노드: 직접 인터넷 노출 불필요. ALB를 통해서만 트래픽 수신. Private에 두면 공격 표면 감소.
- RDS/Redis: 앱 Pod에서만 접근. 인터넷 노출 절대 불필요.

**왜 NAT Gateway는 단일?**
- Multi-AZ NAT Gateway: AZ마다 하나 → 트래픽이 AZ를 벗어나지 않아 고가용성
- 단일 NAT Gateway: AZ-a가 다운되면 Private Subnet의 인터넷 접근 불가
- **비용**: NAT Gateway는 시간당 약 $0.059 + 데이터 처리량 $0.059/GB. 2개면 비용 2배.
- **결정**: 데모/학습 프로젝트이므로 단일 NAT Gateway. 24/7 운영도 안 함.

### 3.2 S3 Gateway VPC Endpoint

**📌 개념 설명: VPC Endpoint**
일반적으로 Private subnet의 리소스(EKS 노드)가 S3에 접근하려면:
```
EKS 노드 → NAT Gateway → 인터넷 → S3
```
NAT Gateway 데이터 처리 비용이 발생한다 ($0.059/GB).

S3 Gateway VPC Endpoint를 사용하면:
```
EKS 노드 → VPC Endpoint → S3 (AWS 내부 네트워크)
```
NAT Gateway를 거치지 않아 **데이터 처리 비용 0원**.

이미지 업로드/다운로드가 많으면 이 비용 차이가 크다. 학습 프로젝트에서도 이런 비용 최적화를 적용하는 것이 인프라 엔지니어링 감각을 보여준다.

```hcl
# modules/vpc/main.tf
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.ap-northeast-2.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.private.id]
}
```

---

## 4. EKS 모듈

### 4.1 terraform-aws-modules/eks/aws 사용

외부 검증된 모듈을 활용한다. EKS 설정에는 복잡한 IAM, OIDC, 보안그룹 설정이 얽혀있어, 직접 작성하면 실수하기 쉽다.

```hcl
# modules/eks/main.tf
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.37"

  cluster_name    = var.cluster_name
  cluster_version = "1.30"

  vpc_id     = var.vpc_id
  subnet_ids = var.private_subnet_ids

  # API 서버를 Public + Private 모두에서 접근 가능
  cluster_endpoint_public_access  = true
  cluster_endpoint_private_access = true

  # EKS Managed Node Group (Karpenter bootstrap용)
  eks_managed_node_groups = {
    bootstrap = {
      instance_types = ["t3.medium"]
      min_size       = 2
      max_size       = 2
      desired_size   = 2

      # iam_role_additional_policies를 통해 SSM, CloudWatch 권한 추가
    }
  }
}
```

### 4.2 Bootstrap 노드그룹이 필요한 이유

Karpenter 자체가 Pod으로 실행된다. Karpenter Pod이 실행될 노드가 없으면 Karpenter가 노드를 만들 수 없다 → chicken-and-egg 문제.

해결: `bootstrap` 노드그룹 (EKS Managed Node Group, 2개)을 미리 만들어서 Karpenter와 기타 시스템 컴포넌트(ArgoCD, Prometheus 등)가 여기서 실행된다. 앱 Pod은 Karpenter가 만든 노드에서 실행된다.

### 4.3 트러블슈팅: EKS bootstrap node join failure

**🔥 트러블슈팅 1: 커스텀 Launch Template의 UserData 비어있는 문제**

**증상:**
```
EKS managed node group 노드가 클러스터에 join하지 못함
EC2 인스턴스는 뜨지만 kubectl get nodes에 나타나지 않음
```

**원인:**
EKS Managed Node Group에 커스텀 launch template을 사용하면, UserData에 EKS bootstrap script를 직접 넣어야 한다. 이를 빠뜨리면 노드가 EKS API 서버를 찾지 못한다.

**해결:**
```hcl
# 커스텀 launch template 사용하지 않음
eks_managed_node_groups = {
  bootstrap = {
    use_custom_launch_template = false  # 기본 EKS 관리 launch template 사용

    # AMI 타입 명시 (AL2_x86_64: Amazon Linux 2)
    ami_type = "AL2_x86_64"

    # 클러스터 기본 보안그룹 연결 (노드 간 통신 허용)
    attach_cluster_primary_security_group = true
  }
}
```

**배운 점:** EKS bootstrap 과정(노드가 클러스터에 join하는 과정)은 UserData 스크립트가 담당한다. 검증된 EKS 모듈을 사용할 때 커스텀 launch template은 충분히 이해한 뒤에 적용해야 한다.

---

## 5. Karpenter 모듈

### 5.1 Karpenter란?

**📌 개념 설명: Karpenter vs Cluster Autoscaler**

Cluster Autoscaler (구방식):
- ASG(Auto Scaling Group) 단위로 노드를 추가/삭제
- Pod이 스케줄 안 되면 → ASG에 스케일아웃 요청 → 새 노드가 뜨는 데 수분
- 인스턴스 타입이 고정 (ASG 설정에 묶여있음)

Karpenter (신방식):
- Pod의 resource request를 보고 최적 인스턴스 타입을 직접 결정
- EC2 Fleet API로 바로 노드를 시작 → 더 빠름
- Spot 인스턴스 통합, 다양한 인스턴스 타입 지원

### 5.2 Karpenter 설치에 필요한 AWS 리소스

```hcl
# modules/karpenter/main.tf

# 1. Karpenter 컨트롤러 IAM Role (IRSA)
# Karpenter Pod이 EC2 Fleet API를 호출하기 위한 권한
resource "aws_iam_role" "karpenter_controller" {
  name = "bodybuddy-dev-karpenter-controller"
  assume_role_policy = data.aws_iam_policy_document.karpenter_assume.json
}

# 2. 노드 IAM Role (Karpenter가 만든 EC2가 사용)
resource "aws_iam_role" "karpenter_node" {
  name = "bodybuddy-dev-karpenter-node"
  # AmazonEKSWorkerNodePolicy, ECR ReadOnly 등 연결
}

# 3. 인스턴스 프로파일 (EC2가 IAM Role을 사용하기 위한 래퍼)
resource "aws_iam_instance_profile" "karpenter_node" {
  name = "bodybuddy-dev-karpenter-node"
  role = aws_iam_role.karpenter_node.name
}

# 4. Spot Interruption SQS Queue
# AWS가 Spot 인터럽션 이벤트를 이 큐로 보냄
# Karpenter가 큐를 폴링해서 인터럽션 사전 감지 → graceful 처리
resource "aws_sqs_queue" "karpenter_interruption" {
  name                      = "bodybuddy-dev-karpenter"
  message_retention_seconds = 300 # 5분
}

# 5. EventBridge Rule: Spot 인터럽션 이벤트 → SQS
resource "aws_cloudwatch_event_rule" "spot_interruption" {
  name        = "bodybuddy-dev-spot-interruption"
  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EC2 Spot Instance Interruption Warning"]
  })
}
```

**📌 개념 설명: Spot Interruption Notice 흐름**
```
AWS → [EventBridge: Spot Interruption Warning] → [SQS] → Karpenter 감지
                                                              ↓
                                               해당 노드에 SIGTERM 전송 시작
                                               새 대체 노드 프로비저닝 시작
                                               기존 노드 drain (Pod 이동)
```
Karpenter가 SQS 큐를 통해 인터럽션을 미리 알면 2분의 여유 시간을 활용할 수 있다.

---

## 6. RDS 모듈

### 6.1 설정

```hcl
# modules/rds/main.tf

# 보안그룹: EKS 노드에서만 접근 허용
resource "aws_security_group" "rds" {
  name   = "bodybuddy-dev-rds"
  vpc_id = var.vpc_id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [var.eks_node_security_group_id]
  }
}

resource "aws_db_instance" "main" {
  identifier        = "bodybuddy-dev-postgres"
  engine            = "postgres"
  engine_version    = "15.7"
  instance_class    = "db.t4g.micro"     # 비용 최소화
  allocated_storage = 20
  storage_type      = "gp3"

  db_name  = "bodybuddy"
  username = "bodybuddy"

  # Secrets Manager에서 비밀번호 자동 관리
  manage_master_user_password = true

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # 백업 (7일 보관)
  backup_retention_period = 7
  backup_window           = "03:00-04:00"  # UTC (KST 12:00-13:00)

  # 암호화
  storage_encrypted = true

  # SSL 강제 (parameter group으로)
  parameter_group_name = aws_db_parameter_group.main.name

  # Multi-AZ: DR 드릴 시간만 ON (비용 절감)
  multi_az = false

  skip_final_snapshot = true  # destroy 시 스냅샷 없이 삭제
}

resource "aws_db_parameter_group" "main" {
  family = "postgres15"

  parameter {
    name  = "rds.force_ssl"
    value = "1"  # SSL 연결 강제
  }
}
```

**📌 개념 설명: manage_master_user_password**
Terraform이 자동으로 AWS Secrets Manager에 비밀번호를 저장하고 관리한다. `terraform.tfstate`에 비밀번호가 평문으로 저장되지 않는다. 애플리케이션은 Secrets Manager에서 비밀번호를 읽어야 한다.

**비용 결정: db.t4g.micro**
- t4g.micro: 2 vCPU, 1GB RAM, 월 약 $13 (단일 AZ)
- 데모 프로젝트에 충분. 실 부하 테스트 시 t4g.small로 업그레이드 가능.
- Multi-AZ는 평소에 끄고 DR 드릴 때만 켠다 (Multi-AZ = 2배 비용).

---

## 7. ElastiCache 모듈

### 7.1 설정

```hcl
# modules/elasticache/main.tf

resource "aws_elasticache_cluster" "main" {
  cluster_id           = "bodybuddy-dev-redis"
  engine               = "redis"
  engine_version       = "7.1"
  node_type            = "cache.t4g.micro"  # 비용 최소화
  num_cache_nodes      = 1                  # 단일 노드 (데모용)

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.redis.id]

  # TLS 암호화
  transit_encryption_enabled = true

  parameter_group_name = "default.redis7"
}
```

**📌 개념 설명: TransitEncryptionMode**
ElastiCache Redis가 TLS를 요구할 때, 클라이언트도 TLS로 연결해야 한다. 이것이 컨테이너 배포 작업의 트러블슈팅 원인이 된다. 앱의 Redis 클라이언트 설정에 `TLSConfig`를 추가해야 한다.

---

## 8. IAM IRSA 모듈

### 8.1 IRSA란?

**📌 개념 설명: IRSA (IAM Roles for Service Accounts)**
Kubernetes ServiceAccount에 AWS IAM Role을 연결하는 메커니즘이다.

전통적인 방식 (나쁜 방법):
```yaml
env:
  - name: AWS_ACCESS_KEY_ID
    value: "AKIAIOSFODNN7EXAMPLE"
  - name: AWS_SECRET_ACCESS_KEY
    value: "wJalrXUtnFEMI..."
```
문제: 키가 노출되면 위험, 키 로테이션 어려움.

IRSA 방식 (올바른 방법):
```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::902371998304:role/bodybuddy-dev-analysis-worker-irsa
```
Pod은 IAM Role을 assume하여 임시 자격증명을 발급받는다. 키가 없어도 AWS API를 호출할 수 있다.

### 8.2 IRSA Trust Policy

```hcl
# modules/iam-irsa/main.tf
data "aws_iam_policy_document" "assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type = "Federated"
      # EKS 클러스터의 OIDC Provider ARN
      identifiers = [var.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_provider}:sub"
      # 특정 네임스페이스의 특정 ServiceAccount만 허용
      values   = ["system:serviceaccount:bodybuddy:${var.service_account_name}"]
    }
  }
}
```

`analysis-worker`의 IRSA 정책:
```hcl
# analysis-worker가 할 수 있는 것:
# - SQS analysis-queue에서 메시지 수신/삭제
# - S3 이미지 버킷의 특정 prefix에서 읽기
resource "aws_iam_policy" "analysis_worker" {
  policy = jsonencode({
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility"]
        Resource = var.analysis_queue_arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:HeadObject"]
        Resource = "${var.image_bucket_arn}/inbody/*"
      }
    ]
  })
}
```

---

## 9. envs/dev/main.tf 모듈 조합

```hcl
# terraform/envs/dev/main.tf

module "vpc" {
  source = "../../modules/vpc"

  vpc_cidr             = local.vpc_cidr
  availability_zones   = local.availability_zones
  public_subnet_cidrs  = local.public_subnet_cidrs
  private_subnet_cidrs = local.private_subnet_cidrs
  environment          = local.environment
  project              = local.project
}

module "eks" {
  source = "../../modules/eks"

  cluster_name            = local.cluster_name
  vpc_id                  = module.vpc.vpc_id
  private_subnet_ids      = module.vpc.private_subnet_ids
  environment             = local.environment
}

module "karpenter" {
  source = "../../modules/karpenter"

  cluster_name            = module.eks.cluster_name
  cluster_endpoint        = module.eks.cluster_endpoint
  oidc_provider_arn       = module.eks.oidc_provider_arn
  node_iam_role_arn       = module.eks.eks_managed_node_groups["bootstrap"].iam_role_arn
  environment             = local.environment
}

module "rds" {
  source = "../../modules/rds"

  vpc_id                     = module.vpc.vpc_id
  private_subnet_ids         = module.vpc.private_subnet_ids
  eks_node_security_group_id = module.eks.node_security_group_id
  environment                = local.environment
}

module "elasticache" {
  source = "../../modules/elasticache"

  vpc_id                     = module.vpc.vpc_id
  private_subnet_ids         = module.vpc.private_subnet_ids
  eks_node_security_group_id = module.eks.node_security_group_id
  environment                = local.environment
}
```

**모듈 간 참조:** `module.vpc.private_subnet_ids` 처럼 한 모듈의 출력을 다른 모듈의 입력으로 전달한다. Terraform이 자동으로 의존관계를 파악하여 VPC가 먼저 생성된 뒤 EKS를 생성한다.

---

## 10. shared/locals.tf

```hcl
# terraform/shared/locals.tf
locals {
  project     = "bodybuddy"
  environment = "dev"
  region      = "ap-northeast-2"
  account_id  = "902371998304"

  cluster_name = "${local.project}-${local.environment}"

  vpc_cidr             = "10.20.0.0/16"
  availability_zones   = ["ap-northeast-2a", "ap-northeast-2c"]
  public_subnet_cidrs  = ["10.20.1.0/24", "10.20.2.0/24"]
  private_subnet_cidrs = ["10.20.11.0/24", "10.20.12.0/24"]

  # 모든 리소스에 공통 태그
  common_tags = {
    Project     = local.project
    Environment = local.environment
    ManagedBy   = "terraform"
    Owner       = "idongjun"
  }
}
```

공통 로컬 변수를 한 곳에 모아두면 환경 이름을 바꾸거나 리전을 바꿀 때 한 파일만 수정하면 된다.

---

## 11. Terraform 명명 규칙

**리소스 이름:** `bodybuddy-<env>-<purpose>`
- `bodybuddy-dev-postgres`
- `bodybuddy-dev-redis`
- `bodybuddy-dev-karpenter-controller` (IAM Role)

**필수 태그:**
```hcl
tags = merge(local.common_tags, {
  Name = "bodybuddy-dev-postgres"
})
```

---

## 12. Terraform 적용 워크플로

### 12.1 일반적인 흐름

```bash
# 1. 초기화 (처음 한 번 또는 모듈 추가 후)
terraform init

# 2. 변경사항 미리보기
terraform plan -out=tfplan

# 3. 계획 검토 후 적용
terraform apply tfplan

# 4. 리소스 삭제 (사용 안 할 때)
terraform destroy
```

### 12.2 destroy 운영 패턴

이 프로젝트는 24/7 운영을 하지 않는다. 작업이 끝나면 `terraform destroy`로 내린다.

```bash
# 비용 나가는 리소스 내리기
terraform destroy -target=module.eks
terraform destroy -target=module.rds
# 또는 전체
terraform destroy
```

**주의:** EKS 클러스터를 destroy하면 Karpenter가 만든 노드들도 삭제되어야 한다. 그렇지 않으면 EC2 인스턴스가 Terraform 외부에 남는다. 이를 방지하기 위해:
1. 먼저 `kubectl delete nodeclaim --all`로 Karpenter 노드를 삭제
2. 그 다음 `terraform destroy`

---

## 13. 비용 의식 결정 모음

| 결정 | 대안 | 이유 |
|---|---|---|
| `db.t4g.micro` | db.t3.small | 데모 트래픽에 micro로 충분, 월 ~$7 절감 |
| RDS Multi-AZ 기본 OFF | 항상 ON | $12→$25/월. DR 드릴 시간만 켜면 됨 |
| 단일 NAT Gateway | 멀티 AZ NAT | AZ 고가용성 없지만 데모 수준에서 충분, 월 ~$30 절감 |
| S3 Gateway VPC Endpoint | NAT 경유 | 이미지 트래픽이 NAT를 거치면 $0.059/GB. Endpoint는 무료 |
| `cache.t4g.micro` 단일 노드 | 클러스터 모드 | 랭킹 캐시 데모용, HA 불필요 |
| Spot 노드 (워커) | on-demand | 워커는 SLA 없음. Spot으로 70~90% 절감 |

---

## 핵심 요약

- **State 백엔드(S3+DynamoDB) 부트스트랩은 수동**: Terraform으로 Terraform State 버킷을 만들 수 없는 닭-달걀 문제. 최초 1회 AWS CLI로 생성.
- **모듈 경계는 AWS 서비스 단위**: vpc, eks, rds, elasticache, sqs 등. 모듈 간 참조는 outputs/variables로 명시적으로 연결.
- **S3 Gateway VPC Endpoint 필수**: 이미지 데이터가 NAT를 거치지 않아 처리비용 절감. 라우팅 테이블에 Endpoint를 연결하면 자동 적용.
- **IRSA로 AccessKey 제거**: Pod에 환경변수로 키를 넣는 것은 보안 위험. ServiceAccount 어노테이션 + Trust Policy로 IAM Role을 assume.
- **Bootstrap 노드그룹**: Karpenter Pod 자체가 실행될 노드가 필요 (닭-달걀). EKS Managed Node Group 2개를 bootstrap으로 유지.
- **EKS bootstrap 트러블슈팅**: 커스텀 launch template 사용 시 UserData 직접 관리 필요. 검증된 모듈 사용 시 `use_custom_launch_template=false` 권장.
- **db.t4g.micro + Single AZ + 단일 NAT**: 데모 환경 비용 최소화. 면접에서 "프로덕션에서는 어떻게 바꾸겠냐"에 답할 수 있어야 한다.
