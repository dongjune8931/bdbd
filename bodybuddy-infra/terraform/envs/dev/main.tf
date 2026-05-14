provider "aws" {
  region = var.aws_region
}

locals {
  project     = "bodybuddy"
  name_prefix = "${local.project}-${var.environment}"
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

module "karpenter" {
  source = "../../modules/karpenter"

  cluster_name              = module.eks.cluster_name
  cluster_oidc_provider     = module.eks.oidc_provider
  cluster_oidc_provider_arn = module.eks.oidc_provider_arn
  tags                      = local.common_tags
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
