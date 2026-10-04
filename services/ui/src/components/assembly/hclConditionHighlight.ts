import rehypePrismGenerator from "rehype-prism-plus/generator";
import type { Grammar } from "prismjs";
import type { Pluggable } from "unified";
import { refractor } from "refractor";
import hcl from "refractor/lang/hcl";

const builtInFunctions = [
  "abs", "alltrue", "anytrue", "base64decode", "base64encode", "basename", "ceil", "chunklist",
  "cidrhost", "cidrnetmask", "cidrsubnet", "coalesce", "coalescelist", "compact", "concat", "contains",
  "csvdecode", "dirname", "distinct", "element", "file", "filebase64", "fileexists", "flatten", "floor",
  "format", "formatdate", "formatlist", "index", "indent", "join", "jsondecode", "jsonencode", "keys",
  "length", "log", "lookup", "lower", "max", "merge", "min", "nonsensitive", "one", "parseint", "range",
  "regex", "regexall", "replace", "reverse", "sensitive", "setintersection", "setproduct", "setsubtract",
  "setunion", "sha1", "sha256", "slice", "sort", "split", "strrev", "substr", "sum", "timeadd", "title",
  "tobool", "tomap", "tonumber", "tolist", "toset", "tostring", "trim", "trimprefix", "trimsuffix",
  "trimspace", "try", "upper", "urlencode", "values", "zipmap",
];

export function extendHclGrammar(grammar: Grammar): void {
  type NestedGrammarToken = { inside?: Record<string, unknown> };
  const grammarTokens = grammar as unknown as Record<string, unknown>;
  grammarTokens.variable = /\b(?:var|local|each|module|data|path|terraform|self|count)\b(?:\.[A-Za-z_][\w-]*)+/;
  grammarTokens.function = new RegExp(`\\b(?:${builtInFunctions.join("|")})\\b(?=\\s*\\()`, "i");
  grammarTokens.expressionKeyword = { pattern: /\b(?:for|in|if|null)\b/, alias: "keyword" };
  grammarTokens.operator = /(?:==|!=|<=|>=|&&|\|\||[=<>!+*/%?-])/;

  const stringToken = grammarTokens.string as NestedGrammarToken | undefined;
  const interpolation = stringToken?.inside?.interpolation as NestedGrammarToken | undefined;
  if (interpolation?.inside) {
    interpolation.inside.type = {
      pattern: /\b(?:count|data|each|local|module|path|self|terraform|var)\b(?:\.[\w*]+)+/i,
      alias: "variable",
    };
  }
}

if (!refractor.registered("hcl")) {
  refractor.register(hcl);
}

extendHclGrammar(refractor.languages.hcl as Grammar);

export const hclConditionPlugins: Pluggable[] = [[rehypePrismGenerator(refractor), { ignoreMissing: true }]];
export { refractor as hclConditionRefractor };

export function hclEditorColorVariables(mode: "dark" | "light") {
  return {
    "--color-prettylights-syntax-sublimelinter-gutter-mark": mode === "dark" ? "#b1bac4" : "#57606a",
    "--color-prettylights-syntax-string-regexp": mode === "dark" ? "#d2a8ff" : "#8250df",
  };
}