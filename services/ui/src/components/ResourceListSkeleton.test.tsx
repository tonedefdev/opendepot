// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import { ResourceListSkeleton } from "./ResourceListSkeleton";

afterEach(() => {
  cleanup();
});

describe("ResourceListSkeleton", () => {
  it("renders the table skeleton with a header row in table mode", () => {
    render(<ResourceListSkeleton view="rows" rows={3} />);

    expect(screen.getByTestId("resource-table-skeleton")).toBeTruthy();
    expect(screen.queryByTestId("resource-card-skeleton")).toBeNull();
    expect(screen.getByRole("table", { name: "Loading registry resources" })).toBeTruthy();
    // One header row plus the requested body rows.
    expect(screen.getAllByRole("row")).toHaveLength(4);
  });

  it("renders the card skeleton and no table in card mode", () => {
    render(<ResourceListSkeleton view="cards" rows={3} />);

    expect(screen.getByTestId("resource-card-skeleton")).toBeTruthy();
    expect(screen.queryByTestId("resource-table-skeleton")).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("omits the source column when no row has a source ref", () => {
    render(<ResourceListSkeleton view="rows" rows={1} showSource={false} />);

    // Kind, Name, Version, Status, Last updated, Risk.
    const headerCells = screen.getAllByRole("columnheader");
    expect(headerCells).toHaveLength(6);
  });
});
