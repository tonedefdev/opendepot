---
tags:
  - configuration
  - scanning
  - trivy
  - security
  - providers
  - modules
---

# Vulnerability Scanning

OpenDepot integrates [Trivy](https://trivy.dev/) to scan both provider artifacts and module archives. Scanning is split into two tiers:

- **Module IaC scanning** — enabled by `scanning.enabled: true`. Detects HCL misconfigurations in module archives using Trivy's bundled config rules. Requires no external infrastructure. The published version-controller image bundles the Trivy binary, so no separate image tag is required.
- **Provider scanning** — enabled by `scanning.providerScanning: true` (requires `scanning.enabled: true`). Scans provider binaries and source dependencies against the Trivy vulnerability database. Requires a shared PersistentVolumeClaim and a CronJob to keep the database current.

## Provider Scanning

For each provider version, the Version controller performs two scans:

- **Binary scan** — runs `trivy rootfs` against the provider binary from the upstream archive. Results are stored in `Version.status.binaryScan` per OS and architecture. The binary is written with `0500` permissions (owner read and execute).
- **Source scan** — fetches `go.mod` from the provider's GitHub repository and runs `trivy fs`. Results are stored in `Version.status.sourceScan` and deduplicated across OS and architecture variants, which share the same source. If `go.mod` cannot be fetched, no source result is stored, the binary result still is, and the next reconcile retries. A Trivy run that finds nothing stores `findings: []`, which means scanned with no findings.

!!! note
    `status.binaryScan` is empty for `Version` resources synced before scanning was enabled. The controller does not re-scan on restart, to avoid re-downloading large provider binaries. To re-download and re-scan once, set `forceSync: true`:

    ```bash
    kubectl patch version aws-5-80-0-linux-amd64 -n opendepot-system \
      --type merge -p '{"spec":{"forceSync":true}}'
    ```

    The controller resets `forceSync` to `false` after reconciliation completes. See [Force re-sync a specific provider version](../guides/operations.md#force-re-sync-a-specific-provider-version) for the full `Version` name pattern.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Module IaC Scanning

For each module version, the Version controller runs an IaC scan on the extracted module archive:

- **IaC scan** — extracts the module archive (`.zip` or `.tar.gz`) to a temporary directory and runs `trivy fs --scanners misconfig`. This detects HCL misconfigurations — for example, S3 buckets with public ACLs, security groups open to `0.0.0.0/0`, or unencrypted storage resources.

Findings are stored in `Version.status.sourceScan` and use the same `SecurityFinding` struct as provider scans. The `vulnerabilityID` field contains a Trivy rule ID (e.g. `aws-0057`) rather than a CVE identifier.

!!! tip
    Current Trivy releases use lowercase provider rule IDs (`aws-0057`, `azu-0012`, `gcp-0003`), not the deprecated `AVD-AWS-0057` format. To exempt a rule in a `ScanPolicy` (see [Policy Enforcement](#policy-enforcement)), copy the ID from `status.sourceScan.findings[].vulnerabilityID` on an affected `Version`. Matching is exact.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Trivy-Enabled Image

Release builds publish multi-architecture (`amd64` and `arm64`) version-controller images with Trivy bundled (`INCLUDE_TRIVY=true`). The chart uses this standard image for module IaC and provider scanning. There is no separate `-scanning` image.

## Prerequisites

Provider scanning requires a shared PersistentVolumeClaim for the Trivy vulnerability database and a CronJob that keeps it up to date. Both are created automatically when `scanning.providerScanning` is set to `true`.

The PVC must use a `StorageClass` that supports `ReadWriteMany` access so that the `trivy-db-updater` CronJob and the version-controller pod can mount it simultaneously. For single-node environments such as Kind, `ReadWriteOnce` with the default storage class is sufficient.

!!! note
    The Trivy DB (PVC + CronJob) is only required for provider binary and source scans. Module IaC scanning uses config rules bundled in the Trivy binary and works without any DB infrastructure — just set `scanning.enabled: true`.

## Scanning Defaults

The chart defaults enable module IaC scanning and provider binary/source scanning. Provider scanning creates the shared Trivy database PVC and updater CronJob:

```yaml
scanning:
  enabled: true
  providerScanning: true
  blockOnCritical: false
  blockOnHigh: false
```

Apply via Helm upgrade:

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true
```

## Customizing Provider Scanning

To use a specific StorageClass for the Trivy cache, set `scanning.cache.*`. The example below uses a ReadWriteMany class, which multi-node clusters need:

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set scanning.cache.storageClassName=efs-sc \
  --set scanning.cache.accessMode=ReadWriteMany \
  --set scanning.cache.size=1Gi
```

## Policy Enforcement

The global flags set the baseline. When `blockOnCritical` or `blockOnHigh` is `true` and no `ScanPolicy` matches a `Version`, the Version controller stops reconciliation for a provider or module version with findings at or above that severity. The `Version` stays unsynced, and its `syncStatus` starts with `Scan policy violation:`. A matching `ScanPolicy` can change the threshold or exempt findings, as described below. The controller clears the block when no blocking finding remains. Stored findings are not refreshed on each reconcile. To rescan after a fix is available, set `forceSync: true` on the `Version`.

```yaml
scanning:
  enabled: true
  blockOnCritical: true
  blockOnHigh: false
```

!!! warning
    Enforcement halts only the affected `Version`. Other versions continue to reconcile.

!!! warning "`blockOnHigh` blocks at HIGH **or above**"
    `blockOnHigh: true` also blocks CRITICAL findings. Before 0.11.0, it blocked only HIGH. See the [upgrade notes](../upgrading.md).

A blocked `Version` keeps its `status.sourceScan` and `status.binaryScan` so that exemptions can be written against its findings. As a result, **anyone with `get` on `Version` resources in a namespace can read the known vulnerabilities of stored artifacts**, blocked or not. Factor this into your [RBAC](../reference/rbac/index.md) design.

### Fine-Grained Exemptions with `ScanPolicy`

The global flags are all-or-nothing. The **`ScanPolicy`** custom resource exempts specific findings, such as a CVE with no fix, without lowering enforcement for everything else.

`ScanPolicy` is **namespaced**, not cluster-scoped, so it works with `rbac.scopeToNamespace: true` and only ever affects `Version` resources in its own namespace.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: ScanPolicy
metadata:
  name: baseline
  namespace: opendepot-system
spec:
  priority: 0
  severityThreshold: HIGH
  exemptions:
    - reason: Transitive dependency of the Terraform SDK; no fixed version published yet.
      vulnerabilityIDs:
        - CVE-2024-24790
      scanTypes:
        - binary
        - source
    - reason: Encryption at rest is handled by the platform-managed CMK, not the module.
      vulnerabilityIDs:
        - aws-0057
      scanTypes:
        - module
      expires: "2026-12-31T00:00:00Z"
```

See [`examples/scanpolicy.yaml`](https://github.com/tonedefdev/opendepot/blob/main/examples/scanpolicy.yaml) for a complete set of three policies demonstrating priority shadowing, `targetRefs` with a version constraint, and an expiring exemption. See [`ScanPolicy` API reference](../reference/api.md#scanpolicy) for the full field list.

**How a policy matches a `Version`:**

- `selector` — a `matchLabels`/`matchExpressions` selector evaluated against the `Version`'s labels.
- `targetRefs` — a list of `{kind: Module|Provider, name, versions}` entries. `name` matches exactly, or `*` matches every resource of that kind. `versions` is an optional comma-separated constraint (e.g. `">= 1.0.0, < 2.0.0"`) with AND semantics. Omit it to match every version.
- When both `selector` and `targetRefs` are omitted, the policy applies to every `Version` in its namespace.

An exemption covers a finding when the finding matches every populated field of the exemption. Omitted fields match everything. `reason` is required.

**Precedence — the highest priority policy wins outright, policies do not merge:**

When more than one `ScanPolicy` matches a `Version`, only the single highest-`priority` policy is applied; its `severityThreshold` and `exemptions` are used in full and every other matching policy is ignored entirely. Ties break by the oldest `creationTimestamp`, then by name ascending. Losing policies report the winning policy's name in `status.supersededBy`.

!!! warning "This is shadowing, not layering"
    A lower-priority policy's exemptions are **not** additive to a higher-priority policy's. If you need every exemption in `baseline` to still apply to a more specific policy, copy them into the higher-priority policy as well.

**Matching is exact, never a glob or regex:**

`vulnerabilityIDs`, `pkgNames`, `severities`, and `scanTypes` compare case-insensitively for exact equality. The only wildcard is the single literal `"*"`, which matches everything. There is deliberately no glob, prefix, or regex support, because partial matching risks silently over-exempting findings and regex matching risks ReDoS on attacker-influenced strings such as package names.

**Exempted findings stay visible, they are not hidden:**

A finding covered by an exemption still appears in `status.sourceScan`/`status.binaryScan` with `exempted: true`, `exemptionReason`, and `exemptedBy` (the name of the `ScanPolicy` that exempted it) set. Exemption only stops the finding from blocking reconciliation — it does not remove the finding from the record, and it is still shown with an "Exempted by" chip in [OpenDepot Workshop](../guides/registry-explorer/module-details.md#scan-findings).

**Expiry:**

An exemption with `expires` set stops applying once that time has passed — the controller requeues the `Version` at the next expiry so the finding starts blocking again automatically, with no user action required.

**Fail-closed:**

If the controller cannot list `ScanPolicy` resources (for example, a transient API error or a missing RBAC grant), it logs the error and applies only the global `blockOnCritical`/`blockOnHigh` flags. If those are `false`, nothing blocks. An invalid `selector` sets an `InvalidSelector` condition on that policy, and the policy matches **no** Versions.

**RBAC is the only control:**

There is no global flag to disable `ScanPolicy` enforcement. Whoever can create or edit `scanpolicies` in a namespace can waive scan enforcement there. The chart grants the version controller `get`, `list`, `watch` on `scanpolicies` and `get`, `patch`, `update` on `scanpolicies/status`. It ships no user-facing `Role` with write access to `scanpolicies`. See [Kubernetes RBAC](../reference/rbac/controller-permissions.md).

### Migrating from global thresholds

The global `scanning.blockOnCritical` and `scanning.blockOnHigh` values remain the baseline for Versions that do not match a policy, or for a matching policy that omits `severityThreshold`. They are not automatically converted into `ScanPolicy` resources. To migrate deliberately:

1. Keep the existing Helm threshold values during the rollout so unmatched Versions retain their current behavior.
2. Create a namespaced policy with the equivalent `severityThreshold` (`CRITICAL`, `HIGH`, or `NONE`) and add narrowly scoped `selector` or `targetRefs` entries.
3. Confirm `status.matchedVersions`, `status.activeExemptions`, and `status.conditions`, then inspect the affected Version findings.
4. Once every intended Version is covered, decide whether to retain the global threshold as a safety baseline or set it to `false`. Do not set it to `false` before policy coverage is verified.

`severityThreshold: NONE` disables blocking for matching Versions. An omitted threshold does **not** disable blocking; it preserves the global baseline. A policy's exemption list is also complete: lower-priority policies are shadowed rather than merged.

### Re-evaluation and retained findings

When a `ScanPolicy` changes, the controller re-evaluates the findings stored on each affected Version. It also requeues each Version for the earliest future exemption expiry, so an expired exemption blocks again without a manual edit. Findings stay in `status.sourceScan` and `status.binaryScan`. Removing or expiring an exemption restores enforcement from that stored result. Use `forceSync: true` only when a fresh scan is needed.

### Policy-management API and OpenDepot Workshop workflow

The server is the write surface for UI-managed `ScanPolicy` objects. It is disabled by default. Enable it with `server.policyManagement.enabled: true`. The server validates and persists Kubernetes-shaped objects. CRD validation, resource versions, RBAC, and audit metadata come from Kubernetes. Routes are documented in the [API reference](../reference/api.md#scanpolicy). For OIDC, configure a separate [SecurityGroupBinding](../guides/security-groupbinding.md). `GroupBinding` controls Registry access only.

In OpenDepot Workshop, open **Security policies**, select a namespace, and choose **New policy** or an existing policy. The editor covers priority, severity threshold, selectors or target references, exact-match exemptions, reasons, and expiry. **Preview** validates a draft without saving it. Write controls are disabled when writes are off or the caller is not authorized. Updates and deletes send the resource version as an `If-Match` precondition. If another operator changed the policy, the UI keeps the draft and asks you to reload. See the [policy API contract](https://github.com/tonedefdev/opendepot/blob/main/services/ui/docs/security-policies-api.md) for route payloads.

!!! warning "Treat policy writes as security-sensitive"
    A policy can lower a threshold or waive findings. Enable writes only after assigning an explicit administrator workflow, namespace ownership, and review/audit process. Keep writes disabled when policies are managed declaratively with `kubectl` or GitOps.

### Rollout, rollback, and validation

Install the `SecurityGroupBinding` and `ScanPolicy` CRDs before you enable policy writes. Create least-privilege bindings with explicit namespace and onboarded-resource scope. Deploy with the policy API disabled, and verify the catalog and existing scan behavior. Then enable `server.policyManagement.enabled` after you test authorized and unauthorized UI workflows.

To roll back, stop policy writes, remove or revert the affected `ScanPolicy` objects, and restore the previous global thresholds if you changed them. Then roll back the chart. Keep the `ScanPolicy` CRD installed until all policy objects are removed, because Helm does not remove CRDs on rollback. Findings stay on Versions, so reverting a policy does not erase scan evidence.

Validate the rollout in a disposable namespace:

- [ ] The global baseline blocks a finding.
- [ ] An exact exemption keeps the finding visible and lets reconciliation proceed.
- [ ] Expiry of that exemption restores blocking.
- [ ] Priority shadowing and first-match binding order behave as configured.
- [ ] Cross-namespace and non-onboarded targets are denied.
- [ ] A stale `If-Match` update returns `409 Conflict` and keeps the unsaved draft.
- [ ] Preview, create, update, and delete work for an authorized identity and are denied for an unauthorized identity.
- [ ] The deployed chart's RBAC and OIDC identity configuration match what you expect.

## Offline Mode

By default, provider scanning runs in offline mode (`scanning.offline: true`). Trivy reads the vulnerability database from the shared PVC populated by the `trivy-db-updater` CronJob and makes no outbound network calls during a scan. This is the recommended configuration for production clusters.

Set `scanning.offline: false` only if you want Trivy to download the database at scan time instead. This requires egress to `ghcr.io` from the version-controller pod.

!!! note
    `scanning.offline` applies only to provider scanning. Module IaC scans run `trivy fs --scanners misconfig` without `--offline-scan` and do not use the vulnerability database.

## Trivy DB Updater CronJob

The `trivy-db-updater` CronJob runs `trivy image --download-db-only` to refresh the local vulnerability database cache. It is created only when `scanning.providerScanning: true`. The default schedule is `0 2 * * *`, interpreted in the kube-controller-manager time zone.

```yaml
scanning:
  dbUpdater:
    schedule: "0 2 * * *"
    image:
      repository: aquasec/trivy
      tag: "0.70.0"
```

## Provider Source Repository Resolution

When performing a source scan, the Version controller resolves the provider's GitHub repository using the following chain:

1. **Explicit override** — if `spec.providerConfig.sourceRepository` is set, it is used directly and no lookup is performed.
2. **OpenTofu registry lookup** — for providers with `upstreamRegistry: registry.opentofu.org`, queries `api.opentofu.org` for the registered VCS URL.
3. **Heuristic fallback** — for Terraform Registry providers, or when the OpenTofu lookup fails, constructs `https://github.com/{namespace}/terraform-provider-{name}` from the configured namespace and name.

Step 3 always produces a URL. If that URL is wrong and `go.mod` cannot be fetched, no source scan is stored, the binary scan still runs, and the Version retries on the next reconcile.

The resolved URL is written to `Provider.status.resolvedSourceRepository` when a Version reconcile completes. Once set, it is not overwritten. To inspect the effective URL:

```bash
kubectl get provider <name> -n <namespace> \
  -o jsonpath='{.status.resolvedSourceRepository}'
```

**`namespace` field**

The `namespace` field on `ProviderConfig` controls which organisation is used in upstream registry lookups and the heuristic fallback. It defaults to `hashicorp`.

Set `namespace` when using a provider that is not published under the `hashicorp` organization:

```yaml
spec:
  providerConfig:
    name: github
    namespace: integrations
```

**`sourceRepository` override**

Use `sourceRepository` to pin a specific GitHub URL when registry metadata is unavailable or the heuristic does not match the provider repository:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    sourceRepository: "https://github.com/my-org/terraform-provider-myprovider"
```

## Authenticated Source Scanning

By default, the Version controller uses an **unauthenticated** GitHub client when fetching a provider's `go.mod` for source scanning. This is sufficient for public provider repositories.

For **private source repositories** or to avoid API rate limits in high-volume environments, set `githubClientConfig.useAuthenticatedClient: true` on the `ProviderConfig`:

```yaml
spec:
  providerConfig:
    name: myprovider
    namespace: my-org
    githubClientConfig:
      useAuthenticatedClient: true
```

!!! note
    If the `opendepot-github-application-secret` Secret is missing from the `Version` resource's namespace or the authenticated client cannot be initialised, the controller falls back to an unauthenticated client automatically. Source scanning continues; no manual intervention is required.

See [GitHub Authentication](github-auth.md) for instructions on creating the `opendepot-github-application-secret` Secret.

See [Helm Chart — Scanning Values](../helm-chart.md#scanning-values) for the full `scanning.*` Helm values reference.
