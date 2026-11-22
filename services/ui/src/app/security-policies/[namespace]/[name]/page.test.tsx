import * as React from "react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SecurityPoliciesSurface from "@/components/security-policies/SecurityPoliciesSurface";
import { getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import SecurityPolicyDetailPage from "./page";
Object.assign(globalThis, { React });

vi.mock("@/components/security-policies/SecurityPoliciesSurface", () => ({
  default: () => null,
}));

vi.mock("@/lib/api", () => ({
  getScanPolicyCatalog: vi.fn(),
}));

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

const params = Promise.resolve({ namespace: "platform", name: "baseline" });

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(getServerSessionToken).mockResolvedValue("session-token" as never);
});

describe("SecurityPolicyDetailPage", () => {
  it("renders the edit surface with the loaded catalog", async () => {
    const catalog = { writesEnabled: true, items: [] };
    vi.mocked(getScanPolicyCatalog).mockResolvedValue(catalog as never);

    const element = (await SecurityPolicyDetailPage({ params })) as ReactElement<Record<string, unknown>>;

    expect(getScanPolicyCatalog).toHaveBeenCalledWith("session-token");
    expect(element.type).toBe(SecurityPoliciesSurface);
    expect(element.props).toEqual({
      mode: "edit",
      namespace: "platform",
      name: "baseline",
      catalog,
    });
  });

  it("falls back to a read-only empty catalog when loading fails", async () => {
    vi.mocked(getScanPolicyCatalog).mockRejectedValue(new Error("offline"));

    const element = (await SecurityPolicyDetailPage({ params })) as ReactElement<Record<string, unknown>>;

    expect(element.props["catalog"]).toEqual({ writesEnabled: false, items: [] });
  });
});
