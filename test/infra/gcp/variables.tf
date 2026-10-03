variable "bucket_suffix" {
  description = "Unique suffix appended to the bucket name to prevent collisions across parallel runs."
  type        = string
}

variable "location" {
  description = "GCS bucket location (multi-region, dual-region, or region)."
  type        = string
  default     = "US"
}

variable "project" {
  description = "GCP project ID that owns the bucket."
  type        = string
}
