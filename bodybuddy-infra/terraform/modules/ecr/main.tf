locals {
  repository_names = {
    for repo in var.repositories :
    repo => "bodybuddy-${var.environment}-${repo}"
  }
}

resource "aws_ecr_repository" "this" {
  for_each = local.repository_names

  name                 = each.value
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = false
  }

  tags = merge(var.tags, {
    Name = each.value
  })
}
