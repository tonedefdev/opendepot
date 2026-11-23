import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { GET } from "./route";

vi.mock("@/lib/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api")>();
  return {
    ...actual,
    getScanPolicyCatalog: vi.fn(),
  };
});

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("security policy catalog GET", () => {
  it("returns the policy catalog with the session token", async () => {
    vi.mocked(getScanPolicyCatalog).mockResolvedValue({ writesEnabled: true, items: [] } as never);

    const response = await GET();

    expect(getScanPolicyCatalog).toHaveBeenCalledWith("session-token");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ writesEnabled: true, items: [] });
  });

  it("maps upstream errors to a generic catalog message with the upstream status", async () => {
    vi.mocked(getScanPolicyCatalog).mockRejectedValue(new ApiRequestError("internal", 500));

    const response = await GET();

    expect(response.status).toBe(500);
    expect(await response.json()).toEqual({
      code: "upstream_error",
      message: "The policy catalog could not be loaded.",
    });
  });

  it("returns 502 for unexpected failures", async () => {
    vi.mocked(getScanPolicyCatalog).mockRejectedValue(new Error("offline"));

    const response = await GET();

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({
      code: "internal_error",
      message: "The policy catalog is unavailable.",
    });
  });
});
