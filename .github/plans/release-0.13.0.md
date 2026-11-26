# Implementation Plan: OpenDepot 0.13.0

Status: approved scope, ready for implementation. Branch `plan/0.13.0`, based on `feat/agent-registry-ui`.
This plan assumes that agents do the implementation work. Steps are small, and each step lists its files, so steps can run in parallel lanes without sharing files.

## Summary

0.13.0 turns OpenDepot from a registry for OpenTofu and agent content into a general registry with an agentic workflow builder. It adds:

- **OCI artifacts and Helm.** One `OCIArtifact` CRD with `artifactType: Helm | Image | Generic`. Artifacts are mirrored from upstream OCI registries, or built from GitHub release assets for Helm and Generic. They are served through the pull side of the OCI Distribution API (`/v2/...`) and as a classic Helm `index.yaml` repository.
- **MCP servers in the registry.** An `MCPServer` CRD with four source types: `Image` (it uses an `OCIArtifact`), `Npm`, `PyPI`, and `Source`. MCP servers are published through `agents.v1` as a third kind. `opendepot apply` writes pinned MCP client configuration. In-cluster runtime and the proxy are in 0.14.0 (see the appendix).
- **One `iac` controller.** The Module and Provider reconcilers move into one `services/iac` deployment. It serves both Terraform and OpenTofu, and its name doesn't clash with Flux's `tofu-controller`. The CRDs do not change. Chart values move to `iac.*`, and the old keys still work for one release.
- **Jev suggest.** `POST /opendepot/agents/v1/suggest` and `opendepot suggest`, as described in [jev-skill-suggest-design.md](jev-skill-suggest-design.md). Suggest also ranks MCP servers.
- **Agent assemblies in Assembly Line.** A typed graph of agents, skills, and MCP servers, with `uses`, `calls`, `delegates` (subagent, handoff, or parallel), and bounded `retry` links. Jev advises on each delegate link while you design. Export produces platform-specific agent files, a pinned `opendepot.hcl`, and `assembly.json`. A runtime hook, `opendepot assembly route`, enforces the graph and can ask Jev for a decision, with the saved decision as the fallback.
- **Schematics.** A `Schematic` CRD that holds a shareable Tofu or Agent assembly. The UI adds a catalog, import and export as a portable bundle, duplicate, and save from the canvas. Access uses GroupBinding. Built-in Schematics ship with the chart.
- **Assembly Line guided tour.** An in-app tour with Tofu and Agent tracks. It uses built-in tutorial Schematics with placeholder nodes, so it works on a fresh install with nothing onboarded.

## Decisions recorded

| # | Decision | Source |
|---|---|---|
| D1 | OCI sources are upstream mirrors and GitHub release assets. There is no push API. Push endpoints return the OCI `UNSUPPORTED` error. | User Q2 |
| D2 | One `OCIArtifact` CRD with `artifactType`. Images can only be mirrored. | User Q3 |
| D3 | Helm is served both through OCI (`helm pull oci://`) and as a classic `index.yaml` repository. | User Q4 |
| D4 | MCP source types are `Image`, `Npm`, `PyPI`, and `Source`. `Source` is listed in the registry but can't run until in-cluster builds exist. | User Q5 |
| D5 | MCP runtime (inheriting the Deployment spec), secrets through `secretRefs`, ESO, and the proxy are in 0.14.0. 0.13.0 MCP support is registry-only, and `apply` writes client configuration. | User Q1 and follow-up Q2 |
| D6 | The Module and Provider CRDs don't change. Only the deployments merge, into the `iac` controller. `iac.*` values replace `module.*` and `provider.*`, which are still read for one release. The Version controller stays separate. | User Q8, P4 follow-up |
| D7 | Agent assembly export: platform `.agent.md` files, `opendepot.hcl`, and `assembly.json`. | User Q9 |
| D8 | The graph is checked: no cycles across `delegates` links, shared nodes are allowed, and `retry` links are the only back edges, each with `maxIterations`. | User Q10, follow-up Q3 |
| D9 | Jev advises at design time and is part of the exported runtime (the `assembly route` hook). Deterministic rules always win, and the saved decision is the fallback. | User Q11 |
| D10 | `Schematic` CRD. Access through GroupBinding. Saving is on by default: any authenticated caller matched by a GroupBinding can save in that binding's namespace. Operators can turn it off per binding or for the whole cluster. | User Q12, Q13, P6 follow-up |
| D11 | In-app guided tour, driven by built-in Schematics with placeholder nodes. | User Q14, follow-up Q1 |
| D12 | Jev suggest ships as designed. Step 1 explains the 0/5 canary result first. | User Q15 |

Decisions the planner made, which the user later confirmed, are under [Planner decisions](#planner-decisions).

## Affected Services

Controllers after 0.13.0: `depot`, `iac` (new, replaces `module` and `provider`), `agent`, `oci` (new), and `version`. There are still five, and each one now covers a family of artifacts.

| Service | Changes |
|---|---|
| `api/v1alpha1` | New `OCIArtifact`, `MCPServer`, and `Schematic` kinds. New fields on Depot, Version, GroupBinding, SecurityGroupBinding, and ScanPolicy. |
| `services/iac` (new) | Hosts `ModuleReconciler` and `ProviderReconciler`, moved from `services/module` and `services/provider`, which are deleted. |
| `services/oci` (new) | `OCIArtifactReconciler`: creates one Version per selected tag and garbage-collects removed Versions. |
| `services/depot` | Discovers upstream OCI tags and GitHub release assets for `ociArtifactConfigs`, and npm, PyPI, and GitHub tags for `mcpServerConfigs`. Adds `SchematicReconciler` for status, reference resolution, and staleness. |
| `services/agent` | Adds `MCPServerReconciler`. |
| `services/version` | OCI sync (manifests and blobs, Helm chart packaging, optional cosign check of upstream signatures, Trivy image and config scans). MCP sync (npm and PyPI dependency vendoring, image reference resolution, source archives, `server.json` metadata, SHA256SUMS signing, Trivy, and Jev). |
| `services/server` | OCI Distribution read endpoints and their auth challenge. Helm `index.yaml` repository. `mcpserver` kind in `agents.v1`. Suggest and handoff endpoints. Agent assembly advise and export. Schematic API. Browse and metrics for the new kinds. |
| `services/ui` | Registry pages for the new kinds. Agent assembly canvas. Schematics catalog. Guided tour. |
| `cli` | `mcp_server` blocks and MCP config targets. `suggest`. `assembly route`. |
| `pkg/oci` (new) | OCI storage layout, upstream client, registry allow-list, Helm metadata, and `index.yaml` rendering. |
| `pkg/mcp` (new) | `server.json` parsing and MCP client config rendering, shared by the Version controller and the CLI. |
| `pkg/agentassembly` (new) | Agent graph model, validation, platform renderers, and the runtime route policy. |
| `pkg/schematic` (new) | Bundle format, canonical digest, document validation, reference extraction, and import sanitization. |
| `pkg/jev` | Question sets for suggest and handoff. |
| `chart/opendepot` | `iac` and `oci` deployments, new CRDs, RBAC, values, and built-in Schematics. Chart version `0.13.0`. |
| `docs` | New guides and reference pages, architecture, upgrading from 0.12 to 0.13, and the tutorial. |

## API / CRD Changes

All new kinds are namespaced and use `+kubebuilder:object:root=true` and `+kubebuilder:subresource:status`. Each new kind is registered in its own file's `init()`, so `types.go` keeps only the shared edits.

### Shared edits in `api/v1alpha1/types.go` (Step 3)

Constants: `OpenDepotOCIArtifact = "OCIArtifact"`, `OpenDepotMCPServer = "MCPServer"`, and `OpenDepotSchematic = "Schematic"`.

`DepotSpec`:

| Field | Type | JSON | Markers |
|---|---|---|---|
| `OCIArtifactConfigs` | `[]OCIArtifactConfig` | `ociArtifactConfigs,omitempty` | none |
| `MCPServerConfigs` | `[]MCPServerSourceConfig` | `mcpServerConfigs,omitempty` | none |

`DepotStatus`: `OCIArtifacts []string` (`ociArtifacts,omitempty`) and `MCPServers []string` (`mcpServers,omitempty`).

`VersionSpec`:

| Field | Type | JSON | Notes |
|---|---|---|---|
| `OCIArtifactConfigRef` | `*OCIArtifactConfig` | `ociArtifactConfigRef,omitempty` | Set by the `oci` controller |
| `MCPServerSourceRef` | `*MCPServerSourceConfig` | `mcpServerSourceRef,omitempty` | Set by the `agent` controller |

Update the `Type` doc comment to list `OCIArtifact` and `MCPServer`. Do not add an enum marker to `Type`.

`VersionStatus`:

| Field | Type | JSON | Markers |
|---|---|---|---|
| `OCIManifest` | `*OCIManifestStatus` | `ociManifest,omitempty` | `+optional` |
| `HelmChart` | `*HelmChartMetadata` | `helmChart,omitempty` | `+optional` |
| `OCIScan` | `*SourceScan` | `ociScan,omitempty` | `+optional`. Reuses the existing type |
| `MCPMetadata` | `*MCPMetadata` | `mcpMetadata,omitempty` | `+optional` |

MCP Versions reuse `ShaSums`, `ShaSumsSignature`, `SigningKeyFingerprint`, `JevAssessment`, `JevExemptionGovernance`, and `JevUnblock`.

New status types in `types.go`:

- `OCIManifestStatus`:
  - `Digest string` (`digest`), with `+kubebuilder:validation:Pattern=^sha256:[0-9a-f]{64}$`.
  - `MediaType string` (`mediaType`), with `MaxLength=255`.
  - `ArtifactType string` (`artifactType,omitempty`), with `MaxLength=255`.
  - `UpstreamDigest string` (`upstreamDigest,omitempty`), with the same pattern.
  - `TagDrift bool` (`tagDrift,omitempty`). True when the upstream tag now points to a different digest. The served digest never changes.
  - `Platforms []string` (`platforms,omitempty`), with `MaxItems=16`.
  - `TotalSizeBytes int64` (`totalSizeBytes`), with `Minimum=0`.
  - `SignatureVerified *bool` (`signatureVerified,omitempty`).
- `HelmChartMetadata`:
  - `Name`, `Version`, and `AppVersion` strings, each with `MaxLength=255`.
  - `Description string`, with `MaxLength=1024`.
  - `KubeVersion string`, with `MaxLength=255`.
  - `Deprecated bool`.
  - `Keywords []string`, with `MaxItems=32` and `items:MaxLength=64`.
  - `Dependencies []HelmChartDependency`, with `MaxItems=64`. Each `HelmChartDependency` has `Name`, `Version`, and `Repository` strings, each with `MaxLength=512`.
- `MCPMetadata`:
  - `ServerName string`, with `MaxLength=255`.
  - `Description string`, with `MaxLength=1024`.
  - `Transport string`, with `+kubebuilder:validation:Enum=stdio;streamable-http`.
  - `Runnable bool`. False for `Source`.
  - `Platforms []string`, with `MaxItems=8`. Holds the PyPI wheel platforms that were vendored.
  - `PackageDigest string`, with the sha256 pattern.

`ScanPolicyTargetRef.Kind`: change the enum to `+kubebuilder:validation:Enum=Module;Provider;Skill;Agent;OCIArtifact;MCPServer`, and update the doc comment.

`ScanExemption.ScanTypes`: change the enum to `+kubebuilder:validation:items:Enum=binary;source;module;agent;oci;mcp`.

`GroupBindingSpec`, with every new field `+optional`:

| Field | Type | JSON | Meaning |
|---|---|---|---|
| `OCIArtifactResources` | `[]string` | `ociArtifactResources,omitempty` | Glob patterns. Empty means none. |
| `MCPServerResources` | `[]string` | `mcpServerResources,omitempty` | Glob patterns. Empty means none. |
| `SchematicResources` | `[]string` | `schematicResources,omitempty` | Glob patterns for reading Schematics. Empty means none. |
| `SchematicManagement` | `*bool` | `schematicManagement,omitempty` | Allows create, update, delete, import, and duplicate. **Unset means true**, so saving works by default. Set it to `false` to make the binding read-only for Schematics |
| `SchematicNamespaces` | `[]string` | `schematicNamespaces,omitempty` | Extra namespaces where writes are allowed. **Writes are always allowed in the GroupBinding's own namespace**, unless `schematicManagement` is `false`. `*` means every namespace. Empty means only the binding's own namespace |

The rule for Schematic writes (the "Schematic write rule"), used by Steps 26 and 41:
- The caller is authenticated and matches a GroupBinding where `schematicManagement` isn't `false`.
- The target namespace is that binding's namespace or is listed in its `schematicNamespaces`.
- The server flag `--schematic-management-enabled` is true. It defaults to true and is a cluster-wide off switch.
- Anonymous callers never write, even when anonymous browsing is on.
- Built-in Schematics are always read-only.
| `JevSecretRef` | `*corev1.SecretKeySelector` | `jevSecretRef,omitempty` | The Jev token for suggest, advise, and handoff calls from this binding's callers. The Secret must be in the server namespace. The key defaults to `jevToken`. |

`SecurityGroupBindingSpec`: add `OCIArtifactResources`, `MCPServerResources`, and `SchematicResources` (`[]string`, `omitempty`).

### New file `api/v1alpha1/ociartifact_types.go`

`OCIArtifact` has print columns `Type` (`.spec.ociArtifactConfig.artifactType`), `LatestVersion`, and `Synced`.

- `OCIArtifactSpec`: `ForceSync bool` (`forceSync,omitempty`), `OCIArtifactConfig OCIArtifactConfig` (`ociArtifactConfig`), and `Versions []OCIArtifactVersion` (`versions`).
- `OCIArtifactStatus`: `LatestVersion *string`, `Synced bool`, `SyncStatus string`, and `VersionRefs map[string]*OCIArtifactVersion` (`versionRefs,omitempty`).
- `OCIArtifactVersion`: `Name string`, `Version string` (the tag), `Digest string` (sha256 pattern, `omitempty`), and `Synced bool`.

`OCIArtifactConfig` has these validation rules:

- `+kubebuilder:validation:XValidation:rule="has(self.mirror) != has(self.github)",message="exactly one of mirror or github is required"`
- `+kubebuilder:validation:XValidation:rule="!has(self.github) || self.artifactType != 'Image'",message="images can only be mirrored"`
- `+kubebuilder:validation:XValidation:rule="!has(self.platforms) || self.artifactType == 'Image'",message="platforms is only valid for Image"`
- `+kubebuilder:validation:XValidation:rule="!has(self.tagPattern) || !has(self.versionConstraints) || size(self.versionConstraints) == 0",message="tagPattern and versionConstraints are mutually exclusive"`

| Field | Type | JSON | Markers |
|---|---|---|---|
| `Name` | `*string` | `name,omitempty` | `Pattern=^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`, `MaxLength=255`. The repository path under the namespace |
| `ArtifactType` | `string` | `artifactType` | `+kubebuilder:validation:Required`, `Enum=Helm;Image;Generic` |
| `Mirror` | `*OCIMirrorSource` | `mirror,omitempty` | none |
| `Github` | `*OCIGithubSource` | `github,omitempty` | none |
| `Platforms` | `[]string` | `platforms,omitempty` | `MaxItems=16`, `items:Pattern=^[a-z0-9]+/[a-z0-9]+(/[a-z0-9.]+)?$` |
| `TagPattern` | `*string` | `tagPattern,omitempty` | `MaxLength=256`. An RE2 regex for tags that aren't semver |
| `VersionConstraints` | `string` | `versionConstraints,omitempty` | Only the Depot controller reads it |
| `VersionHistoryLimit` | `*int` | `versionHistoryLimit,omitempty` | `Minimum=0` |
| `Immutable` | `*bool` | `immutable,omitempty` | Defaults to true when unset. Tag drift is reported and never re-synced |
| `GithubClientConfig` | `*GithubClientConfig` | `githubClientConfig,omitempty` | none |
| `StorageConfig` | `*StorageConfig` | `storageConfig,omitempty` | none |

- `OCIMirrorSource`:
  - `Registry string` (`registry`): `Required`, `MaxLength=253`, `Pattern=^[a-z0-9.-]+(:[0-9]{1,5})?$`. It must be on the chart allow-list, and the server and Version controller both check that.
  - `Repository string` (`repository`): `Required`, `MaxLength=255`.
  - `CredentialsSecretRef *corev1.LocalObjectReference` (`credentialsSecretRef,omitempty`): a `kubernetes.io/dockerconfigjson` Secret.
  - `Verify *OCIVerifyConfig` (`verify,omitempty`).
- `OCIVerifyConfig`: `CosignPublicKeySecretRef *corev1.SecretKeySelector` and `Required bool`. When `Required` is true, a missing or invalid upstream signature blocks the Version.
- `OCIGithubSource`:
  - `RepoOwner string`, `RepoUrl *string`, and `TagPrefix *string`.
  - `AssetPattern string` (`assetPattern`): `Required`, `MaxLength=256`. A glob, for example `*.tgz`.
  - `MediaType *string` and `ArtifactMediaType *string`, each with `MaxLength=255`. Used for Generic only.

### New file `api/v1alpha1/mcpserver_types.go`

`MCPServer` has print columns `Source` (`.spec.mcpServerSourceConfig.source.type`), `LatestVersion`, and `Synced`.

- `MCPServerSpec`: `ForceSync bool`, `MCPServerSourceConfig MCPServerSourceConfig` (`mcpServerSourceConfig`), and `Versions []MCPServerVersion` (`versions`).
- `MCPServerStatus`: `LatestVersion *string`, `Synced bool`, `SyncStatus string`, and `VersionRefs map[string]*MCPServerVersion`.
- `MCPServerVersion`: `Name string`, `Version string`, `FileName *string`, and `Synced bool`.

`MCPServerSourceConfig`:

| Field | Type | JSON | Markers |
|---|---|---|---|
| `Name` | `*string` | `name,omitempty` | none |
| `Source` | `MCPServerSource` | `source` | `Required` |
| `Transport` | `string` | `transport,omitempty` | `Enum=stdio;streamable-http`, `+kubebuilder:default=stdio` |
| `Entrypoint` | `*MCPEntrypoint` | `entrypoint,omitempty` | Overrides the package's default binary or console script |
| `VersionConstraints` | `string` | `versionConstraints,omitempty` | none |
| `VersionHistoryLimit` | `*int` | `versionHistoryLimit,omitempty` | `Minimum=0` |
| `Immutable` | `*bool` | `immutable,omitempty` | none |
| `GithubClientConfig` | `*GithubClientConfig` | `githubClientConfig,omitempty` | none |
| `StorageConfig` | `*StorageConfig` | `storageConfig,omitempty` | none |
| `JevSecretRef` | `*corev1.SecretKeySelector` | `jevSecretRef,omitempty` | none |
| `JevPolicy` | `*JevPolicy` | `jevPolicy,omitempty` | none |

`MCPServerSource` has `Type string` (`type`, `Required`, `Enum=Image;Npm;PyPI;Source`) and one block for each type. It has this rule: `+kubebuilder:validation:XValidation:rule="(self.type == 'Image') == has(self.image) && (self.type == 'Npm') == has(self.npm) && (self.type == 'PyPI') == has(self.pypi) && (self.type == 'Source') == has(self.source)",message="set exactly the block that matches type"`.

- `Image *MCPImageSource`: `OCIArtifactRef corev1.LocalObjectReference` (`ociArtifactRef`, `Required`). It must name an `OCIArtifact` with `artifactType: Image`. The controller checks this and sets a status error otherwise.
- `Npm *MCPNpmSource`: `Package string` (`Required`, `MaxLength=214`, `Pattern=^(@[a-z0-9-~][a-z0-9-._~]*/)?[a-z0-9-~][a-z0-9-._~]*$`) and `Registry *string`. The registry defaults to `https://registry.npmjs.org` and must be on the allow-list.
- `PyPI *MCPPyPISource`: `Package string` (`Required`, `MaxLength=255`), `Index *string` (defaults to `https://pypi.org/simple`, allow-listed), `Platforms []string` (`MaxItems=8`; wheel platform tags such as `manylinux_2_28_x86_64`; defaults set by the chart), and `PythonVersion *string` (`Pattern=^3\.[0-9]+$`).
- `Source *MCPRepoSource`: `RepoOwner string`, `RepoUrl *string`, `Path string`, and `TagPrefix *string`.
- `MCPEntrypoint`: `Command string` (`MaxLength=255`) and `Args []string` (`MaxItems=32`, `items:MaxLength=1024`).

The 0.14.0 fields (`deployment`, `secretRefs`, `externalSecrets`, `proxy`) are additive and are not in this release.

### New file `api/v1alpha1/schematic_types.go`

`Schematic` has print columns `Kind` (`.spec.assemblyKind`), `Valid` (`.status.valid`), and `Age`.

`SchematicSpec`:

| Field | Type | JSON | Markers |
|---|---|---|---|
| `AssemblyKind` | `string` | `assemblyKind` | `Required`, `Enum=Tofu;Agent`, `XValidation:rule="self == oldSelf",message="assemblyKind is immutable"` |
| `SchemaVersion` | `string` | `schemaVersion` | `Required`, `Enum=opendepot.schematic.v1` |
| `DisplayName` | `string` | `displayName` | `MinLength=1`, `MaxLength=128` |
| `Description` | `string` | `description,omitempty` | `MaxLength=2048` |
| `Tags` | `[]string` | `tags,omitempty` | `MaxItems=16`, `items:MaxLength=32`, `items:Pattern=^[a-z0-9][a-z0-9-]*$` |
| `Document` | `apiextensionsv1.JSON` | `document` | `+kubebuilder:pruning:PreserveUnknownFields`, `+kubebuilder:validation:Schemaless`, `+kubebuilder:validation:Type=object`. The server and the reconciler limit it to 256 KiB |
| `Tutorial` | `bool` | `tutorial,omitempty` | The document can contain placeholder nodes and can't be exported to OpenTofu or agent files |
| `Origin` | `*SchematicOrigin` | `origin,omitempty` | none |

- `SchematicOrigin`:
  - `Source string`, with `Enum=Builtin;Import;Duplicate;Canvas`.
  - `ImportedFrom string`, with `MaxLength=512`.
  - `ImportedDigest string`, with the sha256 pattern.
  - `ImportedAt *metav1.Time`.
- Built-in Schematics carry the label `opendepot.defdev.io/builtin: "true"`. The server rejects writes to them, and users duplicate them instead.
- `SchematicStatus`:
  - `ObservedGeneration int64`.
  - `Digest string`: the sha256 of the canonical document.
  - `Valid bool`.
  - `Diagnostics []SchematicDiagnostic`, with `MaxItems=50`.
  - `References []SchematicReference`, with `MaxItems=256`.
  - `Conditions []metav1.Condition`, with types `Ready` and `ReferencesResolved`.
- `SchematicDiagnostic`:
  - `Code string`, with `MaxLength=64`.
  - `Path string`, with `MaxLength=256`.
  - `Message string`, with `MaxLength=512`.
  - `Severity string`, with `Enum=Error;Warning`.
- `SchematicReference`:
  - `Kind string`, with `Enum=Module;Provider;Skill;Agent;MCPServer`.
  - `Namespace string` and `Name string`.
  - `Version string`.
  - `State string`, with `Enum=Available;Missing;Yanked;Blocked;Hidden;Placeholder`.

### Agent assembly document (`pkg/agentassembly`, held in `Schematic.spec.document` when `assemblyKind: Agent`)

These are Go types in `pkg/agentassembly`, not CRD types. The Tofu document reuses the existing types in `pkg/hclschema/assemblyexport`.

- `nodes[]`: `id` (`^[a-z][a-z0-9-]{0,62}$`), `kind` (`agent | skill | mcpServer`), `ref {namespace, name, version}` (`version` is a constraint), `entry bool` (true on exactly one `agent` node), `label`, and `placeholder {sample metadata}` (only valid when `tutorial: true`).
- `links[]`: `id`, `from`, `to`, and `type` (`uses | calls | delegates | retry`).
  - `calls` has `tools[]`, where an empty list means all tools.
  - `delegates` has `mode` (`subagent | handoff | parallel`), `context` (`task-only | summary | full-transcript | outputs`), `outputs[]` when the context is `outputs`, and `group`. Parallel links with the same `group` start together and are waited on together.
  - `retry` has `maxIterations` (1 to 10) and `until` (a natural-language exit condition of at most 512 characters).
  - Every `delegates` and `retry` link can have `decision {decidedBy: jev|user, model, probabilities{subagent,handoff,parallel}, confidence, riskFlags[], decidedAt}`.
- `target`: `copilot | claude | agents-md`. It is the default export target and can be overridden at export.

Validation rules and their stable error codes:

| Code | Rule |
|---|---|
| `entry_count` | Exactly one entry agent |
| `delegate_cycle` | The `delegates` links form no cycle. `retry` links are left out of this check |
| `retry_not_ancestor` | A `retry` target must be a node that reaches the retry source through `delegates` links |
| `retry_iterations` | `maxIterations` is from 1 to 10 |
| `handoff_return` | A node reached by `handoff` can't reach any of its ancestors through `delegates` links. `retry` is the only allowed back edge, and it is bounded |
| `leaf_outgoing` | `skill` and `mcpServer` nodes have no outgoing links |
| `link_kind` | `uses` goes to a skill, `calls` to an MCP server, and `delegates` and `retry` from an agent to an agent |
| `depth_limit` | The depth from the entry node through `delegates` links is at most 5 |
| `fanout_limit` | Each agent has at most 8 outgoing `delegates` links |
| `parallel_group` | Every link in a parallel group has the same `from` node and mode `parallel` |
| `context_outputs` | `outputs[]` is required when the context is `outputs`, and is forbidden otherwise |
| `ref_unresolved` | The server sets this when a reference can't be resolved or is hidden from the caller |
| `placeholder_not_exportable` | A placeholder node blocks export |
| `risk_full_transcript` (warning) | A `full-transcript` context goes to a node with a `calls` link to an MCP server that Jev or the scans flagged for exfiltration risk |

Access is never inherited. A node's skills, MCP servers, and tools are exactly its own links.

### Schematic bundle (`pkg/schematic`, media type `application/vnd.opendepot.schematic.v1+json`)

`{apiVersion: "opendepot.defdev.io/v1alpha1", kind: "SchematicBundle", metadata {name, displayName, description, tags[], exportedAt, exportedFrom {host, namespace, name, generation}}, assemblyKind, schemaVersion, document, digest}`

- `digest` is the sha256 of the canonical JSON of `document`. Import checks it and rejects a mismatch with `digest_mismatch`.
- Export removes defaults from variables marked `sensitive` and removes literal values for inputs marked sensitive in module contracts. The export dialog says that it does this.
- Import treats the bundle as untrusted. The body is limited to 512 KiB, unknown fields are rejected, and descriptions are sanitized. References are re-resolved against the target cluster with the caller's visibility, and missing namespaces are returned as remap candidates.

### HTTP contracts (server)

| Method and path | Gate | Purpose |
|---|---|---|
| `GET /v2/` | auth | OCI API version check. Returns 401 with a `WWW-Authenticate` challenge when auth is required |
| `GET, HEAD /v2/{namespace}/{name...}/manifests/{reference}` | auth and visibility | Serves a manifest or index by tag or digest |
| `GET, HEAD /v2/{namespace}/{name...}/blobs/{digest}` | auth and visibility | Serves a blob, or a presigned redirect |
| `GET /v2/{namespace}/{name...}/tags/list` | auth and visibility | Lists synced tags only |
| `GET /v2/{namespace}/{name...}/referrers/{digest}` | auth and visibility | Signatures and SBOMs mirrored from upstream |
| Every `POST`, `PUT`, `PATCH`, and `DELETE` under `/v2/` | none | Returns 405 with the OCI error code `UNSUPPORTED` |
| `GET /opendepot/helm/v1/{namespace}/index.yaml` | auth and visibility | Classic Helm repository index. It lists only Helm Versions the caller can see |
| `GET /opendepot/helm/v1/{namespace}/charts/{name}-{version}.tgz` | auth and visibility | The chart layer. `.prov` is served when it exists upstream |
| `GET /opendepot/agents/v1/{namespace}/mcpserver/{name}/...` | existing agents.v1 rules | Versions, download, archive, and signatures for the `mcpserver` kind. The catalog also lists `mcpserver` |
| `POST /opendepot/agents/v1/suggest` | `browseAuthCheck` | Body `{task, kind: skill\|agent\|mcpserver\|any}`, as in the design doc |
| `POST /opendepot/agents/v1/handoff` | `browseAuthCheck` | Runtime handoff advice. Body `{assemblyDigest, from, to, linkId, iteration, summary(≤4 KiB, optional)}`. Response `{proceed, probability, recommendedContext, model}` |
| `POST /opendepot/ui/v1/assembly/agent/advise` | assembly enabled | Graph in, one advisory per `delegates` and `retry` link out |
| `POST /opendepot/ui/v1/assembly/agent/export` | assembly enabled | Graph and target in, `agent-assembly.zip` out |
| `GET /opendepot/ui/v1/schematics` | assembly enabled | List, filtered by `SchematicResources` |
| `GET /opendepot/ui/v1/schematics/permissions` | assembly enabled | `{canWrite, writableNamespaces[], reason}` for the caller, computed with the Schematic write rule |
| `GET /opendepot/ui/v1/schematics/{namespace}/{name}` | the same | Detail |
| `GET /opendepot/ui/v1/schematics/{namespace}/{name}/export` | the same | Bundle download |
| `POST /opendepot/ui/v1/schematics/{namespace}` | the Schematic write rule (see `GroupBindingSpec`) | Create |
| `PUT, DELETE /opendepot/ui/v1/schematics/{namespace}/{name}` | the same, and not built-in | Update with `resourceVersion`, or delete |
| `POST /opendepot/ui/v1/schematics/{namespace}/import` | the same | Create from a bundle |
| `POST /opendepot/ui/v1/schematics/{namespace}/{name}/duplicate` | the same | Copy, including from a built-in Schematic |

OCI auth: the server accepts `Authorization: ******` with an OIDC, client-credentials, or Kubernetes token, the same as the other routes. It also accepts `Basic` credentials, used by `helm registry login`, `docker login`, and kubelet `imagePullSecrets`, in two forms:

- `password` is a bearer token.
- `username` is a client-credentials `client_id` and `password` is its `client_secret`. The server exchanges these with the IdP and caches the result until the token expires.

The challenge is `WWW-Authenticate: Basic realm="opendepot"`. Anonymous mode needs no credentials. Every error uses the OCI error body format.

## Implementation Steps

Steps marked **[SHARED]** change shared files (`api/v1alpha1/types.go`, `go.work`/`go.work.sum`, `go.mod`/`go.sum`, the Makefile, the chart, CI workflows, or generated code). They run before the steps that depend on them, and no other step edits those files. Two steps with no dependency between them never share a file.

### Wave 0: research (no production code)

**Step 1. Explain the 0/5 result from the refined Jev suggest canary.**
- **Depends on:** none.
- **Files:** `.github/plans/jev-skill-suggest-design.md`, in a new "Canary diagnosis" section only.
- Re-run the v2 and refined canaries with `gpt-6-luna`, using interleaved trials. Record session IDs, CLI call counts, the skill text in use, the prompt, and the permission flags. Determine whether the drop came from the skill text, the prompt, or the permissions. Record the cause and the proposed fix. Step 31 depends on this result. Result: recorded in `jev-skill-suggest-design.md` under "Canary diagnosis" (live 2×2 trials, 3 rounds).

**Step 2. Library and feasibility spikes.**
- **Depends on:** none.
- **Files:** `.github/plans/release-0.13.0-spikes.md` (new).
- Confirm and record each of these:
  - (a) `oras.land/oras-go/v2` for upstream pulls, and its license.
  - (b) The current Helm SDK major (v3 or v4) for loading charts and rendering `index.yaml`, with its license and the `helm` CLI versions that `helm pull oci://` accepts.
  - (c) That `trivy image --input <oci-layout>` scans an OCI layout from storage without network access, and that `trivy config` scans a Helm chart.
  - (d) Cosign public-key verification without Rekor, using `github.com/sigstore/sigstore-go` or `github.com/sigstore/cosign/v2/pkg/cosign`. Pick the smaller dependency tree.
  - (e) npm vendoring: `npm ci --omit=dev --ignore-scripts` from a generated lockfile, and how to detect native addons.
  - (f) PyPI vendoring: `pip download --only-binary=:all: --platform <tag> --python-version <v>`.
  - (g) Whether Copilot and Claude can block a delegation tool call with a pre-tool hook, and the tool names to match (`runSubagent`, `Task`, and others).
  - (h) The frontmatter keys each platform uses for handoffs and subagents, checked against the `pkg/agentspec` key tables.
  - (i) The current MCP `server.json` schema version.
- These results are inputs to Steps 7, 11, 15, 16, 23, and 32. When a spike disproves an assumption in this plan, update the plan before running the dependent step.

### Wave 1: foundation

**Step 3. Shared API types and the new kind files. [SHARED]**
- **Depends on:** none.
- **Files:**
  - `api/v1alpha1/types.go`
  - `api/v1alpha1/ociartifact_types.go` (new)
  - `api/v1alpha1/mcpserver_types.go` (new)
  - `api/v1alpha1/schematic_types.go` (new)
  - `api/v1alpha1/types_test.go`
  - `api/v1alpha1/go.mod` (adds `k8s.io/apiextensions-apiserver` for `apiextensionsv1.JSON`, if it isn't already there)
- Add every field and type in [API / CRD Changes](#api--crd-changes). Each new file registers its kinds in its own `init()`.
- Tests: CEL rules on OCIArtifactConfig and MCPServerSource, enum values, and a JSON round-trip for each new type.

**Step 4. Code generation and CRD sync. [SHARED]**
- **Depends on:** 3.
- **Files:**
  - `api/v1alpha1/zz_generated.deepcopy.go`
  - `chart/opendepot/crds/*.yaml`: the updated files, plus the new `opendepot.defdev.io_ociartifacts.yaml`, `opendepot.defdev.io_mcpservers.yaml`, and `opendepot.defdev.io_schematics.yaml`
- Run `make generate` and `make manifests` in `api/v1alpha1`, then copy the CRDs into the chart the same way the repo already does. Check that each CRD is under 256 KiB, and set `crd:maxDescLen` if one isn't.

**Step 5. Repository scaffolding and the `iac` consolidation. [SHARED]**
- **Depends on:** none.
- **Files:**
  - `services/iac/**` (new): `cmd/main.go`, `internal/controller/module/**` and `internal/controller/provider/**` moved from `services/module/internal/controller` and `services/provider/internal/controller`, `test/e2e/**` moved from both services, plus `Dockerfile`, `Makefile`, `PROJECT`, `go.mod`, `go.sum`, `README.md`, and the coverage-tagged files.
  - Delete `services/module/**` and `services/provider/**`.
  - Empty module skeletons with a package doc and `go.mod`: `services/oci/**`, `pkg/oci/**`, `pkg/mcp/**`, `pkg/agentassembly/**`, and `pkg/schematic/**`.
  - `go.work`, `go.work.sum`, `Makefile`, `Tiltfile`, `tilt/values.yaml`, `hack/check-go-work-modules.py`, `.github/workflows/build.yaml`, `.github/workflows/e2e.yaml`, `.github/workflows/release.yaml`, and `sonar-project.properties` if it exists.
- `services/iac/cmd/main.go` registers `ModuleReconciler` and `ProviderReconciler` in one manager, the same way `services/agent/cmd/main.go` registers two reconcilers. Use a new `LeaderElectionID`. Keep the controller names `module` and `provider` so metrics and log keys don't change.
- Makefile: `SERVICES` replaces `module-controller provider-controller` with `iac-controller oci-controller`, and adds the matching `_PATH` and `_CONTEXT` entries. `oci-controller` builds a placeholder manager until Step 9.
- No behavior change to Module or Provider reconciliation. The moved unit and envtest suites must pass without changes.

**Step 6. Chart: `iac` and `oci` deployments, and the values migration. [SHARED]**
- **Depends on:** 4, 5.
- **Files:**
  - `chart/opendepot/templates/iac-deployment.yaml`, `iac-rbac.yaml`, and `iac-serviceaccount.yaml` (new)
  - `oci-deployment.yaml`, `oci-rbac.yaml`, and `oci-serviceaccount.yaml` (new)
  - Delete `module-*.yaml` and `provider-*.yaml`.
  - `chart/opendepot/templates/_helpers.tpl`, `chart/opendepot/templates/NOTES.txt` (new), and `chart/opendepot/values.yaml`
- `iac-rbac` is the union of the old module and provider rules.
- `_helpers.tpl` adds `opendepot.iac.value`. It reads `iac.<key>` first, then `module.<key>` or `provider.<key>`, so existing values files still work. `NOTES.txt` prints a deprecation warning when it finds `module.*` or `provider.*`.
- `oci.enabled` defaults to `false`.
- `oci.allowedRegistries` (for example `[ghcr.io, docker.io, registry.k8s.io, quay.io]`) is passed to the depot, oci, version, and server deployments as a flag. An empty list denies all mirrors.

### Wave 2: backend lanes (run in parallel once their dependencies are done)

#### Lane OCI

**Step 7. `pkg/oci` library.**
- **Depends on:** 2, 5.
- **Files:** `pkg/oci/**`, including `go.mod` and `go.sum` for this module only.
- Content-addressed storage layout on `pkg/storage`:
  - `oci/<namespace>/<repo>/blobs/sha256/<hex>`
  - `oci/<namespace>/<repo>/manifests/sha256/<hex>`
  - `oci/<namespace>/<repo>/tags/<tag>`, a small JSON file that points to a digest
- Writes are idempotent, and each blob's digest is checked on write and on read.
- Upstream client wrapper on oras-go. It checks the registry allow-list on every request, including redirects, which blocks SSRF. Credentials come from a dockerconfigjson. It lists tags, pulls by digest, follows indexes filtered by platform, and pulls referrers.
  - Pin `oras.land/oras-go/v2` at `v2.7.0`. Do not use v3, which is a breaking redesign. `auth.Client` keeps a custom `CheckRedirect` and strips cross-origin `Authorization`, but it does not block internal addresses, so add a `CheckRedirect` and dial-time IP validation to enforce the allow-list.
  - Parse the dockerconfigjson `auths` map in memory into a custom `CredentialFunc`. Never write a temporary file. (Spikes a.)
- Helm: parses `Chart.yaml` metadata into `HelmChartMetadata`, packages a chart `.tgz` into an OCI manifest with the Helm media types, and renders `index.yaml` deterministically from a list of chart entries with server-relative URLs.
  - SDK choice: use `helm.sh/helm/v4` (`v4.3.2`, packages `pkg/chart/v2/loader` and `pkg/repo/v1`) for the new code. The v3 SDK (`v3.22.1`, `pkg/chart/loader` and `pkg/repo`) is the alternative. Pick one. Both declare `go 1.26.0` and are Apache-2.0.
  - Media types: config `application/vnd.cncf.helm.config.v1+json`, chart layer `application/vnd.cncf.helm.chart.content.v1.tar+gzip`, provenance `application/vnd.cncf.helm.chart.provenance.v1.prov`.
  - Helm `>= 3.8.0` is the floor for `helm pull oci://` compatibility. (Spikes b.)
- Generic: wraps a release asset as a single-layer OCI artifact with the configured media types.
- Unit tests: digest checks, allow-list including redirects, platform filtering, `index.yaml` golden files, and Helm media types.

**Step 8. Server dependencies. [SHARED]**
- **Depends on:** 5.
- **Files:** `services/server/go.mod` and `services/server/go.sum`.
- Add `pkg/oci`, `pkg/mcp`, `pkg/agentassembly`, and `pkg/schematic`, using the same local-module pattern the server already uses for `pkg/*`. Add any third-party libraries that Step 2 selected for the server.

**Step 9. `oci` controller: `OCIArtifactReconciler`.**
- **Depends on:** 4, 5.
- **Files:** `services/oci/cmd/main.go`, `services/oci/internal/controller/opendepot_ociartifacts_controller.go`, its tests and `suite_test.go`, and `services/oci/go.mod` and `go.sum`.
- Follow the Agent controller pattern:
  - Create one Version per `spec.versions` entry. For Image, this is one Version per tag. The OCI index keeps every selected platform, so there is no per-platform Version fan-out like providers have.
  - Set `VersionSpec.Type = OCIArtifact` and `OCIArtifactConfigRef`.
  - Trim to `versionHistoryLimit`, remove Versions that are no longer declared, and set `LatestVersion` (semver order, or discovery order when `tagPattern` is set).
- Envtest covers create, trim, garbage collection, and the latest version.

**Step 10. Version controller dependencies and type dispatch. [SHARED]**
- **Depends on:** 4, 5.
- **Files:** `services/version/go.mod`, `services/version/go.sum`, `services/version/internal/controller/opendepot_versions_controller.go` (dispatch only), and `services/version/Dockerfile`.
- Dispatch `OCIArtifact` to `reconcileOCIVersion` and `MCPServer` to `reconcileMCPVersion`. Step 11 provides the first and Step 15 the second. Until they exist, the stubs return a status of "unsupported in this build".
- Dockerfile: add an `INCLUDE_MCP_RUNTIMES` build arg that installs pinned Node.js and Python with pip, used only for vendoring. The Makefile target is added in Step 38.

**Step 11. Version controller OCI sync.**
- **Depends on:** 7, 10.
- **Files:** `services/version/internal/controller/oci_sync.go`, `oci_scan.go`, `oci_verify.go`, and their `_test.go` files.
- Mirror:
  - Resolve the tag to a digest once, and record it in `status.ociManifest.digest`.
  - Pull the index or manifest, filtered by platform, and its blobs into storage through `pkg/oci`. Mirror referrers too.
  - On later reconciles, compare the upstream digest and set `tagDrift` when it moved. Never re-sync when `immutable` is unset or true.
- GitHub (Helm and Generic): download the release asset matching `assetPattern` and package it through `pkg/oci`. Helm Versions set `status.helmChart`.
- Verify: when `verify` is set, check the cosign signature against the public key Secret using `github.com/sigstore/cosign/v2` (`v2.6.5`, `go 1.25.0`) with `CheckOpts{SigVerifier, IgnoreTlog: true}`. Do not use `sigstore-go`: it declares `go 1.27.0` and omits OCI `.sig` verification. Fallback, if the cosign closure is judged too large: `github.com/sigstore/sigstore` (`v1.11.4`) with `oras-go` to fetch the `.sig` tag or referrer and verify the simple-signing payload. Add a runtime key-only verification test in this step, because Wave 0 verified only the API and footprint. When `required` is true, a missing or invalid signature blocks the Version. (Spikes d.)
- Scan, when `scanning.enabled` is true:
  - Image: `trivy image --input <unpacked OCI layout directory>` with `--skip-db-update --skip-java-db-update --offline-scan`. A tar of the layout is rejected, and the DB cache must already be seeded. (Spikes c.)
  - Helm: `trivy config --skip-check-update` on the unpacked chart.
  - Generic: `trivy fs`.
  - Write results to `status.ociScan`, and apply ScanPolicy with scan type `oci`.
- Set `archiveSizeBytes` to `totalSizeBytes`.

**Step 12. Depot discovery for OCI and MCP sources.**
- **Depends on:** 4, 7, 16.
- **Files:** `services/depot/internal/controller/oci_discovery.go`, `mcp_discovery.go`, the existing Depot reconciler file (it adds calls to the two discovery functions and status lists), `services/depot/go.mod` and `go.sum`, and tests.
- OCI: list upstream tags through `pkg/oci` and filter them by `tagPattern` or `versionConstraints`. GitHub assets: list releases and filter them by `tagPrefix` and `assetPattern`.
- MCP:
  - npm: the registry packument `versions`.
  - PyPI: the JSON API `releases`, keeping only releases that have wheels for the configured platforms.
  - Source: Git tags, the same as Skills.
  - Image: no discovery. The versions follow the referenced OCIArtifact's synced tags.
- Create and update `OCIArtifact` and `MCPServer` resources, and record them in `status.ociArtifacts` and `status.mcpServers`.
- Check every upstream URL against the allow-list.

**Step 13. Server OCI Distribution read API and auth.**
- **Depends on:** 7, 8.
- **Files:** `services/server/oci_distribution.go`, `oci_auth.go`, `oci_errors.go`, and their `_test.go` files.
- Implement the `/v2/` routes in [HTTP contracts](#http-contracts-server). The name `{namespace}/{name...}` maps to an OCIArtifact. Only synced Versions are served, and blocked ones are not.
- Blob GETs use presigned redirects when `storageConfig.presign` is enabled, and stream otherwise. Set `Docker-Content-Digest` and `Content-Length`, and support `HEAD` and `Range`.
- Auth: accept Bearer and Basic as specified. The client-credentials exchange goes to the IdP token URL already configured (`--oidc-token-url`), with a cache keyed by a hash of the client ID and secret. Never log credentials. Visibility uses `OCIArtifactResources`.
- Write methods return 405 `UNSUPPORTED`. Handlers are exported for wiring in Step 28.

**Step 14. Server Helm repository.**
- **Depends on:** 7, 8.
- **Files:** `services/server/helm_repo.go` and `helm_repo_test.go`.
- `index.yaml` per namespace, built from Helm Versions the caller can see. Short-lived cache keyed by namespace, binding, and the Versions' resourceVersion. Serves chart `.tgz` and `.prov` files from the chart layer blob.

#### Lane MCP

**Step 15. Version controller MCP sync.**
- **Depends on:** 2, 10, 16.
- **Files:** `services/version/internal/controller/mcp_sync.go`, `mcp_vendor.go`, `mcp_jev.go`, and their `_test.go` files.
- By source type:
  - Npm: resolve the exact version. In a scratch directory with no network access except the allow-listed registry, generate a lockfile with `npm install --package-lock-only --ignore-scripts`, then run `npm ci --omit=dev --ignore-scripts`. Never run install scripts. Detect native addons from lockfile metadata and package tarballs, for every platform (not the installed tree): `binding.gyp`, `hasInstallScript` on a native build dependency, `*.node` files, and platform-constrained optional packages (`os`/`cpu`). Fail the Version with `mcp-native-addon` when one is found. (Spikes e.)
  - PyPI: for each configured platform, run `pip3 download --only-binary=:all: --platform <tag> --python-version <v> --no-deps`. Add `--implementation cp --abi cp<v>` to pin the ABI where the wheel set needs it. Never fetch an sdist, because pip runs its build backend. Treat `from versions: none` as `mcp-sdist-only` and fail the Version. (Spikes f.)
  - Image: resolve the referenced OCIArtifact Version by tag, and record its digest. No archive.
  - Source: archive the repo path, the same way Skills are archived. `runnable=false`.
- Package the vendored tree and a generated `opendepot-mcp.json` launch descriptor (from `pkg/mcp`) into an archive. Sign `SHA256SUMS` with the existing agent signing code, and run `trivy fs` on the vendored tree with scan type `mcp`.
- Jev, when it's enabled and `jevSecretRef` is set, assesses `server.json`, the README, and the package metadata using the existing chunked assessment path. Tool descriptions are assessed in 0.14.0, once the runtime can call `tools/list`.
- Set `status.mcpMetadata`.

**Step 16. `pkg/mcp` library.**
- **Depends on:** 2, 5.
- **Files:** `pkg/mcp/**`.
- Parse and validate `server.json` against schema `2025-12-11` (`https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json`). Do not target the unreleased draft. Required: `name` (`^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`), `description`, and `version`. Each `packages[]` entry requires `registryType`, `identifier`, and `transport`. (Spikes i.) Define the `opendepot-mcp.json` launch descriptor: command, args, transport, and runtime (`node`, `python`, or `oci`).
- npm packument and PyPI JSON clients, with allow-list checks.
- Client config renderers for each install target: Copilot (`.vscode/mcp.json`), Claude (`.mcp.json`), Codex (`.codex/config.toml`, with `[mcp_servers.<name>]` TOML tables, not `.mcp.json`; confirm the project-trust requirement before writing it), and a custom path. (Spikes g.)
  - Each entry launches the locally installed, verified copy. For Image, the entry runs the pinned digest from the OpenDepot OCI endpoint.
  - The renderers merge into existing files and only change entries marked as managed by OpenDepot.
- Golden-file tests.

**Step 17. Agent controller: `MCPServerReconciler`.**
- **Depends on:** 4.
- **Files:** `services/agent/internal/controller/opendepot_mcpservers_controller.go`, its tests, and `services/agent/cmd/main.go` (registration).
- Same lifecycle as `AgentReconciler`. For the Image type, check that the referenced OCIArtifact exists and has `artifactType: Image`, and set an error status if it doesn't.

**Step 18. Server: `mcpserver` kind in agents.v1.**
- **Depends on:** 4, 8.
- **Files:** `services/server/agents_mcp.go` (new), `agents_catalog.go`, `agents.go` (kind mapping only), and tests.
- Add `mcpserver` to the kind mapping for the versions, download, archive, and signature routes. Catalog items for MCP servers add `sourceType`, `transport`, and `runnable`. Image-type downloads return the OCI reference and digest instead of an archive URL.
- Visibility uses `MCPServerResources`.

#### Lane Jev

**Step 19. `pkg/jev` question sets for suggest and handoff.**
- **Depends on:** none.
- **Files:** `pkg/jev/suggest.go`, `pkg/jev/handoff.go`, and their tests.
- Suggest, as in the design doc:
  - Stage 1 is a coarse `Choice` over names and descriptions.
  - Stage 2 reranks with a per-candidate `fits` score.
- Handoff:
  - A three-way mode choice (`subagent`, `handoff`, `parallel`), using the two agents' descriptions and tools.
  - A context recommendation.
  - Risk flags: exfiltration risk and overlapping skills.
  - At runtime, a `proceed` probability for a retry, using the `until` condition and the summary.
- Probabilities, not confidence, are used for decisions, following the existing Jev conventions.

**Step 20. Server: Jev credentials from GroupBinding.**
- **Depends on:** 4.
- **Files:** `services/server/jev_credentials.go` and `jev_credentials_test.go`.
- Resolve the caller's binding and read only `jevSecretRef` from the server namespace. When the caller has no binding, or the server runs in anonymous or non-OIDC mode, use the default from the chart value `server.jev.defaultSecretRef` if it's set. Otherwise Jev is unavailable, with error `jev_unavailable`. Log the secret and binding names, never the token.

**Step 21. Server suggest endpoint.**
- **Depends on:** 18, 19, 20.
- **Files:** `services/server/agents_suggest.go` and `agents_suggest_test.go`.
- As in the design doc:
  - Only items that are available and visible to the caller are sent to Jev or returned. Blocked, rejected, or unassessed items are never sent or returned.
  - The response has the top N items with a reason, plus the catalog fields.
  - Logs record the model and item IDs, never the task text.
- Body limits: the task is at most 2 KiB, and the shortlist is capped.

**Step 22. Server agent assembly advise, export, and handoff.**
- **Depends on:** 19, 20, 23.
- **Files:** `services/server/ui_agent_assembly.go`, `agents_handoff.go`, and their `_test.go` files.
- Advise: validate the graph with `pkg/agentassembly`, re-resolve every reference with the caller's visibility, and call Jev once per `delegates` and `retry` link, capped at 32 links per request. Returns one advisory per link. When Jev is unavailable, it returns deterministic advice only, with a notice.
- Export: validate and re-resolve, then render with `pkg/agentassembly` for the chosen target. Returns `agent-assembly.zip` with the agent files, `opendepot.hcl` (exact versions), `assembly.json` (graph, decisions, and digest), and `README.md`, which explains how to install and enable the hook. Diagnostics use the same error shape as the Tofu export.
- Handoff: look up the decision saved in `assembly.json` for the link, which the CLI sends with the digest. Return Jev's advice. It never overrides a deterministic deny, and it never stores the summary.

#### Lane Agent Assembly

**Step 23. `pkg/agentassembly` library.**
- **Depends on:** 2, 5.
- **Files:** `pkg/agentassembly/**`.
- The document types, the validation rules and codes in [Agent assembly document](#agent-assembly-document-pkgagentassembly-held-in-schematicspecdocument-when-assemblykind-agent), and the canonical digest.
- Renderers for `copilot`, `claude`, and `agents-md`:
  - Do not emit Copilot `handoffs` frontmatter, because the Copilot harness does not accept it. Render handoffs and delegation allowlists as a generated "Delegation" body section for `copilot`, `claude`, and `agents-md`. For Claude, use `Agent(...)` in `tools:` where applicable. `agents-md` has no key table in `pkg/agentspec`, so it validates against the declared platform. Update the `pkg/agentspec` key tables only if a new key is needed, such as Copilot `skills` or `hooks`. (Spikes h.)
  - Each agent's `tools` lists only its own linked MCP tools, plus the platform's delegation tool (Copilot `task`, Claude `Agent`) when the agent has `delegates` links.
- An `opendepot.hcl` renderer with `agent`, `agent_skill`, and `mcp_server` blocks.
- Runtime route policy, used by Step 32:
  - Given `assembly.json`, `from`, `to`, and the iteration state, return allow or deny with a reason.
  - Deny when the link isn't declared, when a retry exceeds `maxIterations`, or when the delegation tool targets an undeclared agent.
- Golden-file and property tests: random DAGs, cycles, and retry placements.

#### Lane Schematic

**Step 24. `pkg/schematic` library.**
- **Depends on:** 5, 23.
- **Files:** `pkg/schematic/**`.
- The bundle format, canonical JSON and digest, and document validation that dispatches to `pkg/agentassembly` (Agent) or `pkg/hclschema/assemblyexport` (Tofu).
- Reference extraction to `SchematicReference`.
- Export sanitization: removes sensitive defaults and literal values.
- Import sanitization: unknown fields, size limits, description cleanup, and namespace remapping.
- Placeholder rules.
- Tests: round-trip, tampered digest, oversized input, and sensitive values removed.

**Step 25. Depot controller: `SchematicReconciler`.**
- **Depends on:** 4, 12, 24.
- **Files:** `services/depot/internal/controller/opendepot_schematics_controller.go`, its tests, `services/depot/cmd/main.go` (registration), and `services/depot/go.mod` and `go.sum` (adds `pkg/schematic`).
- On spec change, or every 10 minutes:
  - Validate the document, compute the digest, and resolve the references with the controller's own permissions (no caller visibility applies here).
  - Set `valid`, `diagnostics`, `references[].state`, and the `Ready` and `ReferencesResolved` conditions.
- Watch Version changes through an index from each referenced parent to the Schematic, so that yanking or blocking a Version updates `state`.

**Step 26. Server Schematic API.**
- **Depends on:** 8, 24.
- **Files:** `services/server/ui_schematics.go` and `ui_schematics_test.go`.
- The routes in [HTTP contracts](#http-contracts-server). Reads filter on `SchematicResources`.
- Writes follow the Schematic write rule in `GroupBindingSpec`, so saving works by default in the caller's binding namespace. Writes use optimistic concurrency through `resourceVersion`, and set `origin`. A denied write returns 403 with the code `schematic_write_denied` and the reason: `disabled`, `binding_readonly`, `namespace_not_allowed`, or `anonymous`.
- Built-in Schematics reject `PUT` and `DELETE` with `builtin_readonly`.
- `GET .../schematics/permissions` returns the namespaces the caller can write to, so the UI doesn't have to work out the rule itself.
- Import returns remap candidates for missing references. The UI can resubmit with a namespace map.
- `references[].state` is recomputed with the caller's visibility before it is returned, so hidden references show as `Hidden` and no names leak.

**Step 27. Built-in Schematics content.**
- **Depends on:** 24.
- **Files:** `chart/opendepot/files/schematics/*.json` (new directory).
- Tutorial Schematics, with `tutorial: true` and placeholder nodes that carry embedded sample contracts and metadata:
  - `tutorial-tofu-basics`: one variable, a placeholder `s3-bucket` module repeated with `for_each`, and a placeholder `aws` provider.
  - `tutorial-agent-basics`: the entry agent `release-captain`, a skill, an MCP server with a tool allow-list, a subagent link, a parallel group, and a retry from `security-reviewer` to `changelog-writer` with `maxIterations: 3`.
- Catalog examples, not tutorials, that reference well-known resources and show `Missing` when those aren't onboarded:
  - `tofu-lambda-for-each`, matching the example in the current guide.
  - `agent-code-review-loop`, with a coder, a reviewer, and a retry.
- Validate every file with `pkg/schematic` in a unit test in `pkg/schematic` that reads the chart directory.

**Step 28. Server wiring. [SHARED]**
- **Depends on:** 13, 14, 18, 21, 22, 26.
- **Files:** `services/server/main.go`, `services/server/types.go`, `services/server/ui_browse.go`, `services/server/metrics_collector.go`, `services/server/discovery.go`, and their tests.
- Register every route. Add the flags:
  - `--oci-enabled`
  - `--oci-allowed-registries` (for auth decisions only)
  - `--helm-repo-enabled`
  - `--schematic-management-enabled` (default `true`)
  - `--jev-suggest-enabled`
  - `--jev-default-secret` (optional)
- Browse: add `OCIArtifact` and `MCPServer` cards and details, with usage snippet data. Metrics: counts and sync gauges for the new kinds.
- Discovery adds `helm.v1` and `oci.v1` hints to `/.well-known/terraform.json`, which Terraform ignores, so the CLI and UI can find the endpoints.

#### Lane CLI

**Step 29. CLI `mcp_server` entries and MCP config targets.**
- **Depends on:** 16, 18.
- **Files:** `cli/config.go`, `cli/lock.go`, `cli/mcp.go` (new), `cli/mcp_test.go` (new), `cli/config_test.go`, and `cli/lock_test.go`.
- `opendepot.hcl` gets an `mcp_server "<ns>/<name>" { version = "...", targets = [...] }` block. The lock records `zh:`, `h1:`, and `signature_hash` the same as agents. For Image, it records the OCI digest instead.
- On `apply`, the CLI:
  - Verifies the signature and hashes.
  - Unpacks to `.opendepot/mcp/<ns>/<name>/<version>/`.
  - Renders the client config through `pkg/mcp`.
- `verify` re-checks the installed tree and the managed config entries.
- Image entries: the CLI never pulls the image itself. It writes a config entry that runs the pinned digest.

**Step 30. CLI `suggest`.**
- **Depends on:** 21.
- **Files:** `cli/suggest.go` and `cli/suggest_test.go` (new).
- `opendepot suggest "<task>" [--kind skill|agent|mcpserver|any] [--registry] [--json]`. Sends a POST body, never task text in the URL. Prints a table or JSON. Uses the stored token for the registry host.

**Step 31. CLI skill text update.**
- **Depends on:** 1, 30.
- **Files:** the CLI skill or AGENTS.md text that the CLI ships. The Developer finds the file starting from `cli/agentsmd.go`, and no other step edits it.
- Apply the fix from Step 1, and tell agents to run `opendepot suggest` before `list` and `describe`.
- Step 1 root cause: the original 0/5 drop came from the prompt, which never triggered the skill. The v1 `SKILL.md` description was also invalid YAML (an unquoted `: `), which removed the skill from the catalog. Keep the refined body. Add:
  - a CI test that parses every shipped `SKILL.md` frontmatter as YAML and fails on invalid YAML;
  - explicit trigger wording in the catalog `description`, which is the always-loaded text. The wording should name a task that needs a skill or agent the agent does not have, and include the `opendepot suggest` instruction. This wording is untested, so confirm it with a follow-up canary. (`jev-skill-suggest-design.md`, "Canary diagnosis".)

**Step 32. CLI `assembly route` hook.**
- **Depends on:** 2, 22, 23.
- **Files:** `cli/assembly_route.go` and `cli/assembly_route_test.go` (new).
- `opendepot assembly route --assembly assembly.json --from <id> --to <id> [--link <id>] [--state .opendepot/assembly-state.json] [--jev] [--summary-file <path>]`.
- Exit codes: 0 allows, 2 denies (with the reason on stderr), and 1 is an error.
- The deterministic policy comes from `pkg/agentassembly`. Retry counters are kept in the state file, keyed by the assembly digest and link.
- With `--jev`, the CLI calls `POST /opendepot/agents/v1/handoff` with a timeout of 3 s or less. When the server can't be reached, it uses the saved decision. Jev can only turn an allow into a "stop retrying", never a deny into an allow.
- Document how to wire it into hooks for each platform, as confirmed in Step 2(g). Delegation matchers: Copilot `^(task|runSubagent|Agent|Task)$`, which denies with JSON `permissionDecision: deny` on stdout. Claude `Agent|Task`, which denies with `hookSpecificOutput.permissionDecision: deny` or exit 2. `runSubagent` is deprecated in Copilot (removal February 2027), so keep it in the matcher only for compatibility.

**Step 33. CLI command wiring. [SHARED]**
- **Depends on:** 29, 30, 32.
- **Files:** `cli/commands.go`, `cli/go.mod`, and `cli/go.sum`.
- Register `suggest` and `assembly route`. Add `mcp_server` to `list` and `describe` (`list mcpservers`). Add the `pkg/mcp` and `pkg/agentassembly` modules.

#### Lane UI (UI Developer agent; follow the `opendepot-branding` skill)

**Step 34. Registry UI for the new kinds.**
- **Depends on:** 28.
- **Files:**
  - `services/ui/src/components/Sidebar.tsx`: add the OCI artifacts, MCP servers, and Schematics links.
  - `services/ui/src/components/resourceDisplay.tsx`, `UsageSnippet.tsx`, and `DepotKindIcon.tsx`.
  - `services/ui/src/types/**`.
  - `services/ui/src/app/page.tsx`: the kind filter.
  - `services/ui/src/app/[namespace]/**`: the detail pages.
  - The tests for each.
- OCIArtifact rows show an `artifactType` chip (Helm, Image, or Generic). The detail page shows the digest, platforms, signature verification, tag drift, and scan results. Usage snippets:
  - `helm pull oci://<host>/<ns>/<name> --version <v>`
  - `helm repo add <ns> https://<host>/opendepot/helm/v1/<ns>` with `helm install`
  - `docker pull <host>/<ns>/<name>@<digest>`
  - `oras pull`
- MCPServer rows show the source type, transport, and whether the server can run. The usage snippet is the `mcp_server` HCL block.

**Step 35. Agent assembly canvas.**
- **Depends on:** 28.
- **Files:** `services/ui/src/components/assembly/agent/**` (new), `services/ui/src/components/assembly/AssemblyCanvas.tsx`, `services/ui/src/components/assembly/types.ts`, `services/ui/src/app/assembly/page.tsx`, and the tests.
- A canvas kind switch (Tofu or Agent). The palette lists Agents, Skills, and MCP servers.
- Nodes: agent (with an entry badge), skill, and MCP server (with a tool allow-list picker).
- Link editors:
  - `delegates`: mode, context, outputs, and parallel group.
  - `retry`: `maxIterations` and `until`.
- Layout: a tree, with shared nodes drawn once and extra parents shown as dashed links. Retry links are drawn as curved back links labeled `×N`.
- Validation runs in the browser with the same codes as `pkg/agentassembly`.
- "Ask Jev" calls advise and shows the probabilities. The user accepts or overrides, and the choice is saved as a `decision`.
- The export dialog has a target picker, then shows the diagnostics or downloads the ZIP.
- Canvases persist in `localStorage` as before, with a separate key for each kind.

**Step 36. Schematics catalog.**
- **Depends on:** 34, 35.
- **Files:** `services/ui/src/app/assembly/schematics/**` (new), `services/ui/src/components/schematics/**` (new), `services/ui/src/lib/schematicsApi.ts` (new), `services/ui/src/components/assembly/AssemblyCanvas.tsx` (toolbar actions only), and the tests.
- Catalog: a ledger table, filterable by kind, tag, and namespace, with a built-in badge and a valid or stale state.
- Detail: a read-only canvas preview and the list of references with their state.
- Actions:
  - Open in Assembly Line.
  - Duplicate.
  - Export: downloads the bundle.
  - Import: upload a bundle, remap namespaces, then create.
  - Save as Schematic, from the canvas toolbar.
- Write actions are shown by default. The UI asks the server which namespaces the caller can write to, and the "Save as Schematic" namespace picker lists only those. It defaults to the caller's binding namespace. When the caller can't write (management turned off, a read-only binding, or anonymous), write actions are hidden. Export, opening a Schematic in the canvas, and importing a file into the local canvas still work.

**Step 37. Assembly Line guided tour.**
- **Depends on:** 27, 35, 36.
- **Files:** `services/ui/src/components/tour/**` (new), and `data-tour` attributes only in `services/ui/src/components/assembly/**` and `services/ui/src/components/schematics/**`. Steps 35 and 36 are complete before this step.
- Use a small tour engine built on MUI `Popper` and a backdrop. Add no dependency unless Step 2 approves one.
- Two tracks start from a "Take the tour" button on `/assembly` and from a dismissible prompt on the first visit. Each track loads its tutorial Schematic.
  - **Tofu basics** (about 8 steps): the palette, a variable and its type, a placeholder module, wiring an input, `for_each`, a provider binding, the HCL preview, and save or export (shown as a preview, because placeholders can't be exported).
  - **Agent basics** (about 9 steps): the entry agent, attaching a skill, an MCP server and its tool allow-list, a subagent link, choosing a mode with "Ask Jev" (shows labeled sample advice when Jev is unavailable), a parallel group, a retry with `maxIterations`, validation, and an export preview.
- Progress is saved in `localStorage` and can be resumed or restarted. Every step works with the keyboard, and the tour meets WCAG AA.
- The last step offers "Replace placeholders with real resources" and "Browse Schematics".

### Wave 3: integration

**Step 38. Final chart changes and release version. [SHARED]**
- **Depends on:** 6, 12, 15, 17, 25, 27, 28.
- **Files:** `chart/opendepot/values.yaml`, `Chart.yaml`, `README.md`, the templates `agent-rbac.yaml`, `depot-rbac.yaml`, `server-rbac.yaml`, `version-rbac.yaml`, `oci-rbac.yaml`, `server-deployment.yaml`, `version-deployment.yaml`, `depot-deployment.yaml`, and `oci-deployment.yaml`, the new template `schematics-builtin.yaml`, and the `Makefile` targets `build-version-controller-mcp` and `load-version-controller-mcp`.
- See [Helm Chart](#helm-chart) for every value and RBAC rule. Set `version` and `appVersion` to `0.13.0`.

**Step 39. OCI e2e.**
- **Depends on:** 9, 11, 12, 13, 14, 28, 38.
- **Files:** `services/oci/test/e2e/**` (new).
- See [E2E Test Changes](#e2e-test-changes).

**Step 40. MCP e2e.**
- **Depends on:** 15, 17, 18, 29, 33, 38.
- **Files:** `services/agent/test/e2e/mcpserver_e2e_test.go` and fixtures (new). Do not edit the existing agent e2e files.

**Step 41. Server e2e: suggest, Schematics, and agent assembly.**
- **Depends on:** 21, 22, 26, 28, 38.
- **Files:** `services/server/test/e2e/suggest_e2e_test.go`, `schematics_e2e_test.go`, `agent_assembly_e2e_test.go`, and any `values-*.yaml` they need (all new).

**Step 42. Depot e2e: discovery and the Schematic reconciler.**
- **Depends on:** 12, 25, 38.
- **Files:** `services/depot/test/e2e/oci_mcp_discovery_e2e_test.go` and `schematic_status_e2e_test.go` (new).

**Step 43. UI Playwright tests.**
- **Depends on:** 34, 35, 36, 37.
- **Files:** new specs under the existing Playwright directory in `services/ui/`: `agent-assembly.spec.ts`, `schematics.spec.ts`, `tour.spec.ts`, and `registry-oci-mcp.spec.ts`.

**Step 44. Docs.**
- **Depends on:** 34, 35, 36, 37, 38.
- **Files:**
  - New:
    - `docs/guides/oci-artifacts.md`, `docs/guides/helm.md`, and `docs/guides/mcp-servers.md`
    - `docs/guides/assembly-line-agents.md`, `docs/guides/schematics.md`, and `docs/guides/assembly-line-tutorial.md`
    - `docs/reference/oci-distribution.md` and `docs/reference/helm-repository.md`
  - Updated:
    - `docs/architecture.md`: the controller diagram and the component sections
    - `docs/guides/assembly-line.md`, `docs/reference/agents-v1.md` (the `mcpserver` kind, suggest, and handoff), and `docs/reference/cli.md`
    - `docs/helm-chart.md`, `docs/upgrading.md` (0.12 to 0.13: the `iac.*` values, `kubectl apply -f chart/opendepot/crds/`, and the removed deployments)
    - `docs/configuration/assembly-line.md`, `docs/guides/groupbinding.md`, and `mkdocs.yml` (navigation)
- Use the `ui-screenshot-documentation` skill for screenshots, with dark and light pairs.

**Step 45. Review, security review, and scan.**
- **Depends on:** 39, 40, 41, 42, 43, 44.
- **Files:** none (review only).
- Code Review, then Security Review, then a SonarQube scan with the `opendepot-sonarqube` skill. Security Review must cover:
  - OCI Basic auth and the client-credentials exchange
  - The registry allow-list and redirect SSRF
  - The npm and PyPI vendoring supply chain
  - The Schematic write path and import of untrusted bundles
  - Jev secret RBAC and `resourceNames`
  - Prompt injection through descriptions, Schematics, and handoff summaries
  - `assembly route` deny-by-default behavior

## E2E Test Changes

Run e2e suites with the `opendepot-testing` skill, focused with `E2E_FOCUS`, on an isolated Kind cluster.

- **`services/iac/test/e2e`** (moved in Step 5): the existing module and provider scenarios must pass unchanged. Add an upgrade scenario: install chart 0.12.0, upgrade to 0.13.0 with only `module.*` and `provider.*` values, and check that the `iac` deployment is ready, existing Modules and Providers stay synced, and the old deployments are gone.
- **`services/oci/test/e2e`** (new; uses a `registry:2` or zot deployment in Kind as the upstream, seeded with one chart, one multi-arch image, and one generic artifact):
  - A mirrored Helm OCIArtifact syncs. `helm pull oci://` and `helm repo add` + `helm template` both work.
  - A mirrored Image filtered to `linux/arm64` contains only that platform, and `crane pull` by digest works.
  - A Generic artifact works with `oras pull`.
  - A GitHub release Helm chart. Use the GitHub App secret pattern that the module e2e tests already use.
  - Re-tagging upstream sets `tagDrift`, and the served digest doesn't change.
  - Cosign `required` with an unsigned artifact blocks the Version.
  - A registry that isn't on the allow-list is rejected.
  - Push returns 405 `UNSUPPORTED`.
  - GroupBinding: a private artifact returns 401 to an anonymous caller and is visible to a bound token. Basic auth with client credentials works.
- **`services/agent/test/e2e/mcpserver_e2e_test.go`** (new):
  - An npm fixture served from an in-cluster HTTP registry, a PyPI fixture with wheels, an Image type that references an OCIArtifact, and a Source type that is not runnable.
  - A native addon fails with `mcp-native-addon`.
  - The catalog lists `mcpserver`.
  - `opendepot apply` writes `.vscode/mcp.json` and `.mcp.json` pinned to the verified local paths, and `verify` detects tampering.
- **`services/server/test/e2e`** (new files only):
  - Suggest, using the existing fake Jev endpoint: a blocked item is never returned, and a hidden item is never sent to Jev.
  - Schematics: create, update, and delete with chart defaults, using a binding that leaves `schematicManagement` unset, in its own namespace. Writes are denied in another namespace unless it's listed in `schematicNamespaces`, denied with `schematicManagement: false`, denied with the server flag off, and denied for anonymous callers. Built-in Schematics are read-only. A bundle export and import round-trip preserves the digest, and a tampered bundle is rejected. Remap candidates are returned. Hidden references show as `Hidden`.
  - Agent assembly: advise returns one advisory per link. Export ZIP contents match golden files for each target. A cycle, `maxIterations: 11`, or a placeholder is rejected with the right code. Handoff falls back when Jev is unavailable.
- **`services/depot/test/e2e`** (new files): OCI tag discovery honors `tagPattern` and `versionConstraints`. npm and PyPI discovery works. Yanking a Version referenced by a Schematic sets `references[].state=Yanked` and `valid=false`.
- **UI Playwright**: build an agent assembly with a retry loop and export it. Save it as a Schematic, export it, import it into a fresh namespace with a remap, and open it. Complete both tour tracks with the keyboard only. OCI and MCP detail pages render their snippets.

## Helm Chart

Values, all new except where noted:

| Value | Default | Purpose |
|---|---|---|
| `iac.*` | the merged contents of `module.*` and `provider.*` | The `iac` deployment's image, resources, flags, and scanning settings. `module.*` and `provider.*` are deprecated and still read in 0.13.0 |
| `oci.enabled` | `false` | Deploys the `oci` controller and turns on the server's `/v2/` routes |
| `oci.allowedRegistries` | `[]` | Upstream allow-list used by depot, oci, version, and server |
| `oci.helmRepository.enabled` | `true` when `oci.enabled` | Serves `index.yaml` |
| `oci.verify.defaultRequired` | `false` | The cluster-wide default for cosign `required` |
| `mcp.enabled` | `false` | Turns on `MCPServer` reconciliation (agent controller) and MCP sync (version controller) |
| `mcp.allowedRegistries.npm` / `mcp.allowedRegistries.pypi` | the public registries | Allow-lists for npm and PyPI |
| `mcp.pypi.defaultPlatforms` | `[manylinux_2_28_x86_64, manylinux_2_28_aarch64, macosx_11_0_arm64]` | Default wheel platforms |
| `version.image.mcpVariant` | `false` | Uses the `INCLUDE_MCP_RUNTIMES` image |
| `server.schematicManagement.enabled` | `true` | Allows Schematic writes under the Schematic write rule. Set it to `false` for a read-only catalog |
| `schematics.builtin.enabled` | `true` when Assembly Line is enabled | Installs the built-in Schematics into the release namespace |
| `server.jev.suggest.enabled` | `false` | Turns on suggest, advise, and handoff |
| `server.jev.suggest.secretNames` | `[]` | The Secret names that GroupBindings may reference. Used as RBAC `resourceNames` |
| `server.jev.defaultSecretRef` | unset | Optional fallback for callers without a binding |

RBAC:

- `iac`: the union of the old module and provider rules.
- `oci`: get, list, watch, and update on `ociartifacts`, plus their `status`. Create, update, delete, list, and watch on `versions`.
- `agent`: add `mcpservers` and `mcpservers/status`, plus get on `ociartifacts`.
- `depot`:
  - Create and update on `ociartifacts` and `mcpservers`.
  - Get, list, and watch on `schematics`, and update on `schematics/status`.
  - List and watch on the referenced kinds.
- `version`: get on `ociartifacts` and `mcpservers`, and get on `secrets` for dockerconfigjson pull credentials and cosign keys. This rule already exists for GitHub and Jev Secrets, so extend it.
- `server`:
  - Get, list, and watch on `ociartifacts`, `mcpservers`, and `schematics`.
  - Create, update, and delete on `schematics`, rendered when `server.schematicManagement.enabled` is true (the default).
  - Get on `secrets` with `resourceNames` set to `server.jev.suggest.secretNames`.

Templates: `schematics-builtin.yaml` renders one `Schematic` for each file in `files/schematics/`, with the built-in label.

Images: `iac-controller` and `oci-controller` replace `module-controller` and `provider-controller`. `version-controller` gets an `-mcp` variant.

## Waves and Lanes

| Wave | Lanes (can run in parallel) | Steps |
|---|---|---|
| 0 | Research | 1, 2 |
| 1 | Foundation (one lane, in order) | 3 → 4. Step 5 runs in parallel with 3 and 4. Step 6 runs after 4 and 5 |
| 2a | Libraries and dependencies | 7, 8, 10, 16, 19, 23 (19 can start in Wave 0) |
| 2b | OCI · MCP · Jev · Assembly · Schematic · CLI | 9, 11, 12, 13, 14 · 15, 17, 18 · 20, 21, 22 · 24, 25, 26, 27 · 29, 30, 31, 32 |
| 2c | Wiring | 28, 33 |
| 2d | UI | 34 and 35 in parallel, then 36, then 37 |
| 3 | Integration and tests | 38, then 39, 40, 41, and 42 in parallel, plus 43 and 44 |
| 4 | Review | 45 |

Critical path: 3 → 4 → 9/11 → 28 → 35 → 36 → 37 → 43 → 45.

## Planner decisions

All of these were confirmed by the user.

- **P1. Confirmed.** The `OCIArtifact` reconciler gets its own `oci` controller, so there is one controller per family: depot, iac, agent, oci, and version.
- **P2. Confirmed.** The `Schematic` reconciler runs in the `depot` controller, because that controller already reads every kind. No new deployment is needed.
- **P3. Confirmed.** kubelet and CI image pulls use Basic auth with a `client_id` and `client_secret` that the server exchanges with the IdP. A bearer token in the password field also works. Security Review still checks the implementation (Step 45).
- **P4. Resolved.** The merged controller is named `iac` (`services/iac`, image `ghcr.io/tonedefdev/opendepot/iac-controller`, values `iac.*`). This avoids confusion with Flux's `tofu-controller` (`flux-iac/tofu-controller`) and doesn't favor Terraform or OpenTofu.
- **P5. Confirmed.** npm packages with native addons and PyPI packages that only ship sdists are rejected in 0.13.0, so nothing is compiled during sync.
- **P6. Resolved.** Saving Schematics is on by default. Authenticated callers can write in their GroupBinding's own namespace without any extra setup. `schematicNamespaces` adds more namespaces. `schematicManagement: false` makes a binding read-only. `server.schematicManagement.enabled: false` turns writes off for the whole cluster. Anonymous callers never write.
- **P7. Confirmed.** Suggest, advise, and handoff all choose the Jev credential from the caller's token. The server validates the user's token, finds the GroupBinding the token matches, and uses that binding's `jevSecretRef`, which is new in this release (Step 20). The CLI sends only the user's OpenDepot token and never handles a Jev key. Each team's binding points at its own Secret, so each team pays for its own design-time and runtime Jev calls.

## Out of Scope (0.13.0)

- MCP runtime, proxy, secretRefs, and ESO (see the appendix).
- OCI push, `helm push`, and OpenDepot re-signing of mirrored artifacts. Verifying upstream cosign signatures is in scope.
- In-cluster image builds for `Source` MCP servers.
- Removing the deprecated `module.*` and `provider.*` values. They are removed in 0.14.0.
- Changes to the Module or Provider CRDs.
- Per-user Jev keys. In 0.13.0 the Jev credential is per group, through the GroupBinding's `jevSecretRef`. A later release can add a per-user key, used before the group key when it's set, without changing the API shape.

## Appendix: 0.14.0 MCP runtime and proxy (decided in planning; not built in 0.13.0)

- **`MCPServer.spec.deployment`**: `replicas` and `template corev1.PodTemplateSpec`. It inherits the Deployment pod template, with CRD descriptions removed so the CRD stays under the size limit.
  - The controller always sets the container image to the pinned digest. The user can't override it.
  - It enforces the "restricted" Pod Security Standard, turns off service account token automount, and creates a default-deny NetworkPolicy with egress set from `spec.egress`.
- **Npm and PyPI at runtime**: a fixed OpenDepot runtime image (Node or Python). An init container downloads the vendored archive from storage, checks the signature and hash, and runs the `opendepot-mcp.json` entrypoint. Nothing is fetched from npm or PyPI at runtime.
- **stdio servers**: an OpenDepot sidecar bridges stdio to streamable HTTP.
- **Secrets**: `spec.secretRefs[]` maps environment names to `secretKeyRef` values. CEL rejects inline `env[].value` for those names.
  - When ESO is installed and `mcp.externalSecrets.enabled` is true, `spec.externalSecrets[]` (`name`, `secretStoreRef`, `remoteRef`) makes the controller create `ExternalSecret` objects and wire their target Secrets in.
  - Secret values are never copied into status, events, or logs.
- **Tool pinning**: after a Deployment is ready, the controller calls `tools/list` and records a hash of each tool description. Jev assesses the descriptions for tool poisoning. If the hashes drift at runtime, the proxy blocks the server until it's reviewed.
- **Proxy**, in the server or a new `mcp-gateway` (to decide in 0.14.0):
  - Authenticates with OIDC.
  - Applies `MCPServerResources` and per-tool allow-lists from GroupBinding.
  - Writes an audit log of calls, without arguments by default.
  - Optionally has Jev screen tool responses for prompt injection.
- **CLI**: `opendepot mcp connect <ns>/<name>` is a local stdio-to-HTTPS bridge that refreshes the token. `apply` can write config that points to it instead of a local install.
- **Assemblies**: `calls` links can target a running MCPServer through the proxy, and `assembly route` passes along the per-link tool allow-list.

