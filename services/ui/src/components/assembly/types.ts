import type { CtyType, ProviderSchemaBlock } from "@/lib/api";

export type ScalarTypeSpec = { kind: "string" | "number" | "bool" | "any" };

export interface ObjectTypeSpec {
  kind: "object";
  attributes: ObjectAttributeSpec[];
}

export interface ObjectAttributeSpec {
  name: string;
  type: TypeSpec;
  optional?: boolean;
  hasDefault?: boolean;
  default?: ValueSpec | string;
}

export type TypeSpec =
  | ScalarTypeSpec
  | { kind: "list" | "set"; element: TypeSpec }
  | { kind: "map"; element: TypeSpec }
  | ObjectTypeSpec
  | { kind: "tuple"; elements: TypeSpec[] };

/** A default value mirroring TypeSpec's shape. Scalars are free-typed text
 * (interpreted per the paired TypeSpec at render time); anything with
 * substructure builds up from nested ValueSpecs. */
export type ValueSpec =
  | { kind: "scalar"; literal: string }
  | { kind: "list"; items: ValueSpec[] }
  | { kind: "map"; entries: { key: string; value: ValueSpec }[] }
  | { kind: "object"; entries: { name: string; value: ValueSpec }[] };

export const EMPTY_TYPE_SPEC: TypeSpec = { kind: "string" };
export const EMPTY_VALUE_SPEC: ValueSpec = { kind: "scalar", literal: "" };

export interface VariableValidation {
  condition: string;
  errorMessage: string;
}

/**
 * How a module node is repeated on the canvas, if at all — the visual
 * counterpart of OpenTofu's `count`/`for_each` meta-arguments on a module
 * block.
 *
 * `count.mode: "conditional"` generates `<expr> ? 1 : 0` rather than making
 * the user hand-type ternary HCL — this is the extremely common
 * "create this module or not" pattern. `count`'s expr can be a literal or a
 * reference to anything (including a module output), same as any other
 * field — count only needs to know its LENGTH at plan time, which is a much
 * narrower/lower-risk requirement than for_each's full key set.
 *
 * `for_each` is locked to referencing a Variable node's whole value —
 * a Variable is provably known at plan time by construction, so there is no
 * free-typed literal or module-output path for for_each at all (unlike
 * count). The collection shape (set vs map-of-scalar vs map-of-object) is
 * always derived live from the referenced Variable's own TypeSpec — see
 * forEachShapeFromTypeSpec in typeSpec.ts.
 */
export type Multiplicity =
  | { kind: "none" }
  | { kind: "count"; mode: "fixed" | "conditional"; expr: FieldValue }
  | { kind: "for_each"; variableNodeId: string | null };

export const NONE_MULTIPLICITY: Multiplicity = { kind: "none" };

/** A Variable node's identity + type, as seen by every module on the canvas
 * (used to populate the for_each variable picker and the general reference
 * pool). */
export interface VariableOption {
  nodeId: string;
  name: string;
  type: TypeSpec;
}

/** A reference target another module's field can be wired to: `module.<instanceName>.<output>`. */
export interface ReferenceOption {
  nodeId: string;
  instanceName: string;
  output: string;
  type: CtyType;
  label: string;
  /**
   * The source module's own multiplicity kind. A "none" source is a plain
   * scalar; a "count"/"for_each" source means this output is really a
   * list/map, and the consumer must pick a selector (see
   * FieldValue.refSelector) — the whole collection, or one instance.
   * Always "none" for a variable source (variables are never repeated).
   */
  sourceMultiplicity: Multiplicity["kind"];
  /** Whether this option resolves to `module.<instanceName>.<output>` or
   * `var.<instanceName>` / `var.<instanceName>.<output>`. */
  sourceKind: "module" | "variable";
  /**
   * For a for_each-repeated module source, the literal keys known from its
   * for_each variable's own default value (if one is set) — worked out
   * backwards from the module's Multiplicity.variableNodeId. Lets "one
   * instance" offer a dropdown of real keys instead of a blind free-text
   * field. Undefined when the source isn't for_each-repeated, or its
   * variable has no default value to enumerate keys from.
   */
  forEachKeys?: string[];
}

/**
 * How a consumer picks a value out of a repeated source's output: the whole
 * collection (a `[*]` splat for count, a `for` expression for for_each — the
 * exact HCL differs, but both mean "every instance") or a single instance by
 * index (count) or key (for_each). For for_each, "one instance" always
 * compiles to bracket-key access — `module.<name>["<key>"].<output>` — never
 * dot access, since the module itself is a map once for_each'd.
 */
export type ReferenceSelector =
  | { kind: "all" }
  | { kind: "index"; expr: FieldValue }
  | { kind: "key"; expr: FieldValue };

/**
 * The value of a single input field. In "literal" mode the user typed a raw
 * value directly — this also covers node-scoped self-references like
 * `each.key`, `each.value` or `count.index`, since from this data model's
 * perspective those are just literal HCL text, not a wire to another node
 * (they never carry a refNodeId, so they can never produce a graph edge).
 * In "reference" mode the field is wired to another module's output
 * (`module.<instanceName>.<output>`) or a variable (`var.<name>`) — this IS
 * how a connection between two modules is made, there is no separate
 * wire-dragging gesture. When the referenced module is itself repeated,
 * `refSelector` says whether to take the whole collection or one instance.
 */
export interface FieldValue {
  mode: "literal" | "reference";
  literal: string;
  refNodeId?: string;
  refOutput?: string;
  refSelector?: ReferenceSelector;
  refOutputSelector?: ReferenceSelector;
}

export type ModuleInputValue =
  | { kind: "scalar"; value: FieldValue }
  | { kind: "list"; items: ModuleInputValue[] }
  | { kind: "map"; entries: { key: string; value: ModuleInputValue }[] }
  | { kind: "object"; entries: { name: string; value: ModuleInputValue }[] };

export interface ProviderConfiguration {
  arguments: Record<string, FieldValue>;
  blocks: Record<string, ProviderConfiguration[]>;
}

export interface ProviderOption {
  nodeId: string;
  localName: string;
  alias: string;
  providerNamespace: string;
  providerName: string;
  label: string;
}

export interface ProviderBinding {
  providerNodeId: string;
}

export interface ProviderSchemaState {
  schema: ProviderSchemaBlock;
  configuration: ProviderConfiguration;
}
export interface ProviderConfiguration {
  arguments: Record<string, FieldValue>;
  blocks: Record<string, ProviderConfiguration[]>;
}

export interface ProviderOption {
  nodeId: string;
  localName: string;
  alias: string;
  providerNamespace: string;
  providerName: string;
  label: string;
}

export interface ProviderBinding {
  providerNodeId: string;
}

export interface ProviderSchemaState {
  schema: ProviderSchemaBlock;
  configuration: ProviderConfiguration;
}
