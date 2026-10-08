import type { TypeSpec } from "./types";
import { moduleInputPath, type FieldValue, type ModuleInputValue, type Multiplicity, type ProviderBinding, type ProviderOption, type ReferenceOption, type VariableOption } from "./types";
import { buildModuleSource, stripV } from "@/lib/registrySource";
import { isKnownHclExpression, renderHclHeredoc, renderHclStringLiteral } from "./typeSpec";
import { renderReferenceExpression } from "./hclReference";

export interface ModulePreviewInput {
  name: string;
  type: TypeSpec;
  value?: ModuleInputValue;
}

interface ModulePreviewOptions {
  instanceName: string;
  namespace: string;
  moduleName: string;
  system?: string;
  version: string;
  registryHost: string;
  inputs: ModulePreviewInput[];
  referenceOptions: ReferenceOption[];
  multiplicity: Multiplicity;
  variableOptions: VariableOption[];
  providerBindings: Record<string, ProviderBinding>;
  providerOptions: ProviderOption[];
  optionalFieldVisibility?: Record<string, boolean>;
}

function isProvided(value: ModuleInputValue | undefined): boolean {
  if (!value) return false;
  if (value.kind === "scalar") {
    return value.value.mode === "reference" ? !!value.value.refNodeId : value.value.literal.trim() !== "";
  }
  if (value.kind === "list") return value.items.length > 0;
  return value.entries.length > 0;
}

function isIterationExpression(expression: string): boolean {
  const value = expression.trim();
  return value === "count.index" || value === "each.key" || value === "each.value" || value.startsWith("each.key.") || value.startsWith("each.value.");
}

function indentExpressionContinuations(expression: string, depth: number): string {
  const indentation = "  ".repeat(depth);
  return expression
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line, index) => index > 0 && line ? `${indentation}${line}` : line)
    .join("\n");
}

function renderScalar(type: TypeSpec, value: FieldValue, references: ReferenceOption[], depth: number): string {
  if (value.mode === "reference") return renderReferenceExpression(value, references);
  const literal = value.literal.trim();
  if (!literal) return "null";
  if (type.kind === "string") {
    const heredoc = renderHclHeredoc(value.literal, depth);
    if (heredoc) return heredoc;
  }
  if (isIterationExpression(literal) || (type.kind === "string" && isKnownHclExpression(literal))) {
    return indentExpressionContinuations(literal, depth);
  }
  return type.kind === "string" ? renderHclStringLiteral(value.literal) : value.literal;
}

function renderedKey(key: string, object: boolean): string {
  if (object && /^[A-Za-z_][A-Za-z0-9_-]*$/.test(key)) return key;
  const trimmed = key.trim();
  if (["count.", "data.", "each.", "module.", "var."].some((prefix) => trimmed.startsWith(prefix))) return `(${trimmed})`;
  return JSON.stringify(key);
}

function renderAssignments(entries: Array<{ key: string; value: string }>, depth: number): string[] {
  const width = entries.reduce((max, entry) => Math.max(max, entry.key.length), 0);
  const indent = "  ".repeat(depth);
  return entries.map((entry) => `${indent}${entry.key.padEnd(width)} = ${entry.value}`);
}

function renderValue(
  type: TypeSpec,
  value: ModuleInputValue,
  references: ReferenceOption[],
  optionalFieldVisibility: Record<string, boolean>,
  path: string,
  depth = 0,
): string {
  if (value.kind === "scalar") return renderScalar(type, value.value, references, depth);

  if (value.kind === "list") {
    const elementType = type.kind === "tuple" ? undefined : "element" in type ? type.element : undefined;
    return `[${value.items.map((item, index) => renderValue(
      type.kind === "tuple" ? type.elements[index] ?? { kind: "any" } : elementType ?? { kind: "any" },
      item,
      references,
      optionalFieldVisibility,
      moduleInputPath(path, `item:${index}`),
      depth,
    )).join(", ")}]`;
  }

  const object = value.kind === "object";
  const entries = value.entries.flatMap((entry, index) => {
    const key = "name" in entry ? entry.name : entry.key;
    const attribute = type.kind === "object" ? type.attributes.find((candidate) => candidate.name === key) : undefined;
    if (attribute?.optional && optionalFieldVisibility[path] === false) return [];
    const childType = attribute?.type ?? ("element" in type ? type.element : undefined);
    return [{
      key: renderedKey(key, object),
      value: renderValue(
        childType ?? { kind: "any" },
        entry.value,
        references,
        optionalFieldVisibility,
        moduleInputPath(path, object ? `attribute:${key}` : `map-entry:${index}:${key}`),
        depth + 1,
      ),
    }];
  });
  if (entries.length === 0) return "{}";
  return `{\n${renderAssignments(entries, depth + 1).join("\n")}\n${"  ".repeat(depth)}}`;
}

function renderProviderBindings(
  bindings: Record<string, ProviderBinding>,
  providers: ProviderOption[],
): string | undefined {
  const entries = Object.entries(bindings)
    .flatMap(([childName, binding]) => {
      const provider = providers.find((option) => option.nodeId === binding.providerNodeId);
      if (!provider) return [];
      return [{ key: childName, value: `${provider.localName}${provider.alias ? `.${provider.alias}` : ""}` }];
    })
    .sort((left, right) => left.key.localeCompare(right.key));
  return entries.length ? `\n${renderAssignments(entries, 2).join("\n")}\n  }` : undefined;
}

function renderMultiplicity(
  multiplicity: Multiplicity,
  variables: VariableOption[],
  references: ReferenceOption[],
): { key: string; value: string } | undefined {
  if (multiplicity.kind === "count") {
    const variable = multiplicity.expr.mode === "reference"
      ? variables.find((option) => option.nodeId === multiplicity.expr.refNodeId)
      : undefined;
    const expression = multiplicity.expr.mode === "reference"
      ? variable
        ? `var.${variable.name}`
        : renderReferenceExpression(multiplicity.expr, references)
      : multiplicity.expr.literal;
    return {
      key: "count",
      value: multiplicity.mode === "conditional" ? `${expression} ? 1 : 0` : expression,
    };
  }
  if (multiplicity.kind === "for_each") {
    const variable = variables.find((option) => option.nodeId === multiplicity.variableNodeId);
    return variable ? { key: "for_each", value: `var.${variable.name}` } : undefined;
  }
  return undefined;
}

export function renderModulePreview({
  instanceName,
  namespace,
  moduleName,
  system,
  version,
  registryHost,
  inputs,
  referenceOptions,
  multiplicity,
  variableOptions,
  providerBindings,
  providerOptions,
  optionalFieldVisibility = {},
}: ModulePreviewOptions): string {
  const configuredInputs = inputs.filter((input) => isProvided(input.value)).sort((left, right) => left.name.localeCompare(right.name));
  const lines = [`module ${JSON.stringify(instanceName)} {`];
  const repetition = renderMultiplicity(multiplicity, variableOptions, referenceOptions);
  if (repetition) lines.push(`  ${repetition.key} = ${repetition.value}`);
  lines.push(
    `  source  = ${JSON.stringify(buildModuleSource(registryHost, namespace, moduleName, system))}`,
    `  version = ${JSON.stringify(`~> ${stripV(version)}`)}`,
  );
  const renderedBindings = renderProviderBindings(providerBindings, providerOptions);

  if (configuredInputs.length > 0 || renderedBindings) {
    lines.push("");
    lines.push(...renderAssignments(configuredInputs.map((input) => ({
        key: input.name,
        value: input.value
          ? renderValue(input.type, input.value, referenceOptions, optionalFieldVisibility, moduleInputPath("", `variable:${input.name}`), 1)
          : "null",
      })), 1));
    if (renderedBindings) lines.push(`  providers = {${renderedBindings}`);
  }

  lines.push("}");
  return lines.join("\n");
}