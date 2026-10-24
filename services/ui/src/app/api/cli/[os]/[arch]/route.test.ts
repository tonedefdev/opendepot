import { NextRequest } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GET } from "./route";

function callRoute(os: string, arch: string) {
  return GET(new NextRequest(`http://localhost/api/cli/${os}/${arch}`), {
    params: Promise.resolve({ os, arch }),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("CLI download proxy", () => {
  it("rejects platforms outside the allowlist without calling the server", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await callRoute("linux", "386");

    expect(response.status).toBe(404);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("streams a supported binary with download headers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("binary", {
        status: 200,
        headers: {
          "Content-Type": "application/octet-stream",
          "Content-Disposition": 'attachment; filename="opendepot_linux_amd64"',
        },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const response = await callRoute("linux", "amd64");

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/opendepot\/cli\/v1\/linux\/amd64$/),
      expect.objectContaining({ cache: "no-store" }),
    );
    expect(response.status).toBe(200);
    expect(response.headers.get("Content-Disposition")).toBe('attachment; filename="opendepot_linux_amd64"');
    expect(response.headers.get("X-Content-Type-Options")).toBe("nosniff");
    expect(await response.text()).toBe("binary");
  });

  it("passes through a missing artifact as 404", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("not found", { status: 404 })));

    const response = await callRoute("darwin", "arm64");

    expect(response.status).toBe(404);
  });

  it("returns 502 when the server cannot be reached", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("connection refused")));

    const response = await callRoute("windows", "amd64");

    expect(response.status).toBe(502);
  });
});
