---
tags:
  - configuration
  - oidc
  - dex
  - networking
---

# OIDC Proxy and Deployment Modes

OpenDepot supports a server-proxied Dex deployment, a split-network deployment,
and a shared external Dex deployment.

## Proxy Dex Through the Server

Starting with v0.10.0, `server.oidc.dexProxy.enabled` defaults to `true`. The
server reverse-proxies `/dex/*` requests to the bundled Dex service, so Dex does
not need its own public ingress or hostname. Expose the existing
`server.ingress` or `ui.ingress` and route `/dex` alongside the registry paths.

Set Dex's issuer and the server's issuer URL to the same external path:

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex

server:
  oidc:
    enabled: true
    issuerUrl: https://opendepot.example.com/dex
    clientId: opendepot
    clientSecretName: opendepot-dex-client-secret
    dexProxy:
      enabled: true
```

The server discovers Dex and fetches JWKS through the in-cluster service address.
External clients use the path-based issuer and can use authorization code, device
code, and client credentials flows through the existing ingress.

!!! note
    With `dexProxy.enabled: true`, `server.oidc.authzUrl` and
    `server.oidc.tokenUrl` are usually unnecessary. `login.v1` uses the URLs
    from Dex's external discovery document.

!!! warning "Required settings"
    The chart fails to render when `dexProxy.enabled` is `true` and either
    `dex.enabled` is `false` or `server.oidc.issuerUrl` is blank. The proxy
    only works with the bundled Dex subchart.

## Split-Network OIDC

Use `server.oidc.authzUrl` and `server.oidc.tokenUrl` when the server must reach
Dex through an in-cluster URL but clients need separate externally reachable
endpoints:

```yaml
server:
  oidc:
    enabled: true
    issuerUrl: http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex
    authzUrl: https://dex.example.com/dex/auth
    tokenUrl: https://dex.example.com/dex/token
```

When either value is blank, the URL comes from the Dex discovery document.

!!! note "Local Kind testing"
    The local Kind Make targets use the server-proxied mode because it requires
    only one port-forward. See [Local OIDC E2E Testing](../../contributing.md#local-oidc-e2e-testing).

## Shared External Dex

Use a shared Dex instance when multiple OpenDepot releases share a cluster. Set
`dex.enabled: false` and point each server at the shared issuer:

```yaml
dex:
  enabled: false

server:
  oidc:
    enabled: true
    issuerUrl: https://dex.example.com/dex
    clientId: opendepot-team-a
```

Each release requires its own `staticClient` entry in the shared Dex config,
including all redirect URIs used by `tofu login`:

```yaml
staticClients:
  - id: opendepot-team-a
    name: OpenDepot Team A
    public: true
    redirectURIs:
      - http://localhost:10000/login
      - http://localhost:10001/login
  - id: opendepot-team-b
    name: OpenDepot Team B
    public: true
    redirectURIs:
      - http://localhost:10000/login
      - http://localhost:10001/login
```

Use a distinct `server.oidc.clientId` for every release. Distinct IDs provide
Dex-level client isolation; access within each instance remains controlled by
GroupBinding.
