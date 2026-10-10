---
name: opendepot-testing
description: "Use when: running or choosing tests for OpenDepot code changes, running unit tests or envtest controller tests, running focused e2e tests with make test-e2e and E2E_FOCUS, picking which e2e suite to run for a changed path, checking Docker or Kind readiness before e2e, or triaging test failures such as Helm upgrade timeouts, GitHub App secret errors, or No space left on device in Kind. Triggers: 'run the tests', 'run e2e', 'focused e2e', 'which tests should I run', 'test failed', 'BeforeSuite failed', 'helm upgrade timed out', 'Kind cluster', 'disk full'."
argument-hint: "What changed, e.g. 'controller in services/version changed' or 'pkg/github changed'"
---

# OpenDepot Testing

The full e2e suite takes about 60 minutes and runs in CI (`.github/workflows/e2e.yaml`) on every PR. During development, run the smallest tier that covers the change. Stop at the lowest tier that proves the change works.

## Tier Ladder

1. **Compile and vet (every change).** In the affected module:
   ```bash
   go fmt ./...
   go vet ./...
   go test -count=1 -run '^$' ./...
   ```
   `-run '^$'` compiles every test package, including `_test.go` files, without running specs. `go build ./...` does not compile tests.

2. **Unit tests (every change).** Run the packages you touched:
   ```bash
   go test -count=1 ./pkg/github/...
   go test -count=1 -run TestName ./path/to/pkg
   ```
   Controller packages use envtest. Run `make test` in that service. It provisions `KUBEBUILDER_ASSETS` through `setup-envtest` and skips e2e packages. It also runs `manifests`, `generate`, `fmt`, and `vet` first, so it is slower than a single `go test`. For a quick loop, use `go test` with `KUBEBUILDER_ASSETS` set from `make setup-envtest`.

3. **Focused e2e (when controller behavior, CRDs, API responses, or Helm output change).** Run only the suites for the changed paths, with a focus:
   ```bash
   cd services/<service>
   make test-e2e E2E_FOCUS="<Describe or Context name>" KEEP_KIND=1 KIND_CLUSTER=opendepot-focus-<service>
   ```
   - `E2E_FOCUS` is a Ginkgo focus regex. It can join names with `|`.
   - `KEEP_KIND=1` keeps the Kind cluster so repeat runs skip teardown.
   - Use your own `KIND_CLUSTER` name. Do not reuse `opendepot` or `opendepot-test-e2e`, which may belong to someone else.
   - When you finish, delete only the cluster you created: `kind delete cluster --name opendepot-focus-<service>`.

4. **Full e2e (`make test-e2e` with no focus).** Do not run it during development. CI runs it. Run it locally only when the user asks.

## Path to Suite Map

Mirrors the `changes` filter in `.github/workflows/e2e.yaml` and `chart-validation.yaml`.

| Changed path | Run |
|---|---|
| `services/<svc>/**` | Tiers 1 and 2 in `services/<svc>`, then tier 3 for `<svc>` |
| `api/**` or `pkg/**` | Tiers 1 and 2 for the touched packages, then tier 3 for every e2e suite whose service imports them. When unsure, run all five suites focused. |
| `chart/**` or `test/chart/**` | `cd chart/opendepot && helm lint .`, then `cd test/chart && GOWORK=off go test ./... -count=1` |
| `services/ui/**` | Playwright workflow (`ui-playwright.yaml`). Not part of the Go e2e suites. |
| `services/agent/**` | Tiers 1 and 2 in `services/agent`, then tier 3 for `agent` |

The e2e suites are `agent`, `depot`, `module`, `provider`, `server`, and `version`.

## Finding Focus Names

Do not hand-copy focus names. They drift as specs change. Look them up:

```bash
grep -n -E '(Describe|Context)\("' services/<svc>/test/e2e/e2e_test.go
```

Pick the narrowest `Context` that covers your change. Use the parent `Describe` only when the change crosses contexts.

## Before Running E2e

1. **Docker is running:** `docker info >/dev/null`.
2. **Disk has room:** `docker system df`, and `df -h /` on the Docker host. A full disk causes `No space left on device` inside Kind pods.
3. **GitHub App variables are loaded.** The e2e suite reads `OPENDEPOT_*` values that live in zsh, not necessarily in the shell that runs `make`. Load them without echoing them:
   ```bash
   export OPENDEPOT_JEV_API_KEY="$(zsh -ic 'printf %s "$OPENDEPOT_JEV_API_KEY"')" \
     OPENDEPOT_GH_APP_ID="$(zsh -ic 'printf %s "$OPENDEPOT_GH_APP_ID"')" \
     OPENDEPOT_GH_INSTALL_ID="$(zsh -ic 'printf %s "$OPENDEPOT_GH_INSTALL_ID"')" \
     OPENDEPOT_GH_PRIVATE_KEY="$(zsh -ic 'printf %s "$OPENDEPOT_GH_PRIVATE_KEY"')"
   ```
4. **Helm chart boilerplate exists:** `services/<svc>/hack/boilerplate.go.txt`. If missing, copy it from `api/v1alpha1/hack/boilerplate.go.txt`.

## Running Long Commands

Focused e2e takes 1 to 4 minutes. Do not wait with `sleep`. Run it in the background and check the output:

- Start it with `bash` in `mode: async`, and keep working on other things.
- Check progress with `read_bash` and short `tail` or `grep` on the log.
- Look for `Ran N of M Specs`, `SUCCESS!`, `FAIL!`, and `exit=`.

Do not use `pkill`. It is blocked in this environment. Stop a process with `kill <PID>`, after finding the PID with `ps`.

## Failure Triage

| Symptom | Meaning | Action |
|---|---|---|
| `Failed to create the GitHub App Secret` in `BeforeSuite` | `OPENDEPOT_GH_*` variables missing in the shell | Load them (step 3 above) and rerun |
| `Failed to upgrade Helm release` with `context deadline exceeded` | Pods did not become ready within `--wait --timeout 3m` | Check pods and events in the test namespace (below) |
| `Init:Error` or `BackOff` on `init-service-account` with `No space left on device` | Docker or Kind disk is full | Check disk. Prune only after asking the user, and do not delete volumes without asking |
| `Failed to upgrade Helm release`, pods healthy in another cluster | Environment-specific | Compare with the other cluster. Do not change the chart to get past it |
| Failure also fails on `main` | Pre-existing | Confirm once in a temporary `git worktree` of `main`, remove it, and report with evidence |
| Failure caused by your diff | Fix it | Re-run only the failing focused test |

Useful checks for a Helm timeout, with `<ctx>` set to `kind-<cluster>`:

```bash
kubectl --context <ctx> get pods -n opendepot-system
kubectl --context <ctx> get events -n opendepot-system --sort-by=.lastTimestamp | tail -20
kubectl --context <ctx> logs <pod> -n opendepot-system -c init-service-account
```

Check disk on the Kind node:

```bash
docker exec <cluster>-control-plane df -h /
```

## Cleanup and Safety

- Keep the Kind cluster between focused runs. Use `KEEP_KIND=1` and the same `KIND_CLUSTER` name for every run of that service. Repeat runs then skip cluster creation and teardown, and the setup cost is paid once.
- Delete the cluster only when you are done with that service's focused tests. Delete only clusters you created, and confirm the name before `kind delete cluster --name <name>`.
- Do not run `docker volume prune` or `docker system prune --volumes`. Ask the user first.
- Do not `sleep` to wait on a process.
- Remove temporary logs and scripts you created under `/tmp`.

## Reporting

For each test run, report:
- Tier and command
- Focus name, if any
- Pass or fail counts (for example, `Ran 1 of 29 Specs`)
- Each failure classified as caused by this diff, pre-existing (with evidence), or environment
