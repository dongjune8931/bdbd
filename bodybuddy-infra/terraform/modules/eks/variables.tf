variable "cluster_name" {
  description = "Name of the EKS cluster."
  type        = string
}

variable "cluster_version" {
  description = "Kubernetes version for the EKS cluster."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID where the cluster will run."
  type        = string
}

variable "subnet_ids" {
  description = "Private subnet IDs used by the cluster and node groups."
  type        = list(string)
}

variable "control_plane_subnet_ids" {
  description = "Subnet IDs used by the EKS control plane."
  type        = list(string)
}

variable "endpoint_public_access" {
  description = "Whether the EKS public endpoint is enabled."
  type        = bool
}

variable "endpoint_private_access" {
  description = "Whether the EKS private endpoint is enabled."
  type        = bool
}

variable "cluster_enabled_log_types" {
  description = "Control plane log types to enable."
  type        = list(string)
}

variable "bootstrap_instance_types" {
  description = "Managed node group instance types for the bootstrap node group."
  type        = list(string)
}

variable "bootstrap_desired_size" {
  description = "Desired size for the bootstrap managed node group."
  type        = number
}

variable "bootstrap_min_size" {
  description = "Minimum size for the bootstrap managed node group."
  type        = number
}

variable "bootstrap_max_size" {
  description = "Maximum size for the bootstrap managed node group."
  type        = number
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
