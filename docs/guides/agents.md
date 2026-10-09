# Consuming Agents and Skills

OpenDepot can publish Agent Skills and Agents from Git repositories as versioned Depots. Consumers install them into a project with the `opendepot` CLI, which verifies signatures and hashes before anything is written to disk.

This guide covers how to publish a Depot that serves skill and agent sources, how the optional TypeSafe Jev assessment is configured, and what data leaves your cluster when Jev is enabled.

## Publishing a Depot with skill and agent sources

A Depot publishes skills and agents through the `skillConfigs` and `agentConfigs` fields. Each entry references a Git repository that holds a `SKILL.md` file (for skills) or an agent file (for agents). An agent file is accepted as either `{name}.agent.md`, the Copilot layout, or `{name}.md`, the Claude layout. A source directory that contains both files for the same agent is rejected as ambiguous. The agent's `name` in its frontmatter is display metadata. It may be any non-empty string up to 64 characters. The file name is the registry identifier, so the `name` in the Agent configuration must match the file name without the `.agent.md` or `.md` suffix. For example, `code-reviewer` matches `code-reviewer.agent.md`.

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
| `path` | The directory inside the repository that holds the skill or agent. |
| `platform` | The agent platform whose frontmatter keys are enforced. One of `agentskills`, `claude`, `copilot`, `codex`, `opencode`, or `pi`. Optional. See [Frontmatter validation](#frontmatter-validation). |
| `tagPrefix` | A prefix that Git tags must have to be considered versions. The prefix may contain `/`, for example `skills/`. |
| `versionConstraints` | A version constraint applied to the discovered Git tags. |
| `versionHistoryLimit` | How many synced versions are kept. |
| `immutable` | Prevents a published version from being changed. |
| `storageConfig` | Where the packaged archive, checksums, and signature are stored. |
| `githubClientConfig` | A GitHub App credential reference for private repositories. |
| `jevSecretRef` and `jevPolicy` | Optional TypeSafe Jev assessment. See [Jev assessment](#jev-assessment). |

GitHub inputs are validated before any API request is made. `repoOwner` and the repository name must match `^[A-Za-z0-9._-]{1,100}$`. A Git ref, including a tag, is checked one `/`-separated segment at a time. Each segment must match `^[A-Za-z0-9._+-]{1,200}$`. Empty segments and the `.` and `..` segments are rejected, and so are characters such as `?`, `#`, `%`, `@`, and `\`. If a tag fails these rules, the archive fetch fails and the Version is not synced.

The `version` controller packages each matching Git tag into an archive, signs the `SHA256SUMS` file with the configured GPG key, and runs the deterministic checks before a Version is marked synced. The `server` exposes the results through the agents.v1 protocol. For the endpoints, see [agents.v1 protocol](#agentsv1-protocol). For install steps, see the [CLI reference](../reference/cli.md).

The signing key is configured through the `version.gpg.secretName` Secret, which must hold `OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64` with an unprotected (no passphrase) armored private key. The `server` reads the same variable from the `server.gpg` Secret and derives the fingerprint from it. The `version.gpg` and `server.gpg` Secrets must hold the same key. If the derived fingerprint does not match the one recorded on the Version, the `server` returns HTTP 500 and refuses to serve signing material.

## Deterministic checks

Every skill and agent Version is checked before it is synced. Checks run in this order.

1. **Frontmatter validation.** The `SKILL.md` or agent file is parsed and its frontmatter is checked against the platform set in `platform`. HIGH findings block the Version. See [Frontmatter validation](#frontmatter-validation).

2. **Expr rules.** The rules in `services/version/checks/agents/rules.yaml` evaluate against the parsed agent. Each rule has a severity and a title. The shipped rules flag:

    | Rule ID | Severity | What it flags |
    |---------|----------|---------------|
    | `opendepot-expr-wildcard-tools` | HIGH | Grants every tool with `*`. |
    | `opendepot-expr-unscoped-shell` | HIGH | Grants an unscoped shell or terminal tool. |
    | `opendepot-expr-remote-exec` | HIGH | Pipes `curl` or `wget` output into a shell. |
    | `opendepot-expr-fetch-write` | MEDIUM | Combines a network fetch tool with a file write tool. |
    | `opendepot-expr-missing-description` | LOW | Has no description. |
    | `opendepot-expr-domain-allowlist` | MEDIUM | References a domain outside `agentAllowedDomains`. |
    | `opendepot-expr-unpinned-install` | MEDIUM | Installs a package without a pinned version. |

3. **Trivy scan.** Trivy runs `fs` against the packaged source with the `vuln`, `secret`, and `misconfig` scanners. Trivy scanning of agents is always on. A Trivy run that errors, prints no output, or prints output that is not a valid JSON report blocks the Version.

4. **Allowed domains.** `scanning.agentAllowedDomains` is a comma-separated list of domains. Entries and request hosts are normalized: they are lowercased, and the port and userinfo are removed. Any domain a skill or agent references must appear in the list. The default is empty, which disables this rule.

`scanning.agentScanning` (default `true`) controls whether the `version` controller syncs skill and agent Versions at all. When it is `false`, unsynced agent Versions are blocked with the message `Agent scanning is disabled`. Versions that are already synced keep their status. `agentScanning` does not enable the offline Trivy vulnerability database cache. That cache requires `scanning.providerScanning: true`.

### Frontmatter validation

The frontmatter of the entry file is checked against the key set of the platform in `platform`. Keys outside that set produce a LOW warning. Warnings never block a Version.

- **Unset.** Keys are checked against the union of all platform key sets. A key that any platform uses produces no warning.
- **Set.** Only the selected platform's keys are accepted. Any other top-level key produces `opendepot-agentspec-009` with the platform named in the message.

HIGH findings always block the Version, regardless of `ScanPolicy`. Examples are a missing or invalid `name`, a missing `description`, and an invalid `tools` or `model` value. For skills, a `name` that differs from the directory name is also a HIGH finding. Agent `name` values are display metadata and are not matched to the file name.

The shared base key set for skills is `name`, `description`, `license`, `compatibility`, `metadata`, and `allowed-tools`. Agents add `tools` and `model` to that base. Each platform adds keys as shown below. Claude agents use camelCase keys.

| Platform | Skill keys beyond the base | Agent keys beyond the base |
|----------|----------------------------|----------------------------|
| `agentskills` | None | None |
| `claude` | `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `disallowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, `shell` | `disallowedTools`, `permissionMode`, `mcpServers`, `hooks`, `maxTurns`, `skills`, `initialPrompt`, `memory`, `effort`, `background`, `omitClaudeMd`, `isolation`, `color` |
| `copilot` | `argument-hint` | `target`, `disable-model-invocation`, `user-invocable`, `mcp-servers`, `argument-hint`, `handoffs` |
| `codex` | None | None |
| `opencode` | None | Uses its own set instead of the base. Accepts only `name`, `description`, `mode`, `model`, `temperature`, `top_p`, `steps`, `disable`, `prompt`, `tools`, `permission`, `hidden`, and `color`. |
| `pi` | `disable-model-invocation` | None |

The OpenCode agent key set is inferred and has not been verified against OpenCode vendor documentation. The table describes the scanner's current behavior.

The CRD enum rejects unsupported `platform` values at admission. If an unsupported value reaches the controller anyway, it returns an error and blocks the Version.

!!! warning "Fail closed"
    The Deterministic checks fail closed. A Skill or Agent Version is not synced when any of these occur:

    - The `platform` value is not supported.
    - The frontmatter has a HIGH finding.
    - The Trivy scan errors, prints no output, or prints output that is not a valid JSON report.

    The Version stays unsynced and the reason is recorded in its status.

## Jev assessment

Jev is the TypeSafe AI assessment. It is optional and off by default. The chart sets `scanning.jev.enabled: false`. When it is enabled, the `version` controller asks Jev a fixed set of questions about each Skill or Agent Version and records the answers in `status.jevAssessment`. Jev output is advisory. It never bypasses signature or hash checks. The CLI still verifies the `SHA256SUMS` signature and the hashes before it installs anything.

### Enabling Jev

1. Enable Jev on the `version` controller:

    ```bash
    helm upgrade opendepot chart/opendepot \
      --set scanning.jev.enabled=true
    ```

    The `scanning.jev.endpoint` value defaults to `https://api.typesafe.ai/v1/systemone`. Override it only if TypeSafe gives you a different endpoint. The endpoint must use `https://`. Plain `http://` is accepted only for loopback addresses (`localhost`, `127.0.0.1`, or `::1`), which is useful for local development.

2. Create a Secret in the Depot namespace that holds your Jev token under the `jevToken` key. Use `kubectl create secret` so the token does not appear in shell history or in a manifest:

    ```bash
    kubectl create secret generic jev-token \
      --namespace opendepot-system \
      --from-file=jevToken=./jev-token.txt
    ```

3. Reference the Secret and a policy from the source. `jevSecretRef` supplies the Secret name, and the key defaults to `jevToken` when omitted:

    ```yaml
    agentConfigs:
      - name: code-reviewer
        repoOwner: my-org
        repoUrl: https://github.com/my-org/code-reviewer-agent
        jevSecretRef:
          name: jev-token
        jevPolicy:
          maxInjectionProbability: 0.5
          maxExfiltrationProbability: 0.5
          minConfidence: 0.7
    ```

### Questions asked

Each Version is assessed with these questions. The `skill` or `agent` word is filled in from the Version type.

| Question | Answer used |
|----------|-------------|
| Is this skill or agent safe to use? | `safeProbability` |
| Is this skill or agent susceptible to prompt injection? | `injectionProbability` |
| Does it direct the agent to send data, files, or credentials to an external destination? | `exfiltrationProbability` |
| Does it direct the agent to take destructive or irreversible actions without asking the user first? | `destructiveProbability` |
| Does it contain hidden or obfuscated instructions? | `hiddenInstructionsProbability` |
| Does it ask the agent to do things beyond what its description says? | `scopeMismatchProbability` |
| Does it direct the agent to download and run remote code or scripts, or install unpinned dependencies? | `remoteExecutionProbability` |
| How much harm could it cause if an AI agent follows it? | `riskScore`, `riskLevel`, and `riskConfidence` |

### Thresholds

The `jevPolicy` thresholds decide whether a Version is blocked. Each threshold is optional.

| Threshold | Blocks when |
|-----------|-------------|
| `minSafeProbability` | The safe probability is below the value. |
| `maxInjectionProbability` | The injection probability is above the value. |
| `maxExfiltrationProbability` | The exfiltration probability is above the value. |
| `maxDestructiveProbability` | The destructive probability is above the value. |
| `maxHiddenInstructionsProbability` | The hidden instructions probability is above the value. |
| `maxScopeMismatchProbability` | The scope mismatch probability is above the value. |
| `maxRemoteExecutionProbability` | The remote execution probability is above the value. |
| `maxRiskScore` | The risk score is above the value. |
| `minConfidence` | Never blocks. A Version whose `riskConfidence` is below the value is marked `needsReview`. |

The gate follows three rules:

- **Unset means no block.** A threshold that is not set never blocks a Version. A policy with no thresholds makes the assessment advisory.
- **Confidence never blocks.** `minConfidence` only sets `needsReview`.
- **Fail closed only with thresholds.** If thresholds are set and Jev cannot be reached or the token cannot be read, the Version stays unsynced and the error is recorded. If no thresholds are set, the failure is recorded in `status.jevAssessment.error` and the Version can still sync.

Assessments are cached in `status.jevAssessment`. Set `forceSync: true` on the Skill or Agent to run a new assessment.

## Data handling

When Jev is enabled and a source sets a `jevPolicy` with a `jevSecretRef`, the controller sends a single request to the Jev endpoint with this content:

- **Sent:** the entry file (`SKILL.md` or the agent `.md` file) and the list of file names bundled with the Version. The state is capped at 64 KiB.
- **Not sent:** scripts and bundled file contents. Only file names are included in the list.
- **Withheld:** if the Trivy secret scanner flags the entry file, its content is omitted from the request so that a detected secret is never sent.
- **Never sent:** the Jev token. The token is only used as the request credential and is never logged or written to a resource.

Before you use Jev on private repositories, get the TypeSafe retention and training terms in writing. Confirm how long TypeSafe keeps submitted content and whether it is used for model training. Do not enable Jev for private source until those terms are confirmed.

## Browse view

The UI browse view shows the sync status of each Version. It also shows blocked and unsynced Versions, and the scan and Jev findings for them. Anyone who can see the parent Agent or Skill can see this data. Browse is filtered per Agent through the caller's GroupBinding. This is intentional, so operators can see why a Version was blocked.

## Yanked and blocked Versions

Yank a Version by setting `spec.yanked: true` on the Version resource. Yanked Versions stay listed and downloadable, and the listing includes `yanked: true`. The CLI skips yanked Versions when it resolves a version constraint unless you pass `--allow-yanked`.

A Version that a Jev threshold blocks is never listed or served. A direct request for it returns `404`.

## agents.v1 protocol

The `server` exposes skills and agents under the agents.v1 protocol at `/opendepot/agents/v1/`. `{kind}` is `skills` or `agents`.

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/versions` | List available versions. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/download` | Return the archive location and checksums. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/archive` | Download the packaged archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS` | Return the checksums for the archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS.sig` | Return the GPG signature for `SHA256SUMS`. |

When OIDC is enabled, GroupBinding authorization gates the artifact handlers. These include the agent and skill metadata, the versions, `SHA256SUMS`, `SHA256SUMS.sig`, and archive requests. The `skillResources` and `agentResources` fields of a GroupBinding control which skills and agents a group can read. A denied request returns `403 Forbidden`.

The archive endpoint returns a `302` redirect to the checksum-keyed public route `/opendepot/modules/v1/download/...`. This is the same design that modules and providers use. The redirect URL is a checksum-bound capability. Revoking GroupBinding access does not invalidate a redirect URL that was already issued. This is an accepted, shared design limitation. The `302` response is sent with `Cache-Control: private, no-store`, so shared caches must not store it.

Archive extraction enforces a decompressed-size budget of 2 GiB and a cap of 10,000 entries. An archive that exceeds either limit fails. The CLI verifies the `SHA256SUMS` signature against the pinned signing key before it installs anything.

## Agent service

The agent service is disabled by default. Enable it with `agent.enabled: true` in the chart values. The agent deployment runs with these security settings:

- `seccompProfile: RuntimeDefault`
- `runAsNonRoot: true`
- All Linux capabilities dropped (`drop: [ALL]`)
- A CPU limit of `500m` by default

## Known limitations

- **Name collisions.** Version names use a `prefix-name-version` pattern without escaping. A Skill and an Agent that produce the same Version name can collide. A collision returns `404` and never serves the wrong object.
- **Unmaintained OpenPGP library.** The agent and provider signing paths use `golang.org/x/crypto/openpgp`, which is unmaintained. The advisory GO-2026-5932 has no fixed version. This is an accepted known risk.

## Next steps

- [CLI reference](../reference/cli.md): install skills and agents into a project with `opendepot apply`.
- [Assembly Line](assembly-line.md): automate Depot publishing.
- [GroupBinding Access Control](groupbinding.md): control which subjects can read skills and agents.
