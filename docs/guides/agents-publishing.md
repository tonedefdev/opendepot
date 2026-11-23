# Publishing Agents and Skills

This guide covers how Skills and Agents are published from Git: as Skill and Agent resources in a GitOps workflow or through a Depot. It also covers the agent service, the source configuration, branch sourcing, version bumps, and definition storage. Each Version passes the [deterministic checks](agents-checks.md) and, when enabled, the [Jev assessment](agents-jev.md) before it is served. To install these resources, see [Consuming Agents and Skills](agents.md).

## Agent service

The agent service is disabled by default. Enable it with `agent.enabled: true` in the chart values. The agent deployment runs with these security settings:

- `seccompProfile: RuntimeDefault`
- `runAsNonRoot: true`
- All Linux capabilities dropped (`drop: [ALL]`)
- A CPU limit of `500m` by default

## Publishing Skill and Agent resources with GitOps

A Skill or Agent resource does not require a Depot. Declare it in Git and let your GitOps tool, such as Argo CD, apply it. The `agentSourceConfig` fields are the same ones a Depot entry uses.

A standalone resource does not discover versions. Each entry in `versions` must name a version explicitly, and that version must match an existing Git tag. The `version` controller looks for the tag `v1.0.0` or `1.0.0`, with `tagPrefix` in front when it is set. Only a Depot discovers Git tags for a version range and follows branch commits, so a standalone resource cannot use `ref`.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: code-review
  namespace: opendepot-system
spec:
  agentSourceConfig:
    repoOwner: my-org
    repoUrl: https://github.com/my-org/skills
    path: skills/code-review
  versions:
    - version: 1.0.0
```

For an Agent, set `path` to the agent definition file, such as `agents/code-reviewer.agent.md`.

## Publishing with a Depot

A Depot can also publish skills and agents on your behalf through the `skillConfigs` and `agentConfigs` fields. It creates the Skill and Agent resources for you.

A Depot publishes skills and agents through the `skillConfigs` and `agentConfigs` fields. Each entry references a Git repository that holds a `SKILL.md` file (for skills) or an agent file (for agents). An agent file is accepted as `{name}.agent.md`, the Copilot layout, `{name}.md`, the Claude layout, or `{name}.toml`, the Codex layout. A source directory that contains both files for the same agent is rejected as ambiguous. The agent's `name` in its frontmatter is display metadata. It may be any non-empty string up to 64 characters. The file name is the registry identifier, so the `name` in the Agent configuration must match the file name without the `.agent.md`, `.md`, or `.toml` suffix. For example, `code-reviewer` matches `code-reviewer.agent.md`.

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: team-agents
  namespace: opendepot-system
spec:
  skillConfigs:
    - name: release-notes
      repoOwner: my-org
      repoUrl: https://github.com/my-org/release-notes-skill
      path: skills/release-notes
      versionConstraints: ">= 1.0.0"
      versionHistoryLimit: 5
      storageConfig:
        s3:
          bucket: opendepot-artifacts
          region: us-east-1
  agentConfigs:
    - name: code-reviewer
      repoOwner: my-org
      repoUrl: https://github.com/my-org/code-reviewer-agent
      path: agents/code-reviewer
      versionConstraints: "~> 2.1"
      storageConfig:
        s3:
          bucket: opendepot-artifacts
          region: us-east-1
```

Each entry accepts the same source fields as other OpenDepot sources:

| Field | Purpose |
|-------|---------|
| `name` | The skill or agent name. Defaults to the Skill or Agent resource name when omitted. |
| `repoOwner`, `repoUrl` | The GitHub repository that holds the source. The owner and repository names must match `^[A-Za-z0-9._-]{1,100}$`. |
| `path` | For a skill, the directory that holds `SKILL.md`. For an agent, either the directory that holds the agent definition or the definition file itself, such as `agents/code-reviewer.agent.md`. A file path bundles only that file. |
| `platform` | The agent platform whose frontmatter keys are enforced. One of `agentskills`, `claude`, `copilot`, `codex`, `opencode`, or `pi`. Optional. See [Frontmatter validation](agents-checks.md#frontmatter-validation). |
| `tagPrefix` | A prefix that Git tags must have to be considered versions. The prefix may contain `/`, for example `skills/`. |
| `versionConstraints` | A version constraint applied to the discovered Git tags. Tag mode only. Mutually exclusive with `ref`. |
| `ref` | A Git branch to follow instead of tags. Enables [branch sourcing](#branch-sourcing). Mutually exclusive with `versionConstraints` and `tagPrefix`. |
| `branchPolicy` | Branch-mode thresholds and the minimum interval between Versions. Requires `ref`. See [Version bump classification](#version-bump-classification). |
| `versionHistoryLimit` | How many synced versions are kept. |
| `immutable` | Prevents a published version from being changed. |
| `storageConfig` | Where the packaged archive, checksums, and signature are stored. |
| `githubClientConfig` | A GitHub App credential reference for private repositories. |
| `jevSecretRef` and `jevPolicy` | Optional TypeSafe Jev assessment. See [Jev assessment](agents-jev.md). |

GitHub inputs are validated before any API request is made. `repoOwner` and the repository name must match `^[A-Za-z0-9._-]{1,100}$`. A Git ref, including a tag, is checked one `/`-separated segment at a time. Each segment must match `^[A-Za-z0-9._+-]{1,200}$`. Empty segments and the `.` and `..` segments are rejected, and so are characters such as `?`, `#`, `%`, `@`, and `\`. If a tag fails these rules, the archive fetch fails and the Version is not synced.

The `version` controller packages each matching Git tag into an archive, signs the `SHA256SUMS` file with the configured GPG key, and runs the deterministic checks before a Version is marked synced. The `server` exposes the results through the agents.v1 protocol. For the endpoints, see [agents.v1 protocol](../reference/agents-v1.md). For install steps, see the [CLI reference](../reference/cli.md).

The signing key is configured through the `version.gpg.secretName` Secret, which must hold `OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64` with an unprotected (no passphrase) armored private key. The `server` reads the same variable from the `server.gpg` Secret and derives the fingerprint from it. The `version.gpg` and `server.gpg` Secrets must hold the same key. If the derived fingerprint does not match the one recorded on the Version, the `server` returns HTTP 500 and refuses to serve signing material.

## Branch sourcing

Set `ref` on a skill or agent source to follow a Git branch instead of publishing from tags. Only a Depot discovers branch commits, so set `ref` in `skillConfigs` or `agentConfigs`. A standalone Skill or Agent resource cannot follow a branch. Branch sourcing is off by default. It is enabled only when the chart is installed with `branchSourcing.enabled: true`, which sets `--branch-sourcing-enabled` on the depot and version controllers.

```yaml
skillConfigs:
  - name: release-notes
    repoOwner: my-org
    repoUrl: https://github.com/my-org/release-notes-skill
    path: skills/release-notes
    ref: main
    branchPolicy:
      minVersionInterval: 10m
    storageConfig:
      s3:
        bucket: opendepot-artifacts
        region: us-east-1
```

`ref` and `versionConstraints` are mutually exclusive, and so are `ref` and `tagPrefix`. `branchPolicy` applies only to branch sources and requires `ref`.

Tags are usually more immutable than branches, so tag mode is the recommended default for stable content. A branch head moves with every push. Each Version is still pinned to its exact `sourceCommit`, so consumers always receive the content of a specific commit.

- **Pushing publishes.** A push to the branch publishes a new Version once all checks pass. Protect the branch with required reviews and status checks, because anyone who can push to it can publish.
- **Each new commit is a new Version.** A Version is named `<prefix>-<configName>-<40-char SHA>` and is labeled `opendepot.defdev.io/commit`. It stays Pending until the version controller assigns it a semver after signing. A source has at most one Pending entry at a time.
- **Polling is opt-in.** A branch source picks up new commits on a schedule only when `pollingIntervalMinutes` is set on the Depot. When it is unset, a branch source picks up new commits only when the Depot reconciles for another reason, such as a spec change, an annotation change, or a resync. See [Polling interval](depot.md#options).
- **`immutable` works on branch sources.** Each Version is pinned to its `sourceCommit`, and a re-download must match its recorded `status.checksum`. A moving branch creates new Versions and does not change existing ones. The checksum is stable only while the archive format is stable, so a packaging change can alter it for the same SHA, the same as with tags.
- **Recreating a source resets numbering.** Deleting and recreating a source starts semver numbering again. Consumer lock hashes recorded against the old numbering then fail closed.

Branch sources are intended as a one-time migration. After the migration, manage the source definitions through GitOps.

## Version bump classification

In branch mode, the version controller chooses the semver bump for each new commit. The deterministic checks and Trivy scans run first. Jev runs only after they pass. The 43 KiB state cap also applies to bump classification, so a long diff can be truncated before Jev sees it.

- **A flagged entry file means MAJOR.** When the entry file is flagged, the bump is MAJOR with source `Withheld`, and Jev is not called. A flagged file that is not the entry file is shown as `<path> (content withheld)`.
- **Thresholds set the bump.** The Jev probabilities are compared against `branchPolicy`:
    - MAJOR when the major-change probability is at least `majorThreshold`.
    - MINOR when the major-change probability plus the minor-change probability is at least `minorThreshold`.
    - PATCH otherwise.
- **Low confidence means review.** A Version whose confidence is below `minConfidence` is marked `needsReview`. This is informational only and never blocks a Version.

When Jev is not used for a bump, the result is PATCH with source `Default`:

- No `jevSecretRef` is set on the source.
- `scanning.jev.enabled` is `false`, which sets `--jev-enabled=false`.

When Jev is enabled and a call used to classify a bump for a later commit fails, the Version is Held and is not published as PATCH. The version controller retries with a capped backoff of 1, 2, 4, 8, and then 15 minutes. Bump calls share the limit of 16 in-flight Jev requests and the rate limit of 1,000 per minute with Jev assessments across the controller.

Set the `opendepot.defdev.io/bump-override` annotation on a Version to `major`, `minor`, or `patch` to override the bump size. The override is recorded with source `Manual`. It is honored only while the Version is Pending or Held. It sets the bump size only and does not decide whether the Version is published. It does not release a hold caused by a missing `jevSecretRef`; set `jevSecretRef` on the source to publish.

## Definition storage

Each Skill and Agent Version stores its definition file in a ConfigMap of its own. The ConfigMap is referenced by `status.definitionConfigMapRef` and is owned by the Version, so it is deleted with the Version. The UI renders the file as `SKILL.md` for skills and `AGENT.md` for agents. Module READMEs use a separate reference, `readmeConfigMapRef`, which applies only to modules. Skill and Agent Versions never fill README content.

The definition is stored base64 encoded. The encoded data must be at most 1,000,000 bytes, which keeps it under the 1 MiB ConfigMap limit. A larger definition is not synced. The Version gets `synced: false` and a `syncStatus` message that gives the file name, its size, the encoded size, and the limit, for example:

```text
definition exceeds the ConfigMap size limit: definition file SKILL.md is 800000 bytes, which encodes to 1066668 bytes against a 1000000 byte limit
```

The Version stays unsynced until the definition changes.

## Known limitations

- **Name collisions.** Version names use a `prefix-name-version` pattern without escaping. A Skill and an Agent that produce the same Version name can collide. A collision returns `404` and never serves the wrong object.

## Next steps

- [Skill and Agent checks](agents-checks.md): the checks that run before a Version is synced.
- [Jev assessment](agents-jev.md): optional AI assessment of each Version.
- [Assembly Line](assembly-line.md): automate Depot publishing.
- [GroupBinding Access Control](groupbinding.md): control which subjects can read skills and agents.
