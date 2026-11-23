// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ContractVariable } from "@/lib/api";
import theme from "@/theme";
import InputsModal from "./InputsModal";
import type { ModuleInputValue, Multiplicity, ReferenceOption, VariableOption } from "./types";

// Vitest compiles JSX with the classic runtime, but production files such as DepotKindIcon rely on the automatic
// runtime that Next.js uses. Exposing React globally lets those files render under test without changing config.
Object.assign(globalThis, { React });

const networkOutput: ReferenceOption = {
  nodeId: "module-network",
  instanceName: "network",
  output: "id",
  type: "string",
  label: "module.network.id",
  sourceMultiplicity: "none",
  sourceKind: "module",
};

const collectionOutput: ReferenceOption = {
  nodeId: "module-subnets",
  instanceName: "subnets",
  output: "ids",
  type: ["list", "string"],
  label: "module.subnets.ids",
  sourceMultiplicity: "count",
  sourceKind: "module",
};

const noMultiplicity: Multiplicity = { kind: "none" };

function variable(name: string, overrides: Partial<ContractVariable> = {}): ContractVariable {
  return { name, type: "string", required: false, ...overrides };
}

function makeVariables(optionalCount: number): ContractVariable[] {
  const optional = Array.from({ length: optionalCount }, (_, i) => variable(`opt_${String(i + 1).padStart(2, "0")}`));
  return [variable("region", { required: true, description: "The `region` to deploy into" }), ...optional];
}

function makeProps(overrides: Partial<React.ComponentProps<typeof InputsModal>> = {}): React.ComponentProps<typeof InputsModal> {
  return {
    open: true,
    onClose: vi.fn(),
    instanceName: "vpc",
    namespace: "terraform-aws",
    moduleName: "terraform-aws-vpc",
    version: "5.0.0",
    multiplicity: noMultiplicity,
    variableOptions: [] as VariableOption[],
    providerBindings: {},
    providerOptions: [],
    variables: [variable("region", { required: true })],
    values: {},
    optionalFieldVisibility: {},
    fieldErrors: {},
    referenceOptions: [],
    onFieldChange: vi.fn(),
    onOptionalFieldVisibilityChange: vi.fn(),
    ...overrides,
  };
}

function renderModal(overrides: Partial<React.ComponentProps<typeof InputsModal>> = {}) {
  const props = makeProps(overrides);
  const result = render(
    <ThemeProvider theme={theme}>
      <InputsModal {...props} />
    </ThemeProvider>,
  );
  return { ...result, props };
}

function selectOption(label: RegExp | string, option: string) {
  fireEvent.mouseDown(screen.getByRole("combobox", { name: label }));
  fireEvent.click(screen.getByRole("option", { name: option }));
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

describe("InputsModal header and summary", () => {
  it("renders the instance title and the input count", () => {
    renderModal({ variables: makeVariables(0) });

    expect(screen.getByRole("heading", { name: "module.vpc — Inputs" })).toBeTruthy();
    expect(screen.getByText("1 input")).toBeTruthy();
  });

  it("uses the plural input count and notes how many fields need attention", () => {
    renderModal({
      variables: makeVariables(2),
      fieldErrors: { region: "Region is required", opt_01: "Bad value" },
    });

    expect(screen.getByText("3 inputs · 2 need attention")).toBeTruthy();
  });

  it("uses the singular attention label for one error", () => {
    renderModal({ variables: makeVariables(1), fieldErrors: { region: "Region is required" } });

    expect(screen.getByText("2 inputs · 1 needs attention")).toBeTruthy();
  });

  it("tells the user when the module has no inputs", () => {
    renderModal({ variables: [] });

    expect(screen.getByText("This module has no inputs.")).toBeTruthy();
  });

  it("calls onClose from the close icon and the Done button", () => {
    const { props } = renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    expect(props.onClose).toHaveBeenCalledTimes(2);
  });
});

describe("InputsModal grouping and labels", () => {
  it("labels required and optional inputs and groups them into sections", () => {
    renderModal({ variables: [variable("zone"), variable("region", { required: true })] });

    expect(screen.getByText("Required")).toBeTruthy();
    expect(screen.getByText("Optional")).toBeTruthy();
    expect(screen.getByText("required")).toBeTruthy();
    expect(screen.getByText("optional")).toBeTruthy();
  });

  it("floats provided inputs above required and optional ones", () => {
    const values: Record<string, ModuleInputValue> = {
      zone: { kind: "scalar", value: { mode: "literal", literal: "us-west-2a" } },
    };
    renderModal({
      variables: [variable("region", { required: true }), variable("zone")],
      values,
    });

    const headings = screen.getAllByText(/^(Provided|Required|Optional)$/).map((node) => node.textContent);
    expect(headings).toEqual(["Provided", "Required"]);
  });

  it("renders the markdown description of a variable", () => {
    renderModal({ variables: makeVariables(0) });

    expect(document.querySelector("code")?.textContent).toBe("region");
    expect(document.querySelector("code")?.parentElement?.textContent).toBe("The region to deploy into");
  });

  it("renders a validation error next to the field", () => {
    renderModal({
      variables: [variable("region", { required: true })],
      fieldErrors: { region: "Region must be set" },
    });

    expect(screen.getByText("Region must be set")).toBeTruthy();
  });
});

describe("InputsModal search and paging", () => {
  it("filters inputs by name and reports the matched count", () => {
    renderModal({ variables: makeVariables(3) });

    fireEvent.change(screen.getByPlaceholderText("Search inputs…"), { target: { value: "opt_02" } });

    expect(screen.getByText("1 of 4 inputs")).toBeTruthy();
    expect(screen.queryByText("opt_01")).toBeNull();
  });

  it("filters inputs by description", () => {
    renderModal({ variables: makeVariables(3) });

    fireEvent.change(screen.getByPlaceholderText("Search inputs…"), { target: { value: "deploy into" } });

    expect(screen.getByText("1 of 4 inputs")).toBeTruthy();
  });

  it("shows an empty-search message when nothing matches", () => {
    renderModal({ variables: makeVariables(2) });

    fireEvent.change(screen.getByPlaceholderText("Search inputs…"), { target: { value: "no-such-input" } });

    expect(screen.getByText("No inputs match your search.")).toBeTruthy();
  });

  it("pages through inputs with the previous and next buttons", () => {
    renderModal({ variables: makeVariables(11) });

    expect(screen.getByRole("button", { name: "Previous page" }).hasAttribute("disabled")).toBe(true);
    expect(screen.queryByText("opt_11")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    expect(screen.getByText("opt_11")).toBeTruthy();
    expect(screen.queryByText("opt_01")).toBeNull();
    expect(screen.getByRole("button", { name: "Next page" }).hasAttribute("disabled")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));

    expect(screen.getByText("opt_01")).toBeTruthy();
  });

  it("jumps to a page from the page selector", () => {
    renderModal({ variables: makeVariables(11) });

    selectOption("Page", "2");

    expect(screen.getByText("opt_11")).toBeTruthy();
  });

  it("changes the page size and recounts pages", () => {
    renderModal({ variables: makeVariables(11) });

    expect(screen.getByText("of 2")).toBeTruthy();

    selectOption("Per page", "5");

    expect(screen.getByText("of 3")).toBeTruthy();
  });

  it("resets to the first page and clears the search when reopened", () => {
    const props = makeProps({ variables: makeVariables(11) });
    const { rerender } = render(
      <ThemeProvider theme={theme}>
        <InputsModal {...props} />
      </ThemeProvider>,
    );

    fireEvent.change(screen.getByPlaceholderText("Search inputs…"), { target: { value: "opt" } });
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    rerender(
      <ThemeProvider theme={theme}>
        <InputsModal {...props} open={false} />
      </ThemeProvider>,
    );
    rerender(
      <ThemeProvider theme={theme}>
        <InputsModal {...props} open />
      </ThemeProvider>,
    );

    expect((screen.getByPlaceholderText("Search inputs…") as HTMLInputElement).value).toBe("");
    expect(screen.getByText("opt_01")).toBeTruthy();
  });
});

describe("InputsModal field editing", () => {
  it("forwards a fallback-type value edit as a scalar literal", () => {
    const { props } = renderModal({
      variables: [variable("odd", { type: ["foo", "string"] as unknown as ContractVariable["type"], default: "x" })],
    });

    fireEvent.change(screen.getByPlaceholderText('default: "x"'), { target: { value: "hello" } });

    expect(props.onFieldChange).toHaveBeenCalledWith("odd", { kind: "scalar", value: { mode: "literal", literal: "hello" } });
  });

  it("uses the generic placeholder when a fallback-type input has no default", () => {
    renderModal({
      variables: [variable("odd", { type: ["foo", "string"] as unknown as ContractVariable["type"] })],
    });

    expect(screen.getByPlaceholderText("value or module.…")).toBeTruthy();
  });

  it("forwards edits from a typed input editor", () => {
    const { props } = renderModal({ variables: [variable("region", { required: true })] });

    fireEvent.change(screen.getAllByLabelText("HCL value")[0], { target: { value: "eu-west-1" } });

    expect(props.onFieldChange).toHaveBeenCalledWith("region", expect.objectContaining({ kind: "scalar" }));
  });

  it("shows a reference selector for an input wired to another module output", () => {
    renderModal({
      variables: [variable("network_id", { required: true })],
      values: {
        network_id: { kind: "scalar", value: { mode: "reference", literal: "", refNodeId: "module-network", refOutput: "id" } },
      },
      referenceOptions: [networkOutput],
    });

    expect(screen.getAllByText(/module\.network\.id/).length).toBeGreaterThan(0);
  });

  it("keeps the collection selector beside a reference that picks a whole output", () => {
    renderModal({
      variables: [variable("subnet_ids", { required: true })],
      values: {
        subnet_ids: {
          kind: "scalar",
          value: {
            mode: "reference",
            literal: "",
            refNodeId: "module-subnets",
            refOutput: "ids",
            refOutputSelector: { kind: "all" },
          },
        },
      },
      referenceOptions: [collectionOutput],
    });

    expect(screen.getAllByText("subnet_ids").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/module\.subnets\.ids/).length).toBeGreaterThan(0);
  });

  it("renders a complex object input through the compact reference control", () => {
    renderModal({
      variables: [variable("tags", { required: false, type: ["object", { name: "string" }] })],
    });

    expect(screen.getByText("tags")).toBeTruthy();
  });
});

describe("InputsModal HCL preview", () => {
  it("renders the module preview with the instance name", () => {
    renderModal({ variables: [variable("region", { required: true })] });

    const preview = screen.getByLabelText("Module HCL preview");
    expect(preview.textContent).toContain("vpc");
  });

  it("includes the configured registry host in the preview", () => {
    vi.stubEnv("NEXT_PUBLIC_BASE_URL", "https://registry.example.com");
    renderModal({ variables: [variable("region", { required: true })] });

    expect(screen.getByLabelText("Module HCL preview").textContent).toContain("registry.example.com");
  });

  it("falls back to the raw base URL when it is not a valid URL", () => {
    vi.stubEnv("NEXT_PUBLIC_BASE_URL", "not-a-url");
    renderModal({ variables: [variable("region", { required: true })] });

    expect(screen.getByLabelText("Module HCL preview").textContent).toContain("not-a-url");
  });

  it("expands and restores the preview pane", () => {
    renderModal({ variables: [variable("region", { required: true })] });

    fireEvent.click(screen.getByRole("button", { name: "Expand HCL preview" }));

    const restore = screen.getByRole("button", { name: "Restore HCL preview" });
    expect(restore.getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(restore);

    expect(screen.getByRole("button", { name: "Expand HCL preview" }).getAttribute("aria-pressed")).toBe("false");
  });
});
