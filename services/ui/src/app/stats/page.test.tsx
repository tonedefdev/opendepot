// @vitest-environment jsdom

import * as React from "react";
import { ThemeProvider } from "@mui/material/styles";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import StatsPage from "./page";
import OpenNeedsReviewButton from "@/components/OpenNeedsReviewButton";
import RefreshIconButton from "@/components/RefreshIconButton";
import theme from "@/theme";
import { getDepotsGraph, getScanPolicyCatalog, getStats } from "@/lib/api";
import { formatJevInputCost } from "@/lib/jevCost";
import type { BrowseDepotGraph, BrowseStats, PopularResource, ScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";

vi.mock("@/components/RefreshIconButton", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/PageHeader", () => ({
  default: ({ title, actions }: { title: string; actions?: React.ReactNode }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  ),
}));
vi.mock("@/components/OpenNeedsReviewButton", () => ({
  default: vi.fn(({ children }: { children: React.ReactNode }) => <div data-testid="needs-review-link">{children}</div>),
}));
vi.mock("@/components/DepotKindIcon", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/components/DepotKindIcon")>();
  return { DepotKindIcon: () => null, isDepotKind: actual.isDepotKind };
});
vi.mock("@/lib/api", () => ({
  getStats: vi.fn(),
  getDepotsGraph: vi.fn(),
  getScanPolicyCatalog: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn((): never => {
    throw new Error("NEXT_REDIRECT");
  }),
}));

function popular(overrides: Partial<PopularResource>): PopularResource {
  return {
    namespace: "acme",
    kind: "module",
    name: "vpc",
    version: "1.0.0",
    downloadCount: 1,
    ...overrides,
  };
}

function baseStats(overrides: Partial<BrowseStats> = {}): BrowseStats {
  return {
    totalModules: 4,
    modulesSynced: 4,
    totalProviders: 2,
    providersSynced: 2,
    totalSkills: 1,
    skillsSynced: 1,
    totalAgents: 1,
    agentsSynced: 1,
    totalVersions: 8,
    versionsSynced: 8,
    totalStorageBytes: 0,
    totalDownloads: 0,
    downloadWindow: "30d",
    syncHealth: { syncedVersions: 8, unsyncedVersions: 0, failedVersions: 0 },
    securityPosture: { critical: 0, high: 0, medium: 0, low: 0, unknown: 0, exempted: 0, totalAffectedResources: 0 },
    jev: {
      assessedVersions: 0,
      blockedVersions: 0,
      needsReviewVersions: 0,
      exemptedVersions: 0,
      failedVersions: 0,
      assessedChunks: 0,
      inputTokens: 0,
      riskLevels: { minimal: 0, low: 0, moderate: 0, high: 0, critical: 0 },
    },
    storageDistribution: [],
    mostDownloaded: { modules: [], providers: [], skills: [], agents: [] },
    ...overrides,
  };
}

function graph(totalDepots: number): BrowseDepotGraph {
  return {
    summary: { totalDepots, totalModules: 0, totalProviders: 0, totalSkills: 0, totalAgents: 0 },
  } as unknown as BrowseDepotGraph;
}

const writableCatalog = { writesEnabled: true } as ScanPolicyCatalog;

async function renderPage() {
  const element = await StatsPage();
  return render(<ThemeProvider theme={theme}>{element}</ThemeProvider>);
}

function statCard(label: string): HTMLElement {
  return screen.getAllByText(label)[0].closest(".MuiPaper-root") as HTMLElement;
}

describe("StatsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getServerSessionToken).mockResolvedValue("token-123");
    vi.mocked(getStats).mockResolvedValue(baseStats());
    vi.mocked(getDepotsGraph).mockResolvedValue(graph(5));
    vi.mocked(getScanPolicyCatalog).mockResolvedValue(writableCatalog);
  });

  it("loads stats and depot graph with the session token", async () => {
    await renderPage();

    expect(getStats).toHaveBeenCalledWith(undefined, "token-123");
    expect(getDepotsGraph).toHaveBeenCalledWith(undefined, "token-123");
    expect(getScanPolicyCatalog).toHaveBeenCalledWith("token-123");
    expect(screen.getByRole("heading", { name: "Registry Statistics" })).toBeTruthy();
    expect(RefreshIconButton).toHaveBeenCalled();
  });

  it("renders summary cards with totals, sync notes, and formatted storage", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        totalModules: 4,
        modulesSynced: 3,
        totalStorageBytes: 1536,
        totalDownloads: 12345,
        downloadWindow: "7d",
      }),
    );
    vi.mocked(getDepotsGraph).mockResolvedValue(graph(9));

    await renderPage();

    expect(statCard("Modules").textContent).toContain("3 of 4 synced");
    expect(statCard("Depots").textContent).toContain("9");
    expect(statCard("Storage Used").textContent).toContain("1.5 KB");
    expect(statCard("Downloads (7d)").textContent).toContain("12,345");
  });

  it("omits sync notes when every count is synced and shows 0 B for empty storage", async () => {
    await renderPage();

    expect(screen.queryByText(/of \d+ synced/)).toBeNull();
    expect(statCard("Storage Used").textContent).toContain("0 B");
  });

  it("formats bytes in B, KB, MB, GB, and TB units", async () => {
    vi.mocked(getStats).mockResolvedValue(baseStats({ totalStorageBytes: 512 }));
    const first = await renderPage();
    expect(statCard("Storage Used").textContent).toContain("512 B");
    first.unmount();

    const cases: [number, string][] = [
      [1024 * 1024, "1.0 MB"],
      [5 * 1024 * 1024 * 1024, "5.0 GB"],
      [2 * 1024 ** 4, "2.0 TB"],
    ];
    for (const [bytes, expected] of cases) {
      vi.mocked(getStats).mockResolvedValue(baseStats({ totalStorageBytes: bytes }));
      const { unmount } = await renderPage();
      expect(statCard("Storage Used").textContent).toContain(expected);
      unmount();
    }
  });

  it("shows the sync health breakdown with counts", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({ syncHealth: { syncedVersions: 5, unsyncedVersions: 2, failedVersions: 1 } }),
    );

    await renderPage();

    expect(screen.getByRole("img", { name: "5 synced, 2 unsynced, 1 failed" })).toBeTruthy();
    expect(screen.getByText("Unsynced").nextElementSibling?.textContent).toBe("2");
    expect(screen.getByText("Failed").nextElementSibling?.textContent).toBe("1");
  });

  it("shows an empty sync health state when there are no versions", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({ syncHealth: { syncedVersions: 0, unsyncedVersions: 0, failedVersions: 0 } }),
    );

    await renderPage();

    expect(screen.getByText("No versions found.")).toBeTruthy();
    expect(screen.queryByRole("img", { name: /synced/ })).toBeNull();
  });

  it("lists security findings as chips and counts affected resources", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        securityPosture: { critical: 2, high: 0, medium: 1, low: 0, unknown: 3, exempted: 4, totalAffectedResources: 1 },
      }),
    );

    await renderPage();

    expect(screen.getByText("Critical: 2")).toBeTruthy();
    expect(screen.getByText("Medium: 1")).toBeTruthy();
    expect(screen.getByText("Unknown: 3")).toBeTruthy();
    expect(screen.queryByText("High: 0")).toBeNull();
    expect(screen.queryByText("Low: 0")).toBeNull();
    expect(screen.getByText("Exempted: 4")).toBeTruthy();
    expect(screen.getByText("1 resource with findings")).toBeTruthy();
  });

  it("pluralises affected resources and omits the exempted chip when none are exempt", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        securityPosture: { critical: 0, high: 1, medium: 0, low: 0, unknown: 0, exempted: 0, totalAffectedResources: 3 },
      }),
    );

    await renderPage();

    expect(screen.getByText("High: 1")).toBeTruthy();
    expect(screen.getByText("3 resources with findings")).toBeTruthy();
    expect(screen.queryByText(/Exempted:/)).toBeNull();
  });

  it("shows an empty security state when there are no findings and no exemptions", async () => {
    await renderPage();

    expect(screen.getByText("No scan findings.")).toBeTruthy();
  });

  it("shows the Jev empty state when no versions have been assessed", async () => {
    await renderPage();

    expect(screen.getByText(/No Versions have been assessed by Jev/)).toBeTruthy();
  });

  it("shows Jev metrics, cost estimate, and blocked and failed highlights", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        jev: {
          assessedVersions: 1200,
          blockedVersions: 3,
          needsReviewVersions: 0,
          exemptedVersions: 7,
          failedVersions: 2,
          assessedChunks: 45,
          inputTokens: 1_000_000,
          riskLevels: { minimal: 0, low: 0, moderate: 0, high: 0, critical: 0 },
        },
      }),
    );

    await renderPage();

    expect(screen.getByText("1,200")).toBeTruthy();
    expect(screen.getByText(formatJevInputCost(1_000_000))).toBeTruthy();
    expect(screen.getByText("1,000,000")).toBeTruthy();
    expect(screen.getByText("45")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    expect(screen.getByText("No scored assessments.")).toBeTruthy();
    expect(screen.getByText("Blocked").nextElementSibling).toBeTruthy();
  });

  it("renders the risk level distribution with an accessible summary", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        jev: {
          assessedVersions: 10,
          blockedVersions: 0,
          needsReviewVersions: 2,
          exemptedVersions: 0,
          failedVersions: 0,
          assessedChunks: 10,
          inputTokens: 0,
          riskLevels: { minimal: 4, low: 0, moderate: 3, high: 1, critical: 2 },
        },
      }),
    );

    await renderPage();

    expect(screen.getByRole("img", { name: "4 minimal, 0 low, 3 moderate, 1 high, 2 critical" })).toBeTruthy();
    const legend = screen.getByText("Moderate").parentElement;
    expect(within(legend as HTMLElement).getByText("3")).toBeTruthy();
    expect(screen.queryByText("No scored assessments.")).toBeNull();
  });

  it("opens the needs-review queue link only when Security Policies is enabled", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        jev: {
          assessedVersions: 4,
          blockedVersions: 0,
          needsReviewVersions: 2,
          exemptedVersions: 0,
          failedVersions: 0,
          assessedChunks: 4,
          inputTokens: 0,
          riskLevels: { minimal: 0, low: 0, moderate: 0, high: 0, critical: 0 },
        },
      }),
    );

    await renderPage();

    expect(screen.getByTestId("needs-review-link")).toBeTruthy();
    expect(vi.mocked(OpenNeedsReviewButton)).toHaveBeenCalled();
    expect(screen.getByText("Open queue")).toBeTruthy();
  });

  it("renders plain needs-review metric and hides the queue link when Security Policies is disabled", async () => {
    vi.mocked(getScanPolicyCatalog).mockResolvedValue({ writesEnabled: false } as ScanPolicyCatalog);
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        jev: {
          assessedVersions: 4,
          blockedVersions: 0,
          needsReviewVersions: 2,
          exemptedVersions: 0,
          failedVersions: 0,
          assessedChunks: 4,
          inputTokens: 0,
          riskLevels: { minimal: 0, low: 0, moderate: 0, high: 0, critical: 0 },
        },
      }),
    );

    await renderPage();

    expect(screen.queryByTestId("needs-review-link")).toBeNull();
    expect(screen.queryByText("Open queue")).toBeNull();
    expect(screen.getByText("Needs review")).toBeTruthy();
  });

  it("hides the queue link when the policy catalog lookup fails", async () => {
    vi.mocked(getScanPolicyCatalog).mockRejectedValue(new Error("forbidden"));
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        jev: {
          assessedVersions: 4,
          blockedVersions: 0,
          needsReviewVersions: 2,
          exemptedVersions: 0,
          failedVersions: 0,
          assessedChunks: 4,
          inputTokens: 0,
          riskLevels: { minimal: 0, low: 0, moderate: 0, high: 0, critical: 0 },
        },
      }),
    );

    await renderPage();

    expect(screen.queryByTestId("needs-review-link")).toBeNull();
    expect(screen.getByText("Needs review")).toBeTruthy();
  });

  it("skips the policy catalog lookup when there is no session token", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue(undefined);

    await renderPage();

    expect(getScanPolicyCatalog).not.toHaveBeenCalled();
    expect(getStats).toHaveBeenCalledWith(undefined, undefined);
  });

  it("lists storage backends sorted by count with friendly labels", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        storageDistribution: [
          { backend: "filesystem", count: 2 },
          { backend: "S3", count: 7 },
          { backend: "azurestorage", count: 4 },
          { backend: "gcs", count: 1 },
          { backend: "other", count: 0 },
        ],
      }),
    );

    await renderPage();

    expect(screen.getByText("Storage Distribution")).toBeTruthy();
    const rendered = screen.getAllByText(/Amazon S3|Azure Storage|File System|Google Cloud Storage/).map((node) => node.textContent);
    expect(rendered).toEqual(["Amazon S3", "Azure Storage", "File System", "Google Cloud Storage", "File System"]);
    expect(screen.getByText("Amazon S3").nextElementSibling?.textContent).toBe("7");
  });

  it("hides the storage distribution section when there are no backends", async () => {
    await renderPage();

    expect(screen.queryByText("Storage Distribution")).toBeNull();
  });

  it("renders top downloads per kind with formatted counts and last-downloaded dates", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        mostDownloaded: {
          modules: [
            popular({ name: "vpc", version: "2.0.0", downloadCount: 1200, lastDownloadedAt: "2026-01-02T03:04:05Z" }),
            popular({ name: "eks", namespace: "platform", version: "1.1.0", downloadCount: 3 }),
          ],
          providers: [],
          skills: [popular({ kind: "skill", name: "review", version: "0.3.0", downloadCount: 9 })],
          agents: [],
        },
      }),
    );

    await renderPage();

    expect(screen.getByText("Top 10 Most Downloaded")).toBeTruthy();
    expect(screen.getAllByText("2.0.0").length).toBeGreaterThan(0);
    expect(screen.getAllByText("1,200").length).toBeGreaterThan(0);
    expect(screen.getAllByText(new Date("2026-01-02T03:04:05Z").toLocaleString()).length).toBeGreaterThan(0);
    expect(screen.getAllByText("platform").length).toBeGreaterThan(0);
    expect(screen.getAllByText("review").length).toBeGreaterThan(0);
    expect(screen.getAllByText("0.3.0").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No download events recorded yet.")).toHaveLength(2);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("renders mobile record cards for each top download", async () => {
    vi.mocked(getStats).mockResolvedValue(
      baseStats({
        mostDownloaded: {
          modules: [popular({ name: "vpc", downloadCount: 42 })],
          providers: [],
          skills: [],
          agents: [],
        },
      }),
    );

    await renderPage();

    expect(screen.getAllByText("Last Downloaded").length).toBeGreaterThan(0);
    expect(screen.getAllByText("42").length).toBeGreaterThan(0);
  });

  it("shows the server error and skips stats when the fetch fails", async () => {
    vi.mocked(getStats).mockRejectedValue(new Error("upstream unavailable"));

    await renderPage();

    expect(screen.getByRole("alert").textContent).toContain("upstream unavailable");
    expect(screen.queryByText("Sync Health")).toBeNull();
    expect(redirect).not.toHaveBeenCalled();
  });

  it("falls back to a generic message when the failure is not an Error", async () => {
    vi.mocked(getDepotsGraph).mockRejectedValue("boom");

    await renderPage();

    expect(screen.getByRole("alert").textContent).toContain("Failed to load stats.");
  });

  it.each([
    ["401 Unauthorized", new Error("API returned 401")],
    ["unauthorized", new Error("unauthorized access")],
  ])("redirects to login when the error reports %s", async (_label, error) => {
    vi.mocked(getStats).mockRejectedValue(error);

    await expect(StatsPage()).rejects.toThrow("NEXT_REDIRECT");
    expect(redirect).toHaveBeenCalledWith("/login");
  });
});
