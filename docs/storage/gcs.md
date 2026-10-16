---
tags:
  - storage
  - gcs
  - google-cloud
---

# Google Cloud Storage

Google Cloud Storage stores module archives and provider binaries with checksum
metadata.

## Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bucket` | string | Yes | GCS bucket name |

```yaml
storageConfig:
  gcs:
    bucket: opendepot-modules
```

## Authentication

OpenDepot uses [Application Default Credentials (ADC)](https://cloud.google.com/docs/authentication/application-default-credentials). Common Kubernetes deployments use:

- **GKE with Workload Identity:** Bind a Google service account to the Version
  controller's Kubernetes ServiceAccount. This is the recommended option.
- **Service account key:** Mount a JSON key file and set
  `GOOGLE_APPLICATION_CREDENTIALS`.

## Permissions

The storage identity requires:

- `storage.objects.create` for uploads
- `storage.objects.get` for reads and metadata lookups
- `storage.objects.delete` for removals

The `Storage Object Admin` role includes all three.

See [Pre-signed URL Redirects](presigned-urls.md) for signed provider download
configuration.
