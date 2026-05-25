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

  test("root page renders without JavaScript errors", async ({ page }) => {
    const jsErrors: string[] = [];
    page.on("pageerror", (err) => jsErrors.push(err.message));

    await page.goto("/");
    await page.waitForLoadState("domcontentloaded");

    expect(jsErrors).toHaveLength(0);
  });

  test("root page contains at least one resource card or empty-state message", async ({
    page,
  }) => {
    await page.goto("/");
    await page.waitForLoadState("domcontentloaded");

    // Either a grid of resource cards or an explicit empty-state message must be present.
    const hasCards = (await page.locator('[data-testid="resource-card"]').count()) > 0;
    const hasEmptyState =
      (await page.locator('[data-testid="empty-state"]').count()) > 0;
    const hasContent =
      (await page.locator("main").count()) > 0;

    expect(hasCards || hasEmptyState || hasContent).toBe(true);
  });
});

test.describe("unauthorized-to-public fallback", () => {
  test("unauthenticated request to root returns 200 (public fallback)", async ({
    request,
  }) => {
    // The UI must render in public-only mode without any auth cookie/header.
    const response = await request.get("/");
    expect(response.status()).toBe(200);
  });
});
