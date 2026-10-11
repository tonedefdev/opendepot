# Implementation Plan: Chunked Jev Assessment of Skills and Agents (Revision 3, with D1–D13 applied)

Revision 3 applies the user's answers to the Revision 2 open questions (Q1–Q7) and the rate-limit answer. It replaces the base plan from commit 7156a69.

Things that change most from the base plan:
- **Limited automatic retry.** A Version whose Jev assessment fails with a service error is retried automatically 15 seconds later, until `jevAssessment.retryCount` exceeds the controller limit (`jevMaxRetries`, default 3 retries, so 4 attempts in total, set by the Helm value `scanning.jev.maxRetries`). 401 and 403 failures are re-run but don't count toward the limit. Only ForceSync resets the counter (D11).
- **Jev disabled fails closed.** A source that sets `jevSecretRef` on a controller with Jev disabled is no longer published (D10).
- **Binary files block.** Following D8, any Skill or Agent that bundles an image or other binary file can't publish while Jev is configured.
- **More content goes to TypeSafe.** Today Jev receives at most 43 KiB per Version. This change sends all bundled text, up to 16 parts.
- **The bump path is edited after all.** Step 8 now changes `branch_bump.go`: bump calls share the 16-request in-flight cap and the 1,000-per-minute rate limit, and `buildBumpState` streams files through a fixed-size buffer instead of reading whole files (D12). The base plan left this file alone.
- **No whole-file reads.** File content is read through a buffered stream reader, never loaded whole into memory (D12).
- **Docs and chart comments contradict the decisions.** `docs/guides/agents.md` currently says the opposite of D3, D5 and D8, and the `scanning.jev.enabled` comment in `chart/opendepot/values.yaml` calls Jev advisory. Both are updated in this change (D13; details in step 9).
- **Wrong command in the base plan.** It said to run `make generate` in each service, which does not regenerate deepcopy for the shared API types (details in step 1).

## Summary
Today Jev sees only part of a Skill or Agent:
- `loadAgentContent` holds bundled file bodies against one shared 43 KiB budget. Files that don't fit go into `omittedFiles`.
- `buildJevState` cuts the whole state at `jev.MaxStateBytes`.
- `runAgentJev` makes one call. If content was dropped, it records `jevTruncatedError` and throws away the scores.
- Separately, `content.scripts` joins every script into one string with no memory limit, for the Expr rules.

This change makes Jev see all bundled text:
- **Splitting:** the content is split into parts of at most 43 KiB each. Up to 16 parts are scored at the same time, and every part gets the same questions.
- **Per-part results:** each part's result is kept in `status.jevAssessment.chunks` and shown in a new tree view in the UI.
- **Combined result:** parts are combined worst-case.
- **Fail closed:** when Jev is configured, the Version is not published if any part can't be scored, if any content can't be read, or if Jev is disabled on the controller.
- **Ceiling:** there is a hard limit of 16 Jev calls per Version, checked before any call is made.
- **Token-limit retry:** a part that Jev rejects for its token limit is cut in half and retried, at most twice.
- **Low confidence:** when `minConfidence` is set, any part whose confidence is below it blocks the Version.
- **No mass re-assessment:** existing assessments are never re-run, except a failed assessment's automatic retry (limited by D11) and ForceSync, which resets the retry counter.
- **Bump classification** keeps its single-call path. It joins the shared concurrency cap and rate limit, and it streams files instead of reading them whole.

## Decisions (resolved)

| ID | Final value | Changed from base plan? |
|---|---|---|
| D1 | Parts run concurrently using an errgroup, under a controller-wide limit of 16 Jev requests in flight and a controller-wide rate limit of 1,000 requests per minute. There is no early stop. Every part's result is kept in `status.jevAssessment.chunks` and shown in the UI tree. The 5-minute deadline per Version stays. | **Changed.** The base sent parts one at a time and stopped early. |
| D2 | `jevMaxChunks = 16` Jev calls per Version, as a code constant. Calls made by the token-limit retry count toward it. Going over fails closed before any Jev call. | Same value. A test plan for edge cases is added. |
| D3 | **Fail closed always.** When Jev is configured and any part can't be scored, the Version is not synced and can't be pulled. "Can't be scored" means a network, auth, 4xx or 5xx error, a timeout or deadline, the token limit, a bad response, an unknown risk label, or a failed token-Secret read. This happens with or without thresholds. It also covers a controller with Jev disabled (D10). | **Changed, and a behavior change.** The base kept today's publish-on-failure when no thresholds are set. |
| D4 | SafeProbability takes the **minimum**. RiskScore and the six risk probabilities take the **maximum**. The worst RiskLevel wins. Critical in any part always blocks. | Same. |
| D5 | When `minConfidence` is set, a part whose confidence is below it **blocks** the Version, and `needsReview` is also set. When `minConfidence` is unset, confidence has no effect. The top-level `riskConfidence` becomes the minimum across parts. **Pull gate choice: reuse `JevAssessment.Blocked` and `BlockReasons`, with no separate field.** Reason: the server (`agentVersionServable` requires `Synced && !Blocked`) and the CLI (`cli/install.go` refuses `meta.Assessment.Blocked`) already gate on `Blocked`. A new field would need server and CLI changes, and `cli/` is off limits. | **Changed.** The base said confidence never blocks and took the worst part's confidence. |
| D6 | The token-limit retry is included. When Jev rejects a part with HTTP 400 `max_tokens_exceeded`, the part is cut in half, at most twice per part. Every call counts toward the 16. If a piece still can't be scored, the part gets the status `TokenLimitExceeded` with a plain-language reason. The UI and `syncStatus` show it, and the Version is blocked. | The value is the same. The fail-closed outcome without thresholds and the explicit display are now required. |
| D7 | **No mass re-assessment.** A cached assessment is reused, whether it is legacy or new, except in two cases: ForceSync is set, or a failed assessment is retried automatically (D11). Automatic retries stop at the limit (D11), and ForceSync resets the counter. There is no content digest and no scheme check. Legacy assessments keep their old gate meaning when reused. A synced Version that has no assessment yet and misses the fast path (missing signature or definition) gets its first assessment; the user confirmed this is acceptable under D7. | **Changed.** The base re-ran legacy and digest-mismatched assessments. `ContentDigest` is dropped. Revision 3 adds the limited automatic retry. |
| D8 | **Fail closed** when Jev is configured and any content can't be read. This covers a file withheld because Trivy flagged a secret (entry or bundled), a binary file (a NUL byte or invalid UTF-8), and any other skipped content. The check runs before any Jev call. | **Changed, and a behavior change.** The base kept today's publish-without-thresholds behavior. |
| D9 | The file list goes in part 1 only. Later parts' preambles point back to part 1. | Same. |
| D10 | **Jev disabled fails closed.** When a source sets `jevSecretRef` and Jev is disabled on the controller (`jevDisabledError`), the Version is not published, with or without thresholds. It takes today's thresholds-set path for this case: `runAgentJev` returns the assessment plus an error, and the assessment has `Blocked=true`. No Jev call is made. A `jevDisabledError` assessment still re-runs once Jev is enabled. | **New in Revision 3, and a behavior change.** Revision 2 kept today's publish-unless-thresholds behavior. |
| D11 | **Limited automatic retry.** A failed assessment (the Service outcome in Design §3, `JevUnavailable:`) is re-run automatically 15 seconds later (`RequeueAfter`), until `retryCount` exceeds `jevMaxRetries`. `retryCount` counts failed attempts, except 401 and 403, which don't count: the first attempt is attempt 0, and `retryCount` is 1 after the first failure. `jevMaxRetries` is a controller flag, default 3 retries (attempts 0 to 3), set through the Helm value `scanning.jev.maxRetries`. Only ForceSync resets the counter to 0, and that run is attempt 0, even at the limit. The first real assessment after a `jevDisabledError` is attempt 0. A successful assessment does not reset the counter. The limit protects the user's Jev budget, since each run costs up to 16 calls. 401 and 403 are the exception: they re-run with no cap, so an expired or rotated token recovers as soon as the Secret is updated. | **New in Revision 3.** Revision 2 had no automatic retry and no limit on ForceSync re-runs. |
| D12 | **No whole-file reads.** All file content that the Jev assessment, the Expr rules and the bump path read goes through a buffered stream reader with a fixed-size buffer. Code never loads a whole file into memory, so memory stays within the controller's limits. This includes `buildBumpState`, which today uses `os.ReadFile`. | **New in Revision 3.** |
| D13 | **Docs and chart comments are updated in this change.** `docs/` and the Jev comments in `chart/opendepot/values.yaml` are updated to match D3, D5, D8, D10 and D11. The chart changes are comment text only. | **New in Revision 3.** Revision 2 left the chart comments open. |

**Concurrency cap: 16 Jev requests in flight across the whole controller, and a rate limit of 1,000 requests per minute.** The user confirmed both. Jev publishes a limit of 1,200 requests per minute (about 20 per second); the controller's limit of 1,000 per minute stays below it. I couldn't find an existing Jev rate in the code:
- `pkg/jev` has no rate limiter. It only retries 429 and 5xx up to 3 times, with 0.5, 1 and 2 s backoff.
- Today's peak is 4 requests in flight, because `MaxConcurrentReconciles: 4` and each reconcile makes one call at a time.

So:
- A shared semaphore of size 16 (`jevMaxInFlight = 16`) caps in-flight requests controller-wide. A shared rate limiter of 1,000 requests per minute (`jevRateLimitPerMinute = 1000`, using `golang.org/x/time/rate`, burst 16 = `jevMaxInFlight`) keeps Jev under its 1,200 per minute limit. The bump classifier takes the same semaphore and the same rate limiter. Each attempt, including pkg/jev's internal 429 and 5xx retries, takes its own semaphore slot and rate-limit token.
- Each Version's errgroup is not limited separately. The shared semaphore bounds it.

## Affected Services
- **`api/v1alpha1`:**
  - new `JevAssessment` fields (including `retryCount`) and two new struct types;
  - updated doc comments on `JevAssessment.Blocked` and `JevPolicy.MinConfidence`;
  - deepcopy and the six CRD copies.
- **`pkg/jev`:** the `MaxStateBytes` comment now says it is a per-part budget, and a token-limit error helper is added.
- **`services/version`:**
  - Expr rules evaluated over streamed script segments (`checks/agents`);
  - `agent_scan.go` no longer holds file bodies;
  - new part planner (`agent_jev_chunks.go`);
  - new aggregation and gate (`agent_jev_aggregate.go`);
  - `agent_jev.go` rewired: concurrency, the D3, D7, D8, D10 and D11 rules, and per-part status;
  - controller: the shared Jev semaphore and rate limiter, and the temp dir kept until Jev finishes;
  - `branch_bump.go`: the semaphore and rate limiter around `ClassifyBump`, and streamed file reads in `buildBumpState` (D12);
  - `go.mod`: `golang.org/x/sync` and `golang.org/x/time` become direct dependencies.
- **`services/ui`:** new API types, a tree builder, a tree component, and `JevPanel` changes.
- **`services/agent`:** e2e assertions only.
- **`docs/`:** `guides/agents.md` and `reference/api.md`.
- **`chart/opendepot/values.yaml`:** comment text only (D13).
- **No code change:**
  - `services/server`: it passes the CRD struct straight through in `BrowseResourceDetail.JevAssessment` and `AgentDownloadResponse.Assessment`, so the new fields flow through.
  - `cli/`: off limits. It already blocks on `Blocked`.
  - Chart, apart from the regenerated CRD and the `values.yaml` comments.

## API / CRD Changes (step 1, generated code)
In `api/v1alpha1/types.go`, add these to `JevAssessment` after `Error`. All are optional.
- **`ChunkCount int32` (`json:"chunkCount,omitempty"`)**
  - Markers: `+optional`, `+kubebuilder:validation:Minimum=0`.
  - Doc: the planned number of parts. It is exact when it is at most 16. Above 16 it is a lower bound, and no Jev call was made.
- **`AssessedChunks int32` (`json:"assessedChunks,omitempty"`)**
  - Markers: `+optional`, `Minimum=0`, `Maximum=16`.
  - Doc: the number of part entries Jev scored.
- **`Chunks []JevChunkResult` (`json:"chunks,omitempty"`)**
  - Markers: `+optional`, `+kubebuilder:validation:MaxItems=16`.
  - Doc: one entry per scored or attempted part, sorted by label.
- **`SkippedFiles []JevSkippedFile` (`json:"skippedFiles,omitempty"`)**
  - Markers: `+optional`, `MaxItems=32`.
  - Doc: files Jev could not read. Any entry here blocks the Version.
- **`SkippedFileCount int32` (`json:"skippedFileCount,omitempty"`)**
  - Markers: `+optional`, `Minimum=0`.
  - Doc: the total number of skipped files. `SkippedFiles` holds at most 32 of them.
- **`RetryCount int32` (`json:"retryCount,omitempty"`)** (D11)
  - Markers: `+optional`, `+kubebuilder:validation:Minimum=0`.
  - Doc: the number of failed attempts since the last ForceSync, which resets it to 0. 401 and 403 failures don't count. The first attempt is attempt 0, and this is 1 after the first failure. Automatic retries stop once it exceeds `jevMaxRetries` (default 3, so attempts 0 to 3).
  - The controller carries the value forward when it replaces the assessment, adds 1 on each failed attempt except 401 and 403, and sets it to 0 on a ForceSync or after a `jevDisabledError`.

New type `JevChunkResult`:
- **`Index int32` (`json:"index"`):** the 1-based planned part. Markers: `Minimum=1`, `Maximum=16`.
- **`Label string` (`json:"label"`):** the index plus any halving suffix, for example `3`, `3a` or `3ab`. Markers: `Pattern=^([1-9]|1[0-6])[ab]{0,2}$`, `MaxLength=4`.
- **`Files []string` (`json:"files,omitempty"`):** the paths whose content, or part of it, is in this part, entry file included. Markers: `+optional`, `MaxItems=32`, `+kubebuilder:validation:items:MaxLength=256`.
- **`FileCount int32` (`json:"fileCount"`):** the exact number of files in this part. Marker: `Minimum=0`.
- **`Status string` (`json:"status"`):** marker `Enum=Scored;Failed;TokenLimitExceeded;NotSent`.
- **`RiskLevel string`:** `+optional`, `Enum=Minimal;Low;Moderate;High;Critical`.
- **`RiskScore`, `RiskConfidence`, `SafeProbability *float64`:** `+optional`.
- **`Error string`:** `+optional`, `MaxLength=512`. The reason, redacted.

New type `JevSkippedFile`:
- **`Path string` (`json:"path"`):** marker `MaxLength=256`.
- **`Reason string` (`json:"reason"`):** marker `Enum=Withheld;Binary`.

The controller must never write a value that fails validation:
- paths longer than 256 bytes are cut at a rune boundary to the limit, without a suffix, so the CRD MaxLength holds;
- lists are capped at 32 items;
- errors are capped at 512 bytes.

The worst-case added status size is about 140 KiB.

Doc comment updates (these change the CRD descriptions):
- **`JevAssessment.Blocked`:** "Whether Jev blocks the Version: a threshold, a Critical risk level, a part below minConfidence, content Jev could not read, Jev disabled on the controller, or an assessment that could not complete."
- **`JevAssessment.Error`:** "...or why the assessment is incomplete."
- **`JevPolicy.MinConfidence`:** "A Version is blocked and flagged for review when any part's risk confidence is below this value."

Dropped from the base plan: `ContentDigest`, `WorstChunk` and `WorstChunkFiles`. D7 removes the cache key, and the UI derives the worst part from `chunks`. The change stays additive. Existing fields hold the combined result.

## Design

### 1. Splitting (D9; budget unchanged)
- **Budget per part:**
  - each part's state is at most `jev.MaxStateBytes` (44,032 bytes);
  - 2,048 bytes are reserved for the preamble, which leaves 41,984 bytes of body.
- **Preamble:**
  - `OpenDepot <kind> "<name>", part <label> of N`;
  - the description, capped at 1,024 bytes;
  - the tools, capped at 512 bytes.
  - Parts 2 to N add "The entry file and the full bundled file list are in part 1."
- **Content order:**
  1. the entry file;
  2. the bundled file list, which starts in part 1 and continues into later parts if it is long;
  3. every other file in sorted path order, under `--- <path> ---`.
- **Packing rule:**
  - an item that fits in the space left is appended;
  - an item that would fit in an empty part starts a new part, so small files are never split;
  - a larger item is split across parts with the continuation headers `[part 1 of k, bytes a–b of S]`, `[continued, bytes a–b of S]` and `[continues in part …]`.
- **Where cuts fall:**
  - on the last newline in the final 1 KiB of space;
  - otherwise on a UTF-8 rune boundary;
  - the file list is only split between lines.
- **Pass A (local disk only, after Trivy):**
  - streams every file through a fixed-size buffer and classifies it for D8 (NUL byte or invalid UTF-8, with runes split across read buffers handled correctly); no file is read whole (D12);
  - matches `flaggedFiles` by base name, as today;
  - computes the layout as (file, byte range) segments;
  - records the files in each part;
  - once N passes 16, it stops reading contents and finishes the count from file sizes only (a lower bound).
- **Pass B:** each goroutine builds its own part from its segments. It reads each segment's byte range through a buffered stream reader (open, seek to the start, read at most the range length). No file is read whole (D12). Peak memory is about 4 × 43 KiB per Version.
- **Halving (D6):**
  - splits a part's segment list at its byte midpoint, adjusted to a newline or rune boundary;
  - re-adds the preamble and continuation headers;
  - the two halves are labelled `<label>a` and `<label>b`.
- **Temp dir:**
  - `runAgentScan` no longer deletes the extracted dir on success;
  - `agentContent` gains `dir`, `entryRel` and `release`;
  - the controller defers `release()` right after `runAgentScan` succeeds.

### 2. Aggregation and gate (D4, D5)
- **Worst values across Scored parts:**
  - RiskLevel: the worst, ranked Minimal < Low < Moderate < High < Critical;
  - RiskScore and the six risk probabilities: the maximum;
  - SafeProbability and RiskConfidence: the minimum.
- **Unknown risk label:** the part is marked `Failed` (a service error).
- **`Model`:** taken from the worst part (highest level, then score, then lowest label).
- **Gate:** one reason per breached threshold, naming the part with the worst value (ties go to the lowest label). For example: `safe probability 0.31 (chunk 4) is below minSafeProbability 0.50`.
  - Critical: `risk level Critical (chunk 2) is always blocked`.
  - D5: when `MinConfidence` is set and any part is below it, the reason is `risk confidence 0.41 (chunk 3) is below minConfidence 0.70`, and `NeedsReview=true`. A value exactly equal to the threshold passes, because the test is strictly below.
- **`AssessedChunks`:** the count of Scored entries.

### 3. Fail-closed outcomes (D2, D3, D6, D8, D10)
Every outcome below sets `Blocked=true` with `BlockReasons`, so both the server and the CLI gates see it.

| Outcome | Cause type | `Error` | runAgentJev returns | syncStatus | Branch phase |
|---|---|---|---|---|---|
| N > 16 (no calls made) | Content | empty | no error | `JevBlocked:` | Rejected |
| Unreadable content (D8; no calls made; parts recorded as `NotSent`) | Content | empty | no error | `JevBlocked:` | Rejected |
| A part is `TokenLimitExceeded` and no part is `Failed` | Content | empty | no error | `JevBlocked:` | Rejected |
| Any part `Failed`, or the token Secret can't be read | Service | summary naming the part(s) | error | `JevUnavailable:` | Held |
| Jev disabled on the controller while the source sets `jevSecretRef` (D10; no calls made) | Service | `jevDisabledError` | error | `JevUnavailable:` | Held |
| All parts Scored and the gate trips | Policy | empty | no error | `JevBlocked:` | Rejected |

- Only the Service outcome for a failed assessment is retried automatically (D11, Design §5). The D10 row is not a retry: it is re-evaluated on every reconcile and makes no Jev call.
- A failed assessment still keeps its combined scores over the Scored parts, plus the partial-gate reasons.
- Example reason texts:
  - "jev assessment incomplete: the content needs at least 23 parts of up to 44032 bytes, above the limit of 16 per Version; no Jev calls were made"
  - "jev assessment incomplete: 1 file withheld for a secret, 2 binary files (assets/logo.png, …); Jev cannot read them, so the Version is blocked"
  - "chunk 3ab (10,496 bytes) was still over Jev's token limit after 2 halvings"
  - "chunk 5 had no call budget left under the 16-call limit"
- The ceiling check runs first. If both the ceiling and D8 apply, both reasons are listed, as far as pass A knows them.
- `jevPublishedStatus` is only reachable now for legacy assessments.

### 4. Concurrency and bounds (D1, D2, D6)
- **errgroup:**
  - one `errgroup.Group` per Version with `SetLimit(16)`, so up to 16 parts run at once for that Version (Q10). The controller-wide semaphore still caps all Jev requests at 16;
  - goroutines always return nil and write their own pre-sized result slot;
  - the output is sorted by label, so it doesn't depend on completion order.
- **Semaphore and rate limiter:**
  - every HTTP attempt, including pkg/jev's internal 429 and 5xx retries, acquires `r.jevSem` (size 16) through the attempt gate and releases it before any backoff sleep;
  - every HTTP attempt also waits on the shared rate limiter `r.jevLimiter` (1,000 requests per minute, burst 16);
  - both are controller-wide and shared with the bump path;
  - waits use `select` on the context (`Limiter.Wait(ctx)` for the rate limiter), so cancellation can't leak a slot;
  - a nil semaphore or nil limiter means no global cap, for unit tests.
- **Deadline:**
  - 5 minutes per Version, covering semaphore waits and calls;
  - a part that never started gets `NotSent` with "deadline exceeded before the part was sent";
  - a part cut off in flight gets `Failed`.
- **Call budget:**
  - the N initial calls are reserved when pass A finishes;
  - a halving atomically reserves 2 more;
  - if fewer than 2 remain, the part gets `TokenLimitExceeded` with a "no call budget left" reason;
  - total calls are at most 16, so status entries are at most 16.
  - Which part loses the budget race can vary between runs, but the Version outcome (blocked) can't.
- **Splitting trigger:** only a 400 whose message contains `max_tokens_exceeded` triggers a halving (step 2 helper). Any other 400 marks the part `Failed`.
- **Logs:** V(5) logs per part: label, bytes, duration, status and level. Never the state, and never the token.
- **Expr:** unchanged from the base plan. `content.scripts` is removed, and scripts are streamed in 1 MiB segments with a 64 KiB overlap.

### 5. Cache and retries (D7, D10, D11)
**Guarantee.** Jev is called only from `runAgentJev` and `classifyPreparedBump`, and only on the slow path. `runAgentJev` checks the disabled case and then the cache **before** pass A:
- **Disabled (D10):** if Jev is disabled on the controller, `runAgentJev` returns `jevFailure(jevDisabledError)` as gated, whatever the thresholds: the assessment has `Blocked=true` and an error is returned. No call is made, and `retryCount` is not changed.
- **Retry budget (D11):** `jevMaxRetries` is a controller flag (default 3), set from the Helm value `scanning.jev.maxRetries`.
  - The first attempt is attempt 0, with `RetryCount = 0`.
  - Each failed attempt (Service outcome) adds 1, so `RetryCount` is 1 after the first failure. A 401 or 403 adds nothing.
  - An automatic re-run is allowed only while `cached.RetryCount <= jevMaxRetries`, so with the default 3 there are 4 attempts (0 to 3).
  - A ForceSync resets `RetryCount` to 0 and runs attempt 0, so it always runs, even at the limit.
  - The first real assessment after a `jevDisabledError` is attempt 0 and does not count.
  - A successful assessment keeps the value. Only ForceSync resets it (Q5).
- If `cached != nil && cached.Error != jevDisabledError` and no re-run is allowed, the cached assessment is reused. There is no digest and no scheme check. A re-run is not allowed when:
  - ForceSync is unset and the cached assessment is not a failed new-scheme assessment; or
  - `cached.RetryCount` is above `jevMaxRetries`, for automatic re-runs only. ForceSync resets the counter, as above.
- **Legacy** (`ChunkCount == 0 && len(Chunks) == 0`): today's rule applies exactly when it is reused. Return an error only if `cached.Error != ""` and `hasJevThresholds`. Otherwise reuse it, so it can still publish. D3, D5 and D8 are never applied to legacy assessments that are reused. Legacy assessments are not retried automatically; ForceSync still re-runs them and resets the counter.
- **New, reused:** if `Error != ""`, return the cached assessment plus an error (it stays `JevUnavailable`). Otherwise return it, and the controller acts on `Blocked`.
- **New, failed (Service outcome), `RetryCount <= jevMaxRetries`:** the reconcile returns `RequeueAfter: 15s` (Jev has API limits), and the re-run happens on that next reconcile.

Why nothing else re-runs:
- New assessments always have `ChunkCount ≥ 1`, because pass A runs before every new failure point that records an assessment.
- `hasJevThresholds` is unchanged: `minConfidence` still isn't counted. Changing it would change legacy outcomes.
- Controller restarts are filtered by `skipResyncedCreate`, and synced Versions take the storage fast path. Even when the fast path is bypassed (missing signature or definition), the cache returns before any call.
- **Consequence:** without ForceSync, a failed assessment (other than 401 or 403) runs at most `jevMaxRetries + 1` times (4 by default, attempts 0 to 3). After that, the Version stays unpublished until a ForceSync resets the counter. A 401 or 403 keeps re-running every 15 seconds until the token Secret is fixed, since those responses don't count toward the limit.
- **Unchanged exceptions:**
  - a `jevDisabledError` assessment re-runs once Jev is enabled, because no Jev call was ever made;
  - a Version with no assessment gets its first one (confirmed acceptable under D7).

### 6. Bump path (D12)
`buildBumpState` still makes one call and keeps the 43 KiB state budget, now with a truncation marker when the budget is hit (Q11). Changes:
- The `ClassifyBump` call runs inside the shared `jevSem` and waits on the shared `jevLimiter`, so the 16-request in-flight cap and the 1,000-per-minute rate limit hold across both paths.
- **Streamed reads (D12):** `buildBumpState` no longer calls `os.ReadFile`. It reads each file through a buffered stream reader with a fixed-size buffer:
  - it checks whether the previous and new files are equal by streaming both side by side, without holding either whole;
  - it detects binary content while streaming: a NUL byte, or invalid UTF-8 (Q11). A rune split across the read buffer is not invalid;
  - the text it passes to the diff is read through a bounded reader, capped at the state budget left, so memory stays bounded by the 43 KiB state and not by file size;
  - once the 43 KiB state is full, it stops reading and appends a truncation marker to the state text (Q11).

## Implementation Steps

### Step 1: [GENERATED CODE] JevAssessment fields, deepcopy and the six CRDs (single lane, runs first)
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
  - Add the fields, the two types and the doc-comment changes from API / CRD Changes.
  - **Deepcopy:** run `cd api/v1alpha1 && make generate`.
    - Service `make generate` uses `paths="./..."` and does not cover `api/v1alpha1`. The base plan's instruction was wrong here.
  - **CRDs:** run the per-service regenerate command, `cd services/<svc> && make manifests`, for `version`, `agent`, `depot`, `module` and `provider`.
    - Each run writes `config/crd/bases` and copies into `chart/opendepot/crds/`.
    - If controller-gen versions give different bytes, copy `services/version/config/crd/bases/opendepot.defdev.io_versions.yaml` over the other five.
    - Revert any unrelated diffs, for example `config/rbac/role.yaml`.
  - No change to `Chart.yaml` version or appVersion, and no image tags.
- **Acceptance:**
  - `shasum -a 256` gives the same hash for all six copies.
  - `helm lint chart/opendepot` passes.
  - `cd test/chart && GOWORK=off go test ./... -count=1` passes.
  - `go build ./... && go vet ./...` passes in `api/v1alpha1` and in every service.

### Step 2: Token-limit helper in pkg/jev
- **Depends on:** none
- **Files:** `pkg/jev/client.go`, `pkg/jev/client_test.go`
- **Changes:**
  - The `MaxStateBytes` comment says it is a per-part budget.
  - Add an exported helper that recognizes an `*APIError` with status 400 whose redacted message contains `max_tokens_exceeded`.
  - Add an exported attempt gate option to the client: a function that acquires a `jevSem` slot and a rate-limit token and returns a release function. The client calls it before each HTTP attempt, including the existing 429 and 5xx retries, and calls release before any backoff sleep. The retry count stays at 3.
- **Acceptance (tests):**
  - a 400 token-limit error matches;
  - a 400 with another message, a 429, a 500 and a wrapped non-API error don't;
  - the token never appears in the message;
  - each HTTP attempt, including a 429 retry, calls the gate once, and release runs before the backoff sleep.

### Step 3: Streamed Expr evaluation (unchanged from base)
- **Depends on:** none
- **Files:** `services/version/checks/agents/agents.go`, `services/version/checks/agents/agents_test.go`
- **Changes and acceptance:** as in the base plan:
  - segment evaluation with one finding per fired rule;
  - the union of domains across segments;
  - the comment update;
  - tests for the 2nd-segment remote-exec rule, the end-of-file unpinned install, deduplication, the domain union and the mid-line URL cut.

### Step 4: Part planner, emitter, halving and pass A classification
- **Depends on:** none
- **Files:**
  - `services/version/internal/controller/agent_jev_chunks.go` (new)
  - `services/version/internal/controller/agent_jev_chunks_test.go` (new)
- **Changes:**
  - Implement Design §1, the ceiling count and the D8 classification.
  - The input struct is local to this file: dir, entry rel path, entry flagged, flagged base names, sorted files, kind, name, description, tools.
  - Outputs:
    - the layout;
    - N (exact up to 16, a lower bound above);
    - the files per part (exact count; at most 32 listed, each cut to 256 bytes);
    - the skipped files (Withheld or Binary, exact count, at most 32 listed);
    - a builder for one part;
    - a halving function.
  - Constants: `jevMaxChunks = 16`, a 2,048-byte preamble reserve, `jevMaxHalvings = 2`, and 1 MiB Expr segments.
- **Acceptance:** the Chunking, D8 and D9 cases in the Test Plan.

### Step 5: Aggregation and per-part gate
- **Depends on:** none
- **Files:**
  - `services/version/internal/controller/agent_jev_aggregate.go` (new)
  - `services/version/internal/controller/agent_jev_aggregate_test.go` (new)
- **Changes:**
  - Implement Design §2 over an internal per-part result type that pairs a `jev.Result` with a label.
  - Add a new gate function that returns reasons naming parts, plus `needsReview`.
  - It replaces `evaluateJevGate`, which step 6 removes.
- **Acceptance:** the D4 and D5 cases in the Test Plan.

### Step 13: [SHARED FILE] Make `golang.org/x/sync` and `golang.org/x/time` direct dependencies (runs before step 6)
- **Depends on:** none
- **Files:** `services/version/go.mod`, `services/version/go.sum`
- **Changes:**
  - Run `cd services/version && go get golang.org/x/sync@v0.23.0 golang.org/x/time@v0.14.0`. Both versions are already present as indirect. `golang.org/x/time/rate` provides the shared rate limiter.
  - Only those two lines move from indirect to direct. No other version changes.
- **Acceptance:** Folded into Step 6 (approved by the user). The `git diff` touches only those two requirements, and `go build ./...` passes.

### Step 6: Rewire content loading, runAgentJev and the controller semaphore
- **Depends on:** 1, 2, 3, 4, 5, 13
- **Files:**
  - `services/version/internal/controller/agent_scan.go`
  - `services/version/internal/controller/agent_jev.go`
  - `services/version/internal/controller/agent_jev_test.go`
  - `services/version/internal/controller/agent_scan_test.go` (new)
  - `services/version/internal/controller/opendepot_versions_controller.go`
  - `services/version/cmd/main.go`
  - `pkg/agentspec/agentspec.go` (only if the entry-file reader change needs it)
  - `chart/opendepot/templates/version-deployment.yaml`
  - `chart/opendepot/values.yaml` (the `scanning.jev.maxRetries` key only; the comment text is step 9)
- **Before starting:**
  - Search `services/version` for `fileBodies`, `omittedFiles`, `readBundledPrefix`, `buildJevState`, `writeJevBundledBodies`, `jevTruncatedError`, `jevFileListHeader`, `evaluateJevGate` and `.scripts`.
  - Add any other matching test file to this step, unless step 7 or 8 owns it.
- **`agent_scan.go`:**
  - remove bodies, the omitted set, scripts and the prefix reader;
  - `loadAgentContent` keeps its signature and sets `dir` and `entryRel`;
  - the entry file is read through a buffered stream reader, not `os.ReadFile` (Q12). If `pkg/agentspec` must accept a reader, change it in this step;
  - `runAgentScan` sets `release` and releases on its own error paths;
  - Expr uses step 3 over streamed segments, read through a buffered stream reader; the script `os.ReadFile` is removed (D12).
- **`agent_jev.go`:**
  - `runAgentJev` order:
    1. the ref and required checks (unchanged);
    2. the disabled check, which now always fails closed (D10, Design §5);
    3. the cache and retry-budget check (Design §5, D11);
    4. pass A;
    5. the ceiling check;
    6. the D8 check;
    7. the token read;
    8. the concurrent send with halving (Design §4);
    9. the outcome table (Design §3);
    10. aggregation and the gate (step 5).
  - Fill all the new fields. `RetryCount` is 0 on a ForceSync or after a `jevDisabledError`, otherwise carried forward, and gets plus 1 on each failed attempt.
  - Add the controller flag `--jev-max-retries` (default 3) in `services/version/cmd/main.go`, pass it to the controller as `jevMaxRetries`, and use it for the limit. Wire it from `scanning.jev.maxRetries` in `chart/opendepot/templates/version-deployment.yaml`, with default `3` in `values.yaml`.
  - Remove the dead helpers.
  - `jevFailure` and `hasJevThresholds` stay for the legacy path. The disabled path calls `jevFailure` as gated.
  - Add the semaphore-acquire and rate-limit-wait helper, which is nil-safe.
- **`opendepot_versions_controller.go`:**
  - add the `jevSem chan struct{}` field, created with size `jevMaxInFlight` (16) in `SetupWithManager` next to `scanSem`;
  - add the `jevLimiter *rate.Limiter` field, created in `SetupWithManager` at `jevRateLimitPerMinute` (1,000 per minute, burst 16);
  - pass the attempt gate from step 2 into the pkg/jev client, so every HTTP attempt (including 429 and 5xx retries) takes a `jevSem` slot and a limiter token;
  - defer `content.release()` right after the `runAgentScan` error check;
  - no other logic changes.
- **Acceptance:** the Concurrency, D3, D6, D7, D8, D10 and D11 unit cases in the Test Plan, plus:
  - the existing token-sentinel tests;
  - `agent_scan_test.go`: the temp dir exists after success and is gone after `release()` and after an error;
  - a script over 2 MiB fires the remote-exec rule past the first MiB.

### Step 7: Controller-path tests
- **Depends on:** 6
- **Files:** `services/version/internal/controller/agent_versions_test.go`
- **Changes:** use the existing reconcile harness (stubbed Trivy, Jev via `httptest`) to cover the reconcile cases in the Test Plan.
- **Acceptance:** `make test` in `services/version` passes.

### Step 8: Bump path, shared semaphore and rate limiter, streamed reads and single-call pin
- **Depends on:** 6
- **Files:**
  - `services/version/internal/controller/branch_bump.go`
  - `services/version/internal/controller/branch_bump_test.go`
- **Changes:**
  - Wrap the `ClassifyBump` call in the step 6 semaphore-acquire and rate-limit-wait helper.
  - Rewrite the file reads in `buildBumpState` and `unifiedFileDiff` to stream through a fixed-size buffer, per Design §6 (D12). Remove the `os.ReadFile` calls.
  - Keep the single call and the 43 KiB state.
- **Acceptance:**
  - a tree that needs 3 assessment parts still makes exactly 1 `ClassifyBump` call, with a state ≤ `jev.MaxStateBytes`;
  - when the semaphore is full, the bump waits and respects context cancellation;
  - the bump waits on the shared rate limiter;
  - a bump over files much larger than 43 KiB (for example 50 MiB) keeps memory bounded and still gives a state ≤ `jev.MaxStateBytes`;
  - equal large files are detected as unchanged, and a file that differs only past the read window is still listed as modified;
  - the existing bump tests pass with the same states for small inputs.

### Step 14: UI types and tree builder
- **Depends on:** 1 (field contract)
- **Files:**
  - `services/ui/src/lib/api.ts`
  - `services/ui/src/lib/jevChunkTree.ts` (new)
  - `services/ui/src/lib/jevChunkTree.test.ts` (new)
  - `services/ui/src/lib/jevSeverity.ts`
  - `services/ui/src/lib/jevSeverity.test.ts`
- **Changes:**
  - **`api.ts`:**
    - `JevChunkStatus` and `JevChunkResult`, with `index`, `label`, `files?`, `fileCount`, `status`, `riskLevel?`, `riskScore?`, `riskConfidence?`, `safeProbability?` and `error?`;
    - `JevSkippedFile`;
    - the new optional `JevAssessment` fields, including `retryCount`.
  - **`jevChunkTree.ts`:** a pure builder that turns `chunks` and `skippedFiles` into a directory tree.
    - Each file leaf carries the labels of the parts that contain it, and its worst values: worst level, max score, min safe, min confidence. Status rank is Failed > TokenLimitExceeded > NotSent > Scored.
    - Directories roll up their children.
    - Skipped files become leaves that carry their reason.
    - It reports an "unlisted files" count from `fileCount − files.length`.
  - **`jevSeverity.ts`:** move `RISK_COLORS` here from `JevPanel.tsx`, so the panel and the tree share it.
- **Acceptance:** the UI builder cases in the Test Plan.

### Step 15: UI tree component and JevPanel
- **Depends on:** 14
- **Files:**
  - `services/ui/src/components/JevChunkTree.tsx` (new)
  - `services/ui/src/components/JevChunkTree.test.tsx` (new)
  - `services/ui/src/components/JevPanel.tsx`
  - `services/ui/src/components/JevPanel.test.tsx`
- **Changes:**
  - **`JevChunkTree`:** two views, switched with a `ToggleButtonGroup`.
    - **Files:** an expandable directory tree. Each file shows a risk chip and its part labels.
    - **Chunks:** one row per part with label, status chip, level, score, safe and confidence. Each row expands to its files, with "+N more" when the list was cut. A `Failed` or `TokenLimitExceeded` row shows its `error` text in red, which satisfies D6.
    - Built only from MUI `List`, `Collapse` and `Chip`, with no new npm dependencies.
    - Keyboard-operable, with `aria-expanded`.
    - Renders nothing for legacy assessments, where `chunks`, `skippedFiles` and `chunkCount` are all absent.
  - **`JevPanel`:**
    - render the tree below the bars;
    - when `chunkCount > 16` and `chunks` is empty, show the alert "Content needs at least N parts; the limit is 16; no Jev calls were made";
    - the blocked alert text becomes "Blocked by Jev", with reasons;
    - the confidence chip and "Needs review" tooltips say low confidence blocks when the policy sets `minConfidence`;
    - import `RISK_COLORS` from `jevSeverity.ts`.
- **Acceptance:** the UI component cases in the Test Plan, and the existing `JevPanel` tests updated.

### Step 9: Docs and chart comments (D13)
- **Depends on:** 1, 6, 8, 15
- **Files:** `docs/guides/agents.md`, `docs/reference/api.md`, `chart/opendepot/values.yaml`
- **`docs/guides/agents.md`**, with these **D3, D5, D8, D10 and D11 conflicts to fix**:
  - **Jev intro (around line 157):** "Jev output is advisory" becomes "Jev can block a Version; it never bypasses signature or hash checks."
  - **Thresholds table (`minConfidence` row) and the "Confidence never blocks" rule:** rewrite per D5.
  - **"Fail closed only with thresholds" (around line 236):** replace with "Fail closed whenever Jev is configured" (D3). Remove the "Leaving all thresholds unset is the opt-out" sentence and the truncated or oversized wording. Add the D8 rule, the 16-part ceiling, and the token-limit outcome. Add that a source with `jevSecretRef` on a controller with Jev disabled is not published (D10).
  - **Cache sentence:** a failed assessment is retried automatically 15 seconds later, until `retryCount` exceeds `jevMaxRetries`. ForceSync resets the counter and re-runs the assessment. Assessments from before this release keep their result (D7, D11).
  - **New subsection "How content is split":**
    - the order, the file list in part 1 only, the continuation markers;
    - the 16-call ceiling, including the token-limit halving;
    - 16 concurrent requests and the 1,000-per-minute rate limit, controller-wide;
    - the aggregation table;
    - cost: up to 16 calls per Version per run, and up to 3 retries; per commit in branch mode.
  - **Data handling (around line 276):**
    - "a single request" becomes "one request per part, at most 16 per Version";
    - rewrite the Sent bullet: all text content is sent, and nothing is cut for size;
    - Binary and Withheld now block the Version.
  - **Browse view:** describe the Files and Chunks tree.
  - **"Yanked and blocked Versions":** "a Version that Jev blocks", listing all the causes.
  - The bump section keeps the 43 KiB single call, and its `minConfidence` is the branch policy's, which still never blocks. Add that bump calls share the in-flight cap and the rate limit.
- **`docs/reference/api.md`:** add the new fields (including `retryCount`) and the two types, and update the `minConfidence` description. First confirm the `JevAssessment` section exists.
- **`chart/opendepot/values.yaml` (comment text only; no value, key or version change):**
  - `scanning.jev.enabled`: replace "is advisory unless thresholds are configured" with text that says Jev can block a Version whenever it is configured, and that a source setting `jevSecretRef` is not published while this is `false` (D10).
- **Acceptance:**
  - `mkdocs build --strict` passes;
  - `helm lint chart/opendepot` passes;
  - `cd test/chart && GOWORK=off go test ./... -count=1` passes.

### Step 10: Agent e2e assertions
- **Depends on:** 7
- **Files:** `services/agent/test/e2e/e2e_test.go`
- **Changes:**
  - In the Skill and Agent Jev specs, assert:
    - `chunkCount ≥ 1`;
    - `assessedChunks == len(chunks)`;
    - every chunk has `status == Scored` and a `label` matching the pattern;
    - `skippedFiles` is empty;
    - `error` is empty.
  - For the Agent fixture (`.github/agents` at `0.9.0`): measure the extracted size first. If it is over 41,984 bytes, also assert `chunkCount ≥ 2`.
  - **Pre-check:** confirm neither fixture contains a binary or a Trivy-flagged file. D8 would now block it. Also confirm no agent e2e spec sets `jevSecretRef` while Jev is disabled on the controller; D10 would now block it. If either happens, report back; don't change fixtures.
  - Risk: under D3, a live Jev outage now fails these specs instead of publishing.
  - Assert `retryCount` is absent or 0 on a first successful assessment.
- **Acceptance:** the focused agent e2e passes.

### Step 11: Security Review (gate after implementation; no files)
- **Depends on:** 6, 8, 15
- **Files:** none
- **Checks:**
  - **Attacker-controlled splitting:** padding to the ceiling, token-dense text, and fake `--- path ---` or `[continues in part …]` markers inside content.
  - **Every fail-closed outcome:** sets `Blocked`, can't publish, and is refused by the server (`agentVersionServable`) and the CLI.
  - **D8 evasion:** files withheld by base-name match, binaries, invalid UTF-8.
  - **Legacy:** legacy assessments can't be downgraded or upgraded by the new code.
  - **Token:** absent from states, logs, status and the UI.
  - **Concurrency:** races (`-race`), semaphore leaks on cancel, the call budget held under parallel halvings, and the rate limiter shared by both paths.
  - **Retry limit (D11):** automatic re-runs stop once `retryCount` exceeds `jevMaxRetries` (401 and 403 aside), ForceSync is the only reset, and an automatic re-run that replaces an assessment can't reset it.
  - **Jev disabled (D10):** a source with `jevSecretRef` can't publish while Jev is disabled.
  - **Memory (D12):** no whole-file reads remain in the Jev, Expr or bump paths.
  - **Status size:** bounds hold and validation never rejects a status write.
  - **Paths and errors from archives rendered in the UI:** React text only, no `dangerouslySetInnerHTML`.
  - **Exposure:** file paths of blocked Versions are visible to anyone who can see the parent (an accepted browse design).
  - **Temp dir:** cleaned up.
  - **Expr:** the overlap's residual risk.
- **Acceptance:** written sign-off, or findings fixed in steps 4, 6, 8 or 15.

### Step 12: Focused validation (no files)
- **Depends on:** 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15
- **Files:** none
- **Acceptance:** every check in E2E Test Changes passes.

**Waves:**

| Wave | Steps | Note |
|---|---|---|
| A | 1, 2, 3, 4, 5, 13 | Steps 1 and 13 are the shared-file steps. Each runs alone in its own lane. |
| B | 6, 14 | |
| C | 7, 8, 15 | |
| D | 9, 10, 11 | Step 10 depends on step 7, which is in wave C. |
| E | 12 | |

No two independent steps share a file. Step 6 now owns `opendepot_versions_controller.go`, which the base plan gave to step 7, so it can add the semaphore field it compiles against.

## Test Plan (edge cases)

**Chunking and ceiling (D2, D9), in step 4 unit tests:**
- **Part count at the edges:**
  - an entry-only Skill gives 1 part;
  - a body of exactly the capacity gives 1 part, and capacity + 1 gives 2;
  - exactly 16 parts is allowed;
  - 17 parts is blocked with 0 HTTP requests, `chunkCount ≥ 17`, no `chunks`, and the reason text.
- **Large inputs:**
  - a 5 MiB text tree stops recording the layout after 16, and its memory stays bounded;
  - 10,000 one-byte files make the file list span parts, and that counts toward the ceiling.
- **Cuts:**
  - a rune straddling a cut;
  - a newline inside and outside the final 1 KiB window;
  - a single line with no newline gets a rune cut.
- **Large files:**
  - a 100 KiB file splits across 3 parts with correct byte ranges, and the parts reassemble exactly;
  - a large entry file is split too;
  - a file that fits an empty part is never split.
- **Every part fits:** at index 16, with a maximum-length description and tools, every part is ≤ 44,032 bytes.
- **File list (D9):** only in part 1; parts 2 to N carry the pointer back to part 1.
- **Status bounds:** more than 32 files in a part gives `files` cut to 32 with an exact `fileCount`; a 300-byte path is cut to 256 bytes at a rune boundary.

**Token-limit halving (D6), in step 6:**
- a 400 token-limit error on part 3 gives `3a` and `3b`, both Scored, in 3 calls;
- a second rejection gives `3aa` and `3ab`;
- a depth-2 rejection gives `TokenLimitExceeded`, the reason text, `JevBlocked:` and Rejected for a branch Version;
- N = 16 with one token-limit part means no budget is left, so `TokenLimitExceeded` with exactly 16 calls;
- N = 15 with two token-limit parts:
  - total calls ≤ 16, and the outcome is blocked;
  - run 20 times under `-race`;
- a 400 with another message gives `Failed` with no halving.

**Concurrency (D1), in step 6:**
- 16 parts against a test server that counts concurrent requests: at most 16 in flight;
- two Versions in parallel on one reconciler: at most 16 in flight in total;
- one part gets 503 after its retries:
  - the other parts still finish and are recorded;
  - `assessedChunks = N − 1`;
  - `JevUnavailable:`;
- a shortened deadline (a test hook) leaves the remaining parts `NotSent` or `Failed`, and the semaphore length is 0 afterwards;
- canceling the context while waiting on the semaphore returns cleanly;
- the order is the same across random completion orders.

**Aggregation (D4) and confidence (D5), in step 5:**
- minimum safe, maximum score and probabilities, worst level;
- Critical in the first, middle or last part blocks with no policy;
- an unknown label marks the part `Failed`;
- a single part gives the same result as today's call;
- reasons name the worst part, and ties go to the lowest label;
- `minConfidence`:
  - unset: low confidence doesn't block;
  - set: one part below it blocks and sets `needsReview`;
  - equal to the threshold: doesn't block.

**Fail closed (D3), in steps 6 and 7:**
- no thresholds with a 503: unsynced, `JevUnavailable:`, `Blocked=true`, `Error` names the part;
- a 401 or 403 blocks and is re-run every 15 seconds without changing `retryCount`;
- a missing token Secret blocks without thresholds;
- a malformed response blocks;
- a branch Version is Held on a service failure and Rejected on a content failure;
- ForceSync re-runs it as attempt 0, and `retryCount` resets to 0.

**Jev disabled (D10), in steps 6 and 7:**
- a source with `jevSecretRef` and no thresholds, on a controller with Jev disabled: unsynced, `JevUnavailable:`, `Blocked=true`, `Error` is `jevDisabledError`, 0 calls;
- the same with thresholds: same outcome;
- a branch Version is Held;
- reconciling again makes 0 calls and doesn't change `retryCount`;
- once Jev is enabled, the assessment runs (unchanged) as attempt 0: a failure sets `retryCount` to 1, not 2.

**Retry limit (D11), in steps 6 and 7:**
- a new-scheme assessment that failed with a 503 is re-run after `RequeueAfter` 15s, and `retryCount` goes from 0 to 1;
- after 4 failed attempts (attempts 0, 1, 2 and 3), `retryCount` is 4, a further reconcile makes 0 calls, and the Version stays unsynced;
- with `retryCount` at 4, ForceSync runs once as attempt 0 and resets `retryCount` to 0;
- ForceSync on a Scored assessment re-runs it and resets `retryCount` to 0;
- automatic retries stop at 4 failed attempts, and only a ForceSync starts a new count;
- a 401 or 403 leaves `retryCount` unchanged and is re-run every 15 seconds until it succeeds or a ForceSync runs;
- a retry that succeeds publishes the Version;
- content and policy outcomes (ceiling, D8, token limit, gate) are not retried automatically;
- `retryCount` is carried forward when an automatic re-run replaces the assessment.

**Unreadable content (D8), in steps 4, 6 and 7:**
- a bundled PNG (NUL bytes) is blocked before any call:
  - `skippedFiles` has the file with reason `Binary`;
  - the parts are `NotSent`;
- invalid UTF-8 late in a file is detected;
- a valid rune split across the read buffer is not binary;
- a Trivy-flagged bundled file is blocked (`Withheld`);
- a flagged entry file is blocked;
- a 0-byte file is not skipped;
- more than 32 skipped files gives a list of 32 with an exact `skippedFileCount`.

**No re-assessment (D7), in steps 6 and 7:**
- a legacy cached assessment with the old truncated-error text and no thresholds, on a reconcile that misses the fast path: 0 calls, and it still publishes;
- a legacy blocked assessment stays blocked with 0 calls;
- a legacy assessment with only `minConfidence` set and low confidence is not blocked;
- reconciling a synced Version again (as after a restart) makes 0 calls;
- ForceSync re-runs with parts and resets `retryCount`;
- a legacy errored assessment is not retried automatically;
- a cached `jevDisabledError` re-runs when Jev is enabled (unchanged).

**Bump, in step 8:** one call; it shares the semaphore and the rate limiter; files are streamed, and memory stays bounded for large files.

**Rate limit (D1), in step 6:** calls from many parts and Versions wait on the shared limiter; canceling the context while waiting returns cleanly.

**UI, in steps 14 and 15:**
- nested directories;
- a file in several parts takes the worst values;
- directories roll up their children;
- skipped files show their reason;
- "+N more";
- labels `3a` and `3b`;
- a legacy assessment renders no tree;
- the over-ceiling alert;
- `TokenLimitExceeded` and `Failed` rows show the error text;
- switching views;
- keyboard expand;
- the blocked alert and tooltip text.

## E2E Test Changes
- **Tier 1:** `go build ./... && go vet ./...` in:
  - `api/v1alpha1`, `pkg/jev`;
  - `services/version`, `agent`, `depot`, `module`, `provider`, `server`;
  - `cli` (build only, to confirm compatibility; no edits).
- **Tier 2:**
  - `cd pkg/jev && go test ./... -count=1`
  - `cd services/version && go test ./checks/agents/... -count=1 && make test`
  - Also run the race detector on the Jev tests, with the same `KUBEBUILDER_ASSETS` that `make test` sets up:

    ```bash
    go test -race ./internal/controller/ -run 'Jev|Chunk|Aggregate|Bump' -count=1
    ```

  - `cd services/server && go test ./... -count=1`
- **UI:**
  - `cd services/ui && yarn vitest run src/lib/jevChunkTree.test.ts src/lib/jevSeverity.test.ts src/components/JevChunkTree.test.tsx src/components/JevPanel.test.tsx`
  - `yarn lint`
  - If any tool rewrites `tsconfig.tsbuildinfo`, restore it with `git checkout -- services/ui/tsconfig.tsbuildinfo`.
- **Chart:**
  - `helm lint chart/opendepot`
  - `cd test/chart && GOWORK=off go test ./... -count=1`
- **Tier 3 (focused only):**
  - `cd services/agent && make test-e2e E2E_FOCUS="Skill|Agent" KEEP_KIND=1 KIND_CLUSTER=opendepot-focus-agent`, with `OPENDEPOT_JEV_API_KEY` set. Record the per-part latency from the V(5) logs.
  - `services/version`: run the narrowest agent or Jev Context. Find it with:

    ```bash
    grep -n -E '(Describe|Context)\("' services/version/test/e2e/e2e_test.go
    ```

    If none exists, tiers 1 and 2 cover it.
  - depot, module, provider and server: tiers 1 and 2 only.
- **Docs:** `mkdocs build --strict`.
- **Not run:**
  - full `make test-e2e`;
  - Tilt or a live cluster patch;
  - Trivy runs or the Trivy seed job;
  - Playwright.

## Helm Chart
- The regenerated `chart/opendepot/crds/opendepot.defdev.io_versions.yaml` (step 1), which now includes `retryCount`.
- `chart/opendepot/values.yaml`: comment text only on `scanning.jev.enabled` (step 9, D13). It no longer calls Jev advisory, and it says a source with `jevSecretRef` is not published while Jev is disabled.
- `chart/opendepot/templates/version-deployment.yaml` passes `--jev-max-retries={{ .Values.scanning.jev.maxRetries }}` (step 6), and `values.yaml` gains `scanning.jev.maxRetries: 3`. No RBAC, `Chart.yaml` version, appVersion or image tag changes.
- The ceiling (16), the in-flight cap (16) and the rate limit (1,000 per minute) are code constants. The retry limit is the `--jev-max-retries` flag, default 3, set from `scanning.jev.maxRetries`.

## Hard Rules
- No chart or app version bumps, and no image tag changes.
- No changes to Makefile targets, CI workflows, Trivy setup or the Trivy seed job.
- Do not touch `cli/`, `test/fixtures/` or `tsconfig.tsbuildinfo`. Do not commit fixtures.
- No Tilt, no live cluster patches, no full `make test-e2e`, and no Trivy runs. Verify with unit tests and focused `E2E_FOCUS` e2e only.
- Step 1 and step 13 each run alone, before the steps that depend on them.
- **No whole-file reads (D12).** New and changed code reads file content through a buffered stream reader with a fixed-size buffer. It does not use `os.ReadFile` or `io.ReadAll` on file content.

## Behavior Changes (to put in the release notes)
1. **(D3)** With Jev configured and no thresholds, a failed assessment used to publish the Version. Now the Version stays unsynced and can't be pulled. This covers network, auth, 5xx, timeout, token limit and Secret read errors.
2. **(D7, D11)** A failed assessment is retried automatically 15 seconds later, until 4 attempts have failed (attempts 0 to 3; the limit is `jevMaxRetries`, default 3 retries, set by `scanning.jev.maxRetries`). 401 and 403 failures are re-run but don't count toward the limit. The count is shown in `jevAssessment.retryCount`. Once it exceeds the limit, the assessment is not re-run until a ForceSync resets the counter. Before, a failed assessment re-ran only on ForceSync, with no limit.
3. **(D10)** A source that sets `jevSecretRef` on a controller with Jev disabled is no longer published, with or without thresholds. Before, it published unless thresholds were set.
4. **(D8)** Any bundled binary or invalid-UTF-8 file, or any file Trivy flags for a secret, now blocks the Version when Jev is configured. A Skill that bundles an image can't publish with Jev enabled.
5. **(D5)** `minConfidence` now blocks. The docs example `minConfidence: 0.7` becomes a blocking policy.
6. **(D2)** Content that needs more than 16 parts (about 656 KiB of text) is blocked before any Jev call.
7. Every fail-closed assessment sets `blocked: true`. The UI alert reads "Blocked by Jev" for failures too.
8. **Data sent to TypeSafe:** all bundled text is sent, up to 16 requests per Version. It used to be at most 43 KiB.
9. **Cost:** up to 16 Jev calls per Version per run, up to 16 at a time controller-wide, and at most 1,000 Jev calls per minute controller-wide. Without ForceSync, a failing Version runs at most `jevMaxRetries + 1` (4 by default) times in total, except that 401 and 403 failures keep re-running until fixed. In branch mode this applies to every commit.
10. The top-level `riskConfidence` is now the minimum across parts.
11. The agents.v1 download `assessment` and the browse `jevAssessment` JSON gain `chunks`, `skippedFiles`, `retryCount` and the count fields. This is additive.
12. Bump classification calls share the 16-request in-flight cap and the 1,000-per-minute rate limit, and may wait for a free slot.
13. Bump classification streams files instead of reading them whole, so large files no longer raise controller memory.
14. Assessments from before this release are not retried automatically, and they keep their recorded result and their old meaning. ForceSync still re-runs them.

## Known Pre-Existing Issues (carried over, not fixed here)
- **Standalone Skill or Agent CR:** one with `ref` and no Depot does nothing and gives no status. Status: Code Review FOLLOW-UP.
- **Bump classifier input:** the Jev bump classifier reads untrusted diff text (MEDIUM, from an earlier security review). Status: Code Review FOLLOW-UP.
- **Bump-path gap in `branch_bump.go`:**
  - Fixed here (D12, Q11): whole-file reads with `os.ReadFile` and unbounded memory. Step 8 streams the files, adds a truncation marker to the 43 KiB state, and rejects invalid UTF-8 as binary.
- **Base-name matching:** `flaggedFiles` matches by base name, so a flagged `a/config.env` also withholds `b/config.env`. Under D8 this over-blocks; it never under-blocks. Status: FOLLOW-UP.
- **Stale server comment:** the comment on `agentVersionBlocked` in `services/server/agents.go` still says "a configured JevPolicy threshold". The behavior is correct and the comment is left as is.
- The ForceSync retry storm is fixed in this change by the retry limit (D11) and is no longer listed. ForceSync is the only reset, so each ForceSync starts a new count.

## Open Questions
These are still undecided:
- **Burst size:** the limiter's burst is 16, equal to `jevMaxInFlight`. Confirm or change it.
