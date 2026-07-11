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
