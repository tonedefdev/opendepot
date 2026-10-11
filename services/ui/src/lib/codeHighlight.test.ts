import { describe, expect, it } from "vitest";
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
