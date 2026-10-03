---
tags:
  - guides
  - oidc
  - access-control
  - scanpolicy
---

# ScanPolicy Access with SecurityGroupBinding

`SecurityGroupBinding` is a namespaced CRD for ScanPolicy administration. It is
separate from [`GroupBinding`](groupbinding.md), which controls Registry and
browse access to Modules and Providers. A user may be authorized for one
workflow without being authorized for the other.

SecurityGroupBindings are created in the OpenDepot server namespace, normally
`opendepot-system`. The server sorts bindings by name and applies the first
binding whose expression matches the authenticated user's OIDC groups. It does
not combine grants from multiple bindings.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: SecurityGroupBinding
metadata:
  name: 10-platform-scan-policy-admins
  namespace: opendepot-system
spec:
  expression: '"platform-security" in groups'
  namespaces:
    - platform
  moduleResources:
    - terraform-aws-*
  providerResources:
    - aws
  namespaceWidePolicyManagement: false
```

## Scope

| Field | Description |
|---|---|
| `expression` | Required expr-lang boolean expression evaluated against `groups []string`. |
| `namespaces` | Policy namespace allow-list. An empty list allows no namespaces; `"*"` allows namespaces reachable by the server ServiceAccount. |
| `moduleResources` | Allowed Module names using `path.Match` glob semantics. An empty list allows no Module targets. |
| `providerResources` | Allowed Provider names. Names are exact; `"*"` allows all onboarded Providers. An empty list allows no Provider targets. |
| `namespaceWidePolicyManagement` | When `true`, allows selector-based policies and policies without `targetRefs` in an allowed namespace. Defaults to `false`. |

The server discovers the effective binding independently for every request.
Malformed expressions, expression evaluation errors, missing matches, and failed
binding discovery fail closed. A later binding cannot bypass an invalid earlier
binding.

For a scoped operator, a policy must have no selector, at least one `targetRef`,
and every target must be an allowed Module or Provider. The target must also be
currently onboarded: the UI catalog lists only authorized Module and Provider
resources that exist in the selected namespace. The server remains authoritative
and does not trust names or scope supplied by the UI.

Selectors and empty `targetRefs` are allowed only when
`namespaceWidePolicyManagement: true`. Updates authorize both the persisted and
proposed policy, so an authorized targeted policy cannot be widened into an
unauthorized selector or namespace-wide policy. Deletes reauthorize the
persisted policy before removing it.

## Catalog and UI

The policy UI obtains its namespace and target choices from the server-authorized
catalog:

```text
GET /opendepot/ui/v1/scan-policies/catalog
```

The response contains `writesEnabled` and sorted namespace items. Each item
contains `canRead`, `canWrite`, `canManageNamespaceWidePolicies`, and sorted
`modules` and `providers` lists. Unauthorized namespaces, resources, bindings,
and patterns are not disclosed. The UI does not use the public browse namespace
endpoint for policy discovery and does not accept arbitrary target names.

The global `server.policyManagement.enabled` value is a separate write gate and
defaults to `false`. Enabling it adds write permissions to the server
ServiceAccount; it does not grant an OIDC user access. Keep it disabled when
policies are managed only through GitOps. Kubeconfig and bearer-token requests
continue to rely on the caller's Kubernetes RBAC, while OIDC requests use the
SecurityGroupBinding scope described above.

## Migration and Rollout

Existing `GroupBinding` policy-management fields are not the authorization model
for this implementation. Create and test SecurityGroupBindings before enabling
UI writes. A least-privilege migration is:

1. Apply the `SecurityGroupBinding` CRD and create a binding with explicit namespaces and resource lists.
2. Confirm the catalog exposes only the intended namespaces and onboarded targets.
3. Deploy the chart with `server.policyManagement.enabled: false` and verify existing Registry and scan behavior.
4. Enable policy writes only after authorized and unauthorized preview, create, update, and delete requests have been tested.

To roll back, disable `server.policyManagement.enabled` first. Existing
ScanPolicy objects remain available to the controller and are not deleted by
disabling the UI write gate. Keep the CRD installed until policy objects have
been removed through the chosen declarative workflow.

See the [API reference](../reference/api.md#securitygroupbinding) and
[Kubernetes RBAC](../rbac.md) for the resource and ServiceAccount permissions.