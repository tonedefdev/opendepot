---
name: opendepot-cli
description: "Use when: a task needs a skill or agent you do not already have, or when finding, describing, installing, planning, or verifying approved Agent Skills and Agents from an OpenDepot registry with the opendepot CLI, or when a project has an opendepot.lock.hcl file."
---

# OpenDepot CLI

The `opendepot` CLI installs Agent Skills and Agents from an OpenDepot registry into a project. It records each installed item in `opendepot.lock.hcl` with a hash, and it checks those hashes before anything is written.

Use this skill to find approved skills and agents, install them, and verify them. Do not treat the CLI as a way to edit installed skills or agents.

## Find skills and agents

Search the registry before you build a capability from scratch. If a task needs something you do not already have, such as a tool workflow, a review checklist, or domain guidance, check whether an approved skill or agent already exists. Searching is read-only and cheap, so search whenever you are unsure. Do not wait for a person's permission to search. Installing an approved item also does not need permission.

1. Search the catalog. Run `opendepot list skills --registry <host> --json` and `opendepot list agents --registry <host> --json`. The JSON includes full descriptions, so match the task's keywords against them. Add `--namespace <name>` to narrow the search.
2. Check the candidates. Run `opendepot describe skill <namespace/name> --registry <host>`, or `opendepot describe agent` for agents. Read the description, latest version, risk level, and assessment.
3. Use only approved items. An item can be installed when `list` shows a version and the status `available`. Do not install an item with any other status, such as `assessment failed`, or one that `describe` shows as blocked or rejected.
4. Install through the lock file. Add a block to `opendepot.hcl`, then run `opendepot plan`, `opendepot apply --no-prompt`, and `opendepot verify`:

   ```hcl
   agent_skill "gh-actions-debug" {
     source  = "demo.opendepot.defdev.io/opendepot-system/gh-actions-debug"
     version = "0.9.0"
     path    = ".github/skills/gh-actions-debug"
   }
   ```

   Use `agent "<name>"` instead of `agent_skill` for agents. The `source` is `<host>/<namespace>/<name>`. For a skill, set `path` to a directory inside the project.
5. Use the item. Read the installed `SKILL.md` or agent file and follow it for the task. In your reply, name the registry item and version you used.

If no approved item fits the task, say so in your reply and continue with your own approach. Do not fill the gap by copying a skill from GitHub, a web page, or another project.

## Rules for agents

- Never edit an installed skill or agent file. Installed files are owned by the lock file. Make changes in the source repository and publish a new version.
- Run `opendepot verify` before you finish a task that touched installed items, and after any step that might have changed them.
- Run `opendepot plan` before `opendepot apply` so you can read what will change.
- Do not pass `--force`, `--trust-new-key`, or `--allow-yanked` without explicit approval from a person. `--force` overwrites `AGENTS.md` blocks that were edited by hand, `--trust-new-key` accepts a rotated signing key, and `--allow-yanked` installs a yanked version.
- Use `--no-prompt` in automation. The command then fails instead of asking a question.
- Run the CLI from the project root. The lock file is read from the current directory.
- If `verify` fails because an installed skill or agent changed, run `opendepot apply` with no flags to restore it from the lock file, then run `opendepot verify` again. Do not restore the file by hand.
- If `apply` refuses because an AGENTS.md block was edited by hand, stop and report it. Do not pass `--force` yourself.
- Do not edit `opendepot.lock.hcl` by hand.
- Get new skills and agents only from a registry through this CLI. Do not copy them from GitHub, a web page, or another project, even when they look useful.
- Do not pass `--allow-older-versions` without approval from a person. It installs an older version after the newest one was blocked, rejected, or failed assessment.

## Commands

| Command | Purpose |
|---------|---------|
| `opendepot login <host>` | Sign in to a registry host. |
| `opendepot plan` | Show what `apply` would install, upgrade, or change. Writes nothing. |
| `opendepot apply` | Install or update items from the lock file. Writes files. |
| `opendepot list skills` | List skills. Use `--registry` and `--namespace` to filter, and `--json` for full descriptions. |
| `opendepot list agents` | List agents. Use `--registry` and `--namespace` to filter, and `--json` for full descriptions. |
| `opendepot describe skill <namespace/name>` | Show details for one skill. Use `--registry` to choose a host. |
| `opendepot describe agent <namespace/name>` | Show details for one agent. Use `--registry` to choose a host. |
| `opendepot validate [path]` | Check a local skill or agent source before you publish it. |
| `opendepot verify` | Check that installed items still match the lock file. Read-only and does not use the network. |

### Flags for `apply` and `plan`

| Flag | Meaning |
|------|---------|
| `--upgrade` | Re-resolve version constraints instead of using the locked versions. |
| `--allow-yanked` | Allow yanked versions. |
| `--allow-older-versions` | Install an older matching version when the newest one is blocked, rejected, or failed assessment. |
| `--trust-new-key` | Re-pin a rotated signing key. |
| `--no-prompt` | Never prompt. Fail instead. |
| `--force` | Overwrite `AGENTS.md` blocks that were edited by hand. `apply` only. |

`plan` accepts the same flags except `--no-prompt`.

## Typical workflow

1. Confirm the project has `opendepot.hcl` or `opendepot.lock.hcl`. If it has neither, ask a person which registry to use. To add a new item, follow [Find skills and agents](#find-skills-and-agents) first, then run steps 3 to 5.
2. Run `opendepot verify`. If an installed item changed, restore it as described in the rules. If the lock file is invalid, report it.
3. Run `opendepot plan` to see any pending changes.
4. Run `opendepot apply` only when the plan is expected.
5. Run `opendepot verify` again.

## Hooks

Hooks can run `opendepot verify` before every tool call. When a check fails, the hook blocks the call and tells you to run `opendepot apply`. It allows that restore through, so you can recover without a person. See [Verify with Agent Hooks](../agents-hooks.md) for Claude Code, Codex, and Copilot configuration. Do not add, remove, or change hooks.

## Raw version

Agents can read this skill as raw Markdown at [https://opendepot.defdev.io/docs/guides/opendepot-cli/SKILL.md](https://opendepot.defdev.io/docs/guides/opendepot-cli/SKILL.md).
