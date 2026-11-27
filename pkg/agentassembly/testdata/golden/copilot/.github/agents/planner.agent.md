---
name: planner
description: "Plans the release"
tools: ["github/get_pull_request", "github/list_issues", "task"]
skills: ["release-notes"]
---

# Plans the release

## Delegation

Delegate only to `reviewer`, `scanner`, `writer`.

- Run `reviewer` as a subagent with context `summary` (link `delegate-review`).
- Run `scanner` in parallel (group `research`) with context `outputs`, returning `findings` (link `delegate-scan`).
- Hand off to `writer` with context `task-only` (link `delegate-write`).

