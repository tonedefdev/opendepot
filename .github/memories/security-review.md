# Security Review: RBAC Removal Checklist

**NEVER remove a `secrets get` RBAC rule without first grepping controller source for:**
- `k8sClient.Get` / `r.Client.Get` calls on `corev1.Secret`
- any helper function that wraps a Secret Get (e.g. `GetGithubApplicationSecret`)

Absence of a kubebuilder marker does NOT mean the controller doesn't use the resource —
the chart templates may be manually maintained and out of sync with generated config.

## OpenDepot specific
- Both `version-controller` and `depot-controller` call `GetGithubApplicationSecret`
  in `pkg/github/github.go` — they need `secrets: get` in their RBAC rules.
- See ~/memories/repo/opendepot-auth.md for full details.
