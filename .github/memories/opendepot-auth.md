# OpenDepot Authentication Architecture

## GitHub App Secret (`opendepot-github-application-secret`)

**Both the version-controller AND depot-controller read this Kubernetes Secret at runtime.**

- `pkg/github/github.go` → `GetGithubApplicationSecret(ctx, k8sClient, namespace)` does a `k8sClient.Get` on `corev1.Secret`
- Called from `services/version/internal/controller/opendepot_versions_controller.go` line ~658 (module downloads)
- Called from `services/version/internal/controller/scanner.go` line ~415 (provider source scanning)
- Called from `services/depot/internal/controller/opendepot_depot_controller.go` line ~128 (depot reconciliation)

**RBAC consequence**: Both `version-controller-role` and `depot-controller` ClusterRoles/Roles MUST include:
```yaml
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]
```
Removing this rule breaks authenticated GitHub API access (provider source scans, module archive downloads).
The controllers fall back to unauthenticated clients when the secret is missing — the rule is not dead code.

## OIDC / Server Auth
- `services/server/auth.go` and `services/server/discovery.go` handle OIDC/OAuth2
- Issuer URL comes from operator config, not user input
- Groups claim extracted from verified ID token only

## kubebuilder markers → chart RBAC
- Markers in controller Go files generate `config/rbac/*.yaml` via `make manifests`
- Chart templates in `chart/opendepot/templates/*-rbac.yaml` are manually maintained — they must stay in sync with controller code
- Always cross-check controller source for `k8sClient.Get/List` calls before removing any RBAC rule from the chart
