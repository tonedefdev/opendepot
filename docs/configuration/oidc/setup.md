---
tags:
  - configuration
  - oidc
  - dex
---

# Core OIDC Setup

This page covers the standard bundled-Dex deployment: enable Dex, configure the
server, apply the Helm upgrade, verify service discovery, and sign in with
OpenTofu.

## Step 1: Enable Dex

Set `dex.enabled: true` and configure the issuer and at least one connector.
Dex expands `$ENV_VAR` references at startup, so keep connector secrets in a
Kubernetes Secret and reference them through `dex.envFrom`:

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex
    connectors:
      - type: github
        id: github
        name: GitHub
        config:
          clientID: <github-oauth-app-client-id>
          clientSecret: $GITHUB_CLIENT_SECRET
          redirectURI: https://opendepot.example.com/dex/callback
          org: my-org
  envFrom:
    - secretRef:
        name: dex-connector-secrets
```

Create the connector Secret before deploying:

```bash
kubectl create secret generic dex-connector-secrets \
  --from-literal=GITHUB_CLIENT_SECRET=<github-oauth-app-secret> \
  -n opendepot-system
```

!!! warning
    Do not write connector `clientSecret` values as plain strings in Helm
    values. They are rendered into a Kubernetes ConfigMap and visible to users
    with access to that ConfigMap.

See [Connectors and Secrets](connectors.md) for provider examples and secret
management details.

## Step 2: Enable OIDC on the Server

Add the `server.oidc` block to the same values file:

```yaml
server:
  useBearerToken: false
  oidc:
    enabled: true
    issuerUrl: https://opendepot.example.com/dex
    clientId: opendepot
    clientSecret: $STRONG_RANDOM_VALUE
```

When `server.oidc.issuerUrl` is blank and `dex.enabled: true`, the chart
auto-derives the in-cluster URL:

```
http://<release-name>-dex.<namespace>.svc.cluster.local:5556/dex
```

!!! warning
    Do not commit `clientSecret` in plain text. Use an external secret
    operator or set `server.oidc.clientSecretName` to an existing Secret.

For public and split-network deployments, see [Proxy and Deployment
Modes](proxy.md).

## Step 3: Apply the Helm Upgrade

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  -f oidc-values.yaml \
  --wait
```

Verify the server pod and OIDC flags:

```bash
kubectl get pods -n opendepot-system
kubectl describe pod -n opendepot-system -l app=server | grep oidc
```

## Step 4: Verify Service Discovery

When OIDC is enabled, `/.well-known/terraform.json` includes a `login.v1`
object:

```bash
curl https://opendepot.example.com/.well-known/terraform.json
```

```json
{
  "modules.v1": "/opendepot/modules/v1/",
  "providers.v1": "/opendepot/providers.v1/",
  "login.v1": {
    "authz": "https://opendepot.example.com/dex/auth",
    "token": "https://opendepot.example.com/dex/token",
    "grant_types": ["authz_code", "device_code"],
    "scopes": ["openid", "profile", "email", "groups"],
    "client": "opendepot"
  }
}
```

The advertised URLs assume the recommended server-proxied Dex setup. If
`login.v1` is absent, OIDC is not enabled or the server has not restarted after
the Helm upgrade.

## Step 5: Authenticate with `tofu login`

Users run `tofu login` once and obtain a JWT cached locally:

```bash
tofu login opendepot.example.com
```

OpenTofu opens a browser window and redirects to Dex. After the user signs in
through the upstream IdP, Dex issues a JWT and OpenTofu stores it in
`~/.terraform.d/credentials.tfrc.json`.

On headless systems, OpenTofu uses the device code flow and prints a URL and
short code for authentication in another browser.
