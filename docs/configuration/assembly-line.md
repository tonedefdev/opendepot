---
tags:
  - configuration
  - helm
  - assembly-line
  - opentofu
---

# Assembly Line Configuration

Assembly Line lets users compose onboarded modules and providers into a
validated OpenTofu root module. It is disabled by default and requires the UI
to be enabled.

## Enable Assembly Line

Set the following Helm values:

```yaml
ui:
  enabled: true

assembly:
  enabled: true
```

See the [Helm Chart Reference](../helm-chart.md#assembly-line) for the complete
`assembly.*` value table, including request, node, timeout, output, and
workspace limits.

`ui.baseUrl` must be a valid external HTTP(S) URL. OpenDepot uses its host for
generated module source addresses and the Provider Network Mirror URL. The
server must also be able to reach that origin during export, unless you set a
separate `assembly.validationRegistryUrl`.

## Validation Connectivity

Assembly Line validates exports by running OpenTofu initialization in a
temporary server-side workspace. If the public UI origin is not reachable from
the server pod, configure an internal HTTPS origin and its CA bundle:

```yaml
assembly:
  validationRegistryUrl: https://registry.internal.example.com
  validationCACertPath: /etc/opendepot/ca/registry.pem
```

These values affect only server-side initialization transport. They do not
change generated module sources, provider identities, or the files returned to
the user.

The validation workspace is isolated per export. OpenTofu runs with bounded
output and a configured timeout. The workspace is removed after the request,
including failed requests. The chart mounts only `assembly.workDir` as a
writable, size-limited volume; consider setting ephemeral-storage requests and
limits for production workloads.

## Provider and Module Requirements

Before a resource appears in the Assembly Line palette:

- A module version must have a synced, derived contract.
- A provider version must have a synced, extracted configuration schema.
- The provider must use the `registry.opentofu.org` upstream registry.

Provider schema extraction runs OpenTofu in a temporary scrubbed workspace.
The controller validates provider-controlled source, version, and workspace
components before starting extraction.

Assembly Line does not support Terraform-origin providers, even though the
general provider registry can mirror both OpenTofu and Terraform origins.

## Authentication

Anonymous export does not write credentials. Authenticated export forwards the
caller’s Bearer token through a temporary mode-`0600` OpenTofu credentials file.
The token is not included in command arguments, logs, generated HCL,
diagnostics, or the ZIP archive.

Legacy kubeconfig authentication cannot be forwarded through the registry
protocol. Authenticated exports therefore require a Bearer token.

## Export Behavior

The server re-authorizes every resource and reloads the selected versions,
module contracts, and provider schemas from Kubernetes. It does not trust
metadata supplied by the browser. The server rejects stale, unavailable,
unauthorized, or unsupported provider resources before returning a ZIP.

Successful exports contain only:

```text
main.tf
variables.tf
```

The archive excludes `.terraform`, lock files, state, CLI configuration,
temporary files, and command output.

For the browser workflow, see [Using Assembly Line](../guides/assembly-line.md).