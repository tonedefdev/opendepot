---
tags:
  - configuration
  - ui
  - registry-explorer
  - oidc
---

# Registry Explorer UI

When `ui.enabled: true` is set in the Helm chart, OpenDepot deploys a Next.js frontend alongside the server. NGINX runs inside the UI pod and splits traffic between the registry protocol server and the browser UI.

!!! note
    The UI is disabled by default. Existing server-only deployments are unaffected when `ui.enabled` remains `false`.

## Routing Architecture

NGINX inside the UI pod applies split-path routing:

| Path prefix | Destination |
|-------------|-------------|
| `/opendepot/*` | Server — registry protocol and browse API |
| `/.well-known/*` | Server — service discovery |
| `/` (all other paths) | Next.js UI at port 3000 |

When `ui.ingress.enabled: true`, the Kubernetes Ingress applies the same split-path rules at the load balancer. The `/opendepot` and `/.well-known` paths route directly to the `server` Service; all other paths go to the `ui` Service. NGINX re-proxies browser-originated same-origin API calls from the UI back to the server.

!!! warning
    When `ui.enabled: true`, the `server-ingress.yaml` template is automatically disabled. If you were previously routing directly through the server Ingress, migrate to `ui.ingress` before enabling the UI.

## Prerequisites

- A session secret of at least 32 characters (required — see [Session Secret](#session-secret))
- An OIDC client registration if using `ui.oidc.enabled: true`

## Session Secret

The UI uses encrypted server-side session cookies. Provide a secret of at least 32 characters before deploying:

```bash
kubectl create secret generic ui-session-secret \
  --from-literal=sessionPassword=$(openssl rand -base64 32) \
  -n opendepot-system
```

Then reference it in your Helm values:

```yaml
ui:
  sessionPasswordSecretName: ui-session-secret
```

## OIDC Login

When `ui.oidc.enabled: true`, the UI uses the OIDC authorization code flow to authenticate users. The groups claim from the resulting token is matched against [`GroupBinding`](../guides/groupbinding.md) resources to determine which resources the user can browse beyond the publicly-labelled set.

### Step 1: Register an OIDC client

Add a `staticClient` to your Dex configuration. The UI requires the `authorization_code` grant type and a registered callback URI:

```yaml
dex:
  config:
    staticClients:
      - id: opendepot-ui
        name: OpenDepot UI
        secretEnv: OPENDEPOT_UI_CLIENT_SECRET  # (1)!
        redirectURIs:
          - https://opendepot.example.com/auth/callback
        responseTypes:
          - code
        grantTypes:
          - authorization_code
```

1. Reference the client secret from an environment variable. Expose it via `dex.envFrom` pointing to a Kubernetes Secret — never set it as a plain string literal.

### Step 2: Create the client secret

```bash
kubectl create secret generic ui-oidc-secret \
  --from-literal=clientSecret=<your-client-secret> \
  -n opendepot-system
```

### Step 3: Configure Helm values

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

The `groups` scope (or whichever scope emits the groups claim in your IdP) must be included for `GroupBinding` evaluation to function.

## Developer Token Input

!!! danger "Never enable in production"
    `ui.auth.devTokenInput.enabled: true` renders a token input field that accepts any arbitrary bearer token. This exists solely for local development. Enabling it in a production environment allows any user to authenticate with a token of their choice.

The default is `false`. Do not change it outside of non-production environments.

## Ingress

The `ui.ingress` block follows the same structure as the standard Kubernetes Ingress. When enabled, the chart renders the UI Ingress and suppresses the server Ingress.

```yaml
ui:
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

## Istio VirtualService

When `server.ingress.istio.enabled: true` and `ui.enabled: true`, the `VirtualService` template automatically adds split HTTP match rules: `/opendepot` and `/.well-known` route to the `server` Service; the catch-all default routes to the `ui` Service. No additional Istio configuration is required.

## `ui` Helm Values Reference

| Value | Type | Default | Description |
|-------|------|---------|-------------|
| `ui.enabled` | bool | `false` | When true, deploys the Registry Explorer UI and NGINX proxy pod. |
| `ui.replicaCount` | int | `1` | Number of UI pod replicas. |
| `ui.image.repository` | string | `ghcr.io/tonedefdev/opendepot/ui` | UI container image repository. |
| `ui.image.tag` | string | `""` | Image tag. Defaults to `global.image.tag`, then the chart `appVersion`. |
| `ui.serverHost` | string | `""` | Upstream address (`host:port`) that NGINX proxies registry requests to. Defaults to `server.<namespace>.svc.cluster.local:80`. |
| `ui.sessionPasswordSecretName` | string | `""` | Name of a Kubernetes Secret containing the `sessionPassword` key (min 32 chars). Required. |
| `ui.oidc.enabled` | bool | `false` | Enables OIDC authorization code login in the UI. |
| `ui.oidc.issuerUrl` | string | `""` | Public OIDC issuer URL (must be reachable from browsers). |
| `ui.oidc.clientId` | string | `"opendepot-ui"` | OIDC client ID for the UI. Separate from the `tofu login` client. |
| `ui.oidc.clientSecretName` | string | `""` | Name of a Kubernetes Secret containing the `clientSecret` key. |
| `ui.oidc.scopes` | string | `"openid profile email groups"` | Space-separated OIDC scopes to request. |
| `ui.oidc.callbackPath` | string | `"/auth/callback"` | Redirect URI path registered with the identity provider. |
| `ui.auth.devTokenInput.enabled` | bool | `false` | When true, displays a developer bearer-token input in the UI. **Must be `false` in production.** |
| `ui.ingress.enabled` | bool | `false` | Creates a Kubernetes Ingress for the UI with split-path routing. |
| `ui.ingress.className` | string | `""` | Ingress class name (e.g., `nginx`). |
| `ui.ingress.annotations` | map | `{}` | Annotations applied to the Ingress resource. |
| `ui.ingress.hosts` | list | see values | Host and path rules. |
| `ui.ingress.tls` | list | `[]` | TLS configuration for the Ingress. |
| `ui.resources` | map | — | Pod resource requests and limits. |
| `ui.nodeSelector` | map | `{}` | Node selector constraints for the UI pod. |
| `ui.tolerations` | list | `[]` | Tolerations for the UI pod. |
| `ui.affinity` | map | `{}` | Affinity rules for the UI pod. |
