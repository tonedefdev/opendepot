import { describe, expect, it, vi } from "vitest";
import {
  ApiRequestError,
  createScanPolicy,
  getScanPolicyCatalog,
  getScanPolicyCapabilities,
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