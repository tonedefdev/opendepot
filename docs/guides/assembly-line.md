---
tags:
  - assembly-line
  - modules
  - providers
  - opentofu
---

# Assembly Line

Assembly Line composes onboarded OpenDepot modules and providers into a validated OpenTofu root module, directly in the browser. It is **opt-in and disabled by default** in production — enable it with the Helm value `assembly.enabled=true`, then open **Assembly Line** in the UI.

Assembly Line is **OpenTofu-only** and **browser-local/export-only**: canvases persist in the browser's `localStorage` (nothing is saved server-side), and the only server-side artifact is the validated ZIP produced at export time.

## Build a Root Module

Add module, provider, and variable nodes to the canvas. Every module and provider node pins an exact version. Module inputs and provider configuration fields are rendered only when they are set on the canvas; defaults owned by the child module or provider remain in the child configuration.

Variable nodes become declarations in `variables.tf`. A variable default is emitted only when the canvas defines one. Recursive collection, tuple, object, and optional object attribute types are preserved.

References can connect a root variable or module output to a module input or provider argument. Repeated modules support `count` and `for_each`; references to repeated modules require an explicit all/index/key selector.

Building a canvas node requires the underlying module or provider version to already have an extracted Assembly artifact: a module version needs a synced, derived contract (`status.contractConfigMapRef`), and a provider version needs a synced, extracted configuration schema (`status.providerSchemaRef`). Versions without these artifacts are not offered on the canvas.

## Providers

Provider nodes must reference onboarded OpenDepot `Provider` resources with an extracted configuration schema. Configure required and optional attributes and nested blocks from that schema.

A module's provider selector lists only provider nodes whose upstream identity matches the module contract. For example, a module requirement for `hashicorp/aws` can bind to an onboarded OpenDepot Provider whose `providerNamespace/name` is `hashicorp/aws`.

Multiple configurations of one provider share one `required_providers` entry. Give additional configurations an alias, then bind each module-local provider name to the appropriate root configuration.

### OpenTofu-only eligibility

OpenDepot's provider registry generally mirrors both `registry.opentofu.org` and `registry.terraform.io` origins (see [Consuming Providers](providers.md)). Assembly Line is narrower: it only supports providers whose `spec.providerConfig.upstreamRegistry` resolves to `registry.opentofu.org`.

- The `/assembly` provider palette in the UI filters out any Provider whose upstream registry is not `registry.opentofu.org`, so Terraform-origin providers never appear as candidates.
- Export resolution enforces the same rule authoritatively on the server: a canvas that references a non-OpenTofu Provider fails with a `provider_registry_unsupported` diagnostic, regardless of what the browser submitted.

Provider schema extraction itself is unaffected by this restriction — the version controller extracts a configuration schema for every provider version using its configured `upstreamRegistry`, so both origins remain fully usable outside Assembly Line.

### Generated provider source

Generated provider source addresses use the provider's **canonical short identity**, never the OpenDepot host:

```text
<providerNamespace>/<providerName>
```

For example, an onboarded Provider with `providerNamespace: hashicorp` and `providerName: aws` renders `source = "hashicorp/aws"` in `required_providers`. OpenDepot is only the *installation location* configured in the generated OpenTofu CLI config, never the provider's identity in HCL.

Generated module source addresses continue to use the OpenDepot module registry host:

```text
<ui.baseUrl host>/<kubernetes namespace>/<Module resource name>/<provider system>
```

Exact versions are emitted without a leading `v`.

## Export and Validation

Export sends the typed canvas document to the OpenDepot server. The server does not trust contract, output, provider-schema, source-address, or authorization metadata supplied by the browser. It re-authorizes every resource, resolves every exact Version, reloads module contracts and provider configuration schemas, rejects stale, unavailable, or non-OpenTofu provider nodes, and renders `main.tf` and `variables.tf` from the authoritative documents it just loaded.

### Generated OpenTofu CLI configuration

The server writes a temporary OpenTofu CLI config (`.tofurc`) alongside the rendered files. It groups the resolved providers by their Kubernetes namespace and generates one namespace-scoped HTTPS [Provider Network Mirror](providers.md) per namespace:

```hcl
provider_installation {
  network_mirror {
    url     = "https://<validation registry>/opendepot/providers/mirror/v1/<namespace>/"
    include = ["registry.opentofu.org/<namespace>/<type>", "..."]
  }

  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}
```

`include` lists the exact canonical `registry.opentofu.org/<providerNamespace>/<providerName>` sources resolved for that namespace (deduplicated and sorted), and `direct.exclude` disables OpenTofu's fallback to the public OpenTofu Registry. This also covers provider requirements declared by downloaded child modules, so every provider in the configuration must resolve through OpenDepot's mirror.

The temporary config maps the public OpenDepot host's `modules.v1` service to the validation registry origin. A `credentials` block carries the caller's Bearer token when authentication is enabled. Provider source addresses are never rewritten: both validation and the returned `main.tf` retain canonical sources such as `hashicorp/aws`. No validation-only CLI configuration is included in the ZIP.

By default the temporary module and mirror URLs use the external `ui.baseUrl` origin. Set `assembly.validationRegistryUrl` to a trusted HTTPS origin when the public origin is not reachable from the server pod. If that endpoint uses a private CA, set `assembly.validationCACertPath` to a readable PEM CA bundle. These settings change only validation transport; they do not change generated module sources, canonical provider identities, or files included in the ZIP.

The server then runs, in an isolated temporary workspace:

```bash
tofu init -input=false -backend=false -no-color
tofu validate -no-color
```

Initialization downloads the selected modules and providers from the OpenDepot registry through that generated configuration. In anonymous mode no credentials are written. With Bearer/OIDC authentication, the request token is placed only in a mode-`0600` temporary OpenTofu CLI credentials file. It is never included in command arguments, logs, generated HCL, diagnostics, or the ZIP.

Legacy kubeconfig authentication cannot be forwarded through the registry protocol. Authenticated export therefore requires a Bearer token.

### Request, node, and time limits

Export requests are bounded by Helm-configurable limits (see [Helm Chart Reference](../helm-chart.md#assembly-line)):

- Maximum request body size and maximum total canvas nodes (variables + modules + providers combined) are enforced before any resolution work begins.
- `tofu init` and `tofu validate` each run under their own timeout; a command that exceeds its timeout is treated as a failed phase, not a hang.
- Captured `stdout`/`stderr` from each OpenTofu command is truncated to a configured byte limit before being surfaced in diagnostics or streamed progress.

### Streamed validation output

When the client requests `Accept: application/x-ndjson`, the server streams newline-delimited progress events instead of waiting for the full export to finish: a `started` event, one `output` event per completed line of `tofu init`/`tofu validate` output (tagged with its phase), then either a `complete` event carrying the base64-encoded ZIP or an `error` event with the same diagnostic shape used by the non-streaming response. This is what powers the live output shown in the export dialog.

### Temporary workspace and credential handling

Each export runs in its own temporary directory (under the Helm-configured `assembly.workDir`) containing the rendered `main.tf`/`variables.tf`, an isolated `HOME`, `TMPDIR`, and `TF_DATA_DIR`, and the generated `.tofurc`. The workspace is removed after the request completes, whether it succeeds or fails. Bearer tokens are written only into the mode-`0600` CLI credentials file inside that workspace and are redacted from any output surfaced to the client.

### ZIP contents

A successful download is named `assembly-line.zip` and contains exactly:

```text
main.tf
variables.tf
```

The archive excludes `.terraform`, lock files, state, CLI configuration, command output, and all other temporary files.

## Export Failures

Client validation blocks export while nodes are loading or have known name, input, provider, reference, or multiplicity errors. Server diagnostics identify a stable error code, node, and field path where available.

The server returns no ZIP when resource authorization, exact-version resolution, OpenTofu-only provider eligibility, rendering, `tofu init`, or `tofu validate` fails. Sanitized bounded OpenTofu output is shown in the error dialog for initialization and validation failures.

## Local Development (Tilt)

The checked-in Tilt values enable Assembly Line (`assembly.enabled: true`) and set `ui.baseUrl` to `https://opendepot.localtest.me:8443`. The `dev-tls` resource generates one mkcert certificate containing both the public development hostname and the in-cluster server Service DNS names, stores the certificate, key, and CA in the `opendepot-tls` Secret, and runs the server with TLS enabled. The host-side `provider-mirror-tls` proxy reads the same certificate from that Secret.

Because the host-side listener is not reachable through the pod's loopback address, Tilt sets `assembly.validationRegistryUrl` to `https://server.opendepot-system.svc.cluster.local:80` and `assembly.validationCACertPath` to the mounted mkcert CA. Temporary validation therefore uses the Provider Network Mirror over trusted internal HTTPS, while exported configuration continues to target the public HTTPS origin.

Production deployments still require `ui.baseUrl` to be a valid, publicly trusted HTTPS URL. Leave both validation overrides empty when that origin is reachable from the server pod; otherwise provide a trusted internal HTTPS route serving the same OpenDepot registry.
