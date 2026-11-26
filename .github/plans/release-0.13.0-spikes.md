# Release 0.13.0: Wave 0 spike results

Status: complete for Wave 0 Step 2. Nothing is implemented. This file records feasibility results that the dependent steps in `release-0.13.0.md` rely on. It does not change any production code, `go.mod`, `go.sum`, or `go.work`.

Scope and method:

- **Local spikes (this repo's environment, 2026-11-26):** items (c), (d), (e), (f), and (h). Throwaway modules, packages, and layouts lived in `/tmp` and were removed afterward.
- **External verification (2026-11-26):** items (a), (b), (d), (g), (h), and (i). Each finding was checked against the upstream source, release, or specification.
- **Toolchain context:** `go.work` declares `go 1.26.0`. Local Go measurements used the installed `go1.27.1` toolchain. Re-measure the Go dependency closure with the `go.work` toolchain in Step 7 or Step 11 before pinning a version.

Environment used for local spikes:

| Tool | Version |
|------|---------|
| trivy | 0.70.0 (Vulnerability DB schema 2, cached locally) |
| go | go1.27.1 darwin/arm64 (`go.work` declares 1.26.0) |
| node / npm | v26.0.0 / 11.12.1 |
| python3 / pip3 | 3.14.5 arm64 / pip 26.1.1 (bare `pip` is not on PATH; use `pip3`) |
| helm | v3.22.0 |
| copilot | 1.0.113 |
| oras | not installed (item a was not spiked locally) |

## Summary

| Item | Topic | Result | Changes a plan assumption? |
|------|-------|--------|----------------------------|
| (a) | `oras.land/oras-go/v2` | Use `v2.7.0`. Avoid `v3`. | Yes: pin v2, add SSRF and credential handling |
| (b) | Helm Go SDK and `helm pull oci://` | v3 and v4 both viable. Pick one explicitly. | Yes: the package path depends on the choice |
| (c) | Trivy offline OCI and Helm scanning | Works from an unpacked OCI directory with a warm cache. A tar of a layout is rejected. | Yes: `--input` takes a directory, not a tar |
| (d) | Sigstore public-key verification without Rekor | `cosign/v2` is the only compatible option with OCI `.sig` support. `sigstore-go` is smaller but incompatible here. | Yes: the library choice changes |
| (e) | npm vendoring and native addon detection | Works with `--package-lock-only` and `npm ci --omit=dev --ignore-scripts`. Detection must read lockfile metadata. | Yes: detection signals and scan scope |
| (f) | PyPI vendoring | Works with `--only-binary=:all:` plus `--platform` and `--python-version`. Sdists must never be fetched. | Yes: flags are mandatory |
| (g) | Delegation tool names and hook blocking | Copilot `task`, Claude `Agent` (alias `Task`). Both support deny. | Yes: matchers and Codex config path |
| (h) | Frontmatter key tables by platform | Local table plus external docs. Conflicts on `handoffs` and missing Copilot keys. | Yes: Step 23 delegation design |
| (i) | MCP `server.json` schema | Target `2025-12-11`. Do not target the draft. | Yes: schema version pinned |

## (a) `oras.land/oras-go/v2` (external)

- Latest v2 release is `v2.7.0` (2026-10-23). It declares `go 1.26.0`, matching `go.work`. License is Apache-2.0.
- ORAS `v3.0.0` (`github.com/oras-project/oras-go/v3`) reached GA on 2026-11-01 as a breaking redesign. **Stay on `v2.7.0`.**
- Confirmed APIs: `Repository.Resolve`, `Fetch`, `FetchReference`, `Tags`, `Referrers`, `CopyOptions.WithTargetPlatform`, `CopyOptions.MapRoot`, `auth.Client` with a custom `*http.Client`, and `CredentialFunc`.
- Redirect behavior: `auth.Client.send` keeps a custom `CheckRedirect`, strips `Authorization` on cross-origin redirects, and caps redirects at 10. OpenDepot must still supply its own `CheckRedirect` and dial-time IP validation to enforce the upstream allow-list (SSRF defense).
- `dockerconfigjson` has no constructor from bytes. Parse the `auths` map in memory and return it from a custom `CredentialFunc`. Do not write a temporary file.
- Not spiked locally. Verify with a fixture registry in Step 7.

## (b) Helm Go SDK and `helm pull oci://` (external)

- `helm.sh/helm/v3` `v3.22.1` (2026-10-15) and `helm.sh/helm/v4` `v4.3.2` (2026-11-13) are both maintained. Both are Apache-2.0 and both declare `go 1.26.0`.
- v3 packages: `helm.sh/helm/v3/pkg/chart/loader` (`Load`, `LoadFiles`) and `pkg/repo` (`IndexFile`, `NewIndexFile`, `Add`, `SortEntries`, `WriteFile`).
- v4 packages: `helm.sh/helm/v4/pkg/chart/v2/loader` and `pkg/repo/v1`. The APIs are the same, but the import paths are versioned.
- **Decision required:** choose v4 for the new `pkg/oci` code, or v3 to match the older Helm ecosystem. Pick one and do not mix them.
- OCI media types, verified in v4 `pkg/registry/constants.go`:
  - config: `application/vnd.cncf.helm.config.v1+json`
  - chart layer: `application/vnd.cncf.helm.chart.content.v1.tar+gzip`
  - provenance: `application/vnd.cncf.helm.chart.provenance.v1.prov`
- `helm pull oci://` is generally available from Helm 3.8.0 onward, and in all v4.x releases.

## (c) Trivy offline OCI and Helm scanning (local)

Layout fixture: a minimal OCI image layout (`oci-layout`, `index.json`, blobs) with one Alpine 3.13 layer that contains an old `openssl 1.1.1k-r0` entry. Built with a short Python script.

| # | Command (abridged) | Environment | Exit | Result |
|---|--------------------|-------------|------|--------|
| C1 | `trivy image --input <layout-dir> --skip-db-update --skip-java-db-update --offline-scan --format json -o out.json --skip-version-check -q` | `HTTP(S)_PROXY=http://127.0.0.1:9` (dead proxy) | 0 | Works offline. 4 OS vulnerabilities found. |
| C2 | Same flags with `--input <layout>.tar` (tar of the layout) | dead proxy | 1 | Rejected. Trivy wants an unpacked directory or a docker-archive tar (docker-archive not tested). |
| C3 | `trivy image --input <layout-dir>` (no skip flags) | dead proxy | 1 | Tries to download the DB from `mirror.gcr.io` and fails. |
| C4 | Same as C1 with an empty `--cache-dir` | dead proxy | 1 | "`--skip-db-update` cannot be specified on the first run". A seeded cache is required. |
| C5 | `trivy config --skip-check-update --skip-version-check -f json -o e.json -q chart/opendepot` | dead proxy | 0 | 26 targets (17 `helm`, 9 `kubernetes`). 11 misconfigurations. Helm detection works on raw templates. |
| C6 | Minimal chart with an unhardened Deployment, same flags as C5 | dead proxy | 0 | `templates/deploy.yaml` detected as `helm`. 18 KSV findings. |
| C7 | `trivy config ... --severity HIGH,CRITICAL --exit-code 1 chart/opendepot` | none | 0 | Exit 0. Trivy's exit code is 0 unless `--exit-code` fires on a matching finding. |

Conclusions:

- Offline image scanning works from an unpacked OCI layout directory, with `--skip-db-update --skip-java-db-update --offline-scan` and a seeded cache. Tar input is not accepted.
- The DB must be seeded beforehand. The Trivy DB seed job is out of scope for Wave 0 and is not changed here.
- `trivy config` works fully offline with `--skip-check-update`.

## (d) Sigstore: Rekor-free public-key verification and OCI `.sig` support (local and external)

Local footprint measurement, `go mod tidy` and `go list -deps` in throwaway modules:

| Metric | `sigstore-go` v1.4.1 | `cosign/v2` v2.6.5 |
|--------|----------------------|--------------------|
| `go.mod` requirements (direct + indirect) | 68 | 100 |
| Non-main modules in build closure | 68 | 98 |
| Non-stdlib packages in build closure | 327 | 448 |
| `go.sum` lines | 349 | 568 |
| License | Apache-2.0 | Apache-2.0 |

External verification, which changes the decision:

- `sigstore-go` v1.4.1 declares **`go 1.27.0`**, which is incompatible with `go.work`'s `go 1.26.0`. Its README also **explicitly omits container image and OCI `.sig` verification**. It is smaller, but it cannot verify the artifacts OpenDepot needs.
- `github.com/sigstore/cosign/v3` v3.1.6 declares `go 1.27.0`. It is also incompatible.
- `github.com/sigstore/cosign/v2` v2.6.5 declares `go 1.25.0`, which is compatible. It understands cosign OCI `.sig` tags and OCI 1.1 referrers. Use `CheckOpts{SigVerifier, IgnoreTlog: true}` for public-key verification without Rekor.
- A leaner alternative is `github.com/sigstore/sigstore` v1.11.4 (`pkg/signature`, Apache-2.0) with `oras-go` to fetch the `.sig` tag or referrers and verify the simple-signing payload. This needs about 100 lines of custom code and no Rekor.

API evidence (not executed):

- `sigstore-go` has `root.NewTrustedPublicKeyMaterial` and `verify.WithTransparencyLog(threshold)`.
- `cosign/v2` has `CheckOpts.IgnoreTlog`.

**Recommendation:** use `cosign/v2` with `IgnoreTlog: true`. It is the only option here that covers OCI `.sig` verification under `go 1.26`. Keep the `sigstore` + `oras-go` custom path as the fallback if `cosign/v2`'s closure is judged too large in Step 11.

**Runtime verification is still required in Step 11.** Neither footprint nor API inspection proves that a key-only `.sig` verification passes.

## (e) npm vendoring and native addon detection (local)

Test packages: a pure-JS project (`ms@2.1.3`) and a native-capable project (`better-sqlite3@11.10.0`, `esbuild@0.25.0`).

| # | Command | Exit | Result |
|---|---------|------|--------|
| E1 | `npm install --package-lock-only --ignore-scripts` (pure) | 0 | 505-byte lockfile |
| E2 | `npm ci --omit=dev --ignore-scripts` (pure) | 0 | Installs; scripts not run |
| E3 | `npm ci --omit=dev --ignore-scripts --offline` with an empty cache | 1 | `ENOTCACHED` |
| E4 | Same with a warmed cache | 0 | Installs offline |
| E5 | `--package-lock-only --ignore-scripts` (native project) | 0 | 65 locked packages |
| E6 | `npm ci --omit=dev --ignore-scripts` (native project) | 0 | No `.node` files produced (scripts did not run) |

Detection signals, validated against the lockfile and the installed tree:

| Signal | Source | Caught `better-sqlite3` | Caught `esbuild` | Notes |
|--------|--------|-------------------------|------------------|-------|
| `binding.gyp` in the package | tarball / installed tree | Yes | No | Reliable for source-built addons |
| `hasInstallScript: true` | `package-lock.json` | Yes | Yes | Available from the lockfile alone |
| `gypfile: true` | `package.json` | Not set in this package | n/a | Only some packages set it |
| Build-dependency names: `bindings`, `prebuild-install`, `node-gyp`, `node-gyp-build`, `node-addon-api`, `node-pre-gyp`, `@mapbox/node-pre-gyp` | `dependencies` in `package.json` | Yes (`bindings`, `prebuild-install`) | No | Strong hint |
| `*.node` files | installed tree or tarball (`prebuilds/`) | No (none before a build) | No | Catches prebuilt binaries only |
| `os` / `cpu` on optional packages | lockfile | n/a | Yes (25 `@esbuild/*`) | Platform-specific native executables, not addons |

Key findings:

- `npm ci` installs only the platform-matching optional packages. On this host, 24 platform-only packages were absent from `node_modules`. A detector that walks the installed tree misses them.
- **Scan the lockfile and the downloaded tarballs for every platform**, not the installed tree.
- Always pass `--ignore-scripts`. Without it, `npm ci` runs install scripts such as `prebuild-install` and `node install.js`.

## (f) PyPI vendoring (local)

| # | Command (abridged) | Exit | Result |
|---|--------------------|------|--------|
| F1 | `pip3 download --only-binary=:all: --platform manylinux_2_28_x86_64 --python-version 3.12 --no-deps pyyaml` | 0 | Picked `cp312` manylinux wheel. The platform tag expands to `manylinux_2_17` and `manylinux_2_28`. |
| F2 | Same flags with dependencies (`requests`) | 0 | 5 wheels: `certifi`, `charset_normalizer`, `idna`, `requests`, `urllib3` |
| F3 | F1 plus `--implementation cp --abi cp312` | 0 | Same wheel |
| F4 | `pip3 download --only-binary=:all: --no-deps pyyaml` (no platform flags) | 0 | Returned a host-only `cp314` macOS wheel. The platform flags are required. |
| F5 | sdist-only package (`w0sdistonly-0.1.0.tar.gz`, local flat index) with `--only-binary=:all:` | 1 | "Could not find a version… (from versions: none)". This is the "no wheel" signal. |
| F6 | sdist-only package without `--only-binary` | 1 | pip ran the sdist's `setup.py` build path and failed at build dependencies. **The sdist was executed.** |
| F7 | `--platform` without `--only-binary` | 1 | Same sdist build path. `--platform` does not prevent sdist builds. |

Conclusions:

- Always pass `--only-binary=:all:`, `--platform`, and `--python-version`. Add `--implementation cp --abi cp312` to pin the ABI.
- Exit 1 with "from versions: none" means no compatible wheel. Fail the Version.
- **Never fetch an sdist.** F6 and F7 show that pip runs build backends (arbitrary code) for sdists.

## (g) Delegation tool names and hook blocking (external)

- **GitHub Copilot (VS Code and CLI):** `preToolUse` in `.github/hooks/*.json`, or a `hooks` key in `.agent.md` frontmatter, can block a call with `{"permissionDecision":"deny","permissionDecisionReason":"..."}` or a non-zero exit code (including exit 2). Fail-closed on crash, fail-open on timeout.
  - Native delegation tool: **`task`**. `Agent` is a Claude-compatible alias and `Task` is also accepted. `runSubagent` is local-harness only and is deprecated, with removal in February 2027.
  - Recommended matcher: `^(task|runSubagent|Agent|Task)$`.
- **Claude Code:** `PreToolUse` can block with `hookSpecificOutput.permissionDecision: "deny"` or exit code 2.
  - Delegation tool: **`Agent`**. It was renamed from `Task` in Claude Code 2.1.63. `Task` is still accepted as an alias.
  - Recommended matcher: `Agent|Task`. `TaskCreate` and `TaskUpdate` are task-list tools, not delegation.
- **OpenAI Codex CLI:** the project-level MCP config is **`.codex/config.toml`** (`[mcp_servers.<name>]` tables). It is **not** `.mcp.json`. Hooks live in `.codex/hooks.json` (`PreToolUse` and others). Two items remain open: the Codex delegation tool name, and whether project trust is required before `.codex/config.toml` loads.

Repo consistency check: `docs/guides/agents-hooks.md` documents Copilot's hook as writing a JSON deny to stdout, because Copilot does not show a non-zero exit's stderr to the model. That is compatible with the external finding. Keep the JSON deny method. The exit 2 path is not tested in this repo.

## (h) Frontmatter key tables by platform (local and external)

Local: `pkg/agentspec/agentspec.go` (`platformSkillKeys`, `platformAgentKeys`), matching `docs/guides/agents-checks.md`.

| Platform | Skill keys beyond base | Agent keys beyond base |
|----------|------------------------|------------------------|
| `agentskills` | none | none |
| `claude` | `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `disallowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, `shell` | `disallowedTools`, `permissionMode`, `mcpServers`, `hooks`, `maxTurns`, `skills`, `initialPrompt`, `memory`, `effort`, `background`, `omitClaudeMd`, `isolation`, `color` |
| `copilot` | `argument-hint` | `target`, `disable-model-invocation`, `user-invocable`, `mcp-servers`, `argument-hint`, `handoffs` |
| `codex` | none | none (TOML agents use a separate key set: `name`, `description`, `developer_instructions`, `model`, `model_reasoning_effort`, `model_reasoning_summary`, `model_verbosity`, `features`, `skills`) |
| `opencode` | n/a | `name`, `description`, `mode`, `model`, `temperature`, `top_p`, `steps`, `disable`, `prompt`, `tools`, `permission`, `hidden`, `color` |
| `pi` | `disable-model-invocation` | none |

- Skill base keys: `name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools`. Agents add `tools` and `model`.
- `agents-md` has no key table. It validates against the declared platform, or the union of all platforms when unset.
- No table contains `delegates` or `agents`.

External, from upstream platform docs (verified 2026-11-26):

- **Copilot harness** accepts `name`, `description`, `target`, `tools`, `model`, `hooks`, `user-invocable`, `disable-model-invocation`, `skills`, and `mcp-servers`. It does **not** accept `handoffs` or `agents` frontmatter. Those belong to the deprecated local harness (removal February 2027).
- **Claude Code** (`.claude/agents/*.md`) has no native `handoffs` key. Subagent restriction uses `Agent(a, b)` inside `tools:`. Other keys include `disallowedTools`, `permissionMode`, `mcpServers`, `hooks`, `skills`, `maxTurns`, and `model`.

Conflicts between local and external:

1. **`handoffs` for Copilot.** `pkg/agentspec` accepts it as a known Copilot agent key. The Copilot harness does not accept it. The validator will pass it, but generated output must not depend on it.
2. **Copilot `skills` and `hooks`.** Both are valid Copilot harness keys, but `pkg/agentspec`'s Copilot table does not list them. An agent that sets them today gets a LOW unknown-key warning.

Resolution proposal for Step 23: stop generating `handoffs` for Copilot. Render handoffs and delegation allowlists as a generated `Delegation` body section for Copilot, Claude, and `agents-md`. Use `Agent(...)` in Claude `tools:` where applicable. Add `skills` and `hooks` to the Copilot table only if the Step 23 renderers need them. Removing `handoffs` from the table would change validation of existing entries, so it is a separate decision.

## (i) MCP `server.json` schema (external)

- Target the latest stable dated schema: **`2025-12-11`**, at `https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json`. Do not target the unreleased `draft` schema.
- Required top-level fields:
  - `name`: pattern `^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`, max 200 characters
  - `description`
  - `version`
- Other top-level fields: `$schema`, `_meta`, `icons`, `packages`, `remotes`, `repository`, `title`, `websiteUrl`.
- `packages[]` required fields:
  - `registryType`: `npm`, `pypi`, `oci`, `nuget`, or `mcpb`
  - `identifier`
  - `transport`: `StdioTransport`, `StreamableHttpTransport`, or `SseTransport`

## Plan impacts (updates needed in `release-0.13.0.md` before Wave 1 and Wave 2)

These are the assumptions the spikes changed or confirmed. Edit `release-0.13.0.md` in a later commit after the Orchestrator reviews this file. Do not edit it inside Wave 0.

1. **Step 2 (a) oras:** pin `oras.land/oras-go/v2` `v2.7.0`. Do not use v3. Step 7 and Step 11 must add `CheckRedirect` and dial-time IP validation, and must parse `dockerconfigjson` in memory through a `CredentialFunc`.
2. **Step 2 (b) Helm SDK:** choose v3 or v4 once and record it in Step 7's `pkg/oci` design. Use the OCI media types above. Require Helm 3.8.0 or later for `helm pull oci://` compatibility.
3. **Step 2 (c) and Step 11 (Trivy):** `trivy image --input` takes an unpacked OCI layout directory, not a tar. Offline scans require a seeded cache and `--skip-db-update --skip-java-db-update --offline-scan`. The DB seed job stays out of scope.
4. **Step 2 (d) and Step 11 (Sigstore):** replace the earlier "smallest tree" choice. Use `github.com/sigstore/cosign/v2` with `IgnoreTlog: true`, because `sigstore-go` declares `go 1.27.0` and omits OCI `.sig` verification. Keep `sigstore` + `oras-go` as the fallback. Add a runtime key-only verification test in Step 11. Re-measure the closure under `go 1.26`.
5. **Step 2 (e) and Step 15 (npm):** scan lockfile metadata and tarballs for every platform, not the installed tree. Native signals: `binding.gyp`, `hasInstallScript`, native build dependencies, `*.node` (prebuilt only), and platform-constrained optional packages. Always use `--ignore-scripts`. Proposed trigger for the `mcp-native-addon` finding (to confirm in Step 15): `binding.gyp`, `hasInstallScript` on a native build dependency, or `*.node`.
6. **Step 2 (f) and Step 15 (PyPI):** every fetch passes `--only-binary=:all:`, `--platform`, `--python-version`, and `--abi`/`--implementation`. Never fetch sdists. Treat "from versions: none" as a failed Version.
7. **Step 2 (g) and Step 32 (hook matchers):** Copilot delegation matcher `^(task|runSubagent|Agent|Task)$`. Claude matcher `Agent|Task`. Both platforms can block with a deny decision. Keep Copilot's JSON-deny method, which the repo documents. Re-check Codex delegation naming before Step 16 and Step 32.
8. **Step 2 (g) and Step 16 (Codex MCP):** the Codex project config path is `.codex/config.toml`, not `.mcp.json`. Confirm project trust before Step 16 writes that file.
9. **Step 2 (h) and Step 23 (delegation):** stop generating Copilot `handoffs`. Render a generated `Delegation` section for Copilot, Claude, and `agents-md`. Use `Agent(...)` in Claude `tools:`. Decide separately whether to remove `handoffs` from the `pkg/agentspec` Copilot table, and whether to add `skills` and `hooks` to it.
10. **Step 2 (i) and Step 16 (MCP):** `server.json` targets schema `2025-12-11`. Validate the required `name`, `description`, and `version`, and each `packages[]` entry's `registryType`, `identifier`, and `transport`.
11. **Step 1 (not part of Step 2):** complete. The live 2×2 confirmation trials (gpt-6-luna, 3 interleaved rounds, 12 scored trials) are recorded in `.github/plans/jev-skill-suggest-design.md` under "Canary diagnosis". Only the refined skill text with the Zeta prompt passed (3/3). The Find prompt failed in every cell, and the v1 skill text failed because its frontmatter is invalid YAML. The Step 31 fix is recorded in the release plan.

## Open items not verified in Wave 0

- Item (a): `oras-go` v2.7.0 was not built or run locally. Verify with a fixture registry in Step 7.
- Item (b): Helm v3 and v4 were not compiled locally. The media types and API names are from upstream source.
- Item (d): `cosign/v2` key-only verification was not executed. Verify in Step 11.
- Item (g): Codex delegation tool name and project-trust behavior are still open.
- Item (g): exit-code 2 hook blocking was not tested in this repo. The repo's tested path is the JSON deny.
- Item (i): `server.json` was not validated against the schema. Validate with a fixture in Step 16.
