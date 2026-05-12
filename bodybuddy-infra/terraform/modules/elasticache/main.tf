resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.replication_group_id}-subnet-group"
  subnet_ids = var.subnet_ids

  tags = merge(var.tags, {
    Name = "${var.replication_group_id}-subnet-group"
  })
}

resource "aws_security_group" "this" {
  name        = "${var.replication_group_id}-sg"
  description = "Security group for ${var.replication_group_id} Redis"
  vpc_id      = var.vpc_id

  tags = merge(var.tags, {
    Name = "${var.replication_group_id}-sg"
  })
}

resource "aws_vpc_security_group_ingress_rule" "cidr" {
  for_each = toset(var.allowed_cidr_blocks)

  security_group_id = aws_security_group.this.id
  cidr_ipv4         = each.value
  from_port         = 6379
  to_port           = 6379
  ip_protocol       = "tcp"
  description       = "Redis from ${each.value}"
}

resource "aws_vpc_security_group_ingress_rule" "eks_cluster" {
  count = var.cluster_security_group_id == null ? 0 : 1

  security_group_id            = aws_security_group.this.id
  referenced_security_group_id = var.cluster_security_group_id
  from_port                    = 6379
  to_port                      = 6379
  ip_protocol                  = "tcp"
  description                  = "Redis from EKS cluster security group"
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.this.id
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
  description       = "Allow all outbound traffic"
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id       = var.replication_group_id
  description                = var.description
  node_type                  = var.node_type
  engine                     = "redis"
  engine_version             = var.engine_version
  port                       = 6379
  parameter_group_name       = "default.redis7"
  subnet_group_name          = aws_elasticache_subnet_group.this.name
  security_group_ids         = [aws_security_group.this.id]
  num_cache_clusters         = var.num_cache_clusters
  automatic_failover_enabled = var.automatic_failover_enabled
  multi_az_enabled           = var.multi_az_enabled
  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  apply_immediately          = var.apply_immediately

  tags = merge(var.tags, {
    Name = var.replication_group_id
  })
}
