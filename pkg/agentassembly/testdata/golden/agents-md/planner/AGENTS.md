# Plans the release

## Skills

- `release-notes`

## Tools

- `github/get_pull_request`
- `github/list_issues`

## Delegation

Delegate only to `reviewer`, `scanner`, `writer`.

- Run `reviewer` as a subagent with context `summary` (link `delegate-review`).
- Run `scanner` in parallel (group `research`) with context `outputs`, returning `findings` (link `delegate-scan`).
- Hand off to `writer` with context `task-only` (link `delegate-write`).

