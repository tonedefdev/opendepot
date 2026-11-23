---
tags:
  - reference
  - api
---

# API Reference

For breaking changes between versions, see [Upgrading](../upgrading.md).

## Service Discovery

```
GET /.well-known/terraform.json
```

**Response:**

```json
{
  "modules.v1": "/opendepot/modules/v1/",
  "providers.v1": "/opendepot/providers/v1/",
  "login.v1": {
    "client": "opentofu-cli",
    "grant_types": ["authz_code", "device_code"],
    "authz": "https://opendepot.example.com/dex/auth",
    "token": "https://opendepot.example.com/dex/token",
    "ports": [10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008, 10009, 10010]
  }
}
```

The `authz` and `token` URLs above assume the recommended [server-proxied Dex](../configuration/oidc/proxy.md#proxy-dex-through-the-server) setup, so they share the same host as the rest of the registry API. Without the proxy, they point at wherever Dex itself is exposed.

The `login.v1` field is only present when OIDC authentication is enabled. It advertises the OIDC endpoints and ports that the `tofu login` command uses to obtain a JWT.

## OIDC Device Authorization

```
POST {login.v1.authz}
```

Initiates the OAuth 2.0 device authorization grant flow. Used by `tofu login` on headless systems (servers, CI runners) to obtain a device code and user code.

**Query Parameters:**

| Parameter | Description |
|-----------|-------------|
| `client_id` | From `login.v1.client` |
| `scope` | Space-separated scopes (e.g. `openid profile email groups`) |

**Response:**

```json
{
  "device_code": "AQABAGF...",
  "user_code": "WXYZ-ABCD",
  "verification_uri": "https://opendepot.example.com/dex/device",
  "expires_in": 300,
  "interval": 5
}
```

## OIDC Token Exchange

```
POST {login.v1.token}
```

Exchanges an authorization code (authz_code flow) or device code (device_code flow) for a JWT. Used by `tofu login` to obtain the bearer token returned to the client.

**Request (authz_code grant):**

```
POST https://opendepot.example.com/dex/token
grant_type=authorization_code&code=<AUTH_CODE>&client_id=<CLIENT_ID>&redirect_uri=http://localhost:10000/login
```

**Request (device_code grant):**

```
POST https://opendepot.example.com/dex/token
grant_type=urn:ietf:params:oauth:grant-type:device_code&device_code=<DEVICE_CODE>&client_id=<CLIENT_ID>
```

**Response:**

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "bearer",
  "expires_in": 3600,
  "id_token": "eyJhbGciOiJSUzI1NiIs..."
}
```

The `id_token` (JWT) is returned to `tofu login` and subsequently passed to OpenDepot as a bearer token in the `Authorization: Bearer <id_token>` header.

## Authentication Modes

All endpoints that require authentication accept the `Authorization: Bearer <token>` header. The server operates in one of the following modes, configured at startup:

| Mode | Value | Token accepted | K8s calls use |
|---|---|---|---|
| Anonymous | `--anonymous-auth` | None | Server SA |
| Kubeconfig | (default) | Base64-encoded kubeconfig | Caller's kubeconfig identity |
| Bearer token | `--use-bearer-token` | SA bearer token | Caller's SA RBAC |
| OIDC | `--oidc-issuer-url` + `--oidc-client-id` | Dex JWT | Server SA (GroupBinding controls access) |
| OIDC + SA fallback | above + `--oidc-allow-sa-fallback` | Dex JWT **or** SA token | Dex JWT → Server SA; SA token → Caller's SA RBAC |
| OIDC + client credentials | above + `--oidc-allow-client-credentials` + `--oidc-client-credentials-audiences` | Dex client credentials token whose `aud` is allowlisted | Server SA (GroupBinding matches `client:<sub>`) |

## List Module Versions

```
GET /opendepot/modules/v1/{namespace}/{name}/{system}/versions
```

Returns all available versions of a module. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Module resource |
| `name` | Module name |
| `system` | Provider (e.g., `aws`, `azurerm`) |

## Download Module

```
GET /opendepot/modules/v1/{namespace}/{name}/{system}/{version}/download
```

Returns `204 No Content` with an `X-Terraform-Get` header pointing to the storage-specific download URL. Requires authentication.

## Storage Download Endpoints (Modules)

These endpoints are called by OpenTofu/Terraform after receiving the `X-Terraform-Get` redirect. They validate the SHA256 checksum and stream the module archive.

```
GET /opendepot/modules/v1/download/s3/{bucket}/{region}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/azure/{subID}/{rg}/{account}/{accountUrl}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/gcs/{bucket}/{name}/{fileName}?fileChecksum={checksum}
GET /opendepot/modules/v1/download/fileSystem/{directory}/{name}/{fileName}?fileChecksum={checksum}
```

## List Provider Versions

```
GET /opendepot/providers/v1/{namespace}/{type}/versions
```

Returns all available versions of a provider and the platforms each version supports. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name (e.g., `aws`, `azurerm`) |

**Response:**

```json
{
  "versions": [
    {
      "version": "5.80.0",
      "protocols": ["6.0"],
      "platforms": [
        { "os": "linux", "arch": "amd64" },
        { "os": "linux", "arch": "arm64" }
      ]
    }
  ]
}
```

## Provider Package Metadata

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/download/{os}/{arch}
```

Returns the download URL, SHA256 checksum, and GPG signing key for a specific provider binary. Requires authentication.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `type` | Provider name |
| `version` | Provider version |
| `os` | Operating system (e.g., `linux`, `darwin`) |
| `arch` | CPU architecture (e.g., `amd64`, `arm64`) |

**Response:**

```json
{
  "protocols": ["6.0"],
  "os": "linux",
  "arch": "amd64",
  "filename": "terraform-provider-aws_5.80.0_linux_amd64.zip",
  "download_url": "https://.../opendepot/providers/v1/download/opendepot-system/aws/5.80.0/linux/amd64",
  "shasum": "<hex-sha256>",
  "shasums_url": "https://.../opendepot/providers/v1/opendepot-system/aws/5.80.0/SHA256SUMS/linux/amd64",
  "shasums_signature_url": "https://.../opendepot/providers/v1/opendepot-system/aws/5.80.0/SHA256SUMS.sig/linux/amd64",
  "signing_keys": {
    "gpg_public_keys": [
      {
        "key_id": "<KEY_ID>",
        "ascii_armor": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n..."
      }
    ]
  }
}
```

## Provider Binary Download

```
GET /opendepot/providers/v1/download/{namespace}/{type}/{version}/{os}/{arch}
```

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication — the server uses its own ServiceAccount per the Terraform Provider Registry Protocol, which both OpenTofu and Terraform implement.

## Provider SHA256SUMS

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS/{os}/{arch}
```

Returns the `SHA256SUMS` text file for the specified provider version and platform. Does **not** require client authentication.

## Provider SHA256SUMS Signature

```
GET /opendepot/providers/v1/{namespace}/{type}/{version}/SHA256SUMS.sig/{os}/{arch}
```

Returns the detached GPG signature over the `SHA256SUMS` file, signed with the key configured in `server.gpg.secretName`. Does **not** require client authentication.

## Provider Network Mirror Protocol

OpenDepot implements the [Provider Network Mirror Protocol](https://opentofu.org/docs/internals/provider-network-mirror-protocol/) (shared by both OpenTofu and Terraform) so configurations can reference providers by their canonical upstream identity (e.g., `registry.opentofu.org/hashicorp/aws` or `registry.terraform.io/hashicorp/aws`) while installing from OpenDepot. The mirror URL is namespace-scoped:

```
https://<host>/opendepot/providers/mirror/v1/<namespace>/
```

Clients configure this URL in their `.tofurc` via the `provider_installation.network_mirror` block. See [Consuming Providers](../guides/providers.md) for full usage examples.

### Mirror: List Provider Versions

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/index.json
```

Returns all available versions of a provider from the specified origin registry. Requires authentication when anonymous mode is disabled.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `hostname` | Origin registry hostname (`registry.opentofu.org` or `registry.terraform.io`) |
| `providerNamespace` | Provider namespace at the origin (e.g., `hashicorp`, `datadog`) |
| `type` | Provider name (e.g., `aws`, `azurerm`) |

**Response:**

```json
{
  "versions": {
    "5.80.0": {},
    "5.81.0": {}
  }
}
```

The version keys are normalized semver strings without a leading `v`. The values are empty objects per the Network Mirror Protocol specification.

### Mirror: Provider Version Metadata

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{version}.json
```

Returns archive URLs and checksums for all OS/architecture combinations of a specific provider version. Requires authentication when anonymous mode is disabled.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the Provider resource |
| `hostname` | Origin registry hostname (`registry.opentofu.org` or `registry.terraform.io`) |

**Response:**

```json
{
  "archives": {
    "linux_amd64": {
      "url": "5.80.0/linux/amd64/terraform-provider-aws_5.80.0_linux_amd64.zip",
      "hashes": ["zh:abc123..."]
    },
    "linux_arm64": {
      "url": "5.80.0/linux/arm64/terraform-provider-aws_5.80.0_linux_arm64.zip",
      "hashes": ["zh:def456..."]
    }
  }
}
```

The `url` field is a relative path from the mirror base URL. The `hashes` array uses the `zh:` prefix for packed ZIP checksums as required by the Network Mirror Protocol.

### Mirror: Provider Archive Download

```
GET /opendepot/providers/mirror/v1/{namespace}/{hostname}/{providerNamespace}/{type}/{version}/{os}/{arch}/{filename}
```

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication when using anonymous mirror access — the server uses its own ServiceAccount. Archive URLs are discovered via the version metadata endpoint above.

## Browse API

The browse endpoints power [OpenDepot Workshop](../guides/registry-explorer/index.md) and can also be called directly. All endpoints are accessible without authentication; providing an `Authorization: Bearer <token>` header extends visibility per the [browse visibility rules](../guides/registry-explorer/index.md).

### List Namespaces

```
GET /opendepot/ui/v1/namespaces
```

Returns only namespaces that carry the `opendepot.defdev.io/public=true` label. The label selector is sent directly to the Kubernetes API, so system namespaces (`kube-system`, `default`, `kube-public`) are never included in the response regardless of auth mode. The `public` field is always `true` because unlabeled namespaces are excluded before the response is built.

An admin must label a namespace before it appears in the sidebar:

```bash
kubectl label namespace <ns> opendepot.defdev.io/public=true
```

**Response:**

```json
{
  "items": [
    { "name": "opendepot-system", "public": true }
  ]
}
```

### List Resources

```
GET /opendepot/ui/v1/resources
```

Returns a paginated, filtered list of visible `Module`, `Provider`, `Skill`, and `Agent` resources.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string (repeatable) | Filter by one or more namespaces |
| `kind` | string | `module`, `provider`, `skill`, or `agent`. Omit to return all four kinds. |
| `q` | string | Search string matched against resource name |
| `synced` | bool | Filter to synced (`true`) or unsynced (`false`) resources |
| `os` | string | Filter providers by operating system |
| `arch` | string | Filter providers by CPU architecture |
| `severity` | string | Filter to resources with findings at or above this level (`CRITICAL`, `HIGH`, `MEDIUM`, `LOW`) |
| `public_only` | bool | When `true`, return only publicly-labelled resources |
| `sort_by` | string | Sort field: `name`, `namespace`, `latest_version`, `kind`, `source`, `status`, or `risk` |
| `sort_dir` | string | `asc` or `desc` |
| `page` | int | Page number (1-based) |
| `page_size` | int | Results per page |

**Response:**

```json
{
  "items": [
    {
      "kind": "module",
      "namespace": "opendepot-system",
      "name": "terraform-aws-vpc",
      "latestVersion": "3.19.0",
      "synced": true,
      "provider": "aws",
      "repoUrl": "https://github.com/terraform-aws-modules/terraform-aws-vpc",
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0, "exempted": 0 },
      "public": true,
      "hasUnsyncedVersions": true,
      "totalDownloads": 4821,
      "lastDownloadedAt": "2026-05-25T14:32:00Z"
    }
  ],
  "totalCount": 1,
  "page": 1,
  "pageSize": 20
}
```

`hasUnsyncedVersions` is present and `true` when at least one `Version` CR under the resource has `status.synced: false` or a `status.syncStatus` containing `"failed"` or `"error"` (case-insensitive). The field is omitted from the response when all versions are healthy.

For `skill` and `agent` resources, the item includes `jevRiskLevel` and `blocked` from the Jev assessment of the Version that matches `latestVersion`. For a Skill or Agent with only an unversioned, blocked branch Version, `blocked` and `blockReasons` come from that Version. `blockReasons` is set only when `blocked` is `true`. Fields with no value are omitted.

`scanCounts` reflects vulnerability findings from the **latest version only**. The field is omitted when no version has been scanned. `exempted` counts findings currently exempted by a [`ScanPolicy`](#scanpolicy) and is excluded from the `critical`/`high`/`medium`/`low`/`unknown` counters.

### Resource Detail

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
```

Returns full detail for a single resource including all versions and scan findings. The `sourceScanFindings` and `binaryScanFindings` fields contain findings from the **latest version only**.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module`, `provider`, `skill`, or `agent` |
| `name` | Resource name |

**Response:**

```json
{
  "kind": "module",
  "namespace": "opendepot-system",
  "name": "terraform-aws-vpc",
  "latestVersion": "3.19.0",
  "synced": true,
  "public": true,
  "versions": [
    { "version": "3.19.0", "synced": true },
    { "version": "3.18.0", "synced": true }
  ],
  "sourceScanFindings": [
    {
      "vulnerabilityID": "CVE-2024-12345",
      "pkgName": "some-dep",
      "installedVersion": "1.0.0",
      "fixedVersion": "1.0.1",
      "severity": "HIGH",
      "title": "Example vulnerability"
    }
  ],
  "binaryScanFindings": {
    "linux/amd64": []
  },
  "readmeContent": "# terraform-aws-vpc\n\nTerraform module..."
}
```

`readmeContent` is the decoded (plain markdown) README for the module's latest version. It is only present for `module` resources, and only when a README could be resolved — see [`VersionStatus.readmeConfigMapRef`](#versionstatus-fields). The field is omitted for providers and for modules with no resolvable README.

`skill` and `agent` resources do not return `readmeContent`. They return the definition file of the latest version instead:

- `definitionContent` is the decoded definition file (`SKILL.md` or the agent file). It is read from [`VersionStatus.definitionConfigMapRef`](#versionstatus-fields).
- `definitionFileName` is the source file name, such as `SKILL.md` or `code-reviewer.agent.md`.
- `agentMetadata` is the frontmatter of the latest version. `jevAssessment` is the Jev result for the newest Version that Jev assessed, when one exists. That Version can be blocked or Rejected, so a Skill with no servable Version still shows its Jev result. `jevThresholds` is the source's `jevPolicy`.
- `sourceRef`, `sourceCommit`, and `sourcePath` describe the source. `sourceRef` is set only for branch sources, and `sourceCommit` is set only when `sourceRef` is set. `sourceCommit` is the commit of the latest version.

### List Resource Versions

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
```

Returns a paginated, filtered list of versions for a single resource. Used by the OpenDepot Workshop detail page to populate the versions table. Authentication follows the same rules as the other browse endpoints.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module`, `provider`, `skill`, or `agent` |
| `name` | Resource name |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | int | `1` | Page number (1-based) |
| `page_size` | int | `20` | Items per page (max `100`) |
| `q` | string | — | Case-insensitive substring filter on the version string |
| `synced` | string | — | `true` = healthy versions only, `false` = failed or error versions only; omit for all |
| `os` | string | — | Exact OS filter (case-insensitive); providers only |
| `arch` | string | — | Exact architecture filter (case-insensitive); providers only |

**Response:**

```json
{
  "items": [
    {
      "version": "3.19.0",
      "synced": true,
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0, "exempted": 0 },
      "downloadCount": 1243,
      "lastDownloadedAt": "2026-05-25T14:32:00Z",
      "archiveSizeBytes": 2097152
    }
  ],
  "totalCount": 42,
  "page": 1,
  "pageSize": 20,
  "availableOS": ["darwin", "linux", "windows"],
  "availableArch": ["amd64", "arm64"]
}
```

`availableOS` and `availableArch` are populated from the full (pre-filter) version set so filter dropdowns remain populated while a filter is active. Both fields are omitted for modules; they are only present for providers. Versions are sorted newest-first.

`downloadCount` and `lastDownloadedAt` are omitted when no downloads have been recorded. `archiveSizeBytes` is omitted when `VersionStatus.archiveSizeBytes` has not been set by the version controller.

For `skill` and `agent` resources, the list includes every Version that has a `version`. It also includes branch Versions that Jev blocked or that have the `Rejected` phase, even though they have no `version`. Pending and Held branch Versions without a `version` are not listed. Each item has these additional fields:

| Field | Description |
|-------|-------------|
| `commit` | The commit SHA of the Version. Uses `VersionStatus.sourceCommit` and falls back to `VersionSpec.sourceCommit`. Omitted when neither is set. |
| `branchPhase` | `VersionStatus.branchPhase`: `Pending`, `Held`, `Assigned`, or `Rejected`. Omitted for tag-sourced Versions. |
| `blocked` | `true` when Jev blocked the Version. Omitted otherwise. |
| `blockReasons` | The Jev block reasons. Only set when `blocked` is `true`. |

`fileName`, `checksum`, and `archiveSizeBytes` are omitted for a Skill or Agent Version that is not servable, such as one that is unsynced or blocked.

### Resource Scan Findings

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings
```

Returns scan findings for one resource. `?version=` selects the scanned version. Without it, the most recently scanned version is returned. A leading `v` and surrounding whitespace are stripped, so `v1.2.3`, `1.2.3`, and ` 1.2.3 ` match. Authentication follows the other browse endpoints.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module` or `provider` |
| `name` | Resource name |

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `version` | string | Selects the source scan entry for this exact version. Applies to both modules and providers. Leading `v` and surrounding whitespace are stripped automatically. Omit to return findings from the latest scanned version. |
| `binaryVersion` | string | Providers only. Selects the binary scan entries for this exact version. When omitted, binary findings from the latest scanned version per platform are returned. Leading `v` is stripped automatically. |

**Response (`BrowseScanFindings`):**

```json
{
  "sourceScanFindings": [
    {
      "vulnerabilityID": "CVE-2024-12345",
      "pkgName": "some-dep",
      "installedVersion": "1.0.0",
      "fixedVersion": "1.0.1",
      "severity": "HIGH",
      "title": "Example vulnerability"
    }
  ],
  "binaryScanFindings": {
    "linux/amd64": [],
    "darwin/arm64": []
  },
  "selectedVersion": "3.2.3",
  "scannedVersions": ["3.2.3", "3.2.0"],
  "binaryVersions": ["3.2.3", "3.2.0"]
}
```

`sourceScanFindings` contains IaC (module) or `go.mod` (provider) vulnerability findings. `binaryScanFindings` is a map of `os/arch` → findings; it is only populated for providers. Both fields are omitted when empty.

`selectedVersion` is the version whose source findings are returned. `scannedVersions` lists every version with source scan results, sorted by semver descending. The UI uses it for the source scan version selector. `binaryVersions` is the equivalent list for binary scans and appears only for providers. All three fields are omitted when no scan results exist.

This endpoint is used by the [OpenDepot Workshop](../guides/registry-explorer/module-details.md#scan-findings) refresh button to re-fetch findings without a full page reload.

### Module Contract

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/contract
```

Returns the Assembly Line contract for a module version. The contract is the input schema that the Assembly Line canvas uses to render module inputs. It is derived when the Version is synced. This endpoint is available whether or not Assembly Line is enabled.

Only `module` is supported. Requests for `provider` return `400`.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `version` | string | Exact module version. Omit to use the module's latest version. |

**Responses:**

| Status | Description |
|--------|-------------|
| `200` | The contract JSON, served as `application/json`. |
| `400` | The resource is not a module. |
| `404` | The resource or version was not found, or no contract has been derived for it. An unavailable contract returns `{"error": "contract_unavailable", "message": "...", "version": "..."}`. |

The decompressed payload is capped at 32 MiB.

### Provider Configuration Schema

```
GET /opendepot/ui/v1/resources/{namespace}/provider/{name}/provider-schema
```

Returns the configuration schema for a provider version, extracted from the provider binary. Assembly Line must be enabled with `--assembly-enabled` (`server.assembly.enabled` in the chart). When it is disabled, the route is not registered and the request returns `404`.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `version` | string | Exact provider version. Omit to use the provider's latest version. |

**Response (`providerConfigurationSchemaResponse`):**

```json
{
  "namespace": "opendepot",
  "name": "example",
  "providerNamespace": "hashicorp",
  "providerName": "aws",
  "version": "5.0.0",
  "schemaVersion": "1",
  "configuration": {}
}
```

The decompressed payload is capped at 128 MiB.

### Assembly Line Export

```
POST /opendepot/ui/v1/assembly/export
```

Validates an Assembly Line canvas and packages it as a root module. This endpoint is registered only when Assembly Line is enabled. Authentication follows the browse rules. The export needs a bearer token, so OpenTofu can download the artifacts that the generated module references.

**Request Body (`assembly.export.v1`):**

```json
{
  "schemaVersion": "assembly.export.v1",
  "variables": [],
  "modules": [],
  "providers": []
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schemaVersion` | string | Yes | Must be `assembly.export.v1`. |
| `variables` | array | Yes | Root module input variables. |
| `modules` | array | Yes | Module nodes from the canvas. |
| `providers` | array | No | Provider nodes from the canvas. |

The request body is limited to 2 MiB by default (`--assembly-max-request-bytes`). The total node count across `variables`, `modules`, and `providers` is limited to 100 by default (`--assembly-max-nodes`). Unknown fields and trailing data after the JSON object are rejected.

**Response formats:**

The format depends on the `Accept` header.

- Default: `200 OK` with `application/zip` and `Content-Disposition: attachment; filename="assembly-line.zip"`. The archive is the root module.
- `Accept: application/x-ndjson`: `200 OK` with a stream of newline-delimited JSON events. The stream emits `started`, then zero or more `output` events, then either `complete` or `error`.

| Event `type` | Fields | Description |
|--------------|--------|-------------|
| `started` | `phase` | Validation or packaging began. |
| `output` | `phase`, `output` | Progress output from `tofu init` and validation. |
| `complete` | `archive` | The base64-encoded ZIP archive. |
| `error` | `error` | An error object with `error` and `message` fields. |

**Errors:**

Errors use the `ErrorResponse` shape: `{"error": "<code>", "message": "...", "diagnostics": [], "output": "..."}`. The `diagnostics` and `output` fields appear only when they apply.

| Status | `error` code | Description |
|--------|--------------|-------------|
| `400` | `invalid_request` | The body is not valid JSON, contains unknown fields, or has trailing data. |
| `413` | `too_many_nodes` | The canvas exceeds the node limit. |
| `413` | `request_too_large` | The body exceeds the request size limit. |
| `422` | `invalid_canvas` | The canvas failed validation. `diagnostics` lists the problems. |
| `500` | `internal_error` | The export failed on the server. |
| `500` | `streaming_unavailable` | The client requested NDJSON but the response writer cannot stream. |

### List Depots

```
GET /opendepot/ui/v1/depots
```

Returns a flat list of all visible `Depot` resources with their storage backend, polling interval, and managed resource counts.

**Visibility:** public depots are always included. Non-public depots are included when the server is in anonymous-auth mode or when the caller has a matching `GroupBinding`. An optional `?namespace=` query parameter filters results to a single namespace.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Filter by namespace |

**Response:**

```json
{
  "items": [
    {
      "namespace": "opendepot-system",
      "name": "platform-depot",
      "modules": ["terraform-aws-vpc", "terraform-aws-s3-bucket"],
      "providers": ["aws"],
      "pollingIntervalMinutes": 30,
      "storageBackend": "s3"
    }
  ]
}
```

### Depot Relationship Graph

```
GET /opendepot/ui/v1/depots/graph
```

Returns a graph of all visible `Depot`, `Module`, `Provider`, `Skill`, and `Agent` resources with directed edges connecting each depot to its managed modules, providers, skills, and agents. Each node carries only its `latestVersion`; the Depots page loads each resource's versions from the [Resource Detail](#resource-detail) endpoint to draw version nodes. Used by the [Depots page](../guides/registry-explorer/depots.md) in OpenDepot Workshop to render the interactive relationship diagram.

**Visibility:** same rules as [List Depots](#list-depots).

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Filter the graph to a single namespace |

**Response:**

```json
{
  "depots": [
    {
      "id": "opendepot-system/platform-depot",
      "namespace": "opendepot-system",
      "name": "platform-depot",
      "storageBackend": "s3",
      "pollingIntervalMinutes": 30,
      "managedModuleNames": ["terraform-aws-vpc"],
      "managedProviderNames": ["aws"],
      "managedSkillNames": ["code-review"],
      "managedAgentNames": ["code-review"]
    }
  ],
  "modules": [
    {
      "id": "opendepot-system/module/terraform-aws-vpc",
      "namespace": "opendepot-system",
      "name": "terraform-aws-vpc",
      "provider": "aws",
      "synced": true,
      "latestVersion": "3.19.0",
      "depotID": "opendepot-system/platform-depot",
      "scanCounts": { "critical": 0, "high": 1, "medium": 2, "low": 0, "unknown": 0, "exempted": 0 }
    }
  ],
  "providers": [
    {
      "id": "opendepot-system/provider/aws",
      "namespace": "opendepot-system",
      "name": "aws",
      "synced": true
    }
  ],
  "skills": [
    {
      "id": "opendepot-system/skill/code-review",
      "kind": "skill",
      "namespace": "opendepot-system",
      "name": "code-review",
      "synced": true,
      "latestVersion": "0.9.0"
    }
  ],
  "agents": [
    {
      "id": "opendepot-system/agent/code-review",
      "kind": "agent",
      "namespace": "opendepot-system",
      "name": "code-review",
      "synced": true,
      "latestVersion": "0.9.0"
    }
  ],
  "edges": [
    { "id": "e-depot-mod-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/module/terraform-aws-vpc" },
    { "id": "e-depot-prov-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/provider/aws" },
    { "id": "e-depot-skill-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/skill/code-review" },
    { "id": "e-depot-agent-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/agent/code-review" }
  ],
  "summary": {
    "totalDepots": 1,
    "totalModules": 1,
    "totalProviders": 1,
    "totalSkills": 1,
    "totalAgents": 1
  },
  "generatedAt": "2026-05-25T12:00:00Z"
}
```

### Registry Stats

```
GET /opendepot/ui/v1/stats
```

Returns aggregate registry statistics as JSON. All counts are scoped to the resources visible to the caller (same visibility rules as the other browse endpoints).

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string | Scope statistics to a single namespace. Omit for all accessible namespaces. |

**Response:**

```json
{
  "totalModules": 12,
  "modulesSynced": 11,
  "totalProviders": 3,
  "providersSynced": 3,
  "totalSkills": 14,
  "skillsSynced": 12,
  "totalAgents": 10,
  "agentsSynced": 9,
  "totalVersions": 87,
  "versionsSynced": 82,
  "totalStorageBytes": 5368709120,
  "totalDownloads": 24601,
  "syncHealth": {
    "syncedVersions": 82,
    "unsyncedVersions": 3,
    "failedVersions": 2
  },
  "securityPosture": {
    "critical": 0,
    "high": 4,
    "medium": 11,
    "low": 7,
    "unknown": 1,
    "totalAffectedResources": 6
  },
  "jev": {
    "assessedVersions": 12,
    "blockedVersions": 1,
    "needsReviewVersions": 2,
    "exemptedVersions": 0,
    "failedVersions": 0,
    "assessedChunks": 348,
    "inputTokens": 1893204,
    "riskLevels": {
      "minimal": 5,
      "low": 4,
      "moderate": 2,
      "high": 1,
      "critical": 0
    }
  },
  "storageDistribution": [
    { "backend": "s3", "count": 10 },
    { "backend": "filesystem", "count": 5 }
  ],
  "mostDownloaded": {
    "modules": [
      {
        "namespace": "opendepot-system",
        "kind": "module",
        "name": "terraform-aws-vpc",
        "version": "3.19.0",
        "downloadCount": 4821,
        "lastDownloadedAt": "2026-05-25T14:32:00Z"
      }
    ],
    "providers": [],
    "skills": [],
    "agents": []
  }
}
```

`totalStorageBytes` is the sum of `VersionStatus.archiveSizeBytes` across all visible versions; it is `0` when no archive sizes have been recorded. `totalDownloads` and `mostDownloaded` are queried from Prometheus over the configured lookback window; both are `0` / empty until a matching metric has been scraped. Skill and Agent downloads are included in `totalDownloads`. Download counts are cross-referenced against the caller's visibility set — private resource names do not appear in `mostDownloaded` for unauthenticated callers.

`mostDownloaded` is split by kind so each list ranks only its own resources. Each list holds up to 10 resources, ranked by total downloads across all versions. Each entry reports the resource's most-downloaded version, with ties broken by the higher version. Resources with zero downloads are omitted, so an empty list means no visible downloads for that kind.

## Kubernetes Resource Types

All resources are in the `opendepot.defdev.io/v1alpha1` group. Fields marked *controller-managed* are written by OpenDepot, not by you.

### Module

| Field | Type | Description |
|---|---|---|
| `spec.moduleConfig` | `ModuleConfig` | **Required.** Settings for the module and for each `Version` it creates. |
| `spec.versions` | `[]ModuleVersion` | **Required.** Versions to sync. A leading `v` is trimmed. |
| `spec.forceSync` | `bool` | Forces a re-sync. The controller resets it to `false`. Default `false`. |
| `status.latestVersion` | `string` | *Controller-managed.* Latest available version. |
| `status.synced` | `bool` | *Controller-managed.* Whether the module reached its desired state. |
| `status.syncStatus` | `string` | *Controller-managed.* Reconciliation status. |
| `status.fileName` | `string` | *Controller-managed.* Generated archive file name. |
| `status.moduleVersionRefs` | `map[string]ModuleVersion` | *Controller-managed.* Versions the controller created. |

### ModuleConfig

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name used when naming `Version` resources. Defaults to the `Module` resource name. |
| `provider` | `string` | Main OpenTofu provider the module requires (for example, `aws`). |
| `repoOwner` | `string` | Owner of the GitHub source repository. |
| `repoUrl` | `string` | Full URL of the GitHub source repository. |
| `fileFormat` | `string` | `zip` produces `.zip` archives. Any other value produces `.tar.gz`. Inherited from `global.moduleConfig` when unset in a Depot. |
| `immutable` | `bool` | When `true`, the archive checksum must match the stored `checksumSHA256` on every sync. Inherited from `global.moduleConfig` when unset in a Depot. |
| `versionConstraints` | `string` | Comma-separated constraints such as `>= 1.0.0, < 2.0.0`. Used only by the Depot controller. |
| `versionHistoryLimit` | `int` | Keeps the newest N versions. The controller trims `spec.versions` to the N highest semantic versions and deletes the removed `Version` resources. Unset, `<= 0`, or greater than the number of versions keeps all versions. Falls back to `spec.global.moduleConfig.versionHistoryLimit`, then `spec.global.versionHistoryLimit`. |
| `githubClientConfig` | `GithubClientConfig` | GitHub client settings. See [GithubClientConfig](#githubclientconfig). |
| `storageConfig` | `StorageConfig` | Storage backend for the archives. See [StorageConfig](#storageconfig). |

### ModuleVersion

Items in `spec.versions` and `status.moduleVersionRefs`.

| Field | Type | Description |
|---|---|---|
| `version` | `string` | Semantic version to sync. A leading `v` is trimmed. |
| `name` | `string` | *Controller-managed.* Name of the generated `Version` resource. |
| `fileName` | `string` | *Controller-managed.* Archive file name. |
| `synced` | `bool` | *Controller-managed.* Whether the `Version` has synced. |

### Provider

| Field | Type | Description |
|---|---|---|
| `spec.providerConfig` | `ProviderConfig` | **Required.** Settings for the provider and for each `Version` it creates. |
| `spec.versions` | `[]ProviderVersion` | **Required.** Versions to sync. A leading `v` is trimmed. |
| `spec.forceSync` | `bool` | Forces a re-sync. The controller resets it to `false`. Default `false`. |
| `status.latestVersion` | `string` | *Controller-managed.* Latest available version. |
| `status.synced` | `bool` | *Controller-managed.* Whether the provider reached its desired state. |
| `status.syncStatus` | `string` | *Controller-managed.* Reconciliation status. |
| `status.resolvedSourceRepository` | `string` | *Controller-managed.* See [ProviderStatus fields](#providerstatus-fields). |

### ProviderConfig

The `namespace`, `upstreamRegistry`, and `sourceRepository` fields are described in [ProviderConfig fields](#providerconfig-fields).

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Provider name. Defaults to the `Provider` resource name. |
| `operatingSystems` | `[]string` | Operating systems the provider supports, used as the `os` constraint. |
| `architectures` | `[]string` | Architectures the provider supports, used as the `arch` constraint. |
| `githubClientConfig` | `GithubClientConfig` | GitHub client settings for source scanning. Inherited from `global.providerConfig`, then `global.githubClientConfig`, when unset in a Depot. See [GithubClientConfig](#githubclientconfig). |
| `storageConfig` | `StorageConfig` | Storage backend for the binaries. See [StorageConfig](#storageconfig). |
| `versionHistoryLimit` | `int` | Keeps the newest N versions. The controller trims `spec.versions` to the N highest semantic versions and deletes the removed `Version` resources. Unset, `<= 0`, or greater than the number of versions keeps all versions. Falls back to `spec.global.providerConfig.versionHistoryLimit`, then `spec.global.versionHistoryLimit`. |
| `versionConstraints` | `string` | Comma-separated constraints such as `~> 1.0.0`. Used only by the Depot controller. |

### ProviderVersion

Items in `spec.versions`.

| Field | Type | Description |
|---|---|---|
| `version` | `string` | Semantic version to sync. A leading `v` is trimmed. |
| `architecture` | `string` | Architecture for this provider version. |
| `operatingSystem` | `string` | Operating system for this provider version. |
| `name` | `string` | *Controller-managed.* Provider name. |
| `synced` | `bool` | *Controller-managed.* Whether the `Version` has synced. |

### Version

`Version` resources are created by the Module and Provider controllers. Edit the parent resource instead of creating them by hand.

| Field | Type | Description |
|---|---|---|
| `spec.type` | `string` | **Required.** `Module` or `Provider`. |
| `spec.version` | `string` | **Required.** Semantic version. |
| `spec.architecture` | `string` | Provider architecture. |
| `spec.operatingSystem` | `string` | Provider operating system. |
| `spec.fileName` | `string` | Archive file name. Controller-managed. |
| `spec.forceSync` | `bool` | Forces this version to re-sync. |
| `spec.moduleConfigRef` | `ModuleConfig` | Copy of the parent module's config. Set by the Module controller. |
| `spec.providerConfigRef` | `ProviderConfig` | Copy of the parent provider's config. Set by the Provider controller. |
| `status` | `VersionStatus` | Sync state, checksum, and scan results. See [VersionStatus fields](#versionstatus-fields). |

### Depot

| Field | Type | Description |
|---|---|---|
| `spec.global` | `GlobalConfig` | Defaults for the entries the Depot manages. Requires `storageConfig`. See [GlobalConfig](#globalconfig). |
| `spec.moduleConfigs` | `[]ModuleConfig` | Modules to create and manage. |
| `spec.providerConfigs` | `[]ProviderConfig` | Providers to create and manage. |
| `spec.pollingIntervalMinutes` | `int` | Minutes between checks for new versions. Unset or `0` disables periodic polling. |
| `status.modules` | `[]string` | *Controller-managed.* Names of the `Module` resources the Depot manages. |
| `status.providers` | `[]string` | *Controller-managed.* Names of the `Provider` resources the Depot manages. |

### GlobalConfig

`spec.global` sets defaults for each entry that does not set its own value. Identity fields such as `name`, `sourceRepository`, and `repoOwner` are not allowed in a global block. Set them on each entry.

| Field | Type | Description |
|---|---|---|
| `storageConfig` | `StorageConfig` | **Required.** Storage backend for each module and provider that does not set its own. See [StorageConfig](#storageconfig). |
| `githubClientConfig` | `GithubClientConfig` | GitHub client settings for each module, provider, skill, and agent that does not set its own. See [GithubClientConfig](#githubclientconfig). |
| `versionHistoryLimit` | `int` | Keeps the newest N versions of each module and provider that does not set its own. Minimum `0`. |
| `moduleConfig` | `ModuleDefaults` | Defaults for `moduleConfigs` entries. See **ModuleDefaults** below. |
| `providerConfig` | `ProviderDefaults` | Defaults for `providerConfigs` entries. See **ProviderDefaults** below. |

**Precedence**

An entry value always wins. The kind-level block (`moduleConfig` or `providerConfig`) is checked next, then the top-level `global` block. A list inherits only when it is empty. A pointer inherits only when it is nil, so an explicit `0` or an explicit `githubClientConfig` on an entry stops inheritance.

| Setting | Applies to | Order |
|---|---|---|
| `versionHistoryLimit` | Modules, providers | Entry, `moduleConfig` or `providerConfig`, `global` |
| `storageConfig` | Modules, providers | Entry, `moduleConfig` or `providerConfig`, `global` |
| `githubClientConfig` | Modules, providers | Entry, `moduleConfig` or `providerConfig`, `global` |
| `fileFormat` | Modules | Entry, `moduleConfig` |
| `immutable` | Modules | Entry, `moduleConfig` |
| `operatingSystems` | Providers | Entry, `providerConfig` |
| `architectures` | Providers | Entry, `providerConfig` |

**ModuleDefaults** (`global.moduleConfig`)

| Field | Type | Description |
|---|---|---|
| `fileFormat` | `string` | Archive format for entries that do not set one. See [ModuleConfig](#moduleconfig). |
| `immutable` | `bool` | Checksum enforcement for entries that do not set one. See [ModuleConfig](#moduleconfig). |
| `githubClientConfig` | `GithubClientConfig` | Used when the entry does not set one. Falls back to `global.githubClientConfig`. |
| `storageConfig` | `StorageConfig` | Used when the entry does not set one. Falls back to `global.storageConfig`. |
| `versionHistoryLimit` | `int` | Used when the entry does not set one. Minimum `0`. Falls back to `global.versionHistoryLimit`. |

**ProviderDefaults** (`global.providerConfig`)

| Field | Type | Description |
|---|---|---|
| `operatingSystems` | `[]string` | Used when the entry lists none. |
| `architectures` | `[]string` | Used when the entry lists none. |
| `githubClientConfig` | `GithubClientConfig` | Used when the entry does not set one. Falls back to `global.githubClientConfig`. |
| `storageConfig` | `StorageConfig` | Used when the entry does not set one. Falls back to `global.storageConfig`. |
| `versionHistoryLimit` | `int` | Used when the entry does not set one. Minimum `0`. Falls back to `global.versionHistoryLimit`. |

`global.moduleConfig` does not accept `name`, `namespace`, `sourceRepository`, `versionConstraints`, `provider`, `repoOwner`, or `repoUrl`. `global.providerConfig` does not accept the same identity fields, and it does not accept `upstreamRegistry`. Set `upstreamRegistry` on each `providerConfigs` entry. The API server removes unknown fields when it decodes a Depot. See [Upgrading](../upgrading.md#depot-global-defaults).

### GithubClientConfig

| Field | Type | Description |
|---|---|---|
| `useAuthenticatedClient` | `bool` | Authenticates GitHub requests with a GitHub App to avoid rate limits. Requires a Secret named `opendepot-github-application-secret` in the resource's namespace with `githubAppID`, `githubInstallID`, and a base64 encoded `githubPrivateKey`. |

### StorageConfig

Each field selects a storage backend. `presign` configures pre-signed downloads.

| Field | Type | Description |
|---|---|---|
| `s3` | `AmazonS3Config` | Amazon S3 bucket. |
| `azureStorage` | `AzureStorageConfig` | Azure Storage account. |
| `gcs` | `GoogleCloudStorageConfig` | Google Cloud Storage bucket. |
| `fileSystem` | `FileSystemConfig` | Local filesystem path. |
| `presign` | `PresignConfig` | Pre-signed download URLs. See [PresignConfig fields](#presignconfig-fields). |

**AmazonS3Config**

| Field | Type | Description |
|---|---|---|
| `bucket` | `string` | **Required.** Bucket name. |
| `region` | `string` | **Required.** AWS region of the bucket. |
| `key` | `string` | Key prefix (for example, `my/bucket/prefix`). The file name is generated. |

**AzureStorageConfig**

| Field | Type | Description |
|---|---|---|
| `accountName` | `string` | **Required.** Storage account name. |
| `accountUrl` | `string` | **Required.** Storage account URL. |
| `subscriptionID` | `string` | **Required.** Subscription that contains the account. |
| `resourceGroup` | `string` | **Required.** Resource group that contains the account. |

**GoogleCloudStorageConfig**

| Field | Type | Description |
|---|---|---|
| `bucket` | `string` | **Required.** Bucket name. |

**FileSystemConfig**

| Field | Type | Description |
|---|---|---|
| `directoryPath` | `string` | Directory where archives are stored. |

### SecurityFinding

Represents a single vulnerability finding from a Trivy scan.

| Field | Type | Description |
|---|---|---|
| `vulnerabilityID` | `string` | CVE or GHSA identifier for the vulnerability, or a Trivy rule ID (e.g. `AWS-0057`) for module IaC findings |
| `pkgName` | `string` | Name of the package containing the vulnerability |
| `installedVersion` | `string` | Version of the package currently in use |
| `fixedVersion` | `string` | Minimum version that resolves the vulnerability, if known |
| `severity` | `string` | `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `UNKNOWN` |
| `title` | `string` | Short description of the vulnerability |
| `exempted` | `bool` | Whether a [`ScanPolicy`](#scanpolicy) exempted this finding from blocking reconciliation. Exempted findings are still reported so they remain visible and auditable; only the per-severity counters, `highestSeverity`, and `totalAffectedResources` treat them differently. Omitted when `false`. |
| `exemptionReason` | `string` | The `reason` recorded on the `ScanExemption` that covered this finding. Only set when `exempted` is `true`. |
| `exemptedBy` | `string` | The name of the `ScanPolicy` that exempted this finding. Only set when `exempted` is `true`. |

### BinaryScan

Holds Trivy binary scan (`trivy rootfs`) results for a specific provider artifact. Stored in `Version.status.binaryScan`. Each OS/architecture binary is scanned independently because Go stdlib versions and runtime dependencies may differ between compiled artifacts.

!!! note
    Trivy requires the execute bit to be set on the binary for gobinary detection. Provider binaries are written with `0500` permissions before scanning; binaries without execute permission are silently skipped by Trivy and `findings` will be empty.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the binary scan completed |
| `findings` | `[]SecurityFinding` | Vulnerabilities found in the compiled provider binary |

### SourceScan

Holds Trivy source scan results. Used for both provider `go.mod` dependency scans and module IaC (HCL filesystem) scans. Stored in `Version.status.sourceScan` for every `Version` resource when scanning is enabled.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the scan completed |
| `findings` | `[]SecurityFinding` | Findings produced by the scan. For provider `Version` resources these are `go.mod` dependency vulnerabilities (CVE identifiers). For module `Version` resources these are HCL misconfigurations (`vulnerabilityID` contains a Trivy rule ID such as `AWS-0057`). |

### ScanPolicy

`ScanPolicy` is a **namespaced** resource that lets you exempt specific scan findings, or override the severity threshold that blocks reconciliation, for a subset of `Version` resources without touching the cluster-wide `scanning.blockOnCritical`/`scanning.blockOnHigh` flags. See [Policy Enforcement](../configuration/scanning.md#policy-enforcement) for the full semantics — precedence, exact-match rules, RBAC boundary, and information-disclosure considerations.

The server also exposes the optional policy-management API for UI clients:

| Method | Path | Behavior |
|--------|------|----------|
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}` | Lists policies in the namespace. |
| `GET` | `/opendepot/ui/v1/scan-policies/catalog` | Lists authorized policy namespaces and currently onboarded Module/Provider targets. |
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Gets one policy. |
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}/capabilities` | Returns `writesEnabled`, `canRead`, `canWrite`, supported `methods`, and `supportsPreview`. |
| `POST` | `/opendepot/ui/v1/scan-policies/{namespace}/preview` | Validates a `ScanPolicy` JSON object without persisting it; returns `{ "valid": true, "policy": ... }`. |
| `POST` | `/opendepot/ui/v1/scan-policies/{namespace}` | Creates a policy from a JSON `ScanPolicy` object. |
| `PUT` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Replaces a policy. Requires `If-Match` and matching `metadata.resourceVersion`; missing preconditions return `428`, stale versions return `409 Conflict`. |
| `DELETE` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Deletes a policy. Requires `If-Match`; missing preconditions return `428`, stale versions return `409 Conflict`. |

`POST`, `PUT`, and `DELETE` return `404` until `server.policyManagement.enabled` is `true`. Anonymous callers can read policies but get `403` on mutations. In OIDC mode, access is default-deny and uses the effective `SecurityGroupBinding`. `GroupBinding` and Registry visibility do not grant policy access. Unauthorized namespace or item reads return `404`. A disallowed mutation in an authorized namespace returns `403`. Kubeconfig and bearer-token requests use Kubernetes RBAC. Status codes: `400` validation, `401` authentication, `403` authorization, `409` Kubernetes conflict.

The server logs each successful create, update, and delete with the operation, namespace, policy name, and authenticated subject. Kubernetes audit logging remains the authoritative record of the underlying CRD writes.

The server logs every successful create, update, and delete with the operation, namespace, policy name, and subject. For a durable record of the CRD write, configure the Kubernetes API server audit policy. UI access and a successful preview do not authorize writes.

**ScanPolicySpec fields:**

| Field | Type | Description |
|---|---|---|
| `priority` | `int` | The precedence of this policy. When more than one `ScanPolicy` matches a `Version`, the policy with the highest priority wins outright and supplies both the severity threshold and the exemption set; lower priority policies are ignored entirely rather than merged. Ties are broken by the oldest creation timestamp, then by name ascending. Default: `0` |
| `selector` | `metav1.LabelSelector` | Matched against the labels of the `Version` resources this policy applies to. A `Version` matches when the selector matches it or when any entry in `targetRefs` matches it. When both `selector` and `targetRefs` are omitted, the policy applies to every `Version` in its namespace. |
| `targetRefs` | `[]ScanPolicyTargetRef` | An explicit list of `Module` or `Provider` resources this policy applies to. |
| `severityThreshold` | `string` | The minimum severity that blocks reconciliation for the matched Versions: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `NONE`. Overrides the version controller's `--scan-block-on-critical`/`--scan-block-on-high` flags for matched Versions. `NONE` disables blocking entirely for the match. When omitted, the controller flags remain in effect as the baseline. |
| `exemptions` | `[]ScanExemption` | The findings that must not block reconciliation for the matched Versions. |
| `jevExemptions` | `[]JevExemption` | The Jev blocks that must not block reconciliation for the matched Versions. Each entry matches one exact archive by its `contentDigest`. Lists are keyed by `contentDigest`. See [JevExemption fields](#jevexemption-fields). |

**ScanPolicyStatus fields:**

| Field | Type | Description |
|---|---|---|
| `matchedVersions` | `int` | The number of `Version` resources in this namespace currently matched by this policy. |
| `activeExemptions` | `int` | The number of exemptions currently in effect. |
| `expiredExemptions` | `int` | The number of exemptions whose `expires` has passed and no longer apply. |
| `activeJevExemptions` | `int` | The number of Jev exemptions currently in effect. Entries without an `expires` count as active. Omitted when `0`. |
| `expiredJevExemptions` | `int` | The number of Jev exemptions whose `expires` has passed and no longer apply. Omitted when `0`. |
| `supersededBy` | `string` | The name of the higher priority `ScanPolicy` that shadows this policy for every `Version` it matches. Empty when this policy wins for at least one `Version`, or when it matches no Versions at all. |
| `conditions` | `[]metav1.Condition` | Observed conditions. Reasons: `Active`, `NoMatchingVersions`, `Superseded`, `InvalidSelector`. |

### ScanPolicyTargetRef

Identifies a `Module` or `Provider` resource a `ScanPolicy` applies to.

| Field | Type | Description |
|---|---|---|
| `kind` | `string` | `Module` or `Provider`. |
| `name` | `string` | The name of the `Module` or `Provider` resource. Matching is exact; the single literal `*` matches every resource of the given kind in the namespace. |
| `versions` | `string` | An optional [`hashicorp/go-version`](https://github.com/hashicorp/go-version) constraint string, e.g. `"1.2.1"`, `">= 1.0.0, < 2.0.0"`, or `"~> 1.0.0"`. Constraints use AND semantics. When omitted, matches every version of the resource. |

### ScanExemption

Declares a set of scan findings that must not block reconciliation. A finding is exempted when it matches every populated field of the exemption. All list fields below support only exact matches or the single literal `"*"` — there is no glob or regex support.

| Field | Type | Description |
|---|---|---|
| `vulnerabilityIDs` | `[]string` | Vulnerability or misconfiguration identifiers this exemption covers, e.g. `CVE-2024-1234` or `AWS-0057`. `["*"]` matches every identifier. Omitted covers every identifier. |
| `pkgNames` | `[]string` | Package names this exemption covers, e.g. `stdlib` or `golang.org/x/net`. `["*"]` matches every package. Omitted covers every package. |
| `scanTypes` | `[]string` | Scan types this exemption applies to: `binary`, `source`, `module`. Omitted covers every scan type. |
| `severities` | `[]string` | Severities this exemption covers. Matching is exact and case-insensitive. `["*"]` matches every severity. Omitted covers every severity. |
| `reason` | `string` | **Required.** Justification for the exemption, so every exemption remains auditable. Must be non-empty. |
| `expires` | `string` | RFC3339 timestamp at which this exemption stops applying. Once passed, the covered findings block again with no user action required. Omitted means the exemption never expires. |

Preview accepts the same object as create and update, and returns the normalized policy without writing it. It rejects unknown JSON fields, invalid DNS names, unsupported thresholds or target kinds, missing exemption reasons, and a namespace that differs from the URL. Preview does not reserve a name or check authorization for a later write.

### JevExemption fields

Declares a Jev exemption. An entry clears every Jev block reason for one exact archive. Only the winning `ScanPolicy` for a `Version` supplies its `jevExemptions`. See [Jev exemptions with ScanPolicy](../configuration/scanning.md#jev-exemptions-with-scanpolicy).

| Field | Type | Description |
|---|---|---|
| `contentDigest` | `string` | **Required.** The lowercase hex SHA-256 of the Version archive, copied from `status.jevAssessment.contentDigest`. Must match `^[0-9a-f]{64}$`. The exemption applies only while the archive has this digest. |
| `reason` | `string` | **Required.** Justification for the exemption, so every exemption remains auditable. Must be non-empty. |
| `expires` | `string` | RFC3339 timestamp at which this exemption stops applying. Once passed, the Version blocks again. Omitted means the exemption never expires and stays valid until the archive content changes. |

The server rejects a `contentDigest` that is not 64 lowercase hex characters, a duplicate `contentDigest` in one policy, and a blank `reason`.

### ProviderConfig fields

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | The organisation namespace in the upstream registry (e.g. `hashicorp`, `integrations`, `DataDog`). Defaults to `hashicorp`. Used for binary download and source repository lookup. Existing `Provider` resources without this field continue to work unchanged. |
| `upstreamRegistry` | `string` | The canonical registry used to discover and download this provider. Controls upstream version discovery, archive acquisition, and the canonical provider identity exposed via the Network Mirror Protocol and UI snippets. Allowed values: `registry.opentofu.org`, `registry.terraform.io`. Defaults to `registry.opentofu.org` when omitted. Providers from different origins with the same namespace/type should be placed in separate Kubernetes namespaces to avoid Version resource-name collisions. See [Consuming Providers](../guides/providers.md) for examples. |
| `sourceRepository` | `string` | Full GitHub URL of the provider's source repository (e.g. `https://github.com/hashicorp/terraform-provider-aws`). When omitted for an OpenTofu Registry provider, OpenDepot queries `api.opentofu.org` and falls back to `https://github.com/{namespace}/terraform-provider-{name}` if the lookup fails. Terraform Registry providers use the fallback directly because that registry does not expose equivalent source metadata. Set this field to override an incorrect or unavailable result. |

### ReadmeConfigMapRef

References the ConfigMap and data key holding a module `Version`'s base64 encoded README content. Stored in `Version.status.readmeConfigMapRef`. The ConfigMap is owned by the `Version` resource (via `ownerReferences`), so it is garbage collected automatically when the `Version` is deleted.

This reference applies to module `Version` resources only. Skill and Agent definitions do not fill README content. A Skill or Agent `Version` does not use `readmeConfigMapRef`, and the version controller clears any existing reference on it. Their definition files are stored in [`DefinitionConfigMapRef`](#definitionconfigmapref) instead.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the ConfigMap holding the README content. |
| `key` | `string` | Key within the ConfigMap's `data` holding the base64 encoded README content. |

### DefinitionConfigMapRef

References the ConfigMap and data key holding a Skill or Agent `Version`'s base64 encoded definition file. Stored in `Version.status.definitionConfigMapRef`. The ConfigMap is owned by the `Version` resource (via `ownerReferences`), so it is garbage collected automatically when the `Version` is deleted. The OpenDepot UI shows the file as `SKILL.md` for skills and `AGENT.md` for agents.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the ConfigMap holding the definition file. |
| `key` | `string` | Key within the ConfigMap's `data` holding the base64 encoded definition file. |
| `fileName` | `string` | Name of the source entry file, such as `SKILL.md`, `code-reviewer.agent.md`, or `code-reviewer.md`. Maximum 255 characters. |

The base64 encoded definition must be at most 1,000,000 bytes. This keeps the ConfigMap below the 1 MiB Kubernetes object limit. A larger definition is not synced. The Version gets `synced: false`, and `syncStatus` records the file name, its size, the encoded size, and the limit. The Version stays unsynced until the definition changes.

### VersionStatus fields

| Field | Type | Description |
|---|---|---|
| `binaryScan` | `BinaryScan` | Binary vulnerability scan result for this specific provider artifact. Populated only for provider `Version` resources when scanning is enabled. |
| `sourceScan` | `SourceScan` | Scan result for this version. Contains IaC misconfigurations for module `Version` resources and `go.mod` dependency vulnerabilities for provider `Version` resources. Populated when scanning is enabled. |
| `archiveSizeBytes` | `int64` | Compressed archive size in bytes stored in the backend. Set automatically by the version controller after a successful upload. Omitted until the archive has been synced. |
| `readmeConfigMapRef` | `ReadmeConfigMapRef` | Reference to the ConfigMap holding this version's base64 encoded README content. Only populated for module `Version` resources when a README could be resolved from GitHub or the module archive. See [Module READMEs](../guides/operations.md#module-readmes). |
| `definitionConfigMapRef` | `DefinitionConfigMapRef` | Reference to the ConfigMap holding this version's base64 encoded definition file. Only populated for Skill and Agent `Version` resources. See [`DefinitionConfigMapRef`](#definitionconfigmapref). |
| `jevAssessment` | `JevAssessment` | The Jev assessment of this Version. Only populated when Jev is enabled and the source sets `jevSecretRef`. See [JevAssessment fields](#jevassessment-fields). |

### ProviderStatus fields

| Field | Type | Description |
|---|---|---|
| `resolvedSourceRepository` | `string` | The VCS source URL resolved by the Version controller. Set automatically on first scan; `spec.providerConfig.sourceRepository` takes precedence if set. See [Provider Source Repository Resolution](../configuration/scanning.md#provider-source-repository-resolution). |

### Branch source fields

These fields apply to `AgentSourceConfig`, which is used by Depot `skillConfigs` and `agentConfigs`, by `Skill.spec`, `Agent.spec`, and by `Version.spec.agentSourceRef`. Branch sourcing is enabled only when the chart sets `branchSourcing.enabled: true`. See [Branch sourcing](../guides/agents-publishing.md#branch-sourcing).

| Field | Type | Description |
|---|---|---|
| `ref` | `string` | A Git branch to follow instead of tags. Must match `^[A-Za-z0-9._+/-]+$` and is validated in the controller. Mutually exclusive with `versionConstraints` and `tagPrefix`. |
| `branchPolicy` | `BranchPolicy` | Branch-mode thresholds. Requires `ref`. Applies to branch sources only. |

The following rules are enforced by CEL on the type:

- `ref` and `versionConstraints` are mutually exclusive: "ref and versionConstraints are mutually exclusive".
- `tagPrefix` is not used with `ref`: "tagPrefix is not used with ref".
- `branchPolicy` requires `ref`: "branchPolicy requires ref".

`versionConstraints` applies to tag mode only.

### BranchPolicy fields

`BranchPolicy` classifies each new commit and never blocks a Version. Defaults apply when a field or the whole policy is omitted.

| Field | Type | Default | Description |
|---|---|---|---|
| `minVersionInterval` | `Duration` | `10m` | The minimum time between Versions for a source. `0s` disables the interval. Negative values are rejected. |
| `majorThreshold` | `float64` | `0.5` | MAJOR when the major-change probability is at least this value. Between `0` and `1`. |
| `minorThreshold` | `float64` | `0.5` | MINOR when the major-change probability plus the minor-change probability is at least this value. Between `0` and `1`. |
| `minConfidence` | `float64` | `0.5` | A Version whose confidence is below this value is marked `needsReview`. Informational only. Between `0` and `1`. |

For the classification rules, see [Version bump classification](../guides/agents-publishing.md#version-bump-classification).

### SkillVersion and AgentVersion fields

These types are used by `spec.versions` and `status.versionRefs` on the Skill and Agent resources.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | The name of the Version resource. |
| `version` | `string` | The semver for the entry. Empty for a branch entry until it is assigned. |
| `commit` | `string` | The 40-character commit SHA for a branch entry. Must match `^[0-9a-f]{40}$`. |
| `discoveredAt` | `Time` | When the commit was discovered. Used to order branch entries. |
| `state` | `string` | `Pending`, `Assigned`, or `Rejected`. Set in the spec for branch entries. |

### VersionSpec branch fields

| Field | Type | Description |
|---|---|---|
| `sourceCommit` | `string` | The 40-character commit SHA the Version is pinned to. Must match `^[0-9a-f]{40}$`. Immutable once set. |

For a branch Version, `version` is empty until the version controller assigns a semver, and then it cannot change. A Version without a `version` must set `sourceCommit`, and `sourceCommit` requires `agentSourceRef.ref`.

### VersionStatus branch fields

| Field | Type | Description |
|---|---|---|
| `sourceCommit` | `string` | The commit SHA that was actually downloaded. |
| `branchPhase` | `string` | `Pending`, `Held`, `Assigned`, or `Rejected`. |
| `bumpAssessment` | `BumpAssessment` | The bump decision for this Version. Never contains the Jev token or prompt content. |

### BumpAssessment fields

| Field | Type | Description |
|---|---|---|
| `commit` | `string` | The commit this assessment is for. |
| `previousCommit` | `string` | The commit of the previous Version. |
| `previousVersion` | `string` | The semver of the previous Version. |
| `category` | `string` | `Major`, `Minor`, or `Patch`. |
| `source` | `string` | `Initial`, `Default`, `Jev`, `Withheld`, or `Manual`. |
| `reason` | `string` | The reason for the decision. |
| `model` | `string` | The Jev model that produced the answer, when Jev was used. |
| `evaluatedAt` | `string` | RFC3339 time of the decision. |
| `majorProbability`, `minorProbability`, `patchProbability` | `float64` | Optional. The Jev probabilities for each bump size. |
| `confidence` | `float64` | Optional. The Jev confidence. |
| `needsReview` | `bool` | Set when the confidence is below `minConfidence`. Informational only. |
| `attempts` | `int` | The number of Jev attempts. |
| `error` | `string` | The redacted error message, when a Jev call failed. |

### JevPolicy fields

`jevPolicy` is set on the same sources as `jevSecretRef`. Each threshold is optional. A threshold that is not set never blocks a Version. Each threshold is checked against every part of the content, and a breach in any part blocks the Version. See [Thresholds](../guides/agents-jev.md#thresholds).

| Field | Type | Description |
|---|---|---|
| `minSafeProbability` | `float64` | Blocks when the safe probability is below this value. |
| `maxInjectionProbability` | `float64` | Blocks when the prompt injection probability is above this value. |
| `maxExfiltrationProbability` | `float64` | Blocks when the data exfiltration probability is above this value. |
| `maxDestructiveProbability` | `float64` | Blocks when the destructive action probability is above this value. |
| `maxHiddenInstructionsProbability` | `float64` | Blocks when the hidden instructions probability is above this value. |
| `maxScopeMismatchProbability` | `float64` | Blocks when the scope mismatch probability is above this value. |
| `maxRemoteExecutionProbability` | `float64` | Blocks when the remote execution probability is above this value. |
| `maxRiskScore` | `float64` | Blocks when the risk score is above this value. |
| `minConfidence` | `float64` | Blocks when the risk confidence is below this value, and sets `needsReview`. |

### JevAssessment fields

`jevAssessment` is set on `VersionStatus`. It is combined from the scored parts. See [How content is split](../guides/agents-jev.md#how-content-is-split).

| Field | Type | Description |
|---|---|---|
| `evaluatedAt` | `string` | The time at which the assessment was evaluated. |
| `contentDigest` | `string` | Optional. The lowercase hex SHA-256 of the archive that Jev assessed. It equals the hex form of `status.checksum`. Used as the `jevExemptions` key. A legacy assessment without a digest cannot match an exemption until one `forceSync`. |
| `model` | `string` | Optional. The Jev model that produced the assessment. |
| `safeProbability` | `float64` | Optional. The lowest part value. |
| `injectionProbability`, `exfiltrationProbability`, `destructiveProbability`, `hiddenInstructionsProbability`, `scopeMismatchProbability`, `remoteExecutionProbability` | `float64` | Optional. The highest part value for each probability. |
| `riskScore` | `float64` | Optional. The highest part value. |
| `riskLevel` | `string` | Optional. The worst part value. One of `Minimal`, `Low`, `Moderate`, `High`, or `Critical`. |
| `riskConfidence` | `float64` | Optional. The lowest part value. |
| `needsReview` | `bool` | Set when a part's risk confidence is below `minConfidence`, or when the risk level is `Critical` on a fully scored assessment. An exemption does not change it, but the Workshop Needs review queue and the Stats Needs review count hide the Version while the exemption is active. |
| `blocked` | `bool` | Whether Jev blocks the Version: a threshold, a `Critical` risk level, a part below `minConfidence`, content Jev could not read, Jev disabled on the controller, or an assessment that could not complete. `false` when a matching `jevExemptions` entry cleared every block reason. |
| `blockReasons` | `[]string` | Optional. The Jev block reasons. When the assessment is exempted, this lists the reasons that were cleared. The field is kept when `blocked` is `false`. |
| `exempted` | `bool` | Optional. Whether a `jevExemptions` entry cleared the block reasons for this assessment. Omitted when `false`. This does not mean the Version is synced. When the Jev call failed, `status.synced` stays `false` until a Jev run completes without an error. |
| `exemptedBy` | `string` | Optional. The name of the `ScanPolicy` whose `jevExemptions` entry cleared the block reasons. |
| `exemptionReason` | `string` | Optional. The `reason` given by the `jevExemptions` entry that cleared the block reasons. |
| `exemptionExpires` | `string` | Optional. The `expires` of the `jevExemptions` entry that cleared the block reasons. Omitted when the entry never expires. |
| `error` | `string` | Optional. A redacted error that prevented the assessment from completing, or why it is incomplete. |
| `chunkCount` | `int32` | Optional. The planned number of parts. Above 16, no Jev call is made and the Version is blocked. |
| `assessedChunks` | `int32` | Optional. The number of part entries that Jev scored. At most 16. |
| `chunks` | [`[]JevChunkResult`](#jevchunkresult-fields) | Optional. One entry per scored or attempted part. At most 16 entries. |
| `skippedFiles` | [`[]JevSkippedFile`](#jevskippedfile-fields) | Optional. Files that Jev could not read. Any entry blocks the Version. At most 32 entries are listed. |
| `skippedFileCount` | `int32` | Optional. The total number of skipped files. |
| `retryCount` | `int32` | Optional. The number of counted failed attempts since the last ForceSync, which resets it to `0`. The count starts at `0` and rises by one for each counted failure. 401 and 403 failures are not counted. Automatic retries stop once it exceeds `jevMaxRetries`, which defaults to `3`. See [Retries](../guides/agents-jev-exemptions.md#retries). |

### JevChunkResult fields

Each entry describes one part of a Version's content.

| Field | Type | Description |
|---|---|---|
| `index` | `int32` | The 1-based planned part number, from 1 to 16. A halved part keeps the index of the part it came from. |
| `label` | `string` | The part index plus any halving suffix, such as `3`, `3a`, or `3ab`. At most 4 characters. |
| `files` | `[]string` | Optional. The paths whose content, or part of it, is in this part. The entry file is in part 1. At most 32 entries. Each path is cut to 256 bytes at a rune boundary, with no suffix. |
| `fileCount` | `int32` | The exact number of files in this part. |
| `status` | `string` | `Scored`, `Failed`, `TokenLimitExceeded`, or `NotSent`. |
| `riskLevel` | `string` | Optional. Set when the part is `Scored`. |
| `riskScore` | `float64` | Optional. Set when the part is `Scored`. |
| `riskConfidence` | `float64` | Optional. Set when the part is `Scored`. |
| `safeProbability` | `float64` | Optional. Set when the part is `Scored`. |
| `error` | `string` | Optional. The redacted reason the part failed or was not sent. Cut to 512 bytes. |

### JevSkippedFile fields

| Field | Type | Description |
|---|---|---|
| `path` | `string` | The path of the skipped file. Cut to 256 bytes at a rune boundary, with no suffix. |
| `reason` | `string` | `Withheld` for a file flagged for a secret, or `Binary` for a binary file. |

### GroupBinding

`GroupBinding` is a namespaced resource that grants a group of OIDC users access to specific modules, providers, skills, and agents. The server evaluates all GroupBindings in alphabetical order by name and applies the first one whose `expression` matches the user's groups claim. If an expression fails to compile or evaluate, the request is denied with `403 Forbidden`. Requires OIDC authentication to be enabled.

See the [GroupBinding guide](../guides/groupbinding.md) for usage examples.

**GroupBindingSpec fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `expression` | `string` | Yes | An [expr-lang](https://expr-lang.org/) boolean expression evaluated against the user's groups. The evaluation environment exposes `groups []string`. Must return `true` or `false`. Example: `'"platform-team" in groups'` |
| `moduleResources` | `[]string` | No | Glob patterns (`path.Match` semantics) for module names the group may access. Empty list denies access to all modules. Example: `["aws-*", "gcp-networking"]` |
| `providerResources` | `[]string` | No | Exact provider type names the group may access, or `["*"]` to allow all providers. Empty list denies access to all providers. Example: `["aws", "google"]` |
| `skillResources` | `[]string` | No | Glob patterns (`path.Match` semantics) for skill names the group may access. Empty list denies access to all skills. Example: `["code-review-*"]` |
| `agentResources` | `[]string` | No | Glob patterns (`path.Match` semantics) for agent names the group may access. Empty list denies access to all agents. Example: `["release-*"]` |

**Example manifest:**

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: platform-team-binding
  namespace: opendepot-system
spec:
  expression: '"platform-team" in groups'
  moduleResources:
    - "aws-*"
    - "gcp-networking"
  providerResources:
    - "aws"
    - "google"
```

### SecurityGroupBinding

`SecurityGroupBinding` is a namespaced resource for ScanPolicy administration.
It is independent of `GroupBinding`, which remains responsible for Registry and
browse authorization. The server reads bindings from its configured namespace,
sorts them by name, and applies the first matching expression. Invalid
expressions, evaluation errors, and no matching binding deny policy access.

**SecurityGroupBindingSpec fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `expression` | `string` | Yes | expr-lang boolean expression evaluated against `groups []string`. |
| `namespaces` | `[]string` | No | Policy namespace allow-list. Empty denies access; `"*"` allows all namespaces reachable by the server ServiceAccount. |
| `moduleResources` | `[]string` | No | Allowed Module names using `path.Match` glob semantics. Empty denies Module targets. |
| `providerResources` | `[]string` | No | Allowed Provider names by exact match, or `"*"` for all onboarded Providers. Empty denies Provider targets. |
| `namespaceWidePolicyManagement` | `bool` | No | Allows selector-based and empty-target policies in allowed namespaces. Defaults to `false`. |

The policy catalog is:

```text
GET /opendepot/ui/v1/scan-policies/catalog
```

It returns `{ "writesEnabled": bool, "items": [...] }`. Each item contains
`namespace`, `canRead`, `canWrite`, `canManageNamespaceWidePolicies`, and sorted
`modules` and `providers` arrays whose entries contain `name`. The catalog
contains only authorized namespaces and currently onboarded resources. It does
not disclose binding expressions, patterns, or denied resources.

For OIDC policy requests, a targeted policy is authorized only when it has no
selector, has at least one `targetRef`, and every target is allowed by the
effective SecurityGroupBinding. Selectors and policies without `targetRefs`
require `namespaceWidePolicyManagement: true`. The server applies the same
scope check to reads, previews, creates, updates, and deletes; updates check
both the persisted and proposed object.

### BrowseStats

Returned by `GET /opendepot/ui/v1/stats`.

| Field | Type | Description |
|---|---|---|
| `totalModules` | `int` | Number of visible `Module` resources |
| `modulesSynced` | `int` | Number of visible `Module` resources with `status.synced` set to `true` |
| `totalProviders` | `int` | Number of visible `Provider` resources |
| `providersSynced` | `int` | Number of visible `Provider` resources with `status.synced` set to `true` |
| `totalSkills` | `int` | Number of visible `Skill` resources |
| `skillsSynced` | `int` | Number of visible `Skill` resources with `status.synced` set to `true` |
| `totalAgents` | `int` | Number of visible `Agent` resources |
| `agentsSynced` | `int` | Number of visible `Agent` resources with `status.synced` set to `true` |
| `totalVersions` | `int` | Total number of `Version` resources across all visible modules and providers |
| `versionsSynced` | `int` | Number of those `Version` resources counted as synced in `syncHealth.syncedVersions` |
| `totalStorageBytes` | `int64` | Sum of `VersionStatus.archiveSizeBytes` across all visible versions; `0` when no archive sizes have been recorded |
| `totalDownloads` | `int64` | Download events observed by Prometheus during the configured lookback window, counted only for resources in the caller's visibility set; `0` until a matching metric has been scraped |
| `syncHealth` | `SyncHealthStats` | Breakdown of version sync states |
| `securityPosture` | `SecurityPostureStats` | Aggregate finding counts across all visible resources |
| `jev` | `JevStats` | Jev assessment totals across all visible versions that have `status.jevAssessment`. `inputTokens` sums the input tokens Jev reported, including failed calls that were billed. `riskLevels` counts assessed versions by risk level and does not include versions whose assessment failed |
| `storageDistribution` | `[]StorageBackendStat` | Per-backend version counts |
| `mostDownloaded` | `MostDownloadedStats` | Top 10 most-downloaded resources per kind, as `modules`, `providers`, `skills`, and `agents` arrays. Filtered to the caller's visibility set; zero-download resources are omitted |

### SyncHealthStats

| Field | Type | Description |
|---|---|---|
| `syncedVersions` | `int` | Versions with `status.synced: true` |
| `unsyncedVersions` | `int` | Versions not yet synced |
| `failedVersions` | `int` | Versions where sync has failed or errored |

### SecurityPostureStats

| Field | Type | Description |
|---|---|---|
| `critical` | `int` | Findings at CRITICAL severity |
| `high` | `int` | Findings at HIGH severity |
| `medium` | `int` | Findings at MEDIUM severity |
| `low` | `int` | Findings at LOW severity |
| `unknown` | `int` | Findings at UNKNOWN severity |
| `exempted` | `int` | Findings currently exempted by a [`ScanPolicy`](#scanpolicy). Excluded from the severity counters above and from `totalAffectedResources`. |
| `totalAffectedResources` | `int` | Number of distinct resources with at least one non-exempted finding |

### StorageBackendStat

| Field | Type | Description |
|---|---|---|
| `backend` | `string` | Storage backend identifier (e.g. `s3`, `azure`, `gcs`, `filesystem`) |
| `count` | `int` | Number of versions stored on this backend |

### PopularResource

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | Kubernetes namespace of the resource |
| `kind` | `string` | `module` or `provider` |
| `name` | `string` | Resource name |
| `version` | `string` | Most-downloaded version |
| `downloadCount` | `int64` | Total download events for this resource/version |
| `lastDownloadedAt` | `string` | RFC3339 timestamp of the most recent download; omitted when no downloads recorded |

### PresignConfig fields

Controls pre-signed URL generation for provider and agent downloads. Set on `StorageConfig.presign`. For Agent and Skill sources, the controller copies this setting to each owned Version, and archive downloads use the Version's copy. A change on the source reaches the Versions on the next reconcile.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | When `true`, download requests are redirected to the storage backend via a pre-signed URL instead of proxied through the server. |
| `ttl` | `duration` | `15m` | How long the pre-signed URL remains valid (e.g. `"15m"`, `"1h"`). |
| `fallbackToProxy` | `bool` | `true` | When `true`, if pre-sign generation fails the server falls back to proxying the download. Set to `false` to make pre-signing strictly required. For Agent objects, the default is `false`. See [agents.v1 Protocol](agents-v1.md). |
