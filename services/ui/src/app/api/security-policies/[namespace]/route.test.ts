import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, createScanPolicy, listScanPolicies, type ScanPolicyMutation } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { GET, POST } from "./route";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    listScanPolicies: vi.fn(),
    createScanPolicy: vi.fn(),
  };
});

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

const params = Promise.resolve({ namespace: "platform" });

const mutation = {
  apiVersion: "opendepot.defdev.io/v1alpha1",
  kind: "ScanPolicy",
  metadata: { name: "baseline", namespace: "platform" },
  spec: { severityThreshold: "HIGH" },
} as ScanPolicyMutation;

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("security policy collection GET", () => {
  it("lists policies in the namespace with the session token", async () => {
    vi.mocked(listScanPolicies).mockResolvedValue({ items: [{ metadata: { name: "baseline" } }] } as never);

    const response = await GET(new Request("http://localhost/api/security-policies/platform"), { params });

    expect(listScanPolicies).toHaveBeenCalledWith("platform", "session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ items: [{ metadata: { name: "baseline" } }] });
  });

  it("maps upstream errors to their status", async () => {
    vi.mocked(listScanPolicies).mockRejectedValue(new ApiRequestError("forbidden", 403));

    const response = await GET(new Request("http://localhost/api/security-policies/platform"), { params });

    expect(response.status).toBe(403);
    expect(await response.json()).toEqual({ code: "upstream_error", message: "forbidden", details: undefined });
  });

  it("returns 502 for unexpected failures", async () => {
    vi.mocked(listScanPolicies).mockRejectedValue(new TypeError("fetch failed"));

    const response = await GET(new Request("http://localhost/api/security-policies/platform"), { params });

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: "internal_error",
      message: "The policy service is unavailable.",
    });
  });
});

describe("security policy collection POST", () => {
  it("creates the policy and returns 201", async () => {
    vi.mocked(createScanPolicy).mockResolvedValue({ metadata: { name: "baseline" } } as never);

    const response = await POST(
      new Request("http://localhost/api/security-policies/platform", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(mutation),
      }),
      { params },
    );

    expect(createScanPolicy).toHaveBeenCalledWith("platform", mutation, "session-token");
    expect(response.status).toBe(201);
    expect(await response.json()).toEqual({ metadata: { name: "baseline" } });
  });

  it("maps a conflicting create to the conflict code", async () => {
    vi.mocked(createScanPolicy).mockRejectedValue(new ApiRequestError("already exists", 409));

    const response = await POST(
      new Request("http://localhost/api/security-policies/platform", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(mutation),
      }),
      { params },
    );

    expect(response.status).toBe(409);
    expect(await response.json()).toEqual({ code: "conflict", message: "already exists", details: undefined });
  });
});
