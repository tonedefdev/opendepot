---
tags:
  - guides
  - ui
  - registry-explorer
  - provider
---

# Provider Details

The Provider Details page shows one provider: its registry identity, sync state, mirror usage configuration, overview metadata, storage and GitHub settings, managing Depot, platform versions, and scan findings.

![OpenDepot Workshop Provider Details page showing provider status and the CLI configuration in the Usage section](../../img/workshop-provider-details.png#only-dark)
![OpenDepot Workshop Provider Details page showing provider status and the CLI configuration in the Usage section](../../img/workshop-provider-details-light.png#only-light)

## Provider identity and status

The breadcrumb shows the namespace and kind. The header shows the provider name, the latest version, sync status, and scan counts. A link to the upstream source appears when one is configured.

## Usage

The **Usage** section comes first and provides copyable network mirror configuration for `.tofurc` or `.terraformrc`. The mirror URL includes the deployment and namespace shown on the page.

## Overview

The **Overview** section shows:

| Field | Description |
|-------|-------------|
| Provider Namespace | Registry namespace, such as `hashicorp`. Defaults to `hashicorp` |
| Upstream Registry | Registry used for discovery and downloads. Defaults to `registry.opentofu.org` |
| Repo Owner | Repository owner. Appears when set |
| Version History Limit | Number of versions retained. Appears when greater than zero |
| Version Constraints | Constraint used to select versions |

## Storage, GitHub, and Depot settings

![OpenDepot Workshop provider Overview, Storage Configuration, GitHub Configuration, and Depot sections showing the file system backend, presign setting, authenticated client state, and managing Depot](../../img/workshop-provider-configuration.png#only-dark)
![OpenDepot Workshop provider Overview, Storage Configuration, GitHub Configuration, and Depot sections showing the file system backend, presign setting, authenticated client state, and managing Depot](../../img/workshop-provider-configuration-light.png#only-light)

### Storage Configuration

The **Storage Configuration** section shows the backend, its location fields, and **Presign Enabled**. When **Presign Enabled** is `Yes`, downloads use pre-signed redirects instead of streaming through the server.

When the provider has no storage settings of its own, the section shows the backend inherited from its managing Depot with an **Inherited From** row.

### GitHub Configuration

The **GitHub Configuration** section shows **Authenticated Client**. The value is `Yes` when the provider uses an authenticated GitHub client for source metadata.

### Depot

The **Depot** section appears when a Depot manages the provider. It names that Depot as `namespace / name`.

## Versions and findings

The **Versions** view lists each synchronized provider artifact by platform. Use the search field and the **Sync status**, **OS**, and **Arch** filters to narrow the list.

![OpenDepot Workshop Provider Versions view showing platform artifacts, extraction status, checksums, scan timestamps, and findings](../../img/workshop-provider-versions.png#only-dark)
![OpenDepot Workshop Provider Versions view showing platform artifacts, extraction status, checksums, scan timestamps, and findings](../../img/workshop-provider-versions-light.png#only-light)

Each row shows the version, sync status, resource name, OS, architecture, schema extraction status (`Extracted` or `Failed`), archive filename, checksum, last scan time, and finding counts. A version appears once per OS and architecture artifact. Select a row to inspect its scan results.

## Scan findings

The **Scan Findings** view lists the provider's source findings and binary findings in separate sections.

![OpenDepot Workshop Scan Findings view showing severity filters, vulnerability IDs, affected packages, and remediation guidance](../../img/workshop-provider-scan-findings.png#only-dark)
![OpenDepot Workshop Scan Findings view showing severity filters, vulnerability IDs, affected packages, and remediation guidance](../../img/workshop-provider-scan-findings-light.png#only-light)

Use the search field and the **Severity**, **Sort by**, and **Order** controls to filter the source findings. Each finding shows its identifier, severity, title, message, and recommended resolution. Findings are deduplicated by rule ID.

Visibility follows the same rules as the rest of Workshop.

## Related views

- [Workshop](index.md)
- [Browse resources and dashboards](browse.md)
- [Browse API](api.md)
- [Workshop authentication](../../authentication/registry-explorer.md)
