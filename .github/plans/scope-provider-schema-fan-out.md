# Implementation Plan: Scope Provider-Schema Fan-Out to Actually-Dependent Modules

## Summary

Force-syncing (or naturally re-extracting) a single provider's schema currently
triggers a reconcile storm across the entire namespace: both the watch-driven
fan-out (`moduleVersionsForProviderSchema`) and the in-Reconcile fast-path
bypass decision (`contractNeedsDerivation`) only filter on "is this module's
contract not yet `GradeFull`" — neither checks whether the module's
`required_providers` block actually references the provider whose schema just
changed. Observed in production logs: force-syncing `hashicorp/aws` v6.50.0
immediately re-enqueued and fully re-downloaded/re-derived dozens of unrelated
modules (GKE/google, azurerm-avm/azurerm, terragoat) that don't declare `aws`
at all — each re-derivation also bypasses the archive-fetch fast path, hitting
GitHub's API per module, which is the expensive part at fleet scale.

The Assembly Line contract JSON already records each module's
`required_providers` (source + version constraint, `hclschema.
ContractRequiredProvider`) at derivation time, but this list isn't surfaced
anywhere on the `Version` status today, so neither filtering function has
anything cheap to check against. This plan adds a lightweight
`RequiredProviders []RequiredProviderRef` (source + versionConstraint) field
to `Version.status.contractConfigMapRef`, populated whenever a contract is
derived, and uses it — with full semver-constraint-aware matching (reusing
the same `go-version` approach already used in `selectProviderSchemaVersion`)
— to scope both the fan-out and the fast-path-bypass decision to only the
module Versions that could actually be affected by the specific provider
version that changed.

**Open question, decide at implementation time**: behavior when
`RequiredProviders` is empty/nil (e.g. a contract derived before this field
existed) is intentionally undecided here — revisit then.

**Sequencing**: per user direction, this has not shipped yet — commit directly
to the current branch (`feat/module-builder`, `services/version`) as part of
the same large PR; no special release/versioning sequencing is required.

## Affected Services

- **api/v1alpha1** — new `RequiredProviderRef` type; new field on
  `ContractConfigMapRef`; regenerated deepcopy.
- **services/version** — `contract.go` (new helpers, `contractNeedsDerivation`
  scoping, `upsertContractConfigMap` populating the new field),
  `providerschema.go` (minor dedup refactor reusing the new source helper),
  `opendepot_versions_controller.go` (`moduleVersionsForProviderSchema`
  scoping + doc comments), unit tests, e2e test.
- **chart/opendepot** + **services/{depot,module,provider}** — CRD YAML
  regeneration only (additive optional field, generated artifact — do not
  hand-edit). No values.yaml/template/RBAC changes; this is a pure status
  schema addition to an existing CRD already reconciled by this controller.
- **services/server** — no changes needed; `ContractConfigMapRef` consumers
  there only read `Grade`/`DerivedAt`, and the new field is optional/additive.

## API / CRD Changes

In `api/v1alpha1/types.go`:

```go
// RequiredProviderRef is one required_providers entry from a module's
// Assembly Line contract, used to match a module against the provider a
// schema-change event is for.
type RequiredProviderRef struct {
	// The provider source address (e.g. "hashicorp/aws").
	Source string `json:"source"`
	// The version constraint from required_providers (e.g. "~> 5.0").
	// +optional
	VersionConstraint string `json:"versionConstraint,omitempty"`
}
```

Add to `ContractConfigMapRef`:

```go
	// RequiredProviders are the provider sources/constraints this module
	// declares, so provider-schema-change handling can target only modules
	// that depend on the changed provider.
	// +optional
	RequiredProviders []RequiredProviderRef `json:"requiredProviders,omitempty"`
```

`zz_generated.deepcopy.go` must be regenerated (`make generate` in
`api/v1alpha1`) to add `RequiredProviderRef.DeepCopy()`/`DeepCopyInto` and
update `ContractConfigMapRef.DeepCopyInto` to deep-copy the new slice.

No new go.mod dependency: `go.work` already links `./api/v1alpha1` and
`./services/version` as workspace modules.

## Implementation Steps

1. **`api/v1alpha1/types.go`** — add `RequiredProviderRef` and the new field
   on `ContractConfigMapRef` exactly as specified above.

2. **`api/v1alpha1/zz_generated.deepcopy.go`** — regenerate via `make
   generate` (controller-gen) rather than hand-editing.

3. **CRD manifests** — regenerate via `make manifests` so the new
   `requiredProviders` array-of-object property (with `source` and
   `versionConstraint` string sub-properties) appears under
   `status.properties.contractConfigMapRef.properties` in all 4 copies:
   `chart/opendepot/crds/opendepot.defdev.io_versions.yaml`,
   `services/version/config/crd/bases/opendepot.defdev.io_versions.yaml`,
   `services/module/config/crd/bases/opendepot.defdev.io_versions.yaml`,
   `services/provider/config/crd/bases/opendepot.defdev.io_versions.yaml`.
   These are generated artifacts kept in sync as an existing repo convention
   — do not hand-edit.

4. **`services/version/internal/controller/contract.go`**:
   - Add a shared helper:
     ```go
     // providerSourceOf returns a provider Version's "<namespace>/<name>"
     // source address, e.g. "hashicorp/aws".
     func providerSourceOf(v *opendepotv1alpha1.Version) (string, bool)
     ```
   - Add a shared, constraint-aware matching helper:
     ```go
     // moduleRequiresProviderVersion reports whether source/version
     // satisfies any constraint in required, so callers can tell if a
     // module is affected by a specific provider schema change.
     func moduleRequiresProviderVersion(required []opendepotv1alpha1.RequiredProviderRef, source, version string) bool
     ```
     Implementation reuses `goversion.NewConstraint`/`NewVersion`/`Check`,
     mirroring the existing constraint-check logic in
     `selectProviderSchemaVersion` (providerschema.go).
   - Update `contractNeedsDerivation`: in the loop over provider Versions,
     after the existing `extractedAt.After(derivedAt)` check, additionally
     require
     `moduleRequiresProviderVersion(ref.RequiredProviders, providerSource, opendepotUtils.SanitizeVersion(versions.Items[i].Spec.Version))`
     (computing `providerSource` via `providerSourceOf(&versions.Items[i])`,
     skipping candidates where `providerSourceOf` returns false) before
     returning `true`.
   - Update `upsertContractConfigMap`: populate `RequiredProviders` on the
     returned `ContractConfigMapRef` by mapping `contract.RequiredProviders`
     (`[]hclschema.ContractRequiredProvider`) to
     `[]opendepotv1alpha1.RequiredProviderRef{Source, VersionConstraint}`,
     deduplicated by `Source` (keep first occurrence — a module could
     theoretically declare the same source under two local names).

5. **`services/version/internal/controller/providerschema.go`** — refactor
   `selectProviderSchemaVersion`'s inline namespace-default/source-comparison
   logic to call the new `providerSourceOf` helper instead of duplicating it
   (compare `providerSourceOf(candidate) == rp.Source`, where `rp.Source` is
   already `"<namespace>/<name>"`). Pure cleanup, no behavior change here.

6. **`services/version/internal/controller/opendepot_versions_controller.go`**:
   - Update `moduleVersionsForProviderSchema`: compute
     `changedSource, ok := providerSourceOf(providerVersion)`. For each
     candidate module Version already passing the existing
     "not `GradeFull`" filter, additionally require (when `ok`):
     `moduleVersion.Status.ContractConfigMapRef == nil ||
     moduleRequiresProviderVersion(moduleVersion.Status.ContractConfigMapRef.RequiredProviders, changedSource, opendepotUtils.SanitizeVersion(providerVersion.Spec.Version))`
     before appending it to `requests` (a nil `ContractConfigMapRef` means
     the module has never synced/derived at all, so it must still be
     included).
   - Update the doc comments on `moduleVersionsForProviderSchema` and the
     `providerSchemaExtracted` predicate block to describe the new scoped
     behavior (currently they describe the unscoped "every degraded module"
     behavior being replaced).

## E2E Test Changes

`services/version/test/e2e/e2e_test.go` — add a new scenario (near the
existing `forceSync`/`providerSchemaRef` scenarios around line ~1066):

- Onboard two module Versions with non-overlapping `required_providers`
  (e.g. one requiring only `hashicorp/aws`, one requiring only
  `hashicorp/google` or declaring no providers), both left at a non-`full`
  grade, plus the `hashicorp/aws` provider Version already synced with an
  extracted schema.
- Record the unrelated (non-`aws`) module's
  `status.contractConfigMapRef.derivedAt` via `kubectl ... -o
  jsonpath={.status.contractConfigMapRef.derivedAt}`.
- Patch the `aws` provider Version with `forceSync: true` and wait for it to
  resync (existing pattern from the README `forceSync` test).
- Assert the `aws`-dependent module's `derivedAt` **changes** (correctly
  still re-derived), while the unrelated module's `derivedAt` **stays
  unchanged** (proves the fan-out/fast-path no longer touches it) — this is
  the direct regression test for the reported reconcile storm.

## Unit Test Changes

`services/version/internal/controller/` (Ginkgo/Gomega, envtest — follow the
existing `opendepot_versions_controller_test.go` conventions; add a new
`contract_test.go` or extend the existing suite):

- `contractNeedsDerivation`: source matches + no constraint → true; source
  matches + constraint satisfied → true; source matches + constraint NOT
  satisfied → false; source doesn't match at all → false.
- `moduleVersionsForProviderSchema`: a module requiring a different provider
  is excluded from returned requests; a module requiring the changed
  provider (source + constraint match) is included.
- `providerSourceOf` / `moduleRequiresProviderVersion`: direct coverage,
  including malformed constraint/version inputs (decide expected behavior at
  implementation time).

## Helm Chart

- CRD schema gains the additive, optional `requiredProviders` array field
  under `status.contractConfigMapRef` (regenerated, not hand-edited — see
  step 3 above).
- No `values.yaml`, template, or RBAC changes — this is a pure status-schema
  addition to a CRD already reconciled by this controller.
- Chart version bump deferred: per user direction this feature hasn't
  shipped yet and will be committed directly to the current branch; bump
  `Chart.yaml` version/appVersion later per existing release convention when
  actually cutting a release, not as part of this implementation.
