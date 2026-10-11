# Implementation Plan: Chunked Jev Assessment of Skills and Agents

## Summary
Today Jev sees only part of a Skill or Agent:
- `loadAgentContent` holds bundled file bodies against one shared 43 KiB budget. Files that don't fit go into `omittedFiles`.
- `buildJevState` cuts the whole state at `jev.MaxStateBytes`.
- `runAgentJev` makes one call. If content was dropped, it records `jevTruncatedError`, which also throws away the scores.
- Separately, `content.scripts` concatenates every script with no memory limit, for the Expr rules.

This change makes Jev see **all** bundled text content:
- The content is split into parts. Each part fits the 43 KiB state budget, and each part gets the same question set.
- The results are combined worst-case. If any part is Critical, the Version is Critical and is always blocked.
- The shared budget, `omittedFiles` and whole-state truncation are removed.
- There is a ceiling on parts per Version, and the Jev input is streamed from disk.
- The script concatenation is replaced by streaming the scripts in segments to the Expr rules.
- The cache gains a content digest, and ForceSync keeps working.
- Bump classification stays on its current single-call path.

## Affected Services
- **`api/v1alpha1`**: five new optional fields on `JevAssessment`, plus deepcopy and the six CRD copies.
- **`pkg/jev`**: `MaxStateBytes` stays 43 KiB but is now documented as the per-part budget. A helper is added that recognizes the token-limit rejection.
- **`services/version`**:
  - `checks/agents`: Expr rules evaluated over streamed script segments.
  - `agent_scan.go`: loads content without holding file bodies.
  - `agent_jev_chunks.go` (new): splits the content into parts.
  - `agent_jev_aggregate.go` (new): combines the part results.
  - `agent_jev.go`: rewired to use parts.
  - Controller: keeps the extracted temp dir until Jev finishes.
  - `branch_bump.go`: no code change, only a test.
- **`services/agent`**: e2e assertions only.
- **`docs/`**: agents guide and API reference.
- **Server, UI, CLI:** no changes. The new fields are additive. Showing them in the UI is out of scope.

## API / CRD Changes (generated-code step 1)
In `api/v1alpha1/types.go`, inside `JevAssessment` (lines 672–720), add these fields after `Error`. All are optional:
- `ContentDigest string \`json:"contentDigest,omitempty"\``
  - Markers: `// +optional`, `// +kubebuilder:validation:Pattern=^[0-9a-f]{64}$`
  - Doc: the SHA-256 (hex) of the split content and the splitting scheme. A cached assessment is reused only when this matches.
- `ChunkCount int32 \`json:"chunkCount,omitempty"\``
  - Markers: `// +optional`, `// +kubebuilder:validation:Minimum=0`
  - Doc: the number of parts the content was split into. It is the exact count even when it is above the ceiling.
- `AssessedChunks int32 \`json:"assessedChunks,omitempty"\``
  - Markers: `// +optional`, `// +kubebuilder:validation:Minimum=0`
  - Doc: the number of parts Jev answered successfully.
- `WorstChunk int32 \`json:"worstChunk,omitempty"\``
  - Markers: `// +optional`, `// +kubebuilder:validation:Minimum=0`
  - Doc: the 1-based index of the part with the worst risk result.
- `WorstChunkFiles []string \`json:"worstChunkFiles,omitempty"\``
  - Markers: `// +optional`, `// +kubebuilder:validation:MaxItems=20`
  - Doc: up to 20 paths whose content, or part of it, is in the worst part, in part order.

Also update the doc comment on `Error`: "...the error that prevented the assessment from completing, or why it is incomplete."

All the existing fields keep their names and now hold the combined result (see Aggregation). The change is additive and backward compatible.

## Design

### 1. Splitting the content into parts
- **Budget:** each part's state is at most `jev.MaxStateBytes` (44,032 bytes).
  - Every part starts with a preamble, and 2,048 bytes are reserved for it. That leaves 41,984 bytes of body per part.
  - The preamble reads "OpenDepot <kind> "<name>", part i of N". It also gives the description (capped at 1,024 bytes) and the tools (capped at 512 bytes).
  - Parts after the first add: "The entry file and the full bundled file list are in part 1."
  - The description and tools go in every part so Jev can still judge scope mismatch on a part that holds only a script.
- **Content order** is one stream, packed greedily:
  1. The entry file section, or `[withheld: entry file flagged for a secret]`.
  2. The bundled file list, one path per line.
  3. Each other file in sorted path order, under `--- <path> ---`. A file's body is replaced by `[skipped: binary file]` or `[withheld: flagged for a secret]` where those apply. Both markers stay as they are today.
- **File list placement:** part 1 only, starting there and continuing into later parts if it is long.
  - Reason: repeating it in every part costs (files × parts) bytes, and an archive can hold up to 10,000 entries.
  - Each later part's preamble points back to part 1.
- **Packing rule:**
  - An item that fits in the space left in the current part is appended.
  - An item that would fit in an empty part, but not in the space left, starts a new part. Small files are never split.
  - An item larger than a part's body is split across parts. This includes the entry file and a long file list.
- **Continuation marker for a split file:**
  - The first piece is headed `--- <path> [part 1 of k, bytes 0–a of S] ---` and ends with `[continues in part i+1]`.
  - Each later piece is headed `--- <path> [continued, bytes a–b of S] ---`.
  - A cut falls on the last newline in the final 1 KiB of space. If there is none, it falls on a UTF-8 rune boundary. The file list splits only between lines.
- **Two streaming passes, so memory stays bounded:**
  - **Pass A (layout, local disk only):**
    - Classifies each file by streaming it. A file is binary if it has a NUL byte or invalid UTF-8 anywhere, with runes split across read buffers handled correctly.
    - Computes the part layout and its exact count N.
    - Hashes the body bytes of each part as they will be sent, giving `ContentDigest`. The hash input includes the scheme id `jev-chunks-v1`, the budget and N.
    - Once N passes the ceiling, it stops recording the layout and only counts.
  - **Pass B (sending):** builds one part at a time from the recorded byte ranges.
  - Peak heap for the Jev path is one part plus the layout table. The entry file is already in memory, but the definition ConfigMap limit caps it at about 750 KB.
- **Temp-dir lifetime:** `runAgentScan` no longer deletes the extracted dir on success.
  - `agentContent` gets the fields `dir`, `entryRel` and a `release` function.
  - The controller defers `release()` right after `runAgentScan` succeeds, and `runAgentScan` releases the dir itself on its own error paths.
  - Note: `/tmp` is tmpfs, so the extracted tree uses RAM for the life of the Jev calls. It is no larger than the scan already needs.

### 2. Aggregation
- **RiskLevel:** the worst across parts, ranked Minimal < Low < Moderate < High < Critical.
  - A label outside this set is treated as a part error. This also prevents a status write that the CRD enum would reject.
- **RiskScore:** the maximum.
- **Injection, Exfiltration, Destructive, HiddenInstructions, ScopeMismatch, RemoteExecution:** the maximum per field.
- **SafeProbability:** the **minimum**. Higher is safer, so the worst value is the lowest. Taking the maximum would hide an unsafe part. **(Decision D4.)**
- **Worst part:**
  - Chosen by highest RiskLevel, then highest RiskScore, then lowest index.
  - `WorstChunk`, `WorstChunkFiles` and `Model` come from this part.
  - `RiskConfidence` is also the worst part's value, so the reported level and its confidence come from the same answer. Taking the minimum across parts would pair the worst level with a confidence from a different part.
- **NeedsReview:** true when **any** assessed part's confidence is below `minConfidence`. It still never blocks. **(D5.)**
- **Gate:** `evaluateJevGate` is unchanged and runs on the combined result. The thresholds are monotonic, so this is the same as gating each part and OR-ing the results. Critical from any part still always blocks.
- **EvaluatedAt:** the time the last part finished.

### 3. Incomplete cases (replaces `omittedFiles`, truncation and `jevTruncatedError`)
- `fileBodies`, `omittedFiles`, `readBundledPrefix`, `buildJevState`, `writeJevBundledBodies`, `jevFileListHeader`, `jevTruncatedError` and the `[truncated: …]` / `[omitted: …]` markers are all removed.
- **Oversized content can no longer be dropped**, so "truncated" and "oversized" stop being causes of an incomplete assessment.
- **The incomplete state still exists.** It is still recorded in `Error`, with the prefix `jev assessment incomplete:`. Incomplete assessments now **keep their combined scores** instead of discarding them as today. The causes, and what each does:

| Cause | Can an attacker cause it? | Behavior |
|---|---|---|
| A bundled file or the entry file is withheld for a secret | Yes (by planting a fake secret) | Same as today: blocks with thresholds; without thresholds it publishes and `syncStatus` names it (D8) |
| A binary file is skipped | Yes (by adding one invalid byte) | Same as today (D8) |
| A part fails with a network, auth, 4xx or 5xx error | No | Blocks with thresholds. Without thresholds it publishes (D3). The remaining parts are not sent. |
| The part count is above the ceiling | Yes (by padding) | **Always fails closed**, before any Jev call (D2) |
| A part is still over the token limit after adaptive splitting | Yes (with token-dense text) | **Always fails closed** (D6) |
| The per-Version deadline runs out | No | Treated like a part error (D1) |

- **Message format:** counts plus up to five paths. Examples:
  - `jev assessment incomplete: 1 file withheld for a secret, 2 binary files skipped (scripts/run.sh, assets/logo.png)`
  - `jev assessment incomplete: part 3 of 5 failed: <redacted error>; parts 3 to 5 were not assessed`
- **"Always fails closed"** means the assessment records `Blocked=true` with the reason in `BlockReasons`. The controller then writes `JevBlocked: …`, and a branch Version becomes Rejected.

### 4. Bounds
- **Ceiling:** `jevMaxChunks = 16` Jev calls per Version, as a constant (D2). This includes any calls added by adaptive splitting.
  - Pass A counts N first. When N is above 16, no call is made, and the result is `Blocked=true`, `ChunkCount=N`, `AssessedChunks=0`.
  - Reason text: "jev assessment incomplete: the content needs 23 parts of up to 44032 bytes, above the limit of 16 parts per Version; no Jev calls were made".
- **Calls run one after another, and stop early once the Version is blocked** (Critical or any threshold breach). Later results cannot unblock it. `AssessedChunks` shows the stop.
- **Per-Version deadline:** 5 minutes over all parts (D1).
- **Adaptive split (D6):**
  - When a part is rejected with HTTP 400 `max_tokens_exceeded`, its body is cut in two at a newline or rune boundary and each half is assessed. The two halves are labelled "part 3a" and "part 3b".
  - A part can be halved at most twice, so the smallest piece is about 10 KiB.
  - The extra calls count toward the 16-call ceiling.
  - The 43 KiB budget is a margin "not measured against the 32k-token budget" (comment in `pkg/jev/client.go`), so token-dense text can still be rejected without this.
- **Expr memory:** `content.scripts` is removed.
  - Each script file is streamed in segments of up to 1 MiB, each cut at the last newline.
  - A line longer than 1 MiB is cut there, and the last 64 KiB is carried into the next segment as an overlap.
  - The rules run once on the base environment (frontmatter, `Body`, and the domain set gathered from the body and all segments) and once per segment with `Scripts` set to that segment.
  - Each fired rule produces one finding, in `rules.yaml` order.
  - A domain match that touches a mid-line cut is ignored, because the overlap scans it again whole.
  - The script rules match within a single line, so per-segment evaluation fires on the same content. `$` in the unpinned-install rule now also matches at the end of each file, which is stricter.
  - Residual risk: a match inside a single line over 1 MiB, with a gap wider than 64 KiB, could be missed. Jev still sees that content.

### 5. Caching
- A cached assessment is reused only when `!ForceSync`, `cached.Error != jevDisabledError` **and** `cached.ContentDigest == digest`.
- Pass A runs before the cache check. It reads only local disk and makes no Jev call.
- ForceSync works exactly as today: it always re-runs.
- **Legacy assessments** (no digest) are re-run the next time the Version reaches the Jev step.
  - Synced Versions take the storage fast path and never reach it, so they keep their old assessment until ForceSync (D7).
- Side benefit: a tag whose content changed is now re-assessed. Today the old cache would be reused.
- The digest covers what Jev sees, including withheld markers, so a change in what Trivy flags also invalidates the cache.

### 6. Bump path (outcome: unchanged, one Jev call)
`branch_bump.go` is not changed. `buildBumpState` still sends one diff state, cut by `truncateUTF8` at `jev.MaxStateBytes`. Reasons:
- Bump size is one judgement about the whole change. There is no sound worst-of-N rule for Major, Minor and Patch, and taking the maximum major probability over parts would over-bump.
- It is not a security gate. The new tree's full split assessment runs in `runAgentJev` **before** `assignBranchVersion`.
- The facts header (tools, model, description, file list) comes first and survives truncation.
- Bump runs on every commit, so splitting it would multiply cost.

Because `loadAgentContent` keeps its signature and its `files`, `definition`, `entryName` and `entryFlagged` fields, the bump code compiles unchanged. A test pins the single call (step 8).

## Implementation Steps

### Step 1: [GENERATED CODE] Add the `JevAssessment` fields, deepcopy and CRDs (single lane)
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
- **Changes:**
  - Add the fields in API / CRD Changes.
  - Run `make generate manifests` in each service that has the targets. `make generate` is needed because `WorstChunkFiles` is a slice and needs deepcopy.
  - Copy the generated Versions CRD so all six copies are byte-identical.
  - Do not bump `Chart.yaml` or any other version.
- **Acceptance:**
  - All six YAML files are identical (`diff`).
  - `helm lint chart/opendepot` passes.
  - `cd test/chart && GOWORK=off go test ./... -count=1` passes.
  - Tier 1 passes in every service.

### Step 2: Token-limit helper and per-part budget comment in pkg/jev
- **Depends on:** none
- **Files:** `pkg/jev/client.go`, `pkg/jev/client_test.go`
- **Changes:**
  - Keep `MaxStateBytes = 43 << 10`.
  - Reword its comment: it is the budget for **one part**, callers split larger content, and the margin is still unmeasured against the token budget.
  - Add an exported helper that reports whether an error is an `*APIError` with status 400 whose redacted message contains `max_tokens_exceeded`.
  - No change to retries.
  - No `go.mod` edit (`services/version` already replaces `pkg/jev`).
- **Acceptance:** tests cover:
  - a 400 `max_tokens_exceeded` is matched;
  - a 400 with another message, a 429, a 500 and a wrapped non-API error are not matched;
  - the token never appears in the message.

### Step 3: Streamed Expr evaluation in the agents rule package
- **Depends on:** none
- **Files:** `services/version/checks/agents/agents.go`, `services/version/checks/agents/agents_test.go`
- **Changes:**
  - Add a `RuleSet` method that takes a base env and a sequence of script segments. It returns one finding per fired rule, in `rules.yaml` order.
  - Add a helper that collects the deduplicated domain set across segments. It ignores a match that touches a mid-line cut.
  - Update the `AgentRuleEnv` comment: rules see all content, as segments, not "the truncated Jev state".
- **Acceptance:** tests cover:
  - remote-exec in the 2nd segment fires;
  - unpinned-install at the very end of a file fires;
  - a rule firing in several segments gives one finding;
  - the domain rule uses the union of domains;
  - a URL spanning a mid-line cut is not reported as a truncated host.

### Step 4: Part planner and emitter
- **Depends on:** none
- **Files:**
  - `services/version/internal/controller/agent_jev_chunks.go` (new)
  - `services/version/internal/controller/agent_jev_chunks_test.go` (new)
- **Changes:**
  - Implement Design §1 and the ceiling count from §4.
  - The input struct is defined in this file: dir, entry relative path, entry flagged, flagged base names, sorted files, kind, name, description, tools. It does not depend on `agentContent`, so this step is independent.
  - Pass A outputs: the layout, N, the digest, the incomplete causes (withheld and binary paths) and the files in each part.
  - Pass B builds part i on demand. A function cuts a part in half for the adaptive split.
  - Constants: `jevMaxChunks = 16`, a 2,048-byte preamble reserve, 1 MiB Expr segments and the scheme id `jev-chunks-v1`. Expr segments may share the cut helper.
- **Acceptance (unit tests):**
  - **Boundaries:** body sizes of exactly capacity, capacity + 1, and capacity − 1 followed by a 2-byte item.
  - **Rune and newline cuts:** a multi-byte rune straddling the cut, and a newline inside and outside the final 1 KiB window.
  - **Oversized file:** a single 100 KiB text file splits across 3 parts, carries the continuation markers and correct byte ranges, and the parts reassemble to the original bytes.
  - **Large entry file:** the entry file is split too, and the file list continues across parts.
  - **Small files:** a file that fits an empty part is never split.
  - **Withheld and binary files:** both the entry and a bundled file are withheld and their content never appears. NUL bytes, invalid UTF-8 late in a file and a rune split across the read buffer are all detected as binary.
  - **Every part fits:** each part is ≤ `jev.MaxStateBytes` with its preamble, including the largest index and the longest description.
  - **Digest stability:** same input gives same digest; a change in one byte, in the flagged set or in N gives a different digest.
  - **Ceiling:** 16 parts pass, 17 report N=17 and record no layout past the ceiling.
  - **Memory:** pass B never holds more than one part.

### Step 5: Combine part results
- **Depends on:** none
- **Files:**
  - `services/version/internal/controller/agent_jev_aggregate.go` (new)
  - `services/version/internal/controller/agent_jev_aggregate_test.go` (new)
- **Changes:** implement Design §2: the risk-level ranking, the combined `jev.Result`, the worst-part index with its tie-breaks, the any-part low-confidence flag, and an unknown label as an error.
- **Acceptance (unit tests):**
  - Worst level wins, Critical in any position wins, and a single part gives a pass-through result.
  - Maximum per probability field, minimum `SafeProbability`, maximum score.
  - Ties: same level, so higher score wins; same score, so lower index wins.
  - Confidence comes from the worst part, and needsReview is set from any part.
  - An unknown label is an error.
  - `evaluateJevGate` on the combined result gives the same reasons as OR-ing the gate over each part.

### Step 6: Rewire content loading, Expr and runAgentJev
- **Depends on:** 1, 2, 3, 4, 5
- **Files:**
  - `services/version/internal/controller/agent_scan.go`
  - `services/version/internal/controller/agent_jev.go`
  - `services/version/internal/controller/agent_jev_test.go`
  - `services/version/internal/controller/agent_scan_test.go` (new)
- **Before starting:**
  - Search `services/version` for `fileBodies`, `omittedFiles`, `readBundledPrefix`, `buildJevState`, `writeJevBundledBodies`, `jevTruncatedError`, `jevFileListHeader` and `.scripts`.
  - Add any other test file that matches to this step's file list. It must not be a file owned by step 7 or 8.
- **`agent_scan.go`:**
  - Remove the bodies, the omitted set, the scripts and the prefix reader. `loadAgentContent` keeps its signature and records `dir` and `entryRel`.
  - `runAgentScan` sets `release` instead of deferring cleanup, and releases on its own error returns.
  - The Expr call uses step 3 with segments streamed from disk.
- **`agent_jev.go`:**
  - `runAgentJev` order:
    1. ref, required and disabled checks (unchanged);
    2. pass A;
    3. cache check with the digest;
    4. ceiling check (always fails closed);
    5. token read;
    6. parts sent one after another under a 5-minute deadline, with adaptive split on the step-2 error and early stop on a block;
    7. aggregate (step 5);
    8. gate;
    9. incomplete handling per Design §3.
  - Fill the five new status fields.
  - `jevFailure` and `jevPublishedStatus` keep their contracts. The messages gain the incomplete detail.
  - Log each part at V(5): index, bytes, duration, level. Never log the state or the token.
- **Acceptance (`agent_jev_test.go`, using the existing `httptest` fixture):**
  - A 3-part skill makes 3 calls and every part's state is ≤ the limit.
  - Critical only in part 2 blocks without thresholds, and the call count is 2 (early stop).
  - A threshold breach only in the last part blocks.
  - Part 2 returning 503:
    - with thresholds: error, scores from part 1 kept, `AssessedChunks=1`;
    - without thresholds: publishes, the message names the part.
  - 17 parts make 0 calls, `Blocked`, a clear reason, with and without thresholds.
  - A 400 `max_tokens_exceeded` on part 1 splits it and succeeds. Repeated rejection fails closed without thresholds.
  - A cache hit with the same digest makes 0 calls. A different digest re-runs. A legacy assessment with no digest re-runs once. ForceSync re-runs.
  - A withheld file gives an incomplete assessment that keeps its scores.
  - The token never appears in states, the assessment or the logs (the existing sentinel tests are kept).
  - The old `buildJevState` and `omittedFiles` tests are replaced by their part-based versions.
- **Acceptance (`agent_scan_test.go`):**
  - The temp dir exists after `runAgentScan` succeeds and is gone after `release()` and after an error.
  - A script over 2 MiB fires remote-exec placed past the first MiB.

### Step 7: Controller wiring and controller-path test
- **Depends on:** 6
- **Files:**
  - `services/version/internal/controller/opendepot_versions_controller.go`
  - `services/version/internal/controller/agent_versions_test.go`
- **Changes:**
  - Right after the `runAgentScan` error check, defer `content.release()` so every return path, including branch assignment, cleans up.
  - No other logic changes. The `JevUnavailable:` and `JevBlocked:` handling and `persistAgentBlock` already carry the new status fields.
- **Acceptance (`agent_versions_test.go`, existing reconcile harness with stubbed Trivy and a Jev `httptest`):**
  - A multi-part Skill reconciles to synced, with `chunkCount`, `assessedChunks`, `worstChunk` and `contentDigest` set.
  - A Critical part gives `synced:false`, a `JevBlocked:` syncStatus, and Rejected for a branch Version.
  - Over the ceiling gives `JevBlocked:` with no Jev call.
  - A failed part with thresholds gives `JevUnavailable:`.
  - A second reconcile with no ForceSync makes 0 calls.
  - The extracted temp dir does not exist after the reconcile.

### Step 8: Pin the bump path to a single call
- **Depends on:** 6
- **Files:** `services/version/internal/controller/branch_bump_test.go`
- **Changes:** add a test: a tree large enough to need 3 assessment parts still gives exactly one `ClassifyBump` call, with a state ≤ `jev.MaxStateBytes`. `branch_bump.go` is not edited.
- **Acceptance:** `make test` in `services/version` passes.

### Step 9: Docs
- **Depends on:** 1, 6
- **Files:** `docs/guides/agents.md`, `docs/reference/api.md`
- **`docs/guides/agents.md`:**
  - **L155:** add that the content is split into parts that each fit the Jev budget, each part gets the same questions, and the recorded answers are the worst across parts.
  - **L226** ("Fail closed only with thresholds"):
    - Remove the truncated and oversized wording.
    - Keep the withheld and binary rules.
    - Add that a failed part follows the thresholds rule.
    - Add that exceeding 16 parts, or a part still over the token limit after splitting, always blocks.
    - Keep "Critical always blocks", adding "from any part".
  - **L228:** the cache is keyed by `contentDigest`; ForceSync re-runs it; older assessments are re-run when the Version is next assessed.
  - **New subsection "How content is split"** after the thresholds bullets:
    - the order, the file list in part 1 only, the continuation markers, the 16-part ceiling;
    - the aggregation table, including `safeProbability` as the minimum;
    - `worstChunk` and `worstChunkFiles`;
    - early stop;
    - the cost note: up to 16 calls per Version, and per commit in branch mode.
  - **L244:** bump classification is not split. It sends one request, and a diff longer than 43 KiB is cut.
  - **L276:** "sends a single request" becomes "sends one request per part, at most 16 per Version".
  - **L278 (Sent):** rewrite. Nothing is cut or omitted for size. The entry file, the file list and every text file are sent across parts, and a large file is split with continuation markers.
  - **L279–280:** binary and withheld stay as they are, with "incomplete" kept.
- **`docs/reference/api.md`:** add the five fields to the `JevAssessment` section (check that the section exists).
- **Acceptance:** `mkdocs build --strict` passes.

### Step 10: Agent e2e assertions
- **Depends on:** 7
- **Files:** `services/agent/test/e2e/e2e_test.go`
- **Changes:**
  - In the Skill and Agent Jev specs, assert:
    - `jevAssessment.chunkCount ≥ 1`;
    - `assessedChunks == chunkCount`;
    - `contentDigest` matches `^[0-9a-f]{64}$`;
    - `error` is empty.
  - For the Agent (`.github/agents` at tag `0.9.0`, eight agent files):
    - First measure the extracted size at that tag (`git archive 0.9.0 .github/agents`).
    - If it is above 41,984 bytes, also assert `chunkCount ≥ 2`, so the live multi-part path is covered.
    - Note the risk: with `jevPolicy: {}`, a live Critical on any part would block and fail the spec.
- **Acceptance:** the focused agent e2e passes.

### Step 11: Security Review (gate; no files)
- **Depends on:** 6, 7
- **Files:** none
- **What to check:**
  - attacker-controlled splitting: padding, token-dense text, fake `--- path ---` and `[continues in part …]` markers inside content;
  - the always-fail-closed cases;
  - the withheld and binary evasions without thresholds (D8);
  - that the token never reaches a state, a log or the status;
  - temp-dir cleanup;
  - the Expr overlap and its residual risk.
- **Acceptance:** written sign-off, or findings fixed in steps 4 or 6.

### Step 12: Focused validation (no files)
- **Depends on:** 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11
- **Files:** none
- **Acceptance:** all checks in E2E Test Changes pass.

**Waves:**
- A: 1, 2, 3, 4, 5. Step 1 is the only generated-code step, so it runs in a single lane.
- B: 6
- C: 7, 8, 9
- D: 10, 11
- E: 12

No two independent steps share a file.

## E2E Test Changes
- **Tier 1:** `go build ./... && go vet ./...` in `api/v1alpha1`, `pkg/jev`, and each of `services/version`, `agent`, `depot`, `module`, `provider` and `server`.
- **Tier 2:**
  - `cd pkg/jev && go test ./... -count=1`
  - `cd services/version && go test ./checks/agents/... -count=1 && make test`
- **Chart:** `helm lint chart/opendepot`, then `cd test/chart && GOWORK=off go test ./... -count=1`.
- **Tier 3 (focused):**
  - `cd services/agent && make test-e2e E2E_FOCUS="Skill|Agent" KEEP_KIND=1 KIND_CLUSTER=opendepot-focus-agent`, with `OPENDEPOT_JEV_API_KEY` set (live Jev). During this run, record the time per part from the V(5) logs to check the latency assumption in D1.
  - `services/version`: focus the narrowest agent or Jev Context. Find it with `grep -n -E '(Describe|Context)\("' services/version/test/e2e/e2e_test.go`. If none exists, tiers 1 and 2 cover it.
  - depot, module, provider and server: tiers 1 and 2 only, because the CRD change is additive.
- **Docs:** `mkdocs build --strict`.
- No full `make test-e2e` run.

## Helm Chart
- Only the regenerated `chart/opendepot/crds/opendepot.defdev.io_versions.yaml` (step 1).
- No values, RBAC or template changes. The ceiling is a constant (D2).
- **No chart or app version bump.**

## Out of Scope
- UI and CLI display of the new fields.
- Splitting the bump classifier.
- Sending invalid-UTF-8 text with replacement characters instead of skipping it (D8 follow-up).
- A chart value for the ceiling (D2 alternative).
- Running parts in parallel.
- Re-assessing already-synced Versions automatically.

## Open Decisions (each has a recommended default; none block the plan)
1. **D1: Cost and latency.**
   - **Cost:** the worst case is 16 calls × about 33k input tokens, roughly 530k tokens per Version. It is paid once per Version because of the cache, but in branch mode it is paid **per commit**.
   - **Latency:** I have no measured per-call latency. At an assumed 3–10 s per call, 16 parts take 48–160 s on one reconcile worker. The worst case with retries is held to the 5-minute deadline.
   - **Accuracy:** taking the worst of N parts raises the false-positive rate as N grows. More parts means more chances that one benign part reads as High or Critical.
   - *Recommend:* send parts one after another, stop once blocked, use a 5-minute per-Version deadline, and measure in the step 10 e2e.
2. **D2: The ceiling.** *Recommend 16 parts or calls per Version*, which is about 656 KiB of text after preambles, as a code constant. Exceeding it always fails closed, before any call. Reason: padding is the cheapest way around the Critical block. Alternative: a `--jev-max-chunks` flag with a chart value, which changes the chart (values only, no version bump).
3. **D3: A failed part with no thresholds.** *Recommend keeping today's opt-out (publish).* The error is recorded and `syncStatus` names the part. Network, auth and 5xx failures are not attacker-controlled, and failing closed on a partial failure while a total failure publishes would be inconsistent. Failures an attacker can cause (ceiling, token limit) always fail closed. Alternative: fail closed on any part error once any part has been sent.
4. **D4: SafeProbability uses the minimum, not the maximum.** Your wording said "maximum per field". Safe is higher-is-better, so the maximum would hide an unsafe part. *Recommend the minimum.*
5. **D5: Confidence.** *Recommend* `riskConfidence` from the worst part, and `needsReview` set when any part's confidence is below `minConfidence`. That flag never blocks.
6. **D6: Adaptive split on `max_tokens_exceeded`.** *Recommend including it now* (at most two halvings, counted toward the ceiling). Without it, token-dense text can fail every time. That blocks legitimate dense content when thresholds are set, and skips the Critical check when they are not.
7. **D7: Legacy cached assessments.** *Recommend:* re-run only when the Version reaches the Jev step again. Synced Versions keep their old possibly-truncated assessment until ForceSync. This avoids a cost spike on upgrade.
8. **D8: Withheld and binary evasion without thresholds.** A fake secret or one invalid byte can keep a payload from Jev. *Recommend* keeping today's behavior in this change: Trivy's secret findings may already block under ScanPolicy, and blocking every bundled image would be a regression. Plan a follow-up that sends invalid UTF-8 with replacement characters and treats only NUL-containing files as binary.
9. **D9: File list placement.** *Recommend part 1 only*, with a pointer to it in every later part's preamble. Repeating it costs files × parts bytes.
