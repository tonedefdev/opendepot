---
tags:
  - providers
  - consuming
  - guides
---

# Consuming Providers

Once providers are synced, declare them as required providers in your OpenTofu or Terraform configuration using the `<registry-host>/<namespace>/<name>` source format:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "opendepot.defdev.io/opendepot-system/aws"
      version = "~> 5.80"
    }
    azurerm = {
      source  = "opendepot.defdev.io/opendepot-system/azurerm"
      version = ">= 4.0.0"
    }
  }
}
```

The source format is `<registry-host>/<namespace>/<name>`, where `<namespace>` is the Kubernetes namespace where the `Provider` resource lives and `<name>` matches `spec.providerConfig.name` (or the `Provider` resource name if `name` is omitted).

**Pointing OpenTofu at the provider registry**

Because OpenDepot serves providers at a custom host, you need a `host` block in your `.tofurc` or `.terraformrc` to tell OpenTofu where the `providers.v1` API lives:

```
host "opendepot.defdev.io" {
  services = {
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
}
```

Authenticate with OIDC (recommended):

```bash
tofu login opendepot.defdev.io
tofu init
```

Or if you already have an OIDC access token, use the `.tofurc` config approach:

```hcl
credentials "opendepot.defdev.io" {
  token = "<oidc-access-token>"
}

host "opendepot.defdev.io" {
  services = {
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
}
```

Kubernetes bearer token fallback (use only when OIDC is not available):

```bash
export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
  --cluster-name my-cluster \
  --region us-west-2 \
  --output json | jq -r '.status.token')

tofu init
```

!!! note
    Provider artifact downloads (the binary, `SHA256SUMS`, and `SHA256SUMS.sig`) do not require client authentication. OpenTofu fetches these URLs after receiving the download metadata from the auth-protected `download` endpoint, and the Terraform Provider Registry Protocol does not forward credentials to artifact URLs. The server uses its own ServiceAccount for these requests. Security is enforced at the metadata tier where the download URL is issued.

## Next Steps for Admins

For provider publishing and lifecycle operations (adding versions, force re-sync, source repository overrides), use [Registry Operations](operations.md).

For scan configuration and policy controls, see [Vulnerability Scanning](../configuration/scanning.md).

For pre-signed redirects and storage backends, see [Storage Backends](../storage.md#pre-signed-url-redirects).
