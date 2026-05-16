---
tags:
  - authentication
  - kubernetes
  - security
search:
  boost: 2
---

# Authenticating with OpenDepot

OpenDepot supports three authentication methods: OIDC JWTs via Dex, Kubernetes bearer tokens, and base64-encoded kubeconfigs. Bearer tokens and OIDC are recommended for production; kubeconfig is primarily for development.

### Method 1: OIDC via Dex (Recommended for Production)

OIDC authentication enables single sign-on (SSO) via existing identity providers without distributing credentials or kubeconfigs. The `tofu login` workflow guides users through browser-based authentication to obtain a JWT.

#### Overview

Dex is bundled as a Helm subchart that acts as an OIDC identity broker, federating upstream IdPs (Entra ID, Okta, GitHub, LDAP, etc.) and issuing standard OIDC JWTs. The OpenDepot server validates these JWTs locally via JWKS, enabling the `tofu login` workflow.

#### Helm Setup

Enable Dex and OIDC on the server:

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
          clientID: <github-app-client-id>
          clientSecret: <github-app-client-secret>
          redirectURI: https://dex.example.com/dex/callback

server:
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex
    clientId: opendepot
    clientSecret: <strong-random-value>
```

!!! warning
    Never commit `clientSecret` in plain text. Use an external secret operator (e.g., Sealed Secrets, External Secrets) to manage the value in production.

When `issuerUrl` is blank and `dex.enabled: true`, the chart automatically derives the in-cluster Dex service URL (`http://opendepot-dex.<namespace>.svc.cluster.local:5556/dex`). **This is not reachable from a browser.** The server derives the `login.v1.authz` and `login.v1.token` URLs in service discovery directly from the issuer URL, so if the in-cluster address is used, `tofu login` will attempt to open a browser tab to a URL that cannot be resolved from the user's machine.

**Always set both `dex.config.issuer` and `server.oidc.issuerUrl` to the same URL that is reachable from the user's browser** when `tofu login` is required. Options include an Ingress hostname, a LoadBalancer Service address, minikube tunnel, or `kubectl port-forward` to `localhost` (both Dex and OpenTofu accept `http://localhost` natively, so no TLS is needed for local development).

#### Connector Examples

=== "Entra ID (Azure AD)"

    ```yaml
    dex:
      enabled: true
      config:
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

!!! note
    `staticPasswords` is provided for automated e2e testing only. Do not enable in production.

#### Using `tofu login`

After Dex and OIDC are configured, users authenticate once and obtain a JWT:

```bash
tofu login opendepot.defdev.io
```

The server advertises the `login.v1` service discovery endpoint with authorization and token URLs derived from `server.oidc.issuerUrl`. `tofu login` uses the OAuth2 authorization code + PKCE flow: it opens a browser to Dex's authorization URL and listens on a local port (`10000`–`10010`) for the redirect. **Dex must therefore be reachable from the user's browser** — not just from inside the cluster. Verify that `login.v1.authz` in the service discovery response resolves to an externally accessible hostname before asking users to run `tofu login`:

```bash
curl -s https://opendepot.defdev.io/.well-known/terraform.json | jq '."login.v1".authz'
```

If the output contains an in-cluster service name rather than a public hostname, `server.oidc.issuerUrl` was not set or was set incorrectly. Correct it and redeploy before proceeding.

Once confirmed, subsequent `tofu` commands use the JWT as the bearer token.

**Example `.tofurc` configuration:**

```hcl
credentials "opendepot.defdev.io" {
  token = "<jwt-from-tofu-login>"
}

host "registry.terraform.io" {
  client_id     = "opendepot"
  client_secret = "<dex-client-secret>"
  login_uri     = "https://opendepot.defdev.io/v1/login"
  token_uri     = "https://dex.example.com/dex/token"
}
```

#### GroupBinding Access Control

When [GroupBinding](guides/groupbinding.md/) resources are deployed, the server enforces fine-grained access control after OIDC authentication. The user's groups claim is extracted from the JWT and matched against GroupBinding expressions to determine which modules and providers the user may access.

The groups claim is **required** — the three possible outcomes are:

- **Groups claim absent** — request is **denied with 403 Forbidden**. The claim must be present.
- **Groups claim present, no GroupBinding matches** — request is **denied with 403 Forbidden**.
- **Groups claim present, a GroupBinding matches** — access is governed by that binding's `moduleResources` glob patterns and `providerResources` exact-name list.

!!! warning
    If no `GroupBinding` resources exist in the server namespace, **all** OIDC-authenticated users are denied regardless of their groups. Deploy at least one `GroupBinding` before enabling OIDC in production.

To use a non-standard claim name, set `server.oidc.groupsClaim` in your Helm values. See [Fine-Grained Access Control with GroupBinding](guides/groupbinding.md/) for full setup instructions.

#### CI/CD with ServiceAccount Fallback

By default, OIDC and bearer-token modes are mutually exclusive. If you need CI/CD pipelines to authenticate using a Kubernetes ServiceAccount while human users authenticate via OIDC, enable the SA fallback:

```yaml
server:
  oidc:
    allowServiceAccountFallback: true
```

With this flag, K8s SA tokens (identified by a non-OIDC `iss` claim) are routed to the bearer-token path, and the SA's own RBAC controls access. GroupBinding is not evaluated for SA tokens. See [CI/CD with ServiceAccount Fallback](configuration/oidc.md#cicd-with-serviceaccount-fallback) for full setup details.

#### Security Notes

- **HTTPS required**: In production, issuer URLs must use HTTPS. HTTP is allowed only for localhost (127.0.0.1) and testing.
- **No credential distribution**: Users authenticate directly with Dex; the server never sees or stores user passwords.
- **JWT validation**: JWTs are validated locally using the issuer's JWKS. No call to Dex is made on every request.
- **Token expiry**: JWTs have a short lifespan (typically 1 hour). Users re-run `tofu login` to refresh.
- **Never enable `staticPasswords` in production**: Use real IdP connectors instead.

#### Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| `missing Authorization header` | `.tofurc` missing `credentials` block | Add `credentials "host" { token = "..." }` block |
| `unauthorized` | JWT expired or invalid | Re-run `tofu login` to obtain a fresh JWT |
| `CrashLoopBackOff` on server pod | `server.oidc.issuerUrl` not set or misconfigured | Verify OIDC is enabled and issuer URL is correct; check pod logs |
| Browser opens to an in-cluster hostname (e.g. `opendepot-dex.opendepot-system.svc...`) | `server.oidc.issuerUrl` was left blank; chart derived the in-cluster service URL | Set both `dex.config.issuer` and `server.oidc.issuerUrl` to the external Dex URL and redeploy |
| Browser redirects to localhost but connection fails | Dex `redirectURI` not included in client config | Add all expected localhost ports (10000-10010) to Dex client redirectURIs |

### Method 2: Managed Cluster Tokens

Use the token issued by your cluster's native auth flow when the CI job already has access to the Kubernetes API. OpenDepot forwards that token to Kubernetes; if the API server accepts it, registry reads and module downloads work without Dex credentials.

The variable name is derived from the registry hostname: replace dots with underscores and convert to uppercase.

`opendepot.defdev.io` → `TF_TOKEN_OPENDEPOT_DEFDEV_IO`

=== "Amazon EKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
      --cluster-name my-cluster \
      --region us-west-2 \
      --output json | jq -r '.status.token')

    tofu init
    tofu plan
    ```

=== "Azure AKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(az account get-access-token \
      --resource 6dae42f8-4368-4678-94ff-3960e28e3630 \
      --query accessToken -o tsv)

    tofu init
    tofu plan
    ```

=== "Google GKE"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(gcloud auth print-access-token)

    tofu init
    tofu plan
    ```

Tokens are short-lived and automatically rotate, making this the preferred option for CI/CD jobs when the cluster accepts the provider-issued token.

#### CI/CD Example

```yaml
name: Apply Infrastructure

on:
  push:
    branches: [main]

jobs:
  apply:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::ACCOUNT_ID:role/github-actions-role
          aws-region: us-west-2

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - name: Set registry token
        run: |
          TOKEN=$(aws eks get-token --cluster-name my-cluster --region us-west-2 --output json | jq -r '.status.token')
          echo "TF_TOKEN_OPENDEPOT_DEFDEV_IO=$TOKEN" >> $GITHUB_ENV

      - run: tofu init
      - run: tofu plan
```

### Method 3: Base64-Encoded Kubeconfig (Local Development)

For development or environments where environment variables are not practical, encode your kubeconfig and store it in a credentials file.

!!! note
    This method requires `server.useBearerToken: false` in your Helm values.

**1. Encode your kubeconfig:**

```bash
kubectl config view --raw | base64 | tr -d '\n' > /tmp/kubeconfig.b64
```

**2. Create `~/.terraform.d/credentials.tfrc.json`:**

```json
{
  "credentials": {
    "opendepot.defdev.io": {
      "token": "<contents-of-kubeconfig.b64>"
    }
  }
}
```

```bash
chmod 600 ~/.terraform.d/credentials.tfrc.json
```

### Authentication Comparison

| Feature | Bearer Token | Kubeconfig File | OIDC (Dex) | OIDC + SA Fallback |
|---------|---------------------|-----------------|------------|-------------------|
| Token Lifetime | Short-lived (auto-rotating) | Long-lived (manual rotation) | Short-lived (1 hour typical) | Mixed (SA token + JWT) |
| Security | High | Good | Highest | High |
| Setup Complexity | Low | Low | Medium | Medium |
| Credential Distribution | Via env var or shell | File-based | No distribution (SSO) | No distribution |
| Best For | Production, CI/CD | Development | Enterprise production (SSO) | OIDC orgs with CI/CD pipelines |
| `tofu login` Support | No | No | Yes | Yes (human users) |
| OpenTofu Support | All versions | All versions | All versions | All versions |
| Terraform Support | v1.2+ | All versions | v1.3+ | v1.3+ |
| IdP Integration | No | No | Yes (GitHub, Entra ID, Okta, LDAP, etc.) | Yes |
