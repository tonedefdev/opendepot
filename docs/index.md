---
template: home.html
hide:
    - toc
    - title
tags:
    - home
---

## What OpenDepot serves

OpenDepot is an open source artifact registry that runs in your Kubernetes cluster. It serves:

- **Modules and providers** for OpenTofu and Terraform, through the registry protocol.
- **Agent Skills and Agents** published from Git repositories and installed with the `opendepot` CLI.

MCP servers and OCI artifacts are planned. The server and UI are read-only, and Kubernetes RBAC authorizes every create, update, and delete.

## Where to start

<div class="grid cards" markdown>

- :material-rocket-launch: &nbsp;__Install__

    ---

    Deploy with the Helm chart, or run a local registry with `kind` using the [quickstart](getting-started/quickstart.md). See [installation](getting-started/installation.md).

- :material-package-variant: &nbsp;__Publish modules and providers__

    ---

    Create a Depot that polls your module and provider sources, or manage them declaratively with [GitOps](guides/gitops.md). See the [Depot workflow](guides/depot.md).

- :material-package-down: &nbsp;__Consume modules and providers__

    ---

    Point OpenTofu or Terraform at the registry. See [consuming modules](guides/modules.md) and [consuming providers](guides/providers.md), or compose them into a validated root module with the [Assembly Line](guides/assembly-line.md).

- :material-robot-outline: &nbsp;__Publish Skills and Agents__

    ---

    Publish Skills and Agents from Git tags or branches, either as standalone resources or through a Depot. See [publishing Agents and Skills](guides/agents-publishing.md).

- :material-console: &nbsp;__Consume Skills and Agents__

    ---

    Install versioned Skills and Agents with the `opendepot` CLI, which verifies signed checksums before use. See [consuming Agents and Skills](guides/agents.md).

- :material-shield-check-outline: &nbsp;__Browse and secure__

    ---

    Browse artifacts in the [OpenDepot Workshop](guides/registry-explorer/index.md), manage [Security Policies](guides/registry-explorer/security-policies.md), and review [vulnerability scanning](configuration/scanning.md).

</div>

For how the system fits together, see [Architecture](architecture.md). For deployment options, see [Configuration](configuration/index.md). For API and CLI details, see [Reference](reference/index.md).
