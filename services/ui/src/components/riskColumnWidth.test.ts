import { describe, expect, it } from "vitest";
import type { BrowseResource } from "@/lib/api";
import { RISK_COLUMN_MIN_PX, riskColumnWidth } from "./riskColumnWidth";

const measure = (text: string) => text.length * 8;

function resource(overrides: Partial<BrowseResource>): BrowseResource {
  return {
    kind: "module",
    namespace: "ns",
    name: "n",
    latestVersion: "1.0.0",
    syncStatus: "Synced",
    synced: true,
    public: false,
    provider: "aws",
    repoUrl: "",
    providerNamespace: "",
    upstreamRegistry: "",
    platforms: [],
    scanCounts: null,
    lastScanned: "",
    ...overrides,
  };
}

const counts = (c: Partial<BrowseResource["scanCounts"] & object>) => ({
  critical: 0, high: 0, medium: 0, low: 0, unknown: 0, exempted: 0, ...c,
});

describe("riskColumnWidth", () => {
  it("uses the header minimum when no row has findings", () => {
    expect(riskColumnWidth([resource({})], measure, "sans-serif")).toBe(RISK_COLUMN_MIN_PX);
  });

  it("grows to fit five chips with four-digit counts", () => {
    const row = resource({ scanCounts: counts({ critical: 1000, high: 1000, medium: 1000, low: 1000, unknown: 1000 }) });
    expect(riskColumnWidth([row], measure, "sans-serif")).toBeGreaterThan(RISK_COLUMN_MIN_PX);
  });

  it("uses the widest row, not the sum of rows", () => {
    const small = resource({ scanCounts: counts({ low: 1 }) });
    const large = resource({ scanCounts: counts({ high: 100, medium: 100 }) });
    const onlyLarge = riskColumnWidth([large], measure, "sans-serif");
    expect(riskColumnWidth([small, large], measure, "sans-serif")).toBe(onlyLarge);
  });

  it("measures chip text at the weight the chips render with", () => {
    const fonts: string[] = [];
    const recordFont = (text: string, font: string) => {
      fonts.push(font);
      return text.length * 8;
    };
    riskColumnWidth([resource({ scanCounts: counts({ low: 1 }) })], recordFont, "sans-serif");
    expect(fonts).toContain("600 12px sans-serif");
  });

  it("adds the meter width for agent and skill rows", () => {
    const wide = (text: string) => text.length * 12;
    const withoutMeter = resource({ kind: "module", jevRiskLevel: "Critical", scanCounts: counts({ high: 1000, medium: 1000 }) });
    const withMeter = resource({ kind: "agent", jevRiskLevel: "Critical", scanCounts: counts({ high: 1000, medium: 1000 }) });
    expect(riskColumnWidth([withMeter], wide, "sans-serif")).toBeGreaterThan(
      riskColumnWidth([withoutMeter], wide, "sans-serif"),
    );
  });
});
