import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, getScanPolicyCapabilities } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { GET } from "./route";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    getScanPolicyCapabilities: vi.fn(),
  };
});

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

const params = Promise.resolve({ namespace: "platform" });

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("security policy capabilities GET", () => {
  it("returns the namespace capabilities", async () => {
    vi.mocked(getScanPolicyCapabilities).mockResolvedValue({ canWrite: true, supportsPreview: true } as never);

    const response = await GET(new Request("http://localhost/api/security-policies/platform/capabilities"), { params });

    expect(getScanPolicyCapabilities).toHaveBeenCalledWith("platform", "session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ canWrite: true, supportsPreview: true });
  });

  it("maps upstream errors to their status", async () => {
    vi.mocked(getScanPolicyCapabilities).mockRejectedValue(new ApiRequestError("unauthorized", 401));

    const response = await GET(new Request("http://localhost/api/security-policies/platform/capabilities"), { params });

    expect(response.status).toBe(401);
    expect(await response.json()).toEqual({
      code: "capabilities_unavailable",
      message: "Policy capabilities could not be loaded.",
    });
  });

  it("returns 502 for unexpected failures", async () => {
    vi.mocked(getScanPolicyCapabilities).mockRejectedValue(new Error("boom"));

    const response = await GET(new Request("http://localhost/api/security-policies/platform/capabilities"), { params });

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: "capabilities_unavailable",
      message: "Policy capabilities could not be loaded.",
    });
  });
});
