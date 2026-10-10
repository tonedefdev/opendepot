import { describe, expect, it } from "vitest";

import { probabilitySeverity, riskLevelSeverity } from "./jevSeverity";

describe("probabilitySeverity", () => {
  it("uses the CLI thresholds at the boundaries", () => {
    expect(probabilitySeverity(0.5, false)).toBe("error");
    expect(probabilitySeverity(0.49, false)).toBe("warning");
    expect(probabilitySeverity(0.25, false)).toBe("warning");
    expect(probabilitySeverity(0.24, false)).toBe("success");
  });

  it("inverts the Safe row so a high safe probability is green", () => {
    expect(probabilitySeverity(0.9, true)).toBe("success");
    expect(probabilitySeverity(0.75, true)).toBe("warning");
    expect(probabilitySeverity(0.5, true)).toBe("error");
    expect(probabilitySeverity(0.1, true)).toBe("error");
  });
});

describe("riskLevelSeverity", () => {
  it("maps the CLI risk levels", () => {
    expect(riskLevelSeverity("Minimal")).toBe("success");
    expect(riskLevelSeverity("Low")).toBe("success");
    expect(riskLevelSeverity("Moderate")).toBe("warning");
    expect(riskLevelSeverity("High")).toBe("error");
    expect(riskLevelSeverity("Critical")).toBe("error");
  });

  it("leaves unknown levels neutral", () => {
    expect(riskLevelSeverity("")).toBeUndefined();
    expect(riskLevelSeverity("Unknown")).toBeUndefined();
  });
});
