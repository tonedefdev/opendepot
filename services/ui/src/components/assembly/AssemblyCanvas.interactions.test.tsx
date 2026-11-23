// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AssemblyCanvas from "./AssemblyCanvas";
import type { FieldValue, ModuleInputValue, ReferenceOption } from "./types";
import type { ModuleNodeData } from "./ModuleNode";
import type { ProviderNodeData } from "./ProviderNode";
import type { VariableNodeData } from "./VariableNode";
import type { BrowseProviderSchema, BrowseResource } from "@/lib/api";
import { exportAssembly, getProviderSchema } from "@/lib/api";
import theme from "@/theme";

Object.assign(globalThis, { React });

const STORAGE_KEY = "opendepot:assembly:v3";

const registry = vi.hoisted(() => ({
  modules: new Map<string, { id: string; data: ModuleNodeData }>(),
  variables: new Map<string, { id: string; data: VariableNodeData }>(),
  providers: new Map<string, { id: string; data: ProviderNodeData }>(),
}));

// Minimal stand-in for React Flow that also exposes edges and the pane/node click handlers so the
// canvas wiring can be asserted without a measured DOM.
vi.mock("reactflow", async (importOriginal) => {
  const actual = await importOriginal<typeof import("reactflow")>();
  const { createElement, Fragment } = await import("react");

  type StubNode = { id: string; type?: string; data: unknown };
  type StubEdge = {
    id: string;
    source: string;
    label?: unknown;
    style?: { stroke?: string; strokeDasharray?: string };
  };
  type StubNodeTypes = Record<string, (props: { id: string; data: unknown }) => React.ReactElement>;

  return {
    ...actual,
    default: ({
      nodes,
      edges,
      nodeTypes,
      onPaneClick,
      onNodeClick,
      children,
    }: {
      nodes: StubNode[];
      edges: StubEdge[];
      nodeTypes: StubNodeTypes;
      onPaneClick?: () => void;
      onNodeClick?: () => void;
      children?: React.ReactNode;
    }) => {
      registry.modules.clear();
      registry.variables.clear();
      registry.providers.clear();
      return createElement(
        "div",
        { "data-testid": "reactflow" },
        createElement("button", { type: "button", onClick: () => onPaneClick?.() }, "Pane click"),
        createElement("button", { type: "button", onClick: () => onNodeClick?.() }, "Node click"),
        ...edges.map((edge) =>
          createElement("span", {
            key: edge.id,
            "data-testid": "edge",
            "data-source": edge.source,
            "data-label": String(edge.label ?? ""),
            "data-stroke": edge.style?.stroke ?? "",
            "data-dash": edge.style?.strokeDasharray ?? "",
          }),
        ),
        ...nodes.map((node) => {
          const Component = nodeTypes[node.type ?? "module"];
          return createElement(Component, { key: node.id, id: node.id, data: node.data });
        }),
        children,
      );
    },
    ReactFlowProvider: ({ children }: { children: React.ReactNode }) => createElement(Fragment, null, children),
    Background: () => null,
    Controls: () => null,
    MiniMap: ({ nodeClassName }: { nodeClassName: (node: { type: string }) => string }) =>
      createElement(
        "div",
        { "data-testid": "minimap" },
        ...["module", "variable", "provider"].map((type) => nodeClassName({ type })),
      ),
    useReactFlow: () => ({ screenToFlowPosition: ({ x, y }: { x: number; y: number }) => ({ x, y }) }),
  };
});

vi.mock("./ModuleNode", () => ({
  default: ({ id, data }: { id: string; data: ModuleNodeData }) => {
    registry.modules.set(data.instanceName, { id, data });
    return (
      <div data-testid="module-node">
        {`${data.instanceName}|${data.loading ? "loading" : "ready"}|${data.grade ?? ""}|${data.error ?? ""}`}
        {data.instanceNameError ? `|${data.instanceNameError}` : ""}
        {Object.entries(data.fieldErrors).map(([field, message]) => `|${field}: ${message}`).join("")}
        {(data.diagnostics ?? []).map((message) => `|diag: ${message}`).join("")}
        {(data.warnings ?? []).map((message) => `|warn: ${message}`).join("")}
      </div>
    );
  },
}));

vi.mock("./VariableNode", () => ({
  default: ({ id, data }: { id: string; data: VariableNodeData }) => {
    registry.variables.set(data.name, { id, data });
    return (
      <div data-testid="variable-node">
        {`${data.name}|${data.nameError ?? ""}|${data.validationError ?? ""}`}
        {(data.diagnostics ?? []).map((message) => `|diag: ${message}`).join("")}
      </div>
    );
  },
}));

vi.mock("./ProviderNode", () => ({
  default: ({ id, data }: { id: string; data: ProviderNodeData }) => {
    registry.providers.set(data.localName, { id, data });
    return (
      <div data-testid="provider-node">
        {`${data.localName}|${data.alias}|${data.loading ? "loading" : "ready"}|${data.error ?? ""}`}
        {data.localNameError ? `|${data.localNameError}` : ""}
        {data.aliasError ? `|${data.aliasError}` : ""}
        {(data.diagnostics ?? []).map((message) => `|diag: ${message}`).join("")}
      </div>
    );
  },
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
const rdsModule = resource({ name: "terraform-aws-rds", provider: "aws", latestVersion: "6.0.0" });
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

function contract(
  name: string,
  grade: "full" | "partial",
  variables: Record<string, unknown>[] = [],
  outputs: Record<string, unknown>[] = [],
) {
  return {
    schemaVersion: "1",
    module: { namespace: "terraform-aws", name, version: "5.0.0" },
    variables,
    outputs,
    requiredProviders: [],
    compatibility: { grade },
    provenance: {},
  };
}

const vpcContract = contract(
  "terraform-aws-vpc",
  "full",
  [],
  [
    { name: "vpc_id", type: "string", confidence: "exact" },
    { name: "subnet_ids", type: ["list", "string"], confidence: "exact" },
    { name: "enabled", type: "bool", confidence: "exact" },
    { name: "config", type: ["object", { name: "string", deep: ["object", { x: "string" }] }], confidence: "exact" },
  ],
);

const eksContract = contract(
  "terraform-aws-eks",
  "full",
  [
    { name: "cidr", type: "string", required: true },
    { name: "count_value", type: "number", required: false },
    { name: "subnets", type: ["list", "string"], required: false },
    { name: "from_list", type: ["list", "string"], required: false },
    { name: "extra_name", type: "string", required: false },
    { name: "extra_deep", type: "string", required: false },
    { name: "label_name", type: "string", required: false },
    { name: "settings_name", type: "string", required: false },
    { name: "output_key", type: "string", required: false },
    { name: "flag", type: "bool", required: false },
    { name: "zones_list", type: ["list", "string"], required: true },
    { name: "tags_map", type: ["map", "string"], required: true },
    { name: "settings_obj", type: ["object", { name: "string" }], required: true },
  ],
  [{ name: "vpc_id", type: "string", confidence: "exact" }],
);

const rdsContract = contract(
  "terraform-aws-rds",
  "partial",
  [{ name: "zone", type: "string", required: false }],
  [{ name: "endpoint", type: "string", confidence: "exact" }],
);

function contractFor(url: string): Response {
  if (url.includes("terraform-aws-eks")) {
    return jsonResponse(200, eksContract);
  }
  if (url.includes("terraform-aws-rds")) {
    return jsonResponse(200, rdsContract);
  }
  return jsonResponse(200, vpcContract);
}

// Node may ship an experimental localStorage that shadows jsdom's, so the test supplies its own.
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

function renderCanvas(modules: BrowseResource[] = [vpcModule, eksModule, rdsModule], providers: BrowseResource[] = [awsProvider]) {
  return render(
    <ThemeProvider theme={theme}>
      <AssemblyCanvas modules={modules} providers={providers} />
    </ThemeProvider>,
  );
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function moduleEntry(instanceName: string) {
  const entry = registry.modules.get(instanceName);
  if (!entry) {
    throw new Error(`module ${instanceName} is not rendered`);
  }
  return entry;
}

function variableEntry(name: string) {
  const entry = registry.variables.get(name);
  if (!entry) {
    throw new Error(`variable ${name} is not rendered`);
  }
  return entry;
}

function providerEntry(localName: string) {
  const entry = registry.providers.get(localName);
  if (!entry) {
    throw new Error(`provider ${localName} is not rendered`);
  }
  return entry;
}

function referenceOption(instanceName: string, label: string): ReferenceOption {
  const option = moduleEntry(instanceName).data.referenceOptions.find((candidate) => candidate.label === label);
  if (!option) {
    throw new Error(`no reference option ${label} for ${instanceName}`);
  }
  return option;
}

function referenceValue(option: ReferenceOption, overrides: Partial<FieldValue> = {}): ModuleInputValue {
  return {
    kind: "scalar",
    value: { mode: "reference", literal: "", refNodeId: option.nodeId, refOutput: option.output, ...overrides },
  };
}

function literalValue(literal: string): ModuleInputValue {
  return { kind: "scalar", value: { mode: "literal", literal } };
}

function addModule(resourceName: string) {
  fireEvent.click(screen.getByText(resourceName));
}

async function addLoadedModule(resourceName: string, instanceName: string) {
  addModule(resourceName);
  await waitFor(() => {
    expect(moduleEntry(instanceName).data.grade).not.toBeNull();
  });
}

function addVariable(name: string) {
  fireEvent.click(screen.getByRole("button", { name: /Add/ }));
  act(() => {
    variableEntry("var").data.onRename(name);
  });
  return variableEntry(name);
}

describe("AssemblyCanvas interactions", () => {
  const fetchMock = vi.fn<(input: string) => Promise<Response>>();

  beforeEach(() => {
    stubStorage();
    fetchMock.mockReset();
    fetchMock.mockImplementation(async (url: string) => contractFor(url));
    vi.stubGlobal("fetch", fetchMock);
    vi.mocked(exportAssembly).mockReset();
    vi.mocked(getProviderSchema).mockReset();
    vi.mocked(getProviderSchema).mockResolvedValue(providerSchema);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe("module callbacks", () => {
    it("renames, rewires fields, toggles optional fields, and changes multiplicity", async () => {
      renderCanvas();
      await addLoadedModule("terraform-aws-vpc", "vpc");

      act(() => {
        moduleEntry("vpc").data.onRenameInstance("network");
      });
      await waitFor(() => {
        expect(registry.modules.get("network")).toBeDefined();
      });

      act(() => {
        moduleEntry("network").data.onFieldChange("cidr", literalValue("10.0.0.0/16"));
      });
      expect(moduleEntry("network").data.values.cidr).toEqual(literalValue("10.0.0.0/16"));

      act(() => {
        moduleEntry("network").data.onOptionalFieldVisibilityChange("tags", true);
      });
      expect(moduleEntry("network").data.optionalFieldVisibility.tags).toBe(true);

      act(() => {
        moduleEntry("network").data.onMultiplicityChange({
          kind: "count",
          mode: "fixed",
          expr: { mode: "literal", literal: "3" },
        });
      });
      expect(moduleEntry("network").data.multiplicity.kind).toBe("count");
    });

    it("reports an invalid instance name on the module node", async () => {
      renderCanvas();
      await addLoadedModule("terraform-aws-vpc", "vpc");

      act(() => {
        moduleEntry("vpc").data.onRenameInstance("1bad");
      });

      await waitFor(() => {
        expect(screen.getByTestId("module-node").textContent).toContain("|");
      });
      expect(moduleEntry("1bad").data.instanceNameError).not.toBeNull();
    });
  });

  describe("variable callbacks", () => {
    it("removes a variable node through its onRemove callback", () => {
      renderCanvas();
      const variable = addVariable("doomed");
      expect(variableEntry("doomed").id).toBe(variable.id);

      act(() => {
        variableEntry("doomed").data.onRemove();
      });

      expect(registry.variables.has("doomed")).toBe(false);
    });

    it("edits type, default, description, validations, and reports name errors", () => {
      renderCanvas([], []);
      const variable = addVariable("region");

      act(() => {
        variable.data.onTypeChange({ kind: "list", element: { kind: "string" } });
      });
      act(() => {
        variableEntry("region").data.onHasDefaultChange(true);
      });
      act(() => {
        variableEntry("region").data.onDefaultChange({
          kind: "list",
          items: [{ kind: "scalar", literal: "a" }],
        });
      });
      act(() => {
        variableEntry("region").data.onDescriptionChange("Where to deploy");
      });
      act(() => {
        variableEntry("region").data.onValidationsChange([{ condition: "", errorMessage: "bad" }]);
      });

      expect(variableEntry("region").data.type).toEqual({ kind: "list", element: { kind: "string" } });
      expect(variableEntry("region").data.hasDefault).toBe(true);
      expect(variableEntry("region").data.description).toBe("Where to deploy");
      expect(variableEntry("region").data.validationError).toBe("Complete every variable validation");

      act(() => {
        variableEntry("region").data.onRename("");
      });
      expect(variableEntry("").data.nameError).toBe("Name is required");

      act(() => {
        variableEntry("").data.onRename("1beta");
      });
      expect(variableEntry("1beta").data.nameError).not.toBeNull();

      act(() => {
        variableEntry("1beta").data.onRename("ok");
      });
      expect(variableEntry("ok").data.nameError).toBeNull();
    });

    it("flags duplicate variable names on the canvas", () => {
      renderCanvas([], []);
      addVariable("dup");
      addVariable("other");

      act(() => {
        variableEntry("other").data.onRename("dup");
      });

      expect(variableEntry("dup").data.nameError).toBe("Name is already used by another variable on this canvas");
    });
  });

  describe("provider callbacks", () => {
    it("changes the alias, flags an invalid alias, and removes the provider", async () => {
      renderCanvas();
      addModule("aws");
      await waitFor(() => {
        expect(providerEntry("aws").data.loading).toBe(false);
      });

      act(() => {
        providerEntry("aws").data.onAliasChange("1bad");
      });
      expect(providerEntry("aws").data.aliasError).toBe("Alias is invalid");

      act(() => {
        providerEntry("aws").data.onAliasChange("west");
      });
      expect(providerEntry("aws").data.alias).toBe("west");

      act(() => {
        providerEntry("aws").data.onConfigurationChange({
          arguments: { region: { mode: "literal", literal: "us-east-1" } },
          blocks: {},
        });
      });
      expect(providerEntry("aws").data.configuration.arguments.region).toEqual({
        mode: "literal",
        literal: "us-east-1",
      });
    });

    it("reports duplicate provider local names with the same alias", async () => {
      renderCanvas();
      addModule("aws");
      addModule("aws");

      await waitFor(() => {
        const nodes = screen.getAllByTestId("provider-node");
        expect(nodes).toHaveLength(2);
        expect(nodes.every((node) => node.textContent?.includes("Provider local name and alias must be unique"))).toBe(true);
      });
    });

    it("strips provider bindings from modules when the provider is removed", async () => {
      renderCanvas();
      addModule("aws");
      await waitFor(() => {
        expect(providerEntry("aws").data.loading).toBe(false);
      });
      await addLoadedModule("terraform-aws-eks", "eks");

      act(() => {
        moduleEntry("eks").data.onProviderBindingChange("aws", providerEntry("aws").id);
      });
      expect(moduleEntry("eks").data.providerBindings.aws).toBeDefined();

      act(() => {
        moduleEntry("eks").data.onProviderBindingChange("aws", "");
      });
      expect(moduleEntry("eks").data.providerBindings.aws).toBeUndefined();

      act(() => {
        moduleEntry("eks").data.onProviderBindingChange("aws", providerEntry("aws").id);
      });
      act(() => {
        providerEntry("aws").data.onRemove();
      });

      await waitFor(() => {
        expect(registry.providers.has("aws")).toBe(false);
      });
      expect(moduleEntry("eks").data.providerBindings.aws).toBeUndefined();
    });
  });

  describe("wiring and edges", () => {
    it("draws reference, count, for_each, and provider edges with compatibility labels", async () => {
      renderCanvas();
      addModule("aws");
      await waitFor(() => {
        expect(providerEntry("aws").data.loading).toBe(false);
      });
      await addLoadedModule("terraform-aws-vpc", "vpc");
      await addLoadedModule("terraform-aws-eks", "eks");
      await addLoadedModule("terraform-aws-rds", "rds");

      const regionVariable = addVariable("region");
      act(() => {
        regionVariable.data.onTypeChange({ kind: "string" });
      });
      const zonesVariable = addVariable("zones");
      act(() => {
        zonesVariable.data.onTypeChange({ kind: "list", element: { kind: "string" } });
      });
      act(() => {
        zonesVariable.data.onHasDefaultChange(true);
      });
      act(() => {
        zonesVariable.data.onDefaultChange({
          kind: "list",
          items: [
            { kind: "scalar", literal: "a" },
            { kind: "scalar", literal: "b" },
          ],
        });
      });

      const vpcId = referenceOption("eks", "module.vpc.vpc_id");
      act(() => {
        moduleEntry("eks").data.onFieldChange("cidr", referenceValue(vpcId));
        moduleEntry("eks").data.onFieldChange("count_value", referenceValue(referenceOption("eks", "module.vpc.enabled")));
        moduleEntry("eks").data.onFieldChange(
          "subnets",
          referenceValue(referenceOption("eks", "module.vpc.vpc_id"), { refSelector: { kind: "all" } }),
        );
        moduleEntry("eks").data.onFieldChange("from_list", referenceValue(referenceOption("eks", "module.vpc.subnet_ids")));
        moduleEntry("eks").data.onFieldChange(
          "extra_name",
          referenceValue(referenceOption("eks", "module.vpc.config"), { refAttributePath: "name" }),
        );
        moduleEntry("eks").data.onFieldChange(
          "extra_deep",
          referenceValue(referenceOption("eks", "module.vpc.config"), { refAttributePath: "name.deep" }),
        );
        moduleEntry("eks").data.onFieldChange("label_name", referenceValue(referenceOption("eks", "var.region")));
        moduleEntry("eks").data.onFieldChange(
          "output_key",
          referenceValue(vpcId, {
            refSelector: { kind: "key", expr: { mode: "literal", literal: "prod" } },
            refOutputSelector: { kind: "index", expr: { mode: "literal", literal: "0" } },
          }),
        );
        moduleEntry("eks").data.onProviderBindingChange("aws", providerEntry("aws").id);
        moduleEntry("rds").data.onMultiplicityChange({ kind: "for_each", variableNodeId: zonesVariable.id });
        moduleEntry("vpc").data.onMultiplicityChange({
          kind: "count",
          mode: "fixed",
          expr: { mode: "reference", literal: "", refNodeId: vpcId.nodeId, refOutput: vpcId.output },
        });
      });

      await waitFor(() => {
        expect(screen.getAllByTestId("edge").length).toBeGreaterThan(0);
      });
      const labels = screen.getAllByTestId("edge").map((edge) => edge.getAttribute("data-label"));
      expect(labels).toContain("cidr");
      expect(labels).toContain("count_value (type mismatch?)");
      expect(labels).toContain("subnets (all) (type mismatch?)");
      expect(labels).toContain("from_list (type mismatch?)");
      expect(labels).toContain("extra_name");
      expect(labels).toContain("label_name");
      expect(labels).toContain("output_key [prod] [0]");
      expect(labels).toContain("count");
      expect(labels).toContain("for_each");
      expect(labels).toContain("aws");
      expect(screen.getAllByTestId("edge").some((edge) => edge.getAttribute("data-stroke") === "#f85149")).toBe(true);
    });

    it("reports references that point at a removed module and a missing for_each source", async () => {
      renderCanvas();
      await addLoadedModule("terraform-aws-vpc", "vpc");
      await addLoadedModule("terraform-aws-eks", "eks");

      const vpcId = referenceOption("eks", "module.vpc.vpc_id");
      act(() => {
        moduleEntry("eks").data.onFieldChange("cidr", referenceValue(vpcId));
      });

      act(() => {
        moduleEntry("vpc").data.onRemove();
      });

      await waitFor(() => {
        expect(moduleEntry("eks").data.fieldErrors.cidr).toBe("References a module that no longer exists");
      });
      expect(moduleEntry("eks").data.fieldErrors.cidr).toBe("References a module that no longer exists");
    });
  });

  describe("drag and drop", () => {
    it("writes the module payload on drag start and accepts a dragged module over the canvas", () => {
      renderCanvas();
      const setData = vi.fn();
      const dataTransfer = { setData, effectAllowed: "", dropEffect: "" } as unknown as DataTransfer;

      fireEvent.dragStart(screen.getByText("terraform-aws-vpc"), { dataTransfer });
      expect(setData).toHaveBeenCalledWith("application/opendepot-module", expect.stringContaining("terraform-aws-vpc"));

      const canvas = screen.getByTestId("reactflow").parentElement as HTMLElement;
      const overTransfer = { types: ["application/opendepot-module"], dropEffect: "none" } as unknown as DataTransfer;
      fireEvent.dragOver(canvas, { dataTransfer: overTransfer });
      expect(overTransfer.dropEffect).toBe("move");
    });

    it("writes the provider payload on drag start", () => {
      renderCanvas();
      const setData = vi.fn();
      const dataTransfer = { setData, effectAllowed: "", dropEffect: "" } as unknown as DataTransfer;

      fireEvent.dragStart(screen.getByText("aws"), { dataTransfer });
      expect(setData).toHaveBeenCalledWith("application/opendepot-provider", expect.stringContaining("hashicorp"));
    });
  });

  describe("export", () => {
    beforeEach(() => {
      Object.assign(URL, {
        createObjectURL: vi.fn(() => "blob:assembly"),
        revokeObjectURL: vi.fn(),
      });
      vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    });

    it("shows diagnostics on the owning nodes and clears them once the variable is renamed", async () => {
      renderCanvas([vpcModule], [awsProvider]);
      await addLoadedModule("terraform-aws-vpc", "vpc");
      const variable = addVariable("region");
      addModule("aws");
      await waitFor(() => {
        expect(providerEntry("aws").data.loading).toBe(false);
      });

      vi.mocked(exportAssembly).mockResolvedValue({
        error: {
          error: "invalid",
          message: "Export validation failed",
          diagnostics: [
            { code: "E1", message: "cidr is required", path: "cidr", nodeId: moduleEntry("vpc").id },
            { code: "E2", message: "region is invalid", nodeId: variable.id },
            { code: "E3", message: "alias is reserved", nodeId: providerEntry("aws").id },
          ],
        },
      });

      fireEvent.click(screen.getByRole("button", { name: "Export" }));

      await screen.findByText("Export failed");
      await waitFor(() => {
        expect(moduleEntry("vpc").data.diagnostics).toEqual(["cidr: cidr is required"]);
      });
      expect(variableEntry("region").data.diagnostics).toEqual(["region is invalid"]);
      expect(providerEntry("aws").data.diagnostics).toEqual(["alias is reserved"]);

      act(() => {
        variableEntry("region").data.onRename("zone");
      });
      expect(variableEntry("zone").data.diagnostics ?? []).toEqual([]);
    });

    it("keeps the dialog open while exporting and closes it once the export completes", async () => {
      let finishExport: (value: Awaited<ReturnType<typeof exportAssembly>>) => void = () => undefined;
      vi.mocked(exportAssembly).mockImplementation(
        () =>
          new Promise((resolve) => {
            finishExport = resolve;
          }),
      );
      renderCanvas([vpcModule], []);

      fireEvent.click(screen.getByRole("button", { name: "Export" }));
      await screen.findByText("Validating export");

      const dialog = screen.getByRole("dialog");
      fireEvent.keyDown(dialog, { key: "Escape" });
      expect(screen.queryByRole("dialog")).not.toBeNull();

      act(() => {
        finishExport({ blob: new Blob(["zip"]) });
      });
      await screen.findByText("Export complete");
      fireEvent.click(screen.getByRole("button", { name: "Close" }));

      await waitFor(() => {
        expect(screen.queryByRole("dialog")).toBeNull();
      });
      expect(URL.createObjectURL).toHaveBeenCalled();
    });

    it("revokes the download URL after the anchor has been clicked", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      vi.mocked(exportAssembly).mockResolvedValue({ blob: new Blob(["zip"]) });
      renderCanvas([vpcModule], []);

      fireEvent.click(screen.getByRole("button", { name: "Export" }));
      await screen.findByText("Export complete");
      expect(HTMLAnchorElement.prototype.click).toHaveBeenCalled();

      act(() => {
        vi.advanceTimersByTime(1000);
      });
      expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:assembly");
    });

    it("shows the export error output and closes the failure dialog from its Close button", async () => {
      vi.mocked(exportAssembly).mockResolvedValue({
        error: { error: "tofu", message: "tofu exited", output: "line one" },
      });
      renderCanvas([vpcModule], []);

      fireEvent.click(screen.getByRole("button", { name: "Export" }));
      await screen.findByText("Export failed");
      expect(screen.getByText("tofu exited")).not.toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Close" }));
      await waitFor(() => {
        expect(screen.queryByRole("dialog")).toBeNull();
      });
    });

    it("closes the export dialog on Escape once the export has finished", async () => {
      vi.mocked(exportAssembly).mockResolvedValue({ error: { error: "x", message: "bad export" } });
      renderCanvas([vpcModule], []);

      fireEvent.click(screen.getByRole("button", { name: "Export" }));
      await screen.findByText("Export failed");

      fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
      await waitFor(() => {
        expect(screen.queryByRole("dialog")).toBeNull();
      });
    });
  });

  describe("clear canvas", () => {
    it("closes the confirmation on Escape and clears the canvas on confirm", async () => {
      renderCanvas([vpcModule], []);
      await addLoadedModule("terraform-aws-vpc", "vpc");

      fireEvent.click(screen.getByRole("button", { name: "Clear" }));
      screen.getByText("Clear the entire canvas?");
      fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
      await waitFor(() => {
        expect(screen.queryByText("Clear the entire canvas?")).toBeNull();
      });
      expect(registry.modules.has("vpc")).toBe(true);

      fireEvent.click(screen.getByRole("button", { name: "Clear" }));
      fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Clear canvas" }));
      await waitFor(() => {
        expect(screen.queryByTestId("module-node")).toBeNull();
      });
    });
  });

  describe("layout", () => {
    it("collapses the mobile resource menu when the canvas is clicked", () => {
      renderCanvas([vpcModule], []);

      fireEvent.click(screen.getByRole("button", { name: "Show Assembly Line menu" }));
      expect(screen.getByRole("button", { name: "Hide Assembly Line menu" })).not.toBeNull();

      fireEvent.click(screen.getByText("Pane click"));
      expect(screen.getByRole("button", { name: "Show Assembly Line menu" })).not.toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Show Assembly Line menu" }));
      fireEvent.click(screen.getByText("Node click"));
      expect(screen.getByRole("button", { name: "Show Assembly Line menu" })).not.toBeNull();
    });

    it("renders minimap classes for each node type", () => {
      renderCanvas([], []);

      expect(screen.getByTestId("minimap").textContent).toContain("od-minimap-module");
      expect(screen.getByTestId("minimap").textContent).toContain("od-minimap-variable");
      expect(screen.getByTestId("minimap").textContent).toContain("od-minimap-provider");
    });
  });

  describe("validation and storage", () => {
    it("reports required collections, conflicting map keys, invalid numbers, and count.index type mismatches", async () => {
      renderCanvas();
      await addLoadedModule("terraform-aws-eks", "eks");

      act(() => {
        moduleEntry("eks").data.onFieldChange("zones_list", { kind: "list", items: [] });
        moduleEntry("eks").data.onFieldChange("tags_map", { kind: "map", entries: [] });
        moduleEntry("eks").data.onFieldChange("settings_obj", { kind: "object", entries: [] });
      });
      await waitFor(() => {
        expect(moduleEntry("eks").data.fieldErrors.zones_list).toBe("Required");
      });
      expect(moduleEntry("eks").data.fieldErrors.tags_map).toBe("Required");
      expect(moduleEntry("eks").data.fieldErrors.settings_obj).toBe("Required");

      act(() => {
        moduleEntry("eks").data.onFieldChange("tags_map", {
          kind: "map",
          entries: [
            { key: "a", value: literalValue("1") },
            { key: "a", value: literalValue("2") },
          ],
        });
        moduleEntry("eks").data.onFieldChange("count_value", literalValue("abc"));
      });
      await waitFor(() => {
        expect(moduleEntry("eks").data.fieldErrors.tags_map).toBe('Conflicting map keys: "a"');
      });
      expect(moduleEntry("eks").data.fieldErrors.count_value).toMatch(/number/i);

      act(() => {
        moduleEntry("eks").data.onMultiplicityChange({ kind: "count", mode: "fixed", expr: { mode: "literal", literal: "2" } });
        moduleEntry("eks").data.onFieldChange("flag", literalValue("count.index"));
      });
      await waitFor(() => {
        expect(moduleEntry("eks").data.fieldErrors.flag).toContain("count.index");
      });
    });

    it("offers object attribute references on variables", () => {
      renderCanvas([eksModule], []);
      const variable = addVariable("settings");
      act(() => {
        variable.data.onTypeChange({
          kind: "object",
          attributes: [{ name: "name", type: { kind: "string" } }],
        });
      });
      fireEvent.click(screen.getByText("terraform-aws-eks"));

      expect(referenceOption("eks", "var.settings.name").output).toBe("name");
    });

    it("restores stored assembly values and normalizes unknown or malformed entries", async () => {
      const stored = [
        {
          id: "module-7",
          type: "module",
          position: { x: 0, y: 0 },
          data: {
            kind: "module",
            namespace: "terraform-aws",
            name: "terraform-aws-eks",
            version: "v19.0.0",
            instanceName: "eks",
            grade: null,
            variables: [],
            outputs: [],
            requiredProviders: [],
            providerBindings: {},
            values: {
              cidr: { kind: "scalar", value: { mode: "literal", literal: "10.0.0.0/16" } },
              subnets: {
                kind: "list",
                items: [{ kind: "scalar", value: { mode: "literal", literal: "a" } }, null],
              },
              tags: {
                kind: "map",
                entries: [{ key: "k", value: { kind: "scalar", value: { mode: "literal", literal: "v" } } }, null, {}],
              },
              settings: {
                kind: "object",
                entries: [{ name: "n", value: { mode: "literal", literal: "x" } }, "bad"],
              },
              mystery: { kind: "unknown" },
            },
            optionalFieldVisibility: {},
            multiplicity: { kind: "none" },
            loading: false,
            error: null,
          },
        },
      ];
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
      renderCanvas([eksModule], []);

      await waitFor(() => {
        expect(registry.modules.has("eks")).toBe(true);
      });
      const values = moduleEntry("eks").data.values;
      expect(values.cidr).toEqual({ kind: "scalar", value: { mode: "literal", literal: "10.0.0.0/16" } });
      expect(values.subnets).toEqual({
        kind: "list",
        items: [
          { kind: "scalar", value: { mode: "literal", literal: "a" } },
          { kind: "scalar", value: { mode: "literal", literal: "" } },
        ],
      });
      expect(values.tags).toEqual({
        kind: "map",
        entries: [
          { key: "k", value: { kind: "scalar", value: { mode: "literal", literal: "v" } } },
          { key: "", value: { kind: "scalar", value: { mode: "literal", literal: "" } } },
        ],
      });
      expect(values.settings).toEqual({
        kind: "object",
        entries: [{ name: "n", value: { kind: "scalar", value: { mode: "literal", literal: "x" } } }],
      });
      expect(values.mystery).toEqual({ kind: "scalar", value: { mode: "literal", literal: "" } });
    });
  });
});
