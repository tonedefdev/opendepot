import type { ProviderSchemaBlock, CtyType } from "@/lib/api";
import { isKnownHclExpression, renderHclHeredoc, renderHclStringLiteral } from "./typeSpec";
import { renderReferenceExpression } from "./hclReference";
import type { ProviderConfiguration, ReferenceOption } from "./types";

function indentation(depth: number): string {
  return "  ".repeat(depth);
}

function typeKind(type: CtyType): string {
  if (typeof type === "string") return type;
  return Array.isArray(type) && type.length > 0 ? String(type[0]) : "dynamic";
}

function indentContinuations(value: string, depth: number): string {
  const indent = indentation(depth);
  return value
    .replace(/\r\n/g, "\n")
    .split("\n")
    .map((line, index) => index > 0 && line ? `${indent}${line}` : line)
    .join("\n");
}

function renderAttributeValue(type: CtyType, value: ProviderConfiguration["arguments"][string], references: ReferenceOption[], depth: number): string | undefined {
  if (!value) return undefined;
  if (value.mode === "reference") return renderReferenceExpression(value, references);

  const literal = value.literal.trim();
  if (!literal) return undefined;

  if (typeKind(type) === "string") {
    const heredoc = renderHclHeredoc(value.literal, depth);
    if (heredoc) return heredoc;
    if (isKnownHclExpression(literal)) return indentContinuations(literal, depth);
    return renderHclStringLiteral(value.literal);
  }

  return value.literal;
}

function renderConfigurationContents(
  schema: ProviderSchemaBlock,
  configuration: ProviderConfiguration,
  references: ReferenceOption[],
  depth: number,
  additionalAttributes: Array<{ name: string; value: string }> = [],
): string[] {
  const indent = indentation(depth);
  const attributes = [
    ...additionalAttributes,
    ...Object.entries(schema.attributes ?? {})
    .filter(([, attribute]) => attribute.required || attribute.optional)
    .sort(([left], [right]) => left.localeCompare(right))
    .flatMap(([name, attribute]) => {
      const rendered = renderAttributeValue(attribute.type, configuration.arguments[name], references, depth);
      return rendered === undefined ? [] : [{ name, value: rendered }];
    }),
  ];
  const width = attributes.reduce((longest, attribute) => Math.max(longest, attribute.name.length), 0);
  const lines = attributes.map(({ name, value }) => `${indent}${name.padEnd(width)} = ${value}`);
  const blocks = Object.entries(schema.blocks ?? {}).sort(([left], [right]) => left.localeCompare(right));

  for (const [name, block] of blocks) {
    const instances = configuration.blocks[name] ?? [];
    if (instances.length === 0) continue;
    if (lines.length > 0) lines.push("");

    instances.forEach((instance, index) => {
      if (index > 0) lines.push("");
      lines.push(`${indent}${name} {`);
      lines.push(...renderConfigurationContents(block.block, instance, references, depth + 1));
      lines.push(`${indent}}`);
    });
  }

  return lines;
}

export function renderProviderPreview(
  localName: string,
  alias: string,
  schema: ProviderSchemaBlock,
  configuration: ProviderConfiguration,
  references: ReferenceOption[],
): string {
  const additionalAttributes = alias.trim() ? [{ name: "alias", value: JSON.stringify(alias.trim()) }] : [];
  const contents = renderConfigurationContents(schema, configuration, references, 1, additionalAttributes);
  const providerName = JSON.stringify(localName);
  if (contents.length === 0) return `provider ${providerName} {}`;
  const opening = `provider ${providerName} {`;
  return [opening, ...contents, "}"].join("\n");
}