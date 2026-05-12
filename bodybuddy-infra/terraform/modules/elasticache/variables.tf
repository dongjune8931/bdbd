variable "replication_group_id" {
  description = "Replication group ID."
  type        = string
}

variable "description" {
  description = "Description for the replication group."
  type        = string
}

variable "node_type" {
  description = "ElastiCache node type."
  type        = string
}

variable "engine_version" {
  description = "Redis OSS engine version."
  type        = string
}

variable "num_cache_clusters" {
  description = "Number of cache clusters in the replication group."
  type        = number
}

variable "subnet_ids" {
  description = "Private subnet IDs for the cache subnet group."
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID where the cache security group will live."
  type        = string
}

variable "allowed_cidr_blocks" {
  description = "CIDR blocks allowed to reach Redis."
  type        = list(string)
}

variable "cluster_security_group_id" {
  description = "Optional EKS cluster security group allowed to reach Redis."
  type        = string
  default     = null
}

variable "apply_immediately" {
  description = "Whether modifications are applied immediately."
  type        = bool
}

variable "automatic_failover_enabled" {
  description = "Whether automatic failover is enabled."
  type        = bool
}

variable "multi_az_enabled" {
  description = "Whether Multi-AZ is enabled."
  type        = bool
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
