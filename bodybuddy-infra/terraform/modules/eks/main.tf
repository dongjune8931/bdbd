module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.37"

  cluster_name    = var.cluster_name
  cluster_version = var.cluster_version

  vpc_id     = var.vpc_id
  subnet_ids = var.subnet_ids

  control_plane_subnet_ids = var.control_plane_subnet_ids

  cluster_endpoint_public_access  = var.endpoint_public_access
  cluster_endpoint_private_access = var.endpoint_private_access

  enable_cluster_creator_admin_permissions = true
  enable_irsa                              = true

  cluster_enabled_log_types = var.cluster_enabled_log_types

  cluster_addons = {
    coredns = {
      most_recent = true
    }
    kube-proxy = {
      most_recent = true
    }
    vpc-cni = {
      most_recent = true
    }
  }

  eks_managed_node_groups = {
    bootstrap = {
      name           = "bootstrap"
      instance_types = var.bootstrap_instance_types
      capacity_type  = "ON_DEMAND"

      min_size     = var.bootstrap_min_size
      max_size     = var.bootstrap_max_size
      desired_size = var.bootstrap_desired_size

      labels = {
        "app.kubernetes.io/part-of" = "bodybuddy"
        "bodybuddy.io/nodepool"     = "critical"
        "karpenter.sh/discovery"    = var.cluster_name
      }

      tags = merge(var.tags, {
        Name                     = "${var.cluster_name}-bootstrap"
        "karpenter.sh/discovery" = var.cluster_name
      })
    }
  }

  tags = merge(var.tags, {
    Name                     = var.cluster_name
    "karpenter.sh/discovery" = var.cluster_name
  })
}
