import { test, expect } from "@playwright/test";

/**
 * Theme toggle tests: verify the light/dark mode switch in the sidebar,
 * cookie-based persistence across reloads, and first-visit OS-preference
 * detection. Requires a running dev server (see playwright.config.ts).
 */

const COLOR_MODE_COOKIE = "opendepot_color_mode";

test.describe("theme toggle — desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("toggle button is visible in the expanded sidebar and switches the color scheme", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "networkidle" });

    const toggle = page.getByRole("button", { name: "Toggle color mode" }).first();
    await expect(toggle).toBeVisible();

    const before = await page.locator("html").getAttribute("data-mui-color-scheme");
    await toggle.click();

    await expect(page.locator("html")).not.toHaveAttribute("data-mui-color-scheme", before ?? "");
  });

  test("toggle is visible when the sidebar is collapsed", async ({ page }) => {
    await page.goto("/", { waitUntil: "networkidle" });

    await page.getByRole("button", { name: "Collapse sidebar" }).click();
    await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeVisible();

    const toggle = page.getByRole("button", { name: "Toggle color mode" }).first();
    await expect(toggle).toBeVisible();
  });

  test("preference persists across a full page reload via cookie", async ({
    page,
    context,
  }) => {
    await page.goto("/", { waitUntil: "networkidle" });

    const toggle = page.getByRole("button", { name: "Toggle color mode" }).first();
    await toggle.click();

    const scheme = await page.locator("html").getAttribute("data-mui-color-scheme");
    expect(scheme).toMatch(/^(light|dark)$/);

    const cookies = await context.cookies();
    const modeCookie = cookies.find((c) => c.name === COLOR_MODE_COOKIE);
    expect(modeCookie?.value).toBe(scheme);

    await page.reload({ waitUntil: "domcontentloaded" });

    // Server-rendered <html> attribute must already match the persisted
    // preference before any client JS runs — no flash on reload.
    await expect(page.locator("html")).toHaveAttribute("data-mui-color-scheme", scheme!);
  });
});

test.describe("theme toggle — mobile", () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test("toggle is reachable via the mobile hamburger drawer", async ({ page }) => {
    await page.goto("/", { waitUntil: "networkidle" });

    await page.getByRole("button", { name: "open sidebar" }).click();

    const toggle = page.getByRole("button", { name: "Toggle color mode" }).first();
    await expect(toggle).toBeVisible();
  });
});

test.describe("first-time visitor — OS preference detection", () => {
  test("renders dark when the OS prefers dark and no cookie is set", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto("/", { waitUntil: "domcontentloaded" });

    await expect(page.locator("html")).toHaveAttribute("data-mui-color-scheme", "dark");
  });

  test("renders light when the OS prefers light and no cookie is set", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await page.goto("/", { waitUntil: "domcontentloaded" });

    await expect(page.locator("html")).toHaveAttribute("data-mui-color-scheme", "light");
  });
});
