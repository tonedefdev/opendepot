terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.0"
    }
  }
}

provider "google" {
  project = var.project
}

resource "google_storage_bucket" "integration" {
  force_destroy = true
  location      = var.location
  name          = "opendepot-integration-${var.bucket_suffix}"

  uniform_bucket_level_access = true

  labels = {
    purpose    = "opendepot-storage-integration-tests"
    managed-by = "opentofu"
  }
}
