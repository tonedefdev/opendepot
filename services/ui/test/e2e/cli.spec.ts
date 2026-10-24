import { test, expect } from "@playwright/test";

const osGroup = (page: import("@playwright/test").Page) =>
  page.getByRole("group", { name: "Operating system" });

test.describe("CLI page", () => {
  test("renders without a header bar and offers an OS selector", async ({ page }) => {
    await page.goto("/cli");

    await expect(page.getByRole("heading", { name: "OpenDepot CLI", level: 1 })).toBeVisible();
    await expect(page.getByText("Download the OpenDepot command-line tool.")).toHaveCount(0);
    await expect(osGroup(page).getByRole("button", { name: "macOS" })).toBeVisible();
    await expect(osGroup(page).getByRole("button", { name: "Linux" })).toBeVisible();
    await expect(osGroup(page).getByRole("button", { name: "Windows" })).toBeVisible();
  });

  test("switches the install script to the selected OS", async ({ page }) => {
    await page.goto("/cli");

    const script = page.locator("pre").filter({ hasText: "opendepot" });

    await osGroup(page).getByRole("button", { name: "Windows" }).click();
    await expect(script).toContainText("Get-FileHash -Algorithm SHA256");
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

    await expect(verifyCard.locator('[data-testid="VerifiedUserOutlinedIcon"]')).toHaveCount(1);
    await expect(verifyCard.locator('[data-testid="DownloadOutlinedIcon"]')).toHaveCount(0);
  });
});

test.describe("CLI page on mobile", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("keeps the sidebar toggle reachable without a header bar", async ({ page }) => {
    await page.goto("/cli");

    await expect(page.getByRole("heading", { name: "OpenDepot CLI", level: 1 })).toBeVisible();
    await expect(page.getByRole("button", { name: "open sidebar" })).toBeVisible();
  });
});
