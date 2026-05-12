variable "environment" {
  description = "Deployment environment."
  type        = string
}

variable "bucket_name" {
  description = "S3 bucket name."
  type        = string
}

variable "kms_key_arn" {
  description = "Optional KMS key ARN for SSE-KMS."
  type        = string
  default     = null
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
