// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ScanDrillDown from "./ScanDrillDown";
import theme from "@/theme";
import type { BrowseVersionSummary, SecurityFinding } from "@/lib/api";

const version: BrowseVersionSummary = {
  version: "1.0.0",
  syncStatus: "Synced",
  os: "",
  arch: "",
  lastScanned: "2026-01-01T00:00:00Z",
  synced: true,
  scanCounts: null,
};

function exemptedFinding(exemptedBy?: string): SecurityFinding {
  return {
    id: "CVE-2026-0001",
    severity: "HIGH",
    title: "left-pad",
    message: "left-pad (1.0.0)",
    resolution: "",
    exempted: true,
    exemptionReason: "patched by the base image",
    exemptedBy,
    vulnerabilityID: "CVE-2026-0001",
    pkgName: "left-pad",
    installedVersion: "1.0.0",
  };
}

// renderDrillDown mounts the component with the findings the scan-findings proxy returns on refresh, so the chip
// reflects what the server sent.
function renderDrillDown(findings: SecurityFinding[]) {
  const fetchMock = vi.fn(async () => ({
    ok: true,
    json: async () => ({ sourceScanFindings: findings, scannedVersions: ["1.0.0"] }),
  }));
  vi.stubGlobal("fetch", fetchMock);

  render(
    <ThemeProvider theme={theme}>
      <ScanDrillDown
        namespace="team"
        kind="module"
        name="network"
        initialSourceScanFindings={findings}
        initialBinaryScanFindings={{}}
        versions={[version]}
      />
    </ThemeProvider>,
  );

  return fetchMock;
}

describe("ScanDrillDown exemption chip", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows a plain Exempted chip and no policy name when the server withholds it", async () => {
    const fetchMock = renderDrillDown([exemptedFinding()]);

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    expect(screen.getAllByText("Exempted").length).toBeGreaterThan(0);
    expect(screen.queryByText(/Exempted by/)).toBeNull();
    expect(screen.queryByText(/ScanPolicy/)).toBeNull();
  });

  it("shows the policy name when the server returns it", async () => {
    const fetchMock = renderDrillDown([exemptedFinding("platform-baseline")]);

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    expect(screen.getAllByText("Exempted by platform-baseline").length).toBeGreaterThan(0);
  });
});
