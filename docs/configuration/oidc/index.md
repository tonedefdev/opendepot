---
tags:
  - configuration
  - oidc
  - dex
  - authentication
  - sso
---

# OIDC Helm Configuration

OpenDepot ships Dex as a bundled Helm subchart that acts as an OIDC identity
broker. Dex federates upstream identity providers such as Entra ID, Okta,
GitHub, and LDAP, then issues standard OIDC JWTs. The server validates those
JWTs locally through JWKS.

When OIDC is enabled, the server advertises the `login.v1` block in its service
discovery response. OpenTofu uses that block to authenticate with `tofu login`
without distributing kubeconfig or ServiceAccount credentials.

!!! note
    OIDC and bearer-token modes are mutually exclusive by default. Set
    `server.oidc.enabled: true` and `server.useBearerToken: false` when
    switching to OIDC.

## Configuration Workflow

<div class="grid cards" markdown>

- :material-play-circle-outline: &nbsp;[__Core Setup__](setup.md)

    ---

    Enable Dex and OIDC on the server, apply the Helm upgrade, verify service
discovery, and authenticate with `tofu login`.

- :material-transit-connection-variant: &nbsp;[__Proxy and Deployment Modes__](proxy.md)

    ---

    Configure the recommended server-proxied Dex setup, split-network URLs, or a
    shared external Dex deployment.

- :material-connection: &nbsp;[__Connectors and Secrets__](connectors.md)

    ---

    Configure upstream IdP connectors, manage client secrets, and review OIDC
    security requirements.

- :material-console-line: &nbsp;[__CI/CD Authentication__](ci-cd.md)

    ---

    Configure GroupBinding access, ServiceAccount fallback, or Dex client
    credentials for automated pipelines.

</div>

## Prerequisites

- OpenDepot installed through Helm (see [Installation](../../getting-started/installation.md))
- A publicly reachable OpenDepot hostname. HTTPS is required in production.
- An upstream IdP OAuth application, such as a GitHub App, Azure App
  Registration, or Okta application

## How Authentication Works

Dex acts as a broker between the upstream IdP and OpenDepot. Two independent
registrations are involved:

- A **connector** tells Dex how to authenticate users against the upstream IdP.
  Its callback URL points to Dex.
- A **static client** registers OpenDepot as an application that receives tokens
  from Dex. Its redirect URIs are the localhost ports used by `tofu login`.

See [Core Setup](setup.md) for the complete flow and Helm values.
