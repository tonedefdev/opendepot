# Implementation Plan: Assembly Line Root Module Export

## Summary

Add a validated ZIP export to Assembly Line. The UI will submit the canvas's typed variables, pinned OpenDepot modules, optional OpenDepot provider configurations, input values, references, multiplicity, and provider bindings to the server. The server will re-authorize and resolve every referenced resource/version, render `main.tf` and `variables.tf`, run `tofu init -backend=false` followed by `tofu validate`, and return `assembly-line.zip` only when both commands succeed. Module `source` addresses and provider `required_providers` addresses will use the same OpenDepot registry host derivation already used by UI usage snippets (`new URL(NEXT_PUBLIC_BASE_URL).host`); exact selected versions are pinned. Failed validation returns structured diagnostics and no ZIP.

## Affected Services

- **pkg/hclschema** — extend the reduced provider schema with provider configuration fields; add the shared typed Assembly Line export model, semantic validation, and HCL renderer.
- **services/version** — emit the provider configuration block in newly extracted reduced provider schemas.
- **services/server** — expose provider configuration schemas, accept export requests, re-resolve module/provider contracts, render, initialize, validate, and stream ZIPs.
- **services/ui** — add provider nodes and bindings to the canvas, submit export requests, block locally invalid canvases, and download successful ZIPs.
- **chart/opendepot** — configure the server's registry host from `ui.baseUrl`, provide OpenTofu and writable temporary workspace support, and document export resource settings.
- **docs** — add an Assembly Line guide covering provider nodes, generated files, validation, authentication, and export failures.

## API / CRD Changes

No fields are added to `api/v1alpha1/types.go`; existing `Version.status.contractConfigMapRef` and `Version.status.providerSchemaRef` contain the references needed by the server.

Bump the reduced provider schema wire format from `assembly.provider.v1`. Add a top-level `provider` configuration `Block` alongside `resources` and `dataSources`. Preserve required/optional/computed flags, cty types, and nested blocks so the provider node can render configuration inputs recursively. The server should reject unsupported schema versions with a structured unavailable response rather than guessing.

Add server HTTP contracts:

- `GET /opendepot/ui/v1/resources/{namespace}/provider/{name}/provider-schema?version=<semver>` returns only the selected provider's identity/version and provider configuration block, not its full resource/data-source schema.
- `POST /opendepot/ui/v1/assembly/export` accepts a size-bounded typed canvas document containing root variables, module nodes, provider nodes, module input values, references, multiplicity, and module-to-provider bindings.
- A successful export responds `200 application/zip` with `Content-Disposition: attachment; filename="assembly-line.zip"`.
- Invalid canvas/resource/version/schema/HCL requests respond `400` or `422` JSON with stable error codes and node/field paths. Authorization failures preserve `401`/`403`; hidden resources remain `404`. OpenTofu timeout or internal execution failures return `500`/`504`. No failure response includes a ZIP.

## Implementation Steps

1. **`pkg/hclschema/providerschema/reduce.go`, new provider-schema tests** — retain `provider_schemas[*].provider.block` in `ReducedSchema`, bump `SchemaVersion`, and cover attributes, nested blocks, and v1 rejection/availability behavior. No new dependency.

2. **`services/version/internal/controller/providerschema.go` and version tests** — write reduced schemas through the existing compressed object-storage path and digest/reference flow. Existing provider Versions must be force-synced/re-extracted before their provider configuration can appear in Assembly Line; module contract behavior is unchanged.

3. **`services/server/ui_provider_schema.go`, `main.go`, storage helpers, and server tests** — add the authenticated provider-schema endpoint. Follow `handleBrowseContract` visibility/version selection, verify the target is a visible synced provider Version with a successful `ProviderSchemaRef`, initialize its configured storage backend, stream/decompress with the existing 128 MiB ceiling, verify the stored digest, decode v2, and return only the provider configuration projection.

4. **`pkg/hclschema/assemblyexport/`** — add the shared request types and deterministic renderer. Use `hclwrite` and HCL expression parsing rather than string concatenation. Render:
   - `variables.tf`: only variables defined on the canvas, including declared recursive type, optional object attributes, descriptions/defaults represented by current canvas state, and no `.tfvars` file.
   - `main.tf`: one module block per module node with OpenDepot source `<registryHost>/<kubernetesNamespace>/<moduleName>/<providerSystem>`, exact version without leading `v`, only user-provided input assignments, `count`/`for_each`, references/selectors, optional `providers` mappings, root `terraform.required_providers`, and provider blocks for provider nodes.
   - Provider source addresses use `<registryHost>/<kubernetesNamespace>/<providerResourceName>` and exact selected versions. Match a module contract's upstream required-provider identity (for example `hashicorp/aws`) against the selected OpenDepot Provider's `providerNamespace/name`; render the OpenDepot address, not the upstream registry address.
   - Multiple configurations of one provider share one `required_providers` entry and use provider aliases; module bindings render child-local-name to root provider configuration mappings.

5. **Assembly export semantic validation in `pkg/hclschema/assemblyexport/`** — reject duplicate/invalid module, variable, provider local names and aliases; missing required module inputs; malformed literals/defaults/type expressions; dangling or incompatible references; invalid repeated-module selectors; invalid `count`/`for_each`; duplicate provider requirements with conflicting versions; provider bindings whose upstream identity does not match the module contract; loading/error/unsupported nodes; and resources or exact versions that cannot be re-resolved. Re-fetch contracts and provider schemas server-side and build the render model from those authoritative documents rather than trusting browser-supplied variable/output/schema metadata.

6. **`services/server/ui_assembly_export.go`, `main.go`, focused unit/integration tests** — add the POST handler with request/body/node limits. Derive the registry host from a server flag populated from the same external `ui.baseUrl` used by usage snippets; do not accept an arbitrary registry host from the request. Reuse browse authorization for every module/provider before rendering. Write files into a per-request `os.MkdirTemp` directory, run `tofu init -input=false -backend=false -no-color`, then `tofu validate -no-color`; use separate bounded contexts, scrub inherited environment/cloud credentials, set `HOME`, `TMPDIR`, `TF_DATA_DIR`, `TF_CLI_CONFIG_FILE`, `TF_IN_AUTOMATION=1`, `TF_INPUT=0`, and `CHECKPOINT_DISABLE=1` inside that directory, cap captured output, and always remove the directory.

7. **Validation authentication in `services/server/ui_assembly_export.go`** — for anonymous mode, initialize without credentials. For Bearer/OIDC requests, write the request token only to the mode-0600 temporary CLI config's `credentials` block for the configured OpenDepot registry host so `tofu init` pulls the same authorized OpenDepot modules/providers; never include the token in command arguments, logs, responses, generated HCL, or ZIP. Preserve the existing legacy kubeconfig limitation: if the request is not anonymous and does not provide a Bearer token usable by the registry protocol, fail export with a clear authentication diagnostic before spawning OpenTofu.

8. **ZIP generation in the export handler** — after successful validation, package only `main.tf` and `variables.tf` using `archive/zip`; omit `.terraform`, lock files, CLI config, diagnostics, state, and all temporary files. Stream from memory or a bounded temporary artifact and set no-store/cache-protection headers.

9. **`services/ui/src/lib/api.ts`, new `app/api/provider-schema/.../route.ts`, new `app/api/assembly/export/route.ts`** — add typed provider-schema and export clients. Both Next route handlers forward the encrypted-session bearer token to the server. The export route returns the ZIP response as binary and passes structured JSON failures through unchanged.

10. **`services/ui/src/components/assembly/`** — add `ProviderNode` and provider configuration editor components following existing module/variable patterns. Load both modules and providers on the Assembly page; allow OpenDepot providers to be dragged onto the canvas; pin namespace/resource name/upstream provider identity/version; fetch the selected version's provider configuration schema; render required/optional fields; store only user-set values; support local names and aliases; and expose compatible provider bindings for each module contract `requiredProviders` local name.

11. **`AssemblyCanvas.tsx`, assembly types, and local-storage migration** — extend the persisted graph with provider nodes/bindings and bump the storage schema key/version with a migration for current module/variable snapshots. Derive provider edges from bindings. Add an Export icon action disabled while contracts/schemas are loading or client validation has errors. On click, submit the raw typed graph; show server field diagnostics against affected nodes, show sanitized init/validate output in an error dialog, and trigger the ZIP download only for a successful response.

12. **Shared registry-source behavior** — keep `services/ui/src/lib/registrySource.ts` as the UI display/source authority and pass the same normalized host derived from `NEXT_PUBLIC_BASE_URL` into the server deployment. Add parity tests proving usage snippets and server export produce identical OpenDepot module/provider addresses, including ports and leading-`v` normalization.

13. **Documentation** — add `docs/guides/assembly-line.md`, link it from the guides index/navigation, and update chart documentation. State that all module nodes must be onboarded OpenDepot modules; provider nodes are onboarded OpenDepot providers; required root variables remain declarations unless given canvas defaults; only user-provided module/provider arguments are rendered; validation performs registry downloads; and no artifact is produced on failure.

## E2E Test Changes

- **`services/server/test/e2e/e2e_test.go`** — enable Assembly Line, onboard two compatible modules and a provider with a v2 schema, submit a canvas containing literals, a root variable, module-output references, multiplicity, provider configuration, and a module provider binding. Assert the response ZIP contains exactly `main.tf` and `variables.tf`, all sources point at the OpenDepot test host with exact versions, only canvas-provided inputs are emitted, and the generated root independently passes `tofu init -backend=false` and `tofu validate`.
- Add authenticated export coverage proving the forwarded bearer token permits OpenDepot module/provider downloads without leaking into ZIP or diagnostics.
- Add failure scenarios for missing required input, stale/missing exact module version, unavailable/v1 provider schema, mismatched provider binding, unauthorized resource, malformed expression, and a rendered configuration that fails `tofu validate`; each must return no ZIP.
- **`services/version/test/e2e/e2e_test.go`** — extend provider schema extraction assertions to decode the stored v2 schema and verify the provider configuration block, its required/optional fields, digest, and compression/storage path.
- **`services/ui/test/e2e/assembly.spec.ts`** — cover adding/configuring provider nodes, binding one to a module, persistence/reload, export loading/error states, blocked client-invalid export, and successful browser ZIP download. Use targeted Vitest component tests for local-storage migration and request serialization.

## Helm Chart

- Derive the server `--registry-host` argument from `ui.baseUrl` using the URL host (including a non-default port), matching the existing UI usage-snippet derivation. Fail Helm rendering when `assembly.enabled=true` with UI/export enabled but `ui.baseUrl` is empty or invalid; do not add a second independently configured registry hostname.
- Add export runtime values under `assembly` for init timeout, validate timeout, maximum request size/node count, maximum command output, and temporary workspace size limit, with conservative defaults.
- Copy the same pinned, checksum-verified OpenTofu binary used by the version-controller into the server image and add a configurable `--tofu-bin-path` argument.
- Mount an `emptyDir` at a dedicated server validation path with `sizeLimit`; keep `readOnlyRootFilesystem: true` and point temporary workspaces there. Include ephemeral-storage requests/limits guidance.
- No new Kubernetes API RBAC rules are required: the server already reads Module, Provider, Version, ConfigMap, namespace, and GroupBinding data. Object-storage credentials/mounts already available to the server are reused for provider-schema reads.
