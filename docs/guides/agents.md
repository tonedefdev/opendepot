# Consuming Agents and Skills

OpenDepot publishes Agent Skills and Agents from Git repositories. Install them into a project with the `opendepot` CLI, which verifies signatures and hashes before anything is written to disk. To publish them, see [Publishing Agents and Skills](agents-publishing.md).

## Install with the CLI

```bash
opendepot login registry.example.com
opendepot list skills
opendepot describe skill my-org/release-notes
opendepot plan
opendepot apply
opendepot verify
```

- `opendepot list skills` and `opendepot list agents` show what a registry offers. Listing is read-only.
- `opendepot describe skill <namespace/name>` or `opendepot describe agent <namespace/name>` shows the versions, rejection reasons, and latest Jev assessment for one resource.
- `opendepot plan` shows what `apply` would change without writing anything.
- `opendepot apply` installs every entry in `opendepot.hcl`.
- `opendepot verify` checks the installed targets against `opendepot.lock.hcl` without network access.

For every command and flag, see the [CLI reference](../reference/cli.md).

## Browse view

The UI browse view shows the sync status of each Version. It also shows blocked and unsynced Versions, and the scan and Jev findings for them. Anyone who can see the parent Agent or Skill can see this data. Browse is filtered per Agent through the caller's GroupBinding. This is intentional, so operators can see why a Version was blocked.

For a Skill or Agent that follows a branch, the detail header shows the source repository, the tracked branch, and the source path. It also shows the latest commit as a 7-character SHA. The Versions table lists every Version, including branch Versions in the `Pending` and `Held` phases. Each row shows its commit with a copy button and a branch phase chip. The commit column appears when at least one row has a commit. A Version without a semver shows `—`. See [Agent Details](registry-explorer/agent-details.md) and [Skill Details](registry-explorer/skill-details.md) for the full page layout.

The `presign` setting on an Agent or Skill source is copied to each Version it owns. Changing `presign` on the source updates every existing Version on the next reconcile, including yanked and rejected Versions. Removing `presign` from the source clears it on the Versions. Manual edits to a Version's `presign` are overwritten to match the source. Other Version fields are not changed by this sync.

## Yanked and blocked Versions

Yank a Version by setting `spec.yanked: true` on the Version resource. Yanked Versions stay listed and downloadable, and the listing includes `yanked: true`. The CLI skips yanked Versions when it resolves a version constraint unless you pass `--allow-yanked`.

A Version that Jev blocks is listed with `blocked: true` and its `blockReasons`, so clients can explain why it is unavailable. It is never served, and a direct download request for it returns `404`. The CLI skips blocked Versions when it resolves a version constraint. Causes include a threshold breach, a `Critical` risk level, a Service failure, a content outcome, and Jev disabled on the controller while `jevSecretRef` is set.

### Unavailable candidates

The versions response can include `unavailableCandidates`. It lists newer Versions that match the constraint but cannot be installed, so clients can explain why the newest matching Version is missing. The field is optional and is omitted when there are none. Servers that predate it never send it, so clients must treat a missing field as not reported, not as no candidates.

Each entry has these fields:

| Field | Purpose |
|-------|---------|
| `version` | The version of a versioned candidate. Omitted for an unversioned commit. |
| `commit` | The source commit of an unversioned candidate. Omitted for a versioned candidate. |
| `status` | One of `blocked`, `rejected`, `pending`, or `assessmentFailed`. |
| `reasons` | Sanitized text. Jev block reasons are passed through for Jev-blocked candidates. All other reasons are fixed messages. Raw sync status is never returned. |

| Status | Meaning |
|--------|---------|
| `blocked` | A Jev policy blocks the Version. |
| `rejected` | A scan policy rejected the Version, or the branch lifecycle rejected the commit. An unversioned commit that Jev blocks is also reported as `rejected`. |
| `assessmentFailed` | Syncing or assessing the Version failed. |
| `pending` | An unversioned commit is waiting for an older commit to be assessed, for a version assignment, or for its first assessment. |

Installable Versions stay in `versions`. A blocked Version is listed in `versions` with `blocked: true` when its parent ref is synced. Otherwise it appears only in `unavailableCandidates`. Entries are sorted by version, then commit, then status.

The CLI uses this field to decide whether an older-version fallback needs `--allow-older-versions`. See [Allow older versions](../reference/cli.md#allow-older-versions).

## Next steps

- [Verify with Agent Hooks](agents-hooks.md): run `opendepot verify` before each agent tool call in Claude Code, Codex, and Copilot.
- [OpenDepot CLI skill](opendepot-cli/SKILL.md): a skill that helps agents use the CLI. A raw copy is at [SKILL.md](https://opendepot.defdev.io/docs/guides/opendepot-cli/SKILL.md).
- [CLI reference](../reference/cli.md): commands, flags, and install targets.
- [agents.v1 protocol](../reference/agents-v1.md): the endpoints the CLI calls.
- [Publishing Agents and Skills](agents-publishing.md): configure a Depot to publish skills and agents.
- [GroupBinding Access Control](groupbinding.md): control which subjects can read skills and agents.
