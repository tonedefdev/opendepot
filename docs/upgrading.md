---
tags:
  - upgrading
  - releases
  - breaking-changes
---

# Upgrading

Breaking changes and upgrade steps for each OpenDepot release. Check this page before running `helm upgrade` on an existing installation.

!!! tip
    Helm does not update CRDs during `helm upgrade`. Always apply the latest CRDs before upgrading:
    ```bash
    helm show crds opendepot/opendepot | kubectl apply --server-side -f -
    ```

## v0.9.0

v0.9.0 adds an opt-in reverse proxy so Dex never needs its own public ingress or hostname. See [Proxying Dex Through the Server](configuration/oidc.md#recommended-proxy-dex-through-the-server).

Set `server.oidc.dexProxy.enabled: true` to have the server reverse-proxy `/dex/*` requests to the bundled Dex service. This is fully backward compatible — the flag defaults to `false`, and existing `dex.enabled: true` deployments with a separately exposed Dex continue to work unchanged.

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```
3. (Optional) To adopt the recommended proxy mode, set `dex.config.issuer` and `server.oidc.issuerUrl` to the same external, path-based URL and enable `server.oidc.dexProxy.enabled: true`. See [Proxying Dex Through the Server](configuration/oidc.md#recommended-proxy-dex-through-the-server) for the full walkthrough.

No action is required to keep existing behavior — `dexProxy.enabled` defaults to `false`.

## v0.8.0

v0.8.0 adds automatic README resolution for modules. See [Module READMEs](guides/operations.md#module-readmes) and the [Registry Explorer README rendering](guides/registry-explorer.md#module-readmes).

### New RBAC Permissions

The version-controller ServiceAccount now requires `configmaps` (`create`, `get`, `list`, `patch`, `update`, `watch`) to store resolved READMEs, and the server ServiceAccount now requires `configmaps` (`get`, `list`, `watch`) to serve them through the browse API. Both rules are added automatically by the Helm chart — no values changes are required. See [Kubernetes RBAC](rbac.md#controller-permissions).

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```

No manual action is required for existing `Module` and `Version` resources — the version controller resolves and stores READMEs automatically on each Version's next reconcile, or immediately when `forceSync: true` is set.

### Dependency Updates

The UI's `vitest` dependency was bumped to `^3.2.6`, with `vite` and `undici` pinned via `resolutions`, resolving HIGH/CRITICAL npm advisories. This affects the UI's build and test tooling only — no runtime or Helm values changes are required.

## v0.6.0

v0.6.0 replaces the SQLite download-stats backend with a bundled Valkey instance.

### Breaking Changes

- **`--stats-db-path` is removed.** The server flag no longer exists. Any custom Helm values overrides that reference `server.stats.*` must be removed — the chart will reject unknown values.
- **`server.stats` values block is removed.** Remove `server.stats.emptyDir`, `server.stats.persistence.*`, or any `server.stats` key from your `values.yaml` before upgrading.
- **Stats history is not migrated.** Valkey starts with a clean slate — download counts accumulated in the previous SQLite database are not carried over. Historic data can be discarded or archived manually before upgrading.

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Remove any `server.stats` keys from your `values.yaml`.
3. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```

Valkey is deployed automatically as part of the chart. Download tracking resumes immediately after the server pod becomes ready. For production clusters, `valkey.dataStorage.enabled: true` (the default) ensures stats survive pod restarts — no additional configuration is required.

## v0.5.0

### Breaking Changes

| Change | Affected field | Action required |
|--------|---------------|----------------|
| `Provider.status.sourceScans` removed; per-version provider source scans moved to `Version.status.sourceScan` | `ProviderStatus`, `VersionStatus` | Update any automation or scripts that read `.status.sourceScans` on a `Provider` resource. Provider source scan results are now stored on each `Version` resource in `status.sourceScan`, alongside module IaC scan results. |
| `Provider.status.resolvedSourceRepository` added (read-only, string) | `ProviderStatus` | No action required. The field is populated automatically by the Version controller after the first scan. |

### Upgrade Steps

1. Apply the updated CRDs:
   ```bash
   helm show crds opendepot/opendepot | kubectl apply --server-side -f -
   ```
2. Update any scripts or automation reading `Provider.status.sourceScans` to read `Version.status.sourceScan` instead.
3. Upgrade the chart:
   ```bash
   helm upgrade opendepot opendepot/opendepot -n opendepot-system -f my-values.yaml
   ```
