// @vitest-environment jsdom

import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DepotGraph from "./DepotGraph";
import { TOGGLE_NODE_MAP_EVENT } from "./MobileMapButton";
import type { BrowseDepotGraph } from "@/lib/api";
import theme from "@/theme";

const flowState = vi.hoisted(() => ({
  initialized: false,
  width: 0,
  height: 0,
  nodes: [] as Array<{ id: string; position: { x: number; y: number }; width: number; height: number }>,
  setViewport: vi.fn(),
}));

// React Flow needs a measured canvas, so it is replaced with a stand-in that renders
// each node through the nodeTypes map it receives and exposes positions and edges as attributes.
vi.mock("reactflow", async (importOriginal) => {
  const actual = await importOriginal<typeof import("reactflow")>();
  const { createContext, createElement, useContext } = await import("react");

  type StubNode = { id: string; type?: string; position: { x: number; y: number }; data: unknown };
  type StubEdge = { id: string; source: string; target: string; animated?: boolean };
  type StubNodeTypes = Record<string, React.ComponentType<{ id: string; data: unknown }>>;

  const NodesContext = createContext<StubNode[]>([]);

  function ReactFlowStub({
    nodes,
    edges,
    nodeTypes,
    onNodeClick,
    children,
  }: {
    nodes: StubNode[];
    edges: StubEdge[];
    nodeTypes: StubNodeTypes;
    onNodeClick?: (event: React.MouseEvent, node: StubNode) => void;
    children?: React.ReactNode;
  }) {
    return createElement(
      NodesContext.Provider,
      { value: nodes },
      createElement(
        "div",
        { "data-testid": "reactflow" },
        ...nodes.map((node) => {
          const Component = nodeTypes[node.type ?? ""];
          return createElement(
            "div",
            {
              key: node.id,
              "data-node-id": node.id,
              "data-node-type": node.type,
              "data-y": node.position.y,
              onClick: (event: React.MouseEvent) => onNodeClick?.(event, node),
            },
            createElement(Component, { id: node.id, data: node.data }),
          );
        }),
        ...edges.map((edge) =>
          createElement("span", {
            key: edge.id,
            "data-edge-id": edge.id,
            "data-source": edge.source,
            "data-target": edge.target,
            "data-animated": String(edge.animated === true),
          }),
        ),
        children,
      ),
    );
  }

  function MiniMapStub({ nodeClassName }: { nodeClassName?: (node: StubNode) => string }) {
    const nodes = useContext(NodesContext);
    return createElement(
      "div",
      { "data-testid": "minimap" },
      ...nodes.map((node) => createElement("span", { key: node.id, className: nodeClassName?.(node) })),
    );
  }

  return {
    ...actual,
    default: ReactFlowStub,
    Background: () => null,
    Controls: () => null,
    Handle: () => null,
    MiniMap: MiniMapStub,
    useNodesInitialized: () => flowState.initialized,
    useReactFlow: () => ({ getNodes: () => flowState.nodes, setViewport: flowState.setViewport }),
    useStore: (selector: (state: { width: number; height: number }) => number) =>
      selector({ width: flowState.width, height: flowState.height }),
  };
});

vi.mock("@/components/DepotKindIcon", () => ({ DepotKindIcon: () => null }));

vi.mock("./DepotNodePanel", () => ({
  default: ({ node, onClose }: { node: unknown; onClose: () => void }) => (
    <div data-testid="depot-node-panel">
      <pre data-testid="panel-node">{JSON.stringify(node)}</pre>
      <button type="button" onClick={onClose}>
        Close panel
      </button>
    </div>
  ),
}));

beforeEach(() => {
  flowState.initialized = false;
  flowState.width = 0;
  flowState.height = 0;
  flowState.nodes = [];
  flowState.setViewport.mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function graphOf(overrides: Partial<BrowseDepotGraph> = {}): BrowseDepotGraph {
  return {
    depots: [],
    modules: [],
    providers: [],
    skills: [],
    agents: [],
    edges: [],
    summary: { totalDepots: 0, totalModules: 0, totalProviders: 0, totalSkills: 0, totalAgents: 0 },
    generatedAt: "2026-11-21T00:00:00Z",
    ...overrides,
  };
}

function renderGraph(props: React.ComponentProps<typeof DepotGraph>) {
  return render(
    <ThemeProvider theme={theme}>
      <DepotGraph {...props} />
    </ThemeProvider>,
  );
}

function nodeEl(id: string): HTMLElement {
  const el = document.querySelector<HTMLElement>(`[data-node-id="${id}"]`);
  if (!el) {
    throw new Error(`node ${id} was not rendered`);
  }
  return el;
}

function nodeIds(): string[] {
  return Array.from(document.querySelectorAll("[data-node-id]"), (el) => el.getAttribute("data-node-id") ?? "");
}

function nodeY(id: string): number {
  return Number(nodeEl(id).getAttribute("data-y"));
}

function edgeIds(): string[] {
  return Array.from(document.querySelectorAll("[data-edge-id]"), (el) => el.getAttribute("data-edge-id") ?? "");
}

function selectedNode(): unknown {
  return JSON.parse(screen.getByTestId("panel-node").textContent ?? "null");
}

const fullGraph = graphOf({
  depots: [
    { id: "depot-main", namespace: "ns", name: "main", storageBackend: "s3" },
    { id: "depot-two", namespace: "ns", name: "two" },
  ],
  modules: [{ id: "module-vpc", namespace: "ns", name: "vpc", provider: "aws", synced: true, latestVersion: "5.0.0" }],
  providers: [
    {
      id: "provider-aws",
      namespace: "ns",
      name: "aws",
      providerNamespace: "hashicorp",
      upstreamRegistry: "registry.terraform.io",
      synced: true,
    },
  ],
  skills: [{ id: "skill-context", kind: "skill", namespace: "ns", name: "context", synced: true, latestVersion: "1.0.0" }],
  agents: [{ id: "agent-review", kind: "agent", namespace: "ns", name: "review", synced: false, syncStatus: "Failed" }],
  edges: [{ id: "rel-depot-module", source: "depot-main", target: "module-vpc" }],
});

describe("DepotGraph empty state", () => {
  it("shows the empty message instead of the canvas when the graph has no resources", () => {
    renderGraph({ graph: graphOf() });

    expect(screen.getByText("No depot relationships to display.")).toBeTruthy();
    expect(screen.queryByTestId("reactflow")).toBeNull();
  });
});

describe("DepotGraph node construction", () => {
  it("lays out depots, resources, and their version nodes in lane order", () => {
    renderGraph({ graph: fullGraph });

    expect(nodeIds()).toEqual([
      "depot-main",
      "depot-two",
      "module-vpc",
      "provider-aws",
      "skill-context",
      "agent-review",
      "version/ns/vpc/5.0.0",
      "version/skill/ns/context/1.0.0",
    ]);
    expect(nodeY("depot-two")).toBe(140);
    expect(nodeY("module-vpc")).toBe(0);
    expect(nodeY("provider-aws")).toBe(86);
    expect(nodeY("skill-context")).toBe(172);
    expect(nodeY("agent-review")).toBe(258);
    expect(nodeY("version/skill/ns/context/1.0.0")).toBe(172);
  });

  it("renders the node labels and subtitles for each resource kind", () => {
    renderGraph({ graph: fullGraph });

    expect(within(nodeEl("depot-main")).getByText("main")).toBeTruthy();
    expect(within(nodeEl("depot-main")).getByText("ns · s3")).toBeTruthy();
    expect(within(nodeEl("depot-two")).getByText("ns")).toBeTruthy();
    expect(within(nodeEl("module-vpc")).getByText("ns · aws")).toBeTruthy();
    expect(within(nodeEl("provider-aws")).getByText("ns/hashicorp")).toBeTruthy();
    expect(within(nodeEl("skill-context")).getByText("ns · 1.0.0")).toBeTruthy();
    expect(within(nodeEl("agent-review")).getByText("ns · ⚠ unsynced")).toBeTruthy();
    expect(within(nodeEl("version/ns/vpc/5.0.0")).getByText("5.0.0")).toBeTruthy();
    expect(within(nodeEl("version/ns/vpc/5.0.0")).getByText("vpc")).toBeTruthy();
  });

  it("connects versions to their parents and animates backend relationship edges", () => {
    renderGraph({ graph: fullGraph });

    expect(edgeIds()).toEqual([
      "edge-module-vpc-version/ns/vpc/5.0.0",
      "edge-skill-context-version/skill/ns/context/1.0.0",
      "rel-depot-module",
    ]);
    const relationship = document.querySelector('[data-edge-id="rel-depot-module"]');
    expect(relationship?.getAttribute("data-animated")).toBe("true");
    const versionEdge = document.querySelector('[data-edge-id="edge-module-vpc-version/ns/vpc/5.0.0"]');
    expect(versionEdge?.getAttribute("data-animated")).toBe("false");
    expect(versionEdge?.getAttribute("data-source")).toBe("module-vpc");
  });

  it("treats a module as synced when its sync status reports success", () => {
    renderGraph({
      graph: graphOf({
        modules: [
          { id: "module-a", namespace: "ns", name: "a", synced: false, syncStatus: "Successfully synced" },
          { id: "module-b", namespace: "ns", name: "b", synced: false, syncStatus: "Failed" },
        ],
      }),
    });

    expect(within(nodeEl("module-a")).queryByText(/unsynced/)).toBeNull();
    expect(within(nodeEl("module-b")).getByText("ns · ⚠ unsynced")).toBeTruthy();
  });

  it("omits version nodes for a module that has no known version", () => {
    renderGraph({ graph: graphOf({ modules: [{ id: "module-bare", namespace: "ns", name: "bare", synced: true }] }) });

    expect(nodeIds()).toEqual(["module-bare"]);
    expect(edgeIds()).toEqual([]);
  });

  it("infers depot relationships from managed names when the backend returns no edges", () => {
    renderGraph({
      graph: graphOf({
        depots: [
          {
            id: "depot-main",
            namespace: "ns",
            name: "main",
            managedModuleNames: ["vpc", "missing"],
            managedProviderNames: ["aws"],
            managedSkillNames: ["context"],
            managedAgentNames: ["review", "absent"],
          },
          { id: "depot-empty", namespace: "ns", name: "empty" },
        ],
        modules: [{ id: "module-vpc", namespace: "ns", name: "vpc", synced: true }],
        providers: [{ id: "provider-aws", namespace: "ns", name: "aws", synced: true }],
        skills: [{ id: "skill-context", kind: "skill", namespace: "ns", name: "context", synced: true }],
        agents: [{ id: "agent-review", kind: "agent", namespace: "ns", name: "review", synced: true }],
      }),
    });

    expect(edgeIds()).toEqual([
      "edge-depot-main-module-vpc",
      "edge-depot-main-provider-aws",
      "edge-depot-main-skill-context",
      "edge-depot-main-agent-review",
    ]);
    const inferred = document.querySelector('[data-edge-id="edge-depot-main-agent-review"]');
    expect(inferred?.getAttribute("data-animated")).toBe("true");
  });
});

describe("DepotGraph module versions", () => {
  it("sorts versions newest first and collapses those beyond the page size", () => {
    renderGraph({
      graph: graphOf({
        modules: [{ id: "module-vpc", namespace: "ns", name: "vpc", synced: true }],
      }),
      moduleVersionsByKey: {
        "ns/vpc": ["0.9.0", "v1.10.0", "1.9.0", "1.0.0-alpha", "1.0.0-beta", "0.9.0", ""],
      },
    });

    expect(nodeIds()).toEqual([
      "module-vpc",
      "version/ns/vpc/v1.10.0",
      "version/ns/vpc/1.9.0",
      "version/ns/vpc/1.0.0-beta",
      "versionOverflow/module-vpc",
    ]);
    expect(within(nodeEl("versionOverflow/module-vpc")).getByText("2 more versions")).toBeTruthy();
  });

  it("prefers version metadata and reports the file details when a version is selected", () => {
    renderGraph({
      graph: fullGraph,
      moduleVersionsByKey: { "ns/vpc": ["9.9.9"] },
      moduleVersionMetaByKey: {
        "ns/vpc": [
          {
            version: "2.0.0",
            fileName: "vpc-2.0.0.zip",
            checksum: "abc",
            lastScanned: "2026-11-01T00:00:00Z",
          },
          { version: "2.0.0", fileName: "duplicate.zip" },
          { version: "1.0.0" },
        ],
      },
    });

    expect(nodeIds()).toContain("version/ns/vpc/2.0.0");
    expect(nodeIds()).toContain("version/ns/vpc/1.0.0");
    expect(nodeIds()).not.toContain("version/ns/vpc/9.9.9");

    fireEvent.click(nodeEl("version/ns/vpc/2.0.0"));

    expect(selectedNode()).toEqual({
      kind: "version",
      namespace: "ns",
      name: "vpc",
      latestVersion: "2.0.0",
      fileName: "vpc-2.0.0.zip",
      checksum: "abc",
      lastScanned: "2026-11-01T00:00:00Z",
    });
  });

  it("expands and collapses the overflow node and restacks the resources below", () => {
    renderGraph({
      graph: graphOf({
        modules: [{ id: "module-vpc", namespace: "ns", name: "vpc", synced: true }],
        providers: [{ id: "provider-aws", namespace: "ns", name: "aws", synced: true }],
      }),
      moduleVersionsByKey: { "ns/vpc": ["5.0.0", "4.0.0", "3.0.0", "2.0.0", "1.0.0"] },
    });

    expect(nodeIds()).not.toContain("version/ns/vpc/2.0.0");
    expect(nodeY("versionOverflow/module-vpc")).toBe(240);
    expect(nodeY("provider-aws")).toBe(324);

    fireEvent.click(nodeEl("versionOverflow/module-vpc"));

    expect(nodeIds()).toContain("version/ns/vpc/1.0.0");
    expect(within(nodeEl("versionOverflow/module-vpc")).getByText("−")).toBeTruthy();
    expect(nodeY("provider-aws")).toBe(484);
    expect(screen.queryByTestId("depot-node-panel")).toBeNull();

    fireEvent.click(nodeEl("versionOverflow/module-vpc"));

    expect(nodeIds()).not.toContain("version/ns/vpc/1.0.0");
    expect(nodeY("provider-aws")).toBe(324);
  });
});

describe("DepotGraph provider versions", () => {
  it("names versions from their metadata and orders them by version then name", () => {
    renderGraph({
      graph: graphOf({
        providers: [{ id: "provider-aws", namespace: "ns", name: "aws", synced: true }],
      }),
      providerVersionMetaByKey: {
        "ns/aws": [
          { version: "2.0.0", name: "b-named", fileName: "b.zip" },
          { version: "2.0.0", name: "a-named" },
          { version: "1.0.0" },
        ],
      },
    });

    expect(nodeIds()).toEqual([
      "provider-aws",
      "version/ns/aws/a-named",
      "version/ns/aws/b-named",
      "version/ns/aws/1.0.0-2",
    ]);
    expect(within(nodeEl("version/ns/aws/a-named")).getByText("a-named")).toBeTruthy();
    expect(within(nodeEl("version/ns/aws/a-named")).getByText("2.0.0")).toBeTruthy();
    expect(within(nodeEl("version/ns/aws/1.0.0-2")).getByText("aws")).toBeTruthy();

    fireEvent.click(nodeEl("version/ns/aws/b-named"));

    expect(selectedNode()).toEqual({
      kind: "version",
      namespace: "ns",
      name: "b-named",
      latestVersion: "2.0.0",
      fileName: "b.zip",
    });
  });

  it("falls back to unique version strings and collapses overflow beyond the page size", () => {
    renderGraph({
      graph: graphOf({
        providers: [{ id: "provider-aws", namespace: "ns", name: "aws", synced: true }],
      }),
      providerVersionsByKey: {
        "ns/aws": ["3.0.0", "2.0.0", "2.0.0", "", "1.0.0", "0.9.0"],
      },
    });

    expect(nodeIds()).toEqual([
      "provider-aws",
      "version/ns/aws/3.0.0-0",
      "version/ns/aws/2.0.0-1",
      "version/ns/aws/1.0.0-2",
      "versionOverflow/provider-aws",
    ]);
    expect(within(nodeEl("versionOverflow/provider-aws")).getByText("1 more versions")).toBeTruthy();
  });

  it("omits version nodes for a provider without versions", () => {
    renderGraph({
      graph: graphOf({
        providers: [{ id: "provider-aws", namespace: "ns", name: "aws", synced: true }],
      }),
    });

    expect(nodeIds()).toEqual(["provider-aws"]);
  });
});

describe("DepotGraph skill and agent versions", () => {
  it("labels commit-only versions and reports selected version metadata", () => {
    renderGraph({
      graph: fullGraph,
      agentVersionMetaByKey: {
        "skill/ns/context": [
          { version: "", commit: "98396d2abcdef0123" },
          { version: "1.2.0", fileName: "skill.zip", checksum: "sha", lastScanned: "2026-11-02T00:00:00Z" },
        ],
      },
    });

    expect(nodeIds()).toContain("version/skill/ns/context/1.2.0");
    expect(nodeIds()).toContain("version/skill/ns/context/98396d2");
    expect(nodeIds()).not.toContain("version/skill/ns/context/1.0.0");

    fireEvent.click(nodeEl("version/skill/ns/context/1.2.0"));

    expect(selectedNode()).toEqual({
      kind: "version",
      namespace: "ns",
      name: "context",
      latestVersion: "1.2.0",
      fileName: "skill.zip",
      checksum: "sha",
      lastScanned: "2026-11-02T00:00:00Z",
    });
  });

  it("collapses agent versions beyond the page size behind an expandable overflow node", () => {
    renderGraph({
      graph: graphOf({
        agents: [{ id: "agent-review", kind: "agent", namespace: "ns", name: "review", synced: true }],
      }),
      agentVersionMetaByKey: {
        "agent/ns/review": [{ version: "4.0.0" }, { version: "3.0.0" }, { version: "2.0.0" }, { version: "1.0.0" }],
      },
    });

    expect(nodeIds()).toEqual([
      "agent-review",
      "version/agent/ns/review/4.0.0",
      "version/agent/ns/review/3.0.0",
      "version/agent/ns/review/2.0.0",
      "versionOverflow/agent-review",
    ]);

    fireEvent.click(nodeEl("versionOverflow/agent-review"));

    expect(nodeIds()).toContain("version/agent/ns/review/1.0.0");
    expect(within(nodeEl("versionOverflow/agent-review")).getByText("−")).toBeTruthy();
  });
});

describe("DepotGraph node selection", () => {
  it.each([
    {
      name: "depot",
      id: "depot-main",
      expected: { kind: "depot", namespace: "ns", name: "main", storageBackend: "s3" },
    },
    {
      name: "provider",
      id: "provider-aws",
      expected: {
        kind: "provider",
        namespace: "ns",
        name: "aws",
        synced: true,
        providerNamespace: "hashicorp",
      },
    },
    {
      name: "skill",
      id: "skill-context",
      expected: { kind: "skill", namespace: "ns", name: "context", synced: true, latestVersion: "1.0.0" },
    },
    {
      name: "agent",
      id: "agent-review",
      expected: { kind: "agent", namespace: "ns", name: "review", synced: false },
    },
  ])("opens the detail panel for a $name node", ({ id, expected }) => {
    renderGraph({ graph: fullGraph });

    fireEvent.click(nodeEl(id));

    expect(selectedNode()).toEqual(expected);
  });

  it("passes module storage and GitHub details to the detail panel", () => {
    renderGraph({
      graph: fullGraph,
      moduleDetailByKey: {
        "ns/vpc": { storageConfig: { backend: "s3", bucket: "modules", region: "us-east-1" }, githubAuthenticated: true },
      },
    });

    fireEvent.click(nodeEl("module-vpc"));

    expect(selectedNode()).toEqual({
      kind: "module",
      namespace: "ns",
      name: "vpc",
      synced: true,
      provider: "aws",
      latestVersion: "5.0.0",
      storageConfig: { backend: "s3", bucket: "modules", region: "us-east-1" },
      githubAuthenticated: true,
    });
  });

  it("closes the detail panel from its close control", () => {
    renderGraph({ graph: fullGraph });

    fireEvent.click(nodeEl("depot-main"));
    expect(screen.getByTestId("depot-node-panel")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Close panel" }));

    expect(screen.queryByTestId("depot-node-panel")).toBeNull();
  });
});

describe("DepotGraph minimap and mobile toggle", () => {
  it("colors minimap nodes by their kind", () => {
    renderGraph({ graph: fullGraph });

    const classes = Array.from(document.querySelectorAll('[data-testid="minimap"] span'), (el) => el.className);
    expect(classes).toEqual([
      "od-minimap-depot",
      "od-minimap-depot",
      "od-minimap-module",
      "od-minimap-provider",
      "od-minimap-skill",
      "od-minimap-agent",
      "od-minimap-version",
      "od-minimap-version",
    ]);
  });

  it("shows the minimap when the mobile map toggle event fires", () => {
    renderGraph({ graph: fullGraph });

    const minimapBox = screen.getByTestId("minimap").parentElement as HTMLElement;
    const hiddenClassName = minimapBox.className;

    act(() => {
      window.dispatchEvent(new Event(TOGGLE_NODE_MAP_EVENT));
    });

    expect(minimapBox.className).not.toBe(hiddenClassName);

    act(() => {
      window.dispatchEvent(new Event(TOGGLE_NODE_MAP_EVENT));
    });

    expect(minimapBox.className).toBe(hiddenClassName);
  });

  it("removes the toggle listener when unmounted", () => {
    const removeSpy = vi.spyOn(window, "removeEventListener");
    const { unmount } = renderGraph({ graph: fullGraph });

    unmount();

    expect(removeSpy).toHaveBeenCalledWith(TOGGLE_NODE_MAP_EVENT, expect.any(Function));
  });
});

describe("DepotGraph initial viewport", () => {
  const twoNodeGraph = graphOf({
    depots: [
      { id: "depot-a", namespace: "ns", name: "a" },
      { id: "depot-b", namespace: "ns", name: "b" },
    ],
  });

  function measure(nodes: Array<{ id: string; x: number; y: number }>, width: number, height: number) {
    flowState.nodes = nodes.map((n) => ({ id: n.id, position: { x: n.x, y: n.y }, width: 200, height: 64 }));
    flowState.width = width;
    flowState.height = height;
  }

  it("waits for nodes to initialize before changing the viewport", () => {
    measure([{ id: "a", x: 0, y: 0 }], 1000, 800);
    flowState.initialized = false;

    renderGraph({ graph: twoNodeGraph });

    expect(flowState.setViewport).not.toHaveBeenCalled();
  });

  it("waits for a measured canvas before changing the viewport", () => {
    measure([{ id: "a", x: 0, y: 0 }], 0, 0);
    flowState.initialized = true;

    renderGraph({ graph: twoNodeGraph });

    expect(flowState.setViewport).not.toHaveBeenCalled();
  });

  it("fits a small graph to the canvas when it can be read at a usable zoom", () => {
    measure(
      [
        { id: "a", x: 0, y: 0 },
        { id: "b", x: 200, y: 100 },
      ],
      1000,
      800,
    );
    flowState.initialized = true;

    renderGraph({ graph: twoNodeGraph });

    expect(flowState.setViewport).toHaveBeenCalledTimes(1);
    expect(flowState.setViewport).toHaveBeenCalledWith({ x: 100, y: 236, zoom: 2 });
  });

  it("anchors a wide graph at the readable zoom from the top-left when it cannot be centered", () => {
    measure(
      [
        { id: "a", x: 100, y: 50 },
        { id: "b", x: 3100, y: 50 },
      ],
      1000,
      800,
    );
    flowState.initialized = true;

    renderGraph({ graph: twoNodeGraph });

    expect(flowState.setViewport).toHaveBeenCalledWith({ x: -40, y: 0, zoom: 0.8 });
  });

  it("centers a tall graph horizontally when it is narrow enough at the readable zoom", () => {
    measure(
      [
        { id: "a", x: 0, y: 0 },
        { id: "b", x: 0, y: 2000 },
      ],
      1000,
      500,
    );
    flowState.initialized = true;

    renderGraph({ graph: twoNodeGraph });

    expect(flowState.setViewport).toHaveBeenCalledWith({ x: 420, y: 40, zoom: 0.8 });
  });

  it("applies the initial viewport only once across re-renders", () => {
    measure([{ id: "a", x: 0, y: 0 }], 1000, 800);
    flowState.initialized = true;

    const { rerender } = renderGraph({ graph: twoNodeGraph });
    rerender(
      <ThemeProvider theme={theme}>
        <DepotGraph graph={twoNodeGraph} />
      </ThemeProvider>,
    );

    expect(flowState.setViewport).toHaveBeenCalledTimes(1);
  });
});
