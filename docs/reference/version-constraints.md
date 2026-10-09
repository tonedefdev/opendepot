---
tags:
  - reference
  - version-constraints
---

# Version Constraints

OpenDepot supports all standard OpenTofu version constraint syntax:

| Syntax | Example | Meaning |
|--------|---------|---------|
| Exact | `1.2.0` | Only version 1.2.0 |
| Comparison | `>= 1.0.0, < 2.0.0` | Any 1.x version |
| Pessimistic | `~> 1.2.0` | >= 1.2.0, < 1.3.0 (bugfixes only) |
| Pessimistic (minor) | `~> 1.2` | >= 1.2.0, < 2.0.0 |
| Exclusion | `>= 1.0.0, != 1.5.0` | Any 1.x except 1.5.0 |

## Tags and branches

Skill and Agent sources use `versionConstraints` to select Git tags, or `ref` to follow a Git branch. The two are mutually exclusive.

- **Tags (`versionConstraints`)** are the recommended default for stable content. Tags are usually more immutable than branches.
- **Branches (`ref`)** publish a new Version on every push to the branch, once all checks pass. A branch head moves with every push. Each Version is still pinned to its exact `sourceCommit`.

`immutable` works on branch sources. A re-download must match the Version's recorded `status.checksum`, and a moving branch creates new Versions without changing existing ones. For the full behavior, see [Branch sourcing](../guides/agents.md#branch-sourcing).

