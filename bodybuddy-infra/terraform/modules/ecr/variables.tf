variable "environment" {
  description = "Deployment environment."
  type        = string
}

variable "repositories" {
  description = "Logical application repository names."
  type        = list(string)
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}
