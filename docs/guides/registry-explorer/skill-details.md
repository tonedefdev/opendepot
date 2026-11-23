---
tags:
  - guides
  - ui
  - registry-explorer
  - skill
---

# Skill Details

The Skill Details page shows one Skill: its latest version, the `opendepot.hcl` snippet for each supported client, its frontmatter and `SKILL.md` definition, versions, and scan findings.

![OpenDepot Workshop Skill Details page showing the skill header, status, and opendepot.hcl snippet](../../img/workshop-skill-details.png#only-dark)
![OpenDepot Workshop Skill Details page showing the skill header, status, and opendepot.hcl snippet](../../img/workshop-skill-details-light.png#only-light)

## Skill identity and status

The breadcrumb shows the namespace and kind. The header shows the skill name, the latest synchronized version, the source ref and commit, and sync status. When Jev is enabled, the header also shows the risk level from the latest Jev assessment.

## Client configuration

The **opendepot.hcl** section provides a copyable `agent_skill` block for the skill. Choose a tab to match your client:

- **Claude**
- **Copilot**
- **OpenAI**
- **Generic**

Copy the snippet into your `opendepot.hcl` file. See [Consuming Agents and Skills](../agents.md) for how the block is used.

## Jev Assessment

When Jev is enabled and the latest version has an assessment, the **Jev Assessment** section shows the risk level, risk score, confidence, a probability for each risk category, and the files that were assessed. See [Jev Assessment](../agents-jev.md) for how to read the results.

![Jev assessment panel for a Skill Version showing risk level, risk score, and confidence](../../img/skill-jev-assessment.png#only-dark)
![Jev assessment panel for a Skill Version showing risk level, risk score, and confidence](../../img/skill-jev-assessment-light.png#only-light)

## Frontmatter and definition

The **Frontmatter** section shows the metadata parsed from the top of `SKILL.md`, such as `name`, `description`, `license`, `compatibility`, and `metadata`.

![OpenDepot Workshop Skill Frontmatter card showing parsed name, description, and metadata fields](../../img/workshop-skill-frontmatter.png#only-dark)
![OpenDepot Workshop Skill Frontmatter card showing parsed name, description, and metadata fields](../../img/workshop-skill-frontmatter-light.png#only-light)

The **SKILL.md** section renders the skill definition from the latest version.

![OpenDepot Workshop SKILL.md card showing the rendered skill definition](../../img/workshop-skill-definition.png#only-dark)
![OpenDepot Workshop SKILL.md card showing the rendered skill definition](../../img/workshop-skill-definition-light.png#only-light)

## Overview, storage, GitHub, and Depot settings

The **Overview** section lists the provider, repository owner, source path, version history limit, and version constraints. The source path is the location of the skill within its repository.

The **Storage Configuration** section shows the backend and its location fields. The **GitHub Configuration** section shows the authenticated client used for source metadata, if one is configured.

The **Depot** section appears only when the skill is managed by a `Depot`. It names the `Depot` that manages the skill. Skills that you manage through GitOps do not have a Depot section.

## Versions

The **Versions** view lists the synchronized versions of the skill with their sync status, resource name, archive filename, commit, checksum, last scan time, and findings. Use the search field and **Sync status** filter to narrow the list, and use the pagination controls to move between pages.

![OpenDepot Workshop Skill Versions view showing the synchronized version, commit, checksum, and pagination](../../img/workshop-skill-versions.png#only-dark)
![OpenDepot Workshop Skill Versions view showing the synchronized version, commit, checksum, and pagination](../../img/workshop-skill-versions-light.png#only-light)

## Scan Findings

The **Scan Findings** section lists the source scan findings for the selected version, with the count in the heading. Use the version selector to view findings for another version.

## Related views

- [Workshop](index.md)
- [Agent Details](agent-details.md)
- [Browse resources and dashboards](browse.md)
- [Consuming Agents and Skills](../agents.md)
- [Workshop authentication](../../authentication/registry-explorer.md)
