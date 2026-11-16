# Skill and Agent Checks

Every Skill and Agent Version is checked before it is synced. The checks are deterministic and run in a fixed order. The optional Jev assessment is covered separately in [Jev assessment](agents-jev.md).

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

### Codex TOML agents

A Codex agent is a standalone TOML file, `{name}.toml`, instead of Markdown with frontmatter. The file is the whole definition, and it is copied to `~/.codex/agents/` unchanged. It must set:

- `name` and `description`, both non-empty strings. Missing or invalid values are HIGH findings.
- `developer_instructions`, a non-empty string. This is the agent's body. A missing or blank value is a HIGH finding, opendepot-agentspec-010.

Optional keys are `model`, `model_reasoning_effort`, `model_reasoning_summary`, `model_verbosity`, `features`, and `skills`. Any other top-level key produces a LOW warning. Codex does not read a `tools` key, so `tools` in a TOML agent produces that warning.

A TOML file can only be a Codex agent. Set `platform` to `codex` or leave it unset. Any other platform is an error.

The CRD enum rejects unsupported `platform` values at admission. If an unsupported value reaches the controller anyway, it returns an error and blocks the Version.

!!! warning "Fail closed"
    The Deterministic checks fail closed. A Skill or Agent Version is not synced when any of these occur:

    - The `platform` value is not supported.
    - The frontmatter has a HIGH finding.
    - The Trivy scan errors, prints no output, or prints output that is not a valid JSON report.

    The Version stays unsynced and the reason is recorded in its status.

## Next steps

- [Publishing Agents and Skills](agents-publishing.md): configure the sources these checks run against.
- [Jev assessment](agents-jev.md): the optional AI assessment that runs on each Version.
- [Scanning configuration](../configuration/scanning.md): Trivy and scan policy settings.
