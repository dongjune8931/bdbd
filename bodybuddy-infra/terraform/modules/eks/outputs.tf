output "cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "cluster_version" {
  description = "EKS cluster version."
  value       = module.eks.cluster_version
}

output "cluster_endpoint" {
  description = "EKS cluster endpoint."
  value       = module.eks.cluster_endpoint
}

output "cluster_security_group_id" {
  description = "EKS cluster security group ID."
  value       = module.eks.cluster_security_group_id
}

output "oidc_provider" {
  description = "OIDC provider identifier used for IRSA."
  value       = module.eks.oidc_provider
}

output "oidc_provider_arn" {
  description = "OIDC provider ARN used for IRSA."
  value       = module.eks.oidc_provider_arn
}

output "bootstrap_node_group_name" {
  description = "Bootstrap managed node group name."
  value       = module.eks.eks_managed_node_groups["bootstrap"].node_group_name
}

output "bootstrap_node_group_iam_role_arn" {
  description = "IAM role ARN for the bootstrap managed node group."
  value       = module.eks.eks_managed_node_groups["bootstrap"].iam_role_arn
}
