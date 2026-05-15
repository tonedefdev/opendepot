---
tags:
  - helm
  - installation
---

# Helm Chart

The OpenDepot Helm chart is published to a GitHub Pages Helm repository:

```bash
helm repo add opendepot https://tonedefdev.github.io/opendepot
helm repo update
```

The chart source is also available at [`chart/opendepot/`](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot) in the repository.

See [Installation](getting-started/installation.md) for the full Helm values reference and deployment instructions.

## Server Configuration

### OIDC Authentication

The `server.oidc` section enables OIDC JWT validation for production-ready single sign-on. See [Authenticating with OpenDepot](authentication.md) for detailed setup and examples.

| Value | Type | Description |
|-------|------|-------------|
| `server.oidc.enabled` | bool | When true, enables OIDC JWT validation and advertises the `login.v1` service discovery endpoint. Default: `false` |
| `server.oidc.issuerUrl` | string | OIDC issuer URL (e.g., `https://dex.example.com/dex`). When blank and `dex.enabled: true`, auto-derives the in-cluster Dex service URL. |
| `server.oidc.clientId` | string | OIDC client ID. Must match the Dex static client `id`. Default: `"opendepot"` |
| `server.oidc.clientSecretName` | string | Name of a Kubernetes Secret containing the `clientSecret` key. When blank, the chart creates a Secret from `server.oidc.clientSecret`. |
| `server.oidc.clientSecret` | string | Dex client secret (only used if `clientSecretName` is blank). In production, use an external secret operator instead of storing plaintext here. |

**Example:**

```yaml
server:
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex
    clientId: opendepot
    clientSecret: $(openssl rand -base64 32)
    clientSecretName: ""  # Use the above value; or set to "my-secret" to use external secret
```

## Dex Configuration

The `dex` section deploys Dex as an OIDC identity provider. Dex federates upstream IdPs (GitHub, Entra ID, Okta, LDAP, etc.) and issues JWTs that the server validates locally.

| Value | Type | Description |
|-------|------|-------------|
| `dex.enabled` | bool | When true, deploys Dex as a subchart. Default: `false` |
| `dex.config.issuer` | string | Public issuer URL. In-cluster: `http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex`. External: `https://dex.example.com/dex` |
| `dex.config.connectors` | array | Array of upstream IdP connector configurations. See examples below. Default: `[]` |
| `dex.config.enablePasswordDB` | bool | When true, enables local username/password authentication (testing only). Default: `false` |
| `dex.config.staticPasswords` | array | Array of test users for local auth. Never enable in production. Default: `[]` |

**Basic Example (GitHub):**

```yaml
dex:
  enabled: true
  config:
    issuer: https://dex.example.com/dex
    connectors:
      - type: github
        id: github
        name: GitHub
        config:
          clientID: <github-oauth-app-client-id>
          clientSecret: <github-oauth-app-secret>
          redirectURI: https://dex.example.com/dex/callback
          org: my-org  # (optional) restrict to an org
```

**Entra ID (Azure AD) Example:**

```yaml
dex:
  enabled: true
  config:
    issuer: https://dex.example.com/dex
    connectors:
      - type: microsoft
        id: microsoft
        name: "Azure AD"
        config:
          clientID: <azure-app-id>
          clientSecret: <azure-app-secret>
          redirectURI: https://dex.example.com/dex/callback
          tenant: <azure-tenant-id>
```

For connector configuration details, refer to the [Dex Connector Documentation](https://dexidp.io/docs/connectors/).

!!! warning
    Never set `enablePasswordDB: true` or `staticPasswords` in production. Use real IdP connectors instead.

## Scanning Values

The `scanning` section controls Trivy-based provider vulnerability scanning. See [Vulnerability Scanning](configuration/scanning.md) for full details.

```yaml
scanning:
  enabled: false
  cacheMountPath: /var/cache/trivy
  offline: true
  blockOnCritical: false
  blockOnHigh: false
  cache:
    storageClassName: ""
    accessMode: ReadWriteMany
    size: 1Gi
  dbUpdater:
    schedule: "0 2 * * *"
    image:
      repository: aquasec/trivy
      tag: "0.70.0"
```

