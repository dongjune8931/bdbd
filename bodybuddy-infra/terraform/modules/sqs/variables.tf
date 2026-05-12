variable "environment" {
  description = "Deployment environment."
  type        = string
}

variable "analysis_queue_name" {
  description = "Name of the analysis queue."
  type        = string
}

variable "notification_queue_name" {
  description = "Name of the notification queue."
  type        = string
}

variable "max_receive_count" {
  description = "Maximum receives before sending a message to the DLQ."
  type        = number
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
