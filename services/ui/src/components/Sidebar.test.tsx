// @vitest-environment jsdom

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import Sidebar from "./Sidebar";
import theme from "@/theme";

const router = { push: vi.fn(), refresh: vi.fn() };
let pathname = "/";
let search = "";

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => pathname,
  useSearchParams: () => new URLSearchParams(search),
}));

vi.mock("@mui/material/useMediaQuery", () => ({
  default: () => false,
}));

afterEach(() => {
  vi.restoreAllMocks();
  pathname = "/";
  search = "";
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
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/namespaces", expect.objectContaining({ cache: "no-store" })));
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

describe("Sidebar needs review placement", () => {
  it.each([
    { initialCollapsed: false, state: "expanded" },
    { initialCollapsed: true, state: "collapsed" },
  ])("hides Needs review with Security Policies when disabled in the $state sidebar", ({ initialCollapsed }) => {
    renderSidebar(initialCollapsed, false);

    expect(screen.queryByRole("button", { name: "Needs review" })).toBeNull();
  });

  it("nests Needs review directly under Security Policies in the expanded sidebar", () => {
    renderSidebar(false, true);

    const securityPolicies = screen.getByRole("link", { name: "Security Policies" });
    const needsReview = screen.getByRole("button", { name: "Needs review" });
    expect(securityPolicies.closest("li")?.nextElementSibling?.contains(needsReview)).toBe(true);
  });

  it("opens the needs review drawer from the collapsed icon rail", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input) === "/api/review-queue") {
        return new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } });
      }
      throw new Error("not available in unit test");
    });

    render(
      <ThemeProvider theme={theme}>
        <Sidebar initialCollapsed={true} securityPoliciesEnabled={true} initialNamespaces={[]} />
      </ThemeProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Needs review" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/review-queue", expect.objectContaining({ cache: "no-store" })));
    expect(await screen.findByText("Nothing needs review right now.")).not.toBeNull();
  });
});

describe("Sidebar account control", () => {
  it("expands the collapsed rail when the user avatar is clicked", () => {
    render(
      <ThemeProvider theme={theme}>
        <Sidebar initialCollapsed={true} userInfo={{ email: "dev@example.com", name: "devuser" }} initialNamespaces={[]} />
      </ThemeProvider>,
    );

    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Expand sidebar to view account" }));

    expect(screen.getByRole("button", { name: "Sign out" })).not.toBeNull();
  });
});

describe("Sidebar registry navigation", () => {
  it.each([
    { initialCollapsed: false, state: "expanded" },
    { initialCollapsed: true, state: "collapsed" },
  ])("nests the registry kinds under Registry in the $state sidebar", ({ initialCollapsed }) => {
    renderSidebar(initialCollapsed);

    expect(screen.getByRole("link", { name: "Registry" }).getAttribute("href")).toBe("/");
    expect(screen.getByRole("link", { name: "Agents" }).getAttribute("href")).toBe("/?kind=agent");
    expect(screen.getByRole("link", { name: "Skills" }).getAttribute("href")).toBe("/?kind=skill");
    expect(screen.getByRole("link", { name: "Modules" }).getAttribute("href")).toBe("/?kind=module");
    expect(screen.getByRole("link", { name: "Providers" }).getAttribute("href")).toBe("/?kind=provider");
  });

  it("highlights the active kind child and not Registry", () => {
    pathname = "/";
    search = "kind=skill";
    renderSidebar(false);

    expect(screen.getByRole("link", { name: "Skills" }).classList.contains("Mui-selected")).toBe(true);
    expect(screen.getByRole("link", { name: "Registry" }).classList.contains("Mui-selected")).toBe(false);
    expect(screen.getByRole("link", { name: "Agents" }).classList.contains("Mui-selected")).toBe(false);
  });

  it("highlights Registry when no kind is selected", () => {
    renderSidebar(false);

    expect(screen.getByRole("link", { name: "Registry" }).classList.contains("Mui-selected")).toBe(true);
  });

  it("omits the separate Agents section and the Kind chip filter", () => {
    renderSidebar(false);

    expect(screen.queryByText("Kind")).toBeNull();
    expect(screen.queryByText("Module")).toBeNull();
    expect(screen.queryByRole("button", { name: "Agent" })).toBeNull();
  });
});

describe("Sidebar needs review navigation", () => {
  it("opens the needs review drawer from the navigation list", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input) === "/api/review-queue") {
        return new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } });
      }
      throw new Error("not available in unit test");
    });

    render(
      <ThemeProvider theme={theme}>
        <Sidebar initialCollapsed={false} securityPoliciesEnabled={true} initialNamespaces={[]} />
      </ThemeProvider>,
    );
    expect(fetchMock).not.toHaveBeenCalledWith("/api/review-queue", expect.anything());

    fireEvent.click(screen.getByRole("button", { name: "Needs review" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/review-queue", expect.objectContaining({ cache: "no-store" })));
    expect(await screen.findByText("Nothing needs review right now.")).not.toBeNull();
  });
});
