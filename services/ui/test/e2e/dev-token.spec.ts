import { test, expect } from "@playwright/test";

/**
 * Dev token input tests: verify the developer token input behavior.
 *
 * The dev token input field is guarded by DEV_TOKEN_INPUT_ENABLED=true.
 * In production (or when the flag is not set) the field must not be present.
 * These tests verify the production guard; the active-dev-token flow is
 * exercised separately in environments where DEV_TOKEN_INPUT_ENABLED=true.
 */

test.describe("dev token input — production guard", () => {
  test("dev token input field is not visible in production mode", async ({
    page,
  }) => {
    // Abort secondary resources to avoid overwhelming kubectl port-forward.
    // The dev-token guard is reflected in the SSR'd HTML, so JS is not needed.
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

    // The dev token input must not be rendered unless explicitly enabled.
    const devInput = page.locator('[data-testid="dev-token-input"]');
    const count = await devInput.count();
    expect(count).toBe(0);
  });

  test("GET / does not expose DEV_TOKEN_INPUT_ENABLED env var in page source", async ({
    request,
  }) => {
    const response = await request.get("/");
    expect(response.status()).toBe(200);
    const body = await response.text();
    // The env var value itself must not appear verbatim in the rendered HTML.
    expect(body).not.toContain("DEV_TOKEN_INPUT_ENABLED");
  });
});
