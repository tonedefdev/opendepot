import { describe, expect, it } from "vitest";

import { AGENT_COLOR, MODULE_COLOR, PROVIDER_COLOR, SKILL_COLOR, kindColor } from "./kindColors";

describe("kindColor", () => {
  it("gives each kind a distinct color", () => {
    const colors = [kindColor("Module"), kindColor("Provider"), kindColor("Skill"), kindColor("Agent")];

    expect(new Set(colors).size).toBe(4);
  });

  it("maps kinds case-insensitively", () => {
    expect(kindColor("module")).toBe(MODULE_COLOR);
    expect(kindColor("provider")).toBe(PROVIDER_COLOR);
    expect(kindColor("skill")).toBe(SKILL_COLOR);
    expect(kindColor("AGENT")).toBe(AGENT_COLOR);
  });

  it("falls back to the module color for unknown kinds", () => {
    expect(kindColor("")).toBe(MODULE_COLOR);
    expect(kindColor("Depot")).toBe(MODULE_COLOR);
  });
});
