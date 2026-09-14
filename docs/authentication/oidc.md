---
tags:
  - authentication
  - oidc
  - dex
  - sso
---

# OIDC via Dex

OIDC authentication enables single sign-on through an existing identity
provider without distributing credentials or kubeconfigs. Dex is bundled as a
Helm subchart and federates upstream providers such as Entra ID, Okta, GitHub,
and LDAP.

For Helm values, Dex connectors, static clients, and the `tofu login` flow, see
[OIDC Helm Configuration](../configuration/oidc/index.md).

## Browse Endpoint Authentication Enforcement

When `server.oidc.enabled: true` and `server.anonymousAuth: false`, browse API
endpoints under `/opendepot/ui/v1/*` require a valid
`Authorization: Bearer <token>` header. Requests without a valid token return
`401 Unauthorized`, including requests with an invalid or expired JWT.

A valid token with no matching `GroupBinding` is accepted and receives
public-only visibility. The gate blocks callers with no token or a token that
fails all verification paths. When `server.anonymousAuth: true`, or OIDC is not
configured, browse endpoints remain accessible without authentication.

!!! warning
    Set `server.anonymousAuth: false` together with
    `server.oidc.enabled: true` in production to prevent unauthenticated access
    to the browse API.

## Registry Explorer UI OIDC

When `ui.oidc.enabled: true`, the UI uses a dedicated Dex client, normally
`opendepot-ui`, separate from the `tofu login` client `opendepot`. Set the UI
client ID so the Server accepts UI-issued tokens:

```yaml
ui:
  oidc:
    enabled: true
    clientId: opendepot-ui
    clientSecretName: opendepot-ui-oidc-secret
```

The Dex `opendepot-ui` client must include `trustedPeers: [opendepot]` so Dex
includes the Server audience in UI-issued tokens:

```yaml
dex:
  config:
    staticClients:
      - id: opendepot-ui
        name: OpenDepot UI
        secret: <ui-client-secret>
        trustedPeers:
          - opendepot
        redirectURIs:
          - https://<ui-host>/auth/callback
```

The UI requires an HTTPS issuer in production. Its discovered authorization,
token, and JWKS endpoints must share the configured issuer origin unless an
explicit `ui.oidc.authzUrl` browser override is used. Insecure HTTP endpoints
are enabled only when `global.developmentMode: true`.

!!! note
    Without the Server's UI client ID configured, the Stats page and browse
    endpoints may return empty results for users authenticated through the UI's
    OIDC client.

## GroupBinding Access Control

After OIDC authentication, the Server extracts the JWT `groups` claim and
matches it against [GroupBinding](../guides/groupbinding.md) expressions. The
`groups` claim is required. Requests with no claim, or with a claim that matches
no GroupBinding, receive `403 Forbidden`.

!!! warning
    If no GroupBinding resources exist in the Server namespace, all
    OIDC-authenticated users are denied. Deploy at least one GroupBinding before
    enabling OIDC in production.

See [Fine-Grained Access Control with GroupBinding](../guides/groupbinding.md) for
expression syntax and setup instructions.

## CI/CD with ServiceAccount Fallback

!!! danger "GroupBinding is bypassed for ServiceAccount tokens"
    When ServiceAccount fallback is enabled, ServiceAccount tokens skip
    GroupBinding entirely. Kubernetes RBAC is the only access control applied to
    those tokens. This creates a separate access-control path alongside OIDC.

    Before enabling fallback, consider the [GitOps workflow](../guides/gitops.md)
    for publishing modules or [Dex Client Credentials](../guides/cicd.md#registry-reads-with-dex-client-credentials)
    for registry reads that continue to respect GroupBinding.

See [CI/CD with ServiceAccount Fallback](../configuration/oidc/ci-cd.md#serviceaccount-fallback)
for setup details and required RBAC.

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| `missing Authorization header` | `.tofurc` has no credentials block | Add a `credentials "host" { token = "..." }` block |
| `unauthorized` | JWT expired or invalid | Run `tofu login` again |
| Server pod is `CrashLoopBackOff` | Issuer URL is missing or misconfigured | Verify `server.oidc.issuerUrl` and inspect pod logs |
| Browser opens to an in-cluster hostname | Issuer URL was left blank | Set both `dex.config.issuer` and `server.oidc.issuerUrl` to the external Dex URL |
| Browser redirects to localhost but fails | Dex redirect URI is not registered | Add the expected localhost ports to `redirectURIs` |
| Stats shows zeroes for UI-authenticated users | UI client ID was not propagated to the Server | Enable `ui.oidc` and set a non-empty `ui.oidc.clientId` |
