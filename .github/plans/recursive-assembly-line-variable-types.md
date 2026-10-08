# Implementation Plan: Recursive Assembly Line Variable Types

## Summary
Extend Assembly Line variables so every OpenTofu type constructor can be nested recursively, including objects inside objects and arbitrary combinations of `list`, `set`, `map`, `object`, and `tuple`. Use the same recursive model for variable defaults and optional object-attribute defaults. Render every map default entry as a controlled accordion titled by its key; adding an entry expands it and collapses the previously open sibling.

## Affected Services
- `services/ui`: Generalize the variable type/value model, recursive editors, HCL preview rendering, map default accordions, and tests.
- No controller or backend service changes are required; Assembly Line state is browser-local and generated HCL is computed in the UI.

## API / CRD Changes
None. `api/v1alpha1/types.go` and generated CRDs are unaffected because Assembly Line variable definitions are UI-local `TypeSpec`/`ValueSpec` data stored in browser localStorage.

## Implementation Steps
1. Generalize the recursive data model in `services/ui/src/components/assembly/types.ts`.
   - Keep scalar kinds as recursion terminals.
   - Allow `list`/`set`/`map` elements, tuple elements, and object attribute types to contain any `TypeSpec`.
   - Change optional object-attribute defaults from scalar strings to structured `ValueSpec` values, with a separate presence flag or equivalent representation so `optional(TYPE)` remains distinct from `optional(TYPE, DEFAULT)`.
   - Retain the existing discriminated JSON shapes so saved `opendepot:assembly:v1` scalar and one-level values continue to load without migration.

2. Add recursive construction/rendering helpers in `services/ui/src/components/assembly/typeSpec.ts`.
   - Create an empty `ValueSpec` matching any `TypeSpec` for new collection entries, tuple positions, object attributes, and optional defaults.
   - Make `renderValueSpec` recurse through lists, sets, tuples, maps, and objects instead of assuming scalar children.
   - Render optional attribute defaults with `renderValueSpec` so nested values produce valid `optional(TYPE, DEFAULT)` HCL.
   - Preserve recursive `renderTypeSpec`, `renderTypeSpecCompact`, and `typeSpecToCtyType` behavior while removing scalar-only assumptions.
   - Keep for_each metadata limited to immediate `each.value.<attribute>` references, but return the full nested Cty type for that attribute.

3. Refactor `services/ui/src/components/assembly/TypeEditor.tsx` into a recursive type editor.
   - Offer all supported kinds at every nested type position.
   - Reuse the same editor for collection elements, tuple elements, and object attributes.
   - Keep object attribute name, optional, remove, and add controls at each depth.
   - For an optional attribute, expose whether it has a default and render the recursive value editor when enabled.
   - Use indentation/dividers and responsive flex sizing so deep structures remain legible without fixed scalar-only column widths.

4. Refactor `services/ui/src/components/assembly/ValueEditor.tsx` into a shape-mirroring recursive value editor.
   - Recursively edit collection items, tuple positions, map values, and object attributes according to their `TypeSpec`.
   - Initialize every new child with the matching empty value helper rather than assuming scalar or object.
   - Render each map entry with MUI `Accordion`, `AccordionSummary`, and `AccordionDetails`; use the current key as the summary label and `New entry` while empty.
   - Keep one controlled expanded entry per map editor. A newly added entry becomes expanded and collapses the prior sibling; manual expansion also collapses the previously expanded sibling.
   - Keep key editing and deletion inside the expanded details, and update the controlled index safely after deletion.

5. Wire recursive optional defaults through `services/ui/src/components/assembly/VariableModal.tsx` and verify consumers in `services/ui/src/components/assembly/AssemblyCanvas.tsx`.
   - Ensure top-level and optional nested defaults share the same editor behavior and live HCL preview.
   - Reset or normalize a value only when its selected type shape changes, preventing stale incompatible child shapes while editing.
   - Confirm direct variable references and immediate object-attribute references still use `typeSpecToCtyType` for compatibility checks.

6. Add focused tests under `services/ui/src/components/assembly/`.
   - Add `typeSpec.test.ts` for deeply nested type rendering, deeply nested defaults, structured optional defaults, Cty conversion, and existing shallow shapes.
   - Add `TypeEditor.test.tsx` for selecting object/collection kinds at multiple depths and configuring nested optional defaults.
   - Add `ValueEditor.test.tsx` for recursive value editing and map accordion behavior, including key titles, new-entry expansion, sibling collapse, manual switching, and deletion.
   - Use the existing Vitest and Testing Library dependencies; no new package or `go.mod` dependency is required.

7. Update Assembly Line documentation in `docs/guides/` only if an Assembly Line guide is introduced or already being added during implementation; no current Assembly Line guide exists to amend.

## E2E Test Changes
Add `services/ui/test/e2e/assembly.spec.ts` with a focused variable-authoring scenario: create a variable, build a deeply nested type, enter nested top-level and optional-attribute defaults, add multiple map entries, verify the latest entry is expanded while the previous one collapses, and assert the live HCL preview contains the expected recursive type/default structure. Keep pure serialization edge cases in Vitest rather than duplicating them in Playwright.

## Helm Chart
None. No values, RBAC, deployment, service, ingress, or CRD changes are required.
