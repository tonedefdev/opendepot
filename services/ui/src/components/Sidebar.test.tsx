// @vitest-environment jsdom

import { render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Sidebar from "./Sidebar";
import theme from "@/theme";

const router = { push: vi.fn(), refresh: vi.fn() };
let pathname = "/";

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => pathname,
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("@mui/material/useMediaQuery", () => ({
  default: () => false,
}));

afterEach(() => {
  vi.restoreAllMocks();
  pathname = "/";
  globalThis.localStorage?.clear();
});

function renderSidebar(initialCollapsed = false, securityPoliciesEnabled = false) {
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("not available in unit test")));
  return render(
    <ThemeProvider theme={theme}>
      <Sidebar initialCollapsed={initialCollapsed} securityPoliciesEnabled={securityPoliciesEnabled} />
    </ThemeProvider>,
  );
}

describe("Sidebar namespace fallback", () => {
  it("does not request protected namespaces from the security policy workflow", async () => {
    pathname = "/security-policies";
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [] })));

    render(<Sidebar initialNamespaces={[]} />);
    await waitFor(() => expect(fetchMock).not.toHaveBeenCalled());
  });

  it("keeps the namespace fallback available for the browse page", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ items: [] })));

    render(<Sidebar initialNamespaces={[]} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/opendepot/ui/v1/namespaces"));
  });
});

describe("Sidebar security policy navigation", () => {
  it.each([
    { initialCollapsed: false, state: "expanded" },
    { initialCollapsed: true, state: "collapsed" },
  ])("hides Security Policies when disabled in the $state sidebar", ({ initialCollapsed }) => {
    renderSidebar(initialCollapsed, false);

    expect(screen.queryByRole("link", { name: "Security Policies" })).toBeNull();
  });

  it.each([
    { initialCollapsed: false, state: "expanded" },
    { initialCollapsed: true, state: "collapsed" },
  ])("shows Security Policies with the correct link when enabled in the $state sidebar", ({ initialCollapsed }) => {
    renderSidebar(initialCollapsed, true);

    expect(screen.getByRole("link", { name: "Security Policies" }).getAttribute("href")).toBe("/security-policies");
  });
});