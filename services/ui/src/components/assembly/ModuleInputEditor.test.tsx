// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ModuleInputValue, ReferenceOption, TypeSpec } from "./types";
import ModuleInputEditor from "./ModuleInputEditor";

const referenceOptions: ReferenceOption[] = [{
  nodeId: "module-network",
  instanceName: "network",
  output: "id",
  type: "string",
  label: "module.network.id",
  sourceMultiplicity: "none",
  sourceKind: "module",
}];
const complexReferenceOptions: ReferenceOption[] = [
  ...referenceOptions,
  {
    nodeId: "module-roles",
    instanceName: "roles_source",
    output: "roles",
    type: ["map", ["object", { name: "string" }]],
    label: "module.roles_source.roles",
    sourceMultiplicity: "none",
    sourceKind: "module",
  },
  {
    nodeId: "variable-lambdas",
    instanceName: "lambda_functions",
    output: "",
    type: ["map", ["object", { spec: ["object", { description: "string" }] }]],
    label: "var.lambda_functions",
    sourceMultiplicity: "none",
    sourceKind: "variable",
  },
  ...(["list", "set"] as const).map((kind): ReferenceOption => ({
    nodeId: `module-${kind}-source`,
    instanceName: `${kind}_source`,
    output: "items",
    type: [kind, "string"],
    label: `module.${kind}_source.items`,
    sourceMultiplicity: "none" as const,
    sourceKind: "module" as const,
  })),
];

function Harness({ type }: { type: TypeSpec }) {
  const [value, setValue] = React.useState<ModuleInputValue>({ kind: "map", entries: [] });
  return (
    <>
      <ModuleInputEditor type={type} value={value} referenceOptions={referenceOptions} onChange={setValue} />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </>
  );
}

describe("ModuleInputEditor", () => {
  it("uses compact expandable rows for nested map inputs", () => {
    render(
      <Harness
        type={{
          kind: "map",
          element: { kind: "object", attributes: [{ name: "name", type: { kind: "string" } }] },
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));
    const entry = screen.getByRole("button", { name: "New entry" });
    expect(entry.getAttribute("aria-expanded")).toBe("true");

    fireEvent.change(screen.getByLabelText("Key"), { target: { value: "primary" } });
    expect(screen.getByRole("button", { name: "primary" }).getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("name")).toBeTruthy();
    fireEvent.change(screen.getByRole("textbox", { name: "HCL value" }), { target: { value: "worker" } });
    expect(screen.getByTestId("value").textContent).toContain('"literal":"worker"');
    expect(screen.getByTestId("value").textContent).toContain('"key":"primary"');
  });

  it("places the map entry input chip beside the remove action", () => {
    render(
      <Harness
        type={{
          kind: "map",
          element: { kind: "object", attributes: [{ name: "name", type: { kind: "string" } }] },
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));

    const rowActions = screen.getByTestId("module-input-row-actions");
    const inputChip = rowActions.querySelector('[aria-label="Use input"]');
    const removeButton = rowActions.querySelector('[aria-label="Remove entry"]');
    expect(inputChip).toBeTruthy();
    expect(removeButton).toBeTruthy();
    expect(rowActions.contains(inputChip)).toBe(true);
    expect(rowActions.contains(removeButton)).toBe(true);
  });

  it("places nested object and list input chips on their owning rows", () => {
    const type: TypeSpec = {
      kind: "object",
      attributes: [{
        name: "custom_iam_policy",
        type: {
          kind: "list",
          element: {
            kind: "object",
            attributes: [
              { name: "actions", type: { kind: "list", element: { kind: "string" } } },
              { name: "resources", type: { kind: "list", element: { kind: "string" } } },
            ],
          },
        },
      }],
    };
    const value: ModuleInputValue = {
      kind: "object",
      entries: [{
        name: "custom_iam_policy",
        value: { kind: "list", items: [{ kind: "object", entries: [] }] },
      }],
    };

    render(
      <ModuleInputEditor
        type={type}
        value={value}
        referenceOptions={complexReferenceOptions}
        showReferenceControl={false}
        onChange={() => undefined}
      />,
    );

    const rowActions = screen.getAllByTestId("module-input-row-actions");
    expect(rowActions).toHaveLength(4);
    for (const actions of rowActions) {
      expect(actions.querySelector('[aria-label="Use input"]')).toBeTruthy();
    }
    expect(rowActions[1].querySelector('[aria-label="Remove item"]')).toBeTruthy();
  });

  it("highlights and accepts multiline HCL scalar values", () => {
    function ScalarHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({ kind: "scalar", value: { mode: "literal", literal: "" } });
      return (
        <>
          <ModuleInputEditor type={{ kind: "string" }} value={value} referenceOptions={referenceOptions} onChange={setValue} />
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      );
    }

    const { container } = render(<ScalarHarness />);
    const editor = screen.getByRole("textbox", { name: "HCL value" });
    const expression = '"${replace(each.key, "_", "-")}"';
    fireEvent.change(editor, { target: { value: expression } });

    expect(screen.getByTestId("value").textContent).toContain(JSON.stringify(expression));
    expect(container.querySelector(".token.function")?.textContent).toBe("replace");
    expect(container.querySelector(".token.variable")?.textContent).toBe("each.key");
  });

  it("shows a built-in function hint while the caret is inside the call", () => {
    function FunctionHintHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({
        kind: "scalar",
        value: { mode: "literal", literal: "" },
      });
      return <ModuleInputEditor type={{ kind: "string" }} value={value} referenceOptions={referenceOptions} onChange={setValue} />;
    }

    render(<FunctionHintHarness />);
    const editor = screen.getByRole("textbox", { name: "HCL value" }) as HTMLTextAreaElement;
    fireEvent.focus(editor);
    fireEvent.change(editor, { target: { value: "length(var.items)" } });
    editor.setSelectionRange(10, 10);
    fireEvent.select(editor);

    expect(screen.getByText(/Returns the number of elements/)).toBeTruthy();
    const docsLink = screen.getByRole("link", { name: "OpenTofu docs" });
    expect(docsLink.getAttribute("href"))
      .toBe("https://opentofu.org/docs/language/functions/length/");

    fireEvent.blur(editor, { relatedTarget: docsLink });
    expect(screen.getByRole("link", { name: "OpenTofu docs" })).toBeTruthy();

    fireEvent.blur(docsLink, { relatedTarget: document.body });
    expect(screen.queryByText(/Returns the number of elements/)).toBeNull();
  });

  it("keeps module output references selectable beside the code editor", () => {
    function ReferenceHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({ kind: "scalar", value: { mode: "literal", literal: "" } });
      return (
        <>
          <ModuleInputEditor type={{ kind: "string" }} value={value} referenceOptions={referenceOptions} onChange={setValue} />
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      );
    }

    render(<ReferenceHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Use input" }));
    fireEvent.mouseDown(screen.getByPlaceholderText("Search module outputs"));
    expect(screen.getByRole("listbox").closest(".MuiPopover-paper")).toBeTruthy();
    fireEvent.click(screen.getByRole("option", { name: "module.network.id" }));

    expect(screen.getByTestId("value").textContent).toContain('"mode":"reference"');
    expect(screen.getByTestId("value").textContent).toContain('"refNodeId":"module-network"');
  });

  it("can leave scalar reference selection to the enclosing input header", () => {
    render(
      <ModuleInputEditor
        type={{ kind: "string" }}
        value={{ kind: "scalar", value: { mode: "literal", literal: "" } }}
        referenceOptions={referenceOptions}
        referenceControlPlacement="header"
        onChange={() => undefined}
      />,
    );

    expect(screen.getByRole("textbox", { name: "HCL value" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Use input" })).toBeNull();
  });

  it("allows a complex map input to use and clear a whole-value reference", () => {
    function ComplexReferenceHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({
        kind: "map",
        entries: [{ key: "primary", value: { kind: "object", entries: [] } }],
      });
      return (
        <>
          <ModuleInputEditor
            type={{ kind: "map", element: { kind: "object", attributes: [{ name: "name", type: { kind: "string" } }] } }}
            value={value}
            referenceOptions={complexReferenceOptions}
            onChange={setValue}
          />
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      );
    }

    const { container } = render(<ComplexReferenceHarness />);
    fireEvent.click(screen.getAllByRole("button", { name: "Use input" })[0]);
    fireEvent.mouseDown(screen.getByPlaceholderText("Search module outputs"));
    fireEvent.click(screen.getByRole("option", { name: "module.roles_source.roles" }));

    expect(screen.getByTestId("value").textContent).toContain('"kind":"scalar"');
    expect(screen.getByTestId("value").textContent).toContain('"refNodeId":"module-roles"');
    expect(screen.queryByRole("button", { name: "Add entry" })).toBeNull();

    const selectedReference = screen.getByRole("button", { name: "Module output module.roles_source.roles" });
    const selectorLabel = screen.getByText("roles is a map —");
    expect(selectedReference.compareDocumentPosition(selectorLabel) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    const deleteIcon = container.querySelector(".MuiChip-deleteIcon");
    expect(deleteIcon).toBeTruthy();
    fireEvent.click(deleteIcon!);
    expect(screen.getByTestId("value").textContent).toBe('{"kind":"map","entries":[]}');
    expect(screen.getByRole("button", { name: "Add entry" })).toBeTruthy();
  });

  it.each(["list", "set"] as const)("places a selected %s chip before its selector label", (kind) => {
    function CollectionReferenceHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({ kind: "list", items: [] });
      const type: TypeSpec = { kind, element: { kind: "string" } };
      return <ModuleInputEditor type={type} value={value} referenceOptions={complexReferenceOptions} onChange={setValue} />;
    }

    render(<CollectionReferenceHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Use input" }));
    fireEvent.mouseDown(screen.getByPlaceholderText("Search module outputs"));
    fireEvent.click(screen.getByRole("option", { name: `module.${kind}_source.items` }));

    const selectedReference = screen.getByRole("button", { name: `Module output module.${kind}_source.items` });
    const selectorLabel = screen.getByText(`items is a ${kind} —`);
    expect(selectedReference.compareDocumentPosition(selectorLabel) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("selects an object descendant from one variable map entry", () => {
    function DescendantHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({ kind: "map", entries: [] });
      return (
        <>
          <ModuleInputEditor
            type={{ kind: "map", element: { kind: "object", attributes: [{ name: "description", type: { kind: "string" } }] } }}
            value={value}
            referenceOptions={complexReferenceOptions}
            onChange={setValue}
          />
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      );
    }

    render(<DescendantHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Use input" }));
    fireEvent.mouseDown(screen.getByPlaceholderText("Search module outputs"));
    fireEvent.click(screen.getByRole("option", { name: "var.lambda_functions" }));
    fireEvent.click(screen.getByRole("button", { name: "One instance" }));
    expect(screen.getByText("Whole value").closest(".MuiChip-root")).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("key expression"), { target: { value: "each.key" } });

    const descendant = screen.getByRole("combobox", { name: "Select descendant" });
    fireEvent.mouseDown(descendant);
    fireEvent.click(screen.getByRole("option", { name: "spec" }));
    expect(screen.getByTestId("value").textContent).toContain('"refOutputSelector":{"kind":"key","expr":{"mode":"literal","literal":"each.key"}}');
    expect(screen.getByTestId("value").textContent).toContain('"refAttributePath":"spec"');

    fireEvent.click(screen.getByRole("button", { name: "All instances" }));
    expect(screen.getByTestId("value").textContent).not.toContain('"refAttributePath":"spec"');
  });

  it("keeps optional object attributes behind the existing visibility toggle", () => {
    function OptionalHarness() {
      const [value, setValue] = React.useState<ModuleInputValue>({
        kind: "object",
        entries: [{
          name: "optional_name",
          value: { kind: "scalar", value: { mode: "literal", literal: "preserved" } },
        }],
      });
      return (
        <>
          <ModuleInputEditor
            type={{
              kind: "object",
              attributes: [
                { name: "required_name", type: { kind: "string" } },
                { name: "optional_name", type: { kind: "string" }, optional: true },
              ],
            }}
            value={value}
            referenceOptions={referenceOptions}
            onChange={setValue}
          />
          <output data-testid="value">{JSON.stringify(value)}</output>
        </>
      );
    }

    render(<OptionalHarness />);
    expect(screen.getByText("required_name")).toBeTruthy();
    expect(screen.getByText("optional_name (optional)")).toBeTruthy();

    const toggle = screen.getByRole("checkbox", { name: "Show optional fields" });
    expect((toggle as HTMLInputElement).checked).toBe(true);
    fireEvent.click(toggle);
    expect(screen.queryByText("optional_name (optional)")).toBeNull();
    expect(screen.getByTestId("value").textContent).toContain('"literal":"preserved"');

    fireEvent.click(toggle);
    expect(screen.getByText("optional_name (optional)")).toBeTruthy();
  });
});