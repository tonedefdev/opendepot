---
tags:
  - configuration
  - oidc
  - dex
  - authentication
  - sso
---

# OIDC Authentication (Dex)

OpenDepot ships Dex as a bundled Helm subchart that acts as an OIDC identity broker. Dex federates upstream IdPs (Entra ID, Okta, GitHub, LDAP, and more) and issues standard OIDC JWTs. The server validates those JWTs locally via JWKS — no Dex round-trip on every request.

When OIDC is enabled the server advertises the `login.v1` block in its service discovery response, which allows users to authenticate with `tofu login` instead of distributing kubeconfigs or service account tokens.

!!! note
    OIDC and bearer-token modes are mutually exclusive by default. Set `server.oidc.enabled: true` and `server.useBearerToken: false` when switching to OIDC. If you also need CI/CD pipelines to authenticate with a Kubernetes ServiceAccount, see [CI/CD with ServiceAccount Fallback](#cicd-with-serviceaccount-fallback) below.

## Prerequisites

- OpenDepot installed via Helm (see [Installation](../getting-started/installation.md))
- A publicly reachable hostname for the Dex issuer URL (HTTPS required in production)
- An upstream IdP OAuth application (GitHub App, Azure App Registration, Okta app, etc.)

## Step 1: Enable Dex

Set `dex.enabled: true` and configure the `issuer` and at least one connector in your Helm values:

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
          org: my-org  # (optional) restrict to an organization
```

See [Connector Examples](#connector-examples) below for Entra ID (Azure AD) and other IdP configurations.

## Step 2: Enable OIDC on the Server

Add the `server.oidc` block to the same values file:

```yaml
server:
  useBearerToken: false
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex  # omit to auto-derive in-cluster Dex URL
    clientId: opendepot
    clientSecret: <strong-random-value>
```

!!! warning
    Do not commit `clientSecret` in plain text. Use an external secret operator (e.g., Sealed Secrets, External Secrets Operator) to inject the value in production. Alternatively, create the Secret manually and set `server.oidc.clientSecretName` to its name.

When `server.oidc.issuerUrl` is blank and `dex.enabled: true`, the chart auto-derives the in-cluster URL:

```
http://<release-name>-dex.<namespace>.svc.cluster.local:5556/dex
```

## Step 3: Apply the Helm Upgrade

```bash
helm upgrade opendepot opendepot/opendepot \
  -n opendepot-system \
  --reuse-values \
  -f oidc-values.yaml \
  --wait
```

Verify the server pod is running and OIDC flags appear in the container args:

```bash
kubectl get pods -n opendepot-system
kubectl describe pod -n opendepot-system -l app=server | grep oidc
```

## Step 4: Verify Service Discovery

When OIDC is enabled the `/.well-known/terraform.json` response includes a `login.v1` object:

```bash
curl https://opendepot.example.com/.well-known/terraform.json
```

```json
{
  "modules.v1": "/opendepot/modules/v1/",
  "providers.v1": "/opendepot/providers/v1/",
  "login.v1": {
    "authz": "https://dex.example.com/dex/auth",
    "token": "https://dex.example.com/dex/token",
    "grant_types": ["authz_code", "device_code"]
  }
}
```

If `login.v1` is absent, OIDC is not enabled or the server has not restarted after the Helm upgrade.

## Step 5: Authenticate with `tofu login`

Users run `tofu login` once and obtain a JWT that is cached locally:

```bash
tofu login opendepot.example.com
```

OpenTofu opens a browser window redirecting to Dex. After signing in through the upstream IdP, Dex issues a JWT and OpenTofu stores it in `~/.terraform.d/credentials.tfrc.json`. Subsequent `tofu init`, `tofu plan`, and `tofu apply` commands send the JWT as a bearer token automatically.

On headless systems (CI, servers), the device code flow is used instead — OpenTofu prints a URL and a short code to enter in a browser elsewhere.

## Connector Examples

=== "Entra ID (Azure AD)"

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

=== "GitHub"

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
              org: my-org
    ```

=== "Okta"

    ```yaml
    dex:
      enabled: true
      config:
        issuer: https://dex.example.com/dex
        connectors:
          - type: oidc
            id: okta
            name: Okta
            config:
              issuer: https://<okta-domain>/oauth2/default
              clientID: <okta-client-id>
              clientSecret: <okta-client-secret>
              redirectURI: https://dex.example.com/dex/callback
    ```

For the full list of supported connectors and their configuration options, see the [Dex Connector Documentation](https://dexidp.io/docs/connectors/).

## Client Secret Management

The Dex client secret authenticates the OpenDepot client application to Dex. It is **not** used by the server to validate tokens — the server validates JWTs using the issuer's public JWKS endpoint.

The chart manages the secret in two ways:

| Scenario | Configuration |
|----------|--------------|
| Auto-create from value | Leave `server.oidc.clientSecretName` blank; set `server.oidc.clientSecret` to the desired value. The chart creates an `opendepot-dex-client-secret` Secret automatically. |
| Use an existing Secret | Set `server.oidc.clientSecretName` to the name of a pre-existing Secret that contains a `clientSecret` key. The chart skips Secret creation. |

!!! warning
    The client secret is injected only into the Dex deployment via `envFrom`. The OpenDepot server container never receives it.

## Fine-Grained Access Control (GroupBinding)

After OIDC is enabled, you can deploy `GroupBinding` resources to restrict which modules and providers each group of users may access. The server extracts the groups claim from the JWT and evaluates GroupBindings in alphabetical order by name. The first matching binding determines the allowed resources. If a binding expression fails to compile or evaluate, the request is denied with `403 Forbidden`.

### Groups Claim Name

By default the server reads the `groups` JWT claim. If your IdP uses a different name, set `server.oidc.groupsClaim`:

```yaml
server:
  oidc:
    groupsClaim: "cognito:groups"  # default is "groups"
```

### Required Groups Claim

The groups claim is **required** when OIDC is enabled. A valid JWT that does not carry the configured claim is denied with **403 Forbidden** — there is no bypass path. Configure your IdP connector in Dex to emit the claim before enabling OIDC in production.

See [Fine-Grained Access Control with GroupBinding](../guides/groupbinding.md) for a complete guide, including expression syntax, glob pattern reference, and example manifests.

## Security Notes

- **HTTPS required in production**: The Dex `issuer` URL must use HTTPS. HTTP is accepted only for `127.0.0.1` and in-cluster addresses.
- **JWT validation is local**: The server fetches JWKS from Dex at startup and caches them. No request to Dex is made per API call.
- **Token lifetime**: JWTs are short-lived (typically 1 hour). Users re-run `tofu login` to refresh; CI systems use the device code flow.
- **No `staticPasswords` in production**: The `dex.config.enablePasswordDB` and `dex.config.staticPasswords` options exist for automated e2e testing only. Never enable them in production environments.

For a full comparison of all authentication methods, see [Authenticating with OpenDepot](../authentication.md).

## CI/CD with ServiceAccount Fallback

By default, when OIDC is enabled every token must be a valid Dex JWT. This blocks CI/CD pipelines that use a Kubernetes ServiceAccount to authenticate — the SA token has a different issuer and will be rejected with `401 Unauthorized`.

Set `server.oidc.allowServiceAccountFallback: true` to opt in to mixed-mode authentication:

```yaml
server:
  oidc:
    enabled: true
    allowServiceAccountFallback: true
```

With this flag, the server inspects the `iss` claim of any token that fails OIDC verification. If the issuer does not match the configured OIDC issuer URL, the token is forwarded to the Kubernetes API as a bearer token and the SA's own RBAC determines access. GroupBinding is not evaluated for SA tokens — it is an OIDC-layer concern only.

| Token | Behaviour |
|---|---|
| Valid Dex JWT | OIDC path → GroupBinding → server SA for K8s calls |
| Bad/expired Dex JWT | `401 Unauthorized` (issuer matches, not a fallback candidate) |
| K8s SA token | Bearer token path → SA's own RBAC controls access |
| Garbage non-JWT | `401 Unauthorized` |

!!! note
    Tokens that claim the OIDC issuer but fail signature or expiry checks are **never** routed to the SA fallback path. Only tokens from a clearly different issuer fall back.

### Required RBAC for SA tokens

The SA must have `get` and `list` verbs on the resources it needs to access. For a pipeline that downloads modules and providers:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: opendepot-registry-reader
  namespace: opendepot-system
rules:
- apiGroups: ["opendepot.defdev.io"]
  resources: ["modules", "versions", "providers"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opendepot-registry-reader-binding
  namespace: opendepot-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: opendepot-registry-reader
subjects:
- kind: ServiceAccount
  name: my-ci-sa
  namespace: my-ci-namespace
```

For guidance on using this in a CI/CD pipeline, see [Registry Reads: SA Fallback with OIDC](../guides/cicd.md#registry-reads-sa-fallback-with-oidc).
