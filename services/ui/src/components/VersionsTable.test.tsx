// @vitest-environment jsdom

import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ThemeProvider } from "@mui/material/styles";
import * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import VersionsTable from "./VersionsTable";
import theme from "@/theme";
import type { BrowseVersionSummary } from "@/lib/api";

function makeVersion(overrides: Partial<BrowseVersionSummary> = {}): BrowseVersionSummary {
  return {
    name: "vpc",
    version: "1.0.0",
    syncStatus: "Synced",
    os: "",
    arch: "",
    lastScanned: "2026-01-01",
    synced: true,
    scanCounts: null,
    ...overrides,
  };
}

function listResponse(items: BrowseVersionSummary[], extra: Record<string, unknown> = {}) {
  return {
    items,
    totalCount: items.length,
    page: 1,
    pageSize: 10,
    ...extra,
  };
}

function jsonResponse(body: unknown, init: { ok?: boolean; status?: number; statusText?: string } = {}) {
  return {
    ok: init.ok ?? true,
    status: init.status ?? 200,
    statusText: init.statusText ?? "OK",
    json: async () => body,
  };
}

function stubFetch(body: unknown = listResponse([])) {
  const fetchMock = vi.fn(async () => jsonResponse(body));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderVersions(props: Partial<React.ComponentProps<typeof VersionsTable>> = {}) {
  return render(
    <ThemeProvider theme={theme}>
      <VersionsTable namespace="acme" kind="module" name="vpc" {...props} />
    </ThemeProvider>,
  );
}

function lastRequestUrl(fetchMock: ReturnType<typeof stubFetch>): string {
  const calls = fetchMock.mock.calls as unknown as [string][];
  return calls[calls.length - 1][0];
}

beforeEach(() => {
  vi.useRealTimers();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("VersionsTable loading and data", () => {
  it("requests the first page from the versions proxy with encoded identifiers", async () => {
    const fetchMock = stubFetch(listResponse([makeVersion()]));

    render(
      <ThemeProvider theme={theme}>
        <VersionsTable namespace="my team" kind="module" name="vpc/net" />
      </ThemeProvider>,
    );

    await waitFor(() => expect(screen.getAllByText("v1.0.0").length).toBeGreaterThan(0));
    expect(fetchMock).toHaveBeenCalledWith("/api/versions/my%20team/module/vpc%2Fnet?page=1&page_size=10");
  });

  it("shows skeleton placeholders until the first response arrives", async () => {
    let resolveFetch: (value: unknown) => void = () => undefined;
    vi.stubGlobal("fetch", vi.fn(() => new Promise((resolve) => { resolveFetch = resolve; })));

    const { container } = renderVersions();

    expect(container.querySelectorAll(".MuiSkeleton-root").length).toBeGreaterThan(0);
    expect(screen.queryByRole("tab")).toBeNull();

    await act(async () => {
      resolveFetch(jsonResponse(listResponse([makeVersion()])));
    });

    await waitFor(() => expect(container.querySelectorAll(".MuiSkeleton-root").length).toBe(0));
  });

  it("shows the total count chip and prefixes versions with v", async () => {
    stubFetch(listResponse([makeVersion({ version: "2.0.0" }), makeVersion({ version: "v3.1.0" })], { totalCount: 42 }));

    renderVersions();

    expect(await screen.findByText("42")).toBeTruthy();
    expect(screen.getAllByText("v2.0.0").length).toBeGreaterThan(0);
    expect(screen.getAllByText("v3.1.0").length).toBeGreaterThan(0);
  });

  it("shows dashes for missing names, file names, checksums, and versions", async () => {
    stubFetch(listResponse([makeVersion({ version: "", name: undefined, fileName: undefined, checksum: undefined, lastScanned: "" })]));

    renderVersions();

    await screen.findAllByText("—");
    const row = screen.getAllByRole("row").find((r) => r.textContent?.includes("Synced")) as HTMLElement;
    expect(within(row).getAllByText("—").length).toBeGreaterThanOrEqual(4);
  });

  it("shows the empty state when no versions match", async () => {
    stubFetch(listResponse([]));

    renderVersions();

    expect(await screen.findAllByText("No versions match the current filters.")).toHaveLength(2);
  });

  it("shows the HTTP status when the proxy responds with an error", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse({}, { ok: false, status: 500, statusText: "Server Error" })));

    renderVersions();

    expect(await screen.findByText("500 Server Error")).toBeTruthy();
  });

  it("shows the thrown error message when the request fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => {
      throw new Error("network down");
    }));

    renderVersions();

    expect(await screen.findByText("network down")).toBeTruthy();
  });

  it("falls back to a generic message when a non-Error is thrown", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.reject("boom")));

    renderVersions();

    expect(await screen.findByText("Failed to load versions")).toBeTruthy();
  });

  it("ignores a response that arrives after the namespace has changed", async () => {
    const pending: Array<(value: unknown) => void> = [];
    vi.stubGlobal("fetch", vi.fn(() => new Promise((resolve) => { pending.push(resolve); })));

    const { rerender } = renderVersions({ name: "old" });
    rerender(
      <ThemeProvider theme={theme}>
        <VersionsTable namespace="acme" kind="module" name="new" />
      </ThemeProvider>,
    );

    await act(async () => {
      pending[0](jsonResponse(listResponse([makeVersion({ version: "9.9.9" })])));
    });
    await act(async () => {
      pending[1](jsonResponse(listResponse([makeVersion({ version: "1.2.3" })])));
    });

    expect(await screen.findAllByText("v1.2.3")).not.toHaveLength(0);
    expect(screen.queryByText("v9.9.9")).toBeNull();
  });
});

describe("VersionsTable provider columns", () => {
  it("shows OS, arch, and schema columns for providers", async () => {
    stubFetch(
      listResponse([makeVersion({ os: "linux", arch: "amd64", schemaState: "Succeeded" })], {
        availableOS: ["linux"],
        availableArch: ["amd64"],
      }),
    );

    renderVersions({ kind: "provider", name: "aws" });

    expect(await screen.findAllByRole("columnheader", { name: "OS" })).not.toHaveLength(0);
    expect(screen.getByRole("columnheader", { name: "Arch" })).toBeTruthy();
    expect(screen.getByRole("columnheader", { name: "Schema" })).toBeTruthy();
    expect(screen.getAllByText("linux").length).toBeGreaterThan(0);
    expect(screen.getAllByText("amd64").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Extracted").length).toBeGreaterThan(0);
  });

  it("omits provider columns for modules", async () => {
    stubFetch(listResponse([makeVersion()]));

    renderVersions();

    await screen.findAllByText("v1.0.0");
    expect(screen.queryByRole("columnheader", { name: "OS" })).toBeNull();
    expect(screen.queryByRole("columnheader", { name: "Schema" })).toBeNull();
  });

  it("shows dashes for a provider row with no OS or arch", async () => {
    stubFetch(listResponse([makeVersion({ os: "", arch: "" })]));

    renderVersions({ kind: "provider", name: "aws" });

    await screen.findAllByText("v1.0.0");
    const row = screen.getAllByRole("row").find((r) => r.textContent?.includes("v1.0.0")) as HTMLElement;
    expect(within(row).getAllByText("—").length).toBeGreaterThanOrEqual(2);
  });

  it("shows failed and unknown schema states", async () => {
    stubFetch(
      listResponse([
        makeVersion({ version: "1.0.0", schemaState: "Failed", schemaMessage: "provider crashed" }),
        makeVersion({ version: "2.0.0", schemaState: "Pending" }),
      ]),
    );

    renderVersions({ kind: "provider", name: "aws" });

    expect(await screen.findAllByText("Failed")).not.toHaveLength(0);
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("uses the default schema message when extraction fails without one", async () => {
    stubFetch(listResponse([makeVersion({ schemaState: "Failed" })]));

    renderVersions({ kind: "provider", name: "aws" });

    const failed = (await screen.findAllByText("Failed"))[0];
    fireEvent.mouseOver(failed);

    expect(await screen.findByText("Schema extraction failed")).toBeTruthy();
  });

  it("shows the schema failure message on hover", async () => {
    stubFetch(listResponse([makeVersion({ schemaState: "Failed", schemaMessage: "provider crashed" })]));

    renderVersions({ kind: "provider", name: "aws" });

    fireEvent.mouseOver((await screen.findAllByText("Failed"))[0]);

    expect(await screen.findByText("provider crashed")).toBeTruthy();
  });

  it("filters by OS and architecture only when the provider lists them", async () => {
    const fetchMock = stubFetch(
      listResponse([makeVersion({ os: "linux", arch: "amd64" })], {
        availableOS: ["linux", "darwin"],
        availableArch: ["amd64"],
      }),
    );

    renderVersions({ kind: "provider", name: "aws" });

    await screen.findAllByText("v1.0.0");
    fireEvent.mouseDown(screen.getAllByRole("combobox")[1]);
    fireEvent.click(await screen.findByRole("option", { name: "darwin" }));

    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("os=darwin"));

    fireEvent.mouseDown(screen.getAllByRole("combobox")[2]);
    fireEvent.click(await screen.findByRole("option", { name: "amd64" }));

    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("arch=amd64"));
    expect(lastRequestUrl(fetchMock)).toContain("os=darwin");
  });

  it("hides the OS and arch filters when the provider lists none", async () => {
    stubFetch(listResponse([makeVersion()]));

    renderVersions({ kind: "provider", name: "aws" });

    await screen.findAllByText("v1.0.0");
    expect(screen.queryByRole("combobox", { name: /^OS/ })).toBeNull();
    expect(screen.queryByRole("combobox", { name: /^Arch/ })).toBeNull();
    expect(screen.getAllByRole("combobox")).toHaveLength(2);
  });
});

describe("VersionsTable commit column", () => {
  it("shows the short commit for skill rows that have one", async () => {
    stubFetch(listResponse([makeVersion({ commit: "0123456789abcdef" })]));

    renderVersions({ kind: "skill", name: "review" });

    expect(await screen.findByRole("columnheader", { name: "Commit" })).toBeTruthy();
    expect(screen.getAllByText("0123456").length).toBeGreaterThan(0);
  });

  it("hides the commit column for agents without commits", async () => {
    stubFetch(listResponse([makeVersion()]));

    renderVersions({ kind: "agent", name: "helper" });

    await screen.findAllByText("v1.0.0");
    expect(screen.queryByRole("columnheader", { name: "Commit" })).toBeNull();
  });

  it("omits the commit column for non-agent kinds even when rows carry a commit", async () => {
    stubFetch(listResponse([makeVersion({ commit: "0123456789abcdef" })]));

    renderVersions({ kind: "module", name: "vpc" });

    await screen.findAllByText("v1.0.0");
    expect(screen.queryByRole("columnheader", { name: "Commit" })).toBeNull();
  });
});

describe("VersionsTable sync status", () => {
  it("shows blocked versions with their block reasons", async () => {
    stubFetch(listResponse([makeVersion({ blocked: true, blockReasons: ["critical CVE", "not approved"] })]));

    renderVersions();

    fireEvent.mouseOver((await screen.findAllByText("Blocked by Jev"))[0]);

    expect(await screen.findByText("critical CVE; not approved")).toBeTruthy();
  });

  it("falls back to a generic reason when a blocked version has none", async () => {
    stubFetch(listResponse([makeVersion({ blocked: true })]));

    renderVersions();

    fireEvent.mouseOver((await screen.findAllByText("Blocked by Jev"))[0]);

    expect((await screen.findByRole("tooltip")).textContent).toBe("Blocked by Jev");
  });

  it("shows the raw sync status for unblocked versions", async () => {
    stubFetch(listResponse([makeVersion({ syncStatus: "Progressing", synced: false })]));

    renderVersions();

    expect(await screen.findAllByText("Progressing")).not.toHaveLength(0);
  });
});

describe("VersionsTable filters", () => {
  it("debounces the search text before requesting results", async () => {
    const fetchMock = stubFetch(listResponse([makeVersion()]));

    renderVersions();
    await screen.findAllByText("v1.0.0");

    fireEvent.change(screen.getByPlaceholderText("Search versions…"), { target: { value: "net" } });

    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("q=net"), { timeout: 1500 });
    expect(lastRequestUrl(fetchMock)).toContain("page=1");
  });

  it("filters by sync status", async () => {
    const fetchMock = stubFetch(listResponse([makeVersion()]));

    renderVersions();
    await screen.findAllByText("v1.0.0");

    fireEvent.mouseDown(screen.getAllByRole("combobox")[0]);
    fireEvent.click(await screen.findByRole("option", { name: "Failed" }));

    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("synced=false"));
  });

  it("clears every filter from the clear button", async () => {
    const fetchMock = stubFetch(listResponse([makeVersion()]));

    renderVersions();
    await screen.findAllByText("v1.0.0");

    fireEvent.mouseDown(screen.getAllByRole("combobox")[0]);
    fireEvent.click(await screen.findByRole("option", { name: "Synced" }));
    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("synced=true"));

    fireEvent.click(screen.getByRole("button", { name: /Clear/ }));

    await waitFor(() => expect(lastRequestUrl(fetchMock)).not.toContain("synced="));
    expect(screen.queryByRole("button", { name: /Clear/ })).toBeNull();
  });

  it("returns to the first page when the page size changes", async () => {
    const fetchMock = stubFetch(listResponse([makeVersion()], { totalCount: 50 }));

    renderVersions();
    await screen.findAllByText("v1.0.0");

    fireEvent.click(screen.getByRole("button", { name: "Go to next page" }));
    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("page=2"));

    fireEvent.mouseDown(screen.getAllByRole("combobox").at(-1) as HTMLElement);
    fireEvent.click(await screen.findByRole("option", { name: "20" }));

    await waitFor(() => expect(lastRequestUrl(fetchMock)).toContain("page_size=20"));
    expect(lastRequestUrl(fetchMock)).toContain("page=1");
  });
});

describe("VersionsTable refresh", () => {
  it("re-fetches the current page and reports when every version is synced", async () => {
    const onAllSynced = vi.fn();
    const fetchMock = stubFetch(listResponse([makeVersion(), makeVersion({ version: "2.0.0", blocked: true })]));

    renderVersions({ onAllSynced });
    await screen.findAllByText("v1.0.0");
    expect(onAllSynced).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "refresh versions" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(onAllSynced).toHaveBeenCalledTimes(1));
  });

  it("does not report completion while a version is still progressing", async () => {
    const onAllSynced = vi.fn();
    const fetchMock = stubFetch(listResponse([makeVersion({ synced: false, syncStatus: "Progressing" })]));

    renderVersions({ onAllSynced });
    await screen.findAllByText("v1.0.0");

    fireEvent.click(screen.getByRole("button", { name: "refresh versions" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(onAllSynced).not.toHaveBeenCalled();
  });

  it("does not report completion when a synced version has a failed status", async () => {
    const onAllSynced = vi.fn();
    const fetchMock = stubFetch(listResponse([makeVersion({ syncStatus: "Sync failed" })]));

    renderVersions({ onAllSynced });
    await screen.findAllByText("Sync failed");

    fireEvent.click(screen.getByRole("button", { name: "refresh versions" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(onAllSynced).not.toHaveBeenCalled();
  });

  it("does not report completion when there are no versions", async () => {
    const onAllSynced = vi.fn();
    const fetchMock = stubFetch(listResponse([]));

    renderVersions({ onAllSynced });
    await screen.findAllByText("No versions match the current filters.");

    fireEvent.click(screen.getByRole("button", { name: "refresh versions" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(onAllSynced).not.toHaveBeenCalled();
  });
});
