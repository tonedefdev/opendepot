# Implementation Plan: Agent Registry — Skills, Agent Definitions, and the `opendepot` CLI

## Summary

Bring everything OpenDepot does for modules and providers to agent skills and
agent definitions. Users point a `Depot` (or a `Skill` / `AgentDefinition` CR
directly) at a GitHub repo path; a dedicated agent-controller creates `Version`
CRs; the version-controller fetches, validates, repackages, stores, and scans
them with `trivy fs` plus native frontmatter validation and bundled Rego checks.
The server exposes an `agents.v1` registry API with GPG-signed checksums. A new
`opendepot` CLI resolves `version: ~> 1.0.0` constraints, verifies signatures
and hashes against a lockfile, and installs into Copilot, Claude, AGENTS.md, or
custom target directories — the `tofu` of agent skills and definitions.

## Decisions

- Two CRDs (`Skill`, `AgentDefinition`) reconciled by one agent-controller.
- Monorepo-aware sources: `repoOwner` + repo + `path` + optional `tagPrefix`. GitHub only.
- Publishing = Git tags + Depot polling (same as modules). No push/upload API.
- Discovery key `agents.v1` → `/opendepot/agents/v1/`.
- Consumer manifest `opendepot.yaml` + lockfile `opendepot.lock` (YAML).
- CLI binary `opendepot`, new `cli/` Go module in this repo.
- Install targets: `copilot`, `claude`, `agents-md`, custom `path`.
- Scanning: `trivy fs --scanners vuln,secret,misconfig` + native frontmatter validation + bundled Rego checks.
- Server only resolves synced (scan-passing) versions.
- Signing reuses the provider GPG key (`server.gpg.secretName`); no new key/secret.
- Lockfile records `zh:` (archive sha256) + `h1:` (dirhash of installed tree) + signing key fingerprint; TOFU key pinning unless `trustedKeys` pins explicitly.
- AGENTS.md edits confined to per-entry `<!-- opendepot:begin <source> -->` / `<!-- opendepot:end <source> -->` markers.
- Excluded: non-GitHub VCS, CLI `publish`/`search`, global (home dir) installs, per-namespace signing keys.

## Affected Services

- **api/v1alpha1** — new CRDs, shared source config, Version/Depot/GroupBinding/ScanPolicy fields
- **pkg/agentspec** (new) — frontmatter parsing + validation
- **pkg/archive** (new) — safe extraction + deterministic repack
- **pkg/github** — tag listing, generic archive fetch
- **pkg/utils** — latest/trim/name helpers for new kinds
- **services/agent** (new) — Skill + AgentDefinition reconcilers
- **services/depot** — skill/agent config loops
- **services/version** — agent fetch/store/scan path, Rego checks, policy scan type
- **services/server** — agents API, shared signing, auth, browse, policy
- **cli** (new) — `opendepot` CLI
- **services/ui** — browse pages, sidebar, security policy kinds
- **chart/opendepot**, **Tiltfile**, **Makefile**, **go.work**, **docs**

## Phase 1: API and Shared Packages (blocks all)

1. [api/v1alpha1/types.go](../../api/v1alpha1/types.go):
   - `AgentSourceConfig`: `Name`, `RepoOwner`, `RepoUrl`, `Path`, `TagPrefix`, `GithubClientConfig`, `StorageConfig`, `Immutable`, `VersionConstraints`, `VersionHistoryLimit`.
   - `Skill` / `AgentDefinition` with spec/status mirroring `ModuleSpec`/`ModuleStatus` (`Versions`, `ForceSync`, `LatestVersion`, `VersionRefs`, `Synced`, `SyncStatus`).
   - Constants `OpenDepotSkill`, `OpenDepotAgentDefinition`; register in `init()`.
2. `VersionSpec.AgentSourceRef *AgentSourceConfig`; `VersionStatus.AgentMetadata` (name, description, tools, model).
3. `DepotSpec.SkillConfigs` / `AgentConfigs`; `DepotStatus.Skills` / `AgentDefinitions`.
4. `GroupBindingSpec` and `SecurityGroupBindingSpec`: `SkillResources`, `AgentResources` (glob patterns).
5. `ScanPolicyTargetRef.Kind` enum adds `Skill;AgentDefinition`; `ScanExemption.ScanTypes` enum adds `agent`.
6. Regenerate deepcopy + CRDs; sync into [chart/opendepot/crds](../../chart/opendepot/crds).
7. *(parallel)* `pkg/agentspec`: parse SKILL.md / agent `.md` YAML frontmatter; validate name format/length, name matches directory, required description, known `tools`/`model` keys. Emit `SecurityFinding`s with IDs `opendepot-agentspec-NNN`.
8. *(parallel)* `pkg/archive`: move `extractArchiveToDir` / `extractZipEntry` / `extractTarEntry` out of [scanner.go](../../services/version/internal/controller/scanner.go); add symlink rejection and a deterministic tar.gz writer (sorted entries, zeroed mtime/uid/gid) for stable checksums.
9. *(parallel)* [pkg/github/github.go](../../pkg/github/github.go): `ListMatchingTags(owner, repo, tagPrefix, constraints)` paginating `ListTags` and stripping the prefix; generalize `GetArchiveRequest` to `(owner, repo, ref)`.

## Phase 2: Controllers (depends on Phase 1)

10. `services/agent` scaffolded like [services/module](../../services/module):
    - `SkillReconciler` + `AgentDefinitionReconciler` sharing an internal helper modeled on `ModuleReconciler.Reconcile` (create/update Versions, history trim, `ReconcileVersionRemovals`, `ForceSync` reset).
    - Labels `opendepot.defdev.io/skill` / `opendepot.defdev.io/agent`.
    - Version names `skill-{name}-{ver}` / `agent-{name}-{ver}` to avoid module collisions.
    - Extend `utils.GetLatestVersion`, `VersionsToKeep`, `GetName` for the new kinds.
11. [opendepot_depot_controller.go](../../services/depot/internal/controller/opendepot_depot_controller.go): skill/agent config loops using `ListMatchingTags` with `GlobalConfig` storage/GitHub fallbacks.
12. [opendepot_versions_controller.go](../../services/version/internal/controller/opendepot_versions_controller.go):
    - New type cases, `prepareAgentVersion`, `fetchAgentArchive`: download tarball at `{tagPrefix}{version}` (v/no-v fallback), extract, keep only `Path`, validate via `agentspec`, deterministic repack.
    - Reuse module fast path and `Immutable` checksum guard.
    - SKILL.md / agent `.md` into the existing readme ConfigMap flow; populate `AgentMetadata`.
    - `getVersionStorageConfig` / `getVersionName` / `getVersionFilePath` handle `AgentSourceRef`; storage names `skill-{name}` / `agent-{name}` (Azure container names reject `/`).
    - Dual-config guard covers all three refs; RBAC markers for `skills` / `agentdefinitions`.

## Phase 3: Scanning (depends on 12)

13. `runAgentScan` in [scanner.go](../../services/version/internal/controller/scanner.go):
    - Write sidecar `.opendepot/agent.json` (frontmatter, body, file list).
    - `trivy fs --scanners vuln,secret,misconfig --config-check /opt/opendepot/checks --check-namespaces user`.
    - Merge `agentspec` findings; `policy.Apply(..., policy.ScanTypeAgent)`; persist as `SourceScan` via existing `persistScanViolation` / `reconcileStoredScanPolicy`.
14. `services/version/checks/agents/*.rego`, baked into the version-controller image: wildcard tool grants, unscoped shell/terminal tools, `curl|sh`-style remote exec in body/scripts, fetch + write tool combos, missing description.
15. [policy.go](../../services/version/internal/policy/policy.go): `ScanTypeAgent`; `resourceName` handles new types/labels. Controller flag `--scan-agents`.

## Phase 4: Server (depends on Phase 1; parallel with 2–3)

16. [discovery.go](../../services/server/discovery.go): advertise `agents.v1: /opendepot/agents/v1/`.
17. Extract GPG loading + `openpgp.DetachSign` from `getProviderPackageSHA256SUMSSignature` / `getProviderSigningKeysFromEnv` in [providers.go](../../services/server/providers.go) into `services/server/signing.go`, shared by providers and agents.
18. `services/server/agents.go` (routes in [main.go](../../services/server/main.go)), `kind` ∈ `skills|agents`:
    - `GET /opendepot/agents/v1/{namespace}/{kind}/{name}/versions` — synced versions only.
    - `GET .../{version}/download` — JSON: archive URL, `shasum`, `shasums_url`, `shasums_signature_url`, `signing_keys` (provider metadata shape), `AgentMetadata`.
    - `GET .../{version}/archive` — stream from storage or presigned redirect.
    - `GET .../{version}/SHA256SUMS` and `.../SHA256SUMS.sig` — signed at request time.
    - `recordDownload` for metrics.
19. [auth.go](../../services/server/auth.go): `skill` / `agent` cases in `isResourceAllowed` and `isSecurityResourceAllowed`.
20. [ui_browse.go](../../services/server/ui_browse.go), [policy_catalog.go](../../services/server/policy_catalog.go), [policy_api.go](../../services/server/policy_api.go): new kinds and `agent` scan type.

## Phase 5: CLI (depends on Phase 4 contract; parallel with Phase 6)

21. `cli/` Go module (cobra), added to [go.work](../../go.work). Commands:
    - `login [host]` — `login.v1` PKCE loopback; token in `~/.config/opendepot/credentials.json` (0600); `OPENDEPOT_TOKEN` override; `tofu login` credentials fallback.
    - `init`, `install`, `update [names...]`, `outdated`.
    - `validate [path]` — local `agentspec` checks.
    - `verify` — offline check of installed files against the lock.
22. `opendepot.yaml`: `registry`, default `targets`, `skills[]` / `agents[]` (`source: [host/]namespace/name`, `version`, optional `targets`/`path`), optional `trustedKeys[]` (GPG fingerprints).
23. `opendepot.lock` per entry: `source`, `version`, `hashes` (`zh:<archive sha256>`, `h1:<dirhash Hash1 of extracted tree>`), `signingKey` fingerprint, owned `files`.
24. Install/update verification chain (abort before writing on any mismatch):
    1. Fetch SHA256SUMS + signature; verify with advertised key.
    2. Key must match `trustedKeys` or the lock's pinned fingerprint; new key fails without `--trust-new-key`; first install pins it.
    3. Archive sha256 vs SHA256SUMS and lock `zh:`.
    4. Safe extract via `pkg/archive`.
    5. Compute `h1:` and compare to the lock.
25. Target mapping:
    - `copilot` → `.github/skills/{name}/`, `.github/agents/{name}.agent.md`
    - `claude` → `.claude/skills/{name}/`, `.claude/agents/{name}.md`
    - custom `path`
    - `agents-md` → per-entry blocks in `AGENTS.md`
26. AGENTS.md markers:
    - Only content between `<!-- opendepot:begin <source> -->` and `<!-- opendepot:end <source> -->` is touched; new blocks append at EOF.
    - Block content hash in the lock; hand-edited blocks require `--force`.
    - Removed manifest entries delete their block; unbalanced markers are an error.

## Phase 6: UI (depends on 20)

27. [services/ui/src/lib/api.ts](../../services/ui/src/lib/api.ts): new kinds, `AgentMetadata` type.
28. `[namespace]` browse routes: skill/agent detail pages — rendered SKILL.md, tools/model chips, scan findings, copyable `opendepot.yaml` snippet.
29. [Sidebar.tsx](../../services/ui/src/components/Sidebar.tsx): "Agents" section. [SecurityPoliciesSurface.tsx](../../services/ui/src/components/security-policies/SecurityPoliciesSurface.tsx): new target kinds + `agent` scan type.

## Phase 7: Packaging and Docs (parallel with 5–6, after Phase 2)

30. Chart: `agent-deployment.yaml`, `agent-rbac.yaml`, service account (modeled on module templates); values `agent.*` (default disabled), `scanning.agentScanning` → `--scan-agents` in [version-deployment.yaml](../../chart/opendepot/templates/version-deployment.yaml); RBAC updates for depot/version/server roles.
31. [go.work](../../go.work), [Makefile](../../Makefile) (agent-controller + Dockerfile), [Tiltfile](../../Tiltfile) (`docker_build` + `k8s_resource`).
32. Docs: `docs/guides/agents.md`, `docs/reference/cli.md` (lock hashes, key pinning, AGENTS.md markers); update [helm-chart.md](../../docs/helm-chart.md), [gpg.md](../../docs/configuration/gpg.md), [rbac.md](../../docs/rbac.md), [groupbinding.md](../../docs/guides/groupbinding.md), [scanning.md](../../docs/configuration/scanning.md), [examples/depot.yaml](../../examples/depot.yaml), [mkdocs.yml](../../mkdocs.yml).

## Verification

1. Unit: `pkg/agentspec` fixtures; `pkg/archive` traversal/symlink rejection, repeat repack yields identical `zh:`, `h1:` stable across file ordering; `ListMatchingTags` with/without prefix.
2. Envtest: agent reconcilers (mirror module suite); version controller agent cases — fast path, immutability, blocking scan.
3. Policy: `Skill` / `AgentDefinition` target refs and `agent` scan type in [policy_test.go](../../services/version/internal/policy/policy_test.go).
4. Server (mirror [modules_test.go](../../services/server/modules_test.go)): blocked versions excluded, GroupBinding denial, archive checksum match, SHA256SUMS signature verifies, 501 when no key, provider signature tests unchanged.
5. CLI: constraint resolution; lock round-trip; tampered archive fails `zh:`; tampered SHA256SUMS fails signature; rotated key fails without `--trust-new-key`; modified installed file fails `verify` on `h1:`; AGENTS.md preserves outside content, requires `--force` on edited blocks, removes blocks for dropped entries, rejects unbalanced markers; per-target install layout; `update` removes stale files.
6. E2E (`services/agent/test/e2e`, Tilt): Depot with monorepo skill path + prefix syncs Versions with findings on status; `opendepot login` + `install` into a temp repo populates `.github/skills` and `.claude/skills` and writes a lock with `zh:`/`h1:`/fingerprint; re-`install` is a no-op; `update` across a minor rewrites the lock.
7. UI (https://opendepot.localtest.me:8443): skill detail page; create a ScanPolicy targeting `Skill`.
