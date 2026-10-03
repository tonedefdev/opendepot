// @vitest-environment jsdom

import * as React from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { VariableValidation } from "./types";
import ValidationsEditor from "./ValidationsEditor";

function Harness({ initial }: { initial: VariableValidation[] }) {
  const [value, setValue] = React.useState(initial);
  return <ValidationsEditor validations={value} onChange={setValue} />;
}

describe("ValidationsEditor", () => {
  it("collapses existing validations and expands only the newest one", () => {
    render(<Harness initial={[{ condition: "length(var.name) > 0", errorMessage: "Required." }]} />);

    const first = screen.getByRole("button", { name: "Validation 1" });
    expect(first.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByText("length(var.name) > 0")).toBeTruthy();
    expect(screen.queryByText("incomplete")).toBeNull();

    fireEvent.click(first);
    expect(first.getAttribute("aria-expanded")).toBe("true");

    fireEvent.click(screen.getByRole("button", { name: "Add validation" }));
    expect(first.getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByRole("button", { name: "Validation 2" }).getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("incomplete")).toBeTruthy();
  });

  it("highlights HCL traversals, built-in functions, and for-expression keywords", () => {
    const condition = "alltrue([for _, fn in var.lambda_functions : length(fn.description) <= 100])";
    const { container } = render(<Harness initial={[{ condition, errorMessage: "Description is too long." }]} />);

    fireEvent.click(screen.getByRole("button", { name: "Validation 1" }));

    expect(container.querySelector(".token.variable")?.textContent).toBe("var.lambda_functions");
    expect(Array.from(container.querySelectorAll(".token.function"), (token) => token.textContent)).toEqual(["alltrue", "length"]);
    expect(Array.from(container.querySelectorAll(".token.keyword"), (token) => token.textContent)).toEqual(["for", "in"]);
  });
});
