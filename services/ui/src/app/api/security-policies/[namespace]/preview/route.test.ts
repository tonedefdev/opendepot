import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, previewScanPolicy, type ScanPolicyMutation } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { POST } from "./route";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    previewScanPolicy: vi.fn(),
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

function previewRequest() {
  return new Request("http://localhost/api/security-policies/platform/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(mutation),
  });
}

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("security policy preview POST", () => {
  it("returns the server preview for the submitted policy", async () => {
    vi.mocked(previewScanPolicy).mockResolvedValue({ valid: true, policy: mutation } as never);

    const response = await POST(previewRequest(), { params });

    expect(previewScanPolicy).toHaveBeenCalledWith("platform", mutation, "session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ valid: true, policy: mutation });
  });

  it("passes through the upstream status with a preview_unavailable code", async () => {
    vi.mocked(previewScanPolicy).mockRejectedValue(new ApiRequestError("invalid spec", 422));

    const response = await POST(previewRequest(), { params });

    expect(response.status).toBe(422);
    expect(await response.json()).toEqual({ code: "preview_unavailable", message: "Policy preview failed." });
  });

  it("returns 502 when the preview fails for any other reason", async () => {
    vi.mocked(previewScanPolicy).mockRejectedValue(new Error("network"));

    const response = await POST(previewRequest(), { params });

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ code: "preview_unavailable", message: "Policy preview failed." });
  });
});
