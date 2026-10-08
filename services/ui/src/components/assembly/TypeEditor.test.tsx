// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { TypeSpec } from "./types";
import TypeEditor from "./TypeEditor";

function Harness({ initial }: { initial: TypeSpec }) {
  const [value, setValue] = React.useState(initial);
  return (
    <>
      <TypeEditor value={value} onChange={setValue} />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </>
  );
}

function selectKind(combobox: HTMLElement, kind: TypeSpec["kind"]) {
  fireEvent.mouseDown(combobox);
  fireEvent.click(screen.getByRole("option", { name: kind }));
}

describe("TypeEditor", () => {
  it("collapses and expands nested object definitions", () => {
    render(
      <Harness
        initial={{
          kind: "object",
          attributes: [
            {
              name: "settings",
              type: { kind: "map", element: { kind: "object", attributes: [{ name: "enabled", type: { kind: "bool" } }] } },
            },
          ],
        }}
      />,
    );

    const toggle = screen.getByRole("button", { name: "settings definition" });
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByDisplayValue("enabled")).toBeTruthy();
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByText("1 attribute")).toBeTruthy();
    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
  });

  it("hides a nested attribute's default when its definition collapses", async () => {
    render(
      <Harness
        initial={{
          kind: "object",
          attributes: [
            {
              name: "spec",
              type: { kind: "object", attributes: [{ name: "enabled", type: { kind: "bool" } }] },
              optional: true,
              hasDefault: true,
              default: { kind: "object", entries: [] },
            },
          ],
        }}
      />,
    );

    expect(screen.getByRole("textbox", { name: "enabled value" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "spec definition" }));
    await waitFor(() => expect(screen.queryByRole("textbox", { name: "enabled value" })).toBeNull());
  });

  it("renders collection constructors as one inline chain", () => {
    render(<Harness initial={{ kind: "list", element: { kind: "map", element: { kind: "string" } } }} />);

    expect(screen.getByRole("combobox", { name: "Type" }).textContent).toBe("list");
    expect(screen.getByRole("combobox", { name: "Type list element" }).textContent).toBe("map");
    expect(screen.getByRole("combobox", { name: "Type map value" }).textContent).toBe("string");
  });

  it("offers every constructor at nested type positions", () => {
    render(
      <Harness
        initial={{
          kind: "object",
          attributes: [{ name: "settings", type: { kind: "string" } }],
        }}
      />,
    );

    selectKind(screen.getAllByRole("combobox")[1], "map");
    selectKind(screen.getAllByRole("combobox")[2], "object");

    expect(screen.getByTestId("value").textContent).toContain(
      '"type":{"kind":"map","element":{"kind":"object","attributes":[]}}',
    );
  });

  it("edits structured defaults for optional nested attributes", () => {
    render(
      <Harness
        initial={{
          kind: "object",
          attributes: [
            {
              name: "labels",
              type: { kind: "map", element: { kind: "string" } },
              optional: true,
            },
          ],
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "default" }));
    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));

    fireEvent.change(screen.getByRole("textbox", { name: "Key" }), { target: { value: "team" } });
    fireEvent.change(screen.getByPlaceholderText("value"), { target: { value: "platform" } });

    const value = screen.getByTestId("value").textContent ?? "";
    expect(value).toContain('"hasDefault":true');
    expect(value).toContain('"key":"team"');
    expect(value).toContain('"literal":"platform"');
  });
});
