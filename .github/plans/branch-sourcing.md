# Implementation Plan v2: Branch-Based Sourcing for Skills and Agents

Repo: tonedefdev/opendepot, branch `feat/agent-registry`. This plan replaces `branch-sourcing-plan.md` (v1) and stands on its own.

## Summary
A Skill or Agent source can now follow a branch instead of tags. Setting `ref` on an `AgentSourceConfig` turns on branch mode for that source. Tags stay the default.

Each controller has one job in branch mode:

- **Depot controller.** It makes one decision: has the newest commit that touched the source `path` on the branch changed? If so, it adds a pending entry (commit SHA and timestamp) to the Skill's or Agent's `spec.versions`. It never classifies changes and never picks a semver.
- **Agent controller.** It creates a Version named after the commit SHA for each pending entry. After the version controller assigns a semver, it copies that semver back to the parent's entry and to `status.versionRefs`.
- **Version controller.** It does the rest, in this order:
  1. Downloads the archive at that exact SHA.
  2. Runs all existing checks: Trivy, then the agentspec/Expr rules, then the Jev safety check, then signing.
  3. Decides the bump. The first version is always `1.0.0`. With no Jev, the bump is PATCH. With Jev, it sends Jev a diff of the previous and new trees, after a secret scan of both, and gets MAJOR, MINOR, or PATCH back.
  4. Writes the semver to `spec.version` once. It also records the commit and the bump result in the Version status.

The server and the CLI keep addressing Versions by semver. A Version is never listed or served until it has a semver and has passed every check.

## Affected Services
| Service / package | Change |
|---|---|
| `api/v1alpha1` | New fields: `ref`, `branchPolicy`, `commit`/`discoveredAt`/`state` on version entries, Version `sourceCommit`, `bumpAssessment`, `branchPhase`. New CEL rules. |
| `pkg/github` | Add `ValidateRef`, `ValidateCommitSHA`, `LatestPathCommit`, and an `ErrRefNotFound` sentinel. |
| `pkg/jev` | Add `BumpQuestions`, `ClassifyBump`, and a shared score parser. |
| `services/depot` | Add branch head detection, append-only merging of entries, and the `--branch-sourcing-enabled` flag. Polling uses the existing `spec.pollingIntervalMinutes`. |
| `services/agent` | Create SHA-keyed Versions. Copy assigned semvers back to the parent. Update trimming and deletion for branch entries. |
| `services/version` | Download by SHA, add the branch flag check, assign the semver, classify the bump, and fix the Trivy JSON check (D9). |
| `services/server` | Look up a Version through the parent's `versionRefs[].name` when the semver-derived name doesn't exist. |
| `cli` | No change. |
| `chart/opendepot` | Sync CRDs, add `branchSourcing.*` values and flags for the depot and version deployments. Do not bump Chart.yaml. |

## Recorded decisions and assumptions
| ID | Decision | Status |
|---|---|---|
| D1 | The Depot only detects that the head SHA changed and records a pending entry. The version controller assigns the semver after classification. Naming and status flow are below. | Confirmed (revised) |
| D2 | The Jev bump thresholds are 0.5 for major and 0.5 for major plus minor combined. A confidence below 0.5 sets `needsReview`, which is informational only. | Assumed (original recommendation) |
| D3 | One `--branch-sourcing-enabled` flag, off by default, set from one chart value. The depot and the version controller both honor it. | Confirmed |
| D4 | `minVersionInterval` defaults to 10 minutes. Commits between polls collapse into one version per head the depot sees. | Assumed |
| D5 | Polling is opt-in and uses the existing `spec.pollingIntervalMinutes`. Unset means no requeue, the same as today. Branch sources are intended as a one-time migration that is then managed through GitOps. No new flag or chart value. | Confirmed |
| D6 | If Jev errors, hold and retry. Never fall back to PATCH silently. If Jev is disabled on the controller, use PATCH with source `Default`. | Assumed |
| D7 | E2E fixtures come from `github/awesome-copilot` (`agents/` and `skills/<one-skill>/`). The older seed state is pinned to a fixed SHA. Tests assert only what holds for any current head. | Confirmed |
| D8 | The lock file is unchanged. | Assumed |
| D9 | Fix the `json.Valid` gap in the shared Trivy parse path. | Assumed |
| D10 | In branch mode, `versionHistoryLimit` defaults to 10 when unset. | Assumed |
| D11 | The commit SHA reaches the Version through `spec.sourceCommit`. The commit and the bump result are recorded in `VersionStatus`. | Accepted. The bump result is no longer on the Version spec, because the version controller now computes it. |
| Chart | Do not bump `Chart.yaml` `version` or `appVersion`. | Confirmed |

---

## Naming and status flow for branch Versions (D1)

### Name
A branch Version is named `<prefix>-<configName>-<40-char commit SHA>`, for example `skill-code-review-3f2a…9c1d`.
- The name is fixed when the Version is created and never changes.
- It can't collide with a tag-mode name, because those contain `-` between number segments and a hex SHA never does. A source also can't be in both modes.
- It contains no `.`, so the version controller's name check still passes.
- The storage key doesn't use the semver. `getVersionFilePath` uses `<type>-<name>/<uuid>.tar.gz`, so a SHA-keyed name doesn't affect storage.
- Each branch Version also gets a `opendepot.defdev.io/commit: <sha>` label, so `kubectl get versions` can filter by commit.

### Lifecycle
| Step | Who | Parent `spec.versions[i]` | Version `spec.version` | Version `status.branchPhase` | Listed or served? |
|---|---|---|---|---|---|
| 1. New head seen | Depot | added: `{commit, discoveredAt, state: Pending}`, `version` empty | — | — | No |
| 2. Version created | Agent controller | unchanged | `""` | not set yet | No |
| 3. Checks running, waiting for an earlier Version, or Jev hold | Version controller | `Pending` | `""` | `Pending` / `Held` | No |
| 4a. A check blocks it | Version controller | copied to `state: Rejected` | `""` (no semver used up) | `Rejected` | No |
| 4b. All checks and signing pass, bump decided | Version controller | copied to `version: X.Y.Z`, `state: Assigned` | `X.Y.Z` (write-once) | `Assigned`, `synced: true` | Yes |
| 5. Parent status | Agent controller | `status.versionRefs["X.Y.Z"] = {name: <SHA Version name>, version, commit, fileName}` | — | — | Yes |

**Rules:**
- **Write-once semver.** CEL allows `spec.version` to go from empty to a value once. After that it can't change. `spec.sourceCommit` can't change at all.
- **At most one pending entry per source.** While the newest entry is `Pending`, the depot doesn't compare heads. This keeps the depot's single decision from piling up entries behind a Jev hold. It also means the starting point for classification is always the last assigned Version.
- **Starting point for the bump.** It is the highest semver among sibling Versions (same parent label) that have a `sourceCommit` and an assigned version. That sibling's `sourceCommit` is the "previous commit". If there is no such sibling, the Version gets `1.0.0` with source `Initial` and no Jev call. A rejected first commit doesn't use up `1.0.0`.
- **Guards in the version controller.** These protect against Skills created by hand outside the Depot. Both checks read the API server directly, not the cache:
  - Wait while an older sibling that isn't rejected is still unassigned.
  - Before writing the semver, check that no sibling already has it.
  - If the Version is no longer the newest entry on its parent, mark it `Rejected` with reason `Superseded` instead of assigning it.
- **Assignment comes after signing.** The SHA256SUMS file names the UUID file, not the semver, so signing doesn't depend on the version. Doing assignment last means a semver is only used once the Version is fully publishable.
- **Re-runs don't reclassify.** If `spec.version` is already set, classification is skipped. `forceSync` downloads the same SHA again, which gives the same checksum, so `immutable` still holds.
- **Recreating a Skill or Agent resets history.** Deleting it garbage-collects its Versions, and numbering restarts at `1.0.0`. Consumers' lock hashes then fail to match, so installs stop rather than pull different content (fail closed). This goes in the docs.

### How the server lookup still works
- `agentVersionResourceName` stays as it is and is still tried first. Tag mode is unchanged and costs no extra API call.
- **Download, archive, SHA256SUMS, and signature requests** (all go through `getAgentVersionForRequest`):
  1. GET the Version by its semver-derived name.
  2. On NotFound, GET the parent Skill or Agent. Find the `status.versionRefs` entry whose `version` matches the requested version (with any leading `v` removed). GET the Version by that entry's `name`.
  3. In this fallback path, the server also requires:
     - `spec.version` matches the requested version;
     - the kind label `opendepot.defdev.io/<kind>` equals the parent name;
     - the existing `agentVersionServable` check passes (synced, not blocked, correct type, `agentSourceRef.name` matches).

  A tampered ref therefore can't point to another Version.
- **The versions list** (`buildAgentVersionsResponse`):
  - `agentParent` now also decodes `name` from each `versionRefs` entry.
  - The Version is looked up by `entry.name` when set, otherwise by the derived name.
  - The server also requires `spec.version` to match `entry.version`.
  - Pending, held, and rejected Versions are never in `versionRefs`, so they never appear.

### How the CLI's semver lookup still works
No change is needed:
1. The CLI calls `/versions`, which lists only assigned, synced, unblocked semvers.
2. It applies the consumer's own constraint locally (`cli/constraint.go`).
3. It requests `/{semver}/download`, and the server resolves the semver as above.
4. It checks the signed SHA256SUMS and the hash, then writes `version` and `hashes` to the lock file (D8).

A commit SHA is never addressable through the API.

### Jev-unavailable path
| Case | Result |
|---|---|
| No `jevSecretRef` on the source | PATCH, source `Default`, reason "no jevSecretRef" |
| `jevSecretRef` set, Jev disabled on the controller (`--jev-enabled=false`) | PATCH, source `Default`, reason "jev disabled on controller" (D6) |
| Jev configured but failing (API error, timeout, missing or empty token, integrity mismatch) | Option A: hold and retry with capped backoff (1, 2, 4, 8, then 15 minutes). An operator can unblock it. See below. |

Options for the failing case:
- **A (recommended): hold indefinitely, with an operator override.**
  - `branchPhase: Held`, `synced: false`, and the redacted error goes in `bumpAssessment.error`.
  - Retries use capped exponential backoff: 1, 2, 4, 8, then 15 minutes, with the attempt count stored in `bumpAssessment.attempts`.
  - The depot doesn't add new entries while this one is pending.
  - Ways out:
    - fix Jev or the token;
    - set `forceSync: true` to retry now;
    - annotate the Version with `opendepot.defdev.io/bump-override: major|minor|patch`. This records source `Manual` and is honored only while the Version is `Pending` or `Held`. It sets the bump size only. Trivy, agentspec, the Jev safety check, and signing still apply.
- **B: hold for a fixed time (for example 1 hour), then MAJOR** with `needsReview`, source `Fallback`.
- **C: hold for a fixed time, then PATCH** with `needsReview`. This could hide a breaking change.

A is recommended. Like the D6 assumption, it fails closed, no semver is ever a machine guess, and the hold is visible on the Version. The override is the deliberate human exit. If B or C is chosen, drop the override annotation and add a hold-duration field to `BranchPolicy`.

### How the secret-scan rule still holds (Trivy before Jev)
- Classification runs only after `runAgentScan` on the new tree returns no blocking finding. That scan covers vuln, secret, and misconfig, plus agentspec and Expr. The existing Jev safety check (`runAgentJev`) and signing also run first.
- The previous tree is downloaded again by its SHA. Its checksum must equal the previous Version's `status.checksum`; a mismatch is a hold error. It then gets a secret-only Trivy scan (`--scanners secret`, under `scanSem`, same offline and cache flags).
- `agentContent` gets a `flaggedFiles` set, filled only by the scan functions. The Jev input builder accepts only `agentContent` values, so content can't reach Jev without being scanned. A file flagged in either tree appears only as `<path> (content withheld)`.
- If the entry file is flagged in either tree, nothing is sent to Jev. The result is MAJOR with source `Withheld` and `needsReview: true`. This always reaches a result, so it can't get stuck, and it picks the cautious bump.
- Unit tests check that the fake Jev server gets zero requests whenever an entry file is flagged, and that no flagged file's content appears in the input.

---

## API / CRD Changes (`api/v1alpha1/types.go`)

**`AgentSourceConfig`** (around L364)
- `Ref *string` with JSON `ref,omitempty`. Markers: `+optional`, `MinLength=1`, `MaxLength=255`, `Pattern=^[A-Za-z0-9._+/-]+$`. The real check is still `validRefName` in Go.
- `BranchPolicy *BranchPolicy` with JSON `branchPolicy,omitempty`, `+optional`.
- Type-level `+kubebuilder:validation:XValidation` rules. They use `has()` and `size()` only, never string equality, so CEL cost stays fixed:
  1. `!has(self.ref) || !has(self.versionConstraints) || size(self.versionConstraints) == 0`, message "ref and versionConstraints are mutually exclusive".
  2. `!has(self.ref) || !has(self.tagPrefix)`, message "tagPrefix is not used with ref".
  3. `!has(self.branchPolicy) || has(self.ref)`, message "branchPolicy requires ref".
- These rules apply wherever the type is embedded: Depot `skillConfigs`/`agentConfigs`, `Skill.spec`, `Agent.spec`, and `Version.spec.agentSourceRef`. If the apiserver or controller-gen reports a CEL cost-budget error for the Depot lists, add `+kubebuilder:validation:MaxItems=500` to those two lists.
- Update the `VersionConstraints` doc comment to say it is tag-mode only.

**New `BranchPolicy`.** It classifies only and never blocks. Defaults are set both by markers and by Go constants, so they apply when the whole policy is omitted.
- `MinVersionInterval *metav1.Duration`, `+kubebuilder:default="10m"`. `0s` turns it off. Negative values are rejected in Go.
- `MajorThreshold *float64`, `Minimum=0`, `Maximum=1`, default `0.5`.
- `MinorThreshold *float64`, `Minimum=0`, `Maximum=1`, default `0.5`. This is compared against P(major) + P(minor).
- `MinConfidence *float64`, `Minimum=0`, `Maximum=1`, default `0.5`. Below this, `needsReview` is set.

**`SkillVersion` and `AgentVersion`.** These types are shared by `spec.versions` and `status.versionRefs`.
- `Commit string` with JSON `commit,omitempty`, `Pattern=^[0-9a-f]{40}$`.
- `DiscoveredAt *metav1.Time` with JSON `discoveredAt,omitempty`. This is the ordering key and is used for D4.
- `State string` with JSON `state,omitempty`, `Enum=Pending;Assigned;Rejected`. Spec only.
- Fix the doc comment on `Name`: it holds the Version resource name, not the skill name.
- Update the `SkillSpec.Versions` and `AgentSpec.Versions` doc comments for branch entries, whose `version` is empty until assigned.

**`VersionSpec`** (around L744)
- `SourceCommit string` with JSON `sourceCommit,omitempty`, `Pattern=^[0-9a-f]{40}$`, plus field-level `XValidation` `self == oldSelf`, message "sourceCommit is immutable".
- `Version` stays `json:"version"`. Update its comment to say it is empty until assigned for branch Versions.
- Type-level XValidation rules on `VersionSpec`:
  1. `size(self.version) > 0 || has(self.sourceCommit)`, message "version is required unless sourceCommit is set".
  2. `!has(self.sourceCommit) || (has(self.agentSourceRef) && has(self.agentSourceRef.ref))`, message "sourceCommit requires agentSourceRef.ref".
  3. Transition rule: `!has(oldSelf.sourceCommit) || size(oldSelf.version) == 0 || self.version == oldSelf.version`, message "version is write-once for branch Versions".

**`VersionStatus`** (around L773)
- `SourceCommit string` with JSON `sourceCommit,omitempty`: the SHA actually downloaded.
- `BumpAssessment *BumpAssessment` with JSON `bumpAssessment,omitempty`.
- `BranchPhase string` with JSON `branchPhase,omitempty`, `Enum=Pending;Held;Assigned;Rejected`.

**New `BumpAssessment`.** Never holds the token or prompt content.
- `Commit`, `PreviousCommit`, `PreviousVersion` (string).
- `Category`, `Enum=Major;Minor;Patch`.
- `Source`, `Enum=Initial;Default;Jev;Withheld;Manual`.
- `Reason`, `Model`, `EvaluatedAt` (RFC3339 string).
- `MajorProbability`, `MinorProbability`, `PatchProbability`, `Confidence` (`*float64`, `+optional`).
- `NeedsReview` (bool), `Attempts` (int), `Error` (redacted string).

Not added (they were part of the v1 Depot handshake and are no longer needed): `branchHead` and `branchBump` on the parent, and `Bump` on version entries.

---

## Implementation Steps

### Phase 1 — API types and CRD regeneration
1. Make every type change above in `api/v1alpha1/types.go`.
2. Regenerate deepcopy for the api module, then run `make manifests generate` in `services/depot`, `services/agent`, and `services/version`.
3. Copy the regenerated `skills`, `agents`, `depots`, and `versions` CRDs from each service's `config/crd/bases` into `chart/opendepot/crds/`.
4. Add envtest unit tests in `services/depot/internal/controller` that check:
   - admission rejects `ref` with `versionConstraints`, `ref` with `tagPrefix`, and `branchPolicy` without `ref`;
   - on a Version, rewriting a non-empty `spec.version` or `sourceCommit` is rejected;
   - on a Version, `""` → `"1.0.0"` is allowed.

**Acceptance:** CRDs regenerate cleanly, the chart CRDs match the generated ones, and the envtest cases pass.

### Phase 2 — Head detection (depot) and SHA-keyed Versions (agent)
1. **`pkg/github/github.go`**
   - Export `ValidateRef(ref) error` as a wrapper around `validRefName`.
   - Add `ValidateCommitSHA(sha) error` (40 lowercase hex characters).
   - Add `LatestPathCommit(ctx, client, owner, repo, ref, path) (sha string, err error)`. It calls `Repositories.ListCommits` with `SHA: ref`, `Path: cleaned`, and `PerPage: 1`.
     - It checks owner, repo, ref, and path before calling. The path check rejects `..` segments and strips the leading `/`; empty means the repo root.
     - It returns `ErrRefNotFound` for 404 or 422 and for an empty result, and validates the returned SHA.
   - Tests in `github_test.go`: table tests with an `httptest` GitHub, plus new `TestValidRefName` cases (nested slashes, a SHA as input, `..`).
2. **`services/depot/cmd/main.go`.** Add the flag `--branch-sourcing-enabled` (bool, default false) and pass it into `DepotReconciler`. There is no poll-interval flag.
3. **`services/depot/internal/controller/opendepot_depot_controller.go`**
   - In the skill and agent loops, if `config.Ref != nil`, call a new shared `reconcileBranchSource`. Otherwise run the existing tag path (`matchAgentSourceVersions`) unchanged.
   - Check mutual exclusion again in Go, as a second line of defense.
   - **Flag off:** skip the source, log an error that names the source, and don't create or update its Skill or Agent.
   - **Validation before any GitHub call:** `ValidateRepoName` (owner and the repo from `AgentSourceRepoName`), `ValidateRef`, the path check, and, if set, `repoUrl` must be `https://github.com/<owner>/<repo>` with valid segments. This fixes the earlier LOW finding for branch mode.
   - **The decision** (the depot's only decision):
     1. Get the head with `LatestPathCommit`.
     2. Read the current Skill or Agent. If it doesn't exist, create it with one entry: `{commit, discoveredAt: now, state: Pending}`.
     3. Find the newest entry by `discoveredAt`. No-op if any of these is true:
        - its state is `Pending`;
        - the head SHA already appears in any entry;
        - less time than `minVersionInterval` has passed since its `discoveredAt`. In this case also record the remaining time for the requeue.
     4. Otherwise add a new pending entry.
   - **Merge without overwriting:** inside `RetryOnConflict`, read the parent fresh, set `spec.agentSourceConfig`, and only add entries. Never change `version` or `state` on existing entries, because the agent controller owns those fields.
   - **No assignment:** the depot never sets `version` or computes a bump.
   - **Errors:** on `ErrRefNotFound` or a GitHub error, log, leave the parent unchanged, and continue with the other sources. Don't abort the whole Depot reconcile.
   - **Requeue:** only when `spec.pollingIntervalMinutes` is set, use it, as the existing code does. Unset means no polling. Separately, if a branch source is inside `minVersionInterval`, requeue for the remaining time.
   - Unit tests (`opendepot_depot_controller_test.go`, fake GitHub):
     - first sighting adds one pending entry with no version;
     - a known head is a no-op;
     - a pending newest entry is a no-op;
     - the interval holds back a new entry and sets the requeue time;
     - a new head after an assigned or rejected entry is added;
     - a force-push back to a known SHA is a no-op;
     - with the flag off, no parent is created;
     - a branch that doesn't exist leaves the parent unchanged while other sources still reconcile;
     - an invalid `repoUrl` is rejected;
     - the merge keeps `version` and `state` written by others.
4. **`services/agent/internal/controller/agent_versions.go`, `opendepot_skills_controller.go`, `opendepot_agents_controller.go`**
   - `reconcileAgentVersions` takes the entries, not `[]string`.
     - A branch entry (with `commit`) gets the name from `agentVersionName(kind, configName, commit)`. It is created with `Spec.Version: ""`, `Spec.SourceCommit: commit`, and the commit label.
     - Tag entries are unchanged.
     - The result map is keyed by the Version resource name.
   - **Copying results back to the parent:** for each branch entry, read its Version.
     - If `spec.version` is set, set the entry's `version` and `state: Assigned`.
     - If `status.branchPhase == Rejected`, set `state: Rejected`.
     - Write the parent spec only when something changed, inside `RetryOnConflict` with the parent read fresh.
   - **`status.versionRefs`:** include tag entries as today, plus branch entries only once assigned, keyed by semver, with `name` set to the SHA Version name and `commit`. `latestVersion` uses assigned semvers only.
   - **Trimming (D10):** the limit is `versionHistoryLimit`, or 10 when `ref` is set and the limit is unset. Keep:
     - the newest N assigned entries by semver;
     - any `Pending` entry;
     - the newest entry if it is `Rejected`.

     Drop older `Rejected` entries. Trimming must keep every entry field (fixes the current bug that rebuilds `{Version: v}` only) and keep `discoveredAt` order.
   - **Deletion:** `reconcileAgentVersionRemovals` takes a set of Version names to keep instead of version strings. Tag mode works the same way, since its names are derived from the semver.
   - **Watch:** in the `Owns(&Version{})` predicate, `UpdateFunc` returns true only when the new Version has `spec.sourceCommit` and either `spec.version` or `status.branchPhase` changed. Create, Delete, and Generic stay false.
   - Unit tests (`agent_versions_test.go` and both controller tests):
     - SHA naming and the spec fields;
     - copying back on assignment and on rejection;
     - `versionRefs` keyed by semver with the SHA name;
     - unassigned entries never appear in `versionRefs`;
     - trimming keeps fields and the pending entry;
     - default limit 10 in branch mode;
     - deletion by name;
     - tag mode unchanged.

**Acceptance:** with a fake GitHub, a branch source produces one pending entry and one SHA-named Version with empty `spec.version`. Manually assigning `1.0.0` on that Version is copied to the parent entry and to `versionRefs["1.0.0"].name`. All tag-mode tests pass unchanged.

### Phase 3 — Version controller: download by SHA, checks, semver assignment (no Jev yet)
1. **`services/version/cmd/main.go`.** Add `--branch-sourcing-enabled` (default false) and set `VersionReconciler.BranchSourcingEnabled`. Also give the reconciler `mgr.GetAPIReader()` for the uncached sibling checks.
2. **`opendepot_versions_controller.go`**
   - **Flag check:** right after the existing name check, if `spec.sourceCommit` is set and the flag is off, write a terminal status ("branch sourcing is disabled on the version controller"), set `branchPhase: Rejected`, and don't requeue. This also covers Skills created by hand outside the Depot.
   - **Ordering check:** wait for older unassigned siblings that aren't rejected (status "Waiting for <name>", requeue 30s), then run the superseded check. Both read through the API reader and run before the download.
   - **Download:** refactor `fetchAgentArchive` into `fetchAgentArchiveAt(ctx, version, ref)`.
     - Tag mode keeps its `TagCandidates` loop.
     - Branch mode calls `ValidateCommitSHA(spec.sourceCommit)` and downloads only at that SHA, never by branch name. A missing or invalid SHA gives a terminal status.
   - **Status on block or failure:** `persistScanViolation` and `persistAgentBlock` set `branchPhase: Rejected` for scan, agentspec, Expr, or Jev-safety blocks on branch Versions. They set `Held` for retryable failures (`JevUnavailable`, `SigningUnavailable`).
   - **Assignment:** after signing succeeds and if `spec.version` is empty, call a new `assignBranchVersion`. It:
     1. finds the starting point (see the naming section);
     2. picks the bump. In this phase the choices are `Initial` (`1.0.0`), `Manual` (override annotation), and `Default` (PATCH, when there is no `jevSecretRef` or Jev is disabled). Phase 4 adds Jev;
     3. computes the next semver with the version library the module already uses, preferring `hashicorp/go-version` segments;
     4. checks uniqueness through the API reader;
     5. writes `spec.version` with `RetryOnConflict`.
   - **Final status update:** also set `status.sourceCommit`, `status.bumpAssessment`, and `status.branchPhase: Assigned`. Don't set `Synced: true` on a branch Version until `spec.version` is set.
3. **D9, `scanner.go`.** Add a shared `decodeTrivyReport` used by both `parseTrivyReport` and `parseTrivySecrets`. It rejects:
   - output that fails `json.Valid`;
   - a top level that isn't a JSON object, including `null`;
   - a missing or zero `SchemaVersion`.

   This fails closed and is used by both the existing scan and the new secret-only scan.
4. Tests (`agent_versions_test.go`, `agent_entry_test.go`, `opendepot_versions_controller_test.go`, `scanner` tests):
   - download by SHA;
   - a missing SHA gives a terminal status;
   - flag off gives a terminal status;
   - waiting on an older sibling, and the superseded case;
   - `Initial` → `1.0.0`;
   - `Default` → PATCH;
   - `Manual` override → each category, ignored once assigned;
   - semver uniqueness;
   - a blocked branch Version uses no semver and gets `Rejected`;
   - downloading the same SHA twice gives the same checksum with `immutable` on;
   - a `forceSync` re-run doesn't reclassify;
   - the D9 cases: `null`, an array, invalid JSON, and a missing `SchemaVersion` are all errors.

**Acceptance:** a branch Version with no `jevSecretRef` downloads its SHA and passes every check. It gets `1.0.0` the first time and `1.0.1` for the next entry, and its status shows `sourceCommit`, `bumpAssessment`, and `branchPhase: Assigned`.

### Phase 4 — Jev bump classifier and the secret rule
1. **`pkg/jev/questions.go`.** Add `BumpQuestions(kind)`:
   - `bump`: type `score`, criteria `["Patch","Minor","Major"]`. Instructions in plain words:
     - Major: a complete change from the earlier scope or behavior, such as removing existing tools or processes, or adding six or more.
     - Minor: changed instructions that add a feature or capability.
     - Patch: a change to `model`, or edits that don't change behavior.
   - Three informational `noul` questions: `bump_removes_capability`, `bump_adds_capability`, `bump_non_behavioral`.
2. **`pkg/jev/client.go`.**
   - Generalize `parseRisk` into a shared score parser that returns the probability for each label plus the confidence. `Assess` keeps its current output.
   - Add `ClassifyBump(ctx, token, kind, state) (*BumpResult, error)`. It reuses the request path, retries, redaction, and the `MaxStateBytes` limit.
   - Tests in `client_test.go` with new `testdata` fixtures.
3. **New `services/version/internal/controller/branch_bump.go`.**
   - **`classifyBranchBump`** runs inside `assignBranchVersion` when the source has `jevSecretRef` and Jev is enabled:
     1. Download the previous tree at the previous commit with `fetchAgentArchiveAt`. Its checksum must match the previous Version's `status.checksum`.
     2. Run a secret-only scan on the previous tree.
     3. Reuse the new tree's flagged set from `runAgentScan`. Extend `agentContent` with `flaggedFiles`; `entryFlagged` stays as it is.
     4. If the entry file is flagged in either tree, return MAJOR with source `Withheld`.
     5. Otherwise build the Jev input in `buildBumpState`:
        - a facts header: frontmatter tools added and removed (with counts), the model change, the description change, and the added, removed, and modified file list;
        - a unified diff of files not flagged in either tree;
        - cut at a UTF-8 boundary to fit `MaxStateBytes`.
     6. Read the token with `readJevToken` and call `ClassifyBump`.
     7. Apply the thresholds:
        - MAJOR if P(major) ≥ `majorThreshold`;
        - otherwise MINOR if P(major) + P(minor) ≥ `minorThreshold`;
        - otherwise PATCH.

        `needsReview` is set when confidence < `minConfidence`.
   - **On a Jev error:** `branchPhase: Held`, redacted error, `attempts++`, requeue with the backoff from option A (1, 2, 4, 8, then 15 minutes), and no fallback (D6).
   - **Dependency:** unified diff via `github.com/pmezard/go-difflib` if `go list -m` shows it is already in `services/version`'s module graph. If it isn't, add it to `services/version/go.mod`.
4. Unit tests in `branch_bump_test.go` with a fake Jev server:
   - each category at the exact threshold edges;
   - `needsReview` below 0.5 confidence;
   - Jev disabled gives PATCH `Default`;
   - an error causes a hold, with backoff that grows and is capped;
   - an entry flagged in either tree gives `Withheld` MAJOR with zero Jev requests;
   - a flagged non-entry file's content never appears in the request;
   - a checksum mismatch on the previous tree causes a hold;
   - truncation stays within the limit and on a UTF-8 boundary;
   - an assigned Version is never sent to Jev again.

**Security review checkpoint (required before Phase 6):**
- No unscanned content reaches Jev.
- The ref, SHA, path, and `repoUrl` are validated before every GitHub call.
- The token is never logged.
- The flag is off by default in both controllers.
- Pushing to the branch can't skip any check.
- Write-once `version` and immutable `sourceCommit` are enforced by CEL.
- Server resolution through the parent can't be pointed at another Version.
- Churn: a rapid series of pushes produces at most one entry per interval, and none while an entry is pending.

### Phase 5 — Server lookup, CLI, lock file
1. **`services/server/agents.go`**
   - `agentParent`: decode `name` (and `commit`) from each `versionRefs` entry.
   - `buildAgentVersionsResponse`: use `entry.name` when set, otherwise `agentVersionResourceName`, and require `spec.version` to match `entry.version` (leading `v` removed on both).
   - `getAgentVersionForRequest`: try the derived name first. On NotFound, use the fallback through the parent with the extra checks (see "How the server lookup still works"). HTTP status codes stay the same (404, 403, 500). All four handlers that call it get this behavior.
   - Read through `ui_browse.go` (and any other caller that derives agent Version names or reads `spec.version`). Make sure Versions with empty `spec.version` or not synced are never shown, and that branch Versions resolve through `versionRefs[].name`.
2. Tests in `agents_test.go` and `ui_browse_agent_test.go`:
   - a SHA-named Version resolves by semver for download, archive, SHA256SUMS, and signature;
   - a parent ref pointing to another Skill's Version, or with a mismatched `spec.version`, gives 404;
   - a pending (unassigned) Version is not listed;
   - tag-mode lookups make no extra parent GET.
3. **CLI and lock file:** no code change (D8). The CLI only uses semvers from `/versions` and `/{semver}/download`, as described above.

### Phase 6 — Helm chart
- **`values.yaml`:** add a top-level `branchSourcing` block with `enabled: false` only. It is top level because both the depot and the version controller read it.
- **`templates/depot-deployment.yaml`:** always render `args`. Today `args` only renders when `zapLogLevel` is set. Add `--branch-sourcing-enabled={{ .Values.branchSourcing.enabled }}`, and keep `--zap-log-level` conditional.
- **`templates/version-deployment.yaml`:** add `--branch-sourcing-enabled={{ .Values.branchSourcing.enabled }}` to the version-controller container args.
- **RBAC:** no changes needed.
  - The version controller already has get/list/watch on `skills` and `agents`, and update on `versions`. It no longer writes to Skill or Agent objects.
  - The agent controller already updates `skills`, `agents`, and their status, and watches `versions`.
  - The depot already updates `skills` and `agents`.
- CRDs were synced in Phase 1.
- **Do not change `Chart.yaml`** `version` or `appVersion`.
- If the chart README has a values table, add the new values there.

### Phase 7 — E2E tests
Failures must not be skipped, and the Jev variant must fail if the existing Jev key env var is missing.

**Fixtures (D7):**
- Repo `github/awesome-copilot`, branch `main`.
- **Agent source:** `path: agents`, with `name` set to one agent whose entry file `agentspec.FindAgentEntry` can find.
- **Skill source:** `path: skills/<one-skill-dir>`. A skill source must point at one directory with `SKILL.md` at its root, so `skills/` itself can't be the path.
- **Pinned fixtures (assigned, do not re-derive):**
  - Skill: `path: skills/acquire-codebase-knowledge`, pinned SHA `b8f38227480c9f3fe04d6496d4fcab9a880e5a15` (2026-04-14, the second result of `commits?sha=main&path=skills/acquire-codebase-knowledge`). Its `SKILL.md` exists at that SHA. `main` head at pinning time: `9ce814859eaa473178a1463ee3aa0c54a8860b86`.
  - Agent: `path: agents`, `name: oracle-to-postgres-migration-expert`, pinned SHA `41892f2b635130901532c576b90bdc09c8aad49c` (2026-10-07, the second result of `commits?sha=main&path=agents`). `agents/oracle-to-postgres-migration-expert.agent.md` exists at that SHA (14,411 bytes) and at `main`.
  - The pinned tree must still pass agentspec. The e2e suite verifies this, and a failure is a fixture bug to fix, not a skip.
  - Store the SHAs as named constants, with a comment explaining the choice.
- The pinned tree must pass agentspec. Store the SHAs as named constants with a comment explaining why they were chosen.
- Each spec first checks that the path still exists at `main` (one `gh api` or HTTP call). If it doesn't, the spec fails with "fixture removed upstream" rather than skipping.
- Use `branchPolicy.minVersionInterval: 0s` in the tests. Trigger depot reconciles by changing an annotation on the Depot.
- Enable the feature with `helm upgrade --reuse-values --set branchSourcing.enabled=true --wait`, following the existing Helm-upgrade pattern in the e2e suites.

**Stable assertions (true for any current head):**
- `services/depot/test/e2e/e2e_test.go`:
  1. `ref` together with `versionConstraints` is rejected at admission.
  2. With `branchSourcing.enabled=false` (the chart default), a Depot with a ref source creates no Skill.
  3. After enabling, the first sighting creates exactly one entry: 40-hex `commit`, `state: Pending`, empty `version`. This checks that the Depot never assigns a version.
  4. Triggering the depot again while that entry is pending adds no entry.
- `services/agent/test/e2e/e2e_test.go`:
  1. A Skill with a pending entry (pinned SHA) gets a Version named `skill-<name>-<sha>` with `spec.sourceCommit`, empty `spec.version`, and the commit label.
  2. Patching that Version's `spec.version` to `1.0.0` and its status to `branchPhase: Assigned` (via `--subresource=status`) is copied to the parent entry (`version: 1.0.0`, `state: Assigned`) and to `status.versionRefs["1.0.0"].name`.
  3. A second patch of `spec.version` is rejected (write-once).
- `services/version/test/e2e/e2e_test.go`, full chart:
  1. **Seed:** create the Skill directly with one pending entry at the pinned SHA. It reaches `branchPhase: Assigned` with `spec.version: 1.0.0`, `bumpAssessment.source: Initial`, and `status.sourceCommit` equal to the pinned SHA.
  2. **Progression:** create a Depot pointing at the same source. It adds a second entry whose commit is 40-hex and not the pinned SHA.
     - That Version reaches a terminal phase, either `Assigned` or `Rejected`.
     - If `Assigned` with no Jev: `version: 1.0.1`, `source: Default`, `category: Patch`.
     - If `Rejected`: no semver is used, and the server's `/versions` doesn't list it.
  3. **Jev variant** (separate Skill with `jevSecretRef`): the second Version reaches `Assigned`, `Rejected`, or `Held`.
     - If `Assigned`: `source` is `Jev` or `Withheld`; the version is one of `1.0.1`, `1.1.0`, `2.0.0` and matches `category`; for `Jev`, `model` is non-empty and the three probabilities are each in [0,1] and sum to about 1.
     - No specific category is asserted.
  4. **No duplicates:** after another depot trigger, no two entries share a commit, and assigned semvers are unique and increasing in `discoveredAt` order. Exact entry counts are not asserted.
  5. **Server**, through the existing port-forward:
     - `/versions` includes `1.0.0`;
     - `/1.0.0/download` resolves the SHA-named Version;
     - `shasum` matches the Version's `status.checksum`, decoded to hex;
     - SHA256SUMS and its signature are served.
  6. **Same SHA, same checksum:** `forceSync` on the `1.0.0` Version keeps the same `status.checksum` and the same `spec.version`.
- The exact `1.0.1` arithmetic, threshold edges, the secret-withholding rule, and the hold or backoff behavior all depend on the head's content. Unit tests with fakes cover them (Phases 3–4), so the e2e tests don't depend on what `main` contains.

### Phase 8 — Documentation
- `docs/guides/agents.md` and `docs/guides/depot.md`:
  - branch mode, `ref`, `branchPolicy`, and that polling is opt-in through `pollingIntervalMinutes`, with branch sources intended as a one-time migration managed through GitOps afterwards;
  - when polling is unset, a branch source picks up new commits only when the Depot reconciles for another reason (spec change, annotation, or resync). Say this explicitly so users don't expect continuous tracking;
  - tags versus branches: tags are usually more immutable than branches, so tag mode is the recommended default for consumers who want stable content. A branch head moves with every push, so each new commit becomes a new version. Each Version is still pinned to its exact `sourceCommit`, which makes every published version reproducible even though the branch itself is not;
  - `immutable` works on branch sources. Each Version is pinned to its `sourceCommit`, so a re-download must match the recorded `status.checksum`. A moving branch creates new Versions and doesn't change existing ones. The checksum is only stable while the archive format is, so a packaging change can alter it for the same SHA, the same as with tags;
  - one pending entry at a time, and commits between polls collapsing into one version;
  - the naming and status flow table;
  - recreating a source resets numbering.
- `docs/reference/api.md`: all new fields and the CEL rules.
- `docs/configuration/scanning.md`:
  - Jev bump classification and thresholds;
  - Trivy runs before Jev, and flagged files are withheld;
  - the Jev-unavailable behavior and the `bump-override` annotation.
- `docs/helm-chart.md`: the `branchSourcing.*` values, and that the one flag gates both the depot and the version controller.
- `docs/architecture.md`: the Depot → Agent → Version flow for branch mode, and the server lookup through `versionRefs[].name`.
- `docs/reference/version-constraints.md`: `ref` versus `versionConstraints`, and the immutability point above (tags are generally more immutable than branches, so prefer `versionConstraints` for production sources).
- Security note: pushing to the branch publishes a version once all checks pass, so recommend branch protection.

---

## E2E Test Changes (summary)
- **depot:** admission rejection, flag off, first sighting is pending with no version, no extra entry while pending.
- **agent:** SHA naming, copying back on assignment, write-once `spec.version`.
- **version:** pinned seed → `1.0.0` Initial; head progression to a terminal phase with the per-branch assertions above; the Jev variant (fails if the key is missing); no duplicates; server download through the SHA-named Version; same checksum on re-download.

## Helm Chart (summary)
- Add `branchSourcing.enabled: false`.
- Add args to the depot and version deployments, and make depot `args` render unconditionally.
- Sync the CRDs.
- No RBAC changes and no Chart.yaml bump.

## Validation commands
1. For each touched module (`api/v1alpha1`, `pkg/github`, `pkg/jev`, `services/depot`, `services/agent`, `services/version`, `services/server`): `go fmt ./...` and `go vet ./...`.
2. `make manifests generate` in `services/depot`, `services/agent`, and `services/version`. Then `diff` each regenerated CRD against `chart/opendepot/crds/`; there should be no difference.
3. Unit tests:
   - `make test` in `services/depot`, `services/agent`, `services/version`, and `services/server`;
   - `go test ./...` in `pkg/github`, `pkg/jev`, and `cli`.
4. Chart:
   - `helm lint chart/opendepot`;
   - `helm template chart/opendepot --set branchSourcing.enabled=true` and confirm both deployments render `--branch-sourcing-enabled=true`;
   - confirm the depot args render when `depot.zapLogLevel` is unset;
   - `git diff chart/opendepot/Chart.yaml` shows no changes.
5. `make test-e2e` in `services/depot`, `services/agent`, and `services/version`. All must pass. The Jev specs need the Jev key env var.

## Out of scope
- Webhooks.
- Switching a source between tag and branch mode.
- A per-source branch allowlist, or a "require a protected branch" option.
- Showing the commit or bump in the UI.
- A `source_commit` attribute in the lock file (D8).
- The existing fail-open paths in provider scanning.

## Decisions (all resolved)
1. **Failing Jev:** Option A. Hold and retry indefinitely with capped backoff. The operator unblocks it with `forceSync` or the `opendepot.defdev.io/bump-override` annotation. Trivy, agentspec, the Jev safety check, and signing still apply.
2. **Fixtures:** pinned as listed in Phase 7. The Developer does not re-derive them.
