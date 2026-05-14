variable "identifier" {
  description = "Database identifier."
  type        = string
}

variable "database_name" {
  description = "Initial PostgreSQL database name."
  type        = string
}

variable "engine_version" {
  description = "PostgreSQL engine major or minor version."
  type        = string
}

variable "instance_class" {
  description = "RDS instance class."
  type        = string
}

variable "allocated_storage" {
  description = "Initial storage size in GiB."
  type        = number
}

variable "max_allocated_storage" {
  description = "Maximum storage autoscaling size in GiB."
  type        = number
}

variable "multi_az" {
  description = "Whether Multi-AZ is enabled."
  type        = bool
}

variable "backup_retention_period" {
  description = "Backup retention period in days."
  type        = number
}

variable "deletion_protection" {
  description = "Whether deletion protection is enabled."
  type        = bool
}

variable "apply_immediately" {
  description = "Whether modifications are applied immediately."
  type        = bool
}

variable "subnet_ids" {
  description = "Private subnet IDs for the DB subnet group."
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID where the DB security group will live."
  type        = string
}

variable "allowed_cidr_blocks" {
  description = "CIDR blocks allowed to reach PostgreSQL."
  type        = list(string)
}

variable "cluster_security_group_id" {
  description = "Optional EKS cluster security group allowed to reach PostgreSQL."
  type        = string
  default     = null
}

variable "enable_cluster_security_group_ingress" {
  description = "Whether to allow ingress from the EKS cluster security group."
  type        = bool
  default     = false
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
