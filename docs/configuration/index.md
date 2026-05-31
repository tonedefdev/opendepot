---
tags:
  - configuration
  - helm
  - tls
  - github
  - gpg
---

# Configuration

Configure OpenDepot for your environment. All configuration is done through Helm chart values — no config files to manage.

<div class="grid cards" markdown>

- :material-shield-account: &nbsp;[__OIDC Authentication (Dex)__](oidc.md)

    ---

    Deploy Dex as a bundled OIDC identity provider to enable `tofu login` and single sign-on via Entra ID, Okta, GitHub, LDAP, and other upstream IdPs.

- :material-shield-search: &nbsp;[__Vulnerability Scanning__](scanning.md)

    ---

    Enable Trivy-based vulnerability scanning for provider binaries and source dependencies, with optional policy enforcement to block critical or high findings.

- :material-key: &nbsp;[__GPG Signing__](gpg.md)

    ---

    Set up GPG signing for provider `SHA256SUMS` files so OpenTofu can cryptographically verify provider archives.

- :material-github: &nbsp;[__GitHub Authentication__](github-auth.md)

    ---

    Configure a GitHub App to authenticate API requests and increase rate limits when using the Depot controller with private repositories.

- :material-kubernetes: &nbsp;[__Namespace-Scoped Mode__](namespace.md)

    ---

    Restrict OpenDepot to a single namespace using `Role` and `RoleBinding` instead of cluster-wide `ClusterRole` resources.

- :material-lock: &nbsp;[__TLS__](tls.md)

    ---

    Terminate TLS on the OpenDepot server using a Kubernetes Secret, or delegate to an Ingress controller or service mesh.

- :material-monitor-dashboard: &nbsp;[__Registry Explorer UI__](ui.md)

    ---

    Deploy the browsable, searchable registry frontend. Configure OIDC login, session secrets, and Ingress split-path routing for the Next.js + NGINX UI pod.

</div>
