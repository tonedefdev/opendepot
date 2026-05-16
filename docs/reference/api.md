---
tags:
  - reference
  - api
---

# API Reference

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
    "authz": "https://dex.example.com/dex/auth",
    "token": "https://dex.example.com/dex/token",
    "ports": [10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008, 10009, 10010]
  }
}
```

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
  "verification_uri": "https://dex.example.com/device",
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
POST /dex/token
grant_type=authorization_code&code=<AUTH_CODE>&client_id=<CLIENT_ID>&redirect_uri=http://localhost:10000
```

**Request (device_code grant):**

```
POST /dex/token
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

Streams the provider binary archive (`.zip`) directly from storage. Does **not** require client authentication — the server uses its own ServiceAccount per the Terraform Provider Registry Protocol.

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

## Kubernetes Resource Types

### SecurityFinding

Represents a single vulnerability finding from a Trivy scan.

| Field | Type | Description |
|---|---|---|
| `vulnerabilityID` | `string` | CVE or GHSA identifier for the vulnerability |
| `pkgName` | `string` | Name of the package containing the vulnerability |
| `installedVersion` | `string` | Version of the package currently in use |
| `fixedVersion` | `string` | Minimum version that resolves the vulnerability, if known |
| `severity` | `string` | `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`, or `UNKNOWN` |
| `title` | `string` | Short description of the vulnerability |

### ProviderBinaryScan

Holds Trivy binary scan (`trivy rootfs`) results for a specific provider artifact. Stored in `Version.status.binaryScan`. Each OS/architecture binary is scanned independently because Go stdlib versions and runtime dependencies may differ between compiled artifacts.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the binary scan completed |
| `findings` | `[]SecurityFinding` | Vulnerabilities found in the compiled provider binary |

### ProviderSourceScan

Holds Trivy source scan (`trivy fs`) results for a provider's `go.mod` dependencies. Stored in `Provider.status.sourceScan`. Deduplicated across OS/architecture `Version` resources because all variants share the same source code.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the source scan completed |
| `version` | `string` | Provider version that was scanned (used for deduplication) |
| `findings` | `[]SecurityFinding` | Vulnerabilities found in the provider's source dependencies (go.mod) |

### ProviderConfig fields

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | The organisation namespace in the OpenTofu registry (e.g. `hashicorp`, `integrations`, `DataDog`). Defaults to `hashicorp`. Used for binary download and source repository lookup. Existing `Provider` resources without this field continue to work unchanged. |
| `sourceRepository` | `string` | Full GitHub URL of the provider's source repository (e.g. `https://github.com/hashicorp/terraform-provider-aws`). When omitted, OpenDepot queries the OpenTofu registry (`api.opentofu.org`) for the repository URL, falling back to `https://github.com/{namespace}/terraform-provider-{name}` if the registry lookup fails. Set this field to override an incorrect or unavailable registry result. |

### VersionStatus fields

| Field | Type | Description |
|---|---|---|
| `binaryScan` | `ProviderBinaryScan` | Binary vulnerability scan result for this specific provider artifact. Populated only for provider `Version` resources when scanning is enabled. |
| `sourceScan` | `ModuleSourceScan` | IaC scan result for this module archive. Populated only for module `Version` resources when scanning is enabled. |

### ProviderStatus fields

| Field | Type | Description |
|---|---|---|
| `sourceScan` | `ProviderSourceScan` | Most recent source vulnerability scan result. Populated by the Version controller after scanning the provider's `go.mod`. Deduplicated across all OS/architecture `Version` resources for the same provider version. |

### ModuleSourceScan

Holds Trivy IaC scan (`trivy fs`) results for a module archive. Stored in `Version.status.sourceScan`. Findings represent HCL misconfigurations detected by Trivy's config-class rules.

| Field | Type | Description |
|---|---|---|
| `scannedAt` | `string` | RFC3339 timestamp at which the IaC scan completed |
| `findings` | `[]SecurityFinding` | Misconfigurations found in the module's HCL source. `vulnerabilityID` contains a Trivy rule ID (e.g. `AVD-AWS-0057`) rather than a CVE. |

### GroupBinding

`GroupBinding` is a namespaced resource that grants a group of OIDC users access to specific modules and providers. The server evaluates all GroupBindings in alphabetical order by name and applies the first one whose `expression` matches the user's groups claim. If an expression fails to compile or evaluate, the request is denied with `403 Forbidden`. Requires OIDC authentication to be enabled.

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

### PresignConfig fields

Controls pre-signed URL generation for provider downloads. Set on `StorageConfig.presign`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | When `true`, download requests are redirected to the storage backend via a pre-signed URL instead of proxied through the server. |
| `ttl` | `duration` | `15m` | How long the pre-signed URL remains valid (e.g. `"15m"`, `"1h"`). |
| `fallbackToProxy` | `bool` | `true` | When `true`, if pre-sign generation fails the server falls back to proxying the download. Set to `false` to make pre-signing strictly required. |

