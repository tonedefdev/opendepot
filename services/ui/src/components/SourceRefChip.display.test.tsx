// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import SourceRefChip from "./SourceRefChip";

const repoUrl = "https://github.com/tonedefdev/opendepot";
const commit = "0123456789abcdef0123456789abcdef01234567";
const fullSha = "abcdef0123456789abcdef0123456789abcdef01";

afterEach(() => {
  cleanup();
});

describe("SourceRefChip display", () => {
  it("shortens a full commit SHA ref while linking to the full value", () => {
    render(<SourceRefChip sourceRef={fullSha} repoUrl={repoUrl} />);

    const link = screen.getByText("abcdef0");
    expect(link.getAttribute("href")).toBe(`${repoUrl}/tree/${fullSha}`);
  });

  it("keeps a short ref that only looks like a SHA prefix", () => {
    render(<SourceRefChip sourceRef="abcdef0" repoUrl={repoUrl} />);

    expect(screen.getByText("abcdef0").getAttribute("href")).toBe(`${repoUrl}/tree/abcdef0`);
  });

  it("uses the full ref in the title when the commit is present", () => {
    const { container } = render(<SourceRefChip sourceRef="main" sourceCommit={commit} repoUrl={repoUrl} />);

    expect(container.querySelector("[title]")?.getAttribute("title")).toBe(`main@0123456`);
  });

  it("uses only the ref in the title when no commit is present", () => {
    const { container } = render(<SourceRefChip sourceRef="v1.2.3" repoUrl={repoUrl} />);

    expect(container.querySelector("[title]")?.getAttribute("title")).toBe("v1.2.3");
  });

  it("strips every trailing slash from the repository URL", () => {
    render(<SourceRefChip sourceRef="main" repoUrl={`${repoUrl}///`} />);

    expect(screen.getByText("main").getAttribute("href")).toBe(`${repoUrl}/tree/main`);
  });

  it("does not link a repository URL that is missing", () => {
    render(<SourceRefChip sourceRef="main" sourceCommit={commit} />);

    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("0123456").tagName).toBe("SPAN");
  });

  it("strips leading slashes from the source path", () => {
    render(<SourceRefChip sourceRef="main" sourcePath="///modules/vpc" repoUrl={repoUrl} />);

    expect(screen.getByText("main").getAttribute("href")).toBe(`${repoUrl}/tree/main/modules/vpc`);
  });

  it("renders the plain variant with the same link behavior", () => {
    render(<SourceRefChip sourceRef="main" repoUrl={repoUrl} plain />);

    expect(screen.getByText("main").getAttribute("href")).toBe(`${repoUrl}/tree/main`);
  });
});
