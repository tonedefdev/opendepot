---
tags:
  - storage
  - azure
---

# Azure Blob Storage

Azure Blob Storage is recommended for production deployments. It stores module
archives and provider binaries with checksum metadata.

## Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `accountName` | string | Yes | Azure Storage Account name |
| `accountUrl` | string | Yes | Storage Account URL |
| `subscriptionID` | string | Yes | Azure subscription ID |
| `resourceGroup` | string | Yes | Resource Group containing the Storage Account |

```yaml
storageConfig:
  azureStorage:
    accountName: opendepotmodules
    accountUrl: https://opendepotmodules.blob.core.windows.net
    subscriptionID: 00000000-0000-0000-0000-000000000000
    resourceGroup: opendepot-rg
```

## Authentication

OpenDepot uses [Azure DefaultAzureCredential](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication). Common Kubernetes deployments use:

- **AKS with Workload Identity:** Configure federated identity credentials on
  the Version controller's ServiceAccount. This is the recommended option.
- **Managed Identity:** Assign a managed identity to the AKS node pool or pod.
- **Environment variables:** Set `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and
  `AZURE_CLIENT_SECRET`.

## Azure RBAC Roles

Assign these roles on the Storage Account:

- `Storage Blob Data Contributor` for read, write, and delete operations.
- `Reader` for container metadata operations.
- `Storage Blob Delegator` when pre-signed URLs are enabled, to obtain a User
  Delegation Key for SAS generation.

See [Pre-signed URL Redirects](presigned-urls.md) for configuration.
