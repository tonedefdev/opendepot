import { describe, expect, it } from "vitest";

import { AGENT_COLOR, DEPOT_COLOR, MODULE_COLOR, PROVIDER_COLOR, SKILL_COLOR, VERSION_COLOR, graphKindColor, kindColor } from "./kindColors";

describe("kindColor", () => {
  it("shares the accent across kinds so icons, not colors, tell them apart", () => {
    const colors = [kindColor("Module"), kindColor("Provider"), kindColor("Skill"), kindColor("Agent")];

    expect(new Set(colors).size).toBe(1);
    expect(colors[0]).toBe("var(--od-accent)");
  });

  it("keeps versions on the neutral color", () => {
    expect(kindColor("Version")).not.toBe(kindColor("Module"));
    expect(kindColor("VersionOverflow")).toBe(VERSION_COLOR);
  });

  it("separates Terraform and AI kinds in the stats and depot graph", () => {
    expect(graphKindColor("module")).toBe(graphKindColor("provider"));
    expect(graphKindColor("skill")).toBe(graphKindColor("agent"));
    expect(graphKindColor("module")).not.toBe(graphKindColor("skill"));
    expect(graphKindColor("depot")).toBe("var(--od-accent)");
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
