import { test, expect } from "@playwright/test";

/**
 * Full OIDC login flow tests.
 *
 * These tests exercise the complete browser-facing auth loop: /auth/login →
 * Dex login form → /auth/callback → session established → protected content
 * visible. They require a live Dex identity provider and a fully deployed
 * OpenDepot stack (server + UI + GroupBinding resources).
 *
 * All tests are skipped unless PLAYWRIGHT_OIDC_ENABLED=true is set.
 *
 * To run locally against the Kind dev cluster:
 *
 *   PLAYWRIGHT_BASE_URL=http://opendepot.localtest.me:8080 \
 *   PLAYWRIGHT_OIDC_ENABLED=true \
 *   PLAYWRIGHT_OIDC_USERNAME=dev@example.com \
 *   PLAYWRIGHT_OIDC_PASSWORD=password \
 *   yarn test:e2e test/e2e/oidc-flow.spec.ts
 */

const oidcEnabled = process.env.PLAYWRIGHT_OIDC_ENABLED === "true";
const oidcUsername = process.env.PLAYWRIGHT_OIDC_USERNAME ?? "dev@example.com";
const oidcPassword = process.env.PLAYWRIGHT_OIDC_PASSWORD ?? "password";

/**
 * performLogin navigates to /auth/login, fills the Dex credential form, and
 * waits until the browser lands back on the root page. It assumes Dex is
 * serving its login form at a URL matching /dex/auth/.
 */
async function performLogin(page: import("@playwright/test").Page) {
  await page.goto("/auth/login", { waitUntil: "domcontentloaded" });
  await page.waitForURL(/\/dex\/auth\//);
  await page.fill('input[name="login"]', oidcUsername);
  await page.fill('input[name="password"]', oidcPassword);
  await page.click('button[type="submit"]');
  await page.waitForURL("/");
  await page.waitForLoadState("domcontentloaded");
}

test.describe("OIDC login — session cookie persistence", () => {
  // Regression tests for: iron-session cookie being wiped when the callback
  // route cleared PKCE cookies via ResponseCookies.set(), which overwrote the
  // Set-Cookie header that iron-session had already appended to the response.
  //
  // Fix: callback route uses response.headers.append() for PKCE clearing
  // instead of response.cookies.set(), so iron-session's header is preserved.

  test.skip(!oidcEnabled, "set PLAYWRIGHT_OIDC_ENABLED=true to run");

  test("opendepot_session cookie is present after OIDC callback", async ({
    page,
    context,
  }) => {
    await performLogin(page);

    const cookies = await context.cookies();
    const sessionCookie = cookies.find((c) => c.name === "opendepot_session");

    expect(sessionCookie).toBeDefined();
    expect(sessionCookie!.value.length).toBeGreaterThan(0);
  });

  test("PKCE one-time-use cookies are cleared after OIDC callback", async ({
    page,
    context,
  }) => {
    await performLogin(page);

    const cookies = await context.cookies();
    for (const name of ["oidc_state", "oidc_nonce", "oidc_cv"]) {
      const cookie = cookies.find((c) => c.name === name);
      expect(cookie).toBeUndefined();
    }
  });
});

test.describe("OIDC login — no immediate logout via prefetch", () => {
  // Regression test for: Next.js App Router prefetching <Link href="/auth/logout">
  // which called session.destroy() immediately after the sidebar rendered with
  // user info, wiping the session before the user interacted with the page.
  //
  // Fix: logout button uses component="a" (plain anchor) which is never
  // prefetched by the App Router.

  test.skip(!oidcEnabled, "set PLAYWRIGHT_OIDC_ENABLED=true to run");

  test("session cookie persists after page reload post-login", async ({
    page,
    context,
  }) => {
    await performLogin(page);

    // Wait for any deferred prefetch requests to complete before reloading.
    await page.waitForLoadState("networkidle");

    // Reload and check the session is still intact — the prefetch bug would
    // have wiped the cookie between the first render and this reload.
    await page.reload();
    await page.waitForLoadState("networkidle");

    const cookies = await context.cookies();
    const sessionCookie = cookies.find((c) => c.name === "opendepot_session");

    expect(sessionCookie).toBeDefined();
    expect(sessionCookie!.value.length).toBeGreaterThan(0);
  });

  test("page does not navigate to /auth/login after initial login", async ({
    page,
  }) => {
    await performLogin(page);
    await page.waitForLoadState("networkidle");

    // A session wipe from prefetch would cause the next render to redirect to
    // /auth/login. Assert we remain on the root.
    expect(page.url()).not.toContain("/auth/login");
  });

  test("no request to /auth/logout fires during page load after login", async ({
    page,
  }) => {
    const logoutRequests: string[] = [];
    page.on("request", (req) => {
      if (req.url().includes("/auth/logout")) {
        logoutRequests.push(req.url());
      }
    });

    await performLogin(page);
    await page.waitForLoadState("networkidle");

    expect(logoutRequests).toHaveLength(0);
  });
});

test.describe("OIDC login — GroupBinding module access", () => {
  // Regression test for: server rejecting UI OIDC tokens (aud: opendepot-ui)
  // because the primary OIDC verifier expected aud: opendepot. The token fell
  // through to browseAuthClientCredentials which used virtual groups
  // (client:<sub>) that never match GroupBinding expressions, producing an
  // empty module list even for authorized users.
  //
  // Fix: server creates a second verifier (oidcUIVerifier) configured with the
  // UI client ID. browseAuthState tries this verifier before falling back to
  // the client-credentials path, so user groups are correctly extracted from
  // the token and evaluated against GroupBinding resources.

  test.skip(!oidcEnabled, "set PLAYWRIGHT_OIDC_ENABLED=true to run");

  test("at least one resource card is visible after login for a user with a GroupBinding", async ({
    page,
  }) => {
    await performLogin(page);
    await page.waitForLoadState("networkidle");

    // A user whose groups match a GroupBinding expression must see resources.
    // An empty list here indicates GroupBinding evaluation failed (audience
    // mismatch, group extraction failure, or missing --oidc-ui-client-id flag).
    const card = page.locator('[data-testid="resource-card"]').first();
    await expect(card).toBeVisible({ timeout: 10_000 });
  });
});
