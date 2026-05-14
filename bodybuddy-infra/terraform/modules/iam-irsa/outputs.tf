output "role_arn" {
  description = "IAM role ARN for the service account."
  value       = aws_iam_role.this.arn
}

output "role_name" {
  description = "IAM role name for the service account."
  value       = aws_iam_role.this.name
}

output "policy_arn" {
  description = "IAM policy ARN attached to the service account role."
  value       = aws_iam_policy.this.arn
}
