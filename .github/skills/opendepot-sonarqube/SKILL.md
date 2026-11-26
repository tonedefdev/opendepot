---
name: opendepot-sonarqube
description: "Use when: running a SonarQube static analysis scan on OpenDepot, checking whether the local SonarQube instance is up, validating SONAR_TOKEN, reading open sonar issues, filtering findings to changed lines, or triaging static analysis results. Triggers: 'run sonarqube', 'sonar scan', 'sonar analysis', 'sonar issues', 'static analysis', 'SONAR_TOKEN', 'quality gate', 'sonarqube not running', 'hotspots', 'sonar-scanner not found', 'sonar 401', 'sonar 403'."
argument-hint: "What to check, e.g. 'scan my uncommitted changes' or 'list sonar issues on changed lines'"
---

# OpenDepot SonarQube

Local SonarQube runs in Docker and is used for static analysis of OpenDepot. This skill covers health checks, auth, scanning, and reading results. It is read-only with respect to the SonarQube server.

> **Container name: `sonarqube-pg`.** The SonarQube container is `sonarqube-pg`, not `sonarqube`. Its database is the separate `sonarqube-pg-db` container (`postgres:17`). Use `sonarqube-pg` in every `docker` command, and check `sonarqube-pg-db` if SonarQube fails to start.

## Rules

- **Read-only on the server.** Use only GET endpoints. Never mark issues or hotspots as won't fix, false positive, or reviewed. Never change the quality gate.
- **Never print, log, or write the token.** Reference only the variable name `SONAR_TOKEN`. Check it with `test -n`, never `echo`.
- **Run every SonarQube command through interactive zsh**, as `zsh -ic '<command>'`. `SONAR_TOKEN` is exported from `~/.zshrc`, and plain `bash` and `zsh -lc` do not load it.
- **Security hotspots are not available on Community Edition.** Do not call `/api/hotspots/search`, and do not treat that as a failed precondition. Security findings appear as `VULNERABILITY`-type issues from the issues endpoint.
- **Community Edition has no branch or pull request analysis.** You filter results to changed lines yourself (see Filter to Changed Lines).

## 1. Check the Container

```bash
docker ps -a --filter name=sonarqube-pg
```

Expected: container `sonarqube-pg`, image `sonarqube:community`, port `0.0.0.0:9000->9000/tcp`.

If it is stopped:

```bash
docker start sonarqube-pg
```

The original `docker run` flags are not recorded in this repository, so `docker start` is the only documented start command. If the container does not exist, stop and ask the user. Do not create a new one from memory.

Do not stop or restart the container unless the user asks.

## 2. Wait for Readiness

A freshly started instance can take a minute or more to come up. Poll the health endpoint:

```bash
for i in $(seq 1 30); do
  s=$(curl -s http://localhost:9000/api/system/status | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))' 2>/dev/null)
  [ "$s" = "UP" ] && echo "SonarQube UP" && break
  sleep 10
done
```

Ready looks like `{"id":"...","version":"26.11.0.134175","status":"UP"}`. Any other status means keep waiting. If it has not reached `UP` after about 5 minutes, check the container logs with `docker logs --tail 100 sonarqube-pg`.

The host URL defaults to `http://localhost:9000`. Use `${SONAR_HOST_URL:-http://localhost:9000}` in every command.

## 3. Check and Validate the Token

Confirm the variable is set without printing it:

```bash
zsh -ic 'test -n "$SONAR_TOKEN" && echo set || echo "not set"'
```

Validate auth. Expected response: `{"valid":true}`.

```bash
zsh -ic 'curl -s -u "$SONAR_TOKEN:" "${SONAR_HOST_URL:-http://localhost:9000}/api/authentication/validate"'
```

To check the HTTP status alone:

```bash
zsh -ic 'curl -s -o /dev/null -w "%{http_code}\n" -u "$SONAR_TOKEN:" "${SONAR_HOST_URL:-http://localhost:9000}/api/authentication/validate"'
```

## 4. Run the Scan

From the repository root:

```bash
zsh -ic 'sonar-scanner \
  -Dsonar.host.url="${SONAR_HOST_URL:-http://localhost:9000}" \
  -Dsonar.projectKey=opendepot \
  -Dsonar.projectName=OpenDepot \
  -Dsonar.sources=. \
  -Dsonar.exclusions="site/**,**/node_modules/**,**/zz_generated*.go" \
  -Dsonar.coverage.exclusions="**/*_test.go,**/test/**,pkg/testutils/**,services/ui/**/*.test.*,services/ui/**/*.spec.*,services/ui/playwright.config.ts,test/**,tilt/**,promo/**,**/zz_generated*.go" \
  -Dsonar.sourceEncoding=UTF-8 \
  "-Dsonar.go.coverage.reportPaths=coverage/go/*.out" \
  -Dsonar.javascript.lcov.reportPaths=services/ui/coverage/lcov.info \
  -Dsonar.working.directory=/tmp/opendepot-sonar'
```

Coverage must be generated before the scan. Run `make coverage` from the repository root first. It writes one Go profile per `go.work` module to `coverage/go/*.out` and the UI report to `services/ui/coverage/lcov.info`. Without it, the scan reports 0% coverage. `make coverage` also writes `coverage/go/services-version-jevharness.out`, from the second `services/version` pass with `-tags jevharness`. `coverage-go` runs `hack/check-go-work-modules.py` first.

### Coverage Denominator

`sonar.coverage.exclusions` keeps test code, test support, and non-production tooling out of the coverage denominator. Only code that ships in production counts toward coverage. The exclusions are:

- Test code and test support: `**/*_test.go`, `**/test/**`, `pkg/testutils/**`, `services/ui/**/*.test.*`, `services/ui/**/*.spec.*`, `services/ui/playwright.config.ts`, `test/**`
- Non-production tooling: `tilt/**`, `promo/**`
- Generated code: `**/zz_generated*.go`

`sonar.exclusions` is not extended with these paths. Excluded files are still scanned for issues, so the coverage exclusions change only the denominator. Do not add an exclusion that hides production code.

### E2E Coverage Workflow

Unit tests and the UI cover only the code paths that unit tests reach. E2E coverage adds the paths that run inside the service binaries during the Kind suites. The service images normally use `go build -a`, which has no coverage counters, so this workflow uses a separate coverage build.

1. Build the coverage images and run the e2e suites on an isolated Kind cluster with `make coverage-e2e`. It builds the images with `COVER=1` and writes binary coverage under `coverage/e2e/`. Do not use the shared `opendepot` cluster for this. Pass `KIND_CLUSTER=<isolated-name> KEEP_KIND=1` and delete that cluster when finished.
2. Do not generate or merge envtest profiles. `coverage-merge` does not read `services/<svc>/cover.out`. Stale service profiles were dropped on purpose in Wave 5. Envtest paths are covered only through the `services/version` unit run.
3. Run `make coverage-merge`. It runs `coverage-go`, converts each `coverage/e2e/<svc>/` to `coverage/e2e/<svc>.txt`, then merges `coverage/go/*.out` and `coverage/e2e/*.txt` into one profile per `go.work` module under `coverage/go/`.
4. Run the scan as above.

`KUBEBUILDER_ASSETS` must point at the envtest control-plane binaries (`etcd` and `kube-apiserver`) for the envtest suites to run. The service `test` targets set it from `setup-envtest`. Do not rely on a value exported from your shell. The envtest paths are not merged into the coverage report, so `coverage-merge` does not need these variables.

The `/api/hotspots/search` endpoint is unavailable on Community Edition. Do not call it.

The scanner reads the token from `SONAR_TOKEN`. Never pass it as a command-line argument.

The scan analyzes the working tree as checked out, including uncommitted changes. When you finish, remove the scanner working directory:

```bash
rm -rf /tmp/opendepot-sonar
```

## 5. Read Open Issues

Fetch open issues. Paginate with `p=` if `paging.total` is larger than `ps`.

```bash
zsh -ic 'curl -s -u "$SONAR_TOKEN:" "${SONAR_HOST_URL:-http://localhost:9000}/api/issues/search?projectKeys=opendepot&resolved=false&ps=500"'
```

Page 2 and later:

```bash
zsh -ic 'curl -s -u "$SONAR_TOKEN:" "${SONAR_HOST_URL:-http://localhost:9000}/api/issues/search?projectKeys=opendepot&resolved=false&ps=500&p=2"'
```

Each issue has `component`, `line` (may be absent for file-level findings), `severity`, `type`, `rule`, and `message`. If an issue has no `severity` field, use its `impacts` entries instead. Issue types are `BUG`, `VULNERABILITY`, and `CODE_SMELL`. Security findings are `VULNERABILITY`.

Count only, for quick triage:

```bash
zsh -ic 'curl -s -u "$SONAR_TOKEN:" "${SONAR_HOST_URL:-http://localhost:9000}/api/issues/search?projectKeys=opendepot&resolved=false&ps=1" | python3 -c "import json,sys; print(json.load(sys.stdin)[\"paging\"][\"total\"])"'
```

## 6. Filter to Changed Lines

Community Edition has no pull request analysis, so decide what is in scope yourself.

1. Get changed line ranges with `git --no-pager diff -U0 <root-branch>`, where the root branch is the base of the stack (`main` unless the scope contract says otherwise). This includes uncommitted work and every layer in the stack. `<root-branch>...HEAD` covers only committed work, so do not use it alone.
2. For each file, record the new-side ranges from the `@@ -a,b +start,count @@` hunk headers.
3. Keep an issue as in scope only when its `component` (file path after the `opendepot:` prefix) and `line` fall inside a changed range.

## 7. Map Severity to Review Tier

| SonarQube severity | On changed lines | Outside the diff |
| --- | --- | --- |
| BLOCKER, CRITICAL, MAJOR | BLOCKING | FOLLOW-UP |
| MINOR, INFO | NIT | FOLLOW-UP |

Report out-of-diff findings as FOLLOW-UP. Do not fix them unless they are in the plan.

## Troubleshooting

- **Container not running.** `docker ps -a --filter name=sonarqube-pg` shows it stopped. Run `docker start sonarqube-pg`, then poll the health check in step 2.
- **`SONAR_TOKEN` not set in non-interactive shells.** It is exported from `~/.zshrc`, so `bash -c` and `zsh -lc` do not see it. Use `zsh -ic '<command>'` for every SonarQube command.
- **`sonar-scanner` not on PATH.** The expected binary is `/opt/homebrew/bin/sonar-scanner`. Check it with `zsh -ic 'command -v sonar-scanner'`. If it is missing, reinstall it with Homebrew.
- **HTTP 401 or 403.** Run the token validation in step 3. Confirm `SONAR_TOKEN` is set in `~/.zshrc` and that the value is current. Do not paste the token into a command or file. If validation still fails, report the status code to the user.
- **Process stuck or port in use.** `pkill` is blocked in this environment. Find the process with `pgrep -fl <name>` and stop it with `kill <pid>`.
