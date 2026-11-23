// @vitest-environment jsdom

import { act, cleanup, render, screen } from "@testing-library/react";
import { hydrateRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import { LastUpdated } from "./LastUpdated";

const DAY_MS = 24 * 60 * 60 * 1000;

let root: Root | null = null;

afterEach(() => {
  if (root) {
    act(() => root?.unmount());
    root = null;
  }
  cleanup();
  document.body.innerHTML = "";
});

describe("LastUpdated", () => {
  it("renders no relative phrase in the server markup", () => {
    const value = new Date(Date.now() - 3 * DAY_MS).toISOString();
    const markup = renderToString(<LastUpdated value={value} variant="caption" prefix="Updated" />);

    expect(markup).not.toContain("ago");
    expect(markup).toContain("Updated ");
  });

  it("hydrates the server markup without a recoverable mismatch", async () => {
    const value = new Date(Date.now() - 3 * DAY_MS).toISOString();
    const element = <LastUpdated value={value} variant="caption" prefix="Updated" />;
    const container = document.createElement("div");
    container.innerHTML = renderToString(element);
    document.body.appendChild(container);

    const errors: unknown[] = [];
    await act(async () => {
      root = hydrateRoot(container, element, { onRecoverableError: (error) => errors.push(error) });
    });

    expect(errors).toEqual([]);
  });

  it("shows the relative time after mount", async () => {
    const value = new Date(Date.now() - 3 * DAY_MS).toISOString();
    render(<LastUpdated value={value} variant="body2" />);

    expect(await screen.findByText("3 days ago")).toBeTruthy();
  });

  it("shows an em dash for a missing value", () => {
    render(<LastUpdated variant="body2" />);

    expect(screen.getByText("\u2014")).toBeTruthy();
  });
});
