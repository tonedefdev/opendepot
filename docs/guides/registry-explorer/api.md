---
tags:
  - guides
  - ui
  - api
---

# OpenDepot Workshop Browse API

The browse endpoints power OpenDepot Workshop and can also be called directly
for integrations and automation. In OIDC mode with anonymous access disabled,
requests require an `Authorization: Bearer <token>` header.

## List Namespaces

```http
GET /opendepot/ui/v1/namespaces
```

Returns namespaces carrying the `opendepot.defdev.io/public=true` label. System
namespaces are never returned.

```json
{
  "items": [
    { "name": "opendepot-system", "public": true }
  ]
}
```

## List Resources

```http
GET /opendepot/ui/v1/resources
```

Returns a paginated list of visible `Module` and `Provider` resources.

| Parameter | Type | Description |
|-----------|------|-------------|
| `namespace` | string (repeatable) | Namespace filter |
| `kind` | string | `module` or `provider` |
| `q` | string | Name search |
| `synced` | bool | Synced or unsynced resources |
| `os` / `arch` | string | Provider platform filters |
| `severity` | string | Minimum finding severity |
| `public_only` | bool | Public resources only |
| `sort_by` / `sort_dir` | string | Sort field and direction |
| `page` / `page_size` | int | Pagination controls |

## Resource Detail

```http
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}
```

Returns full detail for one resource, including versions and latest-version scan
findings. The `readmeContent` field is present for modules when a README was
resolved.

## List Resource Versions

```http
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/versions
```

Returns a paginated, filtered version list. Supported query parameters include
`page`, `page_size`, `q`, `synced`, `os`, and `arch`. Provider responses also
include `availableOS` and `availableArch` for filter controls.

## Resource Scan Findings

```http
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/scan-findings
```

Returns findings for the latest scanned version. Use `?version=<semver>` for a
specific source scan or `?binaryVersion=<semver>` for a provider binary scan.
See the [API Reference](../../reference/api.md#resource-scan-findings) for the
full response schema.

## Depot Relationship Graph

```http
GET /opendepot/ui/v1/depots/graph
```

Returns visible `Depot`, `Module`, and `Provider` resources with edges connecting
each depot to the resources it manages. OpenDepot Workshop uses this endpoint
to render the Depots page.

## Module Contract

```http
GET /opendepot/ui/v1/resources/{namespace}/{kind}/{name}/contract
```

Returns the Assembly Line input contract for a module version. Only `module` is
supported. See the [API Reference](../../reference/api.md#module-contract) for
query parameters and responses.

## Provider Configuration Schema

```http
GET /opendepot/ui/v1/resources/{namespace}/provider/{name}/provider-schema
```

Returns the provider configuration schema for a version. Available only when
Assembly Line is enabled. See the
[API Reference](../../reference/api.md#provider-configuration-schema).

## Assembly Line Export

```http
POST /opendepot/ui/v1/assembly/export
```

Validates a canvas and returns a root module as a ZIP archive, or as a stream of
progress events when the request sends `Accept: application/x-ndjson`. Available
only when Assembly Line is enabled. See the
[API Reference](../../reference/api.md#assembly-line-export) for the request
schema and error codes.

## List Depots

```http
GET /opendepot/ui/v1/depots
```

Returns visible `Depot` resources. For the connected view, use the
[Depot Relationship Graph](#depot-relationship-graph). See the
[API Reference](../../reference/api.md#list-depots).

## Registry Stats

```http
GET /opendepot/ui/v1/stats
```

Returns the aggregate data behind the Stats page. See the
[API Reference](../../reference/api.md#registry-stats).

## Scan Policies

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/opendepot/ui/v1/scan-policies/catalog` | Authorized namespaces and onboarded targets |
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}` | List policies in a namespace |
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Get one policy |
| `GET` | `/opendepot/ui/v1/scan-policies/{namespace}/capabilities` | Write support for the namespace |
| `POST` | `/opendepot/ui/v1/scan-policies/{namespace}/preview` | Validate without saving |
| `POST` | `/opendepot/ui/v1/scan-policies/{namespace}` | Create a policy |
| `PUT` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Replace a policy (requires `If-Match`) |
| `DELETE` | `/opendepot/ui/v1/scan-policies/{namespace}/{name}` | Delete a policy (requires `If-Match`) |

Write routes require policy management to be enabled and anonymous access to be
off. See the [API Reference](../../reference/api.md#scanpolicy).
