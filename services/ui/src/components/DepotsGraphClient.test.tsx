// @vitest-environment jsdom

import { ThemeProvider } from "@mui/material/styles";
import { fireEvent, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DepotGraph from "@/components/DepotGraph";
import DepotsGraphClient from "./DepotsGraphClient";
import theme from "@/theme";
import type { BrowseDepotGraph } from "@/lib/api";

vi.mock("@/components/DepotGraph", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/components/DepotGraph")>();
  return {
    default: vi.fn(() => null),
    agentVersionItems: actual.agentVersionItems,
  };
});
vi.mock("@/components/DepotKindIcon", () => ({ DepotKindIcon: () => null }));

const graph: BrowseDepotGraph = {
  depots: [
    {
      id: "depot-main",
      namespace: "acme",
      name: "main",
      managedModuleNames: ["vpc", "shared"],
      managedProviderNames: ["aws"],
      managedSkillNames: ["review"],
      managedAgentNames: ["triage"],
    },
    {
      id: "depot-edge",
      namespace: "acme",
      name: "edge",
      managedModuleNames: ["eks"],
    },
  ],
  modules: [
    { id: "module-vpc", namespace: "acme", name: "vpc", synced: true, latestVersion: "1.0.0" },
    { id: "module-eks", namespace: "acme", name: "eks", synced: true, latestVersion: "2.0.0" },
    { id: "module-shared", namespace: "acme", name: "shared", synced: true },
    { id: "module-orphan", namespace: "acme", name: "orphan", synced: false, latestVersion: "3.0.0" },
  ],
  providers: [
    { id: "provider-aws", namespace: "acme", name: "aws", synced: true },
    { id: "provider-gcp", namespace: "acme", name: "gcp", synced: true },
    { id: "provider-bare", namespace: "acme", name: "bare", synced: false },
  ],
  skills: [{ id: "skill-review", kind: "skill", namespace: "acme", name: "review", synced: true, latestVersion: "0.1.0" }],
  agents: [{ id: "agent-triage", kind: "agent", namespace: "acme", name: "triage", synced: true, latestVersion: "0.2.0" }],
  edges: [
    { id: "edge-1", source: "depot-main", target: "module-shared" },
    { id: "edge-2", source: "depot-main", target: "provider-aws" },
    { id: "edge-3", source: "depot-edge", target: "module-eks" },
  ],
  summary: {
    totalDepots: 2,
    totalModules: 4,
    totalProviders: 3,
    totalSkills: 1,
    totalAgents: 1,
  },
  generatedAt: "2026-01-01T00:00:00Z",
};

const versionProps = {
  moduleVersionsByKey: { "acme/shared": ["0.1.0", "0.1.0", "0.2.0"] },
  providerVersionsByKey: { "acme/gcp": ["4.0.0"] },
  providerVersionMetaByKey: {
    "acme/aws": [
      { version: "1.0.0", name: "linux_amd64" },
      { version: "1.0.0", name: "darwin_arm64" },
      { version: "1.0.0", name: "linux_amd64" },
    ],
  },
  moduleVersionMetaByKey: {
    "acme/vpc": [{ version: "1.0.0" }, { version: "1.1.0" }, { version: "" }],
  },
  moduleDetailByKey: {},
};

function renderClient(overrides: Partial<React.ComponentProps<typeof DepotsGraphClient>> = {}) {
  return render(
    <ThemeProvider theme={theme}>
      <DepotsGraphClient graph={graph} {...versionProps} {...overrides} />
    </ThemeProvider>,
  );
}

function lastGraphProps() {
  return vi.mocked(DepotGraph).mock.lastCall?.[0];
}

function selectDepot(label: string) {
  const input = screen.getByRole("combobox");
  fireEvent.keyDown(input, { key: "ArrowDown" });
  fireEvent.click(screen.getByRole("option", { name: label }));
}

describe("DepotsGraphClient", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows the full graph and summary counts when no depot is selected", () => {
    renderClient();

    expect(screen.getByText("2 depots")).toBeTruthy();
    expect(screen.getByText("4 modules")).toBeTruthy();
    expect(screen.getByText("3 providers")).toBeTruthy();
    expect(screen.getByText("1 skill")).toBeTruthy();
    expect(screen.getByText("1 agent")).toBeTruthy();
    expect(screen.getByText("11 versions")).toBeTruthy();
    expect(screen.queryByText("Clear")).toBeNull();
    expect(lastGraphProps()?.graph).toBe(graph);
  });

  it("passes version and detail maps through to the graph unchanged", () => {
    const moduleDetailByKey = { "acme/vpc": { githubAuthenticated: true } };
    renderClient({ moduleDetailByKey });

    const props = lastGraphProps();
    expect(props?.moduleVersionsByKey).toEqual(versionProps.moduleVersionsByKey);
    expect(props?.providerVersionsByKey).toEqual(versionProps.providerVersionsByKey);
    expect(props?.providerVersionMetaByKey).toEqual(versionProps.providerVersionMetaByKey);
    expect(props?.moduleVersionMetaByKey).toEqual(versionProps.moduleVersionMetaByKey);
    expect(props?.moduleDetailByKey).toBe(moduleDetailByKey);
    expect(props?.agentVersionMetaByKey).toEqual({});
  });

  it("filters the graph and summary to the selected depot", () => {
    renderClient();

    selectDepot("acme / main");

    expect(screen.getByText("1 depot")).toBeTruthy();
    expect(screen.getByText("2 modules")).toBeTruthy();
    expect(screen.getByText("1 provider")).toBeTruthy();
    expect(screen.getByText("1 skill")).toBeTruthy();
    expect(screen.getByText("1 agent")).toBeTruthy();
    expect(screen.getByText("8 versions")).toBeTruthy();
    expect(screen.getByText("Clear")).toBeTruthy();

    const filtered = lastGraphProps()?.graph;
    expect(filtered?.depots.map((depot) => depot.name)).toEqual(["main"]);
    expect(filtered?.modules.map((module) => module.name)).toEqual(["vpc", "shared"]);
    expect(filtered?.providers.map((provider) => provider.name)).toEqual(["aws"]);
    expect(filtered?.skills.map((skill) => skill.name)).toEqual(["review"]);
    expect(filtered?.agents.map((agent) => agent.name)).toEqual(["triage"]);
    expect(filtered?.edges.map((edge) => edge.id)).toEqual(["edge-1", "edge-2"]);
    expect(filtered?.summary).toEqual({
      totalDepots: 1,
      totalModules: 2,
      totalProviders: 1,
      totalSkills: 1,
      totalAgents: 1,
    });
    expect(filtered?.generatedAt).toBe(graph.generatedAt);
  });

  it("includes modules reached only through edges from a selected depot", () => {
    renderClient();

    selectDepot("acme / edge");

    expect(screen.getByText("1 depot")).toBeTruthy();
    expect(screen.getByText("1 module")).toBeTruthy();
    expect(screen.getByText("0 providers")).toBeTruthy();
    expect(screen.getByText("0 skills")).toBeTruthy();
    expect(screen.getByText("0 agents")).toBeTruthy();
    expect(screen.getByText("1 version")).toBeTruthy();

    const filtered = lastGraphProps()?.graph;
    expect(filtered?.modules.map((module) => module.name)).toEqual(["eks"]);
    expect(filtered?.edges.map((edge) => edge.id)).toEqual(["edge-3"]);
  });

  it("unions the graphs of multiple selected depots", () => {
    renderClient();

    selectDepot("acme / main");
    selectDepot("acme / edge");

    expect(screen.getByText("2 depots")).toBeTruthy();
    expect(screen.getByText("3 modules")).toBeTruthy();
    expect(screen.getByText("1 provider")).toBeTruthy();
    expect(lastGraphProps()?.graph.modules.map((module) => module.name)).toEqual(["vpc", "eks", "shared"]);
  });

  it("clears the selection from the Clear chip", () => {
    renderClient();

    selectDepot("acme / main");
    expect(screen.getByText("1 depot")).toBeTruthy();

    fireEvent.click(screen.getByText("Clear"));

    expect(screen.getByText("2 depots")).toBeTruthy();
    expect(screen.getByText("11 versions")).toBeTruthy();
    expect(screen.queryByText("Clear")).toBeNull();
    expect(lastGraphProps()?.graph).toBe(graph);
  });

  it("counts agent versions from metadata when provided", () => {
    renderClient({
      agentVersionMetaByKey: {
        "skill/acme/review": [{ version: "0.1.0" }, { version: "0.2.0" }, { version: "0.3.0" }],
      },
    });

    expect(screen.getByText("13 versions")).toBeTruthy();
    expect(lastGraphProps()?.agentVersionMetaByKey).toEqual({
      "skill/acme/review": [{ version: "0.1.0" }, { version: "0.2.0" }, { version: "0.3.0" }],
    });
  });

  it("falls back to provider version lists when no provider metadata exists", () => {
    renderClient({ providerVersionMetaByKey: {} });

    expect(screen.getByText("9 versions")).toBeTruthy();
  });
});
