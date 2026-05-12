variable "cluster_name" {
  description = "Name of the EKS cluster Karpenter will target."
  type        = string
}

variable "cluster_oidc_provider" {
  description = "OIDC provider path without ARN prefix."
  type        = string
}

variable "cluster_oidc_provider_arn" {
  description = "OIDC provider ARN for IRSA."
  type        = string
}

variable "namespace" {
  description = "Namespace where the Karpenter controller will run."
  type        = string
  default     = "karpenter"
}

variable "service_account_name" {
  description = "Service account used by the Karpenter controller."
  type        = string
  default     = "karpenter"
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
