---
name: opendepot-cli
description: Use when: installing, planning, verifying, or describing Agent Skills and Agents from an OpenDepot registry with the opendepot CLI, or when a project has an opendepot.lock.hcl file.
---

# OpenDepot CLI

The `opendepot` CLI installs Agent Skills and Agents from an OpenDepot registry into a project. It records each installed item in `opendepot.lock.hcl` with a hash, and it checks those hashes before anything is written.

Use this skill to run the CLI correctly. Do not treat the CLI as a way to edit installed skills or agents.

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

## Commands

| Command | Purpose |
|---------|---------|
| `opendepot login <host>` | Sign in to a registry host. |
| `opendepot plan` | Show what `apply` would install, upgrade, or change. Writes nothing. |
| `opendepot apply` | Install or update items from the lock file. Writes files. |
| `opendepot list skills` | List skills. Use `--registry` and `--namespace` to filter. |
| `opendepot list agents` | List agents. Use `--registry` and `--namespace` to filter. |
| `opendepot describe skill <namespace/name>` | Show details for one skill. |
| `opendepot describe agent <namespace/name>` | Show details for one agent. |
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

1. Confirm the project has `opendepot.lock.hcl`. If it does not, ask a person which registry to use.
2. Run `opendepot verify`. If an installed item changed, restore it as described in the rules. If the lock file is invalid, report it.
3. Run `opendepot plan` to see any pending changes.
4. Run `opendepot apply` only when the plan is expected.
5. Run `opendepot verify` again.

## Hooks

Hooks can run `opendepot verify` before every tool call. When a check fails, the hook blocks the call and tells you to run `opendepot apply`. It allows that restore through, so you can recover without a person. See [Verify with Agent Hooks](../agents-hooks.md) for Claude Code, Codex, and Copilot configuration. Do not add, remove, or change hooks.

## Raw version

Agents can read this skill as raw Markdown at [https://opendepot.defdev.io/docs/guides/opendepot-cli/SKILL.md](https://opendepot.defdev.io/docs/guides/opendepot-cli/SKILL.md).
