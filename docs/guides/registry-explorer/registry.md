---
tags:
  - guides
  - ui
  - registry-explorer
  - browse
---

# Registry Explorer

The Registry Explorer is OpenDepot Workshop's discovery surface. It gives users
an overview of the modules, providers, skills, and agents available to them and
lets them open resource details, versions, and security information.

![OpenDepot Workshop Registry Explorer showing modules, providers, versions, and scan findings](../../img/registry-explorer.png)

## Sidebar

The sidebar groups the Registry Explorer under a **Registry** item. The
children filter the explorer by kind:

| Item | Shows |
| --- | --- |
| Registry | All kinds: agents, skills, modules, and providers. |
| Agents | Only agents. |
| Skills | Only skills. |
| Modules | Only modules. |
| Providers | Only providers. |

Each item has an icon. When the sidebar is collapsed, only the icons appear.
Hover over an icon to see its name in a tooltip.

## Reading the Registry Explorer

Each resource card summarizes the latest available version:

| Card attribute | Meaning |
| --- | --- |
| Namespace | The Kubernetes namespace that owns the resource. |
| Resource name | The name used to identify the module, provider, skill, or agent. Select the card to open its details. |
| Provider icon | The provider ecosystem associated with the resource, such as AWS. Modules and providers only. |
| Type | The resource kind: `Module`, `Provider`, `Skill`, or `Agent`. Each kind has its own chip color. |
| Source | For a Skill or Agent that follows a branch, the tracked branch and source path. Select the branch to open it on GitHub. |
| Version | The latest version currently shown for the resource. |
| Sync status | Whether the resource version was successfully synchronized or needs attention. |
| Finding badges | The number of scan findings by severity: `C` critical, `H` high, `M` medium, `L` low, and `U` unknown. |

The footer shows how many resources are displayed, the selected page size, and
pagination controls. Change the page size or move between pages to browse the
full set of resources visible to you.

## What users see

Depending on their authentication and visibility permissions, users can expect
to find:

- **Modules** with versions, README content, usage examples, contracts, inputs,
  outputs, and scan findings.
- **Providers** with versions, platform artifacts, checksums, extracted schemas,
  and source or binary scan findings.
- **Skills** and **Agents** with versions, the `SKILL.md` or `AGENT.md`
  definition, tools and model metadata, scan findings, and Jev assessments when
  enabled. See [Consuming Agents and Skills](../agents.md).

The Registry Explorer shows the resources the current user can access. If a
resource is not visible, check the current authentication mode, public labels,
and any applicable `GroupBinding`.

## Related Guides

- [OpenDepot Workshop](index.md)
- [Module Details](module-details.md)
- [Provider Details](provider-details.md)
- [Consuming Agents and Skills](../agents.md)
- [OpenDepot Workshop Administration](administration.md)
- [OpenDepot Workshop authentication](../../authentication/registry-explorer.md)
