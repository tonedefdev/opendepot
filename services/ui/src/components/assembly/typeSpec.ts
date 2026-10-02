import type { Multiplicity, TypeSpec, ValueSpec, VariableOption, VariableValidation } from "./types";
import type { CtyType } from "@/lib/api";

function attributeTypeOrder(type: TypeSpec): number {
  if (type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any") return 0;
  if (type.kind === "object") return 1;
  if (type.kind === "map") return 2;
  return 3;
}

function isExpressionKey(key: string): boolean {
  return ["count.", "data.", "each.", "module.", "var."].some((prefix) => key.trim().startsWith(prefix));
}

function renderMapKey(key: string): string {
  const trimmed = key.trim();
  return isExpressionKey(trimmed) ? `(${trimmed})` : JSON.stringify(key);
}

export function orderedObjectAttributes<T extends { name: string; type: TypeSpec }>(attributes: T[]): T[] {
  return attributes.slice().sort((left, right) => {
    const typeOrder = attributeTypeOrder(left.type) - attributeTypeOrder(right.type);
    return typeOrder !== 0 ? typeOrder : left.name.localeCompare(right.name);
  });
}

export function typeSpecFromCtyType(
  type: CtyType,
  optionalAttributePaths: string[] = [],
  optionalAttributes: Record<string, boolean> = {},
  prefix = "",
): TypeSpec | null {
  if (typeof type === "string") {
    if (["string", "number", "bool"].includes(type)) {
      return { kind: type as "string" | "number" | "bool" };
    }

    return type === "dynamic" ? { kind: "any" } : null;
  }

  if (!Array.isArray(type) || type.length < 2) {
    return null;
  }

  const kind = type[0];
  if (kind === "list" || kind === "set" || kind === "map") {
    const element = typeSpecFromCtyType(type[1] as CtyType, optionalAttributePaths, optionalAttributes, prefix);
    return element ? { kind, element } : null;
  }

  if (kind === "tuple") {
    const elements = Array.isArray(type[1])
      ? (type[1] as CtyType[]).map((element) => typeSpecFromCtyType(element, optionalAttributePaths, optionalAttributes, prefix))
      : [];
    return elements.every((element): element is TypeSpec => element !== null) ? { kind, elements } : null;
  }

  if (kind !== "object" || !type[1] || typeof type[1] !== "object" || Array.isArray(type[1])) {
    return null;
  }

  const attributes = Object.entries(type[1] as Record<string, CtyType>).map(([name, attributeType]) => {
    const path = prefix ? `${prefix}.${name}` : name;
    const nested = typeSpecFromCtyType(attributeType, optionalAttributePaths, optionalAttributes, path);
    if (!nested) return null;

    return {
      name,
      type: nested,
      optional: optionalAttributePaths.includes(path) || (!prefix && optionalAttributes[name] === true),
    };
  });

  return attributes.every((attribute): attribute is NonNullable<typeof attribute> => attribute !== null)
    ? { kind: "object", attributes }
    : null;
}

export function emptyValueSpec(type: TypeSpec): ValueSpec {
  switch (type.kind) {
    case "string":
    case "number":
    case "bool":
    case "any":
      return { kind: "scalar", literal: "" };
    case "list":
    case "set":
      return { kind: "list", items: [] };
    case "tuple":
      return { kind: "list", items: type.elements.map(emptyValueSpec) };
    case "map":
      return { kind: "map", entries: [] };
    case "object":
      return { kind: "object", entries: [] };
  }
}

export function valueSpecForType(type: TypeSpec, value: ValueSpec | string | undefined): ValueSpec {
  if (typeof value === "string") {
    return { kind: "scalar", literal: value };
  }

  if (
    value &&
    ((value.kind === "scalar" && ["string", "number", "bool", "any"].includes(type.kind)) ||
      (value.kind === "list" && ["list", "set", "tuple"].includes(type.kind)) ||
      (value.kind === "map" && type.kind === "map") ||
      (value.kind === "object" && type.kind === "object"))
  ) {
    return value;
  }

  return emptyValueSpec(type);
}

function optionalDefault(defaultValue: ValueSpec | string | undefined): ValueSpec | undefined {
  if (typeof defaultValue === "string") {
    return defaultValue.trim() === "" ? undefined : { kind: "scalar", literal: defaultValue };
  }

  return defaultValue;
}

/**
 * Renders a TypeSpec as OpenTofu source form, e.g. `map(object({\n  name = string\n}))`.
 * Used for the live syntax-highlighted preview when authoring a Variable.
 */
function indentation(depth: number): string {
  return "  ".repeat(depth);
}

function renderAssignments(entries: Array<{ key: string; value: string }>, depth: number): string[] {
  const width = Math.max(...entries.map(({ key }) => key.length));

  return entries.map(({ key, value }) => `${indentation(depth)}${key.padEnd(width)} = ${value}`);
}

export function renderTypeSpec(spec: TypeSpec, depth = 0): string {
  switch (spec.kind) {
    case "string":
    case "number":
    case "bool":
    case "any":
      return spec.kind;
    case "list":
    case "set":
      return `${spec.kind}(${renderTypeSpec(spec.element, depth)})`;
    case "map":
      return `map(${renderTypeSpec(spec.element, depth)})`;
    case "object": {
      if (spec.attributes.length === 0) {
        return "object({})";
      }

      const entries = spec.attributes.map((a) => {
        const t = renderTypeSpec(a.type, depth + 1);
        if (!a.optional) {
          return { key: a.name, value: t };
        }

        const defaultValue = optionalDefault(a.default);
        const hasDefault = a.hasDefault ?? defaultValue !== undefined;
        const renderedType = hasDefault
          ? `optional(${t}, ${renderValueSpec(a.type, defaultValue ?? emptyValueSpec(a.type), depth + 1)})`
          : `optional(${t})`;

        return { key: a.name, value: renderedType };
      });
      const lines = renderAssignments(entries, depth + 1);

      return `object({\n${lines.join("\n")}\n${indentation(depth)}})`;
    }
    case "tuple": {
      if (spec.elements.length === 0) {
        return "tuple([])";
      }

      return `tuple([\n${spec.elements.map((e) => `${indentation(depth + 1)}${renderTypeSpec(e, depth + 1)}`).join(",\n")}\n${indentation(depth)}])`;
    }
  }
}

/** A compact, single-line summary of a TypeSpec, e.g. "map(object(2))" —
 * used for the node's collapsed "Type" summary button, mirroring
 * renderTypeCompact's role for contract-derived CtyType elsewhere. */
export function renderTypeSpecCompact(spec: TypeSpec): string {
  switch (spec.kind) {
    case "string":
    case "number":
    case "bool":
    case "any":
      return spec.kind;
    case "list":
    case "set":
      return `${spec.kind}(${renderTypeSpecCompact(spec.element)})`;
    case "map":
      return `map(${renderTypeSpecCompact(spec.element)})`;
    case "object":
      return `object(${spec.attributes.length})`;
    case "tuple":
      return `tuple(${spec.elements.length})`;
  }
}

export function isComplexVariableType(type: TypeSpec): boolean {
  return type.kind === "object" || (type.kind === "map" && type.element.kind === "object");
}

/** Quotes a scalar literal per its declared type for HCL source form. */
const knownFunctionNames = new Set([
  "abs", "base64encode", "base64gzip", "base64sha256", "base64sha512",
  "basename", "bcrypt", "can", "ceil", "chomp", "cidrhost", "cidrnetmask", "cidrsubnet", "cidrsubnets",
  "coalesce", "coalescelist", "compact", "concat", "contains", "dirname", "distinct", "element", "endswith",
  "file", "filebase64", "filebase64sha256", "fileexists", "filemd5", "filesha1", "filesha256", "filesha512",
  "flatten", "floor", "format", "formatlist", "indent", "index", "join", "jsondecode", "jsonencode", "keys",
  "length", "lookup", "lower", "merge", "nonsensitive", "one", "parseint", "pathexpand", "plantimestamp", "pow",
  "regexall", "replace", "reverse", "setintersection", "setsubtract", "setunion", "sha1", "sha256", "sha512", "signum", "slice", "sort", "split",
  "startswith", "strcontains", "strrev", "substr", "timeadd", "timecmp", "timestamp", "title", "tolist", "tomap",
  "tonumber", "toset", "tostring", "trim", "trimprefix", "trimspace", "trimsuffix", "try", "upper", "urlencode",
  "uuid", "uuidv5", "values", "yamldecode", "yamlencode",
]);

function isKnownFunctionExpression(literal: string): boolean {
  const match = literal.trim().match(/^([A-Za-z_][A-Za-z0-9_]*)\s*\(/);
  return match !== null && knownFunctionNames.has(match[1]);
}

function renderScalarLiteral(type: TypeSpec, literal: string): string {
  if (literal.trim() === "") {
    return "null";
  }

  if (type.kind === "string" && !isKnownFunctionExpression(literal)) {
    return JSON.stringify(literal);
  }

  return literal;
}

/**
 * Renders a ValueSpec (paired with the TypeSpec it belongs to) as an
 * OpenTofu literal, for the live syntax-highlighted default-value preview.
 */
export function renderValueSpec(type: TypeSpec, value: ValueSpec, depth = 0): string {
  if (value.kind === "scalar") {
    return type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any"
      ? renderScalarLiteral(type, value.literal)
      : value.literal || "null";
  }

  if (value.kind === "list" && (type.kind === "list" || type.kind === "set" || type.kind === "tuple")) {
    if (value.items.length === 0) {
      return "[]";
    }

    const items = value.items.map((item, index) => {
      const elementType = type.kind === "tuple" ? type.elements[index] : type.element;
      return elementType ? renderValueSpec(elementType, item, depth) : "null";
    });
    return `[${items.join(", ")}]`;
  }

  if (value.kind === "map" && type.kind === "map") {
    if (value.entries.length === 0) {
      return "{}";
    }

    const lines = renderAssignments(
      value.entries.map((e) => ({
        key: renderMapKey(e.key),
        value: renderValueSpec(type.element, e.value, depth + 1),
      })),
      depth + 1,
    );

    return `{\n${lines.join("\n")}\n${indentation(depth)}}`;
  }

  if (value.kind === "object" && type.kind === "object") {
    if (value.entries.length === 0) {
      return "{}";
    }

    const entries = value.entries.map((e) => {
      const attr = type.attributes.find((a) => a.name === e.name);
      const rendered = attr ? renderValueSpec(attr.type, e.value, depth + 1) : "null";

      return { key: e.name, value: rendered };
    });
    const lines = renderAssignments(entries, depth + 1);

    return `{\n${lines.join("\n")}\n${indentation(depth)}}`;
  }

  return "null";
}

export function renderVariableSpec(
  name: string,
  type: TypeSpec,
  hasDefault: boolean,
  defaultValue: ValueSpec,
  description = "",
  validations: VariableValidation[] = [],
): string {
  const lines = [`variable ${JSON.stringify(name || "?")} {`];
  const typeLine = `  type = ${renderTypeSpec(type, 1)}`;
  const descriptionLine = description.trim() ? `  description = ${JSON.stringify(description)}` : "";
  const defaultLine = hasDefault ? `  default = ${renderValueSpec(type, defaultValue, 1)}` : "";

  if (isComplexVariableType(type)) {
    if (defaultLine) lines.push(defaultLine);
    if (descriptionLine) lines.push(descriptionLine);
    lines.push(typeLine);
  } else {
    lines.push(typeLine);
    if (defaultLine) lines.push(defaultLine);
    if (descriptionLine) lines.push(descriptionLine);
  }

  for (const validation of validations) {
    lines.push(
      "",
      "  validation {",
      `    condition     = ${validation.condition || "<condition>"}`,
      `    error_message = ${JSON.stringify(validation.errorMessage)}`,
      "  }",
    );
  }

  lines.push("}");

  return lines.join("\n");
}

/** Converts a TypeSpec to the wire-format CtyType shape used by contracts,
 * so a variable's reference options can reuse renderTypeCompact/typesRoughlyCompatible. */
export function typeSpecToCtyType(spec: TypeSpec): CtyType {
  switch (spec.kind) {
    case "string":
    case "number":
    case "bool":
      return spec.kind;
    case "any":
      return "dynamic";
    case "list":
    case "set":
      return [spec.kind, typeSpecToCtyType(spec.element)];
    case "map":
      return ["map", typeSpecToCtyType(spec.element)];
    case "object":
      return ["object", Object.fromEntries(spec.attributes.map((a) => [a.name, typeSpecToCtyType(a.type)]))];
    case "tuple":
      return ["tuple", spec.elements.map((e) => typeSpecToCtyType(e))];
  }
}

export interface ObjectAttributePath {
  path: string;
  type: TypeSpec;
}

/** Enumerates every dot-addressable object attribute, including intermediate
 * objects and deeply nested leaves. Collection elements are not expanded
 * because accessing them requires an explicit key or index. */
export function objectAttributePaths(spec: TypeSpec, prefix = ""): ObjectAttributePath[] {
  if (spec.kind !== "object") {
    return [];
  }

  return spec.attributes.flatMap((attribute) => {
    const path = prefix ? `${prefix}.${attribute.name}` : attribute.name;

    return [{ path, type: attribute.type }, ...objectAttributePaths(attribute.type, path)];
  });
}

/**
 * The for_each shape a Variable's TypeSpec implies — always fully derived,
 * never manually declared, since the Variable's own type is the source of
 * truth. "map_scalar" and "map_of_object" are distinct because only the
 * latter gives each.value attributes worth offering as autocomplete options.
 */
export interface ForEachShape {
  shape: "set" | "map_scalar" | "map_of_object" | "unknown";
  attributes: string[];
}

export function forEachShapeFromTypeSpec(spec: TypeSpec): ForEachShape {
  if (spec.kind === "list" || spec.kind === "set") {
    return { shape: "set", attributes: [] };
  }

  if (spec.kind === "map") {
    const element = spec.element;
    if (element.kind === "object") {
      return { shape: "map_of_object", attributes: objectAttributePaths(element).map(({ path }) => path) };
    }

    return { shape: "map_scalar", attributes: [] };
  }

  return { shape: "unknown", attributes: [] };
}

/**
 * Resolves the concrete type of a module's own for_each/count meta
 * expression — `count.index`, `each.key`, `each.value`, or
 * `each.value.<attr>` — from its for_each variable's TypeSpec, so a literal
 * field wired to one of these can be checked against the receiving
 * variable's declared type just like any other value. Returns null for
 * anything that isn't a recognized meta expression, or whose type can't be
 * pinned down (for_each has no variable picked yet, or the attribute
 * doesn't exist on that variable's element object) — those cases are left
 * unflagged rather than guessed at.
 */
export function metaExprCtyType(expr: string, multiplicity: Multiplicity, variableOptions: VariableOption[]): CtyType | null {
  if (expr === "count.index") {
    return multiplicity.kind === "count" ? "number" : null;
  }

  if (multiplicity.kind !== "for_each") {
    return null;
  }

  const source = variableOptions.find((v) => v.nodeId === multiplicity.variableNodeId);
  if (!source) {
    return null;
  }

  const { type } = source;
  if (type.kind !== "list" && type.kind !== "set" && type.kind !== "map") {
    return null;
  }

  const element = type.element;

  if (expr === "each.key") {
    return type.kind === "map" ? "string" : typeSpecToCtyType(element);
  }

  if (expr === "each.value") {
    return typeSpecToCtyType(element);
  }

  const attrMatch = /^each\.value\.([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)$/.exec(expr);
  if (attrMatch && element.kind === "object") {
    const attribute = objectAttributePaths(element).find(({ path }) => path === attrMatch[1]);
    return attribute ? typeSpecToCtyType(attribute.type) : null;
  }

  return null;
}

/** Whether a variable's type can sensibly serve as a for_each source at all. */
export function isForEachCompatible(spec: TypeSpec): boolean {
  return spec.kind === "list" || spec.kind === "set" || spec.kind === "map";
}

/**
 * Works backwards from a for_each variable's own default value to the
 * literal keys it would produce — a "set"-shaped variable's keys are its
 * elements' literal values (each.key === each.value); a "map"-shaped
 * variable's keys are its entries' literal keys. Returns undefined when the
 * variable has no default (its actual keys are only known at apply time,
 * outside anything the canvas can see) or the value doesn't match the
 * declared type's shape.
 */
export function enumerateForEachKeys(type: TypeSpec, hasDefault: boolean, value: ValueSpec): string[] | undefined {
  if (!hasDefault) {
    return undefined;
  }

  if ((type.kind === "list" || type.kind === "set") && value.kind === "list") {
    const keys = value.items
      .filter((item): item is { kind: "scalar"; literal: string } => item.kind === "scalar" && item.literal.trim() !== "")
      .map((item) => item.literal);

    return keys.length > 0 ? keys : undefined;
  }

  if (type.kind === "map" && value.kind === "map") {
    const keys = value.entries.map((e) => e.key.trim()).filter((k) => k !== "");
    return keys.length > 0 ? keys : undefined;
  }

  return undefined;
}
