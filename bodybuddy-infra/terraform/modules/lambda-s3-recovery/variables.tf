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

variable "alert_email_from" {
  description = "Verified SES sender email used for S3 DR recovery notifications. Null disables email delivery."
  type        = string
  default     = null
}

variable "alert_email_to" {
  description = "List of recipient emails for S3 DR recovery notifications."
  type        = list(string)
  default     = []
}

variable "recovery_delete_ratio_threshold" {
  description = "Deletion ratio percentage threshold included in DR metrics and notification emails."
  type        = number
  default     = 3
}

variable "tags" {
  description = "Common tags applied to resources."
  type        = map(string)
  default     = {}
}
