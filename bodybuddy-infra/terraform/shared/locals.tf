locals {
  project     = "bodybuddy"
  aws_region  = "ap-northeast-2"
  owner       = "dongjune8931"
  name_prefix = "${local.project}-${var.environment}"

  common_tags = {
    Environment = var.environment
    Project     = local.project
    ManagedBy   = "terraform"
    Owner       = local.owner
  }
}
