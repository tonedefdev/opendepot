import { describe, expect, it } from "vitest";

import { AGENT_COLOR, DEPOT_COLOR, MODULE_COLOR, PROVIDER_COLOR, SKILL_COLOR, VERSION_COLOR, kindColor, kindTint } from "./kindColors";

describe("kindColor", () => {
  it("shares the accent across kinds so icons, not colors, tell them apart", () => {
    const colors = [kindColor("Module"), kindColor("Provider"), kindColor("Skill"), kindColor("Agent"), kindColor("depot")];

    expect(new Set(colors).size).toBe(1);
    expect(colors[0]).toBe("var(--od-accent)");
  });

  it("keeps versions on the neutral color", () => {
    expect(kindColor("Version")).not.toBe(kindColor("Module"));
    expect(kindColor("VersionOverflow")).toBe(VERSION_COLOR);
  });

  it("maps kinds case-insensitively", () => {
    expect(kindColor("module")).toBe(MODULE_COLOR);
    expect(kindColor("provider")).toBe(PROVIDER_COLOR);
    expect(kindColor("skill")).toBe(SKILL_COLOR);
    expect(kindColor("AGENT")).toBe(AGENT_COLOR);
    expect(kindColor("depot")).toBe(DEPOT_COLOR);
    expect(kindColor("version")).toBe(VERSION_COLOR);
  });

  it("falls back to the module color for unknown kinds", () => {
    expect(kindColor("")).toBe(MODULE_COLOR);
    expect(kindColor("Unknown")).toBe(MODULE_COLOR);
  });
});

describe("kindTint", () => {
  it("mixes the kind color into a transparent background", () => {
    expect(kindTint("module", 10)).toBe("color-mix(in srgb, var(--od-accent) 10%, transparent)");
  });
});
