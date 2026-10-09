# CLI Reference

The `opendepot` CLI installs skills and agents from OpenDepot registries into a project. It resolves each entry against the registry, verifies the signature and hashes, and writes the files into the project's install targets. The lock file records exactly what was installed so that `verify` can check it later without network access.

## Commands

| Command | Purpose |
|---------|---------|
| `opendepot login <host>` | Log in to a registry with the login.v1 flow and store the token. `<host>` is a hostname such as `registry.example.com`. The registry must advertise login.v1. |
| `opendepot apply` | Resolve, verify, and install every entry in `opendepot.hcl`. |
| `opendepot plan` | Show what `apply` would change, comparing the project and lock file with the registry. Downloads and verifies content, then writes nothing. Never prompts. |
| `opendepot validate [path]` | Validate a `SKILL.md`, a skill directory, or an agent `.md` file. Defaults to the current directory when `path` is omitted. Frontmatter keys are checked against the union of all platform key sets. The command has no platform option. |
| `opendepot verify` | Check the installed targets against `opendepot.lock.hcl` without network access. |

## Flags

Flags apply to `opendepot apply` unless noted. `opendepot plan` accepts `--upgrade`, `--allow-yanked`, `--trust-new-key`, and `--force` with the same meaning, and it never prompts. A changed signing key appears as an error in the plan instead of a prompt.

| Flag | Purpose |
|------|---------|
| `--upgrade` | Re-resolve version constraints instead of reusing the versions in the lock file. |
| `--allow-yanked` | Allow yanked versions. Without this flag, the CLI skips yanked versions for new resolution. |
| `--trust-new-key` | Re-pin a rotated signing key. Without this flag, a changed signing key fails the install. |
| `--no-prompt` | Never prompt. Fail instead of asking for confirmation. Use this in CI. |
| `--force` | Overwrite AGENTS.md blocks that were edited by hand. |

## opendepot.hcl

`opendepot.hcl` declares the skills and agents a project uses. It has one optional `opendepot` block and any number of `agent_skill` and `agent` blocks.

```hcl
opendepot {
  targets = ["copilot", "claude"]
}

agent_skill "release-notes" {
  source  = "registry.example.com/my-org/release-notes"
  version = "~> 1.2"
}

agent "code-reviewer" {
  source      = "registry.example.com/my-org/code-reviewer"
  version     = ">= 2.1.0, < 3.0.0"
  signing_key = "3E9A1F0C2B7D4E5A6F8091A2B3C4D5E6F7A8B9C0"
  path        = "tools/agents/code-reviewer.md"
}
```

| Field | Required | Purpose |
|-------|----------|---------|
| `source` | Yes | The registry address of the skill or agent. |
| `version` | Yes | A version constraint. |
| `signing_key` | No | Pins the expected GPG signing key fingerprint. A mismatch fails the install and never prompts. |
| `path` | No | Installs to this path instead of the default targets. A custom path replaces `targets` for that entry. |
| `targets` | In the `opendepot` block | The install targets for entries that do not set `path`. |

Each block label is the local name of the entry. Names must be unique within the file. An entry needs at least one install target, from `targets` or from `path`.

### Install targets

| Target | Skill | Agent |
|--------|-------|-------|
| `copilot` | `.github/skills/{name}/` | `.github/agents/{name}.agent.md` |
| `claude` | `.claude/skills/{name}/` | `.claude/agents/{name}.md` |
| `agents-md` | A block in `AGENTS.md` | A block in `AGENTS.md` |

The `agents-md` target writes each entry into `AGENTS.md` between markers:

```markdown
<!-- opendepot:begin release-notes -->
...installed content...
<!-- opendepot:end release-notes -->
```

The CLI refuses to overwrite a block that was edited by hand unless you pass `--force`.

## opendepot.lock.hcl

`apply` writes `opendepot.lock.hcl` next to `opendepot.hcl`. Commit this file. It records what was installed:

```hcl
agent_skill "registry.example.com/my-org/release-notes" {
  version        = "1.2.4"
  constraints    = "~> 1.2"
  signing_key    = "3E9A1F0C2B7D4E5A6F8091A2B3C4D5E6F7A8B9C0"
  signature_hash = "h1:..."
  hashes         = ["h1:...", "zh:..."]

  installed "copilot" {
    path = ".github/skills/release-notes"
    hash = "h1:..."
  }
}
```

| Field | Purpose |
|-------|---------|
| `version` | The resolved version. |
| `constraints` | The constraint from `opendepot.hcl` that produced the version. |
| `signing_key` | The pinned GPG signing key fingerprint. |
| `signature_hash` | The hash of the signed `SHA256SUMS` file. |
| `hashes` | The `h1:` directory hash and the `zh:` archive hash of the package. |
| `installed` | One block per install target, labeled with the target name. Each records the `path` and the `h1:` `hash` of the installed content. |

Use `opendepot verify` in CI to confirm that the installed files match the lock file.

## File safety

The CLI does not follow symlinks. It refuses to read `opendepot.hcl`, `opendepot.lock.hcl`, and `AGENTS.md` when any of them is a symlink. It also refuses install targets that are symlinks. The error is `refusing to follow symlink <path>`. Writes and removals use the same check.

Archive extraction is limited to 2 GiB of decompressed data and 10,000 entries. An archive that exceeds either limit fails the install.

## agents.v1 protocol

The registry serves skills and agents under `/opendepot/agents/v1/`. `{kind}` is `skills` or `agents`. The CLI uses these endpoints:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/versions` | List available versions. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/download` | Return the archive location and checksums. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/archive` | Download the packaged archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS` | Return the checksums for the archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS.sig` | Return the GPG signature for `SHA256SUMS`. |

The service discovery document advertises the base path as `agents.v1`. Requests are subject to GroupBinding authorization. Archive requests return a `302` redirect to a checksum-keyed download URL. The redirect URL remains valid after GroupBinding access is revoked. See [agents.v1 protocol](../guides/agents.md#agentsv1-protocol) for details.

## Signing key trust

The first install pins the signing key that signed the package (trust on first use). Later installs must match the pinned key.

- If `signing_key` is set in `opendepot.hcl`, the signer must match it. A mismatch fails and never prompts.
- If the signer changes, the install fails until you re-run with `--trust-new-key` to re-pin the new key.
- With `--no-prompt`, a changed key always fails.

## Plan before apply

`opendepot plan` runs the same resolution, verification, and staging as `apply`, then reports the result without writing any file or the lock file. Each entry shows a header with the locked and resolved versions and one row per install target:

| Symbol | Meaning |
|--------|---------|
| `+` | The target will be created. |
| `~` | The target will be updated to the planned content. |
| `!` | The target was edited by hand. `apply` will overwrite it only with `--force`. |
| `=` | The target already matches. |
| `-` | A stale path or AGENTS.md block will be removed. |

When the registry has a newer version than the lock file, the plan shows it with a note to run `opendepot apply --upgrade`. The plan ends with a summary, and `opendepot apply` writes it.

## Blocked versions

A version that a Jev policy blocks is not installed. The registry returns it as not found, and the CLI also refuses to stage a blocked version if the download metadata marks it as blocked. The error lists the policy reasons. Jev scores are advisory and do not gate installs. They are shown during `apply` and `plan` and are not written to the lock file.

## Example workflow

```bash
opendepot login registry.example.com
opendepot validate ./skills/release-notes
opendepot plan
opendepot apply
opendepot verify
```

In CI, run `opendepot apply --no-prompt` and then `opendepot verify`. Use `opendepot plan` in review steps to see the changes before they are applied.

## See also

- [Consuming Agents and Skills](../guides/agents.md): publish Depots and configure Jev.
