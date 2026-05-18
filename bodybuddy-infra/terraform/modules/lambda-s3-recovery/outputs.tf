output "function_name" {
  description = "S3 auto recovery Lambda function name."
  value       = aws_lambda_function.this.function_name
}

output "function_arn" {
  description = "S3 auto recovery Lambda function ARN."
  value       = aws_lambda_function.this.arn
}

output "event_rule_name" {
  description = "EventBridge rule name for S3 delete events."
  value       = aws_cloudwatch_event_rule.s3_object_deleted.name
}

output "log_group_name" {
  description = "CloudWatch log group name for the S3 auto recovery Lambda."
  value       = aws_cloudwatch_log_group.this.name
}
