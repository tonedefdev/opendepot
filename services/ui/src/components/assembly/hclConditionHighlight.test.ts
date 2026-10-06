import { describe, expect, it } from "vitest";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import { builtInFunctions, extendHclGrammar, getHclFunctionHint, hclConditionRefractor } from "./hclConditionHighlight";

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
  it("matches the callable functions documented by OpenTofu", () => {
    expect(Object.keys(builtInFunctions).sort()).toEqual([
      "abs", "abspath", "alltrue", "anytrue", "assumeequal", "assumelistlength", "assumelistlengthmax", "assumelistlengthmin",
      "assumemaplength", "assumemaplengthmax", "assumemaplengthmin", "assumenotnull", "assumesetlength", "assumesetlengthmax",
      "assumesetlengthmin", "assumestringprefix", "base64decode", "base64encode", "base64gunzip", "base64gzip", "base64sha256",
      "base64sha512", "basename", "bcrypt", "can", "ceil", "chomp", "chunklist", "cidrcontains", "cidrhost", "cidrnetmask",
      "cidrsubnet", "cidrsubnets", "coalesce", "coalescelist", "compact", "concat", "contains", "convert", "csvdecode", "dirname",
      "distinct", "element", "endswith", "ephemeralasnull", "file", "filebase64", "filebase64sha256", "filebase64sha512", "fileexists",
      "filemd5", "fileset", "filesha1", "filesha256", "filesha512", "flatten", "floor", "format", "formatdate", "formatlist", "indent",
      "index", "issensitive", "join", "jsondecode", "jsonencode", "keys", "length", "log", "lookup", "lower", "matchkeys", "max", "md5",
      "merge", "min", "nonsensitive", "one", "parseint", "pathexpand", "plantimestamp", "pow", "range", "regex", "regexall", "replace",
      "reverse", "rsadecrypt", "sensitive", "setintersection", "setproduct", "setsubtract", "setunion", "sha1", "sha256", "sha512", "signum",
      "slice", "sort", "split", "startswith", "strcontains", "strrev", "substr", "sum", "templatefile", "templatestring", "textdecodebase64",
      "textencodebase64", "timeadd", "timecmp", "timestamp", "title", "tobool", "tolist", "tomap", "tonumber", "toset", "tostring", "transpose",
      "trim", "trimprefix", "trimspace", "trimsuffix", "try", "type", "upper", "urldecode", "urlencode", "uuid", "uuidv5", "values", "yamldecode",
      "yamlencode", "zipmap",
    ]);
  });

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

  it("highlights traversals inside template interpolation", () => {
    const highlighted = hclConditionRefractor.highlight(
      '"lambda-${replace(each.key, "_", "-")}"',
      "hcl",
    ) as unknown as HighlightNode;
    const tokens = highlighted.children ?? [];

    expect(tokens.flatMap((node) => tokenValues(node, "variable"))).toContain("each.key");
    expect(tokens.flatMap((node) => tokenValues(node, "function"))).toContain("replace");
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

  it("returns the innermost built-in function hint at the caret", () => {
    const source = 'alltrue([for item in var.items : length(replace(item.name, ")", "") ) > 0])';
    const caretPosition = source.indexOf("item.name") + 2;

    expect(getHclFunctionHint(source, caretPosition)).toMatchObject({
      name: "replace",
      docsUrl: "https://opentofu.org/docs/language/functions/replace/",
    });
  });

  it("links the index hint to its canonical documentation page", () => {
    expect(getHclFunctionHint("index(var.items, 0)", 8)?.docsUrl)
      .toBe("https://opentofu.org/docs/language/functions/index_function/");
  });

  it("highlights and hints strcontains", () => {
    const source = 'strcontains(var.name, "tofu")';
    const highlighted = hclConditionRefractor.highlight(source, "hcl") as unknown as HighlightNode;

    expect((highlighted.children ?? []).flatMap((node) => tokenValues(node, "function"))).toContain("strcontains");
    expect(getHclFunctionHint(source, 5)).toMatchObject({
      name: "strcontains",
      hint: "Tests whether a string contains a specified substring.",
      docsUrl: "https://opentofu.org/docs/language/functions/strcontains/",
    });
  });

  it("does not return a hint for user-defined function calls", () => {
    expect(getHclFunctionHint("custom_helper(var.name)", 8)).toBeUndefined();
  });
});