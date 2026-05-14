variable "role_name" {
  description = "Name of the IRSA role."
  type        = string
}

variable "policy_name" {
  description = "Name of the IAM policy attached to the IRSA role."
  type        = string
}

variable "policy_json" {
  description = "Rendered IAM policy JSON attached to the IRSA role."
  type        = string
}

variable "oidc_provider" {
  description = "OIDC provider identifier used in trust policy conditions."
  type        = string
}

variable "oidc_provider_arn" {
  description = "OIDC provider ARN used in the IRSA trust policy."
  type        = string
}

variable "namespace" {
  description = "Kubernetes namespace of the service account."
  type        = string
}

variable "service_account_name" {
  description = "Kubernetes service account name that will assume the role."
  type        = string
}

variable "tags" {
  description = "Tags applied to IAM resources."
  type        = map(string)
  default     = {}
}
