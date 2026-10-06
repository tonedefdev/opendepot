// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { ProviderConfiguration, ReferenceOption } from "./types";
import ProviderConfigurationEditor from "./ProviderConfigurationEditor";

const referenceOptions: ReferenceOption[] = [{
  nodeId: "module-network",
  instanceName: "network",
  output: "id",
  type: "string",
  label: "module.network.id",
  sourceMultiplicity: "none",
  sourceKind: "module",
}];

function Harness() {
  const [value, setValue] = React.useState<ProviderConfiguration>({
    arguments: { region: { mode: "literal", literal: "us-west-2" } },
    blocks: {},
  });
  return (
    <>
      <ProviderConfigurationEditor
        schema={{ attributes: { region: { type: "string", optional: true } } }}
        value={value}
        referenceOptions={referenceOptions}
        onChange={setValue}
      />
      <output data-testid="provider-value">{JSON.stringify(value)}</output>
    </>
  );
}

describe("ProviderConfigurationEditor", () => {
  it("uses the HCL editor and shared input-reference chip", () => {
    render(<Harness />);

    const editor = screen.getByRole("textbox", { name: "HCL value" });
    expect((editor as HTMLTextAreaElement).value).toBe("us-west-2");
    expect(screen.getByRole("button", { name: "Use input" })).toBeTruthy();
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(screen.getByText("region").nextElementSibling?.textContent).toBe("optional");
    expect((editor as HTMLTextAreaElement).placeholder).toBe("Value or HCL expression");

    fireEvent.change(editor, { target: { value: 'replace(var.region, "_", "-")' } });
    expect(JSON.parse(screen.getByTestId("provider-value").textContent ?? "{}").arguments.region.literal).toBe('replace(var.region, "_", "-")');

    fireEvent.click(screen.getByRole("button", { name: "Use input" }));
    expect(screen.getByPlaceholderText("Search module outputs")).toBeTruthy();
    fireEvent.click(screen.getByRole("option", { name: "module.network.id" }));
    expect(screen.getByTestId("provider-value").textContent).toContain('"refNodeId":"module-network"');
    expect(screen.getByRole("button", { name: "Module output module.network.id" })).toBeTruthy();
  });

  it("uses shared block add and remove actions while enforcing maxItems", () => {
    function BlockHarness() {
      const [value, setValue] = React.useState<ProviderConfiguration>({ arguments: {}, blocks: {} });
      return (
        <ProviderConfigurationEditor
          schema={{ blocks: { assume_role: { nesting: "list", maxItems: 1, block: {} } } }}
          value={value}
          referenceOptions={[]}
          onChange={setValue}
        />
      );
    }

    render(<BlockHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Add assume_role" }));
    expect(screen.getByRole("button", { name: "Remove assume_role 1" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add assume_role" }).hasAttribute("disabled")).toBe(true);
  });
});
