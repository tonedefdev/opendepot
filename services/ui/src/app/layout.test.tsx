import * as React from "react";
import type { ReactElement, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Sidebar from "@/components/Sidebar";
import { getScanPolicyCatalog, listNamespaces } from "@/lib/api";
import { getServerSessionToken, parseJWTClaims } from "@/lib/session";
import { COLOR_MODE_COOKIE } from "@/theme";
import RootLayout, { metadata } from "./layout";

vi.mock("@fontsource-variable/inter", () => ({}));
vi.mock("./fonts.css", () => ({}));

vi.mock("next/headers", () => ({
  cookies: vi.fn(),
}));

vi.mock("@/components/ThemeRegistry", () => ({
  default: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@/components/Sidebar", () => ({
  default: () => null,
  SIDEBAR_COLLAPSED_COOKIE: "od_sidebar_collapsed",
}));

vi.mock("@/lib/api", () => ({
  getScanPolicyCatalog: vi.fn(),
  listNamespaces: vi.fn(),
}));

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
  parseJWTClaims: vi.fn(),
}));

import { cookies } from "next/headers";
Object.assign(globalThis, { React });

type ElementNode = ReactElement<{ children?: ReactNode; [key: string]: unknown }>;

function findElement(node: ReactNode, predicate: (element: ElementNode) => boolean): ElementNode | undefined {
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findElement(child, predicate);
      if (found) return found;
    }
    return undefined;
  }
  if (!node || typeof node !== "object" || !("props" in node)) return undefined;
  const element = node as ElementNode;
  if (predicate(element)) return element;
  return findElement(element.props.children, predicate);
}

function cookieStore(values: Record<string, string>) {
  return {
    get: (name: string) => (name in values ? { name, value: values[name] } : undefined),
  };
}

async function renderLayout(children: ReactNode = null) {
  return (await RootLayout({ children })) as ElementNode;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(cookies).mockResolvedValue(cookieStore({}) as never);
  vi.mocked(getServerSessionToken).mockResolvedValue(undefined as never);
  vi.mocked(listNamespaces).mockResolvedValue({ items: [] } as never);
  vi.mocked(getScanPolicyCatalog).mockResolvedValue({ writesEnabled: false, items: [] } as never);
  vi.mocked(parseJWTClaims).mockReturnValue(null);
  vi.stubEnv("DEV_TOKEN_INPUT_ENABLED", "false");
});

describe("RootLayout metadata", () => {
  it("exports the workshop title and description", () => {
    expect(metadata.title).toBe("OpenDepot Workshop");
    expect(metadata.description).toBe("Browse Terraform modules and providers in your OpenDepot registry.");
  });
});

describe("RootLayout", () => {
  it("renders the children inside the main content region", async () => {
    const tree = await renderLayout(<span>page body</span>);

    const main = findElement(tree, (element) => element.props["component"] === "main");

    expect(main).toBeDefined();
    expect(findElement(main, (element) => element.type === "span")).toBeDefined();
  });

  it("uses the saved light color scheme on the html element", async () => {
    vi.mocked(cookies).mockResolvedValue(cookieStore({ [COLOR_MODE_COOKIE]: "light" }) as never);

    const tree = await renderLayout();

    expect(tree.type).toBe("html");
    expect(tree.props["data-mui-color-scheme"]).toBe("light");
  });

  it("ignores unknown color scheme cookie values", async () => {
    vi.mocked(cookies).mockResolvedValue(cookieStore({ [COLOR_MODE_COOKIE]: "sepia" }) as never);

    const tree = await renderLayout();

    expect(tree.props["data-mui-color-scheme"]).toBeUndefined();
  });

  it("does not load namespaces or policies without a session token", async () => {
    await renderLayout();

    expect(listNamespaces).not.toHaveBeenCalled();
    expect(getScanPolicyCatalog).not.toHaveBeenCalled();
  });

  it("passes namespaces, user info, and policy availability to the sidebar", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
    vi.mocked(listNamespaces).mockResolvedValue({
      items: [{ name: "platform", public: true }],
    } as never);
    vi.mocked(getScanPolicyCatalog).mockResolvedValue({ writesEnabled: true, items: [] } as never);
    vi.mocked(parseJWTClaims).mockReturnValue({ email: "dev@example.com", preferred_username: "dev" });
    vi.mocked(cookies).mockResolvedValue(cookieStore({ od_sidebar_collapsed: "true" }) as never);
    vi.stubEnv("DEV_TOKEN_INPUT_ENABLED", "true");

    const tree = await renderLayout();
    const sidebar = findElement(tree, (element) => element.type === Sidebar);

    expect(listNamespaces).toHaveBeenCalledWith("session-token");
    expect(getScanPolicyCatalog).toHaveBeenCalledWith("session-token");
    expect(sidebar?.props).toMatchObject({
      initialNamespaces: [{ name: "platform", public: true }],
      userInfo: { email: "dev@example.com", name: "dev" },
      devTokenEnabled: true,
      securityPoliciesEnabled: true,
      initialCollapsed: true,
    });
  });

  it("prefers the name claim over preferred_username", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
    vi.mocked(parseJWTClaims).mockReturnValue({ email: "dev@example.com", name: "Dev Person", preferred_username: "dev" });

    const tree = await renderLayout();
    const sidebar = findElement(tree, (element) => element.type === Sidebar);

    expect(sidebar?.props["userInfo"]).toEqual({ email: "dev@example.com", name: "Dev Person" });
  });

  it("leaves user info empty when the token has no email claim", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
    vi.mocked(parseJWTClaims).mockReturnValue({ name: "No Email" });

    const tree = await renderLayout();
    const sidebar = findElement(tree, (element) => element.type === Sidebar);

    expect(sidebar?.props["userInfo"]).toBeNull();
  });

  it("falls back to empty namespaces and hidden policies when the server is unavailable", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
    vi.mocked(listNamespaces).mockRejectedValue(new Error("offline"));
    vi.mocked(getScanPolicyCatalog).mockRejectedValue(new Error("offline"));

    const tree = await renderLayout();
    const sidebar = findElement(tree, (element) => element.type === Sidebar);

    expect(sidebar?.props).toMatchObject({
      initialNamespaces: [],
      securityPoliciesEnabled: false,
    });
  });

  it("returns an empty namespace list when the response has no items", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
    vi.mocked(listNamespaces).mockResolvedValue({} as never);

    const tree = await renderLayout();
    const sidebar = findElement(tree, (element) => element.type === Sidebar);

    expect(sidebar?.props["initialNamespaces"]).toEqual([]);
  });
});
