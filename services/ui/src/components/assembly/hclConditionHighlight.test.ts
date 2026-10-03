import { describe, expect, it } from "vitest";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import { extendHclGrammar, hclConditionRefractor } from "./hclConditionHighlight";

extendHclGrammar(Prism.languages.hcl);

interface HighlightNode {
  value?: string;
  properties?: { className?: string[] };
  children?: HighlightNode[];
}

function tokenValues(node: HighlightNode, tokenName: string): string[] {
  if (node.properties?.className?.includes(tokenName)) {
    return [textContent(node)];
  }
  return (node.children ?? []).flatMap((child) => tokenValues(child, tokenName));
}

function textContent(node: HighlightNode): string {
  return node.value ?? (node.children ?? []).map(textContent).join("");
}

describe("HCL condition highlighting", () => {
  it("highlights traversals, known functions, and for-expression keywords", () => {
    const highlighted = hclConditionRefractor.highlight(
      "alltrue([for _, fn in var.lambda_functions : length(fn.description) <= 100])",
      "hcl",
    ) as unknown as HighlightNode;
    const tokens = highlighted.children ?? [];

    expect(tokens.flatMap((node) => tokenValues(node, "variable"))).toContain("var.lambda_functions");
    expect(tokens.flatMap((node) => tokenValues(node, "function"))).toEqual(["alltrue", "length"]);
    expect(tokens.flatMap((node) => tokenValues(node, "keyword"))).toEqual(["for", "in"]);
  });

  it("does not highlight arbitrary function calls as built-ins", () => {
    const highlighted = hclConditionRefractor.highlight("custom_helper(var.name)", "hcl") as unknown as HighlightNode;

    expect((highlighted.children ?? []).flatMap((node) => tokenValues(node, "function"))).toEqual([]);
    expect((highlighted.children ?? []).flatMap((node) => tokenValues(node, "variable"))).toContain("var.name");
  });

  it("applies the same expression tokens to the Prism code preview", () => {
    const html = Prism.highlight(
      "alltrue([for _, fn in var.lambda_functions : length(fn.description) <= 100])",
      Prism.languages.hcl,
      "hcl",
    );

    expect(html).toContain('<span class="token variable">var.lambda_functions</span>');
    expect(html).toContain('<span class="token function">alltrue</span>');
    expect(html).toContain('<span class="token function">length</span>');
    expect(html).toMatch(/<span class="token [^"]*keyword[^"]*">for<\/span>/);
    expect(html).toMatch(/<span class="token [^"]*keyword[^"]*">in<\/span>/);
  });
});