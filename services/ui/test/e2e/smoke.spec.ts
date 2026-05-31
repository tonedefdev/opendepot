import { test, expect } from "@playwright/test";

/**
 * Smoke tests: verify the landing page and detail navigation load correctly.
 * These tests require a running Next.js + NGINX server (PLAYWRIGHT_BASE_URL).
 */

test.describe("landing page", () => {
  test("root path returns HTTP 200", async ({ request }) => {
    const response = await request.get("/");
    expect(response.status()).toBe(200);
  });

  test("root page has a main element", async ({ page }) => {
    // Abort secondary resources to avoid overwhelming kubectl port-forward.
    // Next.js SSR renders the full DOM server-side, so aborting scripts does
    // not affect the structural assertions below.
    await page.route("**/*", (route) => {
      if (
        ["script", "stylesheet", "image", "font", "media"].includes(
          route.request().resourceType(),
        )
      ) {
        route.abort();
      } else {
        route.continue();
      }
    });

    await page.goto("/", { waitUntil: "domcontentloaded" });

    // The root page must have a <main> element regardless of auth state.
    await expect(page.locator("main").first()).toBeVisible({ timeout: 5000 });
  });
});

test.describe("unauthorized-to-public fallback", () => {
  test("unauthenticated request to root returns 200 (public fallback)", async ({
    page,
  }) => {
    // The UI must render in public-only mode without any auth cookie/header.
    // Abort secondary resources to avoid overwhelming kubectl port-forward.
    await page.route("**/*", (route) => {
      if (
        ["script", "stylesheet", "image", "font", "media"].includes(
          route.request().resourceType(),
        )
      ) {
        route.abort();
      } else {
        route.continue();
      }
    });

    const response = await page.goto("/", { waitUntil: "domcontentloaded" });
    expect(response?.status()).toBe(200);
  });
});

test.describe("depots page", () => {
  test("GET /depots returns a non-error status (200 or redirect)", async ({
    request,
  }) => {
    // /depots is a protected page; unauthenticated requests will be redirected
    // to the OIDC provider. We only verify the server responds without a 5xx
    // error — auth-gated content is covered by the OIDC flow suite.
    const response = await request.get("/depots", { maxRedirects: 0 });
    expect(response.status()).toBeLessThan(500);
  });
});
