// @vitest-environment jsdom

import { fireEvent, render, screen, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import type { NodeProps } from "reactflow";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ModuleNode, { type ModuleNodeData } from "./ModuleNode";
import type { ContractOutput, ContractRequiredProvider, ContractVariable } from "@/lib/api";
import type { ReferenceOption, TypeSpec, VariableOption } from "./types";
import theme from "@/theme";

// Vitest compiles JSX with the classic runtime, but production files such as DepotKindIcon rely on the automatic
// runtime that Next.js uses. Exposing React globally lets those files render under test without changing config.
Object.assign(globalThis, { React });

vi.mock("reactflow", async (importOriginal) => {
  const actual = await importOriginal<typeof import("reactflow")>();

  return {
    ...actual,
    Handle: ({ id }: { id?: string }) => <span data-testid="handle" data-handle-id={id} />,
  };
});

vi.mock("./InputsModal", () => ({
  default: (props: { open: boolean; metaOptions?: string[]; onClose: () => void; onFieldChange: (name: string, value: unknown) => void; onOptionalFieldVisibilityChange: (path: string, visible: boolean) => void }) => (
    <div data-testid="inputs-modal" data-open={String(props.open)}>
      <span>{(props.metaOptions ?? []).join(",")}</span>
      <button type="button" onClick={props.onClose}>
        close inputs
      </button>
      <button
        type="button"
        onClick={() => props.onFieldChange("region", { kind: "scalar", value: { mode: "literal", literal: "eu-west-1" } })}
      >
        change field
      </button>
      <button type="button" onClick={() => props.onOptionalFieldVisibilityChange("variable%3Aregion", true)}>
        show optional
      </button>
    </div>
  ),
}));

vi.mock("./ProvidersModal", () => ({
  default: (props: { open: boolean; onClose: () => void; onProviderBindingChange: (localName: string, providerNodeId: string) => void }) => (
    <div data-testid="providers-modal" data-open={String(props.open)}>
      <button type="button" onClick={props.onClose}>
        close providers
      </button>
      <button type="button" onClick={() => props.onProviderBindingChange("aws", "provider-aws")}>
        bind provider
      </button>
    </div>
  ),
}));

class ResizeObserverStub {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}

const networkOutput: ReferenceOption = {
  nodeId: "module-network",
  instanceName: "network",
  output: "id",
  type: "string",
  label: "module.network.id",
  sourceMultiplicity: "none",
  sourceKind: "module",
};

const mapOfObjectType: TypeSpec = {
  kind: "map",
  element: { kind: "object", attributes: [{ name: "size", type: { kind: "string" } }] },
};

const variableOptions: VariableOption[] = [
  { nodeId: "var-map", name: "buckets", type: mapOfObjectType },
  { nodeId: "var-bad", name: "scalar", type: { kind: "string" } },
];

function makeData(overrides: Partial<ModuleNodeData> = {}): ModuleNodeData {
  return {
    namespace: "terraform-aws",
    name: "terraform-aws-vpc",
    version: "5.0.0",
    instanceName: "vpc",
    instanceNameError: null,
    grade: "full",
    variables: [],
    outputs: [],
    requiredProviders: [],
    providerOptions: [],
    providerBindings: {},
    values: {},
    optionalFieldVisibility: {},
    fieldErrors: {},
    referenceOptions: [],
    variableOptions: [],
    multiplicity: { kind: "none" },
    loading: false,
    error: null,
    onRemove: vi.fn(),
    onRenameInstance: vi.fn(),
    onFieldChange: vi.fn(),
    onOptionalFieldVisibilityChange: vi.fn(),
    onMultiplicityChange: vi.fn(),
    onProviderBindingChange: vi.fn(),
    ...overrides,
  };
}

function nodeProps(data: ModuleNodeData) {
  return {
    id: "module-1",
    type: "module",
    xPos: 0,
    yPos: 0,
    selected: false,
    dragging: false,
    data,
  } as NodeProps<ModuleNodeData>;
}

function renderNode(data: ModuleNodeData) {
  return render(
    <ThemeProvider theme={theme}>
      <ModuleNode {...nodeProps(data)} />
    </ThemeProvider>,
  );
}

function variable(name: string, overrides: Partial<ContractVariable> = {}): ContractVariable {
  return { name, type: "string", required: false, ...overrides };
}

function output(name: string): ContractOutput {
  return { name, type: "string", confidence: "exact" };
}

function openRepeatPopover() {
  fireEvent.click(screen.getByRole("button", { name: /Repeat this module|not bool/ }));
}

function selectOption(label: RegExp | string, option: string) {
  fireEvent.mouseDown(screen.getByRole("combobox", { name: label }));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", ResizeObserverStub);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ModuleNode identity", () => {
  it("renders the module identity with a single leading v and links to module details", () => {
    renderNode(makeData({ version: "5.0.0" }));

    const link = screen.getByRole("link", { name: "Open terraform-aws/terraform-aws-vpc module details" });
    expect(link.getAttribute("href")).toBe("/terraform-aws/module/terraform-aws-vpc");
    expect(link.textContent).toContain("terraform-aws/terraform-aws-vpc · v5.0.0");
  });

  it("does not double the v prefix when the version already has one", () => {
    renderNode(makeData({ version: "v19.0.0" }));

    expect(screen.getByText("terraform-aws/terraform-aws-vpc · v19.0.0")).toBeTruthy();
  });

  it("shows the full identity in a tooltip only when the identity text is truncated", async () => {
    vi.spyOn(Element.prototype, "scrollWidth", "get").mockReturnValue(400);
    vi.spyOn(Element.prototype, "clientWidth", "get").mockReturnValue(120);
    renderNode(makeData());

    fireEvent.mouseOver(screen.getByRole("link", { name: "Open terraform-aws/terraform-aws-vpc module details" }));
    expect((await screen.findByRole("tooltip")).textContent).toBe("terraform-aws/terraform-aws-vpc · v5.0.0");
  });

  it("does not show an identity tooltip when the text fits", () => {
    renderNode(makeData());

    fireEvent.mouseOver(screen.getByRole("link", { name: "Open terraform-aws/terraform-aws-vpc module details" }));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("renders the grade chip only when a grade is known", () => {
    const { unmount } = renderNode(makeData({ grade: "partial" }));
    expect(screen.getByText("partial")).toBeTruthy();
    unmount();

    renderNode(makeData({ grade: null }));
    expect(screen.queryByText("full")).toBeNull();
    expect(screen.queryByText("partial")).toBeNull();
  });
});

describe("ModuleNode instance name and removal", () => {
  it("passes instance renames through onRenameInstance", () => {
    const data = makeData();
    renderNode(data);

    fireEvent.change(screen.getByDisplayValue("vpc"), { target: { value: "network" } });
    expect(data.onRenameInstance).toHaveBeenCalledWith("network");
  });

  it("shows the instance name error", () => {
    renderNode(makeData({ instanceNameError: "must start with a letter" }));

    expect(screen.getByText("must start with a letter")).toBeTruthy();
  });

  it("calls onRemove from the remove button", () => {
    const data = makeData();
    renderNode(data);

    fireEvent.click(screen.getByRole("button", { name: "Remove vpc" }));
    expect(data.onRemove).toHaveBeenCalledTimes(1);
  });
});

describe("ModuleNode repeat (count / for_each) popover", () => {
  it("labels a single module and opens the repeat popover from the chip", () => {
    renderNode(makeData());

    openRepeatPopover();
    expect(screen.getByRole("combobox", { name: /Repeat/ })).toBeTruthy();
  });

  it("changes the repeat kind to none, count, and for_each", () => {
    const data = makeData({ multiplicity: { kind: "count", mode: "fixed", expr: { mode: "literal", literal: "2" } } });
    renderNode(data);

    openRepeatPopover();
    selectOption(/Repeat/, "Single (default)");
    expect(data.onMultiplicityChange).toHaveBeenLastCalledWith({ kind: "none" });
  });

  it("switches a single module to count with a fixed expression of 1", () => {
    const data = makeData();
    renderNode(data);

    openRepeatPopover();
    selectOption(/Repeat/, "count");
    expect(data.onMultiplicityChange).toHaveBeenLastCalledWith({
      kind: "count",
      mode: "fixed",
      expr: { mode: "literal", literal: "1" },
    });
  });

  it("switches a single module to for_each with no variable picked", () => {
    const data = makeData();
    renderNode(data);

    openRepeatPopover();
    selectOption(/Repeat/, "for_each");
    expect(data.onMultiplicityChange).toHaveBeenLastCalledWith({ kind: "for_each", variableNodeId: null });
  });

  it("switches count between fixed and conditional modes", () => {
    const fixed = makeData({ multiplicity: { kind: "count", mode: "fixed", expr: { mode: "literal", literal: "3" } } });
    const { unmount } = renderNode(fixed);

    openRepeatPopover();
    selectOption(/Mode/, "Conditional (create or not)");
    expect(fixed.onMultiplicityChange).toHaveBeenLastCalledWith({
      kind: "count",
      mode: "conditional",
      expr: { mode: "literal", literal: "" },
    });
    unmount();

    const conditional = makeData({ multiplicity: { kind: "count", mode: "conditional", expr: { mode: "literal", literal: "true" } } });
    renderNode(conditional);

    openRepeatPopover();
    selectOption(/Mode/, "Fixed number");
    expect(conditional.onMultiplicityChange).toHaveBeenLastCalledWith({
      kind: "count",
      mode: "fixed",
      expr: { mode: "literal", literal: "1" },
    });
  });

  it("forwards count expression edits from the field editor", () => {
    const data = makeData({ multiplicity: { kind: "count", mode: "fixed", expr: { mode: "literal", literal: "3" } } });
    renderNode(data);

    openRepeatPopover();
    fireEvent.change(screen.getByPlaceholderText("e.g. 3"), { target: { value: "5" } });
    expect(data.onMultiplicityChange).toHaveBeenLastCalledWith({
      kind: "count",
      mode: "fixed",
      expr: { mode: "literal", literal: "5" },
    });
  });

  it("flags a conditional count wired to a non-bool reference on the badge and the popover", async () => {
    renderNode(makeData({
      multiplicity: { kind: "count", mode: "conditional", expr: { mode: "reference", literal: "", refNodeId: "module-network", refOutput: "id" } },
      referenceOptions: [networkOutput],
    }));

    fireEvent.mouseOver(screen.getByRole("button", { name: "module.network.id is string, not bool" }));
    expect((await screen.findByRole("tooltip")).textContent).toBe("module.network.id is string, not bool");
  });

  it("warns when a count expression references another module's output", () => {
    renderNode(makeData({
      multiplicity: { kind: "count", mode: "fixed", expr: { mode: "reference", literal: "", refNodeId: "module-network", refOutput: "id" } },
      referenceOptions: [networkOutput],
    }));

    openRepeatPopover();
    expect(screen.getByText(/A count condition needs to know its length at plan time/)).toBeTruthy();
  });

  it("does not flag a conditional count whose reference is bool-compatible or unresolved", () => {
    renderNode(makeData({
      multiplicity: { kind: "count", mode: "conditional", expr: { mode: "reference", literal: "", refNodeId: "missing", refOutput: "" } },
      referenceOptions: [networkOutput],
    }));

    expect(screen.queryByRole("button", { name: /not bool/ })).toBeNull();
    expect(screen.getByRole("button", { name: "Repeat this module with count or for_each" }).className).not.toContain("MuiChip-colorError");
  });

  it("prompts for a variable when for_each has none available", () => {
    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: null } }));

    openRepeatPopover();
    expect(screen.getByText(/Add a Variable first/)).toBeTruthy();
  });

  it("lists variables for for_each, disabling incompatible ones, and emits the selected variable", () => {
    const data = makeData({ multiplicity: { kind: "for_each", variableNodeId: null }, variableOptions });
    renderNode(data);

    openRepeatPopover();
    fireEvent.mouseDown(screen.getByRole("combobox", { name: /Variable/ }));
    expect(screen.getByRole("option", { name: "var.scalar (not a list/set/map)" }).getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(screen.getByRole("option", { name: "var.buckets" }));
    expect(data.onMultiplicityChange).toHaveBeenLastCalledWith({ kind: "for_each", variableNodeId: "var-map" });
  });

  it("asks for a variable pick before deriving each.key and each.value", () => {
    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: null }, variableOptions }));

    openRepeatPopover();
    expect(screen.getByText("Pick a variable to derive each.key / each.value.")).toBeTruthy();
  });

  it.each([
    { nodeId: "var-map", expected: /each\.value is the object/ },
    { nodeId: "var-set", expected: /each\.key and each\.value will both be the element value/ },
    { nodeId: "var-scalar-map", expected: /each\.key is the map key; each\.value is the map value/ },
    { nodeId: "var-unknown", expected: /isn't list\/set\/map/ },
  ])("describes the for_each shape for $nodeId", ({ nodeId, expected }) => {
    const shapeOptions: VariableOption[] = [
      { nodeId: "var-map", name: "buckets", type: mapOfObjectType },
      { nodeId: "var-set", name: "names", type: { kind: "set", element: { kind: "string" } } },
      { nodeId: "var-scalar-map", name: "tags", type: { kind: "map", element: { kind: "string" } } },
      { nodeId: "var-unknown", name: "flag", type: { kind: "string" } },
    ];

    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: nodeId }, variableOptions: shapeOptions }));

    openRepeatPopover();
    expect(screen.getByText(expected)).toBeTruthy();
  });

  it("hands for_each meta options to the inputs modal", () => {
    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: "var-map" }, variableOptions }));

    expect(within(screen.getByTestId("inputs-modal")).getByText(/each\.value\.size/)).toBeTruthy();
  });

  it("offers count.index as the only meta option for count", () => {
    renderNode(makeData({ multiplicity: { kind: "count", mode: "fixed", expr: { mode: "literal", literal: "2" } } }));

    expect(within(screen.getByTestId("inputs-modal")).getByText("count.index")).toBeTruthy();
  });

  it("offers each.key and each.value when the for_each variable is not known", () => {
    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: "missing" }, variableOptions }));

    expect(within(screen.getByTestId("inputs-modal")).getByText("each.key,each.value")).toBeTruthy();
  });
});

describe("ModuleNode inputs and providers", () => {
  it("opens the inputs modal from the inputs row by click and keyboard", () => {
    renderNode(makeData({ variables: [variable("region")] }));

    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("false");
    fireEvent.click(screen.getByRole("button", { name: /Inputs \(1\)/ }));
    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "close inputs" }));
    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("false");

    fireEvent.keyDown(screen.getByRole("button", { name: /Inputs \(1\)/ }), { key: "Enter" });
    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("true");
  });

  it("opens the inputs modal with the space key", () => {
    renderNode(makeData({ variables: [variable("region")] }));

    fireEvent.keyDown(screen.getByRole("button", { name: /Inputs \(1\)/ }), { key: " " });
    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("true");
  });

  it("ignores other keys on the inputs row", () => {
    renderNode(makeData({ variables: [variable("region")] }));

    fireEvent.keyDown(screen.getByRole("button", { name: /Inputs \(1\)/ }), { key: "a" });
    expect(screen.getByTestId("inputs-modal").getAttribute("data-open")).toBe("false");
  });

  it("shows a configured chip when every input is set and a fix count when inputs have errors", () => {
    const { unmount } = renderNode(makeData({ variables: [variable("region")] }));
    expect(screen.getByText("configured")).toBeTruthy();
    unmount();

    renderNode(makeData({
      variables: [variable("region")],
      fieldErrors: { region: "required", subnets: "bad" },
    }));
    expect(screen.getByText("2 to fix")).toBeTruthy();
    expect(screen.queryByText("configured")).toBeNull();
  });

  it("shows no input status chip when the module has no inputs", () => {
    renderNode(makeData());

    expect(screen.getByText("Inputs (0)")).toBeTruthy();
    expect(screen.queryByText("configured")).toBeNull();
  });

  it("hides the providers row when there are no required providers", () => {
    renderNode(makeData());

    expect(screen.queryByText(/Providers \(/)).toBeNull();
  });

  it("counts configured providers and opens the providers modal", () => {
    const required: ContractRequiredProvider[] = [
      { localName: "aws", source: "hashicorp/aws" },
      { localName: "random", source: "hashicorp/random" },
    ];
    const { unmount } = renderNode(makeData({
      requiredProviders: required,
      providerBindings: { aws: { providerNodeId: "provider-aws" } },
    }));

    expect(screen.getByText("1 configured")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Providers \(2\)/ }));
    expect(screen.getByTestId("providers-modal").getAttribute("data-open")).toBe("true");
    unmount();

    renderNode(makeData({
      requiredProviders: [{ localName: "aws", source: "hashicorp/aws" }],
      providerBindings: { aws: { providerNodeId: "provider-aws" } },
    }));
    expect(screen.getByText("configured")).toBeTruthy();
  });

  it("opens the providers modal from the keyboard and forwards close and binding changes", () => {
    const data = makeData({ requiredProviders: [{ localName: "aws", source: "hashicorp/aws" }] });
    renderNode(data);

    const row = screen.getByRole("button", { name: /Providers \(1\)/ });
    fireEvent.keyDown(row, { key: "Enter" });
    expect(screen.getByTestId("providers-modal").getAttribute("data-open")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "close providers" }));
    expect(screen.getByTestId("providers-modal").getAttribute("data-open")).toBe("false");

    fireEvent.keyDown(row, { key: " " });
    fireEvent.keyDown(row, { key: "x" });
    fireEvent.click(screen.getByRole("button", { name: "bind provider" }));
    expect(data.onProviderBindingChange).toHaveBeenCalledWith("aws", "provider-aws");
  });

  it("forwards field and visibility changes from the inputs modal", () => {
    const data = makeData({ variables: [variable("region")] });
    renderNode(data);

    fireEvent.click(screen.getByRole("button", { name: "change field" }));
    expect(data.onFieldChange).toHaveBeenCalledWith("region", { kind: "scalar", value: { mode: "literal", literal: "eu-west-1" } });

    fireEvent.click(screen.getByRole("button", { name: "show optional" }));
    expect(data.onOptionalFieldVisibilityChange).toHaveBeenCalledWith("variable%3Aregion", true);
  });
});

describe("ModuleNode outputs", () => {
  it("shows none when the module has no outputs", () => {
    renderNode(makeData());

    expect(screen.getByText("none")).toBeTruthy();
  });

  it("renders one source handle per output without a show-more toggle when five or fewer", () => {
    renderNode(makeData({ outputs: [output("id"), output("arn")] }));

    const handles = screen.getAllByTestId("handle").map((handle) => handle.getAttribute("data-handle-id"));
    expect(handles).toEqual(expect.arrayContaining(["out:id", "out:arn", "in"]));
    expect(screen.queryByText(/Show \d+ more/)).toBeNull();
  });

  it("collapses extra outputs behind a show-more toggle and expands them on click and keyboard", () => {
    const outputs = ["a", "b", "c", "d", "e", "f", "g"].map(output);
    renderNode(makeData({ outputs }));

    const toggle = screen.getByRole("button", { name: "Show 2 more" });
    expect(screen.getAllByTestId("handle").map((h) => h.getAttribute("data-handle-id"))).toContain("out:f");

    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Show less" })).toBeTruthy();

    fireEvent.keyDown(screen.getByRole("button", { name: "Show less" }), { key: "Enter" });
    expect(screen.getByRole("button", { name: "Show 2 more" })).toBeTruthy();

    fireEvent.keyDown(screen.getByRole("button", { name: "Show 2 more" }), { key: " " });
    expect(screen.getByRole("button", { name: "Show less" })).toBeTruthy();

    fireEvent.keyDown(screen.getByRole("button", { name: "Show less" }), { key: "z" });
    expect(screen.getByRole("button", { name: "Show less" })).toBeTruthy();
  });
});

describe("ModuleNode load, error, and diagnostics states", () => {
  it("shows a loading indicator and hides body sections while the contract loads", () => {
    renderNode(makeData({ loading: true, variables: [variable("region")], diagnostics: ["bad"] }));

    expect(screen.getByText("Loading contract…")).toBeTruthy();
    expect(screen.queryByText("bad")).toBeNull();
    expect(screen.queryByText(/Inputs \(/)).toBeNull();
  });

  it("shows the load error in place of the body", () => {
    renderNode(makeData({ error: "contract unavailable", variables: [variable("region")] }));

    expect(screen.getByText("contract unavailable")).toBeTruthy();
    expect(screen.queryByText(/Inputs \(/)).toBeNull();
  });

  it("lists diagnostics in the body when the contract loaded cleanly", () => {
    renderNode(makeData({ diagnostics: ["first problem", "second problem"] }));

    expect(screen.getByText("first problem")).toBeTruthy();
    expect(screen.getByText("second problem")).toBeTruthy();
  });

  it("summarises a single warning in the singular", () => {
    renderNode(makeData({ warnings: ["count is unknown"] }));

    expect(screen.getByText("1 warning degrading this contract")).toBeTruthy();
  });

  it("summarises several warnings in the plural and shows the joined list on hover", async () => {
    renderNode(makeData({ warnings: ["first", "second"] }));

    expect(screen.getByText("2 warnings degrading this contract")).toBeTruthy();
    fireEvent.mouseOver(screen.getByText("2 warnings degrading this contract"));
    expect((await screen.findByRole("tooltip")).textContent).toBe("first second");
  });

  it("omits the warnings row when there are none", () => {
    renderNode(makeData({ warnings: [] }));

    expect(screen.queryByText(/warning/)).toBeNull();
  });

  it("uses a for_each shadow when the module is repeated over a variable", () => {
    renderNode(makeData({ multiplicity: { kind: "for_each", variableNodeId: "var-map" }, variableOptions }));

    expect(screen.getByText("for_each")).toBeTruthy();
  });
});
