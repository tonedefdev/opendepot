---
tags:
  - reference
  - api
  - version-constraints
---

# Reference

Technical reference documentation for OpenDepot APIs and version constraint syntax.

<div class="grid cards" markdown>

- :material-api: &nbsp;[__API Reference__](api.md)

    ---

    HTTP endpoints (service discovery, OIDC auth flows, module and provider download), authentication modes, Kubernetes custom resource types (`Module`, `Provider`, `Version`, `Depot`, `GroupBinding`), and scan result types.

- :material-tag-multiple: &nbsp;[__Version Constraints__](version-constraints.md)

    ---

    Supported version constraint syntax for `spec.versions` fields and Depot version filtering.

- :material-chart-box: &nbsp;[__Helm Chart__](../helm-chart.md)

    ---

    Complete Helm values reference for the Server, controllers, storage, UI,
    authentication, scanning, and supporting services.

- :material-shield-key: &nbsp;[__Kubernetes RBAC__](rbac/index.md)

    ---

    Controller permissions, namespace-scoped roles, pipeline publisher access,
    and GroupBinding visibility behavior.

</div>
