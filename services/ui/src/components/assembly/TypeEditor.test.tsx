// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
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
  it("collapses and expands object definitions", () => {
    render(
      <Harness
        initial={{
          kind: "object",
          attributes: [{ name: "settings", type: { kind: "string" } }],
        }}
      />,
    );

    const summary = screen.getByRole("button", { name: "Object definition (1 attribute)" });
    expect(summary.getAttribute("aria-expanded")).toBe("true");
    fireEvent.click(summary);
    expect(summary.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(summary);
    expect(summary.getAttribute("aria-expanded")).toBe("true");
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

    fireEvent.click(screen.getByRole("checkbox", { name: "Has a default value" }));
    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));

    const details = screen.getByRole("button", { name: "New entry" }).closest(".MuiAccordion-root");
    fireEvent.change(within(details as HTMLElement).getByLabelText("Key"), { target: { value: "team" } });
    fireEvent.change(within(details as HTMLElement).getByPlaceholderText("value"), { target: { value: "platform" } });

    const value = screen.getByTestId("value").textContent ?? "";
    expect(value).toContain('"hasDefault":true');
    expect(value).toContain('"key":"team"');
    expect(value).toContain('"literal":"platform"');
  });
});
