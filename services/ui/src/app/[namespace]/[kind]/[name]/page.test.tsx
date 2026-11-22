// @vitest-environment jsdom

import * as React from "react";
import { ThemeProvider } from "@mui/material/styles";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ResourceDetailPage from "./page";
import { AgentConfigSnippet, AgentDefinition, AgentMetadataChips } from "@/components/AgentDetail";
import ContractTable from "@/components/ContractTable";
import DrillDownWarningBridge from "@/components/DrillDownWarningBridge";
import JevPanel from "@/components/JevPanel";
import ProviderLogo from "@/components/ProviderLogo";
import ResourceReadme from "@/components/ResourceReadme";
import RiskMeter from "@/components/RiskMeter";
import ScanDrillDown from "@/components/ScanDrillDown";
import SeverityBadge from "@/components/SeverityBadge";
import SourceRefChip from "@/components/SourceRefChip";
import UsageSnippet from "@/components/UsageSnippet";
import theme from "@/theme";
import {
  getContract,
  getResourceDetail,
  getScanPolicyCapabilities,
  getScanPolicyCatalog,
  listDepots,
} from "@/lib/api";
import type {
  BrowseContract,
  BrowseDepotList,
  BrowseResourceDetail,
  BrowseVersionSummary,
  ScanPolicyCapabilities,
  ScanPolicyCatalog,
} from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { notFound } from "next/navigation";

vi.mock("@/components/SeverityBadge", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/ScanDrillDown", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/ProviderLogo", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/MobileSidebarButton", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/DrillDownWarningBridge", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/UsageSnippet", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/ResourceReadme", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/ContractTable", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/JevPanel", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/AgentDetail", () => ({
  AgentConfigSnippet: vi.fn(() => null),
  AgentDefinition: vi.fn(() => null),
  AgentMetadataChips: vi.fn(() => null),
}));
vi.mock("@/components/SourceRefChip", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/RiskMeter", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/DepotKindIcon", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/components/DepotKindIcon")>();
  return { DepotKindIcon: () => null, isDepotKind: actual.isDepotKind };
});
vi.mock("@/lib/api", () => ({
  getContract: vi.fn(),
  getResourceDetail: vi.fn(),
  getScanPolicyCapabilities: vi.fn(),
  getScanPolicyCatalog: vi.fn(),
  listDepots: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  notFound: vi.fn((): never => {
    throw new Error("NEXT_NOT_FOUND");
  }),
}));

const writableCatalog: ScanPolicyCatalog = {
  writesEnabled: true,
  items: [
    {
      namespace: "acme",
      canRead: true,
      canWrite: true,
      canManageNamespaceWidePolicies: true,
      modules: [],
      providers: [],
      skills: [],
      agents: [],
    },
  ],
};

const writableCapabilities: ScanPolicyCapabilities = {
  writesEnabled: true,
  canRead: true,
  canWrite: true,
  canManageNamespaceWidePolicies: true,
  methods: [],
  supportsPreview: true,
};

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
  };
}

function baseDetail(overrides: Partial<BrowseResourceDetail> = {}): BrowseResourceDetail {
  return {
    kind: "module",
    namespace: "acme",
    name: "vpc",
    latestVersion: "1.2.0",
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
  };
}

const contractFixture: BrowseContract = {
  schemaVersion: "1",
  module: { namespace: "acme", name: "vpc", version: "1.2.0" },
  compatibility: { grade: "full" },
  provenance: { derivedAt: "2026-01-01T00:00:00Z" },
};

async function renderPage(kind: string, name = "vpc") {
  const element = await ResourceDetailPage({ params: Promise.resolve({ namespace: "acme", kind, name }) });
  return render(<ThemeProvider theme={theme}>{element}</ThemeProvider>);
}

describe("ResourceDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.unstubAllEnvs();
    vi.mocked(getServerSessionToken).mockResolvedValue("token-123");
    vi.mocked(getResourceDetail).mockResolvedValue(baseDetail());
    vi.mocked(getContract).mockResolvedValue(null);
    vi.mocked(listDepots).mockResolvedValue({ items: [] } satisfies BrowseDepotList);
    vi.mocked(getScanPolicyCatalog).mockResolvedValue(writableCatalog);
    vi.mocked(getScanPolicyCapabilities).mockResolvedValue(writableCapabilities);
  });

  describe("error handling", () => {
    it("calls notFound when the detail request reports a 404", async () => {
      vi.mocked(getResourceDetail).mockRejectedValue(new Error("404 not found"));

      await expect(
        ResourceDetailPage({ params: Promise.resolve({ namespace: "acme", kind: "module", name: "missing" }) }),
      ).rejects.toThrow("NEXT_NOT_FOUND");
      expect(notFound).toHaveBeenCalled();
    });

    it("shows an error alert when the detail request fails with another error", async () => {
      vi.mocked(getResourceDetail).mockRejectedValue(new Error("upstream exploded"));

      await renderPage("module");

      expect(screen.getByRole("alert").textContent).toBe("Failed to load resource detail: upstream exploded");
      expect(notFound).not.toHaveBeenCalled();
    });

    it("shows an error alert with an empty message for non-Error failures", async () => {
      vi.mocked(getResourceDetail).mockRejectedValue("plain failure");

      await renderPage("module");

      expect(screen.getByRole("alert").textContent).toContain("Failed to load resource detail");
    });
  });

  describe("data loading and header", () => {
    it("requests the detail with the session token and renders breadcrumbs for the kind", async () => {
      await renderPage("module");

      expect(getResourceDetail).toHaveBeenCalledWith("acme", "module", "vpc", "token-123");
      expect(screen.getByRole("link", { name: "Registry" }).getAttribute("href")).toBe("/");
      expect(screen.getByRole("link", { name: "acme" }).getAttribute("href")).toBe("/?namespace=acme");
      expect(screen.getByRole("link", { name: "Modules" }).getAttribute("href")).toBe("/?kind=module");
    });

    it("passes the resource and header props to the header children", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          scanCounts: { critical: 1, high: 0, medium: 0, low: 0, unknown: 0, exempted: 0 },
          sourceRef: "main",
          sourceCommit: "abc1234",
          sourcePath: "modules/vpc",
        }),
      );

      await renderPage("module");

      expect(vi.mocked(ProviderLogo).mock.lastCall?.[0]).toMatchObject({ provider: "aws", size: 38 });
      expect(vi.mocked(SourceRefChip).mock.lastCall?.[0]).toMatchObject({
        sourceRef: "main",
        sourceCommit: "abc1234",
        sourcePath: "modules/vpc",
        repoUrl: "",
        plain: true,
      });
      expect(vi.mocked(SeverityBadge).mock.lastCall?.[0]).toMatchObject({
        counts: { critical: 1, high: 0, medium: 0, low: 0, unknown: 0, exempted: 0 },
      });
    });

    it("links to the source repository when a repo URL is present", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ repoUrl: "https://github.com/acme/vpc" }));

      await renderPage("module");

      expect(screen.getByRole("link", { name: "Open source repository" }).getAttribute("href")).toBe(
        "https://github.com/acme/vpc",
      );
    });

    it("omits the repository link when no repo URL or source repository is set", async () => {
      await renderPage("module");

      expect(screen.queryByRole("link", { name: "Open source repository" })).toBeNull();
    });

    it("uses the source repository when no repo URL is set", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({ repoUrl: "", sourceRepository: "https://github.com/acme/other" }),
      );

      await renderPage("module");

      expect(screen.getByRole("link", { name: "Open source repository" }).getAttribute("href")).toBe(
        "https://github.com/acme/other",
      );
    });
  });

  describe("module and provider variants", () => {
    it("builds the usage snippet from the registry base URL and host", async () => {
      vi.stubEnv("NEXT_PUBLIC_BASE_URL", "https://registry.example.com:8443");

      await renderPage("module");

      expect(vi.mocked(UsageSnippet).mock.lastCall?.[0]).toMatchObject({
        kind: "module",
        namespace: "acme",
        name: "vpc",
        provider: "aws",
        latestVersion: "1.2.0",
        registryHost: "registry.example.com:8443",
        registryBaseUrl: "https://registry.example.com:8443",
      });
    });

    it("falls back to placeholder registry values when no base URL is configured", async () => {
      vi.stubEnv("NEXT_PUBLIC_BASE_URL", "");

      await renderPage("module");

      expect(vi.mocked(UsageSnippet).mock.lastCall?.[0]).toMatchObject({
        registryHost: "your-opendepot-host",
        registryBaseUrl: "https://your-opendepot-host",
      });
    });

    it("renders the README for a module with readme content", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ readmeContent: "# VPC" }));

      await renderPage("module");

      expect(vi.mocked(ResourceReadme).mock.lastCall?.[0]).toMatchObject({
        content: "# VPC",
        kind: "module",
        namespace: "acme",
        name: "vpc",
        provider: "aws",
        registryHost: "your-opendepot-host",
      });
    });

    it("loads and renders the contract for a module", async () => {
      vi.mocked(getContract).mockResolvedValue(contractFixture);

      await renderPage("module");

      expect(getContract).toHaveBeenCalledWith("acme", "module", "vpc", undefined, "token-123");
      expect(vi.mocked(ContractTable).mock.lastCall?.[0]).toMatchObject({ contract: contractFixture });
    });

    it("hides the contract section when the contract request fails", async () => {
      vi.mocked(getContract).mockRejectedValue(new Error("no contract"));

      await renderPage("module");

      expect(ContractTable).not.toHaveBeenCalled();
    });

    it("shows default provider namespace and registry values for a provider", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ kind: "provider", name: "aws" }));

      await renderPage("provider", "aws");

      expect(getContract).not.toHaveBeenCalled();
      expect(screen.getByText("hashicorp")).toBeTruthy();
      expect(screen.getByText("registry.opentofu.org")).toBeTruthy();
      expect(vi.mocked(UsageSnippet).mock.lastCall?.[0]).toMatchObject({ kind: "provider" });
      expect(vi.mocked(ProviderLogo).mock.lastCall?.[0]).toMatchObject({ provider: "aws" });
    });

    it("shows provider namespace and upstream registry when they are set", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({ kind: "provider", name: "aws", providerNamespace: "acme-ns", upstreamRegistry: "registry.example.com" }),
      );

      await renderPage("provider", "aws");

      expect(screen.getByText("acme-ns")).toBeTruthy();
      expect(screen.getByText("registry.example.com")).toBeTruthy();
    });

    it("shows version history, constraints, owner, and GitHub auth values in the overview", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          repoOwner: "acme-owner",
          versionHistoryLimit: 5,
          versionConstraints: "~> 1.2",
          githubConfig: { useAuthenticatedClient: true },
        }),
      );

      await renderPage("module");

      expect(screen.getByText("acme-owner")).toBeTruthy();
      expect(screen.getByText("5")).toBeTruthy();
      expect(screen.getByText("~> 1.2")).toBeTruthy();
      expect(screen.getByText("Yes")).toBeTruthy();
    });

    it("renders each populated storage field and presign values", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          storageConfig: {
            backend: "s3",
            bucket: "modules-bucket",
            region: "us-east-1",
            key: "vpc/",
            directoryPath: "/data",
            accountName: "acct",
            accountUrl: "https://acct.example.com",
            subscriptionID: "sub-1",
            resourceGroup: "rg-1",
            presignEnabled: true,
            presignTTL: "15m",
          },
        }),
      );

      await renderPage("module");

      expect(screen.getByText("modules-bucket")).toBeTruthy();
      expect(screen.getByText("us-east-1")).toBeTruthy();
      expect(screen.getByText("vpc/")).toBeTruthy();
      expect(screen.getByText("/data")).toBeTruthy();
      expect(screen.getByText("acct")).toBeTruthy();
      expect(screen.getByText("https://acct.example.com")).toBeTruthy();
      expect(screen.getByText("sub-1")).toBeTruthy();
      expect(screen.getByText("rg-1")).toBeTruthy();
      expect(screen.getByText("15m")).toBeTruthy();
      expect(screen.getByText("Yes")).toBeTruthy();
      expect(screen.queryByText("Inherited From")).toBeNull();
    });
  });

  describe("depot association and storage inheritance", () => {
    it("inherits the backend and depot name from the managing depot when no storage is configured", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ depotRef: { namespace: "acme", name: "main" } }));
      vi.mocked(listDepots).mockResolvedValue({
        items: [{ namespace: "acme", name: "main", modules: ["vpc"], providers: [], storageBackend: "s3" }],
      });

      await renderPage("module");

      expect(listDepots).toHaveBeenCalledWith("token-123");
      expect(screen.getByText("s3")).toBeTruthy();
      expect(screen.getByText("Inherited From")).toBeTruthy();
      expect(screen.getAllByText("acme / main").length).toBeGreaterThan(0);
    });

    it("falls back to not configured when the depot list fails", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ depotRef: { namespace: "acme", name: "main" } }));
      vi.mocked(listDepots).mockRejectedValue(new Error("depots unavailable"));

      await renderPage("module");

      expect(screen.getByText("Not configured")).toBeTruthy();
      expect(screen.queryByText("Inherited From")).toBeNull();
    });

    it("does not show inheritance when the resource has its own storage backend", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          depotRef: { namespace: "acme", name: "main" },
          storageConfig: { backend: "gcs", bucket: "own-bucket" },
        }),
      );
      vi.mocked(listDepots).mockResolvedValue({
        items: [{ namespace: "acme", name: "main", modules: [], providers: [], storageBackend: "s3" }],
      });

      await renderPage("module");

      expect(screen.getByText("gcs")).toBeTruthy();
      expect(screen.queryByText("Inherited From")).toBeNull();
    });

    it("shows the depot chip for a depot reference", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ depotRef: { namespace: "acme", name: "main" } }));

      await renderPage("module");

      expect(screen.getByText("acme / main")).toBeTruthy();
      expect(listDepots).toHaveBeenCalledTimes(1);
    });

    it("skips the depot lookup when there is no depot reference", async () => {
      await renderPage("module");

      expect(listDepots).not.toHaveBeenCalled();
      expect(screen.queryByText("Depot")).toBeNull();
    });
  });

  describe("agent and skill variants", () => {
    const agentDetail = baseDetail({
      kind: "skill",
      name: "review",
      latestVersion: "0.1.0",
      provider: "",
      definitionContent: "# Review",
      definitionFileName: "SKILL.md",
      agentMetadata: { model: "gpt-x", tools: ["grep"] },
    });

    it("renders agent-specific sections and skips contract, usage, and readme", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(agentDetail);

      await renderPage("skill", "review");

      expect(getContract).not.toHaveBeenCalled();
      expect(UsageSnippet).not.toHaveBeenCalled();
      expect(ResourceReadme).not.toHaveBeenCalled();
      expect(vi.mocked(AgentConfigSnippet).mock.lastCall?.[0]).toMatchObject({
        kind: "skill",
        namespace: "acme",
        name: "review",
        registryHost: "your-opendepot-host",
        latestVersion: "0.1.0",
      });
      expect(vi.mocked(AgentDefinition).mock.lastCall?.[0]).toMatchObject({
        content: "# Review",
        fileName: "SKILL.md",
        title: "SKILL.md",
      });
      expect(vi.mocked(AgentMetadataChips).mock.lastCall?.[0]).toMatchObject({
        metadata: { model: "gpt-x", tools: ["grep"] },
      });
    });

    it("titles the definition AGENT.md for agent kinds", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({ kind: "agent", name: "triage", definitionContent: "# Triage", definitionFileName: "" }),
      );

      await renderPage("agent", "triage");

      expect(vi.mocked(AgentDefinition).mock.lastCall?.[0]).toMatchObject({
        title: "AGENT.md",
        fileName: "",
      });
    });

    it("omits agent metadata when neither a model nor tools are set", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ kind: "agent", name: "triage", agentMetadata: { tools: [] } }));

      await renderPage("agent", "triage");

      expect(AgentMetadataChips).not.toHaveBeenCalled();
      expect(AgentDefinition).not.toHaveBeenCalled();
    });

    it("shows the blocked alert with reasons from the detail when Jev blocks the agent", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          blocked: true,
          blockReasons: ["policy violation", "secret detected"],
          jevAssessment: {
            evaluatedAt: "2026-01-01T00:00:00Z",
            needsReview: false,
            blocked: true,
          },
        }),
      );

      await renderPage("skill", "review");

      const alert = screen.getByRole("alert");
      expect(alert.textContent).toContain("Blocked by Jev. The registry does not serve this version.");
      expect(alert.textContent).toContain("Reason: policy violation; secret detected.");
      expect(screen.getByRole("link", { name: "View assessment" }).getAttribute("href")).toBe("#jev-assessment");
    });

    it("falls back to the assessment block reasons when the detail has none", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: {
            evaluatedAt: "2026-01-01T00:00:00Z",
            needsReview: true,
            blocked: true,
            blockReasons: ["injection risk"],
          },
        }),
      );

      await renderPage("skill", "review");

      expect(screen.getByRole("alert").textContent).toContain("Reason: injection risk.");
    });

    it("does not show a block alert when Jev has not blocked the agent", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: { evaluatedAt: "2026-01-01T00:00:00Z", needsReview: false, blocked: false },
        }),
      );

      await renderPage("skill", "review");

      expect(screen.queryByRole("alert")).toBeNull();
      expect(getScanPolicyCatalog).not.toHaveBeenCalled();
    });

    it("passes the risk level to the risk meter and the assessment to the JEV panel", async () => {
      const jevAssessment = {
        evaluatedAt: "2026-01-01T00:00:00Z",
        needsReview: true,
        blocked: false,
        riskLevel: "Moderate" as const,
      };
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "agent",
          name: "triage",
          jevAssessment,
          jevThresholds: { minConfidence: 0.5 },
          jevUnblock: {
            state: "none",
            governingPriority: 0,
            matchingPriority: 0,
            namesHidden: false,
          },
        }),
      );

      await renderPage("agent", "triage");

      expect(vi.mocked(RiskMeter).mock.lastCall?.[0]).toMatchObject({ level: "Moderate" });
      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({
        assessment: jevAssessment,
        thresholds: { minConfidence: 0.5 },
        unblock: null,
        namesHidden: false,
        exemption: {
          namespace: "acme",
          targetKind: "Agent",
          targetName: "triage",
          canEdit: false,
          governingPolicyHidden: false,
        },
      });
    });

    it("lets the JEV panel edit exemptions when the caller can write them", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          latestVersion: "0.1.0",
          jevAssessment: {
            evaluatedAt: "2026-01-01T00:00:00Z",
            needsReview: false,
            blocked: true,
            contentDigest: "sha256:abc",
          },
        }),
      );

      await renderPage("skill", "review");

      expect(getScanPolicyCatalog).toHaveBeenCalledWith("token-123");
      expect(getScanPolicyCapabilities).toHaveBeenCalledWith("acme", "token-123");
      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({
        exemption: { namespace: "acme", versionName: "0.1.0", targetKind: "Skill", targetName: "review", canEdit: true },
      });
    });

    it("denies exemption editing when the policy lookup fails", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: {
            evaluatedAt: "2026-01-01T00:00:00Z",
            needsReview: false,
            blocked: true,
            contentDigest: "sha256:abc",
          },
        }),
      );
      vi.mocked(getScanPolicyCatalog).mockRejectedValue(new Error("forbidden"));

      await renderPage("skill", "review");

      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({ exemption: { canEdit: false } });
    });

    it("denies exemption editing when the caller lacks namespace-wide policy rights", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: {
            evaluatedAt: "2026-01-01T00:00:00Z",
            needsReview: false,
            blocked: true,
            contentDigest: "sha256:abc",
          },
        }),
      );
      vi.mocked(getScanPolicyCapabilities).mockResolvedValue({ ...writableCapabilities, canManageNamespaceWidePolicies: false });

      await renderPage("skill", "review");

      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({ exemption: { canEdit: false } });
    });

    it("passes the unblock view and hides governing policy names when names are hidden", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: { evaluatedAt: "2026-01-01T00:00:00Z", needsReview: false, blocked: true },
          jevUnblock: {
            state: "needs-entry",
            digest: "sha256:abc",
            governingPolicy: "secret-policy",
            governingPriority: 1,
            matchingPriority: 0,
            namesHidden: true,
          },
        }),
      );

      await renderPage("skill", "review");

      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({
        unblock: { state: "needs-entry", digest: "sha256:abc", namesHidden: true },
        namesHidden: true,
        exemption: { governingPolicyHidden: true },
      });
    });

    it("omits the JEV unblock view for a skill whose assessment has no unblock state", async () => {
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({
          kind: "skill",
          name: "review",
          jevAssessment: { evaluatedAt: "2026-01-01T00:00:00Z", needsReview: false, blocked: false },
          jevUnblock: { state: "none", governingPriority: 0, matchingPriority: 0, namesHidden: false },
        }),
      );

      await renderPage("skill", "review");

      expect(vi.mocked(JevPanel).mock.lastCall?.[0]).toMatchObject({ unblock: null });
    });
  });

  describe("versions and drill-down", () => {
    it("sorts versions newest first using semantic and non-semantic rules", async () => {
      const versionNames = ["1.0.0", "v2.0.0", "1.10.0", "not-a-version", "1.2.0", "1.0", "alpha-build", "beta-build"];
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({ versions: versionNames.map((name) => version({ version: name })) }),
      );

      await renderPage("module");

      expect(vi.mocked(ScanDrillDown).mock.lastCall?.[0]?.versions.map((entry) => entry.version)).toEqual([
        "v2.0.0",
        "1.10.0",
        "1.2.0",
        "1.0.0",
        "1.0",
        "not-a-version",
        "beta-build",
        "alpha-build",
      ]);
    });

    it("passes scan findings and drill-down identity to the scan drill-down", async () => {
      const findings = [{ id: "CVE-1" }] as unknown as BrowseResourceDetail["sourceScanFindings"];
      vi.mocked(getResourceDetail).mockResolvedValue(
        baseDetail({ sourceScanFindings: findings, binaryScanFindings: { linux: findings } }),
      );

      await renderPage("module");

      expect(vi.mocked(ScanDrillDown).mock.lastCall?.[0]).toMatchObject({
        namespace: "acme",
        kind: "module",
        name: "vpc",
        initialSourceScanFindings: findings,
        initialBinaryScanFindings: { linux: findings },
      });
      expect(vi.mocked(DrillDownWarningBridge).mock.lastCall?.[0]).toMatchObject({
        namespace: "acme",
        kind: "module",
        name: "vpc",
      });
    });

    it.each([
      ["an unsynced version", [version({ synced: false, syncStatus: "Pending" })], true],
      ["a failed sync status", [version({ syncStatus: "Sync failed" })], true],
      ["only blocked unsynced versions", [version({ synced: false, blocked: true })], false],
      ["only synced versions", [version({ synced: true, syncStatus: "Synced" })], false],
    ])("reports unsynced versions for %s", async (_label, versions, expected) => {
      vi.mocked(getResourceDetail).mockResolvedValue(baseDetail({ versions }));

      await renderPage("module");

      expect(vi.mocked(DrillDownWarningBridge).mock.lastCall?.[0]).toMatchObject({ initialHasUnsynced: expected });
    });
  });
});
