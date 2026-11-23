<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/img/opendepot_dark_mode.svg" />
    <source media="(prefers-color-scheme: light)" srcset="docs/img/opendepot_light_mode.svg" />
    <img src="docs/img/opendepot_light_mode.svg" width="400" />
  </picture>
</p>

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://github.com/tonedefdev/opendepot/blob/main/LICENSE)
[![Helm](https://img.shields.io/badge/Helm_Chart-0.12.0-0F1689?logo=helm&logoColor=white)](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot)
[![Docs](https://img.shields.io/badge/Docs-opendepot.defdev.io-047df1?logo=materialformkdocs&logoColor=white)](https://opendepot.defdev.io/docs/)

OpenDepot is an open source artifact registry for Kubernetes. It is self-hosted and runs in your cluster, giving organizations complete control over distribution, versioning, and storage without relying on the public registry.

Compatible with **OpenTofu** (all versions) and **Terraform** (v1.2+).

## What it serves

| Artifact | Status | Description |
|----------|--------|-------------|
| OpenTofu and Terraform modules | Available | Served through the [Module Registry Protocol](https://opentofu.org/docs/internals/module-registry-protocol/). |
| OpenTofu and Terraform providers | Available | Served through the [Provider Registry Protocol](https://opentofu.org/docs/internals/provider-registry-protocol/) and the [Provider Network Mirror Protocol](https://opentofu.org/docs/internals/provider-network-mirror-protocol/). |
| Agent Skills and Agents | Available | Published from Git and installed with the `opendepot` CLI, which verifies signatures and hashes before it writes anything to disk. |
| Helm charts | Planned | Not yet available. The OpenDepot chart itself is published from this repository. |
| MCP servers | Planned | Not yet available. |
| OCI artifacts | Planned | Not yet available. |

Jev is an optional AI assessment of Skill and Agent Versions. It is off by default. When enabled, it can block a Version, but it never bypasses signature or hash checks.

## Documentation

Comprehensive documentation is available at **[opendepot.defdev.io/docs/](https://opendepot.defdev.io/docs/)**.

| Guide | Description |
|-------|-------------|
| [Installation](https://opendepot.defdev.io/docs/getting-started/installation/) | Deploy OpenDepot to Kubernetes using the Helm chart |
| [Quickstart](https://opendepot.defdev.io/docs/getting-started/quickstart/) | Get up and running locally in minutes |
| [Helm Chart Reference](https://opendepot.defdev.io/docs/helm-chart/) | Full values reference for the OpenDepot Helm chart |
| [Architecture](https://opendepot.defdev.io/docs/architecture/) | How OpenDepot works under the hood |
| [Authentication](https://opendepot.defdev.io/docs/authentication/) | GitHub App and token-based auth |
| [Kubernetes RBAC](https://opendepot.defdev.io/docs/reference/rbac/) | Fine-grained access control for registry resources |
| [Consuming Modules](https://opendepot.defdev.io/docs/guides/modules/) | Point OpenTofu or Terraform at the registry |
| [Consuming Providers](https://opendepot.defdev.io/docs/guides/providers/) | Mirror and install providers from the registry |
| [Agents and Skills Quickstart](https://opendepot.defdev.io/docs/getting-started/agents-quickstart/) | Publish and install Skills and Agents locally |
| [Consuming Agents and Skills](https://opendepot.defdev.io/docs/guides/agents/) | Install and verify Skills and Agents with the `opendepot` CLI |
| [Publishing Agents and Skills](https://opendepot.defdev.io/docs/guides/agents-publishing/) | Publish Skills and Agents from Git as resources or through a Depot |
| [Jev Assessment](https://opendepot.defdev.io/docs/guides/agents-jev/) | Optional AI assessment of Skill and Agent Versions |
| [agents.v1 Protocol](https://opendepot.defdev.io/docs/reference/agents-v1/) | HTTP API for Skills and Agents |

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
