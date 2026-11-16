---
tags:
  - guides
  - ui
  - registry-explorer
  - module
---

# Module Details

The Module Details page shows one module: its current version, README, usage, contract, storage and GitHub configuration, versions, and scan findings.

![OpenDepot Workshop Module Details page showing module status and the module requirement in the Usage section](../../img/workshop-module-details.png#only-dark)
![OpenDepot Workshop Module Details page showing module status and the module requirement in the Usage section](../../img/workshop-module-details-light.png#only-light)

## Module identity and status

The breadcrumb shows the namespace and kind. The header shows the module name, the latest synchronized version, sync status, and scan counts. A link to the upstream source appears when one is configured.

## Usage and README

The **Usage** section comes first and provides a copyable module block with the OpenDepot source address and version constraint.

The **README** section renders the module README when one is available.

![OpenDepot Workshop module README section showing rendered headings, requirements tables, and providers](../../img/workshop-module-readme.png#only-dark)
![OpenDepot Workshop module README section showing rendered headings, requirements tables, and providers](../../img/workshop-module-readme-light.png#only-light)

## Module contract

The **Contract** section shows the normalized interface extracted from the module source: inputs and outputs with their types, required status, defaults, and descriptions.

![OpenDepot Workshop module contract showing Inputs and Outputs tabs, typed arguments, required status, defaults, and descriptions](../../img/workshop-module-contract.png#only-dark)
![OpenDepot Workshop module contract showing Inputs and Outputs tabs, typed arguments, required status, defaults, and descriptions](../../img/workshop-module-contract-light.png#only-light)

Use **Show more** to expand long input or output lists. A contract appears only when Assembly Line is enabled and the version has a derived contract.

!!! note
    Enable Assembly Line with `assembly.enabled: true` in the Helm values, and set `server.image.tag` and `version.image.tag` to a `-assembly` image tag. The UI must also be enabled with `ui.enabled: true`. See [Assembly Line Configuration](../../configuration/assembly-line.md).

## Overview

The **Overview** section lists the provider, repository owner (when set), version history limit (when greater than zero), and version constraints.

## Storage, GitHub, and Depot settings

![OpenDepot Workshop module Overview, Storage Configuration, GitHub Configuration, and Depot sections](../../img/workshop-module-configuration.png#only-dark)
![OpenDepot Workshop module Overview, Storage Configuration, GitHub Configuration, and Depot sections](../../img/workshop-module-configuration-light.png#only-light)

### Storage Configuration

The **Storage Configuration** section shows the backend, its location fields, and **Presign Enabled**. When **Presign Enabled** is `Yes`, downloads use pre-signed redirects instead of streaming through the server.

When the module has no storage settings of its own, the section shows the backend inherited from its managing Depot with an **Inherited From** row.

### GitHub Configuration

The **GitHub Configuration** section shows **Authenticated Client**. The value is `Yes` when the module uses an authenticated GitHub client for source metadata.

### Depot

The **Depot** section appears when a Depot manages the module. It names that Depot as `namespace / name`.

## Versions

The **Versions** view lists synchronized module archives with their sync status, resource name, filename, checksum, scan timestamp, and findings.

![OpenDepot Workshop module Versions view showing filters, synchronized archives, checksums, scan timestamps, and findings](../../img/workshop-module-versions.png#only-dark)
![OpenDepot Workshop module Versions view showing filters, synchronized archives, checksums, scan timestamps, and findings](../../img/workshop-module-versions-light.png#only-light)

Use the search field and **Sync status** filter to narrow the list. Each row shows the version, resource name, archive filename, checksum, last scan timestamp, and severity counts. Select a version to inspect its scan results.

## Scan Findings

The **Scan Findings** view lists findings for a selected module version. Use the **Version**, search, **Severity**, **Sort by**, and **Order** controls to filter the list.

![OpenDepot Workshop module Scan Findings view showing filters, severity labels, affected packages, and resolutions](../../img/workshop-module-scan-findings.png#only-dark)
![OpenDepot Workshop module Scan Findings view showing filters, severity labels, affected packages, and resolutions](../../img/workshop-module-scan-findings-light.png#only-light)

Each finding shows its rule ID, severity, title, affected package or resource, message, and recommended resolution. Findings are deduplicated by rule ID, and only the highest-severity instance of each rule appears.

## Related views

- [Workshop](index.md)
- [Provider Details](provider-details.md)
- [Browse resources and dashboards](browse.md)
- [Browse API](api.md)
- [Workshop authentication](../../authentication/registry-explorer.md)
