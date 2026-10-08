import type { Multiplicity, TypeSpec, ValueSpec, VariableOption, VariableValidation } from "./types";
import type { CtyType } from "@/lib/api";
import { builtInFunctions } from "./hclConditionHighlight";

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

function isHclTraversal(expression: string): boolean {
  const match = /^(?:module|var|local|output)\.[A-Za-z_][A-Za-z0-9_]*/.exec(expression);
  if (!match) return false;

  let cursor = match[0].length;
  while (cursor < expression.length) {
    const attribute = /^\.[A-Za-z_][A-Za-z0-9_]*/.exec(expression.slice(cursor));
    if (attribute) {
      cursor += attribute[0].length;
      continue;
    }

    if (expression[cursor] !== "[") return false;

    let depth = 1;
    let quote: string | undefined;
    let escaped = false;
    const start = cursor + 1;
    cursor += 1;
    while (cursor < expression.length && depth > 0) {
      const character = expression[cursor];
      if (quote) {
        if (escaped) escaped = false;
        else if (character === "\\") escaped = true;
        else if (character === quote) quote = undefined;
      } else if (character === "\"" || character === "'") {
        quote = character;
      } else if (character === "[") {
        depth += 1;
      } else if (character === "]") {
        depth -= 1;
        if (depth === 0 && expression.slice(start, cursor).trim() === "") return false;
      }
      cursor += 1;
    }
    if (depth !== 0 || quote) return false;
  }

  return true;
}

function isDelimitedHclExpression(expression: string): boolean {
  const closingFor: Record<string, string> = { "(": ")", "[": "]", "{": "}" };
  const rootClosing = closingFor[expression[0]];
  if (!rootClosing) return false;

  const stack: string[] = [];
  let inString = false;
  let escaped = false;

  for (let index = 0; index < expression.length; index += 1) {
    const character = expression[index];
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
      continue;
    }

    if (character === '"') {
      inString = true;
    } else if (closingFor[character]) {
      stack.push(closingFor[character]);
    } else if (")]}".includes(character)) {
      if (stack[stack.length - 1] !== character) {
        if (character === rootClosing && expression.slice(index + 1).trim() === "") return true;
        return false;
      }
      stack.pop();
    }
  }

  return !inString && stack.length === 0;
}

function isConditionalHclExpression(expression: string): boolean {
  const pendingQuestions = new Map<number, number>();
  const stack: string[] = [];
  let inString = false;
  let escaped = false;

  for (const character of expression) {
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
      continue;
    }

    if (character === '"') {
      inString = true;
    } else if (character === "(" || character === "[" || character === "{") {
      stack.push(character);
    } else if (character === ")" || character === "]" || character === "}") {
      stack.pop();
    } else if (character === "?") {
      const depth = stack.length;
      pendingQuestions.set(depth, (pendingQuestions.get(depth) ?? 0) + 1);
    } else if (character === ":") {
      const depth = stack.length;
      const pending = pendingQuestions.get(depth) ?? 0;
      if (pending > 0) return true;
    }
  }

  return false;
}

function hasOnlyKnownHclFunctionCalls(expression: string): boolean {
  let inString = false;
  let escaped = false;

  for (let index = 0; index < expression.length; index += 1) {
    const character = expression[index];
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
      continue;
    }

    if (character === '"') {
      inString = true;
      continue;
    }

    const match = /^[A-Za-z_][A-Za-z0-9_]*/.exec(expression.slice(index));
    if (!match) continue;

    const name = match[0];
    let nextIndex = index + name.length;
    while (nextIndex < expression.length && /\s/.test(expression[nextIndex])) nextIndex += 1;
    if (
      expression[nextIndex] === "(" &&
      !Object.prototype.hasOwnProperty.call(builtInFunctions, name)
    ) {
      return false;
    }
    index += name.length - 1;
  }

  return true;
}

/** Identifies expressions that should remain unquoted in HCL source form. */
export function isKnownHclExpression(literal: string): boolean {
  const expression = literal.trim();
  if (!hasOnlyKnownHclFunctionCalls(expression)) return false;
  if (isHclTraversal(expression) || isDelimitedHclExpression(expression) || isConditionalHclExpression(expression)) return true;
  const match = expression.match(/^([A-Za-z_][A-Za-z0-9_]*)\s*\(/);
  return match !== null && Object.prototype.hasOwnProperty.call(builtInFunctions, match[1]);
}

export function renderHclHeredoc(literal: string, depth = 0): string | undefined {
  const lines = literal.replace(/\r\n/g, "\n").replace(/\n+$/, "").split("\n");
  const opening = lines[0]?.trim().match(/^<<(-?)([A-Za-z_][A-Za-z0-9_]*)$/);
  if (!opening || lines.length < 2 || lines[lines.length - 1].trim() !== opening[2]) return undefined;

  if (!opening[1]) return [lines[0].trim(), ...lines.slice(1, -1), opening[2]].join("\n");

  const markerIndent = indentation(depth);
  return [
    lines[0].trim(),
    ...lines.slice(1, -1).map((line) => line ? `${markerIndent}${line}` : ""),
    `${markerIndent}${opening[2]}`,
  ].join("\n");
}

export function renderHclStringLiteral(literal: string): string {
  let rendered = '"';
  let expressionDepth = 0;
  let inExpressionString = false;
  let escaped = false;

  for (let index = 0; index < literal.length; index += 1) {
    const character = literal[index];

    if (expressionDepth === 0) {
      if (literal.startsWith("$${", index) || literal.startsWith("%%{", index)) {
        rendered += literal.slice(index, index + 3);
        index += 2;
        continue;
      }
      if (literal.startsWith("${", index) || literal.startsWith("%{", index)) {
        rendered += literal.slice(index, index + 2);
        index += 1;
        expressionDepth = 1;
        continue;
      }

      if (character === "\\") rendered += "\\\\";
      else if (character === '"') rendered += '\\"';
      else if (character === "\n") rendered += "\\n";
      else if (character === "\r") rendered += "\\r";
      else if (character === "\t") rendered += "\\t";
      else rendered += character;
      continue;
    }

    rendered += character;
    if (inExpressionString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inExpressionString = false;
    } else if (character === '"') {
      inExpressionString = true;
    } else if (character === "{") {
      expressionDepth += 1;
    } else if (character === "}") {
      expressionDepth -= 1;
    }
  }

  return `${rendered}"`;
}

function renderScalarLiteral(type: TypeSpec, literal: string, depth: number): string {
  if (literal.trim() === "") {
    return "null";
  }

  if (type.kind === "string") {
    const heredoc = renderHclHeredoc(literal, depth);
    if (heredoc) return heredoc;
  }

  if (type.kind === "string" && !isKnownHclExpression(literal)) {
    return renderHclStringLiteral(literal);
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
      ? renderScalarLiteral(type, value.literal, depth)
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
    const conditionLines = (validation.condition || "<condition>").split(/\r?\n/);
    const nonEmptyContinuationLines = conditionLines
      .slice(1)
      .filter((line) => line.trim() && !/^[\])}]+,?$/.test(line.trim()));
    const commonIndent = nonEmptyContinuationLines.length
      ? Math.min(...nonEmptyContinuationLines.map((line) => line.match(/^\s*/)?.[0].length ?? 0))
      : 0;
    const renderedCondition = [
      `    condition     = ${conditionLines[0]}`,
      ...conditionLines.slice(1).map((line) => {
        const trimmedLine = line.trimEnd();
        const trimmedContent = trimmedLine.trim();
        if (!trimmedContent) return "";
        if (/^[\])}]+,?$/.test(trimmedContent)) return `    ${trimmedContent}`;
        const content = trimmedLine.slice(commonIndent);
        return `      ${content}`;
      }),
    ];

    lines.push(
      "",
      "  validation {",
      ...renderedCondition,
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
