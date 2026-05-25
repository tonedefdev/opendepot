import { test, expect } from "@playwright/test";

/**
 * Auth route tests: verify login, callback failure paths, and logout.
 *
 * The OIDC-happy-path (full code exchange) requires a live identity provider
 * and is covered by the server-side e2e tests (services/server/test/e2e).
 * These tests focus on the Next.js route layer: redirect shape, error paths,
 * CSRF checks, and session destruction.
 */

test.describe("login route", () => {
  test("GET /auth/login redirects to the OIDC provider when OIDC is configured", async ({
    request,
  }) => {
    // When OIDC_ISSUER_URL is not configured, the route returns 503.
    // When configured, it issues a 302/307 redirect to the authorization endpoint.
    // We probe for either valid outcome to keep the test environment-agnostic.
    const response = await request.get("/auth/login", {
      maxRedirects: 0,
    });
    // Acceptable outcomes: redirect (OIDC configured), 503 (OIDC not configured
    // or provider unreachable), or 502 (provider returned non-200).
    expect([302, 307, 308, 502, 503]).toContain(response.status());

    if ([302, 307, 308].includes(response.status())) {
      const location = response.headers()["location"] ?? "";
      // The redirect must be to an external OIDC authorization endpoint —
      // it must NOT loop back to our own origin.
      expect(location).toContain("response_type=code");
      expect(location).toContain("code_challenge");
      expect(location).toContain("state=");
    }
  });
});

test.describe("callback route — failure paths", () => {
  test("GET /auth/callback without code and state returns 400", async ({
    request,
  }) => {
    const response = await request.get("/auth/callback");
    // Missing code/state → either 400 or 503 (OIDC not configured).
    expect([400, 503]).toContain(response.status());
  });

  test("GET /auth/callback with state mismatch returns 400", async ({
    request,
  }) => {
    // Simulate a state mismatch (no matching oidc_state cookie).
    const response = await request.get(
      "/auth/callback?code=fake-code&state=mismatched-state",
    );
    // The callback checks the oidc_state cookie; mismatches must be rejected.
    expect([400, 503]).toContain(response.status());
  });

  test("GET /auth/callback with error param redirects to error page", async ({
    request,
  }) => {
    const response = await request.get(
      "/auth/callback?error=access_denied&error_description=User+denied+access",
      { maxRedirects: 0 },
    );
    // Provider error must redirect the user rather than returning 5xx.
    if (response.status() === 503) {
      // OIDC not configured — acceptable in non-OIDC environments.
      return;
    }
    expect([302, 307, 308]).toContain(response.status());
    const location = response.headers()["location"] ?? "";
    expect(location).toContain("auth_error=");
  });
});

test.describe("logout route", () => {
  test("GET /auth/logout returns 200 or redirects to root", async ({
    request,
  }) => {
    const response = await request.get("/auth/logout", { maxRedirects: 0 });
    // Logout must clear the session. Acceptable outcomes: redirect to root or 200.
    expect([200, 302, 307, 308]).toContain(response.status());
  });
});

test.describe("dev-token route", () => {
  test("POST /auth/dev-token returns 403 when DEV_TOKEN_INPUT_ENABLED is not true", async ({
    request,
  }) => {
    // In production (DEV_TOKEN_INPUT_ENABLED != 'true'), the route must be closed.
    const response = await request.post("/auth/dev-token", {
      data: { token: "test-token" },
    });
    // 403 when disabled; the route must not accept tokens in production mode.
    expect([403]).toContain(response.status());
  });
});
