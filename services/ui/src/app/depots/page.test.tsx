// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import * as React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DepotsPage from "./page";
import DepotsGraphClient from "@/components/DepotsGraphClient";
import { getDepotsGraph, getResourceDetail, listResources } from "@/lib/api";
import type { BrowseDepotGraph, BrowseResourceDetail, BrowseResourceList, BrowseVersionSummary } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";

vi.mock("@/components/DepotsGraphClient", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/PageHeader", () => ({
  default: ({ title, actions }: { title: string; actions?: React.ReactNode }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  ),
}));
vi.mock("@/components/RefreshIconButton", () => ({
  default: ({ ariaLabel }: { ariaLabel?: string }) => <button type="button" aria-label={ariaLabel} />,
}));
vi.mock("@/components/MobileMapButton", () => ({ default: () => <span>map toggle</span> }));
vi.mock("@/lib/api", () => ({
  getDepotsGraph: vi.fn(),
  getResourceDetail: vi.fn(),
  listResources: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn((): never => {
    throw new Error("NEXT_REDIRECT");
  }),
}));

function version(overrides: Partial<BrowseVersionSummary>): BrowseVersionSummary {
  return {
    version: "1.0.0",
    syncStatus: "Synced",
    os: "linux",
    arch: "amd64",
    lastScanned: "",
    synced: true,
    scanCounts: null,
    ...overrides,
  } as BrowseVersionSummary;
}

function detail(overrides: Partial<BrowseResourceDetail>): BrowseResourceDetail {
  return {
    kind: "module",
    namespace: "acme",
    name: "vpc",
    latestVersion: "",
    syncStatus: "Synced",
    synced: true,
    public: true,
    provider: "aws",
    repoUrl: "",
    providerNamespace: "",
    upstreamRegistry: "",
    platforms: [],
    scanCounts: null,
    lastScanned: "",
    versions: [],
    sourceScanFindings: [],
    binaryScanFindings: {},
    ...overrides,
  } as BrowseResourceDetail;
}

function resourceList(items: BrowseResourceList["items"] = []): BrowseResourceList {
  return { items, totalCount: items.length, page: 1, pageSize: 500 };
}

const graph: BrowseDepotGraph = {
  depots: [{ id: "depot-1", namespace: "acme", name: "main" }],
  modules: [
    { id: "module-vpc", namespace: "acme", name: "vpc", synced: false, latestVersion: "1.0.0" },
    { id: "module-eks", namespace: "acme", name: "eks", synced: false, latestVersion: "2.0.0" },
    { id: "module-orphan", namespace: "acme", name: "broken", synced: false, latestVersion: "3.0.0" },
  ],
  providers: [
    { id: "provider-aws", namespace: "acme", name: "aws", synced: false },
    { id: "provider-gcp", namespace: "acme", name: "gcp", synced: true },
    { id: "provider-azure", namespace: "acme", name: "azure", synced: true },
  ],
  skills: [{ id: "skill-review", kind: "skill", namespace: "acme", name: "review", synced: true, latestVersion: "0.1.0" }],
  agents: [{ id: "agent-triage", kind: "agent", namespace: "acme", name: "triage", synced: true, latestVersion: "0.2.0" }],
  edges: [],
  summary: { totalDepots: 1, totalModules: 3, totalProviders: 3, totalSkills: 1, totalAgents: 1 },
  generatedAt: "2026-01-01T00:00:00Z",
};

function lastClientProps() {
  return vi.mocked(DepotsGraphClient).mock.lastCall?.[0];
}

describe("DepotsPage data loading", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getServerSessionToken).mockResolvedValue("token-123");
    vi.mocked(getDepotsGraph).mockResolvedValue(graph);
    vi.mocked(listResources).mockResolvedValue(resourceList());
    vi.mocked(getResourceDetail).mockImplementation(async (namespace, kind, name) => {
      if (kind === "module" && name === "vpc") {
        return detail({
          kind,
          name,
          versions: [
            version({ version: "1.0.0", fileName: "vpc-1.0.0.tgz", checksum: "abc", lastScanned: "2026-01-01" }),
            version({ version: "1.0.0", fileName: "vpc-1.0.0.tgz", checksum: "abc", lastScanned: "2026-01-01" }),
            version({ version: "", fileName: "empty.tgz" }),
            version({ version: "0.9.0" }),
          ],
          storageConfig: { backend: "s3", bucket: "modules" },
          githubConfig: { useAuthenticatedClient: true },
        });
      }
      if (kind === "module" && name === "eks") {
        throw new Error("500 internal");
      }
      if (kind === "module" && name === "broken") {
        return detail({ kind, name, versions: [] });
      }
      if (kind === "provider" && name === "aws") {
        return detail({
          kind,
          name,
          versions: [
            version({ version: "3.0.0", name: "linux_amd64" }),
            version({ version: "3.0.0", name: "linux_amd64" }),
            version({ version: "3.0.0", name: "darwin_arm64" }),
            version({ version: "" }),
          ],
        });
      }
      if (kind === "provider" && name === "gcp") {
        return detail({ kind, name, versions: undefined as unknown as BrowseResourceDetail["versions"] });
      }
      if (kind === "provider" && name === "azure") {
        throw new Error("provider detail unavailable");
      }
      if (kind === "skill" && name === "review") {
        return detail({
          kind,
          name,
          versions: [version({ version: "0.1.0", commit: "abcdef1234", fileName: "review.md", checksum: "sum", lastScanned: "t" })],
        });
      }
      if (kind === "agent" && name === "triage") {
        throw new Error("agent detail unavailable");
      }
      return detail({ kind, name });
    });
  });

  it("requests the graph and both resource lists with the session token", async () => {
    render(await DepotsPage());

    expect(getDepotsGraph).toHaveBeenCalledWith(undefined, "token-123");
    expect(listResources).toHaveBeenCalledWith({ kind: "module", page: 1, pageSize: 500 }, "token-123");
    expect(listResources).toHaveBeenCalledWith({ kind: "provider", page: 1, pageSize: 500 }, "token-123");
  });

  it("builds deduplicated module versions, metadata, and detail per module", async () => {
    render(await DepotsPage());

    const props = lastClientProps();
    expect(props?.moduleVersionsByKey).toEqual({
      "acme/vpc": ["1.0.0", "0.9.0"],
      "acme/eks": ["2.0.0"],
      "acme/broken": [],
    });
    expect(props?.moduleVersionMetaByKey["acme/vpc"]).toEqual([
      { version: "1.0.0", fileName: "vpc-1.0.0.tgz", checksum: "abc", lastScanned: "2026-01-01" },
      { version: "0.9.0", fileName: undefined, checksum: undefined, lastScanned: "" },
    ]);
    expect(props?.moduleVersionMetaByKey["acme/eks"]).toEqual([{ version: "2.0.0" }]);
    expect(props?.moduleDetailByKey["acme/vpc"]).toEqual({
      storageConfig: { backend: "s3", bucket: "modules" },
      githubAuthenticated: true,
    });
    expect(props?.moduleDetailByKey["acme/eks"]).toEqual({});
  });

  it("builds provider versions keyed by version and name and tolerates missing version lists", async () => {
    render(await DepotsPage());

    const props = lastClientProps();
    expect(props?.providerVersionMetaByKey["acme/aws"]).toEqual([
      { version: "3.0.0", name: "linux_amd64", fileName: undefined, checksum: undefined, lastScanned: "" },
      { version: "3.0.0", name: "darwin_arm64", fileName: undefined, checksum: undefined, lastScanned: "" },
    ]);
    expect(props?.providerVersionsByKey["acme/aws"]).toEqual(["3.0.0", "3.0.0"]);
    expect(props?.providerVersionMetaByKey["acme/gcp"]).toEqual([]);
    expect(props?.providerVersionsByKey["acme/gcp"]).toEqual([]);
    expect(props?.providerVersionMetaByKey["acme/azure"]).toEqual([]);
    expect(props?.providerVersionsByKey["acme/azure"]).toEqual([]);
  });

  it("keys skill and agent versions by kind and omits resources whose detail failed", async () => {
    render(await DepotsPage());

    const props = lastClientProps();
    expect(props?.agentVersionMetaByKey).toEqual({
      "skill/acme/review": [
        { version: "0.1.0", commit: "abcdef1234", fileName: "review.md", checksum: "sum", lastScanned: "t" },
      ],
    });
  });

  it("falls back to the latest module version when the module detail fails", async () => {
    render(await DepotsPage());

    const props = lastClientProps();
    expect(props?.moduleVersionsByKey["acme/eks"]).toEqual(["2.0.0"]);
    expect(getResourceDetail).toHaveBeenCalledWith("acme", "module", "eks", "token-123");
  });

  it("returns no module versions when the module detail fails without a latest version", async () => {
    vi.mocked(getDepotsGraph).mockResolvedValue({
      ...graph,
      modules: [{ id: "module-nolatest", namespace: "acme", name: "nolatest", synced: false }],
    });
    vi.mocked(getResourceDetail).mockRejectedValue(new Error("unavailable"));

    render(await DepotsPage());

    expect(lastClientProps()?.moduleVersionsByKey).toEqual({ "acme/nolatest": [] });
    expect(lastClientProps()?.moduleVersionMetaByKey).toEqual({ "acme/nolatest": [] });
  });

  it("applies live sync state from the resource lists to modules and providers", async () => {
    vi.mocked(listResources).mockImplementation(async ({ kind }) => {
      if (kind === "module") {
        return resourceList([
          { kind: "module", namespace: "acme", name: "vpc", synced: true, syncStatus: "Synced" } as BrowseResourceList["items"][number],
        ]);
      }
      return resourceList([
        { kind: "provider", namespace: "acme", name: "aws", synced: true } as BrowseResourceList["items"][number],
      ]);
    });

    render(await DepotsPage());

    const props = lastClientProps();
    expect(props?.graph.modules.find((module) => module.name === "vpc")).toMatchObject({ synced: true, syncStatus: "Synced" });
    expect(props?.graph.modules.find((module) => module.name === "eks")).toMatchObject({ synced: false });
    expect(props?.graph.providers.find((provider) => provider.name === "aws")).toMatchObject({ synced: true });
    expect(props?.graph.providers.find((provider) => provider.name === "gcp")).toMatchObject({ synced: true });
  });

  it("keeps the unenriched graph and empty version maps when the resource lists fail", async () => {
    vi.mocked(listResources).mockRejectedValue(new Error("list unavailable"));

    render(await DepotsPage());

    const props = lastClientProps();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(props?.graph.modules.find((module) => module.name === "vpc")).toMatchObject({ synced: false });
    expect(props?.moduleVersionsByKey).toEqual({});
    expect(props?.providerVersionMetaByKey).toEqual({});
    expect(props?.agentVersionMetaByKey).toEqual({});
  });

  it("renders the page header and a refresh control for depots", async () => {
    render(await DepotsPage());

    expect(screen.getByRole("heading", { name: "Depots" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "refresh depots" })).toBeTruthy();
    expect(screen.getByText("map toggle")).toBeTruthy();
    expect(DepotsGraphClient).toHaveBeenCalled();
  });
});

describe("DepotsPage error handling", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getServerSessionToken).mockResolvedValue("token-123");
  });

  it("shows the graph error and skips the client when the graph request fails", async () => {
    vi.mocked(getDepotsGraph).mockRejectedValue(new Error("upstream exploded"));

    render(await DepotsPage());

    expect(screen.getByRole("alert").textContent).toBe("Failed to load depot graph: upstream exploded");
    expect(DepotsGraphClient).not.toHaveBeenCalled();
    expect(listResources).not.toHaveBeenCalled();
  });

  it("stringifies non-Error failures in the graph error", async () => {
    vi.mocked(getDepotsGraph).mockRejectedValue("plain failure");

    render(await DepotsPage());

    expect(screen.getByRole("alert").textContent).toBe("Failed to load depot graph: plain failure");
  });

  it.each([
    "401 Unauthorized",
    "request unauthorized",
  ])("redirects to login when the graph request reports %s", async (message) => {
    vi.mocked(getDepotsGraph).mockRejectedValue(new Error(message));

    await expect(DepotsPage()).rejects.toThrow("NEXT_REDIRECT");
    expect(redirect).toHaveBeenCalledWith("/login");
  });
});
