// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssemblyCanvas from "./AssemblyCanvas";
import { EMPTY_TYPE_SPEC, EMPTY_VALUE_SPEC } from "./types";
import type { ModuleNodeData } from "./ModuleNode";
import type { ProviderNodeData } from "./ProviderNode";
import type { VariableNodeData } from "./VariableNode";
import type { BrowseProviderSchema, BrowseResource } from "@/lib/api";
import { exportAssembly, getProviderSchema } from "@/lib/api";
import theme from "@/theme";

const STORAGE_KEY = "opendepot:assembly:v3";

// Minimal stand-in for React Flow. It renders each node through the nodeTypes
// map it receives, so the real node components are replaced with the stubs
// below and the canvas can be driven without a measured DOM.
vi.mock("reactflow", async (importOriginal) => {
  const actual = await importOriginal<typeof import("reactflow")>();
  const { createElement, Fragment } = await import("react");

  type StubNode = { id: string; type?: string; data: unknown };
  type StubNodeTypes = Record<string, (props: { id: string; data: unknown }) => React.ReactElement>;

  return {
    ...actual,
    default: ({ nodes, nodeTypes }: { nodes: StubNode[]; nodeTypes: StubNodeTypes }) =>
      createElement(
        "div",
        { "data-testid": "reactflow" },
        ...nodes.map((node) => {
          const Component = nodeTypes[node.type ?? "module"];
          return createElement(Component, { key: node.id, id: node.id, data: node.data });
        }),
      ),
    ReactFlowProvider: ({ children }: { children: React.ReactNode }) => createElement(Fragment, null, children),
    Background: () => null,
    Controls: () => null,
    MiniMap: () => null,
    useReactFlow: () => ({ screenToFlowPosition: ({ x, y }: { x: number; y: number }) => ({ x, y }) }),
  };
});

vi.mock("./ModuleNode", () => ({
  default: ({ data }: { data: ModuleNodeData }) => (
    <div data-testid="module-node">
      {`${data.instanceName}|${data.loading ? "loading" : "ready"}|${data.grade ?? ""}|${data.error ?? ""}`}
    </div>
  ),
}));

vi.mock("./VariableNode", () => ({
  default: ({ data }: { data: VariableNodeData }) => <div data-testid="variable-node">{data.name}</div>,
}));

vi.mock("./ProviderNode", () => ({
  default: ({ data }: { data: ProviderNodeData }) => (
    <div data-testid="provider-node">
      {`${data.localName}|${data.loading ? "loading" : "ready"}|${data.error ?? ""}`}
    </div>
  ),
}));

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    exportAssembly: vi.fn(),
    getProviderSchema: vi.fn(),
  };
});

function resource(overrides: Partial<BrowseResource>): BrowseResource {
  return {
    kind: "module",
    namespace: "terraform-aws",
    name: "terraform-aws-vpc",
    latestVersion: "5.0.0",
    syncStatus: "Synced",
    synced: true,
    public: true,
    provider: "aws",
    repoUrl: "",
    providerNamespace: "",
    upstreamRegistry: "",
    platforms: [],
    scanCounts: null,
    ...overrides,
  } as BrowseResource;
}

const vpcModule = resource({});
const eksModule = resource({ name: "terraform-aws-eks", provider: "aws", latestVersion: "v19.0.0" });
const awsProvider = resource({
  kind: "provider",
  namespace: "hashicorp",
  name: "aws",
  latestVersion: "6.0.0",
  provider: "",
  providerNamespace: "hashicorp",
});

const providerSchema: BrowseProviderSchema = {
  namespace: "hashicorp",
  name: "aws",
  providerNamespace: "hashicorp",
  providerName: "aws",
  version: "6.0.0",
  schemaVersion: "assembly.provider.v1",
  configuration: { attributes: {} },
};

function contractResponse(grade: "full" | "partial" | "unsupported") {
  return {
    schemaVersion: "1",
    module: { namespace: "terraform-aws", name: "terraform-aws-vpc", version: "5.0.0" },
    variables: [],
    outputs: [],
    requiredProviders: [],
    compatibility: { grade },
    provenance: {},
  };
}

// Node ships an experimental localStorage that shadows jsdom's, so the test supplies its own.
function stubStorage() {
  const store = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => void store.set(key, value),
      removeItem: (key: string) => void store.delete(key),
      clear: () => store.clear(),
    },
  });
}

function renderCanvas(modules: BrowseResource[] = [vpcModule, eksModule], providers: BrowseResource[] = [awsProvider]) {
  return render(
    <ThemeProvider theme={theme}>
      <AssemblyCanvas modules={modules} providers={providers} />
    </ThemeProvider>,
  );
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("AssemblyCanvas", () => {
  const fetchMock = vi.fn<(input: string) => Promise<Response>>();

  beforeEach(() => {
    stubStorage();
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    vi.mocked(exportAssembly).mockReset();
    vi.mocked(getProviderSchema).mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("shows the empty canvas hint, a disabled Clear button, and no variables", () => {
    renderCanvas();

    screen.getByText(/Drag a module from the left/);
    expect((screen.getByRole("button", { name: "Clear" }) as HTMLButtonElement).disabled).toBe(true);
    screen.getByText("No variables yet.");
  });

  it("filters modules and providers by the search query", () => {
    renderCanvas([vpcModule, eksModule], [awsProvider]);

    fireEvent.change(screen.getByPlaceholderText("Search resources…"), { target: { value: "eks" } });
    screen.getByText("terraform-aws-eks");
    expect(screen.queryByText("terraform-aws-vpc")).toBeNull();
    screen.getByText("No providers found.");

    fireEvent.change(screen.getByPlaceholderText("Search resources…"), { target: { value: "zzz" } });
    screen.getByText("No modules found.");
    screen.getByText("No providers found.");
  });

  it("matches provider resources on the provider namespace", () => {
    renderCanvas([], [awsProvider]);

    fireEvent.change(screen.getByPlaceholderText("Search resources…"), { target: { value: "hashicorp" } });
    expect(screen.getByText("aws").textContent).toBe("aws");
    expect(screen.queryByText("No providers found.")).toBeNull();
  });

  it("adds a module node and loads its contract", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, contractResponse("full")));
    renderCanvas();

    fireEvent.click(screen.getByText("terraform-aws-vpc"));

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/contract/terraform-aws/module/terraform-aws-vpc?version=5.0.0",
    );
    await waitFor(() => {
      expect(screen.getByTestId("module-node").textContent).toContain("vpc|ready|full|");
    });
    expect((screen.getByRole("button", { name: "Clear" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("marks a module as unsupported when the contract is missing", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 404 }));
    renderCanvas();

    fireEvent.click(screen.getByText("terraform-aws-vpc"));

    await waitFor(() => {
      expect(screen.getByTestId("module-node").textContent).toContain("ready|unsupported|");
    });
  });

  it("records a contract load failure on the module node", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 500 }));
    renderCanvas();

    fireEvent.click(screen.getByText("terraform-aws-vpc"));

    await waitFor(() => {
      expect(screen.getByTestId("module-node").textContent).toContain("Failed to load contract: 500");
    });
  });

  it("adds a unique instance name for each repeated module", async () => {
    fetchMock.mockImplementation(async () => jsonResponse(200, contractResponse("partial")));
    renderCanvas();

    fireEvent.click(screen.getByText("terraform-aws-vpc"));
    fireEvent.click(screen.getByText("terraform-aws-vpc"));

    await waitFor(() => {
      const nodes = screen.getAllByTestId("module-node");
      expect(nodes).toHaveLength(2);
      expect(nodes[0].textContent).toContain("vpc|ready|partial");
      expect(nodes[1].textContent).toContain("vpc_2|ready|partial");
    });
  });

  it("adds a provider node with its loaded schema", async () => {
    vi.mocked(getProviderSchema).mockResolvedValue(providerSchema);
    renderCanvas();

    fireEvent.click(screen.getByText("aws"));

    expect(getProviderSchema).toHaveBeenCalledWith("hashicorp", "aws", "6.0.0");
    await waitFor(() => {
      expect(screen.getByTestId("provider-node").textContent).toContain("aws|ready|");
    });
  });

  it("reports an unavailable provider schema on the provider node", async () => {
    vi.mocked(getProviderSchema).mockResolvedValue(null);
    renderCanvas();

    fireEvent.click(screen.getByText("aws"));

    await waitFor(() => {
      expect(screen.getByTestId("provider-node").textContent).toContain("Provider configuration schema is unavailable.");
    });
  });

  it("records a provider schema failure on the provider node", async () => {
    vi.mocked(getProviderSchema).mockRejectedValue(new Error("schema service down"));
    renderCanvas();

    fireEvent.click(screen.getByText("aws"));

    await waitFor(() => {
      expect(screen.getByTestId("provider-node").textContent).toContain("schema service down");
    });
  });

  it("adds variables with unique names", () => {
    renderCanvas();

    const addButton = screen.getByRole("button", { name: /Add/ });
    fireEvent.click(addButton);
    fireEvent.click(addButton);

    expect(screen.getAllByTestId("variable-node").map((n) => n.textContent)).toEqual(["var", "var_2"]);
    screen.getByText("var.var");
    screen.getByText("var.var_2");
  });

  it("adds a module dropped onto the canvas", async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, contractResponse("full")));
    renderCanvas();

    const canvas = screen.getByTestId("reactflow").parentElement as HTMLElement;
    fireEvent.drop(canvas, {
      dataTransfer: {
        getData: (type: string) => (type === "application/opendepot-module" ? JSON.stringify(eksModule) : ""),
      },
    });

    await waitFor(() => {
      expect(screen.getByTestId("module-node").textContent).toContain("eks|ready|full");
    });
  });

  it("adds a provider dropped onto the canvas", async () => {
    vi.mocked(getProviderSchema).mockResolvedValue(providerSchema);
    renderCanvas();

    const canvas = screen.getByTestId("reactflow").parentElement as HTMLElement;
    fireEvent.drop(canvas, {
      dataTransfer: {
        getData: (type: string) => (type === "application/opendepot-provider" ? JSON.stringify(awsProvider) : ""),
      },
    });

    await waitFor(() => {
      expect(screen.getByTestId("provider-node").textContent).toContain("aws|ready|");
    });
  });

  it("ignores drops that carry no Assembly resource", () => {
    renderCanvas();

    const canvas = screen.getByTestId("reactflow").parentElement as HTMLElement;
    fireEvent.drop(canvas, { dataTransfer: { getData: () => "" } });

    expect(screen.queryByTestId("module-node")).toBeNull();
    expect(screen.queryByTestId("provider-node")).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps the canvas when Clear is cancelled", () => {
    renderCanvas();
    fireEvent.click(screen.getByRole("button", { name: /Add/ }));

    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    const dialog = screen.getByRole("dialog");
    within(dialog).getByText("Clear the entire canvas?");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    expect(screen.getByTestId("variable-node").textContent).toContain("var");
  });

  it("clears the canvas and browser storage when confirmed", async () => {
    window.localStorage.setItem(STORAGE_KEY, "[]");
    renderCanvas();
    fireEvent.click(screen.getByRole("button", { name: /Add/ }));

    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    fireEvent.click(screen.getByRole("button", { name: "Clear canvas" }));

    expect(screen.queryByTestId("variable-node")).toBeNull();
    screen.getByText(/Drag a module from the left/);
    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("restores a saved canvas from storage on mount", async () => {
    window.localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify([
        {
          id: "variable-7",
          type: "variable",
          position: { x: 0, y: 0 },
          data: {
            kind: "variable",
            name: "saved_var",
            type: EMPTY_TYPE_SPEC,
            description: "",
            validations: [],
            hasDefault: false,
            default: EMPTY_VALUE_SPEC,
          },
        },
      ]),
    );
    renderCanvas();

    const restored = await screen.findByText("var.saved_var");
    expect(restored.textContent).toBe("var.saved_var");
    expect(screen.getByTestId("variable-node").textContent).toBe("saved_var");
  });

  it("persists the canvas to storage after a change", async () => {
    renderCanvas();
    fireEvent.click(screen.getByRole("button", { name: /Add/ }));

    await waitFor(() => {
      expect(window.localStorage.getItem(STORAGE_KEY)).toContain('"name":"var"');
    });
  });

  it("toggles the desktop and mobile resource menus", () => {
    renderCanvas();

    fireEvent.click(screen.getByRole("button", { name: "Collapse Assembly Line menu" }));
    expect(screen.queryByRole("button", { name: "Collapse Assembly Line menu" })).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Assembly Line menu" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Expand Assembly Line menu" }));
    expect(screen.queryByRole("button", { name: "Expand Assembly Line menu" })).toBeNull();
    expect(screen.getByRole("button", { name: "Collapse Assembly Line menu" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Show Assembly Line menu" }));
    expect(screen.queryByRole("button", { name: "Show Assembly Line menu" })).toBeNull();
    expect(screen.getByRole("button", { name: "Hide Assembly Line menu" })).toBeTruthy();
  });

  it("downloads the archive after a successful export", async () => {
    const createObjectURL = vi.fn(() => "blob:assembly");
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL, revokeObjectURL }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    vi.mocked(exportAssembly).mockResolvedValue({ blob: new Blob(["zip"]) });
    renderCanvas();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await screen.findByText("The validated Assembly archive has been downloaded.");
    screen.getByText("Export complete");
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it("streams validation output and shows the server error", async () => {
    vi.mocked(exportAssembly).mockImplementation(async (_document, onProgress) => {
      onProgress?.({ phase: "validate", output: "checking module" });
      onProgress?.({ output: "plain line" });
      return {
        error: {
          error: "validation_failed",
          message: "Assembly validation failed",
          output: "tofu validate output",
        },
      };
    });
    renderCanvas();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await screen.findByText("Export failed");
    screen.getByText("Assembly validation failed");
    const output = screen.getByLabelText("OpenTofu output");
    expect(output.textContent).toContain("[validate] checking module");
    expect(output.textContent).toContain("plain line");
  });

  it("shows the error message when the export request throws", async () => {
    vi.mocked(exportAssembly).mockRejectedValue(new Error("network down"));
    renderCanvas();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await screen.findByText("network down");
    expect(screen.getByText("Export failed")).toBeTruthy();
    expect(screen.queryByText("Export complete")).toBeNull();
    expect(vi.mocked(exportAssembly)).toHaveBeenCalledTimes(1);
  });

  it("falls back to a generic message when a non-Error export failure occurs", async () => {
    vi.mocked(exportAssembly).mockRejectedValue("boom");
    renderCanvas();

    fireEvent.click(screen.getByRole("button", { name: "Export" }));

    await screen.findByText("Assembly export failed.");
    expect(screen.getByText("Export failed")).toBeTruthy();
    expect(screen.queryByText("boom")).toBeNull();
    expect(vi.mocked(exportAssembly)).toHaveBeenCalledTimes(1);
  });
});
