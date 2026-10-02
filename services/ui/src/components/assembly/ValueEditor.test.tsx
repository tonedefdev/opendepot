// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { TypeSpec, ValueSpec } from "./types";
import ValueEditor from "./ValueEditor";

const mapType: TypeSpec = {
  kind: "map",
  element: {
    kind: "object",
    attributes: [{ name: "enabled", type: { kind: "bool" } }],
  },
};

function Harness({ initial = { kind: "map", entries: [] } }: { initial?: ValueSpec }) {
  const [value, setValue] = React.useState<ValueSpec>(initial);
  return (
    <>
      <ValueEditor type={mapType} value={value} onChange={setValue} />
      <output data-testid="value">{JSON.stringify(value)}</output>
    </>
  );
}

describe("ValueEditor map accordions", () => {
  it("expands a new entry and collapses its previous sibling", () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));
    const firstSummary = screen.getByRole("button", { name: "New entry" });
    expect(firstSummary.getAttribute("aria-expanded")).toBe("true");

    fireEvent.change(screen.getByLabelText("Key"), { target: { value: "first" } });
    fireEvent.click(screen.getByRole("button", { name: "Add entry" }));

    expect(screen.getByRole("button", { name: "first" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("button", { name: "New entry" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("allows manual sibling switching and preserves recursive values", () => {
    render(
      <Harness
        initial={{
          kind: "map",
          entries: [
            { key: "first", value: { kind: "object", entries: [] } },
            { key: "second", value: { kind: "object", entries: [] } },
          ],
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "first" }));
    expect(screen.getByRole("button", { name: "first" }).getAttribute("aria-expanded")).toBe("true");
    const firstAccordion = screen.getByRole("button", { name: "first" }).closest(".MuiAccordion-root");
    fireEvent.change(within(firstAccordion as HTMLElement).getByPlaceholderText("true or false"), {
      target: { value: "true" },
    });

    fireEvent.click(screen.getByRole("button", { name: "second" }));
    expect(screen.getByRole("button", { name: "first" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("button", { name: "second" }).getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByTestId("value").textContent).toContain('"literal":"true"');
  });

  it("keeps accordion state aligned after deleting an earlier entry", () => {
    render(
      <Harness
        initial={{
          kind: "map",
          entries: [
            { key: "first", value: { kind: "object", entries: [] } },
            { key: "second", value: { kind: "object", entries: [] } },
          ],
        }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "second" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove entry" }));

    expect(screen.queryByRole("button", { name: "second" })).toBeNull();
    expect(screen.getByRole("button", { name: "first" }).getAttribute("aria-expanded")).toBe("false");
  });
});
