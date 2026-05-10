---
tags:
  - migration
  - guides
---

# Migrating to OpenDepot

!!! tip
    Migration is one of several Depot use cases. The Depot also supports ongoing upstream provider mirroring and public module tracking with automatic Trivy scanning — see [Pull-Based Workflow: Using the Depot](../guides/depot.md) for the full picture. For a one-time migration, follow the steps below: once everything is synced, delete the Depot and switch to the [push-based CI/CD workflow](../guides/cicd.md). Deleting a Depot **does not** delete the Modules or Providers it created, so your registry stays fully intact.

**Migrating modules** — Use the Depot to bulk-import existing modules into OpenDepot:

1. Create a `Depot` with broad version constraints (e.g., `">= 0.0.0"`) to pull in the full release history
2. Wait for all versions to sync (check `Module` and `Version` status resources)
3. Update your OpenTofu/Terraform configurations to source modules from OpenDepot
4. Delete the Depot — all `Module` and `Version` resources remain untouched
5. Going forward, publish new versions via GitOps or a CI/CD workflow

**Migrating providers** — Use `spec.providerConfigs` in your Depot to mirror providers from the HashiCorp Releases API into your own storage backend:

1. Create a `Depot` with `providerConfigs` listing each provider, your target OS/architecture matrix, and a version constraint
2. Wait for all `Provider` and `Version` resources to sync
3. Update your OpenTofu/Terraform configurations to source providers from OpenDepot (see [Consuming Providers](../guides/providers.md))
4. Delete the Depot — all `Provider` and `Version` resources remain untouched

This pattern lets you adopt OpenDepot incrementally without disrupting existing workflows. The Depot bridges the gap between the public registries and a fully self-hosted solution.
