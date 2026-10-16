---
tags:
  - depot
  - pull-based
  - guides
---

# Pull-Based Workflow: Using the Depot

The Depot is a pull-based controller. It discovers, downloads, and tracks modules and providers from external sources, so your registry stays current without manual imports.

Use it for:

- **Upstream provider mirroring:** sync providers from the OpenTofu or Terraform Registry into your storage backend, and pick up new releases on a schedule
- **Public module tracking:** follow open-source modules by GitHub repository, and pin or float versions with constraints
- **Private module import:** pull from private GitHub repositories with GitHub App authentication. This is also the starting point for [migrating to OpenDepot](migration.md).

---

## Syncing upstream providers

Mirrored providers keep their **canonical identity** (for example `registry.opentofu.org/hashicorp/aws`). Configurations reference the canonical source, and OpenDepot serves it through the Network Mirror Protocol. Set the source with `spec.providerConfigs[].upstreamRegistry`, which defaults to `registry.opentofu.org`. See [Consuming Providers](../guides/providers.md) for `.tofurc` examples.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: upstream-providers
  namespace: opendepot-system
spec:
  global:
    storageConfig:
      s3:
        bucket: opendepot-providers
        region: us-east-1
  providerConfigs:
    - name: aws
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 5.80.0"
    - name: azurerm
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 4.0.0"
    - name: google
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 6.0.0"
  pollingIntervalMinutes: 1440
```

The Depot queries the upstream registry for each provider, filters releases by version constraint, and creates `Provider` and `Version` resources for each matching OS and architecture. When [scanning is enabled](../configuration/scanning.md), the Version controller runs a binary scan (`trivy rootfs`) on each binary. It also runs a source scan (`trivy fs` on the provider's `go.mod`) when the repository is resolvable. If the `go.mod` cannot be downloaded, no source scan is stored and the Version retries.

To mirror from `registry.terraform.io` instead, set the `upstreamRegistry` field explicitly:

```yaml
providerConfigs:
  - name: aws
    upstreamRegistry: registry.terraform.io
    operatingSystems:
      - linux
    architectures:
      - amd64
      - arm64
    versionConstraints: ">= 5.80.0"
```

With `pollingIntervalMinutes: 1440`, the Depot checks for new releases daily and creates matching resources automatically.

!!! tip
    `">= 5.80.0"` tracks every release above a known baseline. `"~> 5.80"` stays on the 5.x line and excludes major upgrades.

!!! note
    Provider names default to the `hashicorp` namespace. To mirror a provider from a different registry namespace (e.g., `DataDog/datadog` or `integrations/github`), set the `namespace` field explicitly:
    ```yaml
    providerConfigs:
      - name: datadog
        namespace: DataDog
        versionConstraints: ">= 3.50.0"
    ```

---

## Tracking public modules

Point the Depot at an upstream GitHub repository and set a version constraint to control which module versions reach your registry.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: public-modules
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-east-1
  moduleConfigs:
    - name: terraform-aws-vpc
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 5.0.0"
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 20.0.0, != 21.13.0"
    - name: terraform-google-kubernetes-engine
      provider: google
      repoOwner: terraform-google-modules
      versionConstraints: ">= 33.0.0"
  pollingIntervalMinutes: 360
```

The Depot queries each repository's GitHub releases, filters them by constraint, creates `Module` and `Version` resources, and stores the archives. When [module IaC scanning](../configuration/scanning.md#module-iac-scanning) is enabled, the Version controller runs `trivy fs` with the misconfig scanner on each archive. This flags HCL issues such as open security groups, public S3 ACLs, or unencrypted storage.

!!! note
    Module IaC scanning does not use the Trivy vulnerability database. To run it without the database PVC, CronJob, and cache volume, disable provider scanning:
    ```yaml
    scanning:
      enabled: true
      providerScanning: false
    ```
    See [Vulnerability Scanning](../configuration/scanning.md) for the full configuration reference.

With `pollingIntervalMinutes: 360`, the Depot checks for new releases every six hours.

---

## Combined example

A single Depot can manage modules and providers together:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: my-team-depot
  namespace: opendepot-system
spec:
  global:
    githubClientConfig:
      useAuthenticatedClient: true
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-west-2
  moduleConfigs:
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 21.10.1, != 21.13.0"
    - name: terraform-azurerm-aks
      provider: azurerm
      repoOwner: azure
      versionConstraints: ">= 10.0.0"
  providerConfigs:
    - name: aws
      operatingSystems:
        - linux
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 5.80.0"
      storageConfig:
        s3:
          bucket: opendepot-modules
          region: us-west-2
  pollingIntervalMinutes: 60
```

This Depot:

1. Queries the `terraform-aws-modules/terraform-aws-eks` and `azure/terraform-azurerm-aks` repositories for releases that match the constraints, and creates `Module` resources
2. Queries the upstream registry for `aws` and creates a `Provider` resource
3. Has the Module and Provider controllers create `Version` resources, which the Version controller fetches and stores
4. Re-checks for new releases every 60 minutes

---

## Options

**Polling interval:** `pollingIntervalMinutes` sets how often the Depot re-queries GitHub and upstream registries. If omitted, the Depot reconciles once and does not poll.

**Per-module storage override:** A module can override the global storage config:

```yaml
moduleConfigs:
  - name: terraform-aws-eks
    provider: aws
    repoOwner: terraform-aws-modules
    versionConstraints: ">= 21.10.1"
    storageConfig:
      azureStorage:
        accountName: opendepotmodules
        accountUrl: https://opendepotmodules.blob.core.windows.net
        subscriptionID: 00000000-0000-0000-0000-000000000000
        resourceGroup: opendepot-rg
```

**Version history limit:** `versionHistoryLimit` is stored on the spec, but the controllers do not prune versions yet. To remove a version, delete its entry from `spec.versions`.
