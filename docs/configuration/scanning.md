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

- **Module IaC scanning** — enabled by `scanning.enabled: true`. Detects HCL misconfigurations in module archives using Trivy's bundled config rules. Requires no external infrastructure. Setting this flag automatically switches the version-controller to the `-scanning` image variant, which bundles the Trivy binary.
- **Provider scanning** — enabled by `scanning.providerScanning: true` (requires `scanning.enabled: true`). Scans provider binaries and source dependencies against the Trivy vulnerability database. Requires a shared PersistentVolumeClaim and a CronJob to keep the database current.

## Provider Scanning

For each provider version, the Version controller performs two scans:

- **Binary scan** — runs `trivy rootfs` against the compiled provider binary extracted from the upstream registry archive. Results are stored per `Version` resource in `Version.status.binaryScan` because each OS/architecture binary may embed different Go standard library versions or runtime dependencies. The binary is written with `0500` (execute) permissions before scanning; Trivy requires the execute bit to be set for gobinary detection — binaries without execute permission are silently skipped.
- **Source scan** — fetches `go.mod` from the provider's GitHub repository and runs `trivy fs` to find vulnerable source dependencies. Results are stored on each `Version` resource in `Version.status.sourceScan`, deduplicated across OS/architecture variants of the same provider version since all variants share the same source code. When a provider repository has no `go.mod`, the scan completes with `findings: []` (an empty slice, not absent) — this is a tombstone indicating the version was scanned and nothing was found, as opposed to not yet scanned.

!!! note
    `status.binaryScan` will be empty for any `Version` that was synced before scanning was enabled. The controller does not automatically re-scan on restart to avoid re-downloading potentially hundreds of large provider binaries. To trigger a one-time re-download and re-scan, set `forceSync: true` on the `Version` resource:

    ```bash
    kubectl patch version aws-5-80-0-linux-amd64 -n opendepot-system \
      --type merge -p '{"spec":{"forceSync":true}}'
    ```

    The controller resets `forceSync` to `false` after reconciliation completes. See [Force re-sync a specific provider version](../guides/providers.md#consuming-providers) for the full `Version` name pattern.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Module IaC Scanning

For each module version, the Version controller runs an IaC scan on the extracted module archive:

- **IaC scan** — extracts the module archive (`.zip` or `.tar.gz`) to a temporary directory and runs `trivy fs` targeting Trivy's config-class checks. This detects HCL misconfigurations — for example, S3 buckets with public ACLs, security groups open to `0.0.0.0/0`, or unencrypted storage resources.

Findings are stored in `Version.status.sourceScan` and use the same `SecurityFinding` struct as provider scans. The `vulnerabilityID` field contains a Trivy rule ID (e.g. `aws-0057`) rather than a CVE identifier.

!!! tip
    Current Trivy releases emit rule IDs in lowercase-provider form (`aws-0057`, `azu-0012`, `gcp-0003`), not the deprecated Aqua/AVD format (`AVD-AWS-0057`). If you plan to reference a rule ID in a `ScanPolicy` exemption (see [Policy Enforcement](#policy-enforcement)), always copy it verbatim from `status.sourceScan.findings[].vulnerabilityID` on an affected `Version` rather than typing it from memory — matching is exact, so a wrong format silently never matches.

See [Vulnerability Scanning Runbooks](../guides/operations.md#vulnerability-scanning-runbooks) for examples of reading scan results.

## Trivy-Enabled Image

The SaaS release publishes one ARM64 version-controller image with the Trivy binary bundled using the `INCLUDE_TRIVY=true` build argument. The chart uses this standard image tag for both module IaC scanning and provider binary/source scanning; there is no separate `-scanning` image variant.

## Prerequisites

Provider scanning requires a shared PersistentVolumeClaim for the Trivy vulnerability database and a CronJob that keeps it up to date. Both are created automatically when `scanning.providerScanning` is set to `true`.

The PVC must use a `StorageClass` that supports `ReadWriteMany` access so that the `trivy-db-updater` CronJob and the version-controller pod can mount it simultaneously. For single-node environments such as Kind, `ReadWriteOnce` with the default storage class is sufficient.

!!! note
    The Trivy DB (PVC + CronJob) is only required for provider binary and source scans. Module IaC scanning uses config rules bundled in the Trivy binary and works without any DB infrastructure — just set `scanning.enabled: true`.

## Scanning Defaults

SaaS defaults enable module IaC scanning and provider binary/source scanning. Provider scanning creates the shared Trivy database PVC and updater CronJob:

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

To customize full provider binary and source scanning:

```yaml
scanning:
  enabled: true
  providerScanning: true
  blockOnCritical: false
  blockOnHigh: false
  cache:
    storageClassName: "efs-sc"   # must support ReadWriteMany in multi-node clusters
    accessMode: ReadWriteMany
    size: 1Gi
```

Apply via Helm upgrade:

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true \
  --set scanning.cache.storageClassName=efs-sc
```

## Policy Enforcement

When `blockOnCritical` or `blockOnHigh` is set to `true`, the Version controller will stop reconciliation for any provider or module version that has findings at or above the configured severity threshold. The `Version` resource will remain in an unsynced state with a descriptive `syncStatus` message until the vulnerability is resolved or the policy flag is relaxed.

```yaml
scanning:
  enabled: true
  blockOnCritical: true
  blockOnHigh: false
```

!!! warning
    Policy enforcement halts reconciliation for the affected `Version` resource only. Other versions that do not have findings at the blocked severity level will continue to reconcile normally.

!!! warning "`blockOnHigh` now blocks at HIGH **or above**"
    Prior to this release, `blockOnHigh` only matched findings whose severity was exactly `HIGH` — a `blockOnHigh: true, blockOnCritical: false` configuration silently let CRITICAL findings through. `blockOnHigh` now means "block at HIGH or above", so CRITICAL findings are blocked as well. This is strictly more restrictive than before, so no previously-blocked version becomes unblocked, but your effective enforcement may tighten on upgrade. See [Upgrading](../upgrading.md) for details.

A blocked `Version` still has its `status.sourceScan` and `status.binaryScan` populated with the findings that caused the block. This is required so an exemption can be authored against a finding, but it also means **anyone with `get` on `Version` resources in a namespace can enumerate the known vulnerabilities of stored artifacts**, whether or not the version is currently blocked. Factor this into your [RBAC](../rbac.md) design.

### Fine-Grained Exemptions with `ScanPolicy`

The global `blockOnCritical`/`blockOnHigh` flags are all-or-nothing: they either block every affected version or none of them. The **`ScanPolicy`** custom resource lets you exempt specific findings — a known CVE with no fix, an accepted-risk misconfiguration — without lowering enforcement for everything else.

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
- `targetRefs` — an explicit list of `{kind: Module|Provider, name, versions}` entries. `versions` is an optional [`hashicorp/go-version`](https://github.com/hashicorp/go-version) constraint string (e.g. `"< 5.0.0"`), the same constraint syntax already used by the [Depot controller](../architecture.md).
- When both `selector` and `targetRefs` are omitted, the policy applies to every `Version` in its namespace.

**Precedence — the highest priority policy wins outright, policies do not merge:**

When more than one `ScanPolicy` matches a `Version`, only the single highest-`priority` policy is applied; its `severityThreshold` and `exemptions` are used in full and every other matching policy is ignored entirely. Ties break by the oldest `creationTimestamp`, then by name ascending. Losing policies report the winning policy's name in `status.supersededBy`.

!!! warning "This is shadowing, not layering"
    A lower-priority policy's exemptions are **not** additive to a higher-priority policy's. If you need every exemption in `baseline` to still apply to a more specific policy, copy them into the higher-priority policy as well.

**Matching is exact, never a glob or regex:**

`vulnerabilityIDs`, `pkgNames`, `severities`, and `scanTypes` only support exact string matches, plus the single literal `"*"` meaning "match everything". There is deliberately no glob, prefix, or regex support — partial matching risks silently over-exempting findings, and regex-based matching risks ReDoS on attacker-influenced strings such as package names.

**Exempted findings stay visible, they are not hidden:**

A finding covered by an exemption still appears in `status.sourceScan`/`status.binaryScan` with `exempted: true`, `exemptionReason`, and `exemptedBy` (the name of the `ScanPolicy` that exempted it) set. Exemption only stops the finding from blocking reconciliation — it does not remove the finding from the record, and it is still shown (dimmed, with an "Exempted" chip) in the [Registry Explorer](../guides/registry-explorer.md#scan-findings).

**Expiry:**

An exemption with `expires` set stops applying once that time has passed — the controller requeues the `Version` at the next expiry so the finding starts blocking again automatically, with no user action required.

**Fail-closed:**

If the controller cannot list `ScanPolicy` resources (e.g. a transient API server error or a missing RBAC grant), it falls back to the stricter `blockOnCritical`/`blockOnHigh` baseline rather than allowing everything through. An invalid `selector` on a policy surfaces an `InvalidSelector` condition on that policy's `status.conditions`, and the policy matches **no** Versions rather than every Version.

**RBAC is the control, there is no separate kill switch:**

There is intentionally no global flag to disable `ScanPolicy` enforcement. Controlling who may create or edit `scanpolicies` in a namespace **is** the control over who may waive scan enforcement. The chart grants the version controller `get`, `list`, `watch` on `scanpolicies` and `get`, `patch`, `update` on `scanpolicies/status`, and deliberately does not ship any user-facing `Role` granting write access to `scanpolicies` — binding that permission to specific users or groups is a decision for your cluster administrators. See [Kubernetes RBAC](../rbac.md#controller-permissions).

### Migrating from global thresholds

The global `scanning.blockOnCritical` and `scanning.blockOnHigh` values remain the baseline for Versions that do not match a policy, or for a matching policy that omits `severityThreshold`. They are not automatically converted into `ScanPolicy` resources. To migrate deliberately:

1. Keep the existing Helm threshold values during the rollout so unmatched Versions retain their current behavior.
2. Create a namespaced policy with the equivalent `severityThreshold` (`CRITICAL`, `HIGH`, or `NONE`) and add narrowly scoped `selector` or `targetRefs` entries.
3. Confirm `status.matchedVersions`, `status.activeExemptions`, and `status.conditions`, then inspect the affected Version findings.
4. Once every intended Version is covered, decide whether to retain the global threshold as a safety baseline or set it to `false`. Do not set it to `false` before policy coverage is verified.

`severityThreshold: NONE` disables blocking for matching Versions. An omitted threshold does **not** disable blocking; it preserves the global baseline. A policy's exemption list is also complete: lower-priority policies are shadowed rather than merged.

### Re-evaluation and retained findings

The Version controller evaluates the effective policy during reconciliation and when a ScanPolicy changes. It requeues Versions for the earliest future exemption expiry, so an expired exemption begins blocking without a manual edit. Findings are retained in `Version.status.sourceScan` and `Version.status.binaryScan` after policy evaluation. Exemptions annotate, rather than remove, findings; removing or expiring an exemption therefore restores enforcement from the retained scan result on the next reconciliation. Use `forceSync: true` only when a fresh artifact scan is required.

### Policy-management API and Registry Explorer workflow

The server is the authoritative write surface for the first UI-managed custom resource. Enable it explicitly with `server.policyManagement.enabled: true`; it is disabled by default. The server validates and persists Kubernetes-shaped `ScanPolicy` objects and leaves CRD validation, resource versions, RBAC, and audit metadata to Kubernetes. The read, catalog, capabilities, preview, create, replace, and delete routes are documented in the [API reference](../reference/api.md#scanpolicy). For OIDC, configure a separate [SecurityGroupBinding](../guides/security-groupbinding.md); `GroupBinding` controls Registry access only.

In the Registry Explorer, an operator opens **Security policies**, selects a namespace, and chooses **New policy** or an existing policy. The editor supports priority, severity threshold, selectors or target references, exact-match exemptions, reasons, and expiry. **Preview** validates the draft without persisting it. The UI reads the capabilities route first and disables write controls when writes are disabled or the caller is unauthorized. A successful save returns the server's resource version; an update or delete uses that version as an `If-Match` precondition. If another operator changed the object, the UI keeps the unsaved draft and asks the operator to reload rather than overwriting it. See the [policy API contract](https://github.com/tonedefdev/opendepot/blob/main/services/ui/docs/security-policies-api.md) for the route payloads.

!!! warning "Treat policy writes as security-sensitive"
    A policy can lower a threshold or waive findings. Enable writes only after assigning an explicit administrator workflow, namespace ownership, and review/audit process. Keep writes disabled when policies are managed declaratively with `kubectl` or GitOps.

### Rollout, rollback, and validation

Roll out the `SecurityGroupBinding` and `ScanPolicy` CRDs before enabling policy writes. Create least-privilege bindings with explicit namespace and onboarded resource scope, deploy the chart with the policy API still disabled, verify the catalog and existing scan behavior, and then enable `server.policyManagement.enabled` only when authorized and unauthorized UI workflows have been tested. The chart version containing this feature is `0.12.6`.

For rollback, first stop policy writes, remove or revert the affected `ScanPolicy` objects, and restore the previous global threshold values if they were changed. Then roll back the application chart. Keep the `ScanPolicy` CRD installed until all policy objects have been removed; Helm does not remove CRDs during rollback. Because findings remain stored on Versions, reverting a policy does not erase scan evidence.

Validate the rollout with an end-to-end test in a disposable namespace: create a blocked finding, confirm the global baseline blocks it, apply an exact exemption and confirm the finding remains visible but reconciliation proceeds, confirm expiry restores blocking, verify priority shadowing, and exercise preview/create/update/delete through the UI with both an authorized and unauthorized identity. Also test first-match binding order, an invalid earlier binding, denied cross-namespace and non-onboarded targets, selector/empty-target rejection for scoped operators, and a stale `If-Match` update with `409 Conflict` without losing the draft. The repository includes focused server, controller, and UI tests for the implemented paths; production rollout validation should additionally verify the deployed chart RBAC and OIDC identity configuration.

## Offline Mode

By default, provider scanning runs in offline mode (`scanning.offline: true`). Trivy reads the vulnerability database from the shared PVC populated by the `trivy-db-updater` CronJob and makes no outbound network calls during a scan. This is the recommended configuration for production clusters.

Set `scanning.offline: false` only if you want Trivy to download the database directly at scan time instead of relying on the CronJob. This requires the version-controller pod to have egress access to `ghcr.io`.

!!! note
    `scanning.offline` only applies to provider scanning. Module IaC scanning uses bundled config rules and makes no network calls regardless of this setting.

## Trivy DB Updater CronJob

The `trivy-db-updater` CronJob runs `trivy image --download-db-only` on a schedule to refresh the local vulnerability database cache. It is only created when `scanning.providerScanning: true`. By default it runs daily at 02:00 UTC.

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

If all three steps fail to produce a usable URL, the Version controller logs a warning and skips the source scan. The binary scan still runs.

The resolved URL is written to `Provider.status.resolvedSourceRepository` after the first successful scan. Once set, subsequent reconciliations do not overwrite it. To inspect the effective repository URL in use:

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
