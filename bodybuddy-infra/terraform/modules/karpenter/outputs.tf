output "controller_iam_role_arn" {
  description = "IAM role ARN for the Karpenter controller."
  value       = aws_iam_role.controller.arn
}

output "controller_iam_policy_arn" {
  description = "IAM policy ARN for the Karpenter controller."
  value       = aws_iam_policy.controller.arn
}

output "node_iam_role_arn" {
  description = "IAM role ARN used by Karpenter-launched nodes."
  value       = aws_iam_role.node.arn
}

output "node_instance_profile_name" {
  description = "IAM instance profile name used by Karpenter-launched nodes."
  value       = aws_iam_instance_profile.node.name
}

output "interruption_queue_name" {
  description = "SQS queue name used by Karpenter interruption handling."
  value       = aws_sqs_queue.interruption.name
}

output "interruption_queue_url" {
  description = "SQS queue URL used by Karpenter interruption handling."
  value       = aws_sqs_queue.interruption.url
}
