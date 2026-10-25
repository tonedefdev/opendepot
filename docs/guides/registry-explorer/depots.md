---
tags:
  - guides
  - ui
  - registry-explorer
  - depots
---

# Depots

The **Depots** page (`/depots`) renders an interactive graph of visible `Depot`
resources and the `Module`, `Provider`, `Skill`, and `Agent` resources each depot
manages. Every Module, Provider, Skill, and Agent with versions fans out to one
node per version.

![OpenDepot Workshop Depots page showing registry storage locations](../../img/registry-depots.png)

```mermaid
graph LR
  D[Depot] -->|manages| M1[Module A]
  D -->|manages| P[Provider]
  D -->|manages| S[Skill]
  D -->|manages| A[Agent]
  M1 --> V1[v3.19.0]
  S --> SV[v0.9.0]
  A --> AV[v0.9.0]
```

Each node opens a detail panel showing storage, polling, versions, sync status,
and scan severity counts. A namespace selector narrows the graph to one
Kubernetes namespace. Skill and Agent versions without a version string are
labeled with their short commit SHA.

The graph uses the [`GET /opendepot/ui/v1/depots/graph`](../../reference/api.md#depot-relationship-graph)
browse endpoint and applies the same visibility rules as the rest of the UI.
