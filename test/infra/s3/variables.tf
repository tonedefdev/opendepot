variable "bucket_suffix" {
  description = "Unique suffix appended to the bucket name to ensure global uniqueness."
  type        = string
}

variable "region" {
  description = "AWS region in which to create the bucket."
  type        = string
  default     = "us-east-1"
}
