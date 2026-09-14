---
tags:
  - storage
  - configuration
---

# Storage Backends

OpenDepot stores module archives and provider binaries in a configurable storage
backend. Choose the backend that matches your deployment environment.

<div class="grid cards" markdown>

- :material-aws: &nbsp;[__Amazon S3__](s3.md)

    ---

    Production object storage using AWS SDK credentials and optional pre-signed
    provider downloads.

- :material-microsoft-azure: &nbsp;[__Azure Blob Storage__](azure.md)

    ---

    Production object storage using Azure identity and optional user delegation
    SAS URLs.

- :material-google-cloud: &nbsp;[__Google Cloud Storage__](gcs.md)

    ---

    Production object storage using Application Default Credentials and optional
    signed provider URLs.

- :material-harddisk: &nbsp;[__Local Filesystem__](filesystem.md)

    ---

    Shared-volume storage for development, testing, and air-gapped environments.

- :material-link-variant: &nbsp;[__Pre-signed URL Redirects__](presigned-urls.md)

    ---

    Let clients download provider binaries directly from object storage instead
    of proxying them through the Server.

</div>

## Storage Configuration Scope

`storageConfig` can be set at multiple levels:

- `Depot.spec.globalConfig.storageConfig` applies to resources managed by a
  Depot unless overridden.
- `Module.spec.moduleConfig.storageConfig` configures a module.
- `Provider.spec.providerConfig.storageConfig` configures a provider.
- Inline `Version.spec.moduleConfigRef.storageConfig` or
  `Version.spec.providerConfigRef.storageConfig` configures a version.

## Backend Comparison

| Feature | Amazon S3 | Azure Blob | Google Cloud Storage | Filesystem |
|---------|-----------|------------|---------------------|------------|
| Production ready | Yes | Yes | Yes | With PVC |
| Checksum validation | SHA256 (native) | SHA256 (metadata) | SHA256 (metadata) | SHA256 (computed) |
| Authentication | AWS SDK v2 defaults | DefaultAzureCredential | ADC | None |
| Server download route | Yes | Yes | Yes | Yes |
| Shared volume required | No | No | No | Yes (PVC or hostPath) |
| Pre-signed URL redirects | Yes | Yes | Yes | No |
