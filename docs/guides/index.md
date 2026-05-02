---
tags:
  - guides
  - gitops
  - cicd
  - depot
---

# Guides

End-to-end workflows for the most common OpenDepot use cases.

<div class="grid cards" markdown>

- :material-source-branch: &nbsp;[__GitOps with Argo CD__](gitops.md)

    ---

    Manage `Module` manifests in Git and let Argo CD sync them to the cluster. Every published version maps to an approved, merged pull request.

- :material-download-circle: &nbsp;[__Depot (Pull-Based)__](depot.md)

    ---

    Automatically discover and sync module and provider versions from GitHub and HashiCorp without writing any `kubectl apply` commands.

- :material-upload-circle: &nbsp;[__CI/CD (Push-Based)__](cicd.md)

    ---

    Create `Module` resources directly from your CI/CD pipeline for private modules you control.

- :material-package: &nbsp;[__Consuming Modules__](modules.md)

    ---

    Reference synced modules from your OpenTofu or Terraform configurations using the registry source format.

- :material-puzzle: &nbsp;[__Consuming Providers__](providers.md)

    ---

    Use OpenDepot as a private provider mirror, including GPG-verified downloads for air-gapped environments.

- :material-transfer: &nbsp;[__Migrating to OpenDepot__](migration.md)

    ---

    Move existing modules and providers from the public registry or another self-hosted registry to OpenDepot.

</div>
