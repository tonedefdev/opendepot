# Verify with Agent Hooks

`opendepot verify` checks that every installed skill and agent still matches the hash recorded in `opendepot.lock.hcl`. On its own, it runs only when you run it. A tool hook runs it before each tool call an agent makes. When a check fails, the hook does not stop the agent. It tells the agent how to restore the item with `opendepot apply`, and it allows that restore to run.

The hook is the enforcement layer. The CLI does not block edits by itself.

## Signatures and hashes

The version controller signs the `SHA256SUMS` file for each skill and agent version with a GPG (OpenPGP) key. The server serves the detached signature at `SHA256SUMS.sig`. The signature shows who published the version. It does not encrypt the content.

The checks happen in two places:

1. **At install.** `apply` verifies the `SHA256SUMS` signature against the signing key pinned on first use. It also checks that the archive matches the signed hash, then records the installed file hashes in `opendepot.lock.hcl`.
2. **At each tool call.** The hook runs `verify`, which hashes the installed files and compares them with the lock file. It does not check the signature again, and it makes no network calls.

So `verify` catches edits to installed files after install. It trusts the lock file, so review changes to `opendepot.lock.hcl` in the same way as `opendepot.hcl`. Set `signing_key` in `opendepot.hcl` to pin the expected signer. A signer change then fails the install instead of prompting.

## How the hook responds

1. Before each tool call, the hook runs `opendepot verify`. If it passes, the call runs.
2. If it fails, the hook blocks the call and gives the agent the reason: do not edit installed files, run `opendepot apply`, then retry.
3. The hook lets through only the exact commands `opendepot apply` and `opendepot verify`. Calls such as `opendepot apply --force`, chained commands, and reads of the installed file stay blocked.
4. The agent runs `opendepot apply`, which rewrites the installed files from the lock file. The retried call passes.

If `apply` refuses because someone edited an AGENTS.md block by hand, the agent stops and tells you. The hook does not allow `--force`, which would overwrite that edit.

## Limits

- **The current write is not blocked.** A hook runs before a tool call, so an edit the agent makes with its current call still lands on disk. The next tool call fails verification, and the agent restores the file.
- **Hook config is editable by the agent.** `.claude/settings.json`, `.codex/hooks.json`, and `.github/hooks/*.json` live in the repository. An agent could remove the hook. Use managed hooks where you need to prevent this. See [Managed hooks](#managed-hooks).
- **Config changes are not guarded.** The agent can edit `opendepot.hcl` and run `apply` to install a different version. The hook checks installed files, not the config. Commit `opendepot.hcl` and `opendepot.lock.hcl` so changes show up in review.
- **Run hooks only in repositories with a lock file.** `verify` fails without `opendepot.lock.hcl`, so a hook in a repository without one blocks every tool call. Add hook config to projects that use `opendepot apply`. Do not add it to your user-level settings, which apply to every project.
- **Run the agent from the project root.** The hook reads `opendepot.lock.hcl` from the directory the agent runs in.
- **Copilot checks once per batch of tool calls.** If the agent issues an edit and a read in the same batch, the read runs without a check. The next batch is checked.
- **Codex hooks must be reviewed before they run.** An unreviewed hook is skipped without a warning. See [Codex](#codex).
- **The hooks feature can be turned off.** Codex has a `[features] hooks = false` setting. Use managed requirements if you rely on this control.

Claude Code and Codex receive the hook's stderr message as the tool result. Copilot does not show a non-zero exit's stderr to the model, so its hook writes a JSON deny with the reason to stdout instead.

Copilot was tested with a real model, `gpt-6-luna`. Claude Code 2.1.311 and Codex 0.169.0 were tested with a scripted model, which checks the hook contract but not how a model acts. The restore command for those two was tested offline with sample payloads. It has not been run in a real Claude Code or Codex session.

## Claude Code

Add this to `.claude/settings.json` in the project root. The `"*"` matcher runs the hook for every tool, including tools in subagents.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "input=$(cat); opendepot verify >/dev/null 2>&1 && exit 0; printf '%s' \"$input\" | grep -Eq '\"command\"[[:space:]]*:[[:space:]]*\"opendepot (apply|verify)\"([[:space:],}]|$)' && exit 0; echo 'OpenDepot: an installed skill or agent changed. Do not edit installed files. Run opendepot apply to restore it from the lock file, then retry. If apply refuses because an AGENTS.md block was edited by hand, stop and tell the user.' >&2; exit 2"
          }
        ]
      }
    ]
  }
}
```

Claude Code merges hook settings across user, project, and managed levels. A hook in your user settings runs in every project, so keep this one in the project file. See [Managed hooks](#managed-hooks) for restricting hooks to managed ones.

## Codex

Add this to `.codex/hooks.json` in the project root. The `".*"` matcher covers every tool that `PreToolUse` fires for.

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "input=$(cat); opendepot verify >/dev/null 2>&1 && exit 0; printf '%s' \"$input\" | grep -Eq '\"command\"[[:space:]]*:[[:space:]]*\"opendepot (apply|verify)\"([[:space:],}]|$)' && exit 0; echo 'OpenDepot: an installed skill or agent changed. Do not edit installed files. Run opendepot apply to restore it from the lock file, then retry. If apply refuses because an AGENTS.md block was edited by hand, stop and tell the user.' >&2; exit 2",
            "timeout": 30
          }
        ]
      }
    ]
  }
}
```

Codex runs a hook only after you review it. The interactive CLI prompts for review before an unreviewed hook runs. In testing, an unreviewed hook did not run, and a tampered skill was read with no warning. `codex exec --dangerously-bypass-hook-trust` skips the review. Use it only for testing. This page does not cover a headless setup that trusts hooks through config.

## Copilot

Add this to `.github/hooks/opendepot.json`. Copilot's `preToolUse` event fires for every tool, so it has no matcher.

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "bash": "input=$(cat); opendepot verify >/dev/null 2>&1 && exit 0; printf '%s' \"$input\" | grep -Eq '\"command\"[[:space:]]*:[[:space:]]*\"opendepot (apply|verify)\"([[:space:],}]|$)' && exit 0; printf '%s\\n' '{\"permissionDecision\": \"deny\", \"permissionDecisionReason\": \"OpenDepot: an installed skill or agent changed. Do not edit installed files. Run opendepot apply to restore it from the lock file, then retry. If apply refuses because an AGENTS.md block was edited by hand, stop and tell the user.\"}'",
        "timeoutSec": 60
      }
    ]
  }
}
```

The hook reads the tool call from stdin and checks `toolArgs.command`. When the check fails, it prints a JSON `permissionDecision: deny` with the reason and exits 0. A non-zero exit also blocks the call, but the agent receives only `hook exited with code 2`. The Windows PowerShell form is not covered here because it has not been tested.

Copilot loads project hooks only when the folder is trusted. Add the project path to `trustedFolders` in your Copilot config (`~/.copilot/config.json`). In testing, an untrusted project's hook did not run.

### Example run

These runs used Copilot CLI 1.0.112 and the small model `gpt-6-luna`. The installed skill was `opendepot-system/gh-actions-debug` 0.9.0 from `demo.opendepot.defdev.io`, and the hook above was in `.github/hooks/opendepot.json`. Each run passed `--add-dir` for the skill folder, which sits outside the project. Without it, Copilot's own permission check denies the read before the hook runs. The stats footer is removed.

The clean state, where the skill matches the lock file:

```text
 MD Read ~/.copilot/skills/gh-actions-debug/SKILL.md L1:1 (1 line read)

---
```

The tampered state, after a line was appended to the skill and the agent was asked to read it:

```text
 MD Read ~/.copilot/skills/gh-actions-debug/SKILL.md Denied by preToolUse hook: OpenDepot: an insta…
   Denied by preToolUse hook: OpenDepot: an installed skill or agent changed. Do not edit installed
   files. Run opendepot apply to restore it from the lock file, then retry. If apply refuses because
   an AGENTS.md block was edited by hand, stop and tell the user.

 ✗ skill(gh-actions-debug)
   Denied by preToolUse hook: OpenDepot: an installed skill or agent changed. Do not edit installed
   files. Run opendepot apply to restore it from the lock file, then retry. If apply refuses because
   an AGENTS.md block was edited by hand, stop and tell the user.

 $ Shell Restore locked skill installation 14 lines…
   opendepot apply

 MD Read ~/.copilot/skills/gh-actions-debug/SKILL.md L1:1 (1 line read)

---
```

The agent first tried to load the skill with the `skill` tool and then to read the file. The hook denied both. The agent then ran `opendepot apply`, which the hook allowed. Its retried read passed, and the file matched the locked version afterward.

The session log records each hook decision. This excerpt is trimmed to the fields that matter:

```text
hook.start   preToolUse  view  SKILL.md
hook.end     success: true   output: "Denied by preToolUse hook: OpenDepot: ..."
tool.execution_complete  view   success: false
hook.start   preToolUse  skill  gh-actions-debug
hook.end     success: true   output: "Denied by preToolUse hook: OpenDepot: ..."
tool.execution_complete  skill  success: false
hook.start   preToolUse  bash  opendepot apply
hook.end     success: true   output: ""
tool.execution_complete  bash   success: true
hook.start   preToolUse  view  SKILL.md
hook.end     success: true   output: ""
tool.execution_complete  view   success: true
```

`success: true` on `hook.end` means the hook ran. The denial is in its output.

## Unattended runs

The restore does not need a person. The agent runs `opendepot apply` itself, so a run with nobody watching still recovers from a changed skill or agent. The restore installs the version in `opendepot.lock.hcl`. It does not upgrade.

Two cases still need a person:

- **A hand-edited AGENTS.md block.** Plain `apply` refuses it, and the hook blocks `apply --force`. The agent stops and reports the block. Decide whether to keep the edit, then run `apply --force` yourself if you want to overwrite it.
- **A failed restore.** If the registry is unreachable or the credentials are missing, `apply` fails, the hook keeps blocking, and the agent reports the failure. The agent's environment needs the same registry access and credentials that `apply` uses.

On the demo registry, `apply` took a median of about 1.2 s over three runs (1.15–1.5 s). It runs only after a failed check, so a passing check still costs the usual few milliseconds.

## Optional: automatic `apply --force`

If nobody can answer the agent's stop-and-report, you can add a second hook that runs `opendepot apply --force` when `verify` fails. Use it only if you accept that it overwrites hand edits in AGENTS.md blocks without asking.

The skill and agent restore does not need `--force`. Plain `apply` already restores them, so `--force` only changes the case where an AGENTS.md block was edited by hand. Leave this hook out unless that trade-off is acceptable.

## Latency

The check is local. It reads the lock file, hashes the installed files, and compares them, with no network calls. Each tool call pays that cost once. Measured on macOS, with the verify command the hook runs:

| Installed content | `opendepot verify` in the hook (median) |
|---|---|
| 1 skill, 8 KB | about 10 ms |
| 25 skills, 600 KB | about 14–17 ms |
| 10 skills × 200 files, about 8 MB | about 45 ms |

The clean and tampered paths do the same check, so their times match. The shell itself adds about 3 ms. In a Copilot run on the 25-skill project, the five hook invocations each took 16–20 ms, as recorded in the session log. Cost grows with the number of installed bytes, not with the number of tool calls.

## Managed hooks

A project hook can be removed by the agent that it guards. To prevent that, deploy the hook from an administrator-controlled location and tell each harness to use only those hooks.

- **Claude Code:** set `allowManagedHooksOnly` in managed settings. Only hooks from managed settings run.
- **Codex:** set `allow_managed_hooks_only = true` in `requirements.toml`, and point `[hooks] managed_dir` at the directory that holds the hook config.
- **Copilot:** Copilot also supports administrator-deployed hooks. See GitHub's hooks reference for how to deploy them.

The managed settings above come from each vendor's documentation. They were not tested here. Check each harness's documentation for the exact file locations on your platform before you rely on managed hooks.

## Related

- [Consuming Agents and Skills](agents.md): install and verify skills and agents.
- [CLI reference](../reference/cli.md): `verify`, the lock file, and install targets.
- [OpenDepot CLI skill](opendepot-cli/SKILL.md): a skill that helps agents use the CLI.
