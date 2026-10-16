---
tags:
  - modules
  - consuming
  - guides
---

# Consuming Modules

Once modules are synced, reference them in your OpenTofu configuration:

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

The source format is `<registry-host>/<namespace>/<name>/<provider>`:

- `<namespace>` is the Kubernetes namespace of the `Module` resource.
- `<name>` is the name of the `Module` resource.
- `<provider>` is the provider name, such as `aws`.

## Authenticate

`tofu login` requires OIDC enabled on the server (`server.oidc.enabled: true`). See [OIDC via Dex](../authentication/oidc.md).

```bash
tofu login opendepot.defdev.io
tofu init
```

If OIDC is not enabled, see [Authentication](../authentication/index.md).

!!! note "Host mapping fallback"
    OpenTofu discovers the module API from `/.well-known/terraform.json`. Only add this host block to your `.tofurc` if discovery does not resolve for your host.

    ```hcl
    host "opendepot.defdev.io" {
      services = {
        "modules.v1" = "https://opendepot.defdev.io/opendepot/modules/v1/"
      }
    }
    ```

## Module Fields

For `Module` spec fields such as `moduleConfig`, `versions`, and `forceSync`, see the [API Reference](../reference/api.md).

## Next Steps for Admins

- [Registry Operations](operations.md): create `Module` resources, add inline `Version` config, and force re-sync.
- [Vulnerability Scanning](../configuration/scanning.md): scan configuration and policy controls.
