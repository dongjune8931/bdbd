variable "function_name" {
  description = "Name of the S3 auto recovery Lambda function."
  type        = string
}

variable "bucket_name" {
  description = "S3 bucket name to protect."
  type        = string
}

variable "bucket_arn" {
  description = "S3 bucket ARN to protect."
  type        = string
}

variable "package_path" {
  description = "Path to the Lambda deployment zip."
  type        = string
}

variable "metric_namespace" {
  description = "CloudWatch metric namespace for DR recovery metrics."
  type        = string
  default     = "BodyBuddy/DR"
}

variable "memory_size" {
  description = "Lambda memory size in MB."
  type        = number
  default     = 128
}

variable "timeout" {
  description = "Lambda timeout in seconds."
  type        = number
  default     = 30
}

variable "tags" {
  description = "Common tags applied to resources."
  type        = map(string)
  default     = {}
}
