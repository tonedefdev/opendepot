---
tags:
  - migration
  - guides
---

# Migrating to OpenDepot

!!! tip
    Migration is one of several Depot use cases. The Depot also supports ongoing upstream provider mirroring and public module tracking with automatic Trivy scanning — see [Pull-Based Workflow: Using the Depot](../guides/depot.md) for the full picture. For a one-time migration, follow the steps below: once everything is synced, delete the Depot and switch to the [GitOps workflow](../guides/gitops.md). Deleting a Depot **does not** delete the Modules or Providers it created, so your registry stays fully intact.

**Migrating modules** — Use the Depot to bulk-import existing modules into OpenDepot:

1. Create a `Depot` with broad version constraints (e.g., `">= 0.0.0"`) to pull in the full release history
2. Wait for all versions to sync (check `Module` and `Version` status resources)
3. Update your OpenTofu/Terraform configurations to source modules from OpenDepot
4. Delete the Depot — all `Module` and `Version` resources remain untouched
5. Going forward, publish new versions via the GitOps workflow

**Migrating providers** — Use `spec.providerConfigs` in your Depot to mirror providers from the HashiCorp Releases API into your own storage backend:

1. Create a `Depot` with `providerConfigs` listing each provider, your target OS/architecture matrix, and a version constraint
2. Wait for all `Provider` and `Version` resources to sync
3. Update your OpenTofu/Terraform configurations to source providers from OpenDepot (see [Consuming Providers](../guides/providers.md))
4. Delete the Depot — all `Provider` and `Version` resources remain untouched

This pattern lets you adopt OpenDepot incrementally without disrupting existing workflows. The Depot bridges the gap between the public registries and a fully self-hosted solution.

## Upgrading to v0.6.0

v0.6.0 replaces the SQLite download-stats backend with a bundled Valkey instance.

### Breaking changes

- **`--stats-db-path` is removed.** The server flag no longer exists. Any custom Helm values overrides that reference `server.stats.*` must be removed — the chart will reject unknown values.
- **`server.stats` values block is removed.** Remove `server.stats.emptyDir`, `server.stats.persistence.*`, or any `server.stats` key from your `values.yaml` before upgrading.
- **Stats history is not migrated.** Valkey starts with a clean slate — download counts accumulated in the previous SQLite database are not carried over. Historic data can be discarded or archived manually before upgrading.

### Upgrade steps

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
