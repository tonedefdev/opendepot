# Implementation Plan: Agent/Skill Definition Storage and Registry Explorer UX (0.12.0)

## Summary
Skill and Agent definitions are currently written into the README ConfigMap (`<version>-readme`). That ConfigMap is written only on the final success status write, so any Version that is blocked or held never gets its ref recorded. This change:
- adds a dedicated, idempotent `definitionConfigMapRef` holding the actual SKILL.md / agent `.md` file, written early and backfilled;
- makes `readmeContent` mean only the README again;
- shows the GitHub source repo, the tracked branch and the commit SHA for Agents and Skills;
- gives Agent and Skill their own chip colors;
- nests Agents, Skills, Modules and Providers under Registry in the sidebar, with an all-kinds default;
- colors Jev bars red, yellow or green using the CLI's thresholds;
- hardens and tests the uncommitted symlink subtree extraction fix.

## Affected Services
- **`api/v1alpha1`**: new status field, deepcopy, and the CRD YAML (six copies).
- **`pkg/archive`**: harden `ExtractSubtreeToDir` and add focused tests.
- **`services/version`**:
  - New definition ConfigMap with an early ref write.
  - Fast-path and startup backfill for Versions with no ref.
  - Agents stop using the README slot.
  - Stale-object status writes are fixed.
- **`services/server`**:
  - `definitionContent` and the definition file name.
  - Agent and Skill `repoUrl` and `repoOwner`.
  - `sourceRef`, `sourceCommit` and `sourcePath`.
  - The list shows all kinds by default.
  - Version summaries gain the commit and the branch phase.
- **`services/ui`**: types, definition card, repo and ref display, kind colors, sidebar, and Jev colors.
- **`services/agent`**: e2e assertions only.
- **`docs/`**: API reference, agents guide, and Registry Explorer guide.

## API / CRD Changes (generated-code step 3)
In `api/v1alpha1/types.go`:
- **`VersionStatus`**: add a field next to `ReadmeConfigMapRef`, with doc comment "DefinitionConfigMapRef references the ConfigMap holding the Skill or Agent definition file (SKILL.md or the agent .md). Only populated for Skill and Agent Versions."
  - Marker: `// +optional`
  - Field: `DefinitionConfigMapRef *DefinitionConfigMapRef \`json:"definitionConfigMapRef,omitempty"\``
- **New type `DefinitionConfigMapRef`**:
  - `Name string \`json:"name"\``: the ConfigMap name, `<version-name>-definition`.
  - `Key string \`json:"key"\``: the data key holding the base64-encoded file.
  - `FileName string \`json:"fileName"\``: the source entry file name, for example `SKILL.md`, `code-review.agent.md` or `code-review.md`. Marker `// +kubebuilder:validation:MaxLength=255`.
- **`ReadmeConfigMapRef` doc comment**: say it is for the README only and is not used for Skill or Agent definitions.

The `ReadmeConfigMapRef` field stays. It is additive and backward compatible.

## Implementation Steps

### Step 1: Harden the archive subtree extraction and add focused tests
- **Depends on:** none
- **Files:** `pkg/archive/archive.go`, `pkg/archive/archive_test.go`
- **Changes:**
  - Keep the uncommitted `ExtractSubtreeToDir` / `extractArchive(keep)` fix. Do not rework it.
  - Harden the `keep` matcher. Clean the entry name with `path.Clean`, then reject or skip any entry whose name contains a `..` segment. This stops `repo/skills/a/../b/x` from passing the subtree prefix check and escaping the subtree.
  - Count skipped entries against the existing entry-count budget, and cap the total header bytes scanned. A bomb in skipped entries must not cost unbounded decompression.
  - Keep the existing `pax_global_header` handling.
- **Tests to add:**
  - Zip-path variants of the existing tar tests: outside links skipped, inside links rejected, sibling-prefix not matched.
  - Dot-dot entry names.
  - The budget is exceeded by skipped entries alone.
  - An empty subtree (the root).
  - A subtree with no matching entries returns a clear error.
- **Acceptance:**
  - `go test -count=1 ./pkg/archive/...` passes.
  - No entry outside the subtree is written.
  - No path escapes `destDir`.
  - Symlinks inside the subtree are rejected, and symlinks outside it are skipped.
  - The budgets apply to skipped entries.

### Step 2: Security Review of archive extraction (gate; no files)
- **Depends on:** 1
- **Files:** none (review only)
- **What:** Security Review of `pkg/archive` and of how `fetchAgentArchive` calls it. Untrusted GitHub archives cross a trust boundary here. Check:
  - path traversal and symlink or hardlink handling, in both tar and zip;
  - the subtree prefix logic;
  - the decompression and entry budgets;
  - the temp-directory lifecycle and cleanup.
- **Acceptance:** written sign-off, or findings that are fixed in step 1, before the release branch merges.

### Step 3: [GENERATED CODE] Add the `DefinitionConfigMapRef` API type, deepcopy and CRDs
- **Depends on:** none
- **Files:**
  - `api/v1alpha1/types.go`
  - `api/v1alpha1/zz_generated.deepcopy.go`
  - `services/version/config/crd/bases/opendepot.defdev.io_versions.yaml`
  - `services/agent/config/crd/bases/opendepot.defdev.io_versions.yaml`
  - `services/depot/config/crd/bases/opendepot.defdev.io_versions.yaml`
  - `services/module/config/crd/bases/opendepot.defdev.io_versions.yaml`
  - `services/provider/config/crd/bases/opendepot.defdev.io_versions.yaml`
  - `chart/opendepot/crds/opendepot.defdev.io_versions.yaml`
- **Changes:** add the field and type above. Run `make generate manifests`, then copy the generated Versions CRD so every copy is byte-identical.
- **Acceptance:**
  - All six YAML files are identical (`diff`).
  - `helm lint chart/opendepot` passes.
  - `cd test/chart && GOWORK=off go test ./... -count=1` passes.
  - Tier 1 (compile/vet) passes in every service module.

### Step 4: Definition ConfigMap helper in the version controller
- **Depends on:** 3
- **Files:** `services/version/internal/controller/definition.go` (new), `services/version/internal/controller/definition_test.go` (new)
- **What:** add `upsertDefinitionConfigMap` and `persistDefinitionRef`, mirroring `readme.go`:
  - **Shape:** ConfigMap `<version>-definition` with key `definition`, holding the raw entry bytes base64-encoded (the same encoding as the README). The full file is stored, including the frontmatter.
  - **Owner and labels:** owner reference to the Version. Labels `opendepot.defdev.io/skill` or `opendepot.defdev.io/agent` set to the parent name, plus `app.kubernetes.io/managed-by`.
  - **Idempotent:** use `controllerutil.CreateOrUpdate`. Running it twice gives the same ConfigMap and no error.
  - **Size cap:** reject entry bytes over about 900 KiB with a clear error, to stay under the ConfigMap limit.
  - **Status write:** `persistDefinitionRef` writes only `Status.DefinitionConfigMapRef`, inside `retry.RetryOnConflict` with Get-then-Update, the `opendepotControllerName` field manager, and a no-op when unchanged.
  - Add a helper to check that the referenced ConfigMap exists.
- **Acceptance (envtest unit tests):**
  - Create, update and no-op paths work.
  - Retry on a conflict works.
  - The owner reference and labels are correct.
  - The oversize entry is rejected.

### Step 5: Wire the definition into Version reconcile and fix lost refs
- **Depends on:** 1, 4
- **Files:**
  - `services/version/internal/controller/opendepot_versions_controller.go`
  - `services/version/internal/controller/opendepot_versions_controller_test.go`
  - `services/version/internal/controller/agent_versions_test.go`

**`fetchAgentArchive`:**
- Return the raw entry bytes and the entry file name: `SKILL.md`, or the base name from `FindAgentEntry`, which is either `<name>.agent.md` or `<name>.md`.
- Stop returning `parsed.Body` as `agentReadme`.
- Keep the call to `ExtractSubtreeToDir`, using step 1's hardened behavior.

**Early write:**
- Right after a successful fetch and the immutability check, and **before** the Trivy, Jev, signing and branch-assign gates, call `upsertDefinitionConfigMap` and then `persistDefinitionRef`.
- Also set `version.Status.DefinitionConfigMapRef` on the in-memory object.
- A failure is logged and non-fatal, and the Version is requeued so it retries.

**README slot:**
- The README block runs for modules only. Agents and Skills no longer write `<version>-readme`.
- Move the module `readmeConfigMapRef` persistence to the same early-write pattern, so blocked modules keep their ref too.

**Stale writes:**
- Replace the stale `_ = r.Status().Update(ctx, version)` calls on the fetch-error and immutable-checksum paths with Get-then-Update retry writes. These write only `Synced` and `SyncStatus`, so they never overwrite refs written earlier in the same reconcile.

**Backfill:**
- Add `isAgent && (Status.DefinitionConfigMapRef == nil || referenced ConfigMap missing)` to `bypassFastPath`.
- In `skipResyncedCreate` (`SetupWithManager`), let through agent Versions that have no `DefinitionConfigMapRef`, so a controller restart re-syncs them once.

**Stale README cleanup:**
- During backfill, for an agent or skill Version that has a `ReadmeConfigMapRef`, delete the `<version>-readme` ConfigMap (owned by this Version) and clear `ReadmeConfigMapRef`. Today it only ever holds the definition body.

**Root cause check (before coding):**
- Run `kubectl get version <skill-aspire version> --show-managed-fields -o yaml` and record which manager and operation last wrote status.
- Add a regression test for that path if it is not one of the paths above.

**Tests:**
- A blocked Version (Jev block, Trivy block, branch Held) still has `DefinitionConfigMapRef` and its ConfigMap.
- A synced Version with no ref is re-fetched and gets the ref; one with a ref takes the fast path.
- The predicate lets through a Version with no ref.
- Agent Versions get no `-readme` ConfigMap.
- A stale `-readme` ConfigMap is removed.
- A controller-level subtree test checks that a skill directory with an outside symlink extracts and that an inside symlink is rejected.

**Acceptance:**
- `make test` in `services/version` passes.
- Every Skill or Agent Version that reaches the download step has a definition ConfigMap and ref, even if it is later blocked.
- Rejected or Superseded branch Versions are expected to have none, because they never download. This probably explains part of the "10 of 20" missing ConfigMaps.

### Step 6: Server browse contract changes
- **Depends on:** 3
- **Files:**
  - `services/server/types.go`
  - `services/server/ui_browse.go`
  - `services/server/ui_browse_agent_test.go`
  - `services/server/ui_browse_test.go` (edit if it exists; otherwise create it)

**Types (in `types.go`):**
- `BrowseResource` adds `SourceRef string \`json:"sourceRef,omitempty"\``, `SourceCommit string \`json:"sourceCommit,omitempty"\`` and `SourcePath string \`json:"sourcePath,omitempty"\``.
- `BrowseResourceDetail` adds `DefinitionContent *string \`json:"definitionContent,omitempty"\`` and `DefinitionFileName string \`json:"definitionFileName,omitempty"\``.
- `BrowseVersionSummary` adds `Commit string \`json:"commit,omitempty"\`` and `BranchPhase string \`json:"branchPhase,omitempty"\``.

**Definition reader:**
- Add `browseAgentDefinition`, which mirrors `browseModuleReadme` and reads `Status.DefinitionConfigMapRef` of the latest Version.
- In the skill/agent detail case, stop filling `ReadmeContent` from the README slot. Leave it empty for agents.

**Repo (Requirement 2):**
- In `agentToCard`, set `RepoURL` from `AgentSourceConfig.RepoUrl` only when it is `https://github.com/<owner>/<repo>`.
- Otherwise build `https://github.com/<RepoOwner>/<repoName>`, with the repo name taken from `RepoUrl` or `Name` the same way `AgentSourceRepoName` does it.
- In the detail response, set `RepoOwner`, `SourcePath` and `GithubConfig`.
- Do not add a `go.mod` requirement. If `services/server/go.mod` does not already require `pkg/github`, write a small local helper instead.

**Ref and commit (Requirement 3):**
- Set `SourceRef` only when the source tracks a branch (`AgentSourceConfig.Ref` set).
- Set `SourceCommit` to the `Status.SourceCommit`, or failing that the `Spec.SourceCommit`, of the latest assigned Version.
- In `agentVersionSummaries`, add `Commit` and `BranchPhase`.
- Tag-mode resources leave these fields empty.

**Default kind (Requirement 5):**
- `handleBrowseResources` with an empty `kind` collects module, provider, skill and agent.
- Sorting and pagination run across the merged set.
- An explicit `kind` behaves as it does today.

**Acceptance (unit tests):**
- The definition is returned with its file name, and `readmeContent` is absent for agents.
- `repoUrl` is filled in from `RepoUrl` and also by the fallback.
- `sourceRef` and `sourceCommit` are present for branch mode and absent for tag mode.
- Version summaries carry the commit and phase.
- The unfiltered list includes all four kinds, and `totalCount` and paging are correct.

### Step 7: Agent e2e assertions
- **Depends on:** 5, 6
- **Files:** `services/agent/test/e2e/e2e_test.go`
- **Changes:** in the `Skill` and `Agent` Describes, add specs that check:
  - `.status.definitionConfigMapRef.name` and `.fileName` are set (`SKILL.md` for the skill; `code-review.agent.md` or `code-review.md` for the agent);
  - the ConfigMap exists, and its key decodes to content starting with the `---` frontmatter;
  - there is no `<version>-readme` ConfigMap.
- **Acceptance:** the focused agent e2e passes (see Validation).

### Step 8: UI API types
- **Depends on:** 6
- **Files:** `services/ui/src/lib/api.ts`
- **Changes:**
  - `BrowseResource` gains `sourceRef?`, `sourceCommit?` and `sourcePath?`.
  - `BrowseResourceDetail` gains `definitionContent?` and `definitionFileName?`.
  - `BrowseVersionSummary` gains `commit?` and `branchPhase?`.
  - Update the `readmeContent` doc comment: README only.
- **Acceptance:** `tsc --noEmit` passes.

### Step 9: Shared kind colors
- **Depends on:** none
- **Files:** `services/ui/src/lib/kindColors.ts` (new), `services/ui/src/lib/kindColors.test.ts` (new)
- **Changes:**
  - Export `MODULE_COLOR` (`#03deb8`), `PROVIDER_COLOR` (`#047df1`), `SKILL_COLOR` and `AGENT_COLOR` (see question 5).
  - Export `kindColor(kind)`.
  - `DepotGraph.tsx` and `DepotNodePanel.tsx` are not touched. They show only modules and providers.
- **Acceptance:** each of the four kinds maps to a distinct color, and unknown kinds fall back to the module color.

### Step 10: Shared Jev severity helper
- **Depends on:** none
- **Files:** `services/ui/src/lib/jevSeverity.ts` (new), `services/ui/src/lib/jevSeverity.test.ts` (new)
- **Changes:** port the logic from `cli/output.go`:
  - `probabilityColor`: `risk = v`, or `1 - v` for higher-is-better rows (Safe). risk ≥ 0.5 is red, ≥ 0.25 is yellow, otherwise green.
  - `riskLevelColor`: Minimal and Low are green, Moderate is yellow, High and Critical are red.
  - Map the colors to the MUI palette `error`, `warning` and `success`.
  - Apply colors only to the rows the CLI colors. Rows the CLI leaves plain stay neutral.
- **Acceptance:** the boundary values 0.25 and 0.5, and the inverted Safe row, are tested.

### Step 11: JevPanel severity colors
- **Depends on:** 10
- **Files:** `services/ui/src/components/JevPanel.tsx`, `services/ui/src/components/JevPanel.test.tsx`
- **Changes:**
  - Each `LinearProgress` uses the color from `jevSeverity`.
  - A breached policy threshold still forces red.
  - The `RISK_COLORS` mapping for `Low` changes from `info` to `success`, to match the CLI.
- **Acceptance:** tests show red, yellow and green bars for sample values, an inverted Safe row, and that a breach overrides the color.

### Step 12: Source ref chip component
- **Depends on:** 8
- **Files:** `services/ui/src/components/SourceRefChip.tsx` (new), `services/ui/src/components/SourceRefChip.test.tsx` (new)
- **Changes:**
  - Shows a branch icon with `ref @ <first 7 characters of the SHA>`, and the full SHA in a tooltip.
  - Links to `<repoUrl>/commit/<sha>` when the repo is a GitHub URL. The ref text links to `<repoUrl>/tree/<ref>/<path>`.
  - Renders nothing when `sourceRef` is empty, so tag mode is unchanged.
- **Acceptance:** tests cover rendering, hiding, links, and a missing commit (shows the ref only).

### Step 13: Definition renderer
- **Depends on:** 8
- **Files:** `services/ui/src/components/AgentDetail.tsx`, `services/ui/src/components/AgentDetail.test.tsx`
- **Changes:**
  - Add an `AgentDefinition` export that takes `content` and `fileName`.
  - The YAML frontmatter is split off and shown in a code block. The body is rendered with the existing ReactMarkdown `AgentBody`.
- **Acceptance:** tests show the frontmatter block, the body markdown, and content with no frontmatter.

### Step 14: Detail page wiring
- **Depends on:** 8, 9, 12, 13
- **Files:** `services/ui/src/app/[namespace]/[kind]/[name]/page.tsx`
- **Changes:**
  - **Definition card:** the card is titled **SKILL.md** for skills and **AGENT.md** for agents, with `definitionFileName` as a caption. It renders from `definitionContent`.
  - **README:** the README card renders only from `readmeContent`.
  - **Repo:** the GitHub icon with the repo path uses the existing module/provider hero block and `registrySource.ts` as they are, fed by `repoUrl` and `repoOwner`. The agent `sourcePath` is appended.
  - **Ref chip:** `SourceRefChip` appears in the hero.
  - **Colors:** the hero chip, border and gradient use `kindColor`.
- **Acceptance:**
  - An Agent page shows AGENT.md and the GitHub link, plus the ref chip in branch mode.
  - Module and Provider pages look as they do today.

### Step 15: Resource card
- **Depends on:** 8, 9, 12
- **Files:** `services/ui/src/components/ResourceCard.tsx`
- **Changes:** the kind chip and hover use `kindColor`. `SourceRefChip` is shown when `sourceRef` is set. Skill and Agent cards show the GitHub repo path the same way module cards do.
- **Acceptance:** Skill and Agent cards have distinct colors, and tag-mode cards are unchanged.

### Step 16: Versions table commit column
- **Depends on:** 8
- **Files:** `services/ui/src/components/VersionsTable.tsx`
- **Changes:** for skill and agent rows that have `commit`, show a short SHA with a copy button and the `branchPhase` chip. The column is hidden when no row has a commit.
- **Acceptance:** branch Versions show the commit, and module and provider tables are unchanged.

### Step 17: Sidebar restructure and home page
- **Depends on:** 6
- **Files:** `services/ui/src/components/Sidebar.tsx`, `services/ui/src/components/Sidebar.test.tsx`, `services/ui/src/app/page.tsx`
- **Changes:**
  - Registry becomes a parent item (`/`, which clears `kind`) with the nested children Agents, Skills, Modules and Providers. Each child links to `/?kind=<kind>`.
  - The active child is highlighted from `kind`.
  - Remove the separate Agents section and the redundant Kind chip filter (question 6).
  - In the collapsed sidebar, the children show as icon buttons with tooltips.
  - Home page text becomes "Browse agents, skills, modules, and providers". With no `kind`, it lists all four.
- **Acceptance:** tests cover the nested links and their hrefs in the expanded and collapsed sidebar, the active state, the absence of the Agents section, and the existing Security Policies tests still passing.

### Step 18: Playwright smoke test
- **Depends on:** 17
- **Files:** `services/ui/test/e2e/smoke.spec.ts`
- **Changes:** assert that the server-rendered sidebar has a Registry link and the four child links with the right `?kind=` values.
- **Acceptance:** the `ui-playwright.yaml` workflow passes.

### Step 19: Docs
- **Depends on:** 3, 6, 17
- **Files:**
  - `docs/reference/api.md`
  - `docs/guides/agents.md`
  - `docs/guides/registry-explorer/browse.md`
  - `docs/guides/registry-explorer/registry.md`
- **Changes:**
  - `definitionConfigMapRef`, and that `readmeConfigMapRef` is README only.
  - Branch ref and commit display.
  - The new sidebar.
  - The Jev color thresholds.
- **Acceptance:** `mkdocs build --strict` passes.

### Step 20: Focused validation (no files)
- **Depends on:** 2, 7, 11, 14, 15, 16, 18, 19
- **Files:** none
- **Acceptance:** all checks in E2E Test Changes below pass.

**Waves (derived from the dependencies):**
- Wave A: 1, 3, 9, 10
- Wave B: 2, 4, 6, 11
- Wave C: 5, 8, 17
- Wave D: 7, 12, 13, 16, 18, 19
- Wave E: 14, 15
- Wave F: 20

No two independent steps share a file.

## E2E Test Changes
- **Tier 1 and 2:**
  - `go fmt`, `go vet` and `go test -run '^$'` in `api/`, `pkg/archive`, and every service.
  - `go test ./pkg/archive/...`.
  - `make test` in `services/version` and `services/server`.
  - UI: `yarn vitest run`, plus `tsc`.
- **Tier 3 (focused only):**
  - `cd services/agent && make test-e2e E2E_FOCUS="Skill|Agent" KEEP_KIND=1 KIND_CLUSTER=opendepot-focus-agent`
  - `services/version`: focus the narrowest agent or readme Context. Find it with `grep -n -E '(Describe|Context)\("' services/version/test/e2e/e2e_test.go`.
  - `services/server`: focus the browse Context, found the same way.
  - depot, module and provider: tiers 1 and 2 only. They get only the additive CRD field and never read it (see question 13).
- **Chart:** `helm lint chart/opendepot`, then `cd test/chart && GOWORK=off go test ./... -count=1`.
- **UI:** the Playwright workflow.
- No full `make test-e2e` run.

## Helm Chart
- Only the generated CRD copy (step 3).
- No values or RBAC changes. The version controller already manages ConfigMaps for the README, and the server already reads them.

## Out of Scope
- Tilt seeding (`tilt/scripts/seed-resources.sh` does not persist `skillConfigs` or `agentConfigs`), and Tilt not creating `opendepot-github-application-secret`. Both are open questions only.
- No changes to the Trivy seed job, Makefile targets or CI workflows. None are required.
- The CLI output itself. It is only the reference for the Jev colors.
- Showing modules or providers in DepotGraph. Branch mode for modules or providers.

## Overall Acceptance Criteria
- Every Skill or Agent Version that reaches download has a `definitionConfigMapRef`, and its ConfigMap holds the real entry file.
- Versions with no ref are backfilled by the next reconcile or a controller restart.
- `readmeContent` is never an agent body. The UI renders SKILL.md or AGENT.md from `definitionContent`.
- Agents and Skills show the GitHub icon with the repo path, like Modules and Providers do.
- Branch-sourced resources show the ref and short SHA on the card, the hero and the versions table. Tag-sourced resources look the same as today.
- Agent and Skill chips have distinct colors.
- The sidebar shows Registry with Agents, Skills, Modules and Providers under it. Registry lists all kinds by default.
- Jev bars use the CLI's thresholds.
- The archive fix is hardened, tested and Security Reviewed. All six CRD copies are identical.

## Open Questions (most important first; each has a recommendation, none block the plan)
1. **Agent file name and title.** Agents use `<name>.agent.md` or `<name>.md`, not `AGENT.md`. *Recommend:* title the card "AGENT.md" as you asked, show the real file name as a caption, and store the real name in `fileName`.
2. **Frontmatter display.** *Recommend:* store the raw file. In the UI, show the frontmatter in a code block above the rendered body.
3. **Real README for agents.** Should a `README.md` next to the definition fill `readmeContent`? *Recommend:* not in 0.12.0. Delete and clear the stale `-readme` ConfigMaps on agent Versions.
4. **Root cause of skill-aspire losing its ref.** It is unverified. *Recommend:* check managedFields before coding step 5. The early write plus backfill fix it either way.
5. **Skill and Agent colors.** *Recommend:* Skill purple `#a371f7`, Agent magenta `#db61a2`. Both avoid the red, yellow and green used for Jev severity and warnings.
6. **Remove the Kind chip filter** now that the sidebar does the filtering? *Recommend:* yes, to avoid two controls doing the same thing.
7. **Discrete or continuous Jev gradient.** *Recommend:* three discrete bands, matching the CLI exactly, and align `Low` to green.
8. **Show Pending or Held branch Versions** (no semver yet) in the versions table? *Recommend:* not in this release. Show the phase only on assigned Versions.
9. **Repo path text and deep link.** *Recommend:* the icon links to the repo root, the same as modules. The ref chip deep-links to the tree path and the commit.
10. **Collapsed sidebar.** *Recommend:* show the four children as icons with tooltips.
11. **Tilt seed.** Should `seed-resources.sh` persist `skillConfigs` and `agentConfigs`? *Recommend:* yes, as a separate small follow-up PR.
12. **Tilt GitHub App secret.** *Recommend:* have Tilt create it from the `OPENDEPOT_GH_*` environment variables when they are set, in a follow-up.
13. **Tier 3 for depot, module and provider.** *Recommend:* skip it. The CRD change is additive and CI runs the full suite.
14. **Ship the symlink fix as its own commit?** *Recommend:* yes, as the first commit, so the Security Review has a clean diff.