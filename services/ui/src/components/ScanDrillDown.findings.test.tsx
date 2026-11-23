// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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

function finding(overrides: Partial<SecurityFinding>): SecurityFinding {
  return {
    id: "CVE-2026-1000",
    severity: "LOW",
    title: "openssl flaw",
    message: "openssl (1.0.0)",
    resolution: "Upgrade openssl",
    vulnerabilityID: "CVE-2026-1000",
    pkgName: "openssl",
    installedVersion: "1.0.0",
    ...overrides,
  };
}

const findings: SecurityFinding[] = [
  finding({ id: "CVE-A", severity: "CRITICAL", title: "Remote code execution", pkgName: "curl", message: "curl (7.0)" }),
  finding({ id: "CVE-B", severity: "HIGH", title: "Heap overflow", pkgName: "zlib", message: "zlib (1.2)" }),
  finding({ id: "CVE-C", severity: "MEDIUM", title: "Timing leak", pkgName: "libssh", message: "libssh (0.9)" }),
];

// Overrides the component's mount-time fetch so each test controls what the refresh and version calls return.
function stubFetch(response: Partial<{ sourceScanFindings: SecurityFinding[]; scannedVersions: string[]; binaryVersions: string[] }> = {}) {
  const fetchMock = vi.fn(async () => ({
    ok: true,
    json: async () => ({ sourceScanFindings: [], scannedVersions: [], ...response }),
  }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderDrillDown(props: Partial<React.ComponentProps<typeof ScanDrillDown>> = {}) {
  return render(
    <ThemeProvider theme={theme}>
      <ScanDrillDown
        namespace="team"
        kind="module"
        name="network"
        initialSourceScanFindings={findings}
        initialBinaryScanFindings={{}}
        versions={[version]}
        {...props}
      />
    </ThemeProvider>,
  );
}

// MUI Select renders its combobox inside the form control that holds the matching InputLabel.
function selectFor(label: string): HTMLElement {
  const labelEl = screen.getAllByText(label, { selector: "label" })[0];
  const control = labelEl.closest(".MuiFormControl-root");
  const combobox = control?.querySelector('[role="combobox"]');
  if (!(combobox instanceof HTMLElement)) throw new Error(`no select for ${label}`);
  return combobox;
}

function pickOption(label: string, option: string) {
  fireEvent.mouseDown(selectFor(label));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

function tableRowTexts(): string[] {
  const table = screen.getByRole("table");
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.textContent ?? "");
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ScanDrillDown empty states", () => {
  it("shows the no-findings message when there are no findings", () => {
    stubFetch();
    renderDrillDown({ initialSourceScanFindings: [] });

    expect(screen.getByText("No findings.")).toBeTruthy();
    expect(screen.getByText("Source Scan Findings (0)")).toBeTruthy();
  });

  it("shows the filtered-empty message when no finding matches the search", () => {
    stubFetch();
    renderDrillDown();

    fireEvent.change(screen.getByPlaceholderText("Search findings…"), { target: { value: "nothing matches this" } });

    expect(screen.getByText("No findings match the current filters.")).toBeTruthy();
  });
});

describe("ScanDrillDown filters", () => {
  it("filters rows by severity and restores them when the filter is cleared", () => {
    stubFetch();
    renderDrillDown();

    pickOption("Severity", "HIGH");
    expect(tableRowTexts()).toEqual([expect.stringContaining("CVE-B")]);

    pickOption("Severity", "All severities");
    expect(tableRowTexts()).toHaveLength(3);
  });

  it("matches the search query against the title and package name", () => {
    stubFetch();
    renderDrillDown();

    fireEvent.change(screen.getByPlaceholderText("Search findings…"), { target: { value: "LIBSSH" } });
    expect(tableRowTexts()).toEqual([expect.stringContaining("CVE-C")]);

    fireEvent.change(screen.getByPlaceholderText("Search findings…"), { target: { value: "heap" } });
    expect(tableRowTexts()).toEqual([expect.stringContaining("CVE-B")]);
  });

  it("shows the Clear button only while a filter is active and resets every filter", () => {
    stubFetch();
    renderDrillDown();

    expect(screen.queryByRole("button", { name: /Clear/ })).toBeNull();

    fireEvent.change(screen.getByPlaceholderText("Search findings…"), { target: { value: "curl" } });
    pickOption("Order", "Asc");
    fireEvent.click(screen.getByRole("button", { name: /Clear/ }));

    expect(screen.queryByRole("button", { name: /Clear/ })).toBeNull();
    expect(tableRowTexts()).toHaveLength(3);
    expect((screen.getByPlaceholderText("Search findings…") as HTMLInputElement).value).toBe("");
  });

  it("sorts by title in ascending order", () => {
    stubFetch();
    renderDrillDown();

    pickOption("Sort by", "Title");
    pickOption("Order", "Asc");

    expect(tableRowTexts()[0]).toContain("CVE-B");
    expect(tableRowTexts()[2]).toContain("CVE-C");
  });

  it("sorts by ID in descending order", () => {
    stubFetch();
    renderDrillDown();

    pickOption("Sort by", "ID");

    expect(tableRowTexts()[0]).toContain("CVE-C");
  });
});

describe("ScanDrillDown pagination", () => {
  const many = Array.from({ length: 12 }, (_, i) =>
    finding({ id: `CVE-P${String(i).padStart(2, "0")}`, title: `Finding ${i}`, severity: "LOW" }),
  );

  it("pages through findings ten at a time by default", () => {
    stubFetch();
    renderDrillDown({ initialSourceScanFindings: many });

    expect(screen.getByText("1–10 of 12")).toBeTruthy();
    expect(tableRowTexts()).toHaveLength(10);

    fireEvent.click(screen.getByRole("button", { name: "Go to next page" }));

    expect(screen.getByText("11–12 of 12")).toBeTruthy();
    expect(tableRowTexts()).toHaveLength(2);
  });

  it("changes the page size and resets to the first page", () => {
    stubFetch();
    renderDrillDown({ initialSourceScanFindings: many });

    fireEvent.click(screen.getByRole("button", { name: "Go to next page" }));
    fireEvent.mouseDown(screen.getByRole("combobox", { name: /Rows per page/ }));
    fireEvent.click(screen.getByRole("option", { name: "5" }));

    expect(screen.getByText("1–5 of 12")).toBeTruthy();
    expect(tableRowTexts()).toHaveLength(5);
  });
});

describe("ScanDrillDown field fallbacks", () => {
  it("uses the vulnerability ID, package name, and fixed version when the primary fields are empty", () => {
    stubFetch();
    renderDrillDown({
      initialSourceScanFindings: [
        finding({ id: "", vulnerabilityID: "GHSA-xyz", title: "", pkgName: "left-pad", resolution: "", fixedVersion: "2.0.0" }),
      ],
    });

    const row = tableRowTexts()[0];
    expect(row).toContain("GHSA-xyz");
    expect(row).toContain("left-pad");
    expect(row).toContain("Upgrade to 2.0.0");
  });

  it("falls back to Untitled finding and UNKNOWN severity when nothing identifies the finding", () => {
    stubFetch();
    renderDrillDown({
      initialSourceScanFindings: [finding({ id: "", vulnerabilityID: "", title: "", pkgName: "", severity: "" })],
    });

    const row = tableRowTexts()[0];
    expect(row).toContain("Untitled finding");
    expect(row).toContain("UNKNOWN");
  });
});

describe("ScanDrillDown refresh and version selection", () => {
  it("refetches scan findings from the refresh button", async () => {
    const fetchMock = stubFetch();
    renderDrillDown();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole("button", { name: "refresh scan findings" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock).toHaveBeenLastCalledWith("/api/scan-findings/team/module/network");
  });

  it("replaces the findings with the refreshed response", async () => {
    stubFetch({ sourceScanFindings: [finding({ id: "CVE-NEW", title: "Fresh finding" })] });
    renderDrillDown();

    await waitFor(() => expect(screen.getAllByText("Fresh finding").length).toBeGreaterThan(0));
    expect(screen.getByText("Source Scan Findings (1)")).toBeTruthy();
  });

  it("keeps the existing findings when the refresh request fails", async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, status: 500, json: async () => ({}) }));
    vi.stubGlobal("fetch", fetchMock);
    renderDrillDown();

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.getByText("Source Scan Findings (3)")).toBeTruthy();
  });

  it("requests the newly chosen scanned version", async () => {
    const fetchMock = stubFetch({ sourceScanFindings: findings, scannedVersions: ["1.0.0", "1.1.0"] });
    renderDrillDown();

    await waitFor(() => expect(selectFor("Version")).toBeTruthy());
    pickOption("Version", "1.1.0");

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith("/api/scan-findings/team/module/network?version=1.1.0"),
    );
  });
});

describe("ScanDrillDown binary findings", () => {
  const binary = {
    "linux/amd64": [finding({ id: "CVE-BIN-1", title: "Binary flaw" })],
    "linux/arm64": [finding({ id: "CVE-BIN-2", title: "ARM flaw" })],
  };

  it("shows the binary accordion expanded when binary findings exist", () => {
    stubFetch();
    renderDrillDown({ initialBinaryScanFindings: binary });

    expect(screen.getByText("Binary Scan Findings (1)")).toBeTruthy();
    expect(screen.getAllByText("Binary flaw").length).toBeGreaterThan(0);
  });

  it("offers an architecture select when several architectures exist on one OS", () => {
    stubFetch();
    renderDrillDown({ initialBinaryScanFindings: binary });

    expect(selectFor("Arch").textContent).toBe("amd64");
    expect(screen.queryByText("OS", { selector: "label" })).toBeNull();
  });

  it("switches to the other architecture's findings", () => {
    stubFetch();
    renderDrillDown({ initialBinaryScanFindings: binary });

    pickOption("Arch", "arm64");

    expect(screen.getAllByText("ARM flaw").length).toBeGreaterThan(0);
    expect(screen.getByText("Binary Scan Findings (1)")).toBeTruthy();
  });
});
