// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ResourceList from "./ResourceList";

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

function renderList(sortBy: string, sortDir: "asc" | "desc") {
  return render(
    <ResourceList
      items={[]}
      totalCount={0}
      sortBy={sortBy}
      sortDir={sortDir}
      pageSize={12}
      baseParams={`sort_by=${sortBy}&sort_dir=${sortDir}`}
    />,
  );
}

function lastUpdatedHeader() {
  return screen.getByRole("columnheader", { name: /Last updated/ });
}

describe("ResourceList sort wiring", () => {
  it("navigates to ascending last_updated when the header is clicked first", () => {
    renderList("name", "asc");

    fireEvent.click(within(lastUpdatedHeader()).getByRole("button", { name: "Last updated" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=last_updated&sort_dir=asc&page_size=12");
  });

  it("flips to descending when the active last_updated header is clicked again", () => {
    renderList("last_updated", "asc");

    expect(lastUpdatedHeader().querySelector(".MuiTableSortLabel-root")?.className).toContain("active");
    fireEvent.click(within(lastUpdatedHeader()).getByRole("button", { name: "Last updated" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=last_updated&sort_dir=desc&page_size=12");
  });

  it("shows the matching dropdown item for the current sort", () => {
    renderList("last_updated", "desc");

    expect(screen.getByRole("combobox", { name: "Sort by" }).textContent).toBe("Sort: Last updated (newest first)");
  });

  it("sets both sort_by and sort_dir when a last_updated item is chosen", () => {
    renderList("name", "asc");

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Sort by" }));
    fireEvent.click(screen.getByRole("option", { name: "Last updated (newest first)" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=last_updated&sort_dir=desc&page_size=12");
    expect(JSON.parse(window.localStorage.getItem("opendepot.registry.preferences") ?? "{}")).toEqual({
      sortBy: "last_updated",
      sortDir: "desc",
    });
  });

  it("keeps the current direction when a plain column item is chosen", () => {
    renderList("last_updated", "desc");

    fireEvent.mouseDown(screen.getByRole("combobox", { name: "Sort by" }));
    fireEvent.click(screen.getByRole("option", { name: "Kind" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=kind&sort_dir=desc&page_size=12");
  });
});
