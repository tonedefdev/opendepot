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
| `server.oidc.groupsClaim` | string | JWT claim name containing the user's groups, used for [GroupBinding](guides/groupbinding.md) evaluation. When blank, defaults to `groups`. Set to `cognito:groups`, `roles`, etc. for non-standard IdPs. |
| `server.oidc.allowServiceAccountFallback` | bool | When true, Kubernetes ServiceAccount bearer tokens with a non-OIDC issuer are authenticated via the bearer-token path using the SA's own RBAC. GroupBinding is bypassed for SA tokens. Requires `server.oidc.enabled: true`. Default: `false` |
| `server.oidc.allowClientCredentials` | bool | When true, Dex tokens whose audience does not match the primary client ID are accepted. The token's `sub` claim is mapped to a virtual group `"client:<sub>"` and evaluated against `GroupBinding` resources. Requires a Dex `staticClient` with `grantTypes: ["client_credentials"]`. Default: `false` |
| `server.oidc.authzUrl` | string | Overrides the authorization URL advertised in `login.v1` of `/.well-known/terraform.json`. Leave blank to use the URL from the OIDC provider discovery document. Use this when the server discovers Dex via an in-cluster address but CLI clients must reach Dex at a different address (e.g. a port-forwarded URL during local Kind testing). |
| `server.oidc.tokenUrl` | string | Overrides the token URL advertised in `login.v1` of `/.well-known/terraform.json`. Same use-case as `authzUrl`. |

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

!!! warning
    When both `dex.enabled` and `server.oidc.enabled` are `true`, the Helm render fails if neither `server.oidc.clientSecret` nor `server.oidc.clientSecretName` is set. For production, pre-create a Kubernetes Secret and reference it via `server.oidc.clientSecretName`.

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

## UI Configuration

The `ui` section deploys the Registry Explorer frontend. See [Registry Explorer UI](configuration/ui.md) for setup details and the [Registry Explorer guide](guides/registry-explorer.md) for enabling public visibility and browse access.

| Value | Type | Description |
|-------|------|-------------|
| `ui.enabled` | bool | When true, deploys the Registry Explorer UI and NGINX proxy. Also suppresses `server-ingress.yaml` — migrate traffic to `ui.ingress` before enabling. Default: `false` |
| `ui.replicaCount` | int | Number of UI pod replicas. Default: `1` |
| `ui.image.repository` | string | UI container image repository. Default: `ghcr.io/tonedefdev/opendepot/ui` |
| `ui.image.tag` | string | Image tag. Defaults to `global.image.tag`, then the chart `appVersion`. |
| `ui.serverHost` | string | Upstream `host:port` that NGINX proxies registry requests to. Defaults to `server.<namespace>.svc.cluster.local:80` when blank. |
| `ui.sessionPasswordSecretName` | string | Name of a Kubernetes Secret with a `sessionPassword` key (min 32 chars). Required when `ui.enabled: true`. |
| `ui.oidc.enabled` | bool | Enables OIDC authorization code login in the UI. Default: `false` |
| `ui.oidc.issuerUrl` | string | Public OIDC issuer URL (must be reachable from browsers). |
| `ui.oidc.clientId` | string | OIDC client ID for the UI. Default: `"opendepot-ui"`. When `ui.oidc.enabled: true` and non-empty, the chart also passes `--oidc-ui-client-id` to the server so UI-issued tokens are accepted on browse and stats endpoints. See [Registry Explorer UI OIDC](../authentication.md#registry-explorer-ui-oidc). |
| `ui.oidc.clientSecretName` | string | Name of a Kubernetes Secret with a `clientSecret` key for the OIDC confidential client. |
| `ui.oidc.scopes` | string | Space-separated OIDC scopes. Default: `"openid profile email groups"` |
| `ui.oidc.callbackPath` | string | OIDC redirect URI path registered with the identity provider. Default: `"/auth/callback"` |
| `ui.auth.devTokenInput.enabled` | bool | When true, shows a developer bearer-token input in the UI. **Must be `false` in production.** Default: `false` |
| `ui.ingress.enabled` | bool | Creates a Kubernetes Ingress for the UI with split-path routing rules. Default: `false` |
| `ui.ingress.className` | string | Ingress class name. |
| `ui.ingress.annotations` | map | Annotations applied to the Ingress resource. |
| `ui.ingress.hosts` | list | Host and path rules. |
| `ui.ingress.tls` | list | TLS configuration for the Ingress. |

## Valkey Stats Store

Download statistics are persisted in a bundled [Valkey](https://valkey.io/) (Redis-compatible) instance deployed automatically alongside the server. No additional setup is required — Valkey is always deployed as part of the chart.

| Value | Type | Description |
|-------|------|-------------|
| `valkey.resources` | map | Resource requests and limits for the Valkey pod |
| `valkey.dataStorage.enabled` | bool | Create a PVC for Valkey data. Default: `true` |
| `valkey.dataStorage.className` | string | StorageClass for the PVC. Leave blank for the cluster default. Default: `""` |
| `valkey.dataStorage.requestedSize` | string | PVC storage size. Default: `1Gi` |
| `valkey.auth.enabled` | bool | Enable Valkey ACL password authentication. Default: `false` |
| `valkey.auth.usersExistingSecret` | string | Name of a pre-existing Secret whose keys are ACL usernames and values are plaintext passwords. Required when `valkey.auth.enabled: true`. Default: `""` |
| `valkey.auth.aclUsers.default.permissions` | string | ACL permissions string for the default user. Default: `~* &* +@all` |
| `server.stats.valkeyPasswordSecretName` | string | Name of the Secret injected as `OPENDEPOT_VALKEY_PASSWORD` into the server pod. Must match `valkey.auth.usersExistingSecret` when auth is enabled. Default: `""` |
| `valkey.nodeSelector` | map | Node selector for the Valkey pod |
| `valkey.tolerations` | list | Tolerations for the Valkey pod |
| `valkey.affinity` | map | Affinity rules for the Valkey pod |

When `valkey.dataStorage.enabled: true` (the default), a PVC is created and mounted at `/data` in the Valkey pod. Set `valkey.dataStorage.enabled: false` to use ephemeral in-pod storage — suitable for local development or Kind clusters where no StorageClass is available. Stats are lost on pod restart when persistence is disabled.

!!! warning "Production Security"
    Valkey ACL authentication is **disabled by default**. For production deployments, create a Kubernetes Secret containing the password, then configure `valkey.auth.enabled: true`, `valkey.auth.usersExistingSecret`, and `server.stats.valkeyPasswordSecretName` to point at it. For regulated environments, use [External Secrets Operator](https://external-secrets.io/) or HashiCorp Vault to provision the Secret rather than storing the password in `values.yaml`.

See [Download Tracking](guides/registry-explorer.md#download-tracking) for details on how stats are recorded and surfaced in the Registry Explorer UI.

## Scanning Values

The `scanning` section controls Trivy-based provider vulnerability scanning. See [Vulnerability Scanning](configuration/scanning.md) for full details.

```yaml
scanning:
  enabled: false
  providerScanning: false
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

