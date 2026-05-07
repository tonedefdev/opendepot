variable "bucket_suffix" {
  type        = string
  description = "Unique suffix appended to the bucket name to prevent collisions across parallel runs."
}

variable "location" {
  type        = string
  description = "GCS bucket location (multi-region, dual-region, or region)."
  default     = "US"
}

variable "project" {
  type        = string
  description = "GCP project ID that owns the bucket."
}
