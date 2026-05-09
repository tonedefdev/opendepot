---
name: gh-actions-debug
description: "Use when: a GitHub Actions workflow run fails, a pipeline is stuck, a job is erroring, CI is broken, you need to fetch run logs, list workflows, trigger a workflow dispatch, cancel a run, view run history, check workflow status, manage secrets or variables, or perform any GitHub Actions operation using the gh CLI. Triggers: 'workflow failed', 'pipeline broken', 'CI error', 'check the run logs', 'trigger the workflow', 'cancel the run', 'list workflows', 'view run history', 'what did the action output', 'why did the workflow fail', 'run the workflow', 'set a secret', 'add a variable'."
argument-hint: "What you want to do — e.g. 'debug the failing release run', 'trigger build workflow on main', 'list recent runs'"
---

# GitHub Actions via `gh` CLI

Use the `gh` CLI to interact with GitHub Actions — list, trigger, monitor, debug, cancel, and manage workflows and runs without leaving the terminal or needing an MCP server.

## Prerequisites

- `gh` CLI installed and authenticated (`gh auth status`)
- Run from within the repository root (where `.github/` lives)

---

## Workflows

### List all workflows in the repo
```bash
gh workflow list
```

### View a specific workflow definition
```bash
gh workflow view <workflow-name-or-id>
```

### Enable / disable a workflow
```bash
gh workflow enable <workflow-name-or-id>
gh workflow disable <workflow-name-or-id>
```

### Trigger a workflow dispatch manually
```bash
gh workflow run <workflow-name-or-id> --ref <branch>
```

Pass inputs if the workflow defines them:
```bash
gh workflow run <workflow-name-or-id> --ref main --field environment=production
```

---

## Runs

### List recent runs (all workflows)
```bash
gh run list --limit 10
```

### Filter by workflow, branch, status, or actor
```bash
gh run list --workflow release.yaml --limit 10
gh run list --branch main --status failure --limit 10
gh run list --user <username> --limit 10
```

### Watch a live run stream in the terminal
```bash
gh run watch <run-id>
```

### Cancel a run
```bash
gh run cancel <run-id>
```

### Delete a run
```bash
gh run delete <run-id>
```

---

## Inspecting & Debugging Runs

### View run summary (jobs + their status)
```bash
gh run view <run-id>
```

### Fetch full logs for a run
```bash
gh run view <run-id> --log
```

### Fetch only failed step logs (best first step for debugging)
```bash
gh run view <run-id> --log-failed
```

### List jobs in a run with statuses
```bash
gh run view <run-id> --json jobs --jq '.jobs[] | {name: .name, status: .status, conclusion: .conclusion}'
```

### View logs for a specific job
```bash
gh run view <run-id> --job <job-id> --log
```

### Re-run only failed jobs
```bash
gh run rerun <run-id> --failed
```

### Re-run the entire run
```bash
gh run rerun <run-id>
```

---

## Secrets & Variables

### List secrets (names only — values are never shown)
```bash
gh secret list
gh secret list --env <environment-name>
```

### Set a secret
```bash
gh secret set MY_SECRET --body "value"
gh secret set MY_SECRET --env production --body "value"
```

### Delete a secret
```bash
gh secret delete MY_SECRET
```

### List variables
```bash
gh variable list
```

### Set a variable
```bash
gh variable set MY_VAR --body "value"
```

---

## Debugging Workflow

When a run fails, follow this sequence:

1. `gh run list --limit 10` — find the failing run ID
2. `gh run view <run-id> --log-failed` — read only the failed steps
3. Cross-reference with `.github/workflows/<name>.yaml` to understand the step context
4. Fix the workflow YAML or underlying code
5. `gh run rerun <run-id> --failed` to re-run without triggering a new push

## Tips

- `--log-failed` trims all successful step output — use it first before pulling full logs
- Run IDs appear in GitHub UI URLs: `github.com/<owner>/<repo>/actions/runs/<run-id>`
- Use `--json` with `--jq` on any `gh run` command to extract structured data
- `gh run watch` streams live output — useful for triggered dispatches
