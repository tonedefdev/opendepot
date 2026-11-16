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

The archive endpoint authorizes the request before it serves the archive. For a private agent, the request needs a bearer token and a GroupBinding that allows the agent. A request without a token returns `401 Unauthorized`. With `server.anonymousAuth` enabled, the archive downloads without a token.

When presign is enabled for the agent source, the archive endpoint returns a `307 Temporary Redirect` to a presigned URL from the storage backend. The response has `Cache-Control: private, no-store`. The URL TTL defaults to `15m`. Revoking a GroupBinding stops new redirects. A URL that was already issued stays valid until it expires. This is an accepted window.

If presign fails and `fallbackToProxy` is `false`, the endpoint returns `500 Internal Server Error` and does not redirect. This applies to a checksum mismatch too. If `fallbackToProxy` is `true`, the server streams the archive bytes instead. The FileSystem backend always fails the presign step, so it streams only when fallback is on. `fallbackToProxy` defaults to `false` for new Agent objects. Existing Agent objects keep the value they were stored with. See [Pre-signed URL Redirects](../storage/presigned-urls.md#agent-archives).

Archive extraction enforces a decompressed-size budget of 2 GiB and a cap of 10,000 entries. An archive that exceeds either limit fails. Symbolic links and hard links are refused. The total decompressed stream, including tar headers, is capped at the 2 GiB budget plus a 64 MiB allowance for headers. The CLI verifies the `SHA256SUMS` signature against the pinned signing key before it installs anything.

## Catalog

`GET /opendepot/agents/v1/catalog` lists the skills and agents the caller can see. Each item is one skill or agent, not one version. The `opendepot list` command uses this endpoint.

| Parameter | Default | Rules |
|-----------|---------|-------|
| `namespace` | None | Limits the results to one namespace. Must be a valid DNS label. |
| `page` | `1` | Must be `1` or greater. |
| `page_size` | `20` | Must be from `1` to `100`. |

An invalid parameter returns `400`. A page past the end returns `200` with an empty `items` array. The response has `items`, `totalCount`, `page`, and `pageSize`. Items are sorted by namespace, kind, and name. If the server cannot list the resources, it returns `500` and no partial catalog.

Each item has `kind` (`skill` or `agent`), `namespace`, `name`, `status`, `latestVersion`, `riskLevel`, `riskScore`, `unavailableCandidate`, and `description`. `latestVersion` is omitted when no version is installable. `unavailableCandidate` is the newest unavailable candidate, in the same shape as the [unavailableCandidates](../guides/agents.md#unavailable-candidates) entries. It is omitted when there is none. `description` is omitted when the latest installable version has none. See [Descriptions](#descriptions).

| Status | Meaning |
|--------|---------|
| `blocked` | The newest unavailable candidate is blocked. This applies even when an older version is installable. |
| `rejected` | The newest unavailable candidate is rejected. This applies even when an older version is installable. |
| `available` | A latest installable version exists, and the newest candidate, if any, is neither blocked nor rejected. |
| `assessmentFailed` | No version is installable, and the newest candidate failed assessment. |
| `pending` | No version is installable, and the newest candidate is pending. Also used when the resource has no versions. |

An `assessmentFailed` or `pending` candidate does not change an `available` status. `riskLevel` and `riskScore` describe `latestVersion` only. They are omitted when the risk is unknown. An omitted risk field means unknown, not safe.

Auth follows the same policy as the UI browse endpoints. With anonymous auth enabled, every resource is visible. With OIDC not configured, only public resources are listed. With OIDC configured, a missing or invalid Bearer token returns `401`. A valid token shows public resources and the resources its GroupBindings allow. Unlike the artifact endpoints above, the catalog does not return `403`.

### Descriptions

Each item can include `description`. It is the `description` from the frontmatter of the latest installable version, the same version named by `latestVersion`. The field is omitted when that version has no description, and when no version is installable. It is author-declared text and is untrusted. Clients must not render it as markup, and must treat it as plain text. The parser limits descriptions to 1024 bytes. Versions synced before the description was recorded have no `description` until they are synced again.

### Detail fields for describe

`opendepot describe skill` and `opendepot describe agent` read the UI browse detail endpoint, `GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}`, not the catalog. The table and `--json` output show the same fields, under the same visibility rules. The detail includes `latestVersion`, `synced`, `syncStatus`, `hasUnsyncedVersions`, `public`, `blocked`, `blockReasons`, `jevRiskLevel`, `sourceRef`, `sourceCommit`, `sourcePath`, `repoUrl`, `repoOwner`, `scanCounts`, `lastScanned`, `totalDownloads`, `lastDownloadedAt`, `definitionFileName`, `definitionContent`, `agentMetadata` (`name`, `description`, `tools`, `model`), `storageConfig` (`backend`, `bucket`, `region`, `key`, `directoryPath`, `accountName`, `accountUrl`, `subscriptionID`, `resourceGroup`, `presignEnabled`, `presignTTL`), `githubConfig` (`useAuthenticatedClient`), and `sourceScanFindings`. Each finding has `vulnerabilityID`, `pkgName`, `installedVersion`, `fixedVersion`, `severity`, `title`, `exempted`, `exemptionReason`, and `exemptedBy`. The table shows the storage, GitHub, and finding fields in rows and writes finding text through the same sanitizer as the rest of the table. Unset storage and GitHub settings are `null` in `--json`, and `sourceScanFindings` is `[]` when there are none. Each entry in `versions` has `name`, `version`, `synced`, `syncStatus`, `branchPhase`, `blocked`, `blockReasons`, `fileName`, `checksum`, `archiveSizeBytes`, `downloadCount`, `lastDownloadedAt`, `lastScanned`, `scanCounts`, `schemaState`, `schemaMessage`, and `schemaAttemptedAt`. The detail also includes `jevAssessment`, `jevExemptionGovernance`, `jevUnblock`, and `jevThresholds`. Policy names in the governance and unblock fields are omitted for OIDC callers. The table omits empty fields, and `definitionContent` appears only in `--json`. `description` is untrusted author text and is shown as plain text.

## Known limitations

- **Name collisions.** Version names use a `prefix-name-version` pattern without escaping. A Skill and an Agent that produce the same Version name can collide. A collision returns `404` and never serves the wrong object.
- **Checksum source.** On S3, the archive checksum check uses the object's SHA-256 checksum metadata. It does not hash the archive bytes.
- **Anonymous download.** On an OIDC-only server, a public agent still needs a bearer token. Anonymous download is available only when `server.anonymousAuth` is enabled.
- **Revocation window.** A revoked GroupBinding does not invalidate a presigned URL that was already issued. The URL stays valid until its TTL expires.

## Next steps

- [CLI reference](cli.md): install skills and agents with `opendepot apply`.
- [Consuming Agents and Skills](../guides/agents.md): choose which Versions a project can install.
