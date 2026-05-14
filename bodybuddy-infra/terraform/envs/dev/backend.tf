terraform {
  backend "s3" {
    bucket         = "bodybuddy-tfstate-902371998304"
    key            = "envs/dev/terraform.tfstate"
    region         = "ap-northeast-2"
    dynamodb_table = "bodybuddy-tflock"
    encrypt        = true
  }
}
