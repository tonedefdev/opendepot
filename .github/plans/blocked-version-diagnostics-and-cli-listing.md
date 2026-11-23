# Implementation Plan: Blocked-Version Diagnostics and CLI Listing

## Summary

Expose blocked and rejected candidates to authorized CLI users, explain why a newer candidate cannot be installed, and require `--allow-older-versions` to fall back to an older installable version. Add commands to list visible skills and agents, using an optional project default registry.

The API will follow the UI’s existing visibility model, including anonymous access when configured. Blocked artifacts remain unavailable for download.

## Affected Services

- **Server:** Return diagnostic metadata for blocked and rejected candidates; provide a paginated catalog for CLI listing.
- **CLI:** Display diagnostics, enforce explicit older-version fallback, resolve the default registry, and implement listing commands.
- **Agent and version controllers:** No planned behavior changes. Their existing Version status and branch-candidate data are the source of diagnostics.

## API / CRD Changes

No CRD fields, kubebuilder markers, generated manifests, or new Go dependencies are planned.

The existing `agents.v1` version response will retain its `versions` array and existing `blocked` and `blockReasons` fields. Add a separate optional **`unavailableCandidates`** array for candidates that cannot safely be represented as installable versions, including unversioned rejected branch commits.

Each candidate should contain its available version or commit identifier, a status category, and safe diagnostic reasons. It must not include artifact URLs, content, credentials, or raw sensitive errors.

Add a paginated catalog endpoint under the discovered `agents.v1` base. Its resource summaries will include namespace, name, latest installable version, risk level and score for that version when available, and a distinct status for blocked or rejected candidates. Missing assessment data is **unknown**, not “safe.”

## Implementation Steps

### 1. Define the server response contracts

- **Depends on:** none.
- **Files:** `types.go`.
- Add the unavailable-candidate and catalog response types. Keep additions backward-compatible so older clients can ignore them.
- Define explicit distinctions among blocked, rejected, pending, and assessment-failed states. Do not label every unsynced candidate as blocked.
- Define which diagnostics are safe to expose; avoid returning raw Kubernetes or internal errors.

### 2. Surface unavailable candidates in version responses

- **Depends on:** 1.
- **Files:** `agents.go`, `agents_test.go`.
- Include versioned Jev-blocked entries even when their parent reference is unsynced.
- Find rejected unversioned branch candidates from the matching Version resources, not just versioned parent references. Associate them with the correct skill or agent.
- Surface non-Jev rejection or assessment-failure information where a safe, meaningful status exists.
- Keep ordinary pending candidates distinct from terminal rejections.
- Preserve the existing download and archive checks: diagnostics never make an artifact servable.

### 3. Add the authorized catalog endpoint

- **Depends on:** 1, 2.
- **Files:** `main.go`, new `agents_catalog.go` and `agents_catalog_test.go` in `services/server/`.
- Reuse the UI browse visibility and resource-matching logic rather than inventing a separate access policy. Test anonymous, authenticated GroupBinding, and service-account access explicitly.
- Return only resources visible to the caller, with an optional namespace filter and stable pagination.
- Match risk data to the version it describes; do not attach a rejected candidate’s score to an older installable version.
- Fail explicitly if required catalog data cannot be retrieved rather than silently presenting an incomplete catalog as complete.

### 4. Add project default registry configuration

- **Depends on:** none.
- **Files:** `config.go`, new `config_test.go` in `cli/`.
- Add an optional default registry host to the `opendepot` block. Existing source addresses remain authoritative for `apply` and `plan`.
- Validate host input consistently with existing source parsing.
- Resolve a listing registry in this order: explicit `--registry`, project default, then a single unique host inferred from project sources.
- If no registry can be determined, return a clear instruction to provide `--registry`. Explicit `--registry` must work without a project file.

### 5. Implement diagnostics and explicit fallback

- **Depends on:** 1, 2.
- **Files:** `registry.go`, `constraint.go`, `install.go`, `commands.go`, `blocked_version.go`, `constraint_test.go`, `apply_test.go`, `blocked_version_test.go`.
- Parse unavailable candidates without passing unversioned commits into semantic-version resolution.
- When a newer matching version is blocked and an older matching version is available, show the reason and stop before writes unless `--allow-older-versions` is set. With the flag, warn and select the older eligible version.
- For an unversioned rejected branch candidate, identify it by commit and explain that no version was assigned. Only treat it as the newer rejected candidate when the server can establish that relationship; never claim it matches a version constraint.
- Keep lock-file behavior explicit: the new flag does not itself re-resolve a lock or permit a blocked version. A blocked locked version still requires `--upgrade` to choose another version, plus the new flag when falling back.
- Apply the same decision to `plan` without writing files. Preserve signature, hash, yank, and policy checks; the new flag bypasses none of them.
- For older servers that do not return candidate diagnostics, preserve compatibility and avoid claiming that a block was checked.

### 6. Implement listing commands

- **Depends on:** 3, 4, 5.
- **Files:** `commands.go`, `registry.go`, new `list.go` and `list_test.go` in `cli/`.
- Add `opendepot list skills` and `opendepot list agents`, with `--registry` and optional `--namespace`.
- Follow every catalog page and render one row per visible resource: source, latest installable version, risk level/score when known, and relevant availability or rejection status.
- Use the existing terminal styling and `NO_COLOR` behavior. Give empty results, authorization failures, unsupported older servers, and incomplete requests distinct messages.
- Listing is read-only and does not download artifacts or modify project files.

### 7. Document and validate

- **Depends on:** 2, 3, 4, 5, 6.
- **Files:** `cli.md`, `agents.md`, `e2e_test.go`.
- Document configuration precedence, commands, output semantics, the new fallback flag, access behavior, and compatibility with older servers.
- Run the CLI and server unit tests, compile checks, and vet. Add focused server e2e coverage for the API and authorization behavior.

## E2E Test Changes

In the server e2e suite, cover:

- Authorized and denied users, plus configured anonymous access.
- Versioned blocked and unversioned rejected candidates.
- A blocked newer version alongside an installable older version.
- Catalog pagination and namespace filtering.
- Continued refusal to download blocked artifacts.

CLI tests should separately verify `plan` and `apply`, including **no writes on refusal**, explicit fallback, lock behavior, genuine empty results, and non-Jev failures.

## Helm Chart

No chart values, RBAC changes, or CRD regeneration are expected. Verify that the existing server permissions cover the catalog’s reads; if they do not, make the smallest required chart/RBAC change and add chart validation.