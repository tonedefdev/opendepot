output "bucket_name" {
  description = "Name of the S3 bucket created for integration testing."
  value       = aws_s3_bucket.integration.bucket
}
