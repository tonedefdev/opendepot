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

The chart does not create the client Secret. Create it before installing. Dex
reads it through `dex.envFrom`. Use any Secret name, and set the same name in
`server.oidc.clientSecretName` and in a `dex.envFrom` `secretRef`. The Secret must
contain the `OPENDEPOT_DEX_CLIENT_SECRET` key.

```yaml
server:
  oidc:
    clientSecretName: opendepot-dex-client-secret
```

!!! warning
    When `dex.enabled` and `server.oidc.enabled` are `true`, the render fails if
    `server.oidc.clientSecretName` is empty or does not match a `dex.envFrom`
    `secretRef` name, and a static client uses `secretEnv: OPENDEPOT_DEX_CLIENT_SECRET`.

Dex loads its environment from the Secret listed in `dex.envFrom`, which defaults to
`opendepot-dex-client-secret`. If you override `dex.envFrom`, keep a `secretRef`
with the same name as `server.oidc.clientSecretName`.
The OpenDepot server container does not receive the client secret.

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

- Use an HTTPS Dex issuer in production. The server accepts `http` or `https`
  issuer URLs.
- JWT validation is local after the server fetches and caches Dex's JWKS.
- Token lifetime is set by Dex. Users can run `tofu login` again to get a new
  token.
- Do not enable `dex.config.enablePasswordDB` or `staticPasswords` in production.
- The local test environment pins Dex v2.45.0 (`make oidc-deploy`). Static-password
  test users use that version for groups claims in GroupBinding expressions.
