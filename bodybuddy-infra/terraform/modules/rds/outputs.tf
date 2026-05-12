output "identifier" {
  description = "Database identifier."
  value       = aws_db_instance.this.identifier
}

output "endpoint" {
  description = "PostgreSQL endpoint."
  value       = aws_db_instance.this.endpoint
}

output "address" {
  description = "PostgreSQL address."
  value       = aws_db_instance.this.address
}

output "port" {
  description = "PostgreSQL port."
  value       = aws_db_instance.this.port
}

output "security_group_id" {
  description = "Security group protecting the DB."
  value       = aws_security_group.this.id
}

output "master_user_secret_arn" {
  description = "Secrets Manager ARN for the managed master user password."
  value       = aws_db_instance.this.master_user_secret[0].secret_arn
}
