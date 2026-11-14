import { describe, expect, it } from "vitest";
import Prism from "prismjs";
import { resolvePrismLanguage } from "./codeHighlight";

describe("resolvePrismLanguage", () => {
  it.each([
    ["bash", "bash"],
    ["sh", "bash"],
    ["shell", "bash"],
    ["zsh", "bash"],
    ["go", "go"],
    ["golang", "go"],
    ["python", "python"],
    ["py", "python"],
    ["typescript", "typescript"],
    ["ts", "typescript"],
    ["javascript", "javascript"],
    ["js", "javascript"],
    ["yaml", "yaml"],
    ["yml", "yaml"],
    ["hcl", "hcl"],
    ["terraform", "hcl"],
    ["TypeScript", "typescript"],
  ])("maps %s to %s", (input, expected) => {
    expect(resolvePrismLanguage(input)).toBe(expected);
  });

  it("returns null for languages without a grammar", () => {
    expect(resolvePrismLanguage("text")).toBeNull();
    expect(resolvePrismLanguage("rust")).toBeNull();
  });
});

describe("HCL block headers", () => {
  function keywordTokens(code: string): string[] {
    return Prism.tokenize(code, Prism.languages.hcl)
      .filter((token): token is Prism.Token => typeof token !== "string" && token.type === "keyword")
      .map((token) => String(token.content));
  }

  it.each([
    ['opendepot {\n}', "opendepot"],
    ['agent "accessibility" {\n}', "agent"],
    ['skill "lint" "v1" {\n}', "skill"],
  ])("colors the block type in %j as a keyword", (code, keyword) => {
    expect(keywordTokens(code)).toContain(keyword);
  });

  it("does not color attribute names or labels as keywords", () => {
    expect(keywordTokens('agent "accessibility" {\n  version = "1.0.0"\n}')).toEqual(["agent"]);
  });
});
