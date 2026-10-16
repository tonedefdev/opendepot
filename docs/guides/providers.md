---
tags:
  - providers
  - consuming
  - guides
---

# Consuming Providers

OpenDepot implements the **Provider Network Mirror Protocol**, which OpenTofu and Terraform share. Configurations reference providers by their canonical identity (for example, `hashicorp/aws`), and the CLI installs binaries from OpenDepot. The lockfile keeps the canonical identity, so configurations stay portable.

OpenDepot supports providers from `registry.opentofu.org` and `registry.terraform.io`. Set the origin per `Provider` with `spec.providerConfig.upstreamRegistry`. It defaults to `registry.opentofu.org`.

## Default workflow: Network Mirror

Once providers are synced, declare them in your configuration using their **canonical source identity**:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.80"
    }
    azurerm = {
      source  = "hashicorp/azurerm"
      version = ">= 4.0.0"
    }
  }
}
```

The `source` field uses `<provider-namespace>/<provider-type>`, or the fully-qualified form `<upstream-registry>/<provider-namespace>/<provider-type>`. It names the upstream identity, not your OpenDepot hostname.

**CLI configuration: namespace-scoped mirror URL**

Configure OpenDepot as the installation source in `.tofurc` (OpenTofu) or `.terraformrc` (Terraform) with a `provider_installation` block.

The `network_mirror.include` and `direct.exclude` patterns must use the same hostname as the `upstreamRegistry` on your `Provider` resources. For example, `upstreamRegistry: registry.terraform.io` requires `registry.terraform.io/*/*` in both patterns.

=== "OpenTofu (registry.opentofu.org)"

    **Anonymous access**

    ```hcl
    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

    The `direct.exclude` entry prevents OpenTofu from falling back to `registry.opentofu.org` if a mirrored provider is missing a version, ensuring all installations come from OpenDepot.

    **Authenticated access (OIDC)**

    ```hcl
    credentials "opendepot.defdev.io" {
      token = "<oidc-access-token>"
    }

    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

    Obtain a token:
    ```bash
    tofu login opendepot.defdev.io
    ```
    Or retrieve it directly from your OIDC provider and place it in `.tofurc`.

=== "Terraform (registry.terraform.io)"

    **Anonymous access**

    ```hcl
    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.terraform.io/*/*"]
      }

      direct {
        exclude = ["registry.terraform.io/*/*"]
      }
    }
    ```

    The `direct.exclude` entry prevents Terraform from falling back to `registry.terraform.io` if a mirrored provider is missing a version, ensuring all installations come from OpenDepot.

    **Authenticated access (OIDC)**

    ```hcl
    credentials "opendepot.defdev.io" {
      token = "<oidc-access-token>"
    }

    provider_installation {
      network_mirror {
        url     = "https://opendepot.defdev.io/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.terraform.io/*/*"]
      }

      direct {
        exclude = ["registry.terraform.io/*/*"]
      }
    }
    ```

    Obtain a token:
    ```bash
    terraform login opendepot.defdev.io
    ```
    Or retrieve it directly from your OIDC provider and place it in `.terraformrc`.

=== "Non-default port"

    When your OpenDepot instance runs on a non-default port, include the port in both the `credentials` hostname and the `network_mirror.url`:

    ```hcl
    credentials "opendepot.localtest.me:8080" {
      token = "<oidc-access-token>"
    }

    provider_installation {
      network_mirror {
        url     = "https://opendepot.localtest.me:8080/opendepot/providers/mirror/v1/opendepot-system/"
        include = ["registry.opentofu.org/*/*"]
      }

      direct {
        exclude = ["registry.opentofu.org/*/*"]
      }
    }
    ```

**Key details**

- **Credentials and namespace**: Credentials match the OpenDepot hostname, including the port if it is non-default. The mirror URL is scoped to a Kubernetes namespace (`/opendepot/providers/mirror/v1/<kubernetes-namespace>/`).
- **Lockfile identity**: `.terraform.lock.hcl` records the canonical upstream identity, not the OpenDepot hostname.
- **Mirror-only installs**: `direct.exclude` is required. Without it, the CLI can fall back to the upstream registry for a version the mirror lacks, bypassing OpenDepot's scanning and access controls.
- **Both origins**: Providers from `registry.opentofu.org` and `registry.terraform.io` can share one cluster-scoped installation. If the same provider namespace and type is mirrored from both registries, place them in separate Kubernetes namespaces. Otherwise their Version resource names collide. With `rbac.scopeToNamespace: true`, controllers only watch `global.namespace`, so multi-namespace serving needs broader cluster scope or separate namespace-scoped installations.

!!! note
    The mirror metadata endpoints (`index.json` and the per-version files) check the caller's access. Archive downloads are served by OpenDepot or redirected to a presigned URL, depending on storage configuration. See [Pre-signed URL Redirects](../storage/presigned-urls.md).

## Advanced: Direct OpenDepot provider identity

For private or internal providers that are not mirrored from an upstream registry, use the **Provider Registry Protocol** with direct OpenDepot identity. The `source` names your OpenDepot hostname and Kubernetes namespace:

```hcl
terraform {
  required_providers {
    customapi = {
      source  = "opendepot.defdev.io/my-team/customapi"
      version = "~> 1.0"
    }
  }
}
```

The source format is `<registry-host>/<namespace>/<name>`. `<namespace>` is the Kubernetes namespace of the `Provider` resource. `<name>` is `spec.providerConfig.name`, or the `Provider` resource name if `name` is omitted.

Discovery at `/.well-known/terraform.json` advertises `providers.v1`, so the CLI needs no extra configuration. Use this `host` block only if discovery fails:

!!! note "Host mapping fallback"
    ```hcl
    host "opendepot.defdev.io" {
      services = {
        "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
      }
    }
    ```

Authenticate with a `credentials "opendepot.defdev.io"` block, as in the mirror examples. Use direct identity only for private or internal providers. Use the Network Mirror workflow for providers from `registry.opentofu.org` or `registry.terraform.io`.

## Next Steps for Admins

For publishing and lifecycle operations, see [Registry Operations](operations.md). For scanning, see [Vulnerability Scanning](../configuration/scanning.md). For redirects and storage, see [Pre-signed URL Redirects](../storage/presigned-urls.md).
