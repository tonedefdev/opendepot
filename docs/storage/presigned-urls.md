---
tags:
  - storage
  - presigned-urls
  - providers
---

# Pre-signed URL Redirects

When enabled for provider downloads, the Server returns an HTTP `307 Temporary
Redirect` to a time-limited URL signed by the storage backend. OpenTofu or
Terraform then downloads the provider directly, reducing bandwidth and egress
through the Server.

Pre-signed URLs are supported for Amazon S3, Google Cloud Storage, and Azure
Blob Storage. The filesystem backend does not support them.

## Configuration

Configure `presign` on any `StorageConfig`:

```yaml
storageConfig:
  s3:
    bucket: opendepot-providers
    region: us-east-1
  presign:
    enabled: true
    ttl: "15m"
    fallbackToProxy: true
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Redirect downloads to the storage backend |
| `ttl` | duration | `15m` | URL validity, such as `"15m"` or `"1h"` |
| `fallbackToProxy` | bool | `true` | Proxy through the Server if signing fails; set to `false` to require signing |

## Backend Permissions

### Amazon S3

No additional IAM permissions are needed. `s3:GetObject`, already required for
the proxy path, is sufficient to generate signed `GET` URLs.

### Azure Blob Storage

Azure uses user delegation SAS tokens. Assign the `Storage Blob Delegator` role
to the controller's identity in addition to `Storage Blob Data Contributor`.

### Google Cloud Storage

GCS uses HMAC/V4 signing. The workload identity requires
`iam.serviceAccounts.signBlob`, included in the `Service Account Token Creator`
role, in addition to the existing storage permissions.
