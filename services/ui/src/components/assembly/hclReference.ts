import type { FieldValue, ReferenceOption } from "./types";

export function renderReferenceExpression(value: FieldValue, references: ReferenceOption[]): string {
  const target = references.find((option) => option.nodeId === value.refNodeId && option.output === value.refOutput);
  if (!target) return "null";

  let expression = target.sourceKind === "variable" ? `var.${target.instanceName}` : `module.${target.instanceName}`;
  if (value.refOutput) expression += `.${value.refOutput}`;

  if (target.sourceKind === "module" && target.sourceMultiplicity === "count") {
    if (value.refSelector?.kind === "all") expression = `module.${target.instanceName}[*].${value.refOutput}`;
    else if (value.refSelector?.kind === "index") expression = `module.${target.instanceName}[${value.refSelector.expr.literal}].${value.refOutput}`;
  } else if (target.sourceKind === "module" && target.sourceMultiplicity === "for_each") {
    if (value.refSelector?.kind === "all") expression = `[for instance in module.${target.instanceName} : instance.${value.refOutput}]`;
    else if (value.refSelector?.kind === "key") {
      const selector = target.forEachKeys?.includes(value.refSelector.expr.literal)
        ? JSON.stringify(value.refSelector.expr.literal)
        : value.refSelector.expr.literal;
      expression = `module.${target.instanceName}[${selector}].${value.refOutput}`;
    }
  }

  if (value.refOutputSelector?.kind === "index" || value.refOutputSelector?.kind === "key") {
    expression += `[${value.refOutputSelector.expr.literal}]`;
  }
  if (value.refAttributePath) expression += `.${value.refAttributePath}`;
  return expression;
}