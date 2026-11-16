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
| `fallbackToProxy` | bool | `true` | Proxy through the Server if signing fails. When `false`, a signing failure returns `502 Bad Gateway`. |

## Agent Archives

Agent archives read the same `presign` fields from the agent source `storageConfig`. The behavior differs in three ways:

- `fallbackToProxy` defaults to `false` for new Agent objects. When `false`, a signing failure returns `500 Internal Server Error` and does not redirect. Providers and modules keep the `true` default.
- The FileSystem backend always fails the signing step. Agent downloads from FileSystem storage stream the archive only when `fallbackToProxy` is `true`.
- Existing Agent objects keep the stored `fallbackToProxy` value. The changed default applies only to objects created after the change.

A presigned URL redirect uses `307 Temporary Redirect` with `Cache-Control: private, no-store`. The URL TTL defaults to `15m`. Revoking a GroupBinding stops new redirects, but a URL already issued stays valid until it expires. This is an accepted window. For the authorization flow, see [agents.v1 Protocol](../reference/agents-v1.md).

## Backend Permissions

### Amazon S3

No additional IAM permissions are needed. `s3:GetObject`, already required for
the proxy path, is sufficient to generate signed `GET` URLs.

### Azure Blob Storage

Azure uses user delegation SAS tokens. Assign the `Storage Blob Delegator` role
to the controller's identity in addition to `Storage Blob Data Contributor`.

### Google Cloud Storage

GCS signs URLs with the Go SDK's default V2 scheme. When the workload identity
has no private key, the signing call goes through IAM, which requires
`iam.serviceAccounts.signBlob`, included in the `Service Account Token Creator`
role, in addition to the existing storage permissions.
