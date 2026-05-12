output "replication_group_id" {
  description = "Replication group ID."
  value       = aws_elasticache_replication_group.this.replication_group_id
}

output "primary_endpoint_address" {
  description = "Primary endpoint address."
  value       = aws_elasticache_replication_group.this.primary_endpoint_address
}

output "reader_endpoint_address" {
  description = "Reader endpoint address."
  value       = aws_elasticache_replication_group.this.reader_endpoint_address
}

output "port" {
  description = "Redis port."
  value       = aws_elasticache_replication_group.this.port
}

output "security_group_id" {
  description = "Security group protecting Redis."
  value       = aws_security_group.this.id
}
