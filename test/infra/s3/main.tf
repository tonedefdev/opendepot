terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.region
}

resource "aws_s3_bucket" "integration" {
  bucket        = "opendepot-integration-${var.bucket_suffix}"
  force_destroy = true

  tags = {
    Purpose   = "opendepot-storage-integration-tests"
    ManagedBy = "opentofu"
  }
}

resource "aws_s3_bucket_public_access_block" "integration" {
  bucket = aws_s3_bucket.integration.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "integration" {
  bucket = aws_s3_bucket.integration.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}
