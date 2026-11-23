import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, deleteScanPolicy, getScanPolicy, updateScanPolicy, type ScanPolicyMutation } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { DELETE, GET, PUT } from "./route";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    getScanPolicy: vi.fn(),
    updateScanPolicy: vi.fn(),
    deleteScanPolicy: vi.fn(),
  };
});

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

const params = Promise.resolve({ namespace: "platform", name: "baseline" });

const mutation: ScanPolicyMutation = {
  apiVersion: "opendepot.defdev.io/v1alpha1",
  kind: "ScanPolicy",
  metadata: { name: "baseline", namespace: "platform" },
  spec: { severityThreshold: "HIGH" },
} as ScanPolicyMutation;

function routeRequest(method: string, body?: unknown, headers: Record<string, string> = {}) {
  return new Request("http://localhost/api/security-policies/platform/baseline", {
    method,
    headers: body === undefined ? headers : { "Content-Type": "application/json", ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("security policy item GET", () => {
  it("returns the policy with the session token", async () => {
    vi.mocked(getScanPolicy).mockResolvedValue({ metadata: { name: "baseline" } } as never);

    const response = await GET(routeRequest("GET"), { params });

    expect(getScanPolicy).toHaveBeenCalledWith("platform", "baseline", "session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ metadata: { name: "baseline" } });
  });

  it("maps an upstream 404 to the upstream_error code and status", async () => {
    vi.mocked(getScanPolicy).mockRejectedValue(new ApiRequestError("policy not found", 404, { reason: "NotFound" }));

    const response = await GET(routeRequest("GET"), { params });

    expect(response.status).toBe(404);
    expect(await response.json()).toEqual({
      code: "upstream_error",
      message: "policy not found",
      details: { reason: "NotFound" },
    });
  });

  it("returns a 502 internal_error for unexpected failures", async () => {
    vi.mocked(getScanPolicy).mockRejectedValue(new Error("socket hang up"));

    const response = await GET(routeRequest("GET"), { params });

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: "internal_error",
      message: "The policy service is unavailable.",
    });
  });
});

describe("security policy item PUT", () => {
  it("requires If-Match before calling the server", async () => {
    const response = await PUT(routeRequest("PUT", mutation), { params });

    expect(response.status).toBe(428);
    expect(await response.json()).toEqual({ code: "missing_if_match", message: "If-Match is required." });
    expect(updateScanPolicy).not.toHaveBeenCalled();
  });

  it("updates the policy with the resource version and returns the result", async () => {
    vi.mocked(updateScanPolicy).mockResolvedValue({ metadata: { name: "baseline", resourceVersion: "8" } } as never);

    const response = await PUT(routeRequest("PUT", mutation, { "If-Match": "7" }), { params });

    expect(updateScanPolicy).toHaveBeenCalledWith("platform", "baseline", mutation, "7", "session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ metadata: { name: "baseline", resourceVersion: "8" } });
  });

  it("maps an upstream conflict to a conflict code", async () => {
    vi.mocked(updateScanPolicy).mockRejectedValue(new ApiRequestError("stale resourceVersion", 409, { current: "9" }));

    const response = await PUT(routeRequest("PUT", mutation, { "If-Match": "7" }), { params });

    expect(response.status).toBe(409);
    expect(await response.json()).toEqual({
      code: "conflict",
      message: "stale resourceVersion",
      details: { current: "9" },
    });
  });

  it("returns 502 internal_error when the request body is not valid JSON", async () => {
    const request = new Request("http://localhost/api/security-policies/platform/baseline", {
      method: "PUT",
      headers: { "If-Match": "7", "Content-Type": "application/json" },
      body: "{not json",
    });

    const response = await PUT(request, { params });

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: "internal_error",
      message: "The policy service is unavailable.",
    });
    expect(updateScanPolicy).not.toHaveBeenCalled();
  });
});

describe("security policy item DELETE", () => {
  it("requires If-Match before calling the server", async () => {
    const response = await DELETE(routeRequest("DELETE"), { params });

    expect(response.status).toBe(428);
    expect(deleteScanPolicy).not.toHaveBeenCalled();
  });

  it("deletes the policy and returns 204 with no body", async () => {
    vi.mocked(deleteScanPolicy).mockResolvedValue(undefined);

    const response = await DELETE(routeRequest("DELETE", undefined, { "If-Match": "7" }), { params });

    expect(deleteScanPolicy).toHaveBeenCalledWith("platform", "baseline", "7", "session-token");
    expect(response.status).toBe(204);
    expect(await response.text()).toBe("");
  });

  it("maps an upstream failure to its status", async () => {
    vi.mocked(deleteScanPolicy).mockRejectedValue(new ApiRequestError("forbidden", 403));

    const response = await DELETE(routeRequest("DELETE", undefined, { "If-Match": "7" }), { params });

    expect(response.status).toBe(403);
    expect(await response.json()).toEqual({ code: "upstream_error", message: "forbidden", details: undefined });
  });
});
