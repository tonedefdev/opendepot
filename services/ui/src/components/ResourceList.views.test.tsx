// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowseResource } from "@/lib/api";
import ResourceList from "./ResourceList";

// Vitest compiles JSX with the classic runtime, but production files such as DepotKindIcon rely on the automatic
// runtime that Next.js uses. Exposing React globally lets those files render under test without changing config.
Object.assign(globalThis, { React });

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push }),
}));

// Node ships an experimental localStorage that shadows jsdom's, so the test supplies its own.
function stubStorage(initial: Record<string, string> = {}) {
  const store = new Map<string, string>(Object.entries(initial));
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => void store.set(key, value),
      clear: () => store.clear(),
    },
  });
}

function resource(overrides: Partial<BrowseResource>): BrowseResource {
  return {
    namespace: "platform",
    kind: "module",
    name: "vpc",
    latestVersion: "1.0.0",
    ...overrides,
  } as BrowseResource;
}

const items: BrowseResource[] = [
  resource({ name: "vpc" }),
  resource({ name: "eks", sourceRef: "main", latestVersion: "2.0.0" }),
];

beforeEach(() => {
  push.mockClear();
  stubStorage();
});

afterEach(() => {
  cleanup();
});

function renderList(props: Partial<React.ComponentProps<typeof ResourceList>> = {}) {
  return render(
    <ResourceList
      items={items}
      totalCount={items.length}
      sortBy="name"
      sortDir="asc"
      pageSize={12}
      baseParams="sort_by=name&sort_dir=asc"
      {...props}
    />,
  );
}

describe("ResourceList summary", () => {
  it("uses the singular label for a single artifact", () => {
    renderList({ items: [items[0]], totalCount: 1 });

    expect(screen.getByText("1 artifact")).toBeTruthy();
  });

  it("uses the plural label for several artifacts", () => {
    renderList();

    expect(screen.getByText("2 artifacts")).toBeTruthy();
  });

  it("shows the source column only when an item has a source ref", () => {
    renderList();
    expect(screen.getByRole("columnheader", { name: /Source/ })).toBeTruthy();

    cleanup();
    renderList({ items: [items[0]], totalCount: 1 });
    expect(screen.queryByRole("columnheader", { name: /Source/ })).toBeNull();
  });
});

describe("ResourceList filter chips", () => {
  it("shows the namespace chip and removes the namespace param on delete", () => {
    renderList({ namespace: "platform", baseParams: "namespace=platform&sort_by=name&sort_dir=asc" });

    fireEvent.click(within(screen.getByText("Namespace: platform").parentElement as HTMLElement).getByTestId("CancelIcon"));

    expect(push).toHaveBeenCalledWith("/?sort_by=name&sort_dir=asc&page_size=12");
  });

  it("shows the search chip and removes the q param on delete", () => {
    renderList({ query: "vpc", baseParams: "q=vpc&sort_by=name&sort_dir=asc" });

    fireEvent.click(within(screen.getByText("Search: vpc").parentElement as HTMLElement).getByTestId("CancelIcon"));

    expect(push).toHaveBeenCalledWith("/?sort_by=name&sort_dir=asc&page_size=12");
  });
});

describe("ResourceList sort direction", () => {
  it("reverses the current sort when the reverse button is clicked", () => {
    renderList({ sortBy: "name", sortDir: "asc" });

    fireEvent.click(screen.getByRole("button", { name: "Reverse sort order" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=name&sort_dir=desc&page_size=12");
    expect(JSON.parse(window.localStorage.getItem("opendepot.registry.preferences") ?? "{}")).toEqual({
      sortBy: "name",
      sortDir: "desc",
    });
  });

  it("sorts by a plain column ascending when that column is not active", () => {
    renderList({ sortBy: "name", sortDir: "desc", baseParams: "sort_by=name&sort_dir=desc" });

    fireEvent.click(within(screen.getByRole("columnheader", { name: /Kind/ })).getByRole("button", { name: "Kind" }));

    expect(push).toHaveBeenCalledWith("/?sort_by=kind&sort_dir=asc&page_size=12");
  });
});

describe("ResourceList view switching", () => {
  it("renders cards for the saved card view", () => {
    stubStorage({ "opendepot.registry.preferences": JSON.stringify({ view: "cards" }) });
    renderList();

    expect(screen.queryByRole("table", { name: "Registry resources" })).toBeNull();
  });

  it("switches to cards and saves the choice when the card view is selected", () => {
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "Card view" }));

    expect(screen.queryByRole("table", { name: "Registry resources" })).toBeNull();
    expect(JSON.parse(window.localStorage.getItem("opendepot.registry.preferences") ?? "{}")).toEqual({ view: "cards" });
  });

  it("switches back to the table when the table view is selected", () => {
    stubStorage({ "opendepot.registry.preferences": JSON.stringify({ view: "cards" }) });
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "Table view" }));

    expect(screen.getByRole("table", { name: "Registry resources" })).toBeTruthy();
  });

  it("ignores a deselected toggle so one view stays active", () => {
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "Table view" }));

    expect(screen.getByRole("table", { name: "Registry resources" })).toBeTruthy();
  });
});
