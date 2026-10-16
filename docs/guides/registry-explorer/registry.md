---
tags:
  - guides
  - ui
  - registry-explorer
  - browse
---

# Registry Explorer

The Registry Explorer is OpenDepot Workshop's discovery surface. It gives users
an overview of the modules and providers available to them and lets them open
resource details, versions, and security information.

![OpenDepot Workshop Registry Explorer showing modules, providers, versions, and scan findings](../../img/registry-explorer.png)

## Reading the Registry Explorer

Each resource card summarizes the latest available version:

| Card attribute | Meaning |
| --- | --- |
| Namespace | The Kubernetes namespace that owns the resource. |
| Resource name | The name used to identify the module or provider. Select the card to open its details. |
| Provider logo | The provider associated with the resource, such as AWS. |
| Kind | Whether the resource is a `Module` or `Provider`. |
| Version | The latest version shown for the resource. |
| Sync status | Whether the latest version is synced. A warning icon means some versions are out of sync. |
| Finding badges | Scan finding counts by severity: `C` critical, `H` high, `M` medium, `L` low, `U` unknown, and `E` exempted. |

The footer shows the resource count, the **Per page** setting, and pagination
controls.

## What users see

- **Modules**: versions, README, usage, contract, and scan findings.
- **Providers**: versions, platform artifacts, checksums, schema extraction status, and scan findings.

The Registry Explorer shows only resources the current user can access. If a
resource is missing, check the authentication mode, public labels, and any
applicable `GroupBinding`.

## Related Guides

- [OpenDepot Workshop](index.md)
- [Module Details](module-details.md)
- [Provider Details](provider-details.md)
- [OpenDepot Workshop Administration](administration.md)
- [OpenDepot Workshop authentication](../../authentication/registry-explorer.md)
