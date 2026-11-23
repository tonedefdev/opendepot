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
