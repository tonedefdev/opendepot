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

- :material-kubernetes: &nbsp;[__Namespace-Scoped Mode__](namespace.md)

    ---

    Restrict OpenDepot to a single namespace using `Role` and `RoleBinding` instead of cluster-wide `ClusterRole` resources.

- :material-github: &nbsp;[__GitHub Authentication__](github-auth.md)

    ---

    Configure a GitHub App to authenticate API requests and increase rate limits when using the Depot controller with private repositories.

- :material-lock: &nbsp;[__TLS__](tls.md)

    ---

    Terminate TLS on the OpenDepot server using a Kubernetes Secret, or delegate to an Ingress controller or service mesh.

- :material-key: &nbsp;[__GPG Signing__](gpg.md)

    ---

    Set up GPG signing for provider `SHA256SUMS` files so OpenTofu can cryptographically verify provider archives.

</div>
