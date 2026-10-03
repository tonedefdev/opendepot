# Security Policies API contract

The UI uses the server's namespaced Kubernetes-style ScanPolicy API. The
server remains authoritative for RBAC, CRD validation, policy precedence, and
status. The UI does not invent preview or capability endpoints.

## Routes

All routes are under `/opendepot/ui/v1` and use the forwarded bearer token.

| Method | Route | Response |
| --- | --- | --- |
| GET | `/scan-policies/{namespace}` | Kubernetes `ScanPolicyList` |
| GET | `/scan-policies/{namespace}/capabilities` | `ScanPolicyCapabilities` |
| POST | `/scan-policies/{namespace}/preview` | `ScanPolicyPreview` |
| GET | `/scan-policies/{namespace}/{name}` | Kubernetes `ScanPolicy` |
| POST | `/scan-policies/{namespace}` | `ScanPolicy` (`201`) |
| PUT | `/scan-policies/{namespace}/{name}` | `ScanPolicy` |
| DELETE | `/scan-policies/{namespace}/{name}` | empty (`204`) |

`ScanPolicyCapabilities` is:
`{ writesEnabled, canRead, canWrite, methods, supportsPreview }`.
The UI only enables writes when both `writesEnabled` and `canWrite` are true,
and only enables preview when `supportsPreview` is true. Write authorization
is still enforced by the server: kubeconfig and bearer-token requests use
Kubernetes RBAC, while OIDC requests are default-deny unless a matching
`GroupBinding` in the requested namespace has
`spec.scanPolicyManagement: true`. Module/provider access alone is not enough,
and a binding from another namespace does not authorize the write.

Writes send a complete Kubernetes-shaped object:

```json
{
  "apiVersion": "opendepot.defdev.io/v1alpha1",
  "kind": "ScanPolicy",
  "metadata": { "name": "baseline", "namespace": "platform" },
  "spec": {
    "priority": 0,
    "severityThreshold": "HIGH",
    "targetRefs": [
      { "kind": "Module", "name": "terraform-aws-vpc", "versions": ">= 5.0.0" }
    ],
    "selector": { "matchLabels": { "environment": "sandbox" } },
    "exemptions": [
      {
        "vulnerabilityIDs": ["CVE-2026-0001"],
        "pkgNames": ["stdlib"],
        "scanTypes": ["source", "binary"],
        "severities": ["HIGH"],
        "reason": "Replacement is scheduled",
        "expires": "2026-12-31T00:00:00Z"
      }
    ]
  }
}
```

`severityThreshold` is `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `NONE`.
`targetRefs.kind` is `Module` or `Provider`. Exemption `reason` is required;
`expires` is an optional RFC3339 timestamp. Omitted selector and target refs
match all Versions in the namespace.

Updates and deletes require an `If-Match` header. Missing headers return
`428 Precondition Required`; stale versions return `409 Conflict`. Updates
also include `metadata.resourceVersion` in the Kubernetes object. The UI
preserves the unsaved draft on conflicts and asks the operator to reload.
Successful writes are logged by the server with the operation, namespace,
policy name, and subject; configure Kubernetes API-server audit logging for the
underlying CRD audit record.

Preview accepts the same Kubernetes-shaped policy object and returns
`{ valid, policy }`.
