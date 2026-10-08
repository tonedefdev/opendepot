---
tags:
  - guides
  - ui
   - opendepot-workshop
---

# OpenDepot Workshop

OpenDepot Workshop is a browsable, searchable frontend for OpenDepot. The
Workshop includes the Registry Explorer, resource details, Depots, Stats,
Assembly Line, and Security Policies. Enable it by setting `ui.enabled: true`
in the Helm chart.

!!! note
    The UI is disabled by default. Existing server-only deployments are
    unaffected when `ui.enabled` remains `false`.

## Workshop components

OpenDepot Workshop brings the following user-facing workflows together:

- [**Registry Explorer**](registry.md) — discover modules and providers, then
  open their versions and resource details.
- [**Module Details**](module-details.md) and [**Provider Details**](provider-details.md)
  — inspect README content, contracts, schemas, artifacts, versions, and scan
  findings.
- [**Depots**](depots.md) — review where resources are managed and how they are
  discovered.
- [**Stats**](browse.md) — compare synchronization health, resource counts,
  storage, and security findings.
- [**Assembly Line**](../assembly-line.md) — compose onboarded modules and
  providers into a validated, downloadable root module.
- [**Security Policies**](security-policies.md) — review and manage scan
  policies when policy management is enabled and you have the required
  permissions.

The components follow the same authentication and visibility rules. If a
resource or workflow is not visible, check the current authentication mode,
public labels, and any applicable `GroupBinding`.

## Navigate OpenDepot Workshop

Use the Workshop components as the starting point for a focused workflow:

1. Select a **module** in the Registry Explorer to open [Module Details](module-details.md).
   Review its README and usage block, then inspect its normalized contract,
   configuration, versions, and scan findings.
2. Select a **provider** in the Registry Explorer to open [Provider Details](provider-details.md).
   Review provider identity and synchronization status, then drill into
   platform versions and source or binary findings.
3. Select a **version** from either details page to inspect the exact archive,
   checksum, scan timestamp, and findings associated with that version.
4. Open a **Depot** to understand which storage backend and discovery settings
   manage its modules and providers.
5. Open **Stats** to compare synchronization health and security posture across
   the resources visible to you.
6. Open **Assembly Line** to compose onboarded modules and providers into a
  validated, downloadable root module.
7. Open **Security Policies** to review or manage scan policies when policy
  management is enabled and you have the required permissions.

## Related Guides

- [Registry Explorer](registry.md)
- [Module Details](module-details.md)
- [Provider Details](provider-details.md)
- [OpenDepot Workshop Administration](administration.md)
- [Stats](browse.md)
- [Browse API](api.md)
- [OpenDepot Workshop authentication](../../authentication/registry-explorer.md)