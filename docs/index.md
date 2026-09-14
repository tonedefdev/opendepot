---
template: home.html
hide:
    - toc
    - title
tags:
    - home
---

## Why OpenDepot?

Managing Terraform and OpenTofu modules and providers often means operating a separate registry, authentication flow, storage layer, and security workflow. OpenDepot brings those concerns into the Kubernetes operating model you already use: it's **free, open source, and Kubernetes-native**, with vulnerability scanning, automatic version discovery, a comprehensive User Interface, and OIDC-based SSO included out of the box.

The server and UI is read-only by design, and Kubernetes RBAC remains the authorization layer for create, update, and delete operations. Deployment requires nothing beyond a Helm chart and a storage backend.

<div class="grid cards" markdown>

- :material-view-dashboard-outline: &nbsp;__Registry Explorer UI__

    ---

    Browse and search modules, providers, versions, READMEs, vulnerability findings, depot relationships, and download statistics from one interface. See the [Registry Explorer guide](guides/registry-explorer/index.md) or [walk through the UI, Dex SSO, and GroupBinding access control](https://www.defdev.io/blog/ui-sso-in-opendepot).

- :material-login: &nbsp;__OIDC Single Sign-On (SSO)__

    ---

    First-class support for the OpenTofu login flow via the bundled [Dex](https://dexidp.io/) subchart. Connect any OIDC-compatible identity provider — GitHub, Entra ID, Okta, or static passwords — and let `tofu login` handle credential acquisition automatically.

- :material-tag-check: &nbsp;__Automatic Discovery & Provider Mirroring__

    ---

    The Depot controller discovers module releases and provider versions from their configured upstream registries. Use the [Provider Network Mirror Protocol](guides/providers.md#default-workflow-network-mirror) to install mirrored providers through OpenDepot while keeping canonical provider source addresses unchanged in module configurations and lockfiles.

- :material-shield-check: &nbsp;__Security First__

    ---

    OIDC is the preferred authentication path (via Dex and your upstream IdP), while the server stays read-only by design. Kubernetes RBAC authorizes create, update, and delete operations — no proprietary tokens, no user database, no extra identity store.


- :material-database-off: &nbsp;__No External Database__

    ---

    The Kubernetes API stores registry state, while the bundled Valkey instance persists download statistics. No separately managed application database is required.

- :material-cloud-check: &nbsp;__Multi-Cloud Storage__

    ---

    S3, Azure Blob, Google Cloud Storage, and local filesystem — all supported out of the box with SDK-native authentication chains.

- :material-shield-refresh: &nbsp;__Self-Healing & Tamper Resistance__

    ---

    Declarative controllers continuously reconcile toward desired state and retry transient failures. For immutable versions, RBAC-protected checksums are verified on every reconciliation to detect artifact replacement.

- :material-magnify-scan: &nbsp;__Built-In Vulnerability Scanning__

    ---

    The Version controller runs [Trivy](https://trivy.dev/) automatically on every provider binary, provider source (`go.mod`), and module archive. Findings are stored on the Kubernetes resource and can optionally block promotion of critical or high severity artifacts.

- :material-link-variant: &nbsp;__Zero-Egress Provider Downloads__

    ---

    Enable pre-signed URL redirects so OpenTofu and Terraform fetch provider binaries directly from S3, GCS, or Azure Blob — no bandwidth through the server, no extra hops, no infrastructure bottleneck.

</div>

!!! tip
    If you're already running Kubernetes, OpenDepot gives you automatic version discovery, built-in vulnerability scanning, and Kubernetes-native auth without adding a license fee or a new piece of infrastructure to operate.

## Next Steps

<div class="grid cards" markdown>

- :material-rocket-launch: &nbsp;[__Install with Helm__](getting-started/installation.md)

    Deploy OpenDepot to your cluster in minutes.

- :material-laptop: &nbsp;[__Local Quickstart__](getting-started/quickstart.md)

    Run a fully functional registry locally with `kind` — no cloud account needed.

- :material-sitemap: &nbsp;[__Architecture__](architecture.md)

    Understand how the four services interact and reconcile.

- :material-cog-outline: &nbsp;[__Configuration__](configuration/index.md)

    Configure authentication, storage, TLS, scanning, and other deployment options.

- :material-book-open-variant: &nbsp;[__Guides__](guides/index.md)

    GitOps, CI/CD, Depot, provider consumption, migration, and upgrade workflows.

- :material-book-check: &nbsp;[__Reference__](reference/index.md)

    APIs, version constraints, Helm values, and Kubernetes RBAC reference material.

</div>
