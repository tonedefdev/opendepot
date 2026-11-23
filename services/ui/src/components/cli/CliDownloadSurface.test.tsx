// @vitest-environment jsdom
import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import CliDownloadSurface from "./CliDownloadSurface";
Object.assign(globalThis, { React });

vi.mock("@/components/HighlightedCodeBlock", () => ({
  HighlightedCodeBlock: ({ code, language }: { code: string; language: string }) => (
    <pre data-testid="code-block" data-language={language}>
      {code}
    </pre>
  ),
}));

function setUserAgent(userAgent: string) {
  Object.defineProperty(window.navigator, "userAgent", { value: userAgent, configurable: true });
}

describe("CliDownloadSurface", () => {
  it("links every platform binary to its download route", () => {
    setUserAgent("Mozilla/5.0 (X11; Linux x86_64)");
    render(<CliDownloadSurface />);

    const downloads = screen.getAllByRole("link").filter((link) => link.hasAttribute("download"));

    expect(downloads.length).toBeGreaterThan(0);
    for (const link of downloads) {
      expect(link.getAttribute("href")).toMatch(/^\/api\/cli\//);
      expect(link.getAttribute("download")).toMatch(/^opendepot_/);
    }
  });

  it("links to the SHA256SUMS file", () => {
    setUserAgent("Mozilla/5.0 (X11; Linux x86_64)");
    render(<CliDownloadSurface />);

    expect(screen.getByRole("link", { name: "SHA256SUMS" }).getAttribute("href")).toBe("/api/cli/SHA256SUMS");
  });

  it("selects the operating system that matches the user agent", () => {
    setUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64)");
    render(<CliDownloadSurface />);

    const toggles = within(screen.getByRole("group", { name: "Operating system" }));
    const windowsToggle = toggles.getAllByRole("button").find((button) => button.textContent?.includes("Windows"));

    expect(windowsToggle?.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("Run in PowerShell").textContent).toBe("Run in PowerShell");
    expect(screen.getAllByTestId("code-block").at(-1)?.getAttribute("data-language")).toBe("powershell");
  });

  it("switches the install script when another operating system is chosen", () => {
    setUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)");
    render(<CliDownloadSurface />);

    const group = screen.getByRole("group", { name: "Operating system" });
    const windowsToggle = within(group).getAllByRole("button").find((button) => button.textContent?.includes("Windows"));
    if (!windowsToggle) throw new Error("Windows toggle not rendered");
    fireEvent.click(windowsToggle);

    expect(windowsToggle?.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("Run in PowerShell").textContent).toBe("Run in PowerShell");
  });

  it("keeps the current selection when the toggle is cleared", () => {
    setUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0)");
    render(<CliDownloadSurface />);

    const group = screen.getByRole("group", { name: "Operating system" });
    const selected = within(group).getAllByRole("button").find((button) => button.getAttribute("aria-pressed") === "true");
    if (!selected) throw new Error("No toggle selected");
    fireEvent.click(selected);

    expect(selected.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("Run in Terminal").textContent).toBe("Run in Terminal");
  });

  it("renders the install and verify sections", () => {
    setUserAgent("Mozilla/5.0 (X11; Linux x86_64)");
    render(<CliDownloadSurface />);

    expect(screen.getByText("Install").textContent).toBe("Install");
    expect(screen.getByText("Verify").textContent).toBe("Verify");
    expect(screen.getByText("OpenDepot CLI").textContent).toBe("OpenDepot CLI");
  });

  it("uses the registry origin from the window in install scripts", () => {
    setUserAgent("Mozilla/5.0 (X11; Linux x86_64)");
    render(<CliDownloadSurface />);

    const scripts = screen.getAllByTestId("code-block").map((block) => block.textContent ?? "");
    expect(scripts.some((script) => script.includes(window.location.origin))).toBe(true);
  });
});
