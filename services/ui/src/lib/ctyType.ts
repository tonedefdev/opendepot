import type { CtyType } from "./api";

export const GRADE_COLOR: Record<string, "success" | "warning" | "default"> = {
  full: "success",
  partial: "warning",
  unsupported: "default",
};

export const CONFIDENCE_COLOR: Record<string, "success" | "info" | "default"> = {
  exact: "success",
  inferred: "info",
  unknown: "default",
};

/**
 * Renders a cty type constraint as a compact single-line summary suitable for
 * a node badge or tooltip trigger, e.g. "string", "list(string)", "object(4)".
 * For the fully pretty-printed, multi-line OpenTofu source form, see
 * ContractTable.tsx's own renderType.
 */
export function renderTypeCompact(t: CtyType): string {
  if (typeof t === "string") {
    return t;
  }

  if (!Array.isArray(t) || t.length === 0) {
    return "dynamic";
  }

  const kind = t[0];

  if (kind === "list" || kind === "set" || kind === "map") {
    return `${kind}(${renderTypeCompact(t[1] as CtyType)})`;
  }

  if (kind === "object") {
    const attrs = (t[1] ?? {}) as Record<string, CtyType>;
    return `object(${Object.keys(attrs).length})`;
  }

  if (kind === "tuple") {
    const elements = (t[1] ?? []) as CtyType[];
    return `tuple(${elements.length})`;
  }

  return String(kind);
}

/**
 * A conservative, best-effort compatibility check between a source output's
 * type and a target variable's type. This is a prototype-grade heuristic, not
 * a full cty type unification: it only flags a connection as suspicious when
 * the top-level "kind" (primitive name, or list/set/map/object/tuple) clearly
 * differs. `dynamic` on either side is always treated as compatible, and
 * string/number are treated as interchangeable since OpenTofu converts
 * between them implicitly.
 */
export function typesRoughlyCompatible(source: CtyType, target: CtyType): boolean {
  const kindOf = (t: CtyType): string => {
    if (typeof t === "string") return t;
    if (!Array.isArray(t) || t.length === 0) return "dynamic";
    return String(t[0]);
  };

  const a = kindOf(source);
  const b = kindOf(target);

  if (a === "dynamic" || b === "dynamic") return true;
  if (a === b) return true;

  const numericish = new Set(["number", "string"]);
  if (numericish.has(a) && numericish.has(b)) return true;

  return false;
}

/**
 * A module instance name (the label after `module.` in generated OpenTofu
 * code, e.g. `module.keypair`) must be a valid HCL identifier: starts with a
 * letter or underscore, followed by letters, digits, underscores or hyphens.
 */
export const INSTANCE_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_-]*$/;

export function isValidInstanceName(name: string): boolean {
  return INSTANCE_NAME_PATTERN.test(name);
}

function primitiveKind(t: CtyType): string {
  if (typeof t === "string") return t;
  if (!Array.isArray(t) || t.length === 0) return "dynamic";
  return String(t[0]);
}

/**
 * Node-scoped self-references to the current module's own iteration
 * variable — `each.key`, `each.value` (optionally with a free-typed
 * `.attr` chain), or `count.index`. These are literal HCL text from the
 * data model's perspective (see FieldValue), so they're exempt from the
 * scalar-type literal checks below: their runtime type depends on the
 * module's for_each/count expression, not the receiving variable's type.
 */
const META_EXPR_PATTERN = /^(each\.(key|value)(\.[A-Za-z_][A-Za-z0-9_]*)*|count\.index)$/;

/**
 * Validates a freeform literal the user typed into a field against the
 * variable's declared type. Only the primitives with an unambiguous textual
 * representation (string/number/bool) are checked — anything else (list/map/
 * object/tuple/dynamic) is accepted as-is in this prototype, since a full HCL
 * literal parser/editor is out of scope. An empty literal is never flagged
 * here; required-field emptiness is validated separately.
 */
export function literalValidationError(type: CtyType, literal: string): string | null {
  const kind = primitiveKind(type);
  const trimmed = literal.trim();

  if (trimmed === "" || META_EXPR_PATTERN.test(trimmed)) {
    return null;
  }

  if (kind === "number" && Number.isNaN(Number(trimmed))) {
    return "Must be a number";
  }

  if (kind === "bool" && !["true", "false"].includes(trimmed.toLowerCase())) {
    return "Must be true or false";
  }

  return null;
}
