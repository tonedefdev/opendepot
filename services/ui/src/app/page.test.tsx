// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import HomePage from "./page";
import ResourceList from "@/components/ResourceList";
import ResourceListControls from "@/components/ResourceListControls";
import PageHeader from "@/components/PageHeader";
import { listNamespaces, listResources } from "@/lib/api";
import type { BrowseResource, BrowseResourceList } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import { DEFAULT_REGISTRY_PAGE_SIZE } from "@/lib/registryPreferences";

vi.mock("@/components/ResourceList", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/ResourceListControls", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/RegistryPreferencesSync", () => ({ default: () => null }));
vi.mock("@/components/RefreshIconButton", () => ({ default: () => null }));
vi.mock("@/components/RegistryNavigation", () => ({
  RegistryNavigationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/PageHeader", () => ({ default: vi.fn(() => null) }));
vi.mock("@/components/DepotKindIcon", () => ({ DepotKindIcon: () => null }));
vi.mock("@/lib/api", () => ({
  listResources: vi.fn(),
  listNamespaces: vi.fn(),
}));
vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));
vi.mock("next/navigation", () => ({
  redirect: vi.fn((): never => {
    throw new Error("NEXT_REDIRECT");
  }),
}));

function resourceList(overrides: Partial<BrowseResourceList> = {}): BrowseResourceList {
  return { items: [], totalCount: 0, page: 1, pageSize: DEFAULT_REGISTRY_PAGE_SIZE, ...overrides };
}

function resource(overrides: Partial<BrowseResource>): BrowseResource {
  return {
    kind: "module",
    namespace: "terraform-aws",
    name: "terraform-aws-vpc",
    latestVersion: "5.0.0",
    syncStatus: "Synced",
    synced: true,
    public: true,
    provider: "aws",
    repoUrl: "",
    providerNamespace: "",
    upstreamRegistry: "",
    platforms: [],
    scanCounts: null,
    ...overrides,
  } as BrowseResource;
}

/** Renders the async server component with the given search params and returns the rendered tree. */
async function renderPage(searchParams?: Record<string, string | string[] | undefined>) {
  const element = await HomePage({ searchParams: searchParams ? Promise.resolve(searchParams) : undefined });
  return render(element);
}

function lastListResourcesArgs() {
  const calls = vi.mocked(listResources).mock.calls;
  return calls[calls.length - 1];
}

function lastResourceListProps() {
  const calls = vi.mocked(ResourceList).mock.calls;
  return calls[calls.length - 1][0];
}

function lastControlsProps() {
  const calls = vi.mocked(ResourceListControls).mock.calls;
  return calls[calls.length - 1][0];
}

function lastHeaderProps() {
  const calls = vi.mocked(PageHeader).mock.calls;
  return calls[calls.length - 1][0];
}

describe("HomePage", () => {
  beforeEach(() => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
    vi.mocked(listResources).mockResolvedValue(resourceList({ items: [resource({})], totalCount: 1 }));
    vi.mocked(listNamespaces).mockResolvedValue({ items: [] });
    vi.mocked(ResourceList).mockClear();
    vi.mocked(ResourceListControls).mockClear();
    vi.mocked(PageHeader).mockClear();
    vi.mocked(redirect).mockClear();
  });

  afterEach(() => {
    cleanup();
  });

  it("uses name ascending, first page, and the default page size when no params are given", async () => {
    await renderPage(undefined);

    expect(lastListResourcesArgs()).toEqual([
      {
        namespace: undefined,
        kind: undefined,
        q: undefined,
        sortBy: "name",
        sortDir: "asc",
        page: 1,
        pageSize: DEFAULT_REGISTRY_PAGE_SIZE,
      },
      "session-token",
    ]);
    expect(listNamespaces).toHaveBeenCalledWith("session-token");
  });

  it("parses sort, filter, and pagination params from the URL", async () => {
    await renderPage({
      namespace: "acme",
      kind: "module",
      q: "vpc",
      sort_by: "version",
      sort_dir: "desc",
      page: "3",
      page_size: "24",
    });

    expect(lastListResourcesArgs()[0]).toEqual({
      namespace: "acme",
      kind: "module",
      q: "vpc",
      sortBy: "version",
      sortDir: "desc",
      page: 3,
      pageSize: 24,
    });
  });

  it("takes the first value when a search param is repeated", async () => {
    await renderPage({ sort_by: ["kind", "name"], page: ["2", "9"] });

    expect(lastListResourcesArgs()[0]).toMatchObject({ sortBy: "kind", page: 2 });
  });

  it("passes the parsed sort state through to the list and controls", async () => {
    await renderPage({ sort_by: "kind", sort_dir: "desc", page: "2", page_size: "12" });

    expect(lastResourceListProps()).toMatchObject({ sortBy: "kind", sortDir: "desc", pageSize: 12 });
    expect(lastControlsProps()).toMatchObject({ page: 2, pageSize: 12 });
  });

  it("falls back to ascending sort for any direction other than desc", async () => {
    await renderPage({ sort_dir: "sideways" });

    expect(lastResourceListProps()).toMatchObject({ sortDir: "asc" });
  });

  it("builds the pagination base params from filters and sort, excluding page and page size", async () => {
    await renderPage({
      namespace: "acme",
      kind: "module",
      q: "vpc",
      sort_by: "version",
      sort_dir: "desc",
      page: "4",
      page_size: "24",
    });

    const expected = "namespace=acme&kind=module&q=vpc&sort_by=version&sort_dir=desc";
    expect(lastResourceListProps()).toMatchObject({ baseParams: expected, namespace: "acme", query: "vpc" });
    expect(lastControlsProps()).toMatchObject({ baseParams: expected });
  });

  it("omits absent filters from the base params", async () => {
    await renderPage({ q: "" });

    expect(lastResourceListProps()).toMatchObject({ baseParams: "sort_by=name&sort_dir=asc" });
  });

  it("titles the page for a kind filter", async () => {
    await renderPage({ kind: "provider" });

    expect(lastHeaderProps()).toMatchObject({ title: "Providers", description: undefined });
  });

  it("uses the registry explorer title for an unknown or absent kind", async () => {
    await renderPage({ kind: "widget" });

    expect(lastHeaderProps()).toMatchObject({ title: "Registry Explorer" });
    expect(lastHeaderProps().description).toContain("Browse agents, skills, modules, and providers");
  });

  it("shows the empty state when no resources match", async () => {
    vi.mocked(listResources).mockResolvedValue(resourceList());

    await renderPage({ q: "nothing" });

    expect(screen.getByText(/No resources found/)).toBeTruthy();
    expect(vi.mocked(ResourceList)).not.toHaveBeenCalled();
  });

  it("renders the list and controls when resources are returned", async () => {
    await renderPage({});

    expect(lastResourceListProps()).toMatchObject({ totalCount: 1 });
    expect(lastControlsProps()).toMatchObject({ totalCount: 1, page: 1 });
  });

  it("shows the authentication error from the query string", async () => {
    await renderPage({ auth_error: "invalid%20token" });

    expect(screen.getByText("Authentication error: invalid token")).toBeTruthy();
  });

  it("shows the fetch error and an empty list when the registry request fails", async () => {
    vi.mocked(listResources).mockRejectedValue(new Error("server unreachable"));

    await renderPage({});

    expect(screen.getByText("The registry could not be loaded.")).toBeTruthy();
    expect(screen.getByText("server unreachable")).toBeTruthy();
    expect(lastResourceListProps()).toMatchObject({ items: [], totalCount: 0 });
  });

  it("uses a generic message when the failure is not an Error", async () => {
    vi.mocked(listResources).mockRejectedValue("boom");

    await renderPage({});

    expect(screen.getByText("Failed to load resources.")).toBeTruthy();
  });

  it("redirects to login when the registry rejects the session", async () => {
    vi.mocked(listResources).mockRejectedValue(new Error("401 unauthorized"));

    await expect(renderPage({})).rejects.toThrow("NEXT_REDIRECT");
    expect(redirect).toHaveBeenCalledWith("/login");
  });
});
