---
tags:
  - reference
  - rbac
  - kubernetes
  - security
---

# Kubernetes RBAC

The Helm chart creates ServiceAccounts and RBAC resources for each controller
automatically when `rbac.create: true`, which is the default.

<div class="grid cards" markdown>

- :material-shield-key: &nbsp;[__Controller Permissions__](controller-permissions.md)

    ---

    Review the resources and verbs granted to the Depot, Module, Provider,
    Version, and Server components.

- :material-package-variant-closed: &nbsp;[__Namespace-Scoped RBAC__](namespace-scoped.md)

    ---

    Reduce the controller blast radius by replacing cluster-wide roles with
    namespace-scoped `Role` and `RoleBinding` resources.

- :material-account-key: &nbsp;[__Pipeline Publisher Role__](pipeline-publisher.md)

    ---

    Grant a CI ServiceAccount the least privilege needed to create or update
    `Module` resources.

- :material-chart-areaspline: &nbsp;[__GroupBinding and Stats Visibility__](visibility.md)

    ---

    Understand how OIDC GroupBinding rules affect Registry Explorer statistics
    and resource visibility.

</div>

## Choosing an RBAC Model

Use the default cluster-wide model when controllers must reconcile resources
across namespaces. Use namespace-scoped mode when all managed resources live in
the installation namespace and a smaller permission boundary is more important
than multi-namespace reconciliation.

See [Namespace-Scoped RBAC](namespace-scoped.md) for the required Helm values
and constraints.
