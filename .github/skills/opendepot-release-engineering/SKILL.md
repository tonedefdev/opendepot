---
name: opendepot-release-engineering
description: "Use when: pushing or rebasing a stacked release branch set, verifying layer ancestry, force-pushing feature layers safely, opening chained stacked PRs with gh pr create, checking CI on every PR in a wave, triaging failing GitHub Actions checks, auditing chart RBAC for new controllers or CRD watches, or checking that build, e2e, and release workflows cover new services or e2e suites. Triggers: 'push the stack', 'stacked PR', 'open PRs for the layers', 'gh pr checks', 'CI failed', 'e2e failed on PR', 'force-with-lease', 'merge-base', 'RBAC forbidden', 'cannot list resource', 'release workflow', 'wave ready'."
argument-hint: "What to do, e.g. 'push layers 20-23' or 'check CI for PRs 133-136'"
---

# OpenDepot Release Engineering

Release work for OpenDepot is a stack of feature layers. Each layer branch is based on the layer below it, and each PR targets the layer below as its base. This skill covers how to keep that stack valid, how to push and open PRs, how to gate a wave on CI, and how to check the chart and CI workflows for coverage gaps.

## Rules

- **Never push to or force-push `main`.** Force pushes are allowed only on feature layer branches (`release-<version>/NN-<name>`), and only with `--force-with-lease`.
- **Never add a `Co-authored-by` trailer or any Copilot author attribution** to commits, PR bodies, or PR comments.
- **Stop on any rebase conflict.** Run `git rebase --abort`, then report the conflicting layer and files. Do not resolve conflicts silently.
- **Do not touch layers outside the wave you were asked to push.** Check the scope before you rebase.
- **Do not bump chart versions during an unreleased release.** Follow the Standing Decisions in the agent instructions.
- **Never mark a wave ready while any PR has failing checks.** See the Post-Push CI Gate.

## 1. Verify Stack Ancestry

Before pushing, confirm that each layer contains the layer below it. Run this from the repo root, with `<root>` set to the release prefix (for example `release-0.13.0`):

```bash
git merge-base --is-ancestor <root>/20-agent-mcp-controller <root>/21-version-mcp-sync && echo ok
git merge-base --is-ancestor <root>/21-version-mcp-sync <root>/22-depot-oci-mcp-discovery && echo ok
git merge-base --is-ancestor <root>/22-depot-oci-mcp-discovery <root>/23-server-agents-mcp && echo ok
```

A missing `ok` means the stack is broken. Rebase the upper layer onto the lower one before pushing:

```bash
git switch <root>/21-version-mcp-sync
git rebase <root>/20-agent-mcp-controller
```

If the rebase stops with a conflict, run `git rebase --abort` and report.

Rebases report `skipped previously applied commit` for commits whose patch is already in the new base. This is expected after a lower layer was rebased. Check the final `Successfully rebased` line and `git --no-pager status --porcelain`.

## 2. Push Feature Layers

Push each feature layer with a lease, so a push never overwrites a remote commit you have not seen:

```bash
git push --force-with-lease origin release-0.13.0/20-agent-mcp-controller
git push --force-with-lease origin release-0.13.0/21-version-mcp-sync
git push --force-with-lease origin release-0.13.0/22-depot-oci-mcp-discovery
git push --force-with-lease origin release-0.13.0/23-server-agents-mcp
```

Rules for pushes:

- Use `--force-with-lease` only, never `--force`.
- Push the lower layers first, so each PR base exists on the remote in the right order.
- After pushing, confirm the working tree is clean on the top layer: `git --no-pager status --porcelain` must print nothing.
- Leave the repo on the top layer of the wave.

## 3. Open Chained Stacked PRs

Each PR targets the layer below it. The bottom PR targets the wave's root branch or `main`, depending on the stack:

```bash
gh pr create --base release-0.13.0/20-agent-mcp-controller \
  --head release-0.13.0/21-version-mcp-sync \
  --title "feat(version): <summary> (0.13.0 Step NN)" \
  --body-file <body.md>
```

PR body requirements:

- Include a **stack table** that lists every layer in the wave, its PR number, its branch, and its base. Use the same table in every PR of the stack, and mark the current PR.
- Describe only the work in that layer.
- Contain no Copilot attribution and no `Co-authored-by` lines.

Standard stack table:

| Layer | PR | Branch | Base |
|-------|----|--------|------|
| 20 | #133 | `release-0.13.0/20-agent-mcp-controller` | `release-0.13.0/19-chart-oci-builds` |
| 21 | #134 | `release-0.13.0/21-version-mcp-sync` | `release-0.13.0/20-agent-mcp-controller` |
| 22 | #135 | `release-0.13.0/22-depot-oci-mcp-discovery` | `release-0.13.0/21-version-mcp-sync` |
| 23 | #136 | `release-0.13.0/23-server-agents-mcp` | `release-0.13.0/22-depot-oci-mcp-discovery` |

When a PR's base changes, update the body so the table still matches the stack. Check the base with `gh pr view <n> --json baseRefName`.

## 4. Mandatory Post-Push CI Gate

Pushing does not finish a wave. After the push, check every PR in the wave, not only the top one:

```bash
gh pr checks <n>
```

Repeat until every check reaches a terminal state (`pass`, `fail`, `skipping`, or `cancel`). A check that is still `pending` or `in_progress` is not done.

For each failing check:

1. Get the failed job log:
   ```bash
   gh run view <run-id> --log-failed
   ```
   Find the run ID from the check link (`.../actions/runs/<run-id>/job/<job-id>`), or from `gh run list --branch <layer-branch>`.
2. Classify the failure:
   - **Caused by the stack.** Examples: `forbidden: User "system:serviceaccount:..." cannot list resource ...`, a `NotFound` that never recovers because the controller cannot read its source CRD, a unit test that depends on code in a lower layer. Fix it in the layer that owns the cause, then push that layer with `--force-with-lease` and rebase the layers above it.
   - **Transient infrastructure flake.** Examples: an HTTP 5xx from a registry or API during image pull, a runner setup timeout, a network reset that has no code cause. Rerun only the failed jobs:
     ```bash
     gh run rerun <run-id> --failed
     ```
     Rerun once. If the same job fails again, classify it as stack-caused and investigate.
3. Record each classification and its evidence in the wave report.

**Never return a wave as ready for user review while any PR has a failing check.** A wave is ready only when every PR in it shows all checks passing or skipped, and every failure has been fixed or confirmed as a transient flake that passed on rerun.

## 5. RBAC and CI Workflow Coverage Checklist

Run this checklist for every layer that changes controllers, CRDs, services, images, or e2e suites.

### RBAC

- [ ] A layer that registers a new controller or watch, or that reads a new CRD, adds the chart RBAC rules **in that same layer**. Never defer RBAC to a later wave when the binary already lists or watches the CRD.
- [ ] Kubebuilder `// +kubebuilder:rbac` markers in the service match the rules in `chart/opendepot/templates/<service>-rbac.yaml`. Each marker resource and verb appears in the template for the same service role.
- [ ] Each service that reads a CRD has that CRD's `get`, `list`, and `watch` rules, including reads done by the server, depot, and version controllers, not only the owning controller.
- [ ] When a template rule is gated (for example by `server.policyManagement.enabled`), render the chart with the gate both on and off and confirm the verbs stay on the right resource:
  ```bash
  helm template x chart/opendepot --set server.policyManagement.enabled=true --show-only templates/server-rbac.yaml
  helm template x chart/opendepot --set server.policyManagement.enabled=false --show-only templates/server-rbac.yaml
  ```
- [ ] Run `helm lint chart/opendepot` and `cd test/chart && GOWORK=off go test -count=1 ./...`.

A `forbidden` line in an e2e log is an RBAC gap until proven otherwise. Check the chart role for the service account named in the error first.

### CI Workflows

Check these three files whenever a layer adds a service, an image variant, or an e2e suite:

- `.github/workflows/build.yaml`: the build matrix lists each service image, its path, and any variant such as `server` with a tag suffix. A new image must appear here, with its Dockerfile path.
- `.github/workflows/e2e.yaml`: the path filter in **Detect E2E scope** lists each service that has an e2e job, and the matrix maps each service to its `workdir`, `image`, and `images`. A new e2e suite needs a matrix entry in the same layer that lands the suite. If the suite is deferred to a later wave, record the deferral explicitly in the wave report.
- `.github/workflows/release.yaml`: release packaging and chart publishing must still cover the new images and chart values.

Also check that new e2e files are wired into the workflow when their wave lands. For example, a new `mcpserver_e2e_test.go` in `services/agent/test/e2e/`, or `oci` and `oci-builds` suites in a service, must have a matching job or focus in `e2e.yaml`.

Known gap to track: the e2e path filter does not list `services/oci/**`, while `build.yaml` builds an `oci-controller` image from `services/oci`. Record any change to that path in the wave report and confirm the e2e filter before relying on it.

### Recording Deferrals

When CI wiring is intentionally deferred to Wave 3, write each deferral in the wave report with four things: the suite or workflow, the layer that will wire it, the reason, and the tracking note. Do not leave a deferral unrecorded.

## 6. Wave Report

Report each wave with:

- The stack table with PR numbers, branches, bases, and commit SHAs.
- Ancestry check results.
- The CI result for each PR, with failing checks classified (stack-caused, flake, or environment) and the fix or rerun for each.
- RBAC and workflow coverage results, including deferrals.
- Whether the wave is ready for user review. It is ready only when the Post-Push CI Gate passes for every PR.
