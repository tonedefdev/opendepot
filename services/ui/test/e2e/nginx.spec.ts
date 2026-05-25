import { test, expect } from "@playwright/test";

/**
 * NGINX routing tests: verify that /opendepot/* and /.well-known/* are proxied
 * to the server service, and that root paths serve the Next.js UI.
 *
 * These tests run against the full NGINX + Next.js stack (PLAYWRIGHT_BASE_URL
 * must point at the NGINX listener, not directly at Next.js port 3000).
 *
 * When the backing OpenDepot server service is not deployed (e.g. local dev
 * without a cluster), the /opendepot/* and /.well-known/* assertions check for
 * a proxy error response rather than server content — both outcomes confirm
 * that NGINX is routing correctly (it's reaching the upstream or reporting that
 * it cannot).
 */

test.describe("NGINX routing — root", () => {
  test("GET / routes to Next.js UI and returns HTML", async ({ request }) => {
    const response = await request.get("/");
    expect(response.status()).toBe(200);

    const contentType = response.headers()["content-type"] ?? "";
    expect(contentType).toContain("text/html");
  });
});

test.describe("NGINX routing — server protocol paths", () => {
  test("GET /.well-known/terraform.json does not return HTML (proxied to server)", async ({
    request,
  }) => {
    const response = await request.get("/.well-known/terraform.json");
    // The server returns JSON for terraform.json; NGINX must not serve the
    // Next.js HTML page for this path.
    // Acceptable: 200 with JSON, or 502/504 when server is unreachable —
    // any of these confirms the route was not handled by Next.js.
    expect([200, 502, 504]).toContain(response.status());

    if (response.status() === 200) {
      const contentType = response.headers()["content-type"] ?? "";
      // Must be JSON, not HTML.
      expect(contentType).not.toContain("text/html");
    }
  });

  test("GET /opendepot/ui/v1/namespaces does not return Next.js HTML", async ({
    request,
  }) => {
    const response = await request.get("/opendepot/ui/v1/namespaces");
    // Acceptable: server response (JSON 200/401) or proxy error (502/504).
    // Any status except a Next.js 200 HTML page confirms NGINX is routing correctly.
    if (response.status() === 200) {
      const contentType = response.headers()["content-type"] ?? "";
      expect(contentType).not.toContain("text/html");
    } else {
      // Non-200 confirms the request was proxied to the server, not handled by Next.js.
      expect([401, 403, 502, 504]).toContain(response.status());
    }
  });
});

test.describe("NGINX proxy headers", () => {
  test("response from root includes expected security headers from Next.js", async ({
    request,
  }) => {
    const response = await request.get("/");
    expect(response.status()).toBe(200);
    // Next.js sets X-Content-Type-Options by default.
    const xct = response.headers()["x-content-type-options"];
    expect(xct).toBe("nosniff");
  });
});
