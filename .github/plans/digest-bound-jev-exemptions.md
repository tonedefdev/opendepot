# Implementation Plan: Digest-Bound Jev Exemptions in ScanPolicy

Status: REVISED (rev 2). The user has answered D1 to D8. Follow-up questions Q1 to Q7 are listed under "Still Undecided". Each has a provisional choice, so the Developer can start. If the user answers Q1 to Q3 differently, only the `jevExemptible` predicate (Steps 3 and 4) and its tests change. Q4 affects only Step 7, and Q5 affects only Step 6.

## Summary
Today a Jev block on a Skill or Agent Version cannot be overridden. A block can be a Critical risk level or a JevPolicy threshold breach. This plan adds `spec.jevExemptions` to ScanPolicy. Each entry binds to one archive content digest (lowercase hex SHA-256, 64 characters), has a required reason, and has an optional expiry. Only the highest-priority ScanPolicy that matches the Version is used.

An exemption applies when the winning policy has an unexpired entry whose digest equals both the digest Jev assessed and the current archive digest. Then every policy-gate block reason on the assessment is cleared: the Critical reason and the JevPolicy threshold reasons. `blocked` becomes false, the original `blockReasons` stay listed as the audit trail, and the exemption is recorded in three places:
- `status.jevAssessment`: the policy name, reason and expiry
- `syncStatus`
- a Kubernetes Event

Some reasons mean Jev did not see or score all the content: errors, incomplete assessments, skipped files, the token limit, and Jev being disabled. These are never cleared (provisional, see Q1). Any content change gives a new digest, so the Version blocks again.

Two behavior changes are explicit:
- A Critical risk level now also sets `needsReview`.
- Published Versions are re-blocked when their exemption expires or is removed.

Rejected branch Versions can be re-opened under four explicit conditions.

## Verified Facts (from code)
- **Digest source.** `fetchAgentArchiveWithDefinition` (opendepot_versions_controller.go, around line 1537) packs the definition with `archive.WriteDeterministicTarGz`. It returns `packed.Bytes()` and `base64(sha256(packed))`. Reconcile sets `fileBytes = packed` and `archiveChecksum = base64`, then passes `fileBytes` to `runAgentScan`, which sets `content.checksum = hex(sha256(archiveBytes))` (agent_scan.go, around line 258). Both checksums hash the same bytes in different encodings. `pkg/archive.SHA256Hex` already computes the hex form, and the hex form is what signed SHA256SUMS use. The deterministic writer uses fixed headers and modes, a lexical WalkDir order, and a gzip header with no name or mtime. The digest is stable across syncs for the same tree and Go toolchain.
- **Where the digest is visible.** `persistAgentBlock` does not write `status.checksum`, so a blocked Version shows no digest today. The new `jevAssessment.contentDigest` field is required.
- **Policy access.** Reconcile calls `resolveScanPolicy` whenever `r.ScanningEnabled || isAgent` (around line 640), before `runAgentScan` and `runAgentJev`. `resolvedPolicy` is already in scope at the Jev block check (around line 733). `policy.Resolved` holds only scanner exemptions today.
- **Skill and Agent use the same path.** Both set `isAgent` and go through runAgentScan, runAgentJev, the `jevAssessment.Blocked` check, persistAgentBlock, and then signing.
- **Cache.** `runAgentJev` reuses a cached blocked assessment with no error unless ForceSync is set (`reuse && cached.Error == ""`). The reuse check does not compare content, so the exemption must be applied after runAgentJev returns, on both fresh and cached results.
- **Re-trigger.** The Version controller watches ScanPolicy (GenerationChanged) and enqueues every Version in the namespace. An unsynced agent Version takes the full path and reuses its cached Jev assessment, so no new Jev call is made. A synced Version takes the fast path into `reconcileStoredScanPolicy`, which re-evaluates only scan findings and returns early when both scans are nil.
- **Branch sources.** `persistAgentBlock` sets `branchPhase=Rejected` on `JevBlocked:`. `reconcileBranchPreflight` returns done for `Rejected && spec.version == ""` (branch_version.go, around line 139). An exemption added after the block can never be seen (see D6).
- **Critical-only detection.** When `Error == ""`, `riskLevel` is Critical, and there is exactly one block reason, the Critical reason is the only one. Every other blocked path either sets `Error` or adds more reasons, and the aggregate `RiskLevel` is the worst scored part, the same set the gate checks.
- **RBAC.** The server write path (`services/server/policy_api.go`) is gated by the `--policy-management-enabled` flag, by authenticated access, and by `SecurityGroupBinding`: namespaces, `namespaceWidePolicyManagement` for policies with a selector or with no targetRefs, and per-resource scope for targetRefs. The `GroupBinding.scanPolicyManagement` and `scanPolicyNamespaces` fields cited in the brief are not what `policy_api.go` checks. `decodeAndValidateScanPolicy` decodes into the shared Go type with `DisallowUnknownFields`, so the new field is accepted once types.go has it. No new permission is needed. The version controller already has `get;list;watch` on scanpolicies.
- **UI data loss.** `services/ui/src/components/security-policies/SecurityPoliciesSurface.tsx` `policyToMutation` copies only priority, selector, targetRefs, severityThreshold and exemptions. A UI edit followed by a PUT would silently delete `jevExemptions`.
- **Generators.** `make -C api/v1alpha1 generate manifests` regenerates the deepcopy file and writes CRDs straight into `chart/opendepot/crds/`. `make -C services/version manifests` writes `services/version/config/crd/bases` and copies them into the chart.
- **Where block reasons come from (relevant to D2).**
  - `evaluateJevPartsGate` (agent_jev_aggregate.go) produces the *policy-gate* reasons, each with a fixed and distinct text prefix:
    - one Critical reason: "risk level Critical (chunk …)"
    - eight threshold reasons: minSafeProbability, maxInjectionProbability, maxExfiltrationProbability, maxDestructiveProbability, maxHiddenInstructionsProbability, maxScopeMismatchProbability, maxRemoteExecutionProbability and maxRiskScore
    - one minConfidence reason
  - `runAgentJev` (agent_jev.go) produces the *non-gate* reasons:
    - Jev disabled, and retries exhausted. `Error` is set.
    - A split error, the layout being over the part limit, and skipped files. These all start with "jev assessment incomplete: …" and `Error` is empty.
    - A part failure. `Error` is set, and the gate reasons are appended after the failure message.
    - The token limit. Reasons start with "part X: …", `Error` is empty, and the gate reasons are appended.
  - Only the final scored path gives an assessment where every reason is a gate reason, `Error == ""`, `SkippedFileCount == 0`, and `AssessedChunks == ChunkCount > 0`.
- **needsReview** is set only on the final scored path (`assessment.NeedsReview = needsReview`). Today it is true only when minConfidence is breached. With a nil JevPolicy, `evaluateJevPartsGate` returns `needsReview=false` even for Critical.
- **Events.** `VersionReconciler` has no event recorder, and `chart/opendepot/templates/version-rbac.yaml` grants no `events` permission. controller-runtime is v0.23.3. The chart RBAC is hand-maintained (`services/version/config` contains only `crd/`), so a new rule is added to the Helm template by hand. A kubebuilder marker is added for parity.
- **Review queue.** I did not find a dedicated review-queue component in `services/ui/src/components`. `needsReview` is visible on `status.jevAssessment` and in JevPanel. See Q7.
- **Module resolution.** `services/version` and `services/server` resolve `api/v1alpha1` through the root `go.work`. There is no `replace` for it in their go.mod files. This is the same as for the existing JevAssessment fields, so no go.mod change is planned.

## Decisions (answered by the user)
- **D1. `expires` is OPTIONAL**, matching ScanExemption. If it is omitted, the exemption never expires. Digest binding is the main safeguard.
  - *Residual risk:* an exemption without an expiry stays valid until the archive content changes, which gives a new digest. A Go toolchain or packing change also gives a new digest. Nothing forces anyone to revisit it.
  - Mitigations in this plan:
    - The docs recommend always setting `expires`.
    - The audit trail shows "no expiry": the Event message, `syncStatus`, and the JevPanel chip.
    - The ScanPolicy status counts list a no-expiry entry as active.
- **D2. Scope is the Critical block AND the JevPolicy threshold reasons, for the exact digest only.** When a matching exemption exists, all policy-gate block reasons on that digest's assessment are cleared. The digest must match. Interaction with the status fields:
  - `blocked` becomes false.
  - `blockReasons` is left unchanged. On an exempted assessment it is the list of the reasons that were cleared.
  - `exempted`, `exemptedBy`, `exemptionReason` and `exemptionExpires` record who cleared them and why.
  - `needsReview` is not changed by the exemption.
  - Auditing:
    - the status fields above
    - `syncStatus`, which names the policy and the number of reasons cleared
    - a Kubernetes Event, `JevExemptionApplied` or `JevExemptionRevoked`, emitted on each transition (Step 6)
    - a controller info log
  - Which reasons count as "threshold reasons" and which never clear: see Q1 to Q3.
- **D3. needsReview: YES.** A Critical risk level sets `needsReview=true`, including when the source has no JevPolicy. This is an explicit, documented behavior change (Step 3, the docs, and the upgrade note). It stays true on an exempted assessment because it is the raw Jev signal.
- **D4. Digest form:** lowercase hex SHA-256 of the packed archive, 64 characters, with exact matching. It is stored on `status.jevAssessment.contentDigest`. It is validated by the CRD pattern `^[0-9a-f]{64}$` and by the server payload validation. It equals hex(base64-decode(`status.checksum`)).
- **D5. Status counts: YES.** Add `activeJevExemptions` and `expiredJevExemptions` to ScanPolicyStatus. `nextExpiry` includes Jev expiries. The existing counts and printcolumns are not changed.
- **D6. Branch re-open: YES.** It is allowed only when all four conditions hold, taken as written (see Step 7 and Q4):
  1. The Version is Rejected and unassigned, and `syncStatus` starts with `JevBlocked:`.
  2. Critical was the only block reason (`jevCriticalOnly`).
  3. `status.jevAssessment.contentDigest` is recorded.
  4. The winning ScanPolicy has an unexpired jevExemption for that digest.
- **D7. Re-block on expiry or removal: YES, for published Versions.** The stored-policy path sets `synced=false` and `syncStatus` to `JevBlocked: …`, and emits `JevExemptionRevoked`. It does not change `branchPhase` on assigned Versions.
- **D8. UI.** Required: pass `jevExemptions` through `policyToMutation` and the TS types. JevPanel shows `contentDigest` with CopyButton, plus an "Exempted by <policy>" chip. Creating exemptions in the UI is out of scope.

## Resolved in the user's review (supersedes the provisional choices below)
- **Q1 (incomplete coverage):** never cleared. Errors, skipped files, token limits, Jev disabled, and exhausted retries are never exempted. A Version with a Critical reason plus a token-limit reason stays blocked.
- **Q4 (branch re-open):** widened. A Rejected branch Version re-opens when a matching exemption exists and the exemption can clear every block reason. Critical-only is no longer required.
- **Q5 (Events RBAC):** approved. Keep Step 6 and add `create` and `patch` on `events`.
- **Q2, Q3, Q6, Q7:** provisional choices stand (threshold-only blocks are exemptable; minConfidence counts as a threshold reason; the exemption is digest-only; no new review-queue surface).

## Still Undecided (provisional choice in brackets; the Developer proceeds with it unless the user says otherwise)
- **Q1. Do "all Jev block reasons" include the non-gate reasons?** Your answer said both "all Jev block reasons for that digest are cleared" and "Critical AND threshold reasons". The earlier Option B text said "never error, incomplete, skipped or token-limit reasons".
  - [Provisional: never clear the non-gate reasons: error, incomplete, skipped files, token limit, Jev disabled, retries exhausted. An exemption is valid only when Jev scored every part of the content. A Version with a Critical reason plus a token-limit reason stays blocked.]
  - If you want these cleared too, that means approving content Jev never fully saw. It also needs Reconcile changes, because those paths return an error from `runAgentJev` before the `Blocked` check.
- **Q2. Can a threshold-only block (no Critical reason, for example High plus maxDestructiveProbability) be exempted?**
  - [Provisional: yes. A less severe verdict should not be harder to exempt than a Critical one.]
  - The alternative is to require a Critical reason to be present.
- **Q3. Is the minConfidence reason a "threshold reason"?**
  - [Provisional: yes. It is a JevPolicy field, and `needsReview` stays true, so a human still sees it.]
- **Q4. Conflict between D6 and D2.** D6 condition 2 requires Critical to be the *only* block, but D2 also clears threshold reasons. So a Rejected branch Version blocked by Critical plus maxDestructiveProbability would *not* re-open, while the same content from a tag source would be exempted.
  - [Provisional: implement D6 as written, test and document the gap.]
  - Should condition 2 widen to "every block reason is exemptible" (the same predicate as Step 4)? I did not resolve this myself because you asked to keep the four conditions explicit.
- **Q5. Kubernetes Events need new RBAC:** `create` and `patch` on `events` for the version controller, in the Helm template and the kubebuilder marker.
  - [Provisional: add it (Step 6).]
  - The alternative is to audit through status, `syncStatus` and logs only, and drop Step 6.
- **Q6. Should an exemption also pin the reasons or the risk level it covers?** It is bound only to the digest. If the source JevPolicy is tightened later and the Version is force-synced, the same exemption also clears the new threshold reasons.
  - [Provisional: digest only, documented as a risk.]
  - A follow-up could add an optional cap, for example `maxRiskLevel`.
- **Q7. Review queue.** I found no review-queue UI. `needsReview` is shown on the assessment and in JevPanel.
  - [Provisional: no new review-queue surface.]
  - Confirm, or tell me where the queue lives.

## Affected Services
- `api/v1alpha1`: a new type, new ScanPolicySpec and ScanPolicyStatus fields, and new JevAssessment fields.
- `services/version`:
  - the policy package
  - the gate reason constants and needsReview
  - the Jev overlay
  - the Reconcile wiring
  - the stored-policy path
  - Events (wired in `cmd/main.go`)
  - branch preflight
  - ScanPolicy status
- `services/server`: payload validation for jevExemptions.
- `services/ui`: the TS types, the editor pass-through, and the JevPanel display. Routed to the OpenDepot UI Developer agent.
- `chart/opendepot`: the regenerated CRDs and one new `events` RBAC rule in `templates/version-rbac.yaml` (Q5).
- `docs`: the scanning configuration, the agents guide (Jev), the upgrade notes, the API reference, and the controller permissions reference.

## API / CRD Changes (`api/v1alpha1/types.go`)
New type, placed after `ScanExemption`:
- `JevExemption`. Doc: "clears the Jev policy-gate block (the Critical risk level and JevPolicy threshold reasons) for one exact archive. It never clears incomplete, error, skipped-file or token-limit reasons."
  - `ContentDigest string` with `json:"contentDigest"`. Markers: `+kubebuilder:validation:Required` and ``+kubebuilder:validation:Pattern=`^[0-9a-f]{64}$` ``. Doc: lowercase hex SHA-256 of the Version archive, copied from `status.jevAssessment.contentDigest`.
  - `Reason string` with `json:"reason"`. Markers: `+kubebuilder:validation:Required` and `+kubebuilder:validation:MinLength=1`.
  - `Expires *metav1.Time` with `json:"expires,omitempty"` and `+optional` (D1). Doc: when omitted, the exemption never expires and stays valid until the archive content changes. Setting it is recommended.

`ScanPolicySpec`:
- `JevExemptions []JevExemption` with `json:"jevExemptions,omitempty"`. Markers: `+optional`, `+listType=map`, `+listMapKey=contentDigest`. The map list type rejects duplicate digests.
- Update the `Priority` doc comment to say the winning policy also supplies jevExemptions.

`ScanPolicyStatus` (D5):
- `ActiveJevExemptions int` with `json:"activeJevExemptions,omitempty"` and `+optional`. Entries without an expiry count as active.
- `ExpiredJevExemptions int` with `json:"expiredJevExemptions,omitempty"` and `+optional`.

`JevAssessment`:
- `ContentDigest string` with `json:"contentDigest,omitempty"`. Markers: `+optional` and ``+kubebuilder:validation:Pattern=`^[0-9a-f]{64}$` ``. Doc: the archive Jev assessed; copy this value into a jevExemption.
- `Exempted bool` with `json:"exempted,omitempty"`.
- `ExemptedBy string` with `json:"exemptedBy,omitempty"`. This is the ScanPolicy name.
- `ExemptionReason string` with `json:"exemptionReason,omitempty"`.
- `ExemptionExpires *metav1.Time` with `json:"exemptionExpires,omitempty"`. Nil means the exemption never expires.
- Update these doc comments:
  - `Blocked`: false when a matching jevExemption cleared every block reason.
  - `BlockReasons`: when `Exempted` is true, this lists the reasons that were cleared.
  - `NeedsReview`: also set by a Critical risk level (D3), and not changed by an exemption.

## Implementation Steps

### Step 1: API types and generated output [SHARED, GENERATED, runs alone first]
- **Depends on:** none
- **Files:**
  - `api/v1alpha1/types.go`
  - `api/v1alpha1/zz_generated.deepcopy.go` (generated)
  - `chart/opendepot/crds/opendepot.defdev.io_scanpolicies.yaml` (generated)
  - `chart/opendepot/crds/opendepot.defdev.io_versions.yaml` (generated)
  - `services/version/config/crd/bases/` copies of those two CRDs, if they are tracked (generated)
- Add the types and fields above.
- Run `make -C api/v1alpha1 generate manifests`. If `services/version/config/crd/bases` is tracked, also run `make -C services/version manifests`.
- Never hand-edit generated files. If the generator changes other CRDs, stop and report it. Do not commit unrelated generator drift.
- Validate with `cd api/v1alpha1 && go vet ./... && go test ./...`.

### Step 2: Resolve Jev exemptions in the policy package
- **Depends on:** 1
- **Files:**
  - `services/version/internal/policy/policy.go`
  - `services/version/internal/policy/policy_test.go`
- Add `JevExemptions []opendepotv1alpha1.JevExemption` to `Resolved`. It holds only the winning policy's unexpired entries.
- In `Resolve`, filter on the same `Expires.After(now)` rule and fold Jev expiries into `NextExpiry`. A nil `Expires` never expires and does not affect `NextExpiry` (D1).
- Add `(Resolved) JevExemptionFor(digest string) *opendepotv1alpha1.JevExemption`. It uses exact matching and returns nil for an empty digest.
- Add `CountJevExemptions(policy, now) (active, expired int)`. A nil `Expires` counts as active.
- Do not change `Apply`, `covers` or `CountExemptions`.
- Tests:
  - Expired entries are dropped.
  - An entry with no expiry is kept and does not set NextExpiry.
  - NextExpiry takes the earliest across both lists.
  - Exact matching: mixed case and empty input do not match.
  - Priority shadowing: Select plus Resolve, where a lower-priority policy holds the matching entry, gives no Jev exemption.
  - CountJevExemptions handles a mix of nil, future and past expiries.

### Step 3: Gate reason constants, exemptibility classifier, and needsReview (D2, D3)
- **Depends on:** none
- **Files:**
  - `services/version/internal/controller/agent_jev_aggregate.go`
  - `services/version/internal/controller/agent_jev_aggregate_test.go`
- Move each `evaluateJevPartsGate` reason format into a named prefix constant plus format string. There are ten: Critical, the eight thresholds, and minConfidence. Steps 4 and 7 match on these prefixes, so the text cannot drift. Keep the reason text byte-identical, because stored assessments already contain it.
- Add `isJevGateReason(reason string) bool`. It returns true when the reason starts with one of the ten prefixes. The set is per Q1 and Q3 (provisional: all ten).
- D3 behavior change: set `needsReview = true` when a Critical part is found. This also applies when `policy == nil`, so the nil-policy early return must return true when Critical was found. Update the function's doc comment.
- Keep the `evaluateJevPartsGate` signature unchanged, because `agent_jev_harness.go` (build tag `jevharness`) calls it. Check whether the harness reads `needsReview` and report what you find.
- Tests:
  - needsReview (D3):
    - Critical with a nil policy gives needsReview true.
    - Critical with a policy that has no minConfidence gives true.
    - High with no minConfidence breach stays false.
    - A minConfidence breach without Critical still gives true.
    - Update existing tests that expect `false` for Critical.
  - A table test: every reason `evaluateJevPartsGate` can emit (all ten) is classified by `isJevGateReason`.
  - These non-gate strings are not gate reasons:
    - a "jev assessment incomplete: …" reason
    - a "part 1: …" token-limit reason
    - `jevDisabledError`
    - a "read jev secret …" error
  - A reason text regression test pins the exact strings.

### Step 4: Digest stamping and the exemption overlay (D2)
- **Depends on:** 1, 2, 3
- **Files:**
  - `services/version/internal/controller/agent_jev.go`
  - `services/version/internal/controller/agent_jev_test.go`
- In `runAgentJev`, set `assessment.ContentDigest = content.checksum` on every fresh assessment built after content is known:
  - The scored path is mandatory.
  - The layout-over, skipped, split-error and incomplete paths are recommended, for display only.
  - Never stamp a cached assessment on reuse.
  - A legacy assessment with no digest cannot be exempted until it has had one forceSync.
- Add `jevCriticalOnly(a) bool`. Only Step 7 (D6) uses it. It returns true when all of these hold:
  - `Error == ""`
  - `RiskLevel` equals Critical (case-insensitive)
  - `len(BlockReasons) == 1`
  - the reason has the Critical prefix
- Add `jevExemptible(a) bool`, the D2 predicate. It returns true when all of these hold:
  - `a.Error == ""`
  - `len(a.BlockReasons) >= 1`
  - every reason satisfies `isJevGateReason` (Q1, Q3)
  - `a.ChunkCount > 0`
  - `a.AssessedChunks == a.ChunkCount`
  - `a.SkippedFileCount == 0`

  It does not require a Critical reason (Q2, provisional). The last three conditions are defense in depth: they make sure Jev scored every part.
- Add the pure function `applyJevExemption(a *JevAssessment, resolved policy.Resolved, digest string) *JevAssessment`. It returns a deep copy and never changes the input, because the cached value points into the Version status.
  - When the assessment is not exemptible: clear any stale exemption fields and leave `Blocked` unchanged.
  - When it is exemptible, `a.ContentDigest != ""`, `a.ContentDigest == digest`, and `resolved.JevExemptionFor(digest)` is non-nil:
    - set `Blocked=false`, `Exempted=true`, `ExemptedBy=resolved.PolicyName`, `ExemptionReason`, and `ExemptionExpires` (nil when the entry has no expiry)
    - leave `BlockReasons` and `NeedsReview` unchanged
  - Otherwise, when it is exemptible: set `Blocked=true` and clear the exemption fields. This makes the overlay reversible.
- Extend `jevPublishedStatus` for the exempted case. Example text: "Successfully synced version; Jev block exempted by ScanPolicy <name>: <n> reason(s) cleared for digest <first 12 hex>; expires <RFC3339 | never>". Keep the existing error branch.
- Tests:
  - A Critical-only assessment with a match is unblocked, and all exemption fields are set.
  - **Threshold-reason clearing (D2).** Critical plus maxDestructiveProbability plus maxRiskScore with a match: `Blocked=false`, all three reasons stay in `BlockReasons`, and `Exempted=true`.
  - **Threshold-only (Q2).** High plus maxDestructiveProbability with a match is unblocked.
  - **minConfidence (Q3).** Critical plus minConfidence with a match is unblocked, and `NeedsReview` stays true.
  - **Never cleared (Q1).** These stay blocked even with a matching exemption:
    - Critical plus a token-limit reason
    - a part failure (`Error` set, with gate reasons appended)
    - skipped files
    - layout over
    - `AssessedChunks < ChunkCount`
    - Jev disabled
  - Digest checks:
    - the policy digest does not match: blocked
    - the assessment digest differs from the current archive digest: blocked
    - a legacy assessment with an empty digest: blocked
  - An exempted assessment re-blocks when the exemption is gone, and the exemption fields are cleared.
  - An entry with no expiry is applied, and `ExemptionExpires` is nil.
  - The input is not changed.
  - A fresh scored assessment carries `ContentDigest`, and a cached one keeps its stored value.
  - `jevPublishedStatus` text for an exempted assessment, both with and without an expiry.

### Step 5: Reconcile wiring and the stored-policy path (D2, D7)
- **Depends on:** 4
- **Files:**
  - `services/version/internal/controller/opendepot_versions_controller.go`
  - `services/version/internal/controller/agent_versions_test.go`
- After `runAgentJev` returns with `jevErr == nil` and a non-nil assessment, apply `applyJevExemption(jevAssessment, resolvedPolicy, content.checksum)` before the `Blocked` check.
  - When `jevErr != nil`, which covers the incomplete paths, no overlay is applied. This matches Q1.
  - When the assessment is exempted, log at info level with the policy name, the digest, the number of reasons, and the expiry ("never" when there is none).
- The existing `resolvedPolicy.NextExpiry` requeue then covers Jev expiry. Entries without an expiry never requeue.
- In `reconcileStoredScanPolicy` (the published fast path):
  - Widen the early-return guard so a Version whose `status.jevAssessment` is exemptible (exempted or blocked) is not skipped.
  - Recompute the current digest as hex(base64-decode(`status.checksum`)). It must equal `assessment.contentDigest`. If decoding fails or the values differ, fail closed by treating it as no exemption.
  - Re-apply the overlay.
    - If the assessment was `Exempted` and no longer is (D7): set `synced=false`, set `syncStatus` to `JevBlocked: <reasons>`, and persist the updated assessment.
    - If it is still exempted but `ExemptedBy`, `ExemptionReason` or `ExemptionExpires` changed (for example a different policy now wins with the same digest): persist the new fields and do not re-block.
  - The scan-violation message still takes precedence.
  - Do not change `branchPhase` on assigned Versions.
- Expose the before and after `Exempted` transition to the caller (a return value or a small struct) so Step 6 can emit Events without re-deriving it.
- Reconcile-level tests, for both Skill and Agent. Use the existing fake-client, Jev httptest and Trivy stub pattern in this file.
  - With no policy, Critical stays blocked and `needsReview` is true (D3).
  - With a matching exemption on a Critical-only block: synced, the exemption fields are set, and `syncStatus` names the policy.
  - **D2:** with a matching exemption on Critical plus threshold reasons (stub Jev so maxDestructiveProbability and maxRiskScore also breach): synced, `blockReasons` keeps all reasons, `blocked=false`.
  - With a matching exemption when one part hits the token limit: still blocked (Q1).
  - An expired exemption: blocked. An exemption without an expiry: synced.
  - Priority shadowing: blocked.
  - A digest mismatch: blocked.
  - On a published, exempted Version:
    - The exemption is removed: re-blocked through the stored path, with `synced=false` and `syncStatus` starting with `JevBlocked:`.
    - The exemption expires: re-blocked after the NextExpiry requeue.
    - The winning policy switches to another one with the same digest: it stays synced, and `exemptedBy` is updated.

### Step 6: Kubernetes Events for exemption audit (only if Q5 is accepted)
- **Depends on:** 5
- **Files:**
  - `services/version/internal/controller/opendepot_versions_controller.go`
  - `services/version/internal/controller/agent_versions_test.go`
  - `services/version/cmd/main.go`
  - `chart/opendepot/templates/version-rbac.yaml` **[SHARED: Helm RBAC]**
- Add an event recorder field to `VersionReconciler` and wire it in `cmd/main.go` from the manager. In controller-runtime v0.23, use the manager's recorder API and note whether it writes `events.k8s.io/v1` or core `v1` Events. Treat a nil recorder as a no-op so existing tests keep working.
- Emit Events only on transitions, not on every reconcile:
  - `Normal JevExemptionApplied`, when the assessment goes from not exempted to exempted (full path or stored path).
  - `Warning JevExemptionRevoked`, when an exempted assessment re-blocks. Say whether the entry expired or was removed or no longer matches.
- The message includes the policy name, the digest, the number of reasons cleared, and the expiry or "never". It must never include the token or any skill content.
- Add a kubebuilder RBAC marker for `events` with `create;patch` in the group the recorder uses. Add the same rule to the top-level `rules` in `version-rbac.yaml`. That block feeds both the Role (`scopeToNamespace`) and the ClusterRole variants.
- Tests: a fake recorder records exactly one Applied event on the first exempt reconcile, none on a second identical reconcile, and one Revoked event after removal.

### Step 7: Re-open Rejected branch Versions (D6, as written; see Q4)
- **Depends on:** 4
- **Files:**
  - `services/version/internal/controller/branch_version.go`
  - `services/version/internal/controller/branch_version_test.go`
- In `reconcileBranchPreflight`, skip the `Rejected && spec.version == ""` short-circuit only when all four D6 conditions hold:
  1. `syncStatus` starts with `JevBlocked:`
  2. `jevCriticalOnly(status.jevAssessment)` is true
  3. `status.jevAssessment.contentDigest` is not empty
  4. `r.resolveScanPolicy(...)` gives a winner whose `JevExemptionFor(contentDigest)` is not nil
- On a match, return not-done so the normal sibling, superseded and Jev-ref checks run.
- Reconcile then re-fetches the commit archive, and Step 5 checks the stored digest against the current archive digest. Condition 3 is therefore not the final digest check.
- Check that the Agent controller (`services/agent/internal/controller/agent_versions.go`) does not prune or keep Rejected entries in a way that conflicts. Report what you find; do not change it.
- Tests:
  - Rejected for a scan violation: stays terminal.
  - Rejected by Jev with no exemption: stays terminal.
  - Rejected by Jev (Critical only) with a matching exemption: proceeds.
  - Rejected by Jev with an expired exemption: stays terminal.
  - **Q4 gap, pinned:** Rejected by Jev for Critical plus a threshold reason, with a matching exemption: stays terminal.
  - Superseded still wins.

### Step 8: ScanPolicy status counts and expiry requeue (D5)
- **Depends on:** 1, 2
- **Files:**
  - `services/version/internal/controller/opendepot_scanpolicy_controller.go`
  - its test: the existing ScanPolicyReconciler test if one exists (search `*_test.go`), otherwise a new `opendepot_scanpolicy_controller_test.go`
- In `observe`, fill the new counts with `policy.CountJevExemptions`. Make `nextExpiry` include jevExemptions that have an expiry.
- Tests:
  - The counts are correct for a mix of active, expired and no-expiry entries.
  - `nextExpiry` picks the earliest across both lists.
  - The existing counts are unchanged.

### Step 9: Server payload validation (D1, D4)
- **Depends on:** 1
- **Files:**
  - `services/server/policy_api.go`
  - `services/server/policy_api_test.go`
- In `decodeAndValidateScanPolicy`, return 400 for any of these:
  - `jevExemptions[i].reason` is blank after trimming
  - `contentDigest` does not match `^[0-9a-f]{64}$` (uppercase is rejected)
  - a digest appears twice
- `expires` is optional. If it is set, apply whatever rule this function already applies to scanner exemption `expires`. Do not invent a stricter rule.
- No authorization change.
- Tests: each 400 case, a valid entry without `expires`, a valid entry with `expires`, and an uppercase digest rejected.

### Step 10: UI (OpenDepot UI Developer agent only)
- **Depends on:** 1
- **Files:**
  - `services/ui/src/lib/api.ts`
  - `services/ui/src/components/security-policies/SecurityPoliciesSurface.tsx`
  - `services/ui/src/components/security-policies/SecurityPoliciesSurface.test.tsx`
  - `services/ui/src/components/JevPanel.tsx`
  - `services/ui/src/components/JevPanel.test.tsx`
  - Check-only, edit only if needed: `services/ui/src/components/AgentDetail.tsx`, `services/ui/src/components/VersionsTable.tsx`, `services/ui/src/components/UnsyncedWarning.tsx`
- Types:
  - Add `jevExemptions` to the `ScanPolicySpec` and `ScanPolicyMutation` types, with `expires` optional.
  - Add the new ScanPolicyStatus counts.
  - Add the new JevAssessment fields.
- `policyToMutation` must deep-copy `jevExemptions` through (required, D8).
- JevPanel:
  - Show `contentDigest` with CopyButton.
  - When `exempted` is true:
    - Show an "Exempted by <policy>" chip, with a tooltip giving the reason and the expiry, or "No expiry" when there is none.
    - Show `blockReasons` under an "Exempted block reasons" label, not as active blocks.
  - Keep the needsReview indicator, which now appears for Critical (D3).
- Check whether any component treats a non-empty `blockReasons` as blocked. If one does, switch it to `blocked`.
- Tests:
  - A loaded policy with jevExemptions keeps them in the PUT body and in the YAML preview, including one entry without `expires`.
  - The digest renders, and the copy button copies it.
  - The exempted chip appears with the reason and expiry, including "No expiry".
  - No chip appears when the assessment is not exempted.
  - Exempted reasons are not shown as active blocks.

### Step 11: Docs
- **Depends on:** 5, 6, 7, 8, 9, 10
- **Files:**
  - `docs/configuration/scanning.md` (ScanPolicy section; there is no separate ScanPolicy guide under docs/guides)
  - `docs/guides/agents.md` (Jev section)
  - `docs/upgrading.md`
  - `docs/reference/api.md` (only if it documents ScanPolicySpec, ScanPolicyStatus or JevAssessment fields)
  - `docs/reference/rbac/controller-permissions.md` (the events rule, only if Step 6 lands)
- Cover:
  - How to read `status.jevAssessment.contentDigest`, with a kubectl jsonpath example, and how it relates to `status.checksum`.
  - A jevExemptions example with a placeholder digest, a reason, and `expires`. Also show that `expires` is optional, with a warning that an exemption without it stays valid until the content changes. Recommend always setting it.
  - The D2 scope:
    - which reasons are cleared: Critical, the eight thresholds, and minConfidence
    - which are never cleared: errors, incomplete, skipped files, token limit, Jev disabled
    - an exempted assessment keeps `blockReasons` with `blocked=false`
    - where to audit: the status fields, `syncStatus`, Events, and the ScanPolicy counts
  - The exemption covers whatever gate reasons exist for that digest, including reasons added later when the JevPolicy is tightened (Q6).
  - Priority shadowing.
  - Re-blocking published Versions on expiry or removal (D7).
  - A legacy assessment needs one forceSync to get a digest.
  - Branch sources: the four re-open conditions, and the Q4 gap (a Critical plus threshold block on a Rejected branch Version needs a new commit) unless Q4 is changed.
  - **Behavior change (D3):** Critical now sets `needsReview`.
  - **Upgrade note:** Helm does not upgrade CRDs, so run `kubectl apply -f chart/opendepot/crds/` before upgrading. Mention the D3 needsReview change and the new events RBAC.
- Do not add sample skill content (no conventional-commit text).

### Step 12: Focused validation
- **Depends on:** all
- **Files:** none
- Commands:
  - `make -C api/v1alpha1 generate manifests`, then `git diff --stat` shows only the intended CRDs.
  - `cd services/version && go vet ./... && go vet -tags jevharness ./internal/controller/ && go build -o /dev/null ./...`
  - `cd services/version && go test ./internal/policy/... ./internal/controller/... -run 'Jev|ScanPolicy|Branch|Policy|Exempt'`, then `make -C services/version test` for the full envtest run.
  - `cd services/server && go vet ./... && go test ./... -run 'ScanPolic'`
  - `helm template chart/opendepot > /dev/null`, rendered with both `rbac.scopeToNamespace=true` and `false`, to check the RBAC template.
  - UI: the existing vitest, lint and typecheck scripts from `services/ui/package.json`, scoped to the changed components.

## E2E Test Changes
- No e2e is required. Jev needs the TypeSafe endpoint, and the controller paths are covered by Reconcile-level tests.
- If `services/agent/test/e2e` or `services/version/test/e2e` already has a Jev stub that can return Critical, add one focused scenario:
  1. The Version is Critical-blocked, and `needsReview` is true.
  2. Read `contentDigest`.
  3. Apply a ScanPolicy with a jevExemption that has no `expires`.
  4. The Version syncs, and `exempted` is true.
  5. Remove the entry. The Version is re-blocked.
- Run only with `E2E_FOCUS` on an isolated Kind cluster name, never on the shared `opendepot` cluster.

## Helm Chart
- The regenerated CRDs in `chart/opendepot/crds/` (Step 1). Helm does not upgrade CRDs, so the upgrade note in Step 11 is required.
- One new RBAC rule in `templates/version-rbac.yaml`: `events` with `create` and `patch` (Step 6, Q5). Add it to the shared rules block so both the Role and the ClusterRole variants get it.
- No values, chart version or appVersion changes.

## Constraints for the Developer
- Never run `go build` in the 32Mi sidecar. Use `go build -o /dev/null` or `go vet`.
- Do not add sample skill content or the conventional-commit text as a fixture.
- Add comments only where the code needs them.
- Choose tests by the opendepot-testing skill guidance, and use the existing runners.
- Keep the change surgical: no unrelated refactors.
- Never hand-edit generated files (deepcopy, CRD YAML). Use the make targets.
- Run `git status` first and leave existing uncommitted changes untouched.
- No commits and no deploys.

## Risks
1. **D2 makes the override broader.** One exemption now clears every policy-gate reason for the digest, not just Critical. The JevPolicy thresholds a team set are bypassed for that archive, including probability limits unrelated to the Critical verdict.
   - Mitigations:
     - The exemption is digest-bound.
     - It is limited to the winning policy.
     - Non-gate reasons are never cleared (Q1).
     - The scored-every-part checks in `jevExemptible`.
     - `blockReasons` is retained, plus Events, and `needsReview` stays true.
   - **Residual risk:** the exemption is not bound to the reasons it was granted for. If the JevPolicy is tightened later and the Version is force-synced, the same exemption also clears the new reasons (Q6).
2. **D1 allows permanent exemptions.** An exemption without `expires` stays valid until the content changes. The Version stays published with no forced re-review. If the policy author never revisits it, nothing re-blocks the Version.
   - Mitigations: the docs recommend `expires`, "never" is shown in the status, Events and UI, and the active count.
   - Expiry is not enforced.
3. **D3 changes behavior.** `needsReview` becomes true for every Critical assessment, including ones with no JevPolicy and ones that are exempted. Any dashboards, alerts or consumers that key on `needsReview` will see more entries. This is called out in the upgrade note.
4. **D6 and D2 are inconsistent (Q4).** A Rejected branch Version blocked by Critical plus a threshold reason cannot be re-opened, while the same content from a tag source can be exempted.
5. **The UI editor drops `jevExemptions` on save** unless Step 10 lands. This is silent data loss. Ship Step 10 together with Steps 1 to 5.
6. **Cache reuse is not content-bound.** Cached assessments are reused without comparing content. This is pre-existing. It is mitigated here because the exemption requires the stamped digest to equal the current digest. Legacy assessments need one forceSync, which costs Jev tokens.
7. **Digest stability depends on the toolchain.** The digest depends on the Go `compress/flate` output and on the packing code. A Go upgrade or an archive change re-blocks exempted Versions. This fails closed and must be documented.
8. **`BlockReasons` changes meaning.** A non-empty `BlockReasons` with `Blocked=false` is new. Check consumers such as `services/server/agents.go` and the UI for "blocked if reasons are present" logic (Step 10 covers the UI; the Developer reports what the server does).
9. **Reason text becomes a contract.** Exemptibility depends on the gate reason prefixes. Step 3 pins them with constants and a regression test. Rewording a reason later would make stored assessments non-exemptible, which fails closed.
10. **ForceSync stays true on blocked Versions.** It is only reset on success, so every ScanPolicy edit re-runs Jev for those Versions. This is pre-existing and costs tokens.
11. **The RBAC fields in the brief are not the ones enforced.** `GroupBinding.scanPolicyManagement` is not what `policy_api.go` checks; `SecurityGroupBinding` is. A namespace-wide policy needs `namespaceWidePolicyManagement`. No change is needed; this is noted for the docs.
12. **New controller permission (Q5).** The version controller gains `events` `create` and `patch`. This is low risk, but clusters that audit RBAC diffs will see it.
