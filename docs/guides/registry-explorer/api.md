---
tags:
  - guides
  - ui
  - api
---

# Registry Explorer Browse API

The browse endpoints power the Registry Explorer and can also be called directly
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
each depot to the resources it manages. The Registry Explorer uses this endpoint
to render the Depots page.