---
tags:
  - guides
  - ui
  - registry-explorer
  - browse
---

# Registry Explorer UI

The Registry Explorer is a browsable, searchable frontend for OpenDepot. Enable
it by setting `ui.enabled: true` in the Helm chart.

!!! note
    The UI is disabled by default. Existing server-only deployments are
    unaffected when `ui.enabled` remains `false`.

## Routing Architecture

When `ui.enabled: true`, OpenDepot deploys a Next.js frontend alongside the
server. NGINX runs inside the UI pod and splits traffic between the registry
protocol server and the browser UI:

| Path prefix | Destination |
|-------------|-------------|
| `/opendepot/*` | Server — registry protocol and browse API |
| `/.well-known/*` | Server — service discovery |
| `/` (all other paths) | Next.js UI at port 3000 |

When `ui.ingress.enabled: true`, the Kubernetes Ingress applies the same
split-path rules at the load balancer. The `/opendepot` and `/.well-known` paths
route directly to the `server` Service; all other paths go to the `ui` Service.
NGINX re-proxies browser-originated same-origin API calls from the UI back to the
server.

When `server.ingress.istio.enabled: true` and `ui.enabled: true`, the Istio
`VirtualService` template automatically adds the same split HTTP match rules. No
additional Istio configuration is required.

!!! warning
    When `ui.enabled: true`, the `server-ingress.yaml` template is automatically
    disabled. If you were previously routing directly through the server Ingress,
    migrate to `ui.ingress` before enabling the UI.

## Local Quick Start with Tilt

The fastest way to try the Registry Explorer is the repository's
[Tilt](https://tilt.dev/) environment. It creates or reuses a local Kind cluster,
builds the complete stack, configures Dex OIDC, generates development secrets,
and enables live updates.

Install the [local quickstart prerequisites](../../getting-started/quickstart.md#prerequisites),
then run from the repository root:

```bash
export OPENDEPOT_DEV_PASSWORD='choose-a-local-password'
tilt/scripts/up.sh
```

Wait for the `ui` resource to become ready in the [Tilt dashboard](http://localhost:10350),
then open [https://opendepot.localtest.me:8443](https://opendepot.localtest.me:8443).
Sign in as `dev@example.com` with the value of `OPENDEPOT_DEV_PASSWORD`.

Create sample resources from the Tilt dashboard or run:

```bash
tilt trigger seed-sample-resources
```

The sample `GroupBinding` grants the local user access to the seeded resources.
Refresh the Registry Explorer to browse version metadata, READMEs, and scan
findings.

Stop Tilt with ++ctrl+c++, then remove the deployed resources while preserving
the local cluster and image registry:

```bash
tilt down
```

## Enabling the UI

### Create the session secret

The UI uses encrypted server-side session cookies. Provide a secret of at least
32 characters before deploying:

```bash
kubectl create secret generic ui-session-secret \
  --from-literal=sessionPassword=$(openssl rand -base64 32) \
  -n opendepot-system
```

### Register an OIDC client

When `ui.oidc.enabled: true`, the UI uses the OIDC authorization code flow to
authenticate users. The groups claim from the resulting token is matched against
[`GroupBinding`](../groupbinding.md) resources. Skip this step if you only need
anonymous browsing.

Add a `staticClient` to your Dex configuration:

```yaml
dex:
  config:
    staticClients:
      - id: opendepot-ui
        name: OpenDepot UI
        secretEnv: OPENDEPOT_UI_CLIENT_SECRET
        redirectURIs:
          - https://opendepot.example.com/auth/callback
        responseTypes:
          - code
        grantTypes:
          - authorization_code
```

Create the client secret:

```bash
kubectl create secret generic ui-oidc-secret \
  --from-literal=clientSecret=<your-client-secret> \
  -n opendepot-system
```

### Set Helm values

At minimum:

```yaml
ui:
  enabled: true
  sessionPasswordSecretName: ui-session-secret
  ingress:
    enabled: true
    className: nginx
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "true"
    hosts:
      - host: opendepot.example.com
        paths:
          - path: /
            pathType: Prefix
    tls:
      - secretName: opendepot-tls
        hosts:
          - opendepot.example.com
```

To enable OIDC login, add:

```yaml
ui:
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex
    clientId: opendepot-ui
    clientSecretName: ui-oidc-secret
    scopes: "openid profile email groups"
    callbackPath: /auth/callback
```

!!! danger "Developer token input — never enable in production"
    `ui.auth.devTokenInput.enabled: true` renders a token input field in the
    Sidebar for testing the UI against a live cluster. The default is `false`.

See [Helm Chart — UI Configuration](../../helm-chart.md#ui-configuration) for
the full `ui.*` Helm values reference.

### Apply the chart

```bash
helm upgrade opendepot opendepot/opendepot \
  --namespace opendepot-system \
  -f values.yaml
```

## Public Visibility Labels

By default, browse endpoints return only resources explicitly marked as public:

| Label | Applied to | Effect |
|-------|------------|--------|
| `opendepot.defdev.io/public: "true"` | Kubernetes Namespace | Marks the namespace as publicly discoverable |
| `opendepot.defdev.io/public: "true"` | `Module` or `Provider` | Marks the resource as publicly viewable |

Both labels must be present for unauthenticated callers to see a resource.

```bash
kubectl label namespace opendepot-system opendepot.defdev.io/public=true
```

## Browse Visibility Rules

| Caller | Resources visible |
|--------|-------------------|
| Unauthenticated in OIDC mode with anonymous access disabled | **401 Unauthorized** |
| Unauthenticated in non-OIDC or anonymous mode | Public namespace and public resources |
| OIDC-authenticated without a matching `GroupBinding` | Public namespace and public resources |
| OIDC-authenticated with a matching `GroupBinding` | Public resources plus resources allowed by the binding |
| `anonymousAuth: true` | All resources; public labels are ignored |
| Non-OIDC bearer token | Public resources only |

!!! note "Namespace list is always label-filtered"
    The namespace endpoint always enforces the `opendepot.defdev.io/public=true`
    label. System namespaces are never returned regardless of auth mode.

## GroupBinding for Browse Access

`GroupBinding` resources grant OIDC-authenticated users access beyond the public
set. They use an [expr-lang](https://expr-lang.org/) expression against the OIDC
groups claim.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: platform-team-binding
  namespace: opendepot-system
spec:
  expression: '"platform-team" in groups'
  moduleResources:
    - "terraform-aws-*"
    - "terraform-gcp-vpc"
  providerResources:
    - "aws"
    - "google"
```

See [GroupBinding Access Control](../groupbinding.md) for full expression
syntax, client credentials, and first-match semantics.

## Related Guides

- [Browse resources and dashboards](browse.md)
- [Browse API](api.md)
- [Registry Explorer authentication](../../authentication/registry-explorer.md)