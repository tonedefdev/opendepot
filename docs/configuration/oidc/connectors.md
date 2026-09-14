---
tags:
  - configuration
  - oidc
  - dex
  - secrets
---

# OIDC Connectors and Secrets

Dex connectors connect OpenDepot to an upstream identity provider. The
connector callback URL points to Dex, not directly to OpenDepot.

## Connector Examples

=== "Entra ID (Azure AD)"

    ```yaml
    dex:
      enabled: true
      config:
        issuer: https://opendepot.example.com/dex
        connectors:
          - type: microsoft
            id: microsoft
            name: "Azure AD"
            config:
              clientID: <azure-app-id>
              clientSecret: $AZURE_CLIENT_SECRET
              redirectURI: https://opendepot.example.com/dex/callback
              tenant: <azure-tenant-id>
      envFrom:
        - secretRef:
            name: dex-connector-secrets
    ```

    ```bash
    kubectl create secret generic dex-connector-secrets \
      --from-literal=AZURE_CLIENT_SECRET=<azure-app-secret> \
      -n opendepot-system
    ```

=== "GitHub"

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

    ```bash
    kubectl create secret generic dex-connector-secrets \
      --from-literal=GITHUB_CLIENT_SECRET=<github-oauth-app-secret> \
      -n opendepot-system
    ```

=== "Okta"

    ```yaml
    dex:
      enabled: true
      config:
        issuer: https://opendepot.example.com/dex
        connectors:
          - type: oidc
            id: okta
            name: Okta
            config:
              issuer: https://<okta-domain>/oauth2/default
              clientID: <okta-client-id>
              clientSecret: $OKTA_CLIENT_SECRET
              redirectURI: https://opendepot.example.com/dex/callback
      envFrom:
        - secretRef:
            name: dex-connector-secrets
    ```

    ```bash
    kubectl create secret generic dex-connector-secrets \
      --from-literal=OKTA_CLIENT_SECRET=<okta-client-secret> \
      -n opendepot-system
    ```

For the full list of supported connectors, see the
[Dex Connector Documentation](https://dexidp.io/docs/connectors/).

## Client Secret Management

The Dex client secret authenticates the OpenDepot client application to Dex. It
is not used by the server to validate tokens; the server validates JWTs through
the issuer's public JWKS endpoint.

| Scenario | Configuration |
|----------|--------------|
| Auto-create from value | Leave `server.oidc.clientSecretName` blank and set `server.oidc.clientSecret`. |
| Use an existing Secret | Set `server.oidc.clientSecretName` to a Secret containing a `clientSecret` key. |

!!! warning "Production requirement"
    When both `dex.enabled` and `server.oidc.enabled` are true, set either
    `server.oidc.clientSecret` or `server.oidc.clientSecretName`. For production,
    use an external secret operator or an existing Kubernetes Secret.

```yaml
server:
  oidc:
    clientSecretName: my-oidc-client-secret
```

The client secret is injected into the Dex deployment through `envFrom`. The
OpenDepot server container never receives it.

### Connector Secrets

Connector secrets are separate from the OpenDepot client secret. Store each IdP
secret in Kubernetes, reference it with `$ENV_VAR`, and mount it into Dex with
`dex.envFrom`:

```yaml
dex:
  envFrom:
    - secretRef:
        name: opendepot-dex-client-secret
        optional: true
    - secretRef:
        name: dex-connector-secrets
```

This keeps secret values out of Helm values files and in-cluster ConfigMaps.

## Security Notes

- Use HTTPS for the Dex issuer in production. HTTP is accepted only for
  `127.0.0.1` and in-cluster addresses.
- JWT validation is local after the server fetches and caches Dex's JWKS.
- JWTs are short-lived, typically one hour. Users can run `tofu login` again to
  refresh them.
- Do not enable `dex.config.enablePasswordDB` or `staticPasswords` in production.
- Dex v2.45.0 or later is required when static-password test users need groups
  claims for GroupBinding expressions.
