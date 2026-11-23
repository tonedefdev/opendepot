// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ResourceListControls from "./ResourceListControls";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

// Node ships an experimental localStorage that shadows jsdom's, so the test supplies its own.
function stubStorage() {
  const store = new Map<string, string>();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => void store.set(key, value),
      clear: () => store.clear(),
    },
  });
}

beforeEach(() => {
  push.mockClear();
  stubStorage();
});

afterEach(() => {
  cleanup();
});

describe("ResourceListControls", () => {
  it("renders nothing when there are no results", () => {
    const { container } = render(<ResourceListControls totalCount={0} page={1} pageSize={12} />);

    expect(container.innerHTML).toBe("");
  });

  it("shows the visible range and the default noun", () => {
    render(<ResourceListControls totalCount={30} page={2} pageSize={12} />);

    expect(screen.getByText(/13–24 of 30 resources/)).toBeTruthy();
  });

  it("uses the singular noun and clamps the range for a single result", () => {
    render(<ResourceListControls totalCount={1} page={1} pageSize={12} noun={["module", "modules"]} />);

    expect(screen.getByText(/1–1 of 1 module$/)).toBeTruthy();
  });

  it("uses a custom plural noun and caps the last page at the total", () => {
    render(<ResourceListControls totalCount={14} page={2} pageSize={12} noun={["module", "modules"]} />);

    expect(screen.getByText(/13–14 of 14 modules/)).toBeTruthy();
  });

  it("navigates to the chosen page while keeping the base params and page size", () => {
    render(<ResourceListControls totalCount={60} page={1} pageSize={12} baseParams="q=vpc&sort_by=name" />);

    fireEvent.click(screen.getByRole("button", { name: /Go to page 3/ }));

    expect(push).toHaveBeenCalledWith("/?q=vpc&sort_by=name&page=3&page_size=12");
  });

  it("calls onChange instead of navigating when provided", () => {
    const onChange = vi.fn();
    render(<ResourceListControls totalCount={60} page={1} pageSize={12} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: /Go to page 2/ }));

    expect(onChange).toHaveBeenCalledWith(2, 12);
    expect(push).not.toHaveBeenCalled();
  });

  it("resets to page one and saves the page size when the size changes", () => {
    render(<ResourceListControls totalCount={60} page={3} pageSize={12} />);

    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "48" }));

    expect(push).toHaveBeenCalledWith("/?page=1&page_size=48");
    expect(JSON.parse(window.localStorage.getItem("opendepot.registry.preferences") ?? "{}")).toEqual({ pageSize: 48 });
  });

  it("reports a page size change through onChange without saving it", () => {
    const onChange = vi.fn();
    render(<ResourceListControls totalCount={60} page={3} pageSize={12} onChange={onChange} />);

    fireEvent.mouseDown(screen.getByRole("combobox"));
    fireEvent.click(screen.getByRole("option", { name: "24" }));

    expect(onChange).toHaveBeenCalledWith(1, 24);
    expect(push).not.toHaveBeenCalled();
    expect(window.localStorage.getItem("opendepot.registry.preferences")).toBeNull();
  });
});
