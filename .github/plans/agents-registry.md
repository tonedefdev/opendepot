# Implementation Plan: Agent Registry — Skills, Agents, and the `opendepot` CLI

## Summary

Bring everything OpenDepot does for modules and providers to agent skills and
agents. Users point a `Depot` (or a `Skill` / `Agent` CR
directly) at a GitHub repo path; a dedicated agent-controller creates `Version`
CRs; the version-controller fetches, validates, repackages, stores, and scans
them with `trivy fs` plus native frontmatter validation and bundled Expr rules.
The server exposes an `agents.v1` registry API that mirrors the Terraform
provider registry protocol: a version list, per-version download metadata, and a
GPG-signed `SHA256SUMS` that is signed once at sync. Versions can also be
withdrawn ("yanked") without being deleted, so existing lock files still resolve
while new installs skip them. A new `opendepot` CLI resolves Terraform-style
constraints (`version = "~> 1.2"`), verifies signature, archive hash, and
extracted-tree hash against `opendepot.lock.hcl`, and installs into Copilot,
Claude, AGENTS.md, or custom target directories. It replaces `npx skills` for
OpenDepot-managed skills and definitions.

Trivy, agentspec, and Expr rule scans always run on every Version. When the cluster
enables the Jev feature (`scanning.jev.enabled`) and a source provides a
`jevSecretRef`, the version controller also asks TypeSafe's Jev model (System One
API, `jev-latest`) safety questions about the skill or agent: whether it is safe to
use, whether it is susceptible to prompt injection, whether it could exfiltrate
data, and how much harm it could cause. Scores are stored on the Version, shown in
the UI and CLI, and can be gated by optional thresholds that set your risk appetite.
Jev never replaces or overrides the deterministic scans.

## Decisions

- Two CRDs (`Skill`, `Agent`) reconciled by one agent-controller.
- Monorepo-aware sources: `repoOwner` + repo + `path` + optional `tagPrefix`. GitHub only.
- Publishing = Git tags + Depot polling (same as modules). No push/upload API.
- Discovery key `agents.v1` → `/opendepot/agents/v1/`.
- Consumer config `opendepot.hcl` (an `opendepot` block plus one `agent_skill` or `agent` block per entry) and lock `opendepot.lock.hcl` (HCL with the same block names as the config). The lock format is OpenDepot's own; it does not need to match Terraform's `.terraform.lock.hcl` byte-for-byte.
- CLI binary `opendepot`, new `cli/` Go module in this repo.
- Install targets: `copilot`, `claude`, `agents-md`, custom `path`.
- Scanning: `trivy fs --scanners vuln,secret,misconfig` + native frontmatter validation + bundled Expr rules. Trivy always runs on every Version; Jev is additive.
- Bundled checks use Expr, the expression engine already used by `GroupBinding`, instead of Rego. Trivy custom checks would require Rego, so the Trivy invocation drops `--config-check` and `--check-namespaces`.
- Server only resolves synced (scan-passing) versions.
- Signing: the version controller signs `SHA256SUMS` once at sync with the provider GPG key material (`version.gpg.secretName`) and stores the signature on the Version. The server serves the stored bytes, so the private key is never needed on the read path. A Version is never published unsigned.
- Lock records `zh:` (archive SHA-256), `h1:` (directory hash of the extracted tree), the signing key fingerprint, `signature_hash` (`sha256:` over the binary `SHA256SUMS.sig`, the GPG signature the install was verified against), and an `installed` hash per target. First install pins the key (TOFU, as in Terraform). A `signing_key` in `opendepot.hcl` pins it explicitly, and a mismatch never prompts.
- Constraint operators match Terraform (`=`, `!=`, `>`, `>=`, `<`, `<=`, `~>`), parsed with `github.com/hashicorp/go-version`, the library Terraform uses. `init` honors the lock; `init -upgrade` re-resolves within constraints.
- Yanking: `Version.spec.yanked` keeps a Version listed with `yanked: true`. Constraint resolution skips it, and `init` refuses it unless `-allow-yanked` is passed. `verify` flags installed yanked entries.
- Protocol version: download metadata carries `protocols: ["1.0"]`. Clients refuse unknown major versions.
- The signed, pinned `opendepot` CLI is the only install path in v1. `npx skills` and `.well-known/skills` compatibility are out of scope, because that path has no lock, no version pins, and no signature check. Offering it would weaken the guarantees this registry exists to provide. A read-only index can be reconsidered once the CLI is stable. Jev output is advisory, and no flag skips signature or hash checks.
- AGENTS.md edits confined to per-entry `<!-- opendepot:begin <source> -->` / `<!-- opendepot:end <source> -->` markers.
- Jev is off unless the cluster enables it (`scanning.jev.enabled`, default `false`). When enabled, a source opts in with `jevSecretRef` (a Secret key holding the Jev token). Without either, no Jev call is made and no content leaves the cluster.
- The token is read from the Secret at reconcile time. It is never copied into CR or Version status, events, logs, error messages, or the lockfile.
- Jev runs once per Version, after the Trivy, agentspec, and Expr rule scans pass. All questions go in one `system_one` request so they are evaluated over the same state.
- Gating uses Jev probabilities (`safe`, `prompt_injection`, `risk`), never `confidence`. TypeSafe defines Choice/Score confidence as distribution concentration, not correctness, so low confidence sets `needsReview` only.
- Jev can add a block but never clears a scan finding.
- `JevPolicy` thresholds are the user's risk appetite. Unset thresholds mean informational only.
- Privacy: when Jev runs, skill and agent content goes to TypeSafe, a third party. The UI, CLI, and docs say so, label results as a TypeSafe assessment, and state that scores are probabilities, not a certification.
- Excluded: non-GitHub VCS, CLI `publish`/`search`, global (home dir) installs, per-namespace signing keys, Jev-based ScanPolicy exemptions (v1), Jev enabled by default, a transparency log (candidate follow-up), `trustedKeys[]` (replaced by per-entry `signing_key`), and the `update` / `outdated` commands (replaced by `init -upgrade`).

## Protocol: agents.v1

Modeled on the Terraform provider registry protocol and the code already in
[providers.go](../../services/server/providers.go) and [discovery.go](../../services/server/discovery.go).

| Terraform provider registry | `agents.v1` | Notes |
|---|---|---|
| `providers.v1` in `/.well-known/terraform.json` | `agents.v1: /opendepot/agents/v1/` | Terraform and OpenTofu ignore unknown discovery keys |
| `GET .../versions` | `GET .../{kind}/{name}/versions` | Synced Versions only. Yanking is an OpenDepot addition, not part of Terraform's protocol: yanked Versions stay listed with `yanked: true` |
| `GET .../{version}/download/{os}/{arch}` | `GET .../{version}/download` | Skills and definitions are platform-independent, so no os/arch |
| `shasum`, `shasums_url`, `shasums_signature_url`, `signing_keys` | same fields | Signed once at sync (15a). The server never signs on read |
| `protocols: ["5.0"]` | `protocols: ["1.0"]` | Clients refuse an unknown major version |
| Terraform lock file (`.terraform.lock.hcl`) | `opendepot.lock.hcl` | Same block-per-entry shape. Holds `zh:`, `h1:`, and `signature_hash`, see step 23 |
| Per-entry `required_providers` blocks | `agent_skill` / `agent` blocks | In `opendepot.hcl`, see step 22. The `required_` prefix dropped because these are install targets, not mandatory dependencies |
| Version constraints (`~> 1.2`) | Same operators | Parsed with `github.com/hashicorp/go-version` |

Trust model: only the signed `SHA256SUMS` line authenticates content. The
`download_url` can be a presigned or CDN URL without weakening trust, because the
archive hash is checked against the signed line. First use pins the signing key
(TOFU), with the same first-use weakness as Terraform. A `signing_key` pin in
`opendepot.hcl` removes that weakness in CI.

Scale: archives are immutable and cacheable, the version list is small, and
signing cost is paid once per Version at sync instead of on every request.

## Affected Services

- **api/v1alpha1** — new CRDs, shared source config, Version/Depot/GroupBinding/ScanPolicy fields
- **pkg/agentspec** (new) — frontmatter parsing + validation
- **pkg/archive** (new) — safe extraction + deterministic repack
- **pkg/signing** (new) — GPG key loading and `SHA256SUMS` signing, shared by providers and agents
- **pkg/github** — tag listing, generic archive fetch
- **pkg/jev** (new) — TypeSafe System One client, default question set, gate evaluation
- **pkg/utils** — latest/trim/name helpers for new kinds
- **services/agent** (new) — Skill + Agent reconcilers
- **services/depot** — skill/agent config loops
- **services/version** — agent fetch/store/scan path, Expr rules, policy scan type
- **services/server** — agents.v1 API, auth, browse, policy
- **cli** (new) — `opendepot` CLI
- **services/ui** — browse pages, sidebar, security policy kinds
- **chart/opendepot**, **Tiltfile**, **Makefile**, **go.work**, **docs**

## Phase 1: API and Shared Packages (blocks all)

1. [api/v1alpha1/types.go](../../api/v1alpha1/types.go):
   - `AgentSourceConfig`: `Name`, `RepoOwner`, `RepoUrl`, `Path`, `TagPrefix`, `GithubClientConfig`, `StorageConfig`, `Immutable`, `VersionConstraints`, `VersionHistoryLimit`, `JevSecretRef` (`corev1.SecretKeySelector`, default key `jevToken`), `JevPolicy` (optional `minSafeProbability`, `maxInjectionProbability`, `maxExfiltrationProbability`, `maxDestructiveProbability`, `maxHiddenInstructionsProbability`, `maxScopeMismatchProbability`, `maxRemoteExecutionProbability`, `maxRiskScore`, `minConfidence`; all unset = informational only).
   - `Skill` / `Agent` with spec/status mirroring `ModuleSpec`/`ModuleStatus` (`Versions`, `ForceSync`, `LatestVersion`, `VersionRefs`, `Synced`, `SyncStatus`).
   - Constants `OpenDepotSkill`, `OpenDepotAgent`; register in `init()`.
2. `VersionSpec.AgentSourceRef *AgentSourceConfig`; `VersionSpec.Yanked bool` (stays listed, skipped by resolution); `VersionStatus.AgentMetadata` (name, description, tools, model); `VersionStatus.ShaSums` and `ShaSumsSignature` (signed once at sync, see 15a); `VersionStatus.SigningKeyFingerprint`; `VersionStatus.JevAssessment` (see 13e).
3. `DepotSpec.SkillConfigs` / `AgentConfigs`; `DepotStatus.Skills` / `Agents`.
4. `GroupBindingSpec` and `SecurityGroupBindingSpec`: `SkillResources`, `AgentResources` (glob patterns).
5. `ScanPolicyTargetRef.Kind` enum adds `Skill;Agent`; `ScanExemption.ScanTypes` enum adds `agent`.
6. Regenerate deepcopy + CRDs; sync into [chart/opendepot/crds](../../chart/opendepot/crds).
7. *(parallel)* `pkg/agentspec`: parse SKILL.md / agent `.md` YAML frontmatter; validate name format/length and that a skill name matches its directory (agent `name` is display metadata: non-empty and within length, not matched to the file name), required description, known `tools`/`model` keys. Emit `SecurityFinding`s with IDs `opendepot-agentspec-NNN`.
8. *(parallel)* `pkg/archive`: move `extractArchiveToDir` / `extractZipEntry` / `extractTarEntry` out of [scanner.go](../../services/version/internal/controller/scanner.go); add symlink rejection and a deterministic tar.gz writer (sorted entries, zeroed mtime/uid/gid) for stable checksums.
9. *(parallel)* [pkg/github/github.go](../../pkg/github/github.go): `ListMatchingTags(owner, repo, tagPrefix, constraints)` paginating `ListTags` and stripping the prefix; generalize `GetArchiveRequest` to `(owner, repo, ref)`.
9b. *(parallel)* `pkg/signing` (new): load the GPG private key from a mounted Secret and expose `SignSHA256SUMS(sums []byte) (sig []byte, fingerprint string, err error)` (`openpgp.DetachSign`) and `PublicKeyArmor()`. Moved out of [providers.go](../../services/server/providers.go) so providers and agents share one implementation. Tested with [pkg/testutils/gpg.go](../../pkg/testutils/gpg.go).

## Phase 2: Controllers (depends on Phase 1)

10. `services/agent` scaffolded like [services/module](../../services/module):
    - `SkillReconciler` + `AgentReconciler` sharing an internal helper modeled on `ModuleReconciler.Reconcile` (create/update Versions, history trim, `ReconcileVersionRemovals`, `ForceSync` reset).
    - Labels `opendepot.defdev.io/skill` / `opendepot.defdev.io/agent`.
    - Version names `skill-{name}-{ver}` / `agent-{name}-{ver}` to avoid module collisions.
    - Extend `utils.GetLatestVersion`, `VersionsToKeep`, `GetName` for the new kinds.
11. [opendepot_depot_controller.go](../../services/depot/internal/controller/opendepot_depot_controller.go): skill/agent config loops using `ListMatchingTags` with `GlobalConfig` storage/GitHub fallbacks.
12. [opendepot_versions_controller.go](../../services/version/internal/controller/opendepot_versions_controller.go):
    - New type cases, `prepareAgentVersion`, `fetchAgentArchive`: download tarball at `{tagPrefix}{version}` (v/no-v fallback), extract, keep only `Path`, validate via `agentspec`, deterministic repack.
    - Reuse module fast path and `Immutable` checksum guard.
    - SKILL.md / agent `.md` into the existing readme ConfigMap flow; populate `AgentMetadata`.
    - `getVersionStorageConfig` / `getVersionName` / `getVersionFilePath` handle `AgentSourceRef`; storage names `skill-{name}` / `agent-{name}` (Azure container names reject `/`).
    - Dual-config guard covers all three refs; RBAC markers for `skills` / `agents`.

## Phase 3: Scanning (depends on 12)

13. `runAgentScan` in [scanner.go](../../services/version/internal/controller/scanner.go):
    - Write sidecar `.opendepot/agent.json` (frontmatter, body, file list).
    - `trivy fs --scanners vuln,secret,misconfig`.
    - Merge `agentspec` findings; `policy.Apply(..., policy.ScanTypeAgent)`; persist as `SourceScan` via existing `persistScanViolation` / `reconcileStoredScanPolicy`.
14. `services/version/checks/agents/rules.yaml` (new), embedded with `go:embed` and compiled at startup with `expr.Compile(src, expr.Env(AgentRuleEnv{}), expr.AsBool())`, the same pattern as the GroupBinding evaluator in [auth.go](../../services/server/auth.go). A compile error fails startup. Each rule is a deny expression: `true` is a finding. `AgentRuleEnv` fields: `Name`, `Description`, `Tools []string`, `Body string`, `Scripts string` (bundled shell, Python, and JS file contents), `Domains []string` (URLs found in `Body` and `Scripts`), and `AllowedDomains []string` (from `scanning.agentAllowedDomains`). Built-in rules: wildcard tool grants (`"*" in Tools`), unscoped shell or terminal tools, `curl|sh`-style remote exec in `Body` or `Scripts`, fetch plus write tool combos, missing description, `Domains` outside `AllowedDomains` (disabled when the allowlist is empty), and unpinned package install commands (`npx`, `uvx`, `pip install`, `npm install` with no version). v1 supports built-in rules only. Add `github.com/expr-lang/expr` to [services/version/go.mod](../../services/version/go.mod); it is only an indirect dependency today.
15. [policy.go](../../services/version/internal/policy/policy.go): `ScanTypeAgent`; `resourceName` handles new types/labels. Controller flag `--scan-agents`.
15a. Signing at sync (last step, after the Trivy, Expr, and Jev gates pass): the version controller builds `SHA256SUMS` as `<sha256>  <filename>` for the stored archive and signs it with `pkg/signing`, using the key mounted from `version.gpg.secretName`. The bytes and signature go on `VersionStatus.ShaSums` and `ShaSumsSignature`, and the fingerprint on `SigningKeyFingerprint`. If the key is unavailable, the Version stays unsynced with reason `SigningUnavailable` and retries. A Version is never published unsigned.

## Phase 3b: Jev Assessment (depends on 12–13; parallel with 14–15)

13a. `pkg/jev` (new): thin client for `POST {endpoint}` (default `https://api.typesafe.ai/v1/systemone`, overridable via `--jev-endpoint`) with `model: "jev-latest"`. The token is read from the Secret per reconcile, sent in the auth header, and never logged. Errors are redacted. 30s timeout; up to 3 retries on 429/5xx. Verified 2026-10-08 with a live test call: `Authorization: Bearer <token>` returns 200 (unauthenticated returns 403). Response: `{"model": "jev-1.13.0", "answers": {"<id>": {"type": "noul", "noul": 0.99}}, "usage": {...}}`. `noul` answers carry `noul`. `score` answers carry `score` (probability-weighted level index), `legend`, `probabilities`, and `confidence`. `choice` answers carry `choice`, `probabilities`, and `confidence`.

13b. Default questions. All are sent in one request over the same `state` and are evaluated in parallel. Noul values are the probability that the answer is yes. Question IDs are local keys and are not sent to the model.
    - `safe` (noul): "Is this {skill|agent} safe to use?" → `safeProbability`.
    - `prompt_injection` (noul): "Is this {skill|agent} susceptible to prompt injection?" Criteria `true`: instructions an AI model would follow from untrusted input such as fetched pages, issues, or files. → `injectionProbability`.
    - `data_exfiltration` (noul): "Does this {skill|agent} direct the agent to send data, files, or credentials to an external destination?" → `exfiltrationProbability`.
    - `destructive_actions` (noul): "Does this {skill|agent} direct the agent to take destructive or irreversible actions (deleting data, force-pushing, changing production systems) without asking the user first?" → `destructiveProbability`.
    - `hidden_instructions` (noul): "Does this {skill|agent} contain hidden or obfuscated instructions, such as encoded or invisible text, or instructions to conceal actions from the user?" → `hiddenInstructionsProbability`.
    - `scope_mismatch` (noul): "Does this {skill|agent} ask the agent to do things beyond what its description says it does?" → `scopeMismatchProbability`.
    - `remote_execution` (noul): "Does this {skill|agent} direct the agent to download and run remote code or scripts, or to install unpinned dependencies?" → `remoteExecutionProbability`.
    - `risk` (score type, criteria ordered `Minimal`, `Low`, `Moderate`, `High`, `Critical` = indexes 0–4): "How much harm could this {skill|agent} cause if an AI agent follows it?" → `riskScore` = response `score` (0–4, probability-weighted), `riskLevel` = the highest-probability level name from `legend`, `riskConfidence` = response `confidence`.
    `state` is the parsed frontmatter, body, and bundled file list from `.opendepot/agent.json`, truncated at 64 KiB. Files flagged by the Trivy secret scan are left out of `state`. A change to the question set applies to Versions evaluated after the change; existing results are not re-run.

13c. Gating. Each threshold is separate and optional. Unset means informational only. A Version is blocked if any configured threshold is crossed:
    - `safeProbability < minSafeProbability`
    - `injectionProbability > maxInjectionProbability`; likewise `exfiltrationProbability`, `destructiveProbability`, `hiddenInstructionsProbability`, `scopeMismatchProbability`, and `remoteExecutionProbability` against their `max*` thresholds
    - `riskScore > maxRiskScore`

    A blocked Version stays unsynced with reason `JevBlocked` and lists each crossed threshold. `riskConfidence < minConfidence` sets `needsReview` only and never blocks.
    - If any threshold is configured and Jev fails, the Version stays unsynced with reason `JevUnavailable` and retries with backoff.
    - If no threshold is configured, the error is recorded on status and sync continues.
    - Jev runs only after the deterministic scans pass. A scan-blocked Version never reaches Jev, and Jev never clears a Trivy or Expr rule finding.

13d. Feature flag and opt-in. Jev runs only when the version controller has `--jev-enabled` (Helm `scanning.jev.enabled`, default `false`) and the source sets `jevSecretRef` (Skill/Agent, or inherited from a Depot skill/agent config). If `jevSecretRef` is set while the flag is off, the source reports that Jev is disabled and makes no call. Results are cached on the Version and re-run only on Version creation or `ForceSync`. The module fast path and `Immutable` checksum guard are unchanged.

13e. `VersionStatus.JevAssessment`: `evaluatedAt`, `model` (as returned by the API, e.g. `jev-1.13.0`), `safeProbability`, `injectionProbability`, `exfiltrationProbability`, `destructiveProbability`, `hiddenInstructionsProbability`, `scopeMismatchProbability`, `remoteExecutionProbability`, `riskScore`, `riskLevel`, `riskConfidence`, `needsReview`, `blocked`, `blockReasons`, `error`. The token and raw prompt content are never stored.

13f. Privacy and data handling. When Jev runs, the `state` content from 13b (the skill or agent and its bundled file list, truncated at 64 KiB) is sent to TypeSafe for each evaluated Version. This includes private repositories. TypeSafe's privacy policy says it does not train on Input and does not disclose Input to third parties other than its service providers. Its DPA lists subprocessors at trust.typesafe.ai/subprocessors. The DPA text reviewed does not state a retention period. TypeSafe's legal page points to the DPA for retention and offers zero data retention to enterprise customers. Before enabling Jev on private repositories, confirm retention with TypeSafe in writing (sales@typesafe.ai). No cluster, environment, or other Secret data is sent.

13g. Security. `state` is untrusted repository content, so a skill can contain text aimed at the judge. Treat answers as advisory and act on them only through the gates in 13c. Read the token from the `secretKeyRef` in the CR's namespace. Confirm the version-controller role can `get` that Secret in namespaced scope. Do not grant cluster-wide Secret access.

13h. Disclosure. The UI (step 28), CLI (step 21), and docs (step 32) label Jev output as a TypeSafe assessment, say that skill and agent content was sent to TypeSafe, and state that scores are model probabilities, not a certification or an OpenDepot verdict.

13i. Calibration. Before any threshold default is documented, run Jev over a labeled set of known-safe and known-unsafe skills and agents, and record false-positive and false-negative rates in the PR. All thresholds stay unset by default until then.

## Phase 4: Server (depends on Phase 1; parallel with 2–3)

16. [discovery.go](../../services/server/discovery.go) and [types.go](../../services/server/types.go): add `agents.v1: /opendepot/agents/v1/` to `ServiceDiscoveryResponse`. `login.v1` is reused.
17. `services/server/agents.go` (routes in [main.go](../../services/server/main.go)), `kind` ∈ `skills|agents`. All handlers are read-only and serve stored status:
    - `GET /opendepot/agents/v1/{namespace}/{kind}/{name}/versions` — `{"versions":[{"version":"1.2.0","yanked":false}]}`, synced Versions only.
    - `GET .../{version}/download` — `protocols: ["1.0"]`, `kind`, `name`, `version`, `yanked`, `filename`, `download_url`, `shasum` (zh), `shasums_url`, `shasums_signature_url`, `signing_keys.gpg_public_keys` (`key_id`, `ascii_armor`), and an advisory `assessment` (scan result and Jev summary). Never includes the token.
    - `GET .../{version}/archive` — stream from storage or presigned redirect, with `Cache-Control: immutable`.
    - `GET .../{version}/SHA256SUMS` and `.../SHA256SUMS.sig` — serve `status.shaSums` and `status.shaSumsSignature`. Return 501 when absent. The server never signs on read.
    - `recordDownload` for metrics.
18. Key consistency: the server reads only the public key armor, derived from the provider GPG env vars through `pkg/signing`. It must match the fingerprint recorded on the Version (`SigningKeyFingerprint`). A mismatch returns 500 and logs a key-rotation error, so a rotated key cannot silently publish mismatched signatures.
19. [auth.go](../../services/server/auth.go): `skill` / `agent` cases in `isResourceAllowed` and `isSecurityResourceAllowed`.
20. [ui_browse.go](../../services/server/ui_browse.go), [policy_catalog.go](../../services/server/policy_catalog.go), [policy_api.go](../../services/server/policy_api.go): new kinds and `agent` scan type.

## Phase 5: CLI (depends on Phase 4 contract; parallel with Phase 6)

21. `cli/` Go module (cobra), added to [go.work](../../go.work). Commands:
    - `login <host>` — required hostname argument (bare host with optional port; a scheme or path is rejected before any request). `login.v1` PKCE loopback; token in `~/.config/opendepot/credentials.json` (0600); `OPENDEPOT_TOKEN` override; `tofu login` credentials fallback.
    - `init [--upgrade] [--allow-yanked] [--trust-new-key] [--no-prompt] [--force]`. Flags use the `--` form, which cobra requires for long flags — resolve constraints, verify (step 24), install into targets, and write `opendepot.lock.hcl`. Without `-upgrade`, `init` honors the existing lock. `-upgrade` re-resolves within constraints. `init` prints each entry's Jev `assessment` summary when present, labeled as a TypeSafe Jev assessment, with a note that skill or agent content was sent to TypeSafe. The summary is advisory and is not written to the lock. There is no `install`, `update`, or `outdated`; `init -upgrade` replaces them.
    - `validate [path]` — local `agentspec` checks.
    - `verify` — offline check of installed files against the lock.
22. `opendepot.hcl` (parsed with `hashicorp/hcl/v2`, the library the server already uses): an `opendepot` block with default `targets`, plus one `agent_skill` or `agent` block per entry, labeled by local name:
    ```hcl
    opendepot {
      targets = ["copilot", "claude"]
    }

    agent_skill "code-review" {
      source      = "registry.example.com/acme/code-review"
      version     = "~> 1.2"
      signing_key = "ABCD1234EF567890ABCD1234EF567890ABCD1234" # optional 40-hex fingerprint pin; a mismatch fails and never prompts
      path        = ".github/skills/code-review" # optional custom target
    }

    agent "reviewer" {
      source  = "registry.example.com/acme/reviewer"
      version = ">= 2.0.0, < 3.0.0"
    }
    ```
23. `opendepot.lock.hcl`, the shape of `.terraform.lock.hcl`, one block per entry. The block type matches the config (`agent_skill` or `agent`), and the label is the full source address:
    ```hcl
    agent_skill "registry.example.com/acme/code-review" {
      version     = "1.2.0"
      constraints = "~> 1.2"
      signing_key    = "ABCD1234EF567890ABCD1234EF567890ABCD1234"
      signature_hash = "sha256:..."
      hashes         = ["h1:...", "zh:..."]

      installed "copilot" {
        path = ".github/skills/code-review"
        hash = "h1:..."
      }
    }
    ```
    `zh:` is the archive SHA-256 from the signed `SHA256SUMS`. `h1:` is the directory hash of the extracted tree, using `golang.org/x/mod/sumdb/dirhash` `Hash1`, which is OpenDepot's own tree hash. `signature_hash` is `sha256:` over the binary `SHA256SUMS.sig` after dearmoring, so armored and binary forms of the same signature record the same value. A published version's signature does not change, so a re-signed version with the same signing key fails the check on a same-version re-resolve. Rotated keys re-pin through the trust rules and record the new signature. `installed` hashes are an OpenDepot addition used by `verify`.
24. Install verification chain (abort before writing on any mismatch):
    1. Resolve the constraint over `versions`, skipping yanked unless `-allow-yanked`. Refuse a `protocols` major other than 1.
    2. Fetch `SHA256SUMS` and its signature; verify with the advertised key.
    3. Key trust: a `signing_key` pin must match or the install fails. Otherwise the lock's pinned key must match. A new key fails under `-no-prompt`, prompts otherwise, and `-trust-new-key` re-pins. The first install pins the key (TOFU).
    4. Archive SHA-256 must match the signed line and the lock's `zh:`.
    5. Safe extract via `pkg/archive`.
    6. The `h1:` of the extracted tree must match the lock. For a same-version re-resolve under the same pinned key, the `signature_hash` must match the lock. Then write targets and record `installed` hashes.
25. Target mapping:
    - `copilot` → `.github/skills/{name}/`, `.github/agents/{name}.agent.md`
    - Agent source files are accepted as `{name}.agent.md` or `{name}.md`. Both present for one agent is an error. The agent frontmatter `name` is display metadata and is not required to match the file name.
    - `claude` → `.claude/skills/{name}/`, `.claude/agents/{name}.md`
    - custom `path`
    - `agents-md` → per-entry blocks in `AGENTS.md`
26. AGENTS.md markers:
    - Only content between `<!-- opendepot:begin <source> -->` and `<!-- opendepot:end <source> -->` is touched; new blocks append at EOF.
    - The block's content hash is recorded as its `installed` hash in `opendepot.lock.hcl`. A hand-edited block fails `verify` and requires `--force` to overwrite.
    - Removed manifest entries delete their block; unbalanced markers are an error.

## Phase 6: UI (depends on 20)

27. [services/ui/src/lib/api.ts](../../services/ui/src/lib/api.ts): new kinds, `AgentMetadata` and `JevAssessment` types.
28. `[namespace]` browse routes: skill/agent detail pages — rendered SKILL.md, tools/model chips, scan findings, copyable `opendepot.hcl` snippet. Jev panel: each question's probability (safe, injection, exfiltration, destructive actions, hidden instructions, scope mismatch, remote execution), risk level with confidence; `needsReview` badge; block reasons; model and evaluated time; disclosure "Assessed by TypeSafe Jev. Skill and agent content was sent to TypeSafe."; advisory disclaimer.
29. [Sidebar.tsx](../../services/ui/src/components/Sidebar.tsx): "Agents" section. [SecurityPoliciesSurface.tsx](../../services/ui/src/components/security-policies/SecurityPoliciesSurface.tsx): new target kinds + `agent` scan type.

## Phase 7: Packaging and Docs (parallel with 5–6, after Phase 2)

30. Chart: `agent-deployment.yaml`, `agent-rbac.yaml`, service account (modeled on module templates); values `agent.*` (default disabled), `scanning.agentScanning` → `--scan-agents`; `scanning.agentAllowedDomains` (default empty, which disables the domain rule) → `--agent-allowed-domains`; `scanning.jev.enabled` (default `false`) → `--jev-enabled`; `scanning.jev.endpoint` → `--jev-endpoint` (default TypeSafe URL) in [version-deployment.yaml](../../chart/opendepot/templates/version-deployment.yaml); RBAC updates for depot/version/server roles. The version controller gets a namespaced `get` on the signing Secret named by `version.gpg.secretName` and mounts it; no cluster-wide Secret access.
31. [go.work](../../go.work), [Makefile](../../Makefile) (agent-controller + Dockerfile), [Tiltfile](../../Tiltfile) (`docker_build` + `k8s_resource`).
32. Docs: `docs/guides/agents.md`, `docs/reference/cli.md` (`opendepot.hcl`, `opendepot.lock.hcl`, key pinning, the agents.v1 protocol, AGENTS.md markers); Jev setup in `docs/guides/agents.md` (enabling the feature, creating the Secret, the question list, threshold semantics, and a data-handling section covering what is sent, TypeSafe's no-training and no-disclosure commitments from its privacy policy, the DPA and subprocessor links, and confirming retention with TypeSafe before using Jev on private repositories); update [helm-chart.md](../../docs/helm-chart.md), [gpg.md](../../docs/configuration/gpg.md) (version controller signing mount), [rbac.md](../../docs/rbac.md), [groupbinding.md](../../docs/guides/groupbinding.md), [scanning.md](../../docs/configuration/scanning.md), [examples/depot.yaml](../../examples/depot.yaml), [mkdocs.yml](../../mkdocs.yml).

## Verification

1. Unit: `pkg/agentspec` fixtures; `pkg/archive` traversal/symlink rejection, repeat repack yields identical `zh:`, `h1:` stable across file ordering; `ListMatchingTags` with/without prefix.
2. Envtest: agent reconcilers (mirror module suite); version controller agent cases — fast path, immutability, blocking scan.
3. Policy: `Skill` / `Agent` target refs and `agent` scan type in [policy_test.go](../../services/version/internal/policy/policy_test.go).
4. Server (mirror [modules_test.go](../../services/server/modules_test.go)): blocked versions excluded, yanked versions listed with `yanked: true` and skipped by constraint resolution, GroupBinding denial, archive checksum match, SHA256SUMS signature verifies, 501 when `shaSums` is absent, the signing-key fingerprint mismatch returns 500, provider signature tests unchanged.
5. CLI: constraint resolution (all seven operators); lock round-trip; tampered archive fails `zh:`; tampered SHA256SUMS fails signature; re-signed locked version fails `signature_hash`; armored and binary signatures hash the same; `login` rejects a missing or non-hostname argument; rotated key fails under `-no-prompt` and re-pins only with `-trust-new-key`; `signing_key` mismatch fails without prompting; yanked versions skipped unless `-allow-yanked`; unsupported protocol major fails; modified installed file fails `verify` on `h1:`; AGENTS.md preserves outside content, requires `--force` on edited blocks, removes blocks for dropped entries, rejects unbalanced markers; per-target install layout; `init -upgrade` removes stale files.
6. E2E (`services/agent/test/e2e`, Tilt): Depot with monorepo skill path + prefix syncs Versions with findings on status; `opendepot login` + `init` into a temp repo populates `.github/skills` and `.claude/skills` and writes `opendepot.lock.hcl` with `zh:`/`h1:`/fingerprint; re-running `init` is a no-op; `init -upgrade` across a minor rewrites the lock; `agents.v1` discovery and the versions/download/SHA256SUMS endpoints return the documented shapes.
7. UI (https://opendepot.localtest.me:8443): skill detail page; create a ScanPolicy targeting `Skill`.
8. Jev: `pkg/jev` request and response mapping from recorded fixtures; gate table tests (each of the nine thresholds alone and combined, unset = no block, `confidence` never blocks); fail-closed when gated and unavailable, informational when not; no HTTP call when the feature flag is off, without `jevSecretRef`, or after a scan block; Trivy runs with Jev enabled and disabled; token absent from status, events, logs, and errors (test with a sentinel token). E2E uses a mock endpoint via `--jev-endpoint`. The live API is smoke-tested manually, not in CI.
9. Expr rules: each built-in rule has a positive and a negative fixture; an invalid rule expression fails startup; the domain rule is disabled when `AllowedDomains` is empty; rules run against the full extracted content, not the 64 KiB Jev state.