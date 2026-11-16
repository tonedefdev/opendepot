---
tags:
  - guides
  - ui
  - registry-explorer
  - agent
---

# Agent Details

The Agent Details page shows one Agent: its latest version, the `opendepot.hcl` snippet for each supported client, its frontmatter and `AGENT.md` definition, tools and model metadata, versions, and scan findings.

![OpenDepot Workshop Agent Details page showing the agent header, status, and opendepot.hcl snippet](../../img/workshop-agent-details.png#only-dark)
![OpenDepot Workshop Agent Details page showing the agent header, status, and opendepot.hcl snippet](../../img/workshop-agent-details-light.png#only-light)

## Agent identity and status

The breadcrumb shows the namespace and kind. The header shows the agent name, the latest synchronized version, the source ref and commit, and sync status. When Jev is enabled, the header also shows the risk level from the latest Jev assessment.

## Client configuration

The **opendepot.hcl** section provides a copyable `agent` block for the agent. Choose a tab to match your client:

- **Claude**
- **Copilot**
- **OpenAI**
- **Generic**

Copy the snippet into your `opendepot.hcl` file. See [Consuming Agents and Skills](../agents.md) for how the block is used.

## Jev Assessment

When Jev is enabled and the latest version has an assessment, the **Jev Assessment** section shows the risk level, risk score, confidence, a probability for each risk category, and the files that were assessed. See [Jev Assessment](../agents-jev.md) for how to read the results.

![Jev assessment panel for an Agent Version showing Low risk, a risk score, and confidence](../../img/agent-jev-assessment.png#only-dark)
![Jev assessment panel for an Agent Version showing Low risk, a risk score, and confidence](../../img/agent-jev-assessment-light.png#only-light)

## Frontmatter and definition

The **Frontmatter** section shows the metadata parsed from the top of `AGENT.md`, including the `name`, `description`, and `tools` fields.

![OpenDepot Workshop Agent Frontmatter card showing parsed name and description fields](../../img/workshop-agent-frontmatter.png#only-dark)
![OpenDepot Workshop Agent Frontmatter card showing parsed name and description fields](../../img/workshop-agent-frontmatter-light.png#only-light)

The **AGENT.md** section renders the agent definition from the latest version.

![OpenDepot Workshop AGENT.md card showing the rendered agent definition](../../img/workshop-agent-definition.png#only-dark)
![OpenDepot Workshop AGENT.md card showing the rendered agent definition](../../img/workshop-agent-definition-light.png#only-light)

## Tools and Model

The **Tools and Model** section appears only when the agent declares a model or tools. It lists the tools from the frontmatter as chips. When the agent sets a model, the section also shows the model.

![OpenDepot Workshop Tools and Model card listing the tools declared by an agent](../../img/workshop-agent-tools-model.png#only-dark)
![OpenDepot Workshop Tools and Model card listing the tools declared by an agent](../../img/workshop-agent-tools-model-light.png#only-light)

## Overview, storage, GitHub, and Depot settings

The **Overview** section lists the provider, repository owner, source path, version history limit, and version constraints.

The **Storage Configuration** section shows the backend and its location fields. The **GitHub Configuration** section shows the authenticated client used for source metadata, if one is configured.

The **Depot** section appears only when the agent is managed by a `Depot`. It names the `Depot` that manages the agent. Agents that you manage through GitOps do not have a Depot section.

## Versions

The **Versions** view lists the synchronized versions of the agent with their sync status, resource name, archive filename, commit, checksum, last scan time, and findings. Use the search field and **Sync status** filter to narrow the list, and use the pagination controls to move between pages.

![OpenDepot Workshop Agent Versions view showing the filter bar, synchronized versions, and pagination](../../img/workshop-agent-versions.png#only-dark)
![OpenDepot Workshop Agent Versions view showing the filter bar, synchronized versions, and pagination](../../img/workshop-agent-versions-light.png#only-light)

## Scan Findings

The **Scan Findings** section lists the source scan findings for the selected version, with the count in the heading. Use the version selector to view findings for another version.

## Related views

- [Workshop](index.md)
- [Skill Details](skill-details.md)
- [Browse resources and dashboards](browse.md)
- [Consuming Agents and Skills](../agents.md)
- [Workshop authentication](../../authentication/registry-explorer.md)
