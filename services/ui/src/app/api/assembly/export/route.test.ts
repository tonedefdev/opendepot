import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getServerSessionToken } from "@/lib/session";
import { POST } from "./route";

vi.mock("@/lib/session", () => ({
  getServerSessionToken: vi.fn(),
}));

const exportBody = JSON.stringify({ modules: [], providers: [] });

function exportRequest(headers: Record<string, string> = {}) {
  return new NextRequest("http://localhost/api/assembly/export", {
    method: "POST",
    headers: { "Content-Type": "application/json", ...headers },
    body: exportBody,
  });
}

beforeEach(() => {
  vi.mocked(getServerSessionToken).mockResolvedValue(undefined);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("assembly export proxy", () => {
  it("forwards the request body and default Accept header without a session token", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("zip-bytes", { status: 200, headers: { "Content-Type": "application/zip" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await POST(exportRequest());

    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/\/opendepot\/ui\/v1\/assembly\/export$/),
      expect.objectContaining({
        method: "POST",
        body: exportBody,
        cache: "no-store",
        headers: {
          Accept: "application/zip, application/json",
          "Content-Type": "application/json",
        },
      }),
    );
  });

  it("forwards the caller Accept header and the session token upstream", async () => {
    vi.mocked(getServerSessionToken).mockResolvedValue("session-token");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("zip-bytes", { status: 200, headers: { "Content-Type": "application/zip" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await POST(exportRequest({ Accept: "application/zip" }));

    const headers = (fetchMock.mock.calls[0][1] as RequestInit).headers as Record<string, string>;
    expect(headers.Accept).toBe("application/zip");
    expect(headers.Authorization).toContain("session-token");
  });

  it("streams NDJSON responses and forwards download and buffering headers", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response('{"type":"started"}\n', {
          status: 200,
          headers: {
            "Content-Type": "application/x-ndjson",
            "Content-Disposition": 'attachment; filename="assembly.ndjson"',
            "X-Accel-Buffering": "no",
          },
        }),
      ),
    );

    const response = await POST(exportRequest());

    expect(response.status).toBe(200);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    expect(response.headers.get("Content-Type")).toBe("application/x-ndjson");
    expect(response.headers.get("Content-Disposition")).toBe('attachment; filename="assembly.ndjson"');
    expect(response.headers.get("X-Accel-Buffering")).toBe("no");
    expect(await response.text()).toBe('{"type":"started"}\n');
  });

  it("returns the buffered binary body with the upstream content type", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("zip-bytes", { status: 200, headers: { "Content-Type": "application/zip" } }),
      ),
    );

    const response = await POST(exportRequest());

    expect(response.status).toBe(200);
    expect(response.headers.get("Content-Type")).toBe("application/zip");
    expect(response.headers.get("Content-Disposition")).toBeNull();
    expect(await response.text()).toBe("zip-bytes");
  });

  it("defaults the content type to JSON when upstream omits it", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 200 })));

    const response = await POST(exportRequest());

    expect(response.headers.get("Content-Type")).toBe("application/json");
  });

  it("passes through a JSON error body from upstream", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "invalid_assembly", message: "module is required" }), {
          status: 422,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const response = await POST(exportRequest());

    expect(response.status).toBe(422);
    expect(response.headers.get("Content-Type")).toBe("application/json");
    expect(await response.json()).toEqual({ error: "invalid_assembly", message: "module is required" });
  });

  it("wraps a plain-text upstream error in an upstream_error payload", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("  gateway down  \n", { status: 502 })));

    const response = await POST(exportRequest());

    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ error: "upstream_error", message: "gateway down" });
  });

  it("uses a status-based message when the upstream error body is empty", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 500 })));

    const response = await POST(exportRequest());

    expect(response.status).toBe(500);
    expect(await response.json()).toEqual({
      error: "upstream_error",
      message: "Assembly export failed with status 500.",
    });
  });
});
