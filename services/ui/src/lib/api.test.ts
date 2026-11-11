import { describe, expect, it, vi } from "vitest";
import {
  ApiRequestError,
  createScanPolicy,
  getScanPolicyCatalog,
  getScanPolicyCapabilities,
  listJevReviewQueue,
  previewScanPolicy,
  readAssemblyStream,
  updateScanPolicy,
} from "./api";

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
  it("keeps only Skills and Agents whose assessment needs review, across pages", async () => {
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
          return new Response(JSON.stringify({ items: [card("skill", "exempt-skill")], totalCount: 3, page: 2, pageSize: 2 }));
        }
        return new Response(JSON.stringify({ items: [card("agent", "review-agent")], totalCount: 1, page: 1, pageSize: 100 }));
      }
      const name = decodeURIComponent(url.pathname.split("/").pop() ?? "");
      const details: Record<string, object> = {
        "critical-skill": detail("skill", "critical-skill", { needsReview: true, blocked: true, riskLevel: "Critical", exempted: false }),
        "quiet-skill": detail("skill", "quiet-skill", { needsReview: false, blocked: false, riskLevel: "Low" }),
        "exempt-skill": detail("skill", "exempt-skill", { needsReview: true, blocked: false, riskLevel: "Critical", exempted: true }),
        "review-agent": detail("agent", "review-agent", { needsReview: true, blocked: false, riskLevel: "Moderate" }),
      };
      return new Response(JSON.stringify(details[name]));
    });

    const queue = await listJevReviewQueue("token");

    expect(queue.items).toEqual([
      { kind: "skill", namespace: "platform", name: "critical-skill", riskLevel: "Critical", blocked: true, exempted: false },
      { kind: "skill", namespace: "platform", name: "exempt-skill", riskLevel: "Critical", blocked: false, exempted: true },
      { kind: "agent", namespace: "platform", name: "review-agent", riskLevel: "Moderate", blocked: false, exempted: false },
    ]);
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("kind=skill&page=2"))).toBe(true);
  });
});
