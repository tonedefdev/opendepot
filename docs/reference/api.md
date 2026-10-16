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
grant_type=authorization_code&code=<AUTH_CODE>&client_id=<CLIENT_ID>&redirect_uri=http://localhost:10000
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
  "download_url": "https://.../opendepot/providers/v1/download/opendepot-system/aws/5.80.0",
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
GET /opendepot/providers/v1/download/{namespace}/{type}/{version}
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

Returns a paginated, filtered list of visible `Module` and `Provider` resources.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string (repeatable) | Filter by one or more namespaces |
| `kind` | string | `module` or `provider` |
| `q` | string | Search string matched against resource name |
| `synced` | bool | Filter to synced (`true`) or unsynced (`false`) resources |
| `os` | string | Filter providers by operating system |
| `arch` | string | Filter providers by CPU architecture |
| `severity` | string | Filter to resources with findings at or above this level (`CRITICAL`, `HIGH`, `MEDIUM`, `LOW`) |
| `public_only` | bool | When `true`, return only publicly-labelled resources |
| `sort_by` | string | Sort field |
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
| `kind` | `module` or `provider` |
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

### List Resource Versions

```
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
```

Returns a paginated, filtered list of versions for a single resource. Used by the OpenDepot Workshop detail page to populate the versions table. Authentication follows the same rules as the other browse endpoints.

**Path Parameters:**

| Parameter | Description |
|-----------|-------------|
| `namespace` | Kubernetes namespace of the resource |
| `kind` | `module` or `provider` |
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

Returns a graph of all visible `Depot`, `Module`, and `Provider` resources with directed edges connecting each depot to its managed modules and providers. Used by the [Depots page](../guides/registry-explorer/depots.md) in OpenDepot Workshop to render the interactive relationship diagram.

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
      "managedProviderNames": ["aws"]
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
  "edges": [
    { "id": "e-depot-mod-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/module/terraform-aws-vpc" },
    { "id": "e-depot-prov-0", "source": "opendepot-system/platform-depot", "target": "opendepot-system/provider/aws" }
  ],
  "summary": {
    "totalDepots": 1,
    "totalModules": 1,
    "totalProviders": 1
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
  "totalProviders": 3,
  "totalVersions": 87,
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
  "storageDistribution": [
    { "backend": "s3", "count": 10 },
    { "backend": "filesystem", "count": 5 }
  ],
  "mostDownloaded": [
    {
      "namespace": "opendepot-system",
      "kind": "module",
      "name": "terraform-aws-vpc",
      "version": "3.19.0",
      "downloadCount": 4821,
      "lastDownloadedAt": "2026-05-25T14:32:00Z"
    }
  ]
}
```

`totalStorageBytes` sums `VersionStatus.archiveSizeBytes` across visible versions and is `0` when no sizes are recorded. `totalDownloads` and `mostDownloaded` come from Prometheus over the configured lookback window. Both stay `0` or empty until a matching metric is scraped. Download counts are filtered by the caller's visibility, so unauthenticated callers do not see private names in `mostDownloaded`.

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
| `versionHistoryLimit` | `int` | Number of versions to keep. Unset or `<= 0` keeps all versions. |
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
| `githubClientConfig` | `GithubClientConfig` | GitHub client settings for source scanning. See [GithubClientConfig](#githubclientconfig). |
| `storageConfig` | `StorageConfig` | Storage backend for the binaries. See [StorageConfig](#storageconfig). |
| `versionHistoryLimit` | `int` | Number of versions to keep. Unset or `<= 0` keeps all versions. |
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
| `spec.global` | `GlobalConfig` | Defaults for Depot-managed modules. Requires `storageConfig`. Optional `githubClientConfig` and `moduleConfig`. |
| `spec.moduleConfigs` | `[]ModuleConfig` | Modules to create and manage. |
| `spec.providerConfigs` | `[]ProviderConfig` | Providers to create and manage. |
| `spec.pollingIntervalMinutes` | `int` | Minutes between checks for new versions. Unset or `0` disables periodic polling. |
| `status.modules` | `[]string` | *Controller-managed.* Names of the `Module` resources the Depot manages. |
| `status.providers` | `[]string` | *Controller-managed.* Names of the `Provider` resources the Depot manages. |

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
| `vulnerabilityID` | `string` | CVE or GHSA identifier for the vulnerability, or a Trivy rule ID (e.g. `aws-0057`) for module IaC findings |
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
| `findings` | `[]SecurityFinding` | Findings produced by the scan. For provider `Version` resources these are `go.mod` dependency vulnerabilities (CVE identifiers). For module `Version` resources these are HCL misconfigurations (`vulnerabilityID` contains a Trivy rule ID such as `aws-0057`). |

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

The server logs every successful create, update, and delete with the operation, namespace, policy name, and subject. For a durable record of the CRD write, configure the Kubernetes API server audit policy. UI access and a successful preview do not authorize writes.

**ScanPolicySpec fields:**

| Field | Type | Description |
|---|---|---|
| `priority` | `int` | The precedence of this policy. When more than one `ScanPolicy` matches a `Version`, the policy with the highest priority wins outright and supplies both the severity threshold and the exemption set; lower priority policies are ignored entirely rather than merged. Ties are broken by the oldest creation timestamp, then by name ascending. Default: `0` |
| `selector` | `metav1.LabelSelector` | Matched against the labels of the `Version` resources this policy applies to. A `Version` matches when the selector matches it or when any entry in `targetRefs` matches it. When both `selector` and `targetRefs` are omitted, the policy applies to every `Version` in its namespace. |
| `targetRefs` | `[]ScanPolicyTargetRef` | An explicit list of `Module` or `Provider` resources this policy applies to. |
| `severityThreshold` | `string` | The minimum severity that blocks reconciliation for the matched Versions: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `NONE`. Overrides the version controller's `--scan-block-on-critical`/`--scan-block-on-high` flags for matched Versions. `NONE` disables blocking entirely for the match. When omitted, the controller flags remain in effect as the baseline. |
| `exemptions` | `[]ScanExemption` | The findings that must not block reconciliation for the matched Versions. |

**ScanPolicyStatus fields:**

| Field | Type | Description |
|---|---|---|
| `matchedVersions` | `int` | The number of `Version` resources in this namespace currently matched by this policy. |
| `activeExemptions` | `int` | The number of exemptions currently in effect. |
| `expiredExemptions` | `int` | The number of exemptions whose `expires` has passed and no longer apply. |
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
| `vulnerabilityIDs` | `[]string` | Vulnerability or misconfiguration identifiers this exemption covers, e.g. `CVE-2024-1234` or `aws-0057`. `["*"]` matches every identifier. Omitted covers every identifier. |
| `pkgNames` | `[]string` | Package names this exemption covers, e.g. `stdlib` or `golang.org/x/net`. `["*"]` matches every package. Omitted covers every package. |
| `scanTypes` | `[]string` | Scan types this exemption applies to: `binary`, `source`, `module`. Omitted covers every scan type. |
| `severities` | `[]string` | Severities this exemption covers. Matching is exact and case-insensitive. `["*"]` matches every severity. Omitted covers every severity. |
| `reason` | `string` | **Required.** Justification for the exemption, so every exemption remains auditable. Must be non-empty. |
| `expires` | `string` | RFC3339 timestamp at which this exemption stops applying. Once passed, the covered findings block again with no user action required. Omitted means the exemption never expires. |

Preview accepts the same object as create and update, and returns the normalized policy without writing it. It rejects unknown JSON fields, invalid DNS names, unsupported thresholds or target kinds, missing exemption reasons, and a namespace that differs from the URL. Preview does not reserve a name or check authorization for a later write.

### ProviderConfig fields

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | The organisation namespace in the upstream registry (e.g. `hashicorp`, `integrations`, `DataDog`). Defaults to `hashicorp`. Used for binary download and source repository lookup. Existing `Provider` resources without this field continue to work unchanged. |
| `upstreamRegistry` | `string` | The canonical registry used to discover and download this provider. Controls upstream version discovery, archive acquisition, and the canonical provider identity exposed via the Network Mirror Protocol and UI snippets. Allowed values: `registry.opentofu.org`, `registry.terraform.io`. Defaults to `registry.opentofu.org` when omitted. Providers from different origins with the same namespace/type should be placed in separate Kubernetes namespaces to avoid Version resource-name collisions. See [Consuming Providers](../guides/providers.md) for examples. |
| `sourceRepository` | `string` | Full GitHub URL of the provider's source repository (e.g. `https://github.com/hashicorp/terraform-provider-aws`). When omitted for an OpenTofu Registry provider, OpenDepot queries `api.opentofu.org` and falls back to `https://github.com/{namespace}/terraform-provider-{name}` if the lookup fails. Terraform Registry providers use the fallback directly because that registry does not expose equivalent source metadata. Set this field to override an incorrect or unavailable result. |

### ReadmeConfigMapRef

References the ConfigMap and data key holding a module `Version`'s base64 encoded README content. Stored in `Version.status.readmeConfigMapRef`. The ConfigMap is owned by the `Version` resource (via `ownerReferences`), so it is garbage collected automatically when the `Version` is deleted.

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Name of the ConfigMap holding the README content. |
| `key` | `string` | Key within the ConfigMap's `data` holding the base64 encoded README content. |

### VersionStatus fields

| Field | Type | Description |
|---|---|---|
| `binaryScan` | `BinaryScan` | Binary vulnerability scan result for this specific provider artifact. Populated only for provider `Version` resources when scanning is enabled. |
| `sourceScan` | `SourceScan` | Scan result for this version. Contains IaC misconfigurations for module `Version` resources and `go.mod` dependency vulnerabilities for provider `Version` resources. Populated when scanning is enabled. |
| `archiveSizeBytes` | `int64` | Compressed archive size in bytes stored in the backend. Set automatically by the version controller after a successful upload. Omitted until the archive has been synced. |
| `readmeConfigMapRef` | `ReadmeConfigMapRef` | Reference to the ConfigMap holding this version's base64 encoded README content. Only populated for module `Version` resources when a README could be resolved from GitHub or the module archive. See [Module READMEs](../guides/operations.md#module-readmes). |

### ProviderStatus fields

| Field | Type | Description |
|---|---|---|
| `resolvedSourceRepository` | `string` | The VCS source URL resolved by the Version controller. Set automatically on first scan; `spec.providerConfig.sourceRepository` takes precedence if set. See [Provider Source Repository Resolution](../configuration/scanning.md#provider-source-repository-resolution). |

### GroupBinding

`GroupBinding` is a namespaced resource that grants OIDC groups access to specific modules and providers. The server evaluates bindings alphabetically by name and applies the first whose `expression` matches the user's groups claim. An expression that fails to compile or evaluate denies the request with `403 Forbidden`. Requires OIDC authentication.

See the [GroupBinding guide](../guides/groupbinding.md) for usage examples.

**GroupBindingSpec fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `expression` | `string` | Yes | An [expr-lang](https://expr-lang.org/) boolean expression evaluated against the user's groups. The evaluation environment exposes `groups []string`. Must return `true` or `false`. Example: `'"platform-team" in groups'` |
| `moduleResources` | `[]string` | No | Glob patterns (`path.Match` semantics) for module names the group may access. Empty list denies access to all modules. Example: `["aws-*", "gcp-networking"]` |
| `providerResources` | `[]string` | No | Exact provider type names the group may access, or `["*"]` to allow all providers. Empty list denies access to all providers. Example: `["aws", "google"]` |

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
| `totalProviders` | `int` | Number of visible `Provider` resources |
| `totalVersions` | `int` | Total number of `Version` resources across all visible modules and providers |
| `totalStorageBytes` | `int64` | Sum of `VersionStatus.archiveSizeBytes` across all visible versions; `0` when no archive sizes have been recorded |
| `totalDownloads` | `int64` | Download events observed by Prometheus during the configured lookback window; `0` until a matching metric has been scraped |
| `syncHealth` | `SyncHealthStats` | Breakdown of version sync states |
| `securityPosture` | `SecurityPostureStats` | Aggregate finding counts across all visible resources |
| `storageDistribution` | `[]StorageBackendStat` | Per-backend version counts |
| `mostDownloaded` | `[]PopularResource` | Top 10 most-downloaded resources, filtered to the caller's visibility set |

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

Controls pre-signed URL generation for provider downloads. Set on `StorageConfig.presign`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | When `true`, download requests are redirected to the storage backend via a pre-signed URL instead of proxied through the server. |
| `ttl` | `duration` | `15m` | How long the pre-signed URL remains valid (e.g. `"15m"`, `"1h"`). |
| `fallbackToProxy` | `bool` | `true` | When `true`, if pre-sign generation fails the server falls back to proxying the download. Set to `false` to make pre-signing strictly required. |
