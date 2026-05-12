output "analysis_queue_url" {
  description = "Analysis queue URL."
  value       = aws_sqs_queue.analysis.url
}

output "analysis_queue_arn" {
  description = "Analysis queue ARN."
  value       = aws_sqs_queue.analysis.arn
}

output "notification_queue_url" {
  description = "Notification queue URL."
  value       = aws_sqs_queue.notification.url
}

output "notification_queue_arn" {
  description = "Notification queue ARN."
  value       = aws_sqs_queue.notification.arn
}
