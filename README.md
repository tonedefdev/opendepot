<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/img/opendepot_dark_mode.svg" />
    <source media="(prefers-color-scheme: light)" srcset="docs/img/opendepot_light_mode.svg" />
    <img src="docs/img/opendepot_light_mode.svg" width="400" />
  </picture>
</p>

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/tonedefdev/opendepot/blob/main/LICENSE)
[![Helm](https://img.shields.io/badge/Helm_Chart-0.10.0-0F1689?logo=helm&logoColor=white)](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot)
[![Docs](https://img.shields.io/badge/Docs-opendepot.defdev.io-047df1?logo=materialformkdocs&logoColor=white)](https://opendepot.defdev.io/docs/)

A Kubernetes-native, self-hosted OpenTofu/Terraform module and provider registry that implements the [Module Registry Protocol](https://opentofu.org/docs/internals/module-registry-protocol/), the [Provider Network Mirror Protocol](https://opentofu.org/docs/internals/provider-network-mirror-protocol/) and the [Provider Registry Protocol](https://opentofu.org/docs/internals/provider-registry-protocol/). OpenDepot gives organizations complete control over distribution, versioning, and storage — without relying on the public registry.

Compatible with **OpenTofu** (all versions) and **Terraform** (v1.2+).

## Documentation

Comprehensive documentation is available at **[opendepot.defdev.io/docs/](https://opendepot.defdev.io/docs/)**.

| Guide | Description |
|-------|-------------|
| [Installation](https://opendepot.defdev.io/docs/getting-started/installation/) | Deploy OpenDepot to Kubernetes using the Helm chart |
| [Quickstart](https://opendepot.defdev.io/docs/getting-started/quickstart/) | Get up and running locally in minutes |
| [Helm Chart Reference](https://opendepot.defdev.io/docs/helm-chart/) | Full values reference for the OpenDepot Helm chart |
| [Architecture](https://opendepot.defdev.io/docs/architecture/) | How OpenDepot works under the hood |
| [Authentication](https://opendepot.defdev.io/docs/authentication/) | GitHub App and token-based auth |
| [Kubernetes RBAC](https://opendepot.defdev.io/docs/rbac/) | Fine-grained access control for registry resources |

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
