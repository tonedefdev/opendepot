# CLI Reference

The `opendepot` CLI installs skills and agents from OpenDepot registries into a project. It resolves each entry against the registry, verifies the signature and hashes, and writes the files into the project's install targets. The lock file records exactly what was installed so that `verify` can check it later without network access.

## Download the binary

The registry server serves prebuilt `opendepot` binaries, so users can install the CLI without a Go toolchain. The UI exposes them on the **Download CLI** page, and the same files are available directly from the server at the following public paths:

| Path | Returns |
|------|---------|
| `/opendepot/cli/v1/{os}/{arch}` | The binary for one platform. |
| `/opendepot/cli/v1/SHA256SUMS` | SHA-256 checksums for every binary. |

Supported platforms are `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, and `windows/amd64`. Windows binaries end in `.exe`. Any other platform returns 404.

Verify a download against the checksums before you install it:

```bash
curl -fLO https://<registry-host>/opendepot/cli/v1/linux/amd64
curl -fLO https://<registry-host>/opendepot/cli/v1/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
chmod +x opendepot_linux_amd64
sudo mv opendepot_linux_amd64 /usr/local/bin/opendepot
opendepot --version
```

The server image bundles the binaries at `/opt/opendepot/cli/`. The image build sets the version with `--build-arg CLI_VERSION=<version>`, which defaults to `dev`. The server reads that directory from the `-cli-dist-dir` flag. If the directory has no binary for a platform, that platform returns 404, so local development without bundled binaries still works. The binaries are not GPG-signed. Use the checksum file to verify them.

## Commands

| Command | Purpose |
|---------|---------|
| `opendepot login <host>` | Log in to a registry with the login.v1 flow and store the token. `<host>` is a hostname such as `registry.example.com`. The registry must advertise login.v1. |
| `opendepot apply` | Resolve, verify, and install every entry in `opendepot.hcl`. |
| `opendepot plan` | Show what `apply` would change, comparing the project and lock file with the registry. Downloads and verifies content, then writes nothing. Never prompts. |
| `opendepot validate [path]` | Validate a `SKILL.md`, a skill directory, or an agent `.md` file. Defaults to the current directory when `path` is omitted. Frontmatter keys are checked against the union of all platform key sets. The command has no platform option. |
| `opendepot verify` | Check the installed targets against `opendepot.lock.hcl` without network access. |
| `opendepot list skills` or `opendepot list agents` | List the skills or agents on a registry. Read-only. See [List skills and agents](#list-skills-and-agents). |
| `opendepot describe skill <namespace/name>` or `opendepot describe agent <namespace/name>` | Show every detail for one resource. `--json` prints them as JSON. |

Command failures are written to standard error in a distinct error section, with the source when available and the underlying details. Jev assessment output includes an `Error` row; `none reported` means the assessment did not report an error.

## Flags

Flags apply to `opendepot apply` unless noted. `opendepot plan` accepts `--upgrade`, `--allow-yanked`, `--allow-older-versions`, `--trust-new-key`, and `--force` with the same meaning, and it never prompts. A changed signing key appears as an error in the plan instead of a prompt.

| Flag | Purpose |
|------|---------|
| `--upgrade` | Re-resolve version constraints instead of reusing the versions in the lock file. |
| `--allow-yanked` | Allow yanked versions. Without this flag, the CLI skips yanked versions for new resolution. |
| `--allow-older-versions` | Install an older matching version when a newer one is blocked, rejected, or failed assessment. See [Allow older versions](#allow-older-versions). |
| `--trust-new-key` | Re-pin a rotated signing key. Without this flag, a changed signing key fails the install. |
| `--no-prompt` | Never prompt. Fail instead of asking for confirmation. Use this in CI. |
| `--force` | Overwrite AGENTS.md blocks that were edited by hand. |

## opendepot.hcl

`opendepot.hcl` declares the skills and agents a project uses. It has one optional `opendepot` block and any number of `agent_skill` and `agent` blocks.

```hcl
opendepot {
  targets  = ["copilot", "claude"]
  registry = "registry.example.com"
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
| `registry` | No, in the `opendepot` block | The registry host that `opendepot list` queries, such as `registry.example.com`. Use a bare host, not a URL. `apply` and `plan` ignore it. |

Each block label is the local name of the entry. Names must be unique within the file. An entry needs at least one install target, from `targets` or from `path`.

### Install targets

The `copilot` and `claude` targets install into folders in your home directory, so they are shared by every project on the machine:

| Target | Skill | Agent |
|--------|-------|-------|
| `copilot` | `~/.copilot/skills/{name}/` | `~/.copilot/agents/{name}.agent.md` |
| `claude` | `~/.claude/skills/{name}/` | `~/.claude/agents/{name}.md` |
| `codex` | `~/.agents/skills/{name}/` | `~/.codex/agents/{name}.toml` |
| `agents-md` | A block in `AGENTS.md` | A block in `AGENTS.md` |

Agent files are copied as published. Claude agents use the `.md` format, and Copilot agents keep the `.agent.md` suffix.

Each agent format installs only to the targets that read it:

- A Codex TOML agent (`{name}.toml`) installs only to `codex`, or to a custom `path`. It is never written to `AGENTS.md`, because that target would store the raw TOML in Markdown.
- A Markdown agent (`{name}.agent.md` or `{name}.md`) installs to `copilot`, `claude`, and `agents-md`. It never installs to `codex`.

If an agent has no target that accepts its format, the install fails before anything is written. Add `codex` to `targets` for a TOML agent, or add `copilot` or `claude` for a Markdown agent.

To install into a project instead, set `path` on the entry, for example `path = ".github/skills/release-notes"`. A custom path is relative to the project and replaces `targets` for that entry.

The `agents-md` target writes each entry into `AGENTS.md` between markers:

```markdown
<!-- opendepot:begin release-notes -->
...installed content...
<!-- opendepot:end release-notes -->
```

The CLI refuses to overwrite a block that was edited by hand unless you pass `--force`.

## opendepot.lock.hcl

`apply` writes `opendepot.lock.hcl` next to `opendepot.hcl`. It records what was installed:

```hcl
agent_skill "registry.example.com/my-org/release-notes" {
  version        = "1.2.4"
  constraints    = "~> 1.2"
  signing_key    = "3E9A1F0C2B7D4E5A6F8091A2B3C4D5E6F7A8B9C0"
  signature_hash = "h1:..."
  hashes         = ["h1:...", "zh:..."]

  installed "copilot" {
    path = "/Users/you/.copilot/skills/release-notes"
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

Commit this file. Paths for the `copilot` and `claude` targets are absolute home-directory paths, so the lock file is specific to the machine that last ran `apply`. Teammates on other machines get different paths after they run `apply`. Use `path` on an entry when the install must live in the repository.

Use `opendepot verify` in CI to confirm that the installed files match the lock file. To run it before each agent tool call, see [Verify with Agent Hooks](../guides/agents-hooks.md).

## File safety

The CLI does not follow symlinks. It refuses to read `opendepot.hcl`, `opendepot.lock.hcl`, and `AGENTS.md` when any of them is a symlink. It also refuses install targets that are symlinks. The error is `refusing to follow symlink <path>`. Writes and removals use the same check.

Archive extraction is limited to 2 GiB of decompressed data and 10,000 entries. An archive that exceeds either limit fails the install.

## agents.v1 protocol

For the full protocol reference, see [agents.v1 Protocol](agents-v1.md).

The registry serves skills and agents under `/opendepot/agents/v1/`. `{kind}` is `skills` or `agents`. The CLI uses these endpoints:

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/versions` | List available versions. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/download` | Return the archive location and checksums. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/archive` | Download the packaged archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS` | Return the checksums for the archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS.sig` | Return the GPG signature for `SHA256SUMS`. |
| GET | `/opendepot/agents/v1/catalog` | List the skills and agents the caller can see. Used by `opendepot list`. |

The service discovery document advertises the base path as `agents.v1`. Requests are subject to GroupBinding authorization. Archive requests return a `307` redirect to a presigned URL when presign is enabled for the agent source. Otherwise the server streams the archive. A presigned URL remains valid until it expires, even after GroupBinding access is revoked. See [agents.v1 protocol](agents-v1.md) for details.

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

## List skills and agents

`opendepot list skills` and `opendepot list agents` show the skills or agents on a registry. The command is read-only. It writes no files, and it prints nothing until every page of the catalog has loaded.

| Flag | Purpose |
|------|---------|
| `--registry <host>` | The registry host to query. Overrides the project setting. |
| `--namespace <name>` | List only the resources in one namespace. |
| `--json` | Print the catalog as JSON, with the full `description` for each item. |

The CLI picks the registry in this order:

1. The `--registry` flag.
2. The `registry` attribute in the `opendepot` block of `opendepot.hcl`.
3. The single host shared by every `source` in `opendepot.hcl`.

If none of these yields one host, the command fails. The error asks you to pass `--registry`, or to set `registry` in `opendepot.hcl`. With several different hosts in `source`, the error lists them.

Each row shows one skill or agent:

| Column | Meaning |
|--------|---------|
| `SOURCE` | The `namespace/name` of the resource. |
| `VERSION` | The latest installable version, or `none`. |
| `RISK` | The risk level and score of the latest version, or `unknown`. |
| `STATUS` | Whether the displayed version is available, plus any unavailable candidate's status. |
| `DESCRIPTION` | The first line of the latest installable version's description, with ANSI escape sequences deleted and other control or format characters replaced by spaces, and the text cut to 80 characters. The text is author-supplied. The table shows it as plain text. `--json` prints the full description. |

Each resource takes one line. Use `opendepot describe skill <namespace/name>` or `opendepot describe agent <namespace/name>` to inspect a single resource. The table shows its source, sync and block state, usage counts, metadata, full description, storage backend and GitHub client settings, source scan findings, every version with its status and checksum, the latest Jev assessment, and the exemption, unblock, and threshold details. Empty fields are left out of the table. Description and other author text is shown as plain text, with ANSI escape sequences deleted and other control or format characters replaced by spaces. Lines wrap to the table width. `--json` prints every field, including `definitionContent`, as one object with stable keys. Empty values are `null` or `[]`. The fields are listed in [Detail fields for describe](agents-v1.md#detail-fields-for-describe). Describe accepts `--registry` and uses the same registry selection and UI browse access rules as list. A version marked `synced` is not necessarily installable. See [Unavailable candidates](../guides/agents.md#unavailable-candidates) for the statuses.

If the registry has OIDC enabled, `list` needs a token. Run `opendepot login <host>` first. The listing then shows public resources and the resources that your GroupBindings allow.

The command reports these errors:

| Response | Message |
|----------|---------|
| `401` or `403` | Not authorized. The message includes the HTTP status and tells you to run `opendepot login <host>` and retry. |
| `404` | The registry does not support the agents.v1 catalog. It may be an older OpenDepot server. |
| `5xx` | The catalog could not be listed. The message says that no results were shown. |

## Blocked versions

A version that a Jev policy blocks is not installed. The registry lists it with `blocked: true` and the Jev block reasons, so the CLI does not treat it as missing. Resolution skips a blocked version and picks the highest unblocked match. When the only match is blocked, the CLI explains why in the same label-column layout it uses for Jev assessments. It names the source and version, the Jev status, each policy reason, and what to do next:

```text
✖ registry.example.com/opendepot-system/anti-ui-slop@1.0.0
  Status       blocked by a Jev policy · not installable
  Reason       overall risk score 0.80 in part 1 is above the policy limit of 0.50
  Next         installs are refused until the policy allows this version
```

A locked version that becomes blocked gets the same output, with a `Next` line that says the lock file pins the version and to re-run with `--upgrade`. When no version satisfies the constraint, the error lists the published versions, marked `(yanked)` or `(blocked)` where they apply. Jev scores are shown during `apply` and `plan` and are not written to the lock file.

### Allow older versions

Resolution can also find a newer version that matches the constraint but cannot be installed. If that version is blocked, rejected, or failed assessment, and an older matching version is installable, `apply` and `plan` stop before they write anything. The error names the newer version, its status, and its reasons, and it suggests `--allow-older-versions`.

Pass `--allow-older-versions` to install the older version instead. Both `apply` and `plan` print a warning that names the newer version that was skipped. The flag does not bypass any of these checks:

- Signature, hash, and signing-key verification.
- Yank checks. Use `--allow-yanked` for yanked versions.
- Jev blocks on the version that would be installed.

Some cases do not refuse:

- A `pending` version is not a refusal. The CLI installs the best installable version without the flag.
- An unversioned commit is never compared against a version constraint. It can add context to a refusal, but it cannot cause one by itself.

A locked version is never re-resolved by this flag. If the lock file pins a version that is now blocked, `apply` fails and asks you to re-run with `--upgrade`. To fall back to an older version after that, pass both `--upgrade` and `--allow-older-versions`.

Registries that predate the `unavailableCandidates` field do not report newer blocked versions. The CLI cannot see them on those registries, so it installs the best installable version without a refusal.


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

- [Consuming Agents and Skills](../guides/agents.md): install skills and agents into a project.
- [Publishing Agents and Skills](../guides/agents-publishing.md): configure the Depots that publish them.
- [agents.v1 Protocol](agents-v1.md): the endpoints the CLI calls.
