variable "environment" {
  description = "Deployment environment name."
  type        = string
  default     = "dev"
}

variable "aws_region" {
  description = "AWS region for the dev environment."
  type        = string
  default     = "ap-northeast-2"
}

variable "availability_zones" {
  description = "Availability zones used by the VPC."
  type        = list(string)
  default     = ["ap-northeast-2a", "ap-northeast-2c"]
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC."
  type        = string
  default     = "10.20.0.0/16"
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets."
  type        = list(string)
  default     = ["10.20.0.0/24", "10.20.1.0/24"]
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets."
  type        = list(string)
  default     = ["10.20.10.0/24", "10.20.11.0/24"]
}

variable "single_nat_gateway" {
  description = "Whether to provision a single NAT gateway for dev."
  type        = bool
  default     = true
}

variable "cluster_name" {
  description = "Name of the EKS cluster."
  type        = string
  default     = "bodybuddy-dev-eks"
}

variable "cluster_version" {
  description = "Kubernetes version for the EKS cluster."
  type        = string
  default     = "1.33"
}

variable "cluster_endpoint_public_access" {
  description = "Whether to enable the public EKS API endpoint."
  type        = bool
  default     = true
}

variable "cluster_endpoint_private_access" {
  description = "Whether to enable the private EKS API endpoint."
  type        = bool
  default     = true
}

variable "cluster_enabled_log_types" {
  description = "Control plane log types enabled for EKS."
  type        = list(string)
  default     = ["api", "audit", "authenticator"]
}

variable "bootstrap_node_instance_types" {
  description = "Bootstrap managed node group instance types."
  type        = list(string)
  default     = ["t3.medium"]
}

variable "bootstrap_node_desired_size" {
  description = "Desired size of the bootstrap managed node group."
  type        = number
  default     = 1
}

variable "bootstrap_node_min_size" {
  description = "Minimum size of the bootstrap managed node group."
  type        = number
  default     = 1
}

variable "bootstrap_node_max_size" {
  description = "Maximum size of the bootstrap managed node group."
  type        = number
  default     = 2
}

variable "ecr_repositories" {
  description = "Per-service ECR repositories."
  type        = list(string)
  default = [
    "user-service",
    "score-service",
    "analysis-worker",
    "notification-worker",
  ]
}

variable "s3_bucket_name" {
  description = "S3 bucket for inbody uploads."
  type        = string
  default     = "bodybuddy-dev-inbody"
}

variable "sqs_queue_names" {
  description = "Logical queue names for the application."
  type = object({
    analysis     = string
    notification = string
  })
  default = {
    analysis     = "analysis"
    notification = "notification"
  }
}

variable "kms_key_arn" {
  description = "Optional KMS key ARN for S3 encryption. Null uses AES256 for now."
  type        = string
  default     = null
}

variable "db_identifier" {
  description = "RDS instance identifier."
  type        = string
  default     = "bodybuddy-dev-postgres"
}

variable "db_name" {
  description = "Initial PostgreSQL database name."
  type        = string
  default     = "bodybuddy"
}

variable "db_engine_version" {
  description = "PostgreSQL engine version. Major version lets AWS choose a recent supported minor release."
  type        = string
  default     = "16"
}

variable "db_instance_class" {
  description = "RDS instance class for dev."
  type        = string
  default     = "db.t4g.micro"
}

variable "db_allocated_storage" {
  description = "Initial RDS storage size in GiB."
  type        = number
  default     = 20
}

variable "db_max_allocated_storage" {
  description = "Maximum autoscaled RDS storage size in GiB."
  type        = number
  default     = 100
}

variable "db_multi_az" {
  description = "Whether Multi-AZ is enabled for RDS."
  type        = bool
  default     = false
}

variable "db_backup_retention_period" {
  description = "Backup retention period in days."
  type        = number
  default     = 7
}

variable "db_deletion_protection" {
  description = "Whether deletion protection is enabled for RDS."
  type        = bool
  default     = false
}

variable "cache_replication_group_id" {
  description = "ElastiCache replication group ID."
  type        = string
  default     = "bodybuddy-dev-redis"
}

variable "cache_node_type" {
  description = "ElastiCache node type for dev."
  type        = string
  default     = "cache.t4g.micro"
}

variable "cache_engine_version" {
  description = "Redis OSS engine version."
  type        = string
  default     = "7.1"
}

variable "dr_alert_email_from" {
  description = "Verified SES sender email for S3 DR recovery alerts. Null disables email notifications."
  type        = string
  default     = "ldj990517@gmail.com"
}

variable "dr_alert_email_to" {
  description = "Recipient emails for S3 DR recovery alerts."
  type        = list(string)
  default     = ["ldj990517@gmail.com"]
}
