output "vpc_id" {
  description = "VPC ID for the dev environment."
  value       = module.vpc.vpc_id
}

output "private_subnet_ids" {
  description = "Private subnet IDs for the dev environment."
  value       = module.vpc.private_subnet_ids
}

output "public_subnet_ids" {
  description = "Public subnet IDs for the dev environment."
  value       = module.vpc.public_subnet_ids
}

output "s3_bucket_name" {
  description = "S3 bucket used for inbody uploads."
  value       = module.s3.bucket_name
}

output "analysis_queue_url" {
  description = "Analysis queue URL."
  value       = module.sqs.analysis_queue_url
}

output "notification_queue_url" {
  description = "Notification queue URL."
  value       = module.sqs.notification_queue_url
}

output "ecr_repository_urls" {
  description = "Repository URLs keyed by logical service name."
  value       = module.ecr.repository_urls
}

output "eks_cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "EKS cluster endpoint."
  value       = module.eks.cluster_endpoint
}

output "eks_oidc_provider_arn" {
  description = "OIDC provider ARN for IRSA."
  value       = module.eks.oidc_provider_arn
}

output "karpenter_controller_iam_role_arn" {
  description = "IAM role ARN for the Karpenter controller."
  value       = module.karpenter.controller_iam_role_arn
}

output "karpenter_node_iam_role_arn" {
  description = "IAM role ARN used by Karpenter-managed nodes."
  value       = module.karpenter.node_iam_role_arn
}

output "karpenter_interruption_queue_url" {
  description = "Interruption queue URL for Karpenter."
  value       = module.karpenter.interruption_queue_url
}

output "rds_endpoint" {
  description = "RDS PostgreSQL endpoint."
  value       = module.rds.endpoint
}

output "rds_master_user_secret_arn" {
  description = "Secrets Manager ARN for the RDS managed master password."
  value       = module.rds.master_user_secret_arn
}

output "elasticache_primary_endpoint_address" {
  description = "ElastiCache primary endpoint address."
  value       = module.elasticache.primary_endpoint_address
}
