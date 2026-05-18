provider "aws" {
  region = var.aws_region
}

locals {
  project       = "bodybuddy"
  name_prefix   = "${local.project}-${var.environment}"
  app_namespace = "bodybuddy"
  common_tags = {
    Environment = var.environment
    Project     = local.project
    ManagedBy   = "terraform"
    Owner       = "dongjune8931"
  }
}

module "vpc" {
  source = "../../modules/vpc"

  environment          = var.environment
  name                 = "${local.name_prefix}-vpc"
  cidr                 = var.vpc_cidr
  azs                  = var.availability_zones
  public_subnets       = var.public_subnet_cidrs
  private_subnets      = var.private_subnet_cidrs
  single_nat_gateway   = var.single_nat_gateway
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = local.common_tags
}

module "s3" {
  source = "../../modules/s3"

  environment = var.environment
  bucket_name = var.s3_bucket_name
  kms_key_arn = var.kms_key_arn
  tags        = local.common_tags
}

module "s3_auto_recovery" {
  source = "../../modules/lambda-s3-recovery"

  function_name = "${local.name_prefix}-s3-auto-recovery"
  bucket_name   = module.s3.bucket_name
  bucket_arn    = module.s3.bucket_arn
  package_path  = "${path.module}/../../../lambda/s3-auto-recovery/dist/bootstrap.zip"
  tags          = local.common_tags
}

module "sqs" {
  source = "../../modules/sqs"

  environment             = var.environment
  analysis_queue_name     = "${local.project}-${var.environment}-${var.sqs_queue_names.analysis}-queue"
  notification_queue_name = "${local.project}-${var.environment}-${var.sqs_queue_names.notification}-queue"
  max_receive_count       = 3
  tags                    = local.common_tags
}

module "ecr" {
  source = "../../modules/ecr"

  environment  = var.environment
  repositories = var.ecr_repositories
  tags         = local.common_tags
}

module "eks" {
  source = "../../modules/eks"

  cluster_name              = var.cluster_name
  cluster_version           = var.cluster_version
  vpc_id                    = module.vpc.vpc_id
  subnet_ids                = module.vpc.private_subnet_ids
  control_plane_subnet_ids  = module.vpc.private_subnet_ids
  endpoint_public_access    = var.cluster_endpoint_public_access
  endpoint_private_access   = var.cluster_endpoint_private_access
  cluster_enabled_log_types = var.cluster_enabled_log_types

  bootstrap_instance_types = var.bootstrap_node_instance_types
  bootstrap_desired_size   = var.bootstrap_node_desired_size
  bootstrap_min_size       = var.bootstrap_node_min_size
  bootstrap_max_size       = var.bootstrap_node_max_size

  tags = local.common_tags
}

resource "aws_security_group" "user_service_alb" {
  name                   = "${local.name_prefix}-user-service-alb"
  description            = "Security group for the user-service ALB."
  vpc_id                 = module.vpc.vpc_id
  revoke_rules_on_delete = true

  tags = merge(local.common_tags, {
    Name = "${local.name_prefix}-user-service-alb"
  })
}

resource "aws_vpc_security_group_ingress_rule" "user_service_alb_http" {
  security_group_id = aws_security_group.user_service_alb.id
  description       = "HTTP from the internet to user-service ALB"
  ip_protocol       = "tcp"
  from_port         = 80
  to_port           = 80
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_egress_rule" "user_service_alb_all" {
  security_group_id = aws_security_group.user_service_alb.id
  description       = "All outbound traffic from user-service ALB"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_vpc_security_group_ingress_rule" "user_service_alb_to_nodes_http" {
  security_group_id            = module.eks.node_security_group_id
  description                  = "user-service ALB to Karpenter and bootstrap nodes"
  referenced_security_group_id = aws_security_group.user_service_alb.id
  ip_protocol                  = "tcp"
  from_port                    = 8080
  to_port                      = 8080
}

module "karpenter" {
  source = "../../modules/karpenter"

  cluster_name              = module.eks.cluster_name
  cluster_oidc_provider     = module.eks.oidc_provider
  cluster_oidc_provider_arn = module.eks.oidc_provider_arn
  tags                      = local.common_tags
}

resource "aws_eks_access_entry" "karpenter_node" {
  cluster_name  = module.eks.cluster_name
  principal_arn = module.karpenter.node_iam_role_arn
  type          = "EC2_LINUX"

  tags = merge(local.common_tags, {
    Name = "${local.name_prefix}-karpenter-node-access-entry"
  })
}

data "aws_iam_policy_document" "aws_load_balancer_controller_irsa" {
  statement {
    effect = "Allow"
    actions = [
      "iam:CreateServiceLinkedRole",
    ]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "iam:AWSServiceName"
      values   = ["elasticloadbalancing.amazonaws.com"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:DescribeAccountAttributes",
      "ec2:DescribeAddresses",
      "ec2:DescribeAvailabilityZones",
      "ec2:DescribeInternetGateways",
      "ec2:DescribeVpcs",
      "ec2:DescribeVpcPeeringConnections",
      "ec2:DescribeSubnets",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeInstances",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeTags",
      "ec2:GetCoipPoolUsage",
      "ec2:DescribeCoipPools",
      "ec2:GetSecurityGroupsForVpc",
      "ec2:DescribeIpamPools",
      "ec2:DescribeRouteTables",
      "elasticloadbalancing:DescribeLoadBalancers",
      "elasticloadbalancing:DescribeLoadBalancerAttributes",
      "elasticloadbalancing:DescribeListeners",
      "elasticloadbalancing:DescribeListenerCertificates",
      "elasticloadbalancing:DescribeSSLPolicies",
      "elasticloadbalancing:DescribeRules",
      "elasticloadbalancing:DescribeTargetGroups",
      "elasticloadbalancing:DescribeTargetGroupAttributes",
      "elasticloadbalancing:DescribeTargetHealth",
      "elasticloadbalancing:DescribeTags",
      "elasticloadbalancing:DescribeTrustStores",
      "elasticloadbalancing:DescribeListenerAttributes",
      "elasticloadbalancing:DescribeCapacityReservation",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "cognito-idp:DescribeUserPoolClient",
      "acm:ListCertificates",
      "acm:DescribeCertificate",
      "iam:ListServerCertificates",
      "iam:GetServerCertificate",
      "waf-regional:GetWebACL",
      "waf-regional:GetWebACLForResource",
      "waf-regional:AssociateWebACL",
      "waf-regional:DisassociateWebACL",
      "wafv2:GetWebACL",
      "wafv2:GetWebACLForResource",
      "wafv2:AssociateWebACL",
      "wafv2:DisassociateWebACL",
      "shield:GetSubscriptionState",
      "shield:DescribeProtection",
      "shield:CreateProtection",
      "shield:DeleteProtection",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:RevokeSecurityGroupIngress",
    ]
    resources = ["*"]
  }

  statement {
    effect    = "Allow"
    actions   = ["ec2:CreateSecurityGroup"]
    resources = ["*"]
  }

  statement {
    effect    = "Allow"
    actions   = ["ec2:CreateTags"]
    resources = ["arn:aws:ec2:*:*:security-group/*"]

    condition {
      test     = "StringEquals"
      variable = "ec2:CreateAction"
      values   = ["CreateSecurityGroup"]
    }

    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:CreateTags",
      "ec2:DeleteTags",
    ]
    resources = ["arn:aws:ec2:*:*:security-group/*"]

    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["true"]
    }

    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:RevokeSecurityGroupIngress",
      "ec2:DeleteSecurityGroup",
    ]
    resources = ["*"]

    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:CreateLoadBalancer",
      "elasticloadbalancing:CreateTargetGroup",
    ]
    resources = ["*"]

    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:CreateListener",
      "elasticloadbalancing:DeleteListener",
      "elasticloadbalancing:CreateRule",
      "elasticloadbalancing:DeleteRule",
    ]
    resources = ["*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:AddTags",
      "elasticloadbalancing:RemoveTags",
    ]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:targetgroup/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/net/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/app/*/*",
    ]

    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["true"]
    }

    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:AddTags",
      "elasticloadbalancing:RemoveTags",
    ]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:listener/net/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener/app/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener-rule/net/*/*/*",
      "arn:aws:elasticloadbalancing:*:*:listener-rule/app/*/*/*",
    ]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:ModifyLoadBalancerAttributes",
      "elasticloadbalancing:SetIpAddressType",
      "elasticloadbalancing:SetSecurityGroups",
      "elasticloadbalancing:SetSubnets",
      "elasticloadbalancing:DeleteLoadBalancer",
      "elasticloadbalancing:ModifyTargetGroup",
      "elasticloadbalancing:ModifyTargetGroupAttributes",
      "elasticloadbalancing:DeleteTargetGroup",
      "elasticloadbalancing:ModifyListenerAttributes",
      "elasticloadbalancing:ModifyCapacityReservation",
      "elasticloadbalancing:ModifyIpPools",
    ]
    resources = ["*"]

    condition {
      test     = "Null"
      variable = "aws:ResourceTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect  = "Allow"
    actions = ["elasticloadbalancing:AddTags"]
    resources = [
      "arn:aws:elasticloadbalancing:*:*:targetgroup/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/net/*/*",
      "arn:aws:elasticloadbalancing:*:*:loadbalancer/app/*/*",
    ]

    condition {
      test     = "StringEquals"
      variable = "elasticloadbalancing:CreateAction"
      values = [
        "CreateTargetGroup",
        "CreateLoadBalancer",
      ]
    }

    condition {
      test     = "Null"
      variable = "aws:RequestTag/elbv2.k8s.aws/cluster"
      values   = ["false"]
    }
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:RegisterTargets",
      "elasticloadbalancing:DeregisterTargets",
    ]
    resources = ["arn:aws:elasticloadbalancing:*:*:targetgroup/*/*"]
  }

  statement {
    effect = "Allow"
    actions = [
      "elasticloadbalancing:SetWebAcl",
      "elasticloadbalancing:ModifyListener",
      "elasticloadbalancing:AddListenerCertificates",
      "elasticloadbalancing:RemoveListenerCertificates",
      "elasticloadbalancing:ModifyRule",
      "elasticloadbalancing:SetRulePriorities",
    ]
    resources = ["*"]
  }
}

module "aws_load_balancer_controller_irsa" {
  source = "../../modules/iam-irsa"

  role_name            = "AmazonEKSLoadBalancerControllerRole"
  policy_name          = "AWSLoadBalancerControllerIAMPolicy"
  policy_json          = data.aws_iam_policy_document.aws_load_balancer_controller_irsa.json
  oidc_provider        = module.eks.oidc_provider
  oidc_provider_arn    = module.eks.oidc_provider_arn
  namespace            = "kube-system"
  service_account_name = "aws-load-balancer-controller"
  tags                 = local.common_tags
}

module "rds" {
  source = "../../modules/rds"

  identifier                            = var.db_identifier
  database_name                         = var.db_name
  engine_version                        = var.db_engine_version
  instance_class                        = var.db_instance_class
  allocated_storage                     = var.db_allocated_storage
  max_allocated_storage                 = var.db_max_allocated_storage
  multi_az                              = var.db_multi_az
  backup_retention_period               = var.db_backup_retention_period
  deletion_protection                   = var.db_deletion_protection
  apply_immediately                     = true
  subnet_ids                            = module.vpc.private_subnet_ids
  vpc_id                                = module.vpc.vpc_id
  allowed_cidr_blocks                   = [var.vpc_cidr]
  cluster_security_group_id             = module.eks.cluster_security_group_id
  enable_cluster_security_group_ingress = true
  tags                                  = local.common_tags
}

module "elasticache" {
  source = "../../modules/elasticache"

  replication_group_id                  = var.cache_replication_group_id
  description                           = "Redis cache for BodyBuddy dev"
  node_type                             = var.cache_node_type
  engine_version                        = var.cache_engine_version
  num_cache_clusters                    = 1
  subnet_ids                            = module.vpc.private_subnet_ids
  vpc_id                                = module.vpc.vpc_id
  allowed_cidr_blocks                   = [var.vpc_cidr]
  cluster_security_group_id             = module.eks.cluster_security_group_id
  enable_cluster_security_group_ingress = true
  apply_immediately                     = true
  automatic_failover_enabled            = false
  multi_az_enabled                      = false
  tags                                  = local.common_tags
}

data "aws_iam_policy_document" "analysis_worker_irsa" {
  statement {
    sid    = "AnalysisQueueAccess"
    effect = "Allow"
    actions = [
      "sqs:ChangeMessageVisibility",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:GetQueueUrl",
      "sqs:ReceiveMessage",
    ]
    resources = [module.sqs.analysis_queue_arn]
  }

  statement {
    sid    = "InbodyBucketRead"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:HeadObject",
      "s3:ListBucket",
    ]
    resources = [
      module.s3.bucket_arn,
      "${module.s3.bucket_arn}/*",
    ]
  }
}

data "aws_iam_policy_document" "notification_worker_irsa" {
  statement {
    sid    = "NotificationQueueAccess"
    effect = "Allow"
    actions = [
      "sqs:ChangeMessageVisibility",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:GetQueueUrl",
      "sqs:ReceiveMessage",
    ]
    resources = [module.sqs.notification_queue_arn]
  }

  statement {
    sid    = "SendSesEmail"
    effect = "Allow"
    actions = [
      "ses:SendEmail",
      "ses:SendRawEmail",
    ]
    resources = ["*"]
  }
}

module "analysis_worker_irsa" {
  source = "../../modules/iam-irsa"

  role_name            = "${local.name_prefix}-analysis-worker-irsa"
  policy_name          = "${local.name_prefix}-analysis-worker-irsa"
  policy_json          = data.aws_iam_policy_document.analysis_worker_irsa.json
  oidc_provider        = module.eks.oidc_provider
  oidc_provider_arn    = module.eks.oidc_provider_arn
  namespace            = local.app_namespace
  service_account_name = "analysis-worker"
  tags                 = local.common_tags
}

module "notification_worker_irsa" {
  source = "../../modules/iam-irsa"

  role_name            = "${local.name_prefix}-notification-worker-irsa"
  policy_name          = "${local.name_prefix}-notification-worker-irsa"
  policy_json          = data.aws_iam_policy_document.notification_worker_irsa.json
  oidc_provider        = module.eks.oidc_provider
  oidc_provider_arn    = module.eks.oidc_provider_arn
  namespace            = local.app_namespace
  service_account_name = "notification-worker"
  tags                 = local.common_tags
}
