---
tags:
  - authentication
  - kubernetes
  - security
search:
  boost: 2
---

# Authentication

OpenDepot supports several authentication paths for registry clients and the
Registry Explorer UI. Choose the workflow that matches the credentials your
users or pipelines already have.

<div class="grid cards" markdown>

- :material-shield-account: &nbsp;[__OIDC via Dex__](oidc.md)

    ---

    Recommended for production users and SSO. Authenticate with `tofu login`,
    federate an existing identity provider, and apply GroupBinding access rules.

- :material-cloud-key: &nbsp;[__Managed Cluster Tokens__](managed-cluster-tokens.md)

    ---

    Use short-lived EKS, AKS, or GKE tokens when a pipeline already authenticates
    to the Kubernetes cluster.

- :material-file-lock: &nbsp;[__Base64-Encoded Kubeconfig__](kubeconfig.md)

    ---

    Use a kubeconfig credential for local development or environments where an
    environment token is not practical.

- :material-monitor-dashboard: &nbsp;[__Registry Explorer UI__](registry-explorer.md)

    ---

    Configure browser sessions, UI OIDC, public visibility, and developer token
    input for the Registry Explorer.

</div>

## Choosing a Method

| Feature | Managed Cluster Token | Kubeconfig File | OIDC (Dex) | OIDC + ServiceAccount Fallback |
|---------|------------------------|-----------------|------------|--------------------------------|
| Token lifetime | Short-lived, auto-rotating | Long-lived, manual rotation | Short-lived, typically one hour | Mixed: ServiceAccount token and JWT |
| Security | High | Good | Highest | High |
| Setup complexity | Low | Low | Medium | Medium |
| Credential distribution | Environment variable or shell | Credentials file | No distribution through SSO | No distribution for human users |
| `tofu login` support | No | No | Yes | Yes for human users |
| Best for | Production CI/CD | Development | Enterprise production | OIDC deployments that require direct cluster API access |

!!! warning
    OIDC and bearer-token modes are mutually exclusive by default. Set
    `server.oidc.enabled: true` and `server.useBearerToken: false` when switching
    to OIDC. For CI/CD alternatives, see [Managed Cluster Tokens](managed-cluster-tokens.md)
    and [OIDC ServiceAccount fallback](oidc.md#cicd-with-serviceaccount-fallback).

## Access Control

OIDC-authenticated requests can be restricted with
[GroupBinding](../guides/groupbinding.md) resources. ServiceAccount tokens use
Kubernetes RBAC instead; when ServiceAccount fallback is enabled, those tokens
bypass GroupBinding entirely.

The Server supports anonymous access for public registries. When OIDC is enabled
and `server.anonymousAuth: false`, browse API requests require a valid bearer
token. See [OIDC](oidc.md#browse-endpoint-authentication-enforcement) for the
exact enforcement behavior.
