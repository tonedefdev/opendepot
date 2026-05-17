---
tags:
  - modules
  - consuming
  - guides
---

# Consuming Modules

Once modules are synced, reference them in your OpenTofu or Terraform configuration:

```hcl
module "eks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-aws-eks/aws"
  version = "~> 21.0"
}

module "aks" {
  source  = "opendepot.defdev.io/opendepot-system/terraform-azurerm-aks/azurerm"
  version = ">= 10.0.0"
}
```

The source format is `<registry-host>/<namespace>/<name>/<provider>`, where `<namespace>` is the Kubernetes namespace where the `Module` resource lives.

## Authenticate with tofu login (Recommended)

OpenDepot is designed around OIDC for client authentication. For module consumption, the preferred workflow is:

```bash
tofu login opendepot.defdev.io
tofu init
```

If you have not configured a host mapping yet, add this to your `.tofurc` (or `.terraformrc`) so OpenTofu can find the module registry API:

```hcl
host "opendepot.defdev.io" {
  services = {
    "modules.v1" = "https://opendepot.defdev.io/opendepot/modules/v1/"
  }
}
```

`tofu login` stores credentials for subsequent `tofu init` runs, so you do not need to inject bearer tokens into your shell for normal module reads.

## Next Steps for Admins

For module publishing and lifecycle operations (creating `Module` resources, inline `Version` config, force re-sync), use [Registry Operations](operations.md).

For scan configuration and policy controls, see [Vulnerability Scanning](../configuration/scanning.md).
