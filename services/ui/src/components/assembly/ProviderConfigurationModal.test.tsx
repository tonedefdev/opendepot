// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ProviderSchemaBlock } from "@/lib/api";
import theme from "@/theme";
import ProviderConfigurationModal from "./ProviderConfigurationModal";
import type { ProviderConfiguration } from "./types";

// Vitest compiles JSX with the classic runtime, but production files such as DepotKindIcon rely on the automatic
// runtime that Next.js uses. Exposing React globally lets those files render under test without changing config.
Object.assign(globalThis, { React });

const awsSchema: ProviderSchemaBlock = {
  attributes: {
    region: { type: "string", required: true },
    profile: { type: "string", optional: true },
    access_key: { type: "string", optional: true },
    id: { type: "string", computed: true },
  },
  blocks: {
    assume_role: {
      nesting: "list",
      minItems: 1,
      maxItems: 1,
      block: { attributes: { role_arn: { type: "string", required: true } } },
    },
    ignore_tags: {
      nesting: "single",
      block: { attributes: { keys: { type: ["list", "string"], optional: true } } },
    },
  },
};

const emptyValue: ProviderConfiguration = { arguments: {}, blocks: {} };
const configuredValue: ProviderConfiguration = {
  arguments: { profile: { mode: "literal", literal: "dev" } },
  blocks: {},
};

function makeAttributes(count: number): ProviderSchemaBlock {
  const attributes: NonNullable<ProviderSchemaBlock["attributes"]> = {};
  for (let i = 1; i <= count; i += 1) {
    attributes[`setting_${String(i).padStart(2, "0")}`] = { type: "string", optional: true };
  }
  return { attributes };
}

function makeProps(overrides: Partial<React.ComponentProps<typeof ProviderConfigurationModal>> = {}): React.ComponentProps<typeof ProviderConfigurationModal> {
  return {
    open: true,
    onClose: vi.fn(),
    localName: "aws",
    alias: "",
    schema: awsSchema,
    value: emptyValue,
    referenceOptions: [],
    onChange: vi.fn(),
    ...overrides,
  };
}

function renderModal(overrides: Partial<React.ComponentProps<typeof ProviderConfigurationModal>> = {}) {
  const props = makeProps(overrides);
  const result = render(
    <ThemeProvider theme={theme}>
      <ProviderConfigurationModal {...props} />
    </ThemeProvider>,
  );
  return { ...result, props };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ProviderConfigurationModal header and summary", () => {
  it("renders the provider title without an alias", () => {
    renderModal();

    expect(screen.getByRole("heading", { name: "provider.aws — Configuration" })).toBeTruthy();
  });

  it("appends the alias to the provider title", () => {
    renderModal({ alias: "west" });

    expect(screen.getByRole("heading", { name: "provider.aws.west — Configuration" })).toBeTruthy();
  });

  it("counts the configurable fields and how many are configured", () => {
    renderModal({ value: configuredValue });

    expect(screen.getByText("5 fields · 1 configured")).toBeTruthy();
  });

  it("uses the singular field label for a single configurable field", () => {
    renderModal({ schema: { attributes: { region: { type: "string", required: true } } } });

    expect(screen.getByText("1 field · 0 configured")).toBeTruthy();
  });

  it("tells the user when the provider has no configurable fields", () => {
    renderModal({ schema: { attributes: { id: { type: "string", computed: true } } } });

    expect(screen.getByText("This provider has no configurable fields.")).toBeTruthy();
  });

  it("calls onClose from the close icon and the Done button", () => {
    const { props } = renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("button", { name: "Done" }));

    expect(props.onClose).toHaveBeenCalledTimes(2);
  });

  it("shows the logo for a known provider and a kind icon for an unknown one", () => {
    const { unmount } = renderModal({ localName: "aws" });
    expect(screen.getAllByAltText("AWS").length).toBeGreaterThan(0);
    unmount();

    renderModal({ localName: "custom" });
    expect(screen.queryByAltText("AWS")).toBeNull();
  });
});

describe("ProviderConfigurationModal sections and editing", () => {
  it("groups fields into configured, required, and optional sections", () => {
    renderModal({ value: configuredValue });

    const headings = screen.getAllByText(/^(Configured|Required|Optional)$/).map((node) => node.textContent);
    expect(headings).toEqual(["Configured", "Required", "Optional"]);
  });

  it("marks a block with a minimum item count as required", () => {
    renderModal({ schema: { blocks: { assume_role: awsSchema.blocks?.assume_role ?? { nesting: "list", block: {} } } } });

    expect(screen.getByText("Required")).toBeTruthy();
    expect(screen.queryByText("Optional")).toBeNull();
  });

  it("forwards a configured field change through onChange", () => {
    const { props } = renderModal({ value: configuredValue });

    fireEvent.change(screen.getAllByRole("textbox", { name: "HCL value" })[0], { target: { value: "staging" } });

    expect(props.onChange).toHaveBeenCalledWith({
      arguments: { profile: { mode: "literal", literal: "staging" } },
      blocks: {},
    });
  });

  it("reflects configured values in the HCL preview", () => {
    renderModal({ value: configuredValue });

    expect(screen.getByLabelText("Provider HCL preview").textContent).toContain("dev");
  });
});

describe("ProviderConfigurationModal search and paging", () => {
  it("filters fields by name and reports the matched count", () => {
    renderModal({ value: configuredValue });

    fireEvent.change(screen.getByPlaceholderText("Search configuration…"), { target: { value: "access" } });

    expect(screen.getByText("1 of 5 fields · 1 configured")).toBeTruthy();
  });

  it("shows an empty-search message when nothing matches", () => {
    renderModal();

    fireEvent.change(screen.getByPlaceholderText("Search configuration…"), { target: { value: "no-such-field" } });

    expect(screen.getByText("No fields match your search.")).toBeTruthy();
  });

  it("pages through fields with the previous and next buttons", () => {
    renderModal({ schema: makeAttributes(12) });

    expect(screen.getByRole("button", { name: "Previous page" }).hasAttribute("disabled")).toBe(true);
    expect(screen.queryByText("setting_11")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    expect(screen.getByText("setting_11")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Next page" }).hasAttribute("disabled")).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));

    expect(screen.queryByText("setting_11")).toBeNull();
  });

  it("jumps to a page from the page selector", () => {
    renderModal({ schema: makeAttributes(12) });

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Page" }));
    fireEvent.click(screen.getByRole("option", { name: "2" }));

    expect(screen.getByText("setting_11")).toBeTruthy();
  });

  it("changes the page size and recounts pages", () => {
    renderModal({ schema: makeAttributes(12) });

    expect(screen.getByText("of 2")).toBeTruthy();

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Per page" }));
    fireEvent.click(screen.getByRole("option", { name: "5" }));

    expect(screen.getByText("of 3")).toBeTruthy();
  });

  it("resets the search and page when the modal reopens", () => {
    const props = makeProps({ schema: makeAttributes(12) });
    const { rerender } = render(
      <ThemeProvider theme={theme}>
        <ProviderConfigurationModal {...props} />
      </ThemeProvider>,
    );

    fireEvent.change(screen.getByPlaceholderText("Search configuration…"), { target: { value: "setting" } });
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));

    rerender(
      <ThemeProvider theme={theme}>
        <ProviderConfigurationModal {...props} open={false} />
      </ThemeProvider>,
    );
    rerender(
      <ThemeProvider theme={theme}>
        <ProviderConfigurationModal {...props} open />
      </ThemeProvider>,
    );

    expect((screen.getByPlaceholderText("Search configuration…") as HTMLInputElement).value).toBe("");
    expect(screen.getByText("setting_01")).toBeTruthy();
  });
});

describe("ProviderConfigurationModal HCL preview", () => {
  it("expands and restores the preview pane", () => {
    renderModal();

    fireEvent.click(screen.getByRole("button", { name: "Expand HCL preview" }));

    const restore = screen.getByRole("button", { name: "Restore HCL preview" });
    expect(restore.getAttribute("aria-pressed")).toBe("true");

    fireEvent.click(restore);

    expect(screen.getByRole("button", { name: "Expand HCL preview" }).getAttribute("aria-pressed")).toBe("false");
  });
});
