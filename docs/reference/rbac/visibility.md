---
tags:
  - reference
  - rbac
  - groupbinding
  - registry-explorer
---

# GroupBinding and Stats Visibility

The Registry Explorer Stats page, `GET /opendepot/ui/v1/stats`, aggregates only
resources visible to the authenticated user.

For OIDC users with a matching `GroupBinding`, module and provider counts,
version counts, storage bytes, and security findings are scoped to the resources
permitted by that binding.

Users without a matching `GroupBinding`, including users in anonymous-auth or
bearer-token mode, see all resources in labelled namespaces.

See [GroupBinding Access Control](../../guides/groupbinding.md) for the full
expression syntax and setup instructions.
