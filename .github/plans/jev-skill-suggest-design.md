# Design: server-side Jev skill suggestion for OpenDepot CLI

Status: draft for review. Nothing is implemented. No cluster or repo changes.

## Problem

Agents that use OpenDepot must find the right skill or agent. The discovery canaries show the agent often skips the CLI or can't choose among skills from a list:

- v1 (old CLI, no `describe`): CLI found 1/5; no-CLI 0/5.
- v2 (CLI with `list` and `describe`): CLI found 5/5; no-CLI 0/5. CLI was called 4–7 times per trial.
- refined (CLI skill rewritten with a "Find skills and agents" section): found 0/5. Only 2 of 5 trials called the CLI at all.

The refined result is unexplained and must be diagnosed before this design is built. A ranking endpoint does not help if the agent never calls it.

## Jev credential selection

- Each GroupBinding sets `jevSecretRef` (`name`, `key`) for suggest calls. It is scoped to the group, so every member whose token matches the binding uses the same secret. It is not per user.
- Each team creates its own secret in the server namespace and points its GroupBinding at it. Jev tracks usage per API key, as shown in the Jev dashboard's API Key view (input tokens per key). Per-team usage tracking is therefore possible. Budget limits per key are not confirmed. Check whether Jev enforces them before relying on them as a hard cap.

- Suggest calls have no target item yet, so the item's `jevSecretRef` does not apply to them. Each GroupBinding names one Jev secret reference (`namespace`, `name`, `key`), and suggest calls from that binding's callers use it.
- Items keep their own `jevSecretRef`, which the Version controller uses at publish time when it assesses each skill or agent. That credential is separate from the suggest credential.
- The request body carries only the task and kind. It never names a secret.
- Callers with no binding, and servers in anonymous or non-OIDC mode, get no suggest credential. Suggest is disabled for them unless the server sets a default secret.

- The server reads only the named key from the named secret. It must not list or read other secrets. Limit the server's RBAC to `get` on the specific secrets the bindings reference, if the cluster setup allows it.
- Log the secret name and the binding name, never the token.

## Proposal

Add a registry endpoint that ranks the depot's available skills and agents for a task description, using Jev. The CLI exposes it as `opendepot suggest "<task>"`.

Flow:

1. The agent runs `opendepot suggest "<task>" --registry <host>`.
2. The CLI calls `POST /opendepot/agents/v1/suggest` with a JSON body `{"task": "...", "kind": "skill|agent|any"}`. The task text stays out of URLs, which proxies and access logs usually record.
3. The server loads the catalog, drops every item that is not `available` (blocked, rejected, unassessed), and keeps only the ones the gate allows.
4. The server sends a shortlist to Jev. Jev returns a ranked list with a confidence per item.
5. The server returns the top few items with a short reason, plus the existing catalog fields.
6. The agent runs `opendepot describe` on the top item and then `apply` as before.

Jev is called server-side. The token comes from a secret the caller's GroupBinding allows (see Jev credential selection). The CLI never needs `OPENDEPOT_JEV_API_KEY`.

## Ranking method

Modeled on the TypeSafe skill-suggestion cookbook, with two stages:

- Stage 1 (coarse): rank the full catalog by name and short description. Cookbook-style `Choice` plus a `named` answer.
- Stage 2 (rerank): send the top 3 to 5 candidates with the full description and a `SKILL.md` excerpt. Ask a per-candidate `fits` score.

Thresholds are not copied from the cookbook. The cookbook set its thresholds by hand (0.30, 0.50, 0.30) on its own roster.

Known limits: the cookbook's numbers came from one vendor-published run, using Claude Haiku 4.5 as the agent and labels written by Claude Sonnet 5. Its requests are easier than real ones. We have no data for `gpt-6-luna`.

## Safety invariants

- Suggestions are advisory. `apply` still enforces the gate, and `--allow-older-versions` behavior is unchanged.
- The endpoint never returns an item that is blocked, rejected, or unassessed.
- The endpoint sends only catalog metadata and `SKILL.md` excerpts to Jev. No credentials and no full archives.
- The Jev key stays server-side. The CLI must not read it.
- Log the Jev model used (`jev-1.14.0`) and the item IDs returned, not the raw task text.

## Authentication

- In OIDC mode, the endpoint requires `Authorization: Bearer <Dex-issued token>`, the same as the catalog and agent routes. A missing or invalid token gets 401.
- Use the same check as the catalog (`browseAuthCheck`), so the UI client audience and client-credentials tokens are accepted the same way.
- Results are filtered by the caller's GroupBinding visibility. Items the caller cannot see are never ranked, returned, or sent to Jev.
- With `--anonymous-auth` or without OIDC configured, no token is required, matching the other routes.
- Do not add the SA-fallback path to this endpoint.
- The CLI reads the token from its existing registry credentials. It never reads the Jev key.

## Evaluation plan

Use a held-out roster and task set that the ranker has never seen. Do not tune on the canary tasks.

Arms, on the same tasks and roster:

- A: no suggestion. Agent uses `list` and `describe` as today.
- B: `suggest` as designed above.
- C (optional): stage 1 only, to measure the value of the rerank.

Agent for Copilot CLI runs: `gpt-6-luna` only. Use the discovery canary method, interleaved trials, and record session IDs and CLI call counts.

Metrics: correct item in top 1 and top 3; wrong item chosen; agent called the CLI at all; blocked items never suggested (must be 0); latency and Jev cost per suggestion.

Decision rule (proposed, to confirm): B must beat A on top-1 correct, with no increase in wrong-item loads and zero blocked suggestions.

## Open questions

1. Diagnose the refined canary drop first. Does the agent skip the CLI because of the skill text, the prompt, or the permission flags?
2. Should `suggest` be a new endpoint, or a `list` option that returns ranks?
3. Is the POST body acceptable for the task text, or should we use GET with a length limit?
4. Who pays for Jev calls per suggestion, and what is the rate limit?
5. Does `gpt-6-luna` as the agent change the cookbook's conclusions? We have to measure this.

## Not included

- Changes to the Jev risk questions (protocol v5 is separate).
- Any change to the catalog or to `apply`.
- Any cluster change. The demo depot is untouched.

## Canary diagnosis

Status: Step 1 complete. Answers open question 1. Run on 2026-11-26 against `demo.opendepot.defdev.io`, which the user logged into for this run. The shared `opendepot` Kind cluster was not touched.

### Reconstructed original transcripts

The original harness directory, `/tmp/canary-trials`, no longer exists. The transcripts were rebuilt from the local Copilot session store (`~/.copilot/session-state`). All original sessions used `gpt-6-luna`.

| Original batch | Prompt | Skill text in workspace | `opendepot-cli` in system catalog | Result |
|----------------|--------|-------------------------|-----------------------------------|--------|
| v1 (`A-*`, 07:16–07:20) | "Find the one-time canary code word…" | v1: unquoted `description` that fails YAML parsing | No (0 of 5 catalogs) | CLI used in 1 of 5 trials. 1/5 correct. |
| "v2" (`v2/A-*`, 07:33–07:37) | "You need a zeta discovery skill…" | **Refined text.** Loaded body is identical to the refined batch. | Yes | `skill` tool called 5/5. CLI 4–8 calls per trial. 5/5 correct. |
| "refined" (`R-*`, 07:29–07:31) | "Find the one-time canary code word…" | Refined text | Yes | No `skill` calls. No `opendepot` calls. 2 of 5 read `SKILL.md`. 0/5 correct. |

**Correction to the earlier diagnosis:** the "v2" arm did not use a separate skill text. It used the refined text, so the skill text was the same in the "v2" and "refined" batches. The only skill-text difference was in the v1 batch. The v1 `description` (`Use when: installing…`) is unquoted and contains `: `. It fails YAML parsing (checked with a Go YAML parser: "mapping values are not allowed in this context"). Copilot leaves the skill out of the catalog when the frontmatter does not parse.

### Interleaved 2×2 live trials

Design: skill text (`v1` = `docs/guides/opendepot-cli/SKILL.md` at `e37bd37b`, blob `4aa18505`; `refined` = the same file at `99d31e17`, blob `f2350d8d`) × prompt (`Find` and `Zeta`). Three rounds, rotated so each round starts from a different cell (Latin-square order). Each trial ran in a fresh directory `/tmp/w1-canary/trials/<skill>-<prompt>-r<round>` containing `opendepot.hcl` (`registry = "demo.opendepot.defdev.io"`) and `.github/skills/opendepot-cli/SKILL.md`. The zeta-discovery skill was not installed in any trial. The agent had to find it in the registry.

Prompts (exact):

- `Find`: "Find the one-time canary code word for the zeta discovery test. Reply with only the code word on the first line, then one line naming where you found it. If you cannot find it, reply NOT FOUND. Do not ask me questions."
- `Zeta`: "You need a zeta discovery skill to find the one-time canary code word for the zeta discovery test. Find and use an approved skill that provides it. Reply with only the code word on the first line, then one line naming the skill or place you got it from. If you cannot find it, reply NOT FOUND. Do not ask me questions."

Permission flags, identical for every trial: `copilot -p <prompt> --model gpt-6-luna --allow-tool=shell --allow-tool=read --allow-tool=write --no-ask-user -s`. Copilot's bypass mode is disabled by enterprise policy, so `--allow-all-tools` was not used. The original harness's flags are not recorded in its session logs, so these are not claimed to match it.

Pass means the first line is exactly `QUILL-7392-HARBOR`.

| Round | Cell (skill text / prompt) | Session ID | `skill` tool calls | Skill loaded | `opendepot` CLI calls | Failed tool calls | Result |
|-------|----------------------------|------------|--------------------|--------------|-----------------------|-------------------|--------|
| 1 | refined / Find | `961fcef8-47c9-444a-bb69-2f17632b83aa` | 0 | No | 0 | 1 (unrelated GitHub MCP error) | Fail |
| 1 | refined / Zeta | `07f46555-e9e4-4b32-9e55-8955f2a22a17` | 1 | Yes | 7 | 0 | **Pass** |
| 1 | v1 / Find | `f1a68338-caf1-4eb5-9449-a52b910f2156` | 0 | No | 0 | 0 | Fail |
| 1 | v1 / Zeta | `05fbe0ae-2f53-4bda-9ee2-47bfb56ded5a` | 1 (`zeta-discovery`, not found) | No | 0 | 1 (skill not found) | Fail |
| 2 | refined / Find | `3698b631-75df-4c32-8a8b-c337bdd83527` | 0 | No | 0 | 0 | Fail |
| 2 | refined / Zeta | `8cb551ab-6b7e-4951-af8a-9ac69e195d4f` | 1 | Yes | 6 | 0 | **Pass** |
| 2 | v1 / Find | `2be4ce6f-5553-4f18-b48e-f20227e2f66c` | 0 | No | 0 | 0 | Fail |
| 2 | v1 / Zeta | `ff3ff09f-3a55-4cbf-976a-82189234d91b` | 1 | No | 0 | 1 | Fail |
| 3 | refined / Find | `955dbe1c-ba0b-480a-ae7d-7cc9b6d524ca` | 0 | No | 0 | 0 | Fail |
| 3 | refined / Zeta | `983ae950-761c-4822-b585-cfdaef11ccc0` | 1 | Yes | 4 | 0 | **Pass** |
| 3 | v1 / Find | `5234570f-6222-4f15-aca0-d9c331b47f66` | 0 | No | 0 | 0 | Fail |
| 3 | v1 / Zeta | `0b9cc61d-357f-4581-a536-b398e0af723f` | 1 | No | 0 | 1 | Fail |

Totals: refined / Zeta 3/3. refined / Find 0/3. v1 / Find 0/3. v1 / Zeta 0/3.

Notes:

- Copilot CLI 1.0.116 was used for every scored trial. A pilot trial ran earlier on 1.0.113 and is excluded from the table. Its session ID is `79d9ca96-ba6b-4109-b0e0-0a9e74ffe9d8`. It passed with the same counts.
- The `opendepot` CLI was `dev` (local build). Every passing run used the registry's synced `zeta-discovery-canary` v1.0.0.
- In all 6 refined-text sessions, the system catalog lists `opendepot-cli`. In all 6 v1 sessions, it does not.
- The only failed tool calls were the skill-not-found error (v1 / Zeta, round 1, verified) and an unrelated GitHub MCP error (refined / Find, round 1). No permission denials occurred.

### Root cause

Two gates, both required. The prompt sets the first gate, and the skill text sets the second.

1. **Prompt gate (`Find` fails).** The refined skill is in the catalog, but the `Find` prompt never asks for a skill. The agent searched the workspace for the code word, found nothing, and stopped. It made no CLI calls in 3 of 3 trials. The `Zeta` prompt names the need for an approved skill and triggers the skill in 3 of 3 trials. The original refined batch's 0/5 is explained by this gate, since it used the same `Find` prompt and the valid refined text.
2. **Skill-text gate (v1 fails).** The v1 description is invalid YAML. The catalog omits the skill, and an agent that does get a skill prompt cannot find it. With `Zeta`, the agent guessed the name `zeta-discovery` and failed. This gate is not what caused the original 0/5, but it is a separate failure mode that would hit any unquoted frontmatter.

Permissions are not a cause. The same flags were used in all 12 trials, and no permission denial appeared in any of them.

### Proposed fix for Step 31

Untested items are marked as such. They need a follow-up canary before they are relied on.

1. **Keep the refined body unchanged.** It is what produced 3/3 with the `Zeta` prompt. Keep the frontmatter `description` quoted.
2. **Guard frontmatter in CI.** Add a test that parses every shipped `SKILL.md` frontmatter as YAML and fails on invalid YAML. Untested: check whether `opendepot validate` and the Version controller already reject this. Their behavior on unquoted `: ` was not verified in this run.
3. **Put the trigger in the catalog description.** The description is the always-loaded text the model sees. Step 31 should edit only that `description`, so it names the trigger explicitly. Untested proposal: "Use when: a task needs a skill or agent you do not already have, when asked to find, install, or use an approved skill or agent, or when a task needs a capability an OpenDepot registry may provide." Confirm it with a follow-up canary that adds a third prompt type.
4. **Put the suggest instruction where the trigger fires.** Step 31's "run `opendepot suggest` before `list` and `describe`" must be in the same catalog description and body. Otherwise a `Find`-style prompt will not reach it.
5. **Keep the `Zeta` prompt as the primary benchmark.** Report `Find` as a no-trigger baseline, not as a discovery measure.

### Implications for the suggest design

- `suggest` cannot help an agent that never calls the CLI. Its evaluation must measure the trigger rate first, on the same prompts for every arm.
- The Evaluation plan's arms must use the `Zeta`-style prompt, with the registry and skill text held fixed.
- Trial results depend on the Copilot CLI version. Record the version for each run.
