---
tags:
  - reference
  - rbac
  - kubernetes
  - namespace
---

# Namespace-Scoped RBAC

By default, OpenDepot creates `ClusterRole` and `ClusterRoleBinding` resources
so controllers can watch the entire cluster. For a smaller permission boundary,
set:

```yaml
rbac:
  scopeToNamespace: true
```

The chart then creates `Role` and `RoleBinding` resources instead. This limits
controller permissions to `global.namespace` and reduces the blast radius if a
controller is compromised.

## Resource Placement Requirement

All `Module`, `Provider`, `Version`, and `Depot` resources must reside in the
same namespace as the controllers. Do not enable namespace-scoped mode when
managed resources span multiple namespaces.

!!! warning
    Namespace-scoped mode is a deployment-wide constraint. It is not a way to
    grant different controllers access to different namespaces.

## GitHub App Secrets

The `secrets: [get]` permission required for GitHub App authentication is always
granted through dedicated namespace-scoped `Role` objects, regardless of
`rbac.scopeToNamespace`. Controller `ClusterRole` objects never grant Secret
access.
