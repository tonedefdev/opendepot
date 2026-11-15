import { test, expect } from "@playwright/test";

const osGroup = (page: import("@playwright/test").Page) =>
  page.getByRole("group", { name: "Operating system" });

test.describe("CLI page", () => {
  test("renders the OpenDepot CLI heading and an OS selector", async ({ page }) => {
    await page.goto("/cli");

    await expect(page.getByRole("heading", { name: "OpenDepot CLI", level: 6 })).toBeVisible();
    await expect(osGroup(page).getByRole("button", { name: "macOS" })).toBeVisible();
    await expect(osGroup(page).getByRole("button", { name: "Linux" })).toBeVisible();
    await expect(osGroup(page).getByRole("button", { name: "Windows" })).toBeVisible();
  });

  test("switches the install script to the selected OS", async ({ page }) => {
    await page.goto("/cli");

    // The verify command renders above the install script and also mentions opendepot, so the script is the last code block.
    const script = page.locator("pre").last();

    // Retry the click until React has hydrated and the selection takes effect.
    await expect(async () => {
      await osGroup(page).getByRole("button", { name: "Windows" }).click();
      await expect(script).toContainText("Get-FileHash -Algorithm SHA256", { timeout: 1000 });
    }).toPass();
    await expect(script).toContainText("LOCALAPPDATA");
    await expect(page.getByText("Run in PowerShell")).toBeVisible();

    await osGroup(page).getByRole("button", { name: "Linux" }).click();
    await expect(script).toContainText('sha256sum "$TMP/$BIN"');
    await expect(script).not.toContainText("Get-FileHash");
    await expect(page.getByText("Run in Terminal")).toBeVisible();

    await osGroup(page).getByRole("button", { name: "macOS" }).click();
    await expect(script).toContainText("sudo install -m 0755");
    await expect(script).toContainText("/api/cli/darwin/$ARCH");
  });

  test("verify card uses a verification icon instead of the download icon", async ({ page }) => {
    await page.goto("/cli");

    const verifyCard = page
      .locator(".MuiCard-root")
      .filter({ has: page.getByRole("heading", { name: "Verify", exact: true }) });

    await expect(verifyCard.getByTestId("verify-icon")).toHaveCount(1);
    await expect(verifyCard.locator('[data-testid="DownloadOutlinedIcon"]')).toHaveCount(0);
  });
});

test.describe("CLI page on mobile", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("shows the shared mobile header bar with the sidebar toggle", async ({ page }) => {
    await page.goto("/cli");

    const barTitle = page.getByRole("heading", { name: "OpenDepot CLI", level: 6 });
    const toggle = page.getByRole("button", { name: "open sidebar" });

    await expect(barTitle).toBeVisible();
    await expect(toggle).toBeVisible();

    const titleBox = await barTitle.boundingBox();
    const toggleBox = await toggle.boundingBox();
    const titleCenterY = (titleBox?.y ?? 0) + (titleBox?.height ?? 0) / 2;
    const toggleCenterY = (toggleBox?.y ?? 0) + (toggleBox?.height ?? 0) / 2;
    expect(Math.abs(titleCenterY - toggleCenterY)).toBeLessThan(8);
  });
});
