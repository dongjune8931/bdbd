variable "environment" {
  description = "Deployment environment."
  type        = string
}

variable "name" {
  description = "Name of the VPC."
  type        = string
}

variable "cidr" {
  description = "CIDR block for the VPC."
  type        = string
}

variable "azs" {
  description = "Availability zones to use."
  type        = list(string)
}

variable "public_subnets" {
  description = "CIDR blocks for public subnets."
  type        = list(string)
}

variable "private_subnets" {
  description = "CIDR blocks for private subnets."
  type        = list(string)
}

variable "single_nat_gateway" {
  description = "Whether to use a single NAT gateway in dev."
  type        = bool
}

variable "enable_dns_hostnames" {
  description = "Whether to enable DNS hostnames."
  type        = bool
}

variable "enable_dns_support" {
  description = "Whether to enable DNS support."
  type        = bool
}

variable "tags" {
  description = "Common tags applied to all resources."
  type        = map(string)
}

variable "private_subnet_tags" {
  description = "Additional tags applied only to private subnets."
  type        = map(string)
  default     = {}
}
