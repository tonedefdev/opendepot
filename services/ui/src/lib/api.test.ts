import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ApiRequestError,
  createScanPolicy,
  deleteScanPolicy,
  exportAssembly,
  getContract,
  getDepotsGraph,
  getProviderSchema,
  getResourceDetail,
  getScanPolicy,
  getScanPolicyCatalog,
  getScanPolicyCapabilities,
  getStats,
  jevExemptionActive,
  listDepots,
  listJevReviewQueue,
  listNamespaces,
  listResources,
  listScanPolicies,
  previewScanPolicy,
  readAssemblyStream,
  updateScanPolicy,
} from "./api";
import type { JevAssessment } from "./api";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

function buildZip(commentLength = 0): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(98 + commentLength);
  const view = new DataView(bytes.buffer);
  view.setUint32(0, 0x04034b50, true);
  view.setUint32(30, 0x02014b50, true);
  view.setUint32(76, 0x06054b50, true);
  view.setUint16(86, 2, true);
  view.setUint32(88, 46, true);
  view.setUint32(92, 30, true);
  view.setUint16(96, commentLength, true);
  return bytes;
}

function mutateZip(mutate: (view: DataView) => void): Uint8Array<ArrayBuffer> {
  const bytes = buildZip();
  mutate(new DataView(bytes.buffer));
  return bytes;
}

function encodeBase64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

describe("browse API client", () => {
  it("lists scan policies in a namespace with an encoded path and bearer token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listScanPolicies("plat/x", "tok")).resolves.toEqual({ items: [] });

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/scan-policies\/plat%2Fx$/);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      cache: "no-store",
      headers: { Accept: "application/json", Authorization: "Bearer tok" },
    });
  });

  it("omits the Authorization header when no token is supplied", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await listNamespaces();

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/namespaces$/);
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty("Authorization");
  });

  it("fetches a single scan policy by namespace and name", async () => {
    const policy = { apiVersion: "opendepot.defdev.io/v1alpha1", kind: "ScanPolicy", metadata: { name: "base line", namespace: "platform" } };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(policy), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getScanPolicy("platform", "base line", "tok")).resolves.toEqual(policy);

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/scan-policies\/platform\/base%20line$/);
  });

  it("deletes a scan policy with an If-Match precondition and resolves on 204", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(deleteScanPolicy("platform", "baseline", "v3", "tok")).resolves.toBeUndefined();

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/scan-policies\/platform\/baseline$/);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      method: "DELETE",
      headers: { Accept: "application/json", "If-Match": "v3", Authorization: "Bearer tok" },
    });
  });

  it("deletes a scan policy without a precondition or token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await deleteScanPolicy("platform", "baseline");

    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "DELETE", headers: { Accept: "application/json" } });
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty("Authorization");
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty("If-Match");
  });

  it("throws an ApiRequestError carrying the status and parsed JSON body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: "forbidden" }), { status: 403, statusText: "Forbidden" })),
    );

    const error = await getScanPolicy("platform", "baseline").catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect(error).toMatchObject({
      name: "ApiRequestError",
      message: "API request failed: 403 Forbidden",
      status: 403,
      body: { code: "forbidden" },
    });
  });

  it("leaves the ApiRequestError body undefined when the error response is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" })));

    const error = await listNamespaces().catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect(error).toMatchObject({ status: 502, body: undefined });
  });

  it("uses the configured server URL and strips a trailing slash", async () => {
    vi.stubEnv("OPENDEPOT_SERVER_URL", "http://opendepot.test:8080/");
    vi.resetModules();
    const api = await import("./api");
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await api.listNamespaces();

    expect(fetchMock.mock.calls[0][0]).toBe("http://opendepot.test:8080/opendepot/ui/v1/namespaces");
  });

  it("falls back to the server host when no server URL is configured", async () => {
    vi.stubEnv("OPENDEPOT_SERVER_URL", undefined);
    vi.stubEnv("OPENDEPOT_SERVER_HOST", "opendepot-server.internal:9000");
    vi.resetModules();
    const api = await import("./api");
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await api.listNamespaces();

    expect(fetchMock.mock.calls[0][0]).toBe("http://opendepot-server.internal:9000/opendepot/ui/v1/namespaces");
  });

  it("builds the resource list query from every filter and encodes values", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], totalCount: 0, page: 2, pageSize: 50 }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await listResources(
      {
        namespace: "platform",
        kind: "module",
        q: "vpc & net",
        synced: false,
        os: "linux",
        arch: "amd64",
        severity: "HIGH",
        publicOnly: true,
        sortBy: "name",
        sortDir: "asc",
        page: 2,
        pageSize: 50,
      },
      "tok",
    );

    expect(String(fetchMock.mock.calls[0][0])).toMatch(
      /\/opendepot\/ui\/v1\/resources\?namespace=platform&kind=module&q=vpc\+%26\+net&synced=false&os=linux&arch=amd64&severity=HIGH&public_only=true&sort_by=name&sort_dir=asc&page=2&page_size=50$/,
    );
  });

  it("omits the resource list query when no filters are set and keeps falsy values that are defined", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ items: [], totalCount: 0, page: 0, pageSize: 0 }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await listResources({});
    await listResources({ namespace: "", synced: false, publicOnly: false, page: 0, pageSize: 0 });

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/resources$/);
    expect(String(fetchMock.mock.calls[1][0])).toMatch(/\/opendepot\/ui\/v1\/resources\?synced=false&public_only=false&page=0&page_size=0$/);
  });

  it("fetches resource detail with every path segment encoded", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ name: "vpc/aws" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getResourceDetail("plat form", "module", "vpc/aws", "tok")).resolves.toEqual({ name: "vpc/aws" });

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/resources\/plat%20form\/module\/vpc%2Faws$/);
  });

  it("lists depots from the depots route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [{ namespace: "platform", name: "core" }] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listDepots("tok")).resolves.toEqual({ items: [{ namespace: "platform", name: "core" }] });

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/depots$/);
  });

  it("requests the depot graph with or without a namespace filter", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ nodes: [] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getDepotsGraph();
    await getDepotsGraph("plat form", "tok");

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/depots\/graph$/);
    expect(String(fetchMock.mock.calls[1][0])).toMatch(/\/opendepot\/ui\/v1\/depots\/graph\?namespace=plat%20form$/);
  });

  it("requests stats with or without a namespace filter", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ totals: {} }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getStats()).resolves.toEqual({ totals: {} });
    await getStats("platform", "tok");

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/stats$/);
    expect(String(fetchMock.mock.calls[1][0])).toMatch(/\/opendepot\/ui\/v1\/stats\?namespace=platform$/);
  });

  it("updates a scan policy without a precondition when no resource version is supplied", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ name: "baseline" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await updateScanPolicy("platform", "baseline", {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "baseline", namespace: "platform" },
      spec: { priority: 0, severityThreshold: "HIGH", exemptions: [] },
    });

    const init = fetchMock.mock.calls[0][1];
    expect(init.method).toBe("PUT");
    expect(init.headers).not.toHaveProperty("If-Match");
    expect(JSON.parse(String(init.body)).metadata).toEqual({ name: "baseline", namespace: "platform" });
  });
});

describe("getContract", () => {
  it("returns the contract for the latest version with the bearer token", async () => {
    const contract = { schemaVersion: "1", module: { namespace: "platform", name: "vpc", version: "1.0.0" } };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(contract), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getContract("platform", "module", "vpc", undefined, "tok")).resolves.toEqual(contract);

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/opendepot\/ui\/v1\/resources\/platform\/module\/vpc\/contract$/);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({
      cache: "no-store",
      headers: { Accept: "application/json", Authorization: "Bearer tok" },
    });
  });

  it("encodes the requested version in the query string", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({}), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await getContract("platform", "module", "vpc", "1.0.0+build");

    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/contract\?version=1\.0\.0%2Bbuild$/);
    expect(fetchMock.mock.calls[0][1].headers).not.toHaveProperty("Authorization");
  });

  it("returns null when the contract does not exist", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 404, statusText: "Not Found" })));

    await expect(getContract("platform", "module", "vpc")).resolves.toBeNull();
  });

  it("throws a plain error for other failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 500, statusText: "Internal Server Error" })),
    );

    const error = await getContract("platform", "module", "vpc").catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(Error);
    expect(error).not.toBeInstanceOf(ApiRequestError);
    expect(error).toHaveProperty("message", "API request failed: 500 Internal Server Error");
  });
});

describe("getProviderSchema", () => {
  it("loads the provider schema for an exact version from the same-origin proxy", async () => {
    const schema = { namespace: "hashicorp", name: "aws", version: "5.0.0", schemaVersion: "assembly.provider.v1" };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(schema), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getProviderSchema("hashicorp", "aws", "5.0.0")).resolves.toEqual(schema);

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/provider-schema/hashicorp/aws?version=5.0.0",
      { cache: "no-store" },
    );
  });

  it("returns null when the provider schema is missing", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 404, statusText: "Not Found" })));

    await expect(getProviderSchema("hashicorp", "aws", "5.0.0")).resolves.toBeNull();
  });

  it("throws when the proxy returns another failure", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 503, statusText: "Service Unavailable" })));

    await expect(getProviderSchema("hashicorp", "aws", "5.0.0")).rejects.toThrow(
      "Provider schema request failed: 503 Service Unavailable",
    );
  });
});

describe("exportAssembly", () => {
  it("posts the generated document as JSON and returns the server error when the export is rejected", async () => {
    const error = { error: "validation_failed", message: "Module source is required", diagnostics: [{ code: "E1", message: "missing" }] };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(error), { status: 422, statusText: "Unprocessable Entity" }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(exportAssembly({ nodes: [] })).resolves.toEqual({ error });

    expect(fetchMock).toHaveBeenCalledWith("/api/assembly/export", {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/x-ndjson, application/json" },
      body: JSON.stringify({ nodes: [] }),
    });
  });

  it("returns a zip blob from a binary export response", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(buildZip(), { status: 200 })));

    const result = await exportAssembly({ nodes: [] });

    expect(result.error).toBeUndefined();
    expect(result.blob?.type).toBe("application/zip");
    expect(result.blob?.size).toBe(98);
  });

  it("accepts a zip archive that carries a trailing comment", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(buildZip(4), { status: 200 })));

    const result = await exportAssembly({});

    expect(result.blob?.size).toBe(102);
  });

  it("rejects a binary export that is not a complete zip archive", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new Uint8Array(21), { status: 200 })));

    await expect(exportAssembly({})).resolves.toEqual({
      error: {
        error: "invalid_export_response",
        message: "The export server returned an invalid ZIP archive. Please try again.",
      },
    });
  });

  it.each([
    ["the local file header signature is missing", (view: DataView) => view.setUint32(0, 0, true)],
    ["the end of central directory record is missing", (view: DataView) => view.setUint32(76, 0, true)],
    ["the trailing comment length is inconsistent", (view: DataView) => view.setUint16(96, 1, true)],
    ["the archive does not contain exactly two entries", (view: DataView) => view.setUint16(86, 1, true)],
    ["the central directory size does not end at the record", (view: DataView) => view.setUint32(88, 47, true)],
    ["the central directory header signature is missing", (view: DataView) => view.setUint32(30, 0, true)],
  ])("rejects a zip archive when %s", async (_description, mutate) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(mutateZip(mutate), { status: 200 })));

    const result = await exportAssembly({});

    expect(result.blob).toBeUndefined();
    expect(result.error?.error).toBe("invalid_export_response");
  });

  it("streams NDJSON exports and reports progress until the archive is complete", async () => {
    const payload = [
      JSON.stringify({ type: "started", output: "Packaging module" }),
      JSON.stringify({ type: "complete", archive: encodeBase64(buildZip()) }),
    ].join("\n");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(payload, { status: 200, headers: { "Content-Type": "application/x-ndjson" } })));
    vi.stubGlobal("window", { atob: globalThis.atob });
    const onProgress = vi.fn();

    const result = await exportAssembly({}, onProgress);

    expect(onProgress).toHaveBeenCalledWith({ phase: undefined, output: "Packaging module" });
    expect(result.blob?.type).toBe("application/zip");
  });
});

describe("readAssemblyStream edge cases", () => {
  it("reports an empty export response when the body is missing", async () => {
    await expect(readAssemblyStream(new Response(null))).resolves.toEqual({
      error: { error: "invalid_export_response", message: "The export server returned an empty response." },
    });
  });

  it("returns a blob from a complete event without a trailing newline and ignores blank, unknown, and incomplete events", async () => {
    const payload = [
      "",
      "   ",
      JSON.stringify({ type: "heartbeat" }),
      JSON.stringify({ type: "started" }),
      JSON.stringify({ type: "output", output: "Writing archive" }),
      JSON.stringify({ type: "error" }),
      JSON.stringify({ type: "complete" }),
      JSON.stringify({ type: "complete", archive: encodeBase64(buildZip()) }),
    ].join("\n");
    vi.stubGlobal("window", { atob: globalThis.atob });

    const result = await readAssemblyStream(new Response(payload));

    expect(result.error).toBeUndefined();
    expect(result.blob?.type).toBe("application/zip");
    expect(result.blob?.size).toBe(98);
  });

  it("reports an invalid archive when the completed payload is not a zip", async () => {
    vi.stubGlobal("window", { atob: globalThis.atob });
    const payload = `${JSON.stringify({ type: "complete", archive: encodeBase64(new Uint8Array([1, 2, 3])) })}\n`;

    await expect(readAssemblyStream(new Response(payload))).resolves.toEqual({
      error: {
        error: "invalid_export_response",
        message: "The export server returned an invalid ZIP archive. Please try again.",
      },
    });
  });

  it("reports that the stream ended before completion when no terminal event arrives", async () => {
    const onProgress = vi.fn();
    const payload = `${JSON.stringify({ type: "output", phase: "plan", output: "Planning" })}\n`;

    await expect(readAssemblyStream(new Response(payload), onProgress)).resolves.toEqual({
      error: { error: "invalid_export_response", message: "The export stream ended before completion." },
    });
    expect(onProgress).toHaveBeenCalledWith({ phase: "plan", output: "Planning" });
  });

  it("delivers streamed errors without a progress callback", async () => {
    const payload = `${JSON.stringify({ type: "output", output: "Working" })}\n${JSON.stringify({ type: "error", error: { error: "boom", message: "Failed" } })}\n`;

    await expect(readAssemblyStream(new Response(payload))).resolves.toEqual({
      error: { error: "boom", message: "Failed" },
    });
  });
});

describe("jevExemptionActive", () => {
  const assessment = (overrides: Partial<JevAssessment>): JevAssessment => ({
    evaluatedAt: "2026-05-20T12:00:00Z",
    needsReview: true,
    blocked: false,
    ...overrides,
  });
  const NOW = Date.parse("2026-05-20T12:00:00Z");

  it("treats an assessment that is not exempted as inactive", () => {
    expect(jevExemptionActive(assessment({ exempted: false, exemptionExpires: "2999-01-01T00:00:00Z" }), NOW)).toBe(false);
  });

  it("treats an exemption without an expiry as active", () => {
    expect(jevExemptionActive(assessment({ exempted: true }), NOW)).toBe(true);
  });

  it("treats an exemption expiring after now as active and one expired at or before now as lapsed", () => {
    expect(jevExemptionActive(assessment({ exempted: true, exemptionExpires: "2026-05-21T12:00:00Z" }), NOW)).toBe(true);
    expect(jevExemptionActive(assessment({ exempted: true, exemptionExpires: "2026-05-20T12:00:00Z" }), NOW)).toBe(false);
  });
});

describe("listJevReviewQueue pagination and failures", () => {
  it("stops paging when a page comes back empty even though the total says more remain", async () => {
    const fetchMock = vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      return new Response(JSON.stringify({ items: [], totalCount: 5, page: Number(url.searchParams.get("page")), pageSize: 100 }));
    });
    vi.stubGlobal("fetch", fetchMock);

    await expect(listJevReviewQueue("tok")).resolves.toEqual({ items: [] });

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("returns an empty queue without fetching details when no Skills or Agents exist", async () => {
    const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ items: [], totalCount: 0, page: 1, pageSize: 100 })));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listJevReviewQueue()).resolves.toEqual({ items: [] });

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\?kind=skill&page=1&page_size=100$/);
    expect(String(fetchMock.mock.calls[1][0])).toMatch(/\?kind=agent&page=1&page_size=100$/);
  });

  it("reviews every agent when the queue spans more than one detail batch", async () => {
    const agents = Array.from({ length: 9 }, (_unused, index) => ({ kind: "agent", namespace: "platform", name: `agent-${index}` }));
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname === "/opendepot/ui/v1/resources") {
        const isAgent = url.searchParams.get("kind") === "agent";
        return new Response(JSON.stringify({ items: isAgent ? agents : [], totalCount: isAgent ? agents.length : 0, page: 1, pageSize: 100 }));
      }
      const name = decodeURIComponent(url.pathname.split("/").pop() ?? "");
      return new Response(JSON.stringify({
        kind: "agent",
        namespace: "platform",
        name,
        jevAssessment: { evaluatedAt: "2026-05-20T12:00:00Z", needsReview: true, blocked: false, riskLevel: "Low" },
      }));
    }));

    const queue = await listJevReviewQueue("tok");

    expect(queue.items).toHaveLength(9);
    expect(queue.items[8]).toEqual({ kind: "agent", namespace: "platform", name: "agent-8", riskLevel: "Low", blocked: false, exempted: false });
  });

  it("fails the whole queue when a resource detail request fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname === "/opendepot/ui/v1/resources") {
        return new Response(JSON.stringify({ items: [{ kind: "skill", namespace: "platform", name: "broken" }], totalCount: 1, page: 1, pageSize: 100 }));
      }
      return new Response(JSON.stringify({ code: "internal" }), { status: 500, statusText: "Internal Server Error" });
    }));

    await expect(listJevReviewQueue("tok")).rejects.toBeInstanceOf(ApiRequestError);
  });
});

describe("readAssemblyStream", () => {
  it("reports output split across chunks and returns a streamed error", async () => {
    const payload = [
      JSON.stringify({ type: "started", output: "Preparing generated configuration" }),
      JSON.stringify({ type: "output", phase: "init", output: "Initializing modules..." }),
      JSON.stringify({ type: "error", error: { error: "tofu_init_failed", message: "OpenTofu validation failed" } }),
      "",
    ].join("\n");
    const chunks = [payload.slice(0, 17), payload.slice(17, 73), payload.slice(73)];
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk));
        controller.close();
      },
    });
    const progress = vi.fn();

    const result = await readAssemblyStream(new Response(stream), progress);

    expect(progress).toHaveBeenNthCalledWith(1, { phase: undefined, output: "Preparing generated configuration" });
    expect(progress).toHaveBeenNthCalledWith(2, { phase: "init", output: "Initializing modules..." });
    expect(result.error).toEqual({ error: "tofu_init_failed", message: "OpenTofu validation failed" });
  });
});

describe("scan policy API contract", () => {
  it("uses namespaced capabilities and preview routes with exact response shapes", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        writesEnabled: true,
        canRead: true,
        canWrite: true,
        canManageNamespaceWidePolicies: true,
        methods: ["GET", "POST", "PUT", "DELETE"],
        supportsPreview: true,
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        valid: true,
        policy: { apiVersion: "opendepot.defdev.io/v1alpha1", kind: "ScanPolicy" },
      }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getScanPolicyCapabilities("platform")).resolves.toMatchObject({ supportsPreview: true });
    await expect(previewScanPolicy("platform", {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "baseline", namespace: "platform" },
      spec: { severityThreshold: "HIGH" },
    })).resolves.toMatchObject({ valid: true });

    expect(fetchMock.mock.calls[0][0]).toContain("/scan-policies/platform/capabilities");
    expect(fetchMock.mock.calls[1][0]).toContain("/scan-policies/platform/preview");
    vi.unstubAllGlobals();
  });

  it("returns capabilities for callers who cannot manage namespace-wide policies", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      writesEnabled: true,
      canRead: true,
      canWrite: true,
      canManageNamespaceWidePolicies: false,
      methods: ["GET", "POST", "PUT", "DELETE"],
      supportsPreview: true,
    }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getScanPolicyCapabilities("platform")).resolves.toMatchObject({
      canWrite: true,
      canManageNamespaceWidePolicies: false,
    });
    vi.unstubAllGlobals();
  });

  it("sends jevExemptions unchanged so existing Jev exemptions survive a save", async () => {
    const jevExemptions = [
      { contentDigest: "a".repeat(64), reason: "Vendor attested", expires: "2027-01-01T00:00:00Z" },
      { contentDigest: "b".repeat(64), reason: "Reviewed" },
    ];
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ metadata: { name: "baseline" } }), { status: 201 }));
    vi.stubGlobal("fetch", fetchMock);

    await createScanPolicy("platform", {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "baseline", namespace: "platform" },
      spec: { severityThreshold: "HIGH", jevExemptions },
    });

    const body = JSON.parse(String(fetchMock.mock.calls[0][1]?.body));
    expect(body.spec.jevExemptions).toEqual(jevExemptions);
    vi.unstubAllGlobals();
  });

  it("loads the server-authorized catalog route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      writesEnabled: true,
      items: [{ namespace: "platform", canRead: true, canWrite: true, canManageNamespaceWidePolicies: false, modules: [{ name: "terraform-aws-vpc" }], providers: [] }],
    }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(getScanPolicyCatalog("id-token")).resolves.toMatchObject({ items: [{ namespace: "platform" }] });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/opendepot/ui/v1/scan-policies/catalog"),
      expect.objectContaining({ headers: expect.objectContaining({ Authorization: "Bearer id-token" }) }),
    );
    vi.unstubAllGlobals();
  });

  it("posts a Kubernetes-shaped policy to the namespaced collection route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ apiVersion: "opendepot.defdev.io/v1alpha1", kind: "ScanPolicy" }), { status: 201 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await createScanPolicy("platform", {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "baseline", namespace: "platform" },
      spec: { priority: 10, severityThreshold: "HIGH", exemptions: [{ reason: "Tracked", scanTypes: ["source"] }] },
    });

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/opendepot/ui/v1/scan-policies/platform"),
      expect.objectContaining({
        method: "POST",
        body: expect.stringContaining('"severityThreshold":"HIGH"'),
      }),
    );
    expect(fetchMock.mock.calls[0][1].body).not.toContain("failOnSeverity");
    vi.unstubAllGlobals();
  });

  it("sends optimistic concurrency headers on updates", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ namespace: "platform", name: "baseline", resourceVersion: "v2" }), { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await updateScanPolicy(
      "platform",
      "baseline",
      {
        apiVersion: "opendepot.defdev.io/v1alpha1",
        kind: "ScanPolicy",
        metadata: { name: "baseline", namespace: "platform" },
        spec: { priority: 0, severityThreshold: "HIGH", exemptions: [] },
      },
      "v1",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/opendepot/ui/v1/scan-policies/platform/baseline"),
      expect.objectContaining({
        method: "PUT",
        headers: expect.objectContaining({ "If-Match": "v1", "Content-Type": "application/json" }),
        body: expect.stringContaining('"resourceVersion":"v1"'),
      }),
    );
    vi.unstubAllGlobals();
  });

  it("preserves upstream conflict responses for callers", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: "conflict" }), { status: 409, statusText: "Conflict" })),
    );

    await expect(
      updateScanPolicy(
        "platform",
        "baseline",
        {
          apiVersion: "opendepot.defdev.io/v1alpha1",
          kind: "ScanPolicy",
          metadata: { name: "baseline", namespace: "platform" },
          spec: { priority: 0, severityThreshold: "HIGH", exemptions: [] },
        },
        "stale",
      ),
    ).rejects.toBeInstanceOf(ApiRequestError);
    vi.unstubAllGlobals();
  });
});
describe("listJevReviewQueue", () => {
  it("keeps Skills and Agents whose assessment needs review, leaving out active exemptions across pages", async () => {
    const card = (kind: string, name: string) => ({ kind, namespace: "platform", name });
    const detail = (kind: string, name: string, jevAssessment?: object) => ({ ...card(kind, name), jevAssessment });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = new URL(String(input), "http://localhost");
      if (url.pathname === "/opendepot/ui/v1/resources") {
        const kind = url.searchParams.get("kind");
        const page = url.searchParams.get("page");
        if (kind === "skill" && page === "1") {
          return new Response(JSON.stringify({ items: [card("skill", "critical-skill"), card("skill", "quiet-skill")], totalCount: 3, page: 1, pageSize: 2 }));
        }
        if (kind === "skill" && page === "2") {
          return new Response(JSON.stringify({
            items: [card("skill", "exempt-skill"), card("skill", "lapsed-skill"), card("skill", "active-exempt-skill")],
            totalCount: 5,
            page: 2,
            pageSize: 3,
          }));
        }
        return new Response(JSON.stringify({ items: [card("agent", "review-agent")], totalCount: 1, page: 1, pageSize: 100 }));
      }
      const name = decodeURIComponent(url.pathname.split("/").pop() ?? "");
      const details: Record<string, object> = {
        "critical-skill": detail("skill", "critical-skill", { needsReview: true, blocked: true, riskLevel: "Critical", exempted: false }),
        "quiet-skill": detail("skill", "quiet-skill", { needsReview: false, blocked: false, riskLevel: "Low" }),
        "exempt-skill": detail("skill", "exempt-skill", { needsReview: true, blocked: false, riskLevel: "Critical", exempted: true }),
        "lapsed-skill": detail("skill", "lapsed-skill", {
          needsReview: true,
          blocked: false,
          riskLevel: "Critical",
          exempted: true,
          exemptionExpires: "2020-01-01T00:00:00Z",
        }),
        "active-exempt-skill": detail("skill", "active-exempt-skill", {
          needsReview: true,
          blocked: false,
          riskLevel: "High",
          exempted: true,
          exemptionExpires: "2999-01-01T00:00:00Z",
        }),
        "review-agent": detail("agent", "review-agent", { needsReview: true, blocked: false, riskLevel: "Moderate" }),
      };
      return new Response(JSON.stringify(details[name]));
    });

    const queue = await listJevReviewQueue("token");

    expect(queue.items).toEqual([
      { kind: "skill", namespace: "platform", name: "critical-skill", riskLevel: "Critical", blocked: true, exempted: false },
      { kind: "skill", namespace: "platform", name: "lapsed-skill", riskLevel: "Critical", blocked: false, exempted: true },
      { kind: "agent", namespace: "platform", name: "review-agent", riskLevel: "Moderate", blocked: false, exempted: false },
    ]);
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("kind=skill&page=2"))).toBe(true);
  });
});
