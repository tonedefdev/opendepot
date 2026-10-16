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
    # Setting envFrom replaces the chart default, so keep the client secret entry too.
    - secretRef:
        name: opendepot-dex-client-secret
        optional: true
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

Create the OpenDepot client Secret. Dex loads the `OPENDEPOT_DEX_CLIENT_SECRET`
key from it. The chart requires `server.oidc.clientSecretName` to be
`opendepot-dex-client-secret`, because `dex.envFrom` reads that name:

```bash
SECRET=$(openssl rand -hex 32)
kubectl create secret generic opendepot-dex-client-secret \
  -n opendepot-system \
  --from-literal=OPENDEPOT_DEX_CLIENT_SECRET="$SECRET"
```

Add the `server.oidc` block to the same values file:

```yaml
server:
  oidc:
    enabled: true
    issuerUrl: https://opendepot.example.com/dex
    clientId: opendepot
    clientSecretName: opendepot-dex-client-secret
```

When `server.oidc.issuerUrl` is blank and `dex.enabled: true`, the chart
auto-derives the in-cluster URL:

```
http://<release-name>-dex.<namespace>.svc.cluster.local:5556/dex
```

!!! warning
    Do not put the client secret in Helm values. Create the Secret out of band
    or use an external secret operator. The chart does not create it. See
    [Client Secret Management](connectors.md#client-secret-management).

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
  "providers.v1": "/opendepot/providers/v1/",
  "login.v1": {
    "client": "opendepot",
    "grant_types": ["authz_code", "device_code"],
    "authz": "https://opendepot.example.com/dex/auth",
    "token": "https://opendepot.example.com/dex/token",
    "scopes": ["openid", "profile", "email", "groups"],
    "ports": [10000, 10010]
  }
}
```

The advertised URLs assume the server-proxied Dex setup. If `login.v1` is
absent, OIDC is disabled or the server has not restarted since the upgrade.

## Step 5: Authenticate with `tofu login`

```bash
tofu login opendepot.example.com
```

OpenTofu opens a browser and redirects to Dex. After sign-in, OpenTofu stores
the JWT in `~/.terraform.d/credentials.tfrc.json`. On headless systems it uses
the device code flow and prints a URL and code.
