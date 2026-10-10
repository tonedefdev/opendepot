// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import SourceRefChip from "./SourceRefChip";

const repoUrl = "https://github.com/tonedefdev/opendepot";
const commit = "0123456789abcdef0123456789abcdef01234567";

afterEach(() => {
  cleanup();
});

describe("SourceRefChip", () => {
  it("renders nothing when sourceRef is empty", () => {
    const { container } = render(<SourceRefChip sourceRef="" repoUrl={repoUrl} sourceCommit={commit} />);

    expect(container.innerHTML).toBe("");
  });

  it("shows the ref and the first seven characters of the commit", () => {
    render(<SourceRefChip sourceRef="main" sourceCommit={commit} sourcePath=".github/skills/x" repoUrl={repoUrl} />);

    expect(screen.getByText("main").tagName).toBe("A");
    expect(screen.getByText("0123456").tagName).toBe("A");
    expect(screen.getByText("@")).toBeTruthy();
  });

  it("links the ref to the tree path and the commit to the commit page", () => {
    render(<SourceRefChip sourceRef="main" sourceCommit={commit} sourcePath="/.github/skills/x" repoUrl={`${repoUrl}/`} />);

    expect(screen.getByText("main").getAttribute("href")).toBe(`${repoUrl}/tree/main/.github/skills/x`);
    expect(screen.getByText("0123456").getAttribute("href")).toBe(`${repoUrl}/commit/${commit}`);
  });

  it("shows the ref only when the commit is missing", () => {
    render(<SourceRefChip sourceRef="v2" repoUrl={repoUrl} />);

    expect(screen.getByText("v2").getAttribute("href")).toBe(`${repoUrl}/tree/v2`);
    expect(screen.queryByText("@")).toBeNull();
  });

  it("renders plain text for a non-GitHub repository", () => {
    render(<SourceRefChip sourceRef="main" sourceCommit={commit} repoUrl="https://gitlab.example.com/acme/repo" />);

    expect(screen.getByText("main").tagName).toBe("SPAN");
    expect(screen.getByText("0123456").tagName).toBe("SPAN");
    expect(screen.queryByRole("link")).toBeNull();
  });
});
