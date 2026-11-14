# agents.v1 Protocol

The `server` exposes skills and agents under the agents.v1 protocol at `/opendepot/agents/v1/`. `{kind}` is `skills` or `agents`.

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/versions` | List available versions. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/download` | Return the archive location and checksums. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/archive` | Download the packaged archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS` | Return the checksums for the archive. |
| GET | `/opendepot/agents/v1/{namespace}/{kind}/{name}/{version}/SHA256SUMS.sig` | Return the GPG signature for `SHA256SUMS`. |
| GET | `/opendepot/agents/v1/catalog` | List the skills and agents the caller can see. |

When OIDC is enabled, GroupBinding authorization gates the artifact handlers. These include the agent and skill metadata, the versions, `SHA256SUMS`, `SHA256SUMS.sig`, and archive requests. The `skillResources` and `agentResources` fields of a GroupBinding control which skills and agents a group can read. A denied request returns `403 Forbidden`.

The archive endpoint returns a `302` redirect to the checksum-keyed public route `/opendepot/modules/v1/download/...`. This is the same design that modules and providers use. The redirect URL is a checksum-bound capability. Revoking GroupBinding access does not invalidate a redirect URL that was already issued. This is an accepted, shared design limitation. The `302` response is sent with `Cache-Control: private, no-store`, so shared caches must not store it.

Archive extraction enforces a decompressed-size budget of 2 GiB and a cap of 10,000 entries. An archive that exceeds either limit fails. Symbolic links and hard links are refused. The total decompressed stream, including tar headers, is capped at the 2 GiB budget plus a 64 MiB allowance for headers. The CLI verifies the `SHA256SUMS` signature against the pinned signing key before it installs anything.

## Catalog

`GET /opendepot/agents/v1/catalog` lists the skills and agents the caller can see. Each item is one skill or agent, not one version. The `opendepot list` command uses this endpoint.

| Parameter | Default | Rules |
|-----------|---------|-------|
| `namespace` | None | Limits the results to one namespace. Must be a valid DNS label. |
| `page` | `1` | Must be `1` or greater. |
| `page_size` | `20` | Must be from `1` to `100`. |

An invalid parameter returns `400`. A page past the end returns `200` with an empty `items` array. The response has `items`, `totalCount`, `page`, and `pageSize`. Items are sorted by namespace, kind, and name. If the server cannot list the resources, it returns `500` and no partial catalog.

Each item has `kind` (`skill` or `agent`), `namespace`, `name`, `status`, `latestVersion`, `riskLevel`, `riskScore`, and `unavailableCandidate`. `latestVersion` is omitted when no version is installable. `unavailableCandidate` is the newest unavailable candidate, in the same shape as the [unavailableCandidates](../guides/agents.md#unavailable-candidates) entries. It is omitted when there is none.

| Status | Meaning |
|--------|---------|
| `blocked` | The newest unavailable candidate is blocked. This applies even when an older version is installable. |
| `rejected` | The newest unavailable candidate is rejected. This applies even when an older version is installable. |
| `available` | A latest installable version exists, and the newest candidate, if any, is neither blocked nor rejected. |
| `assessmentFailed` | No version is installable, and the newest candidate failed assessment. |
| `pending` | No version is installable, and the newest candidate is pending. Also used when the resource has no versions. |

An `assessmentFailed` or `pending` candidate does not change an `available` status. `riskLevel` and `riskScore` describe `latestVersion` only. They are omitted when the risk is unknown. An omitted risk field means unknown, not safe.

Auth follows the same policy as the UI browse endpoints. With anonymous auth enabled, every resource is visible. With OIDC not configured, only public resources are listed. With OIDC configured, a missing or invalid Bearer token returns `401`. A valid token shows public resources and the resources its GroupBindings allow. Unlike the artifact endpoints above, the catalog does not return `403`.

## Known limitations

- **Name collisions.** Version names use a `prefix-name-version` pattern without escaping. A Skill and an Agent that produce the same Version name can collide. A collision returns `404` and never serves the wrong object.

## Next steps

- [CLI reference](cli.md): install skills and agents with `opendepot apply`.
- [Consuming Agents and Skills](../guides/agents.md): choose which Versions a project can install.
