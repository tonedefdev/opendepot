import { test, expect } from "@playwright/test";
import { LAYOUT_UI_URL } from "./support/layout-stack";

/**
 * Registry layout tests: the worst-case Risk row (Critical meter plus six
 * four-digit chips) must fit inside the table at desktop widths without
 * horizontal overflow. Runs against the stub-backed UI server from playwright.config.ts.
 */

test.use({ baseURL: LAYOUT_UI_URL });

for (const width of [1200, 1280, 1440, 1920]) {
  test(`worst-case Risk row fits without table overflow at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.goto("/", { waitUntil: "domcontentloaded" });

    const table = page.getByRole("table", { name: "Registry resources" });
    await expect(table).toBeVisible();

    const chips = table.locator(".MuiChip-root");
    await expect(chips).toHaveCount(6);
    await expect(chips.nth(0)).toHaveText("C 8888");
    await expect(chips.nth(5)).toHaveText("E 8888");

    const layout = await table.evaluate((el) => {
      const container = el.parentElement;
      if (!container) throw new Error("registry table has no container");
      const box = container.getBoundingClientRect();
      const chipBoxes = Array.from(el.querySelectorAll(".MuiChip-root")).map((chip) => {
        const rect = chip.getBoundingClientRect();
        return { label: chip.textContent ?? "", left: rect.left, right: rect.right };
      });
      return {
        scrollWidth: container.scrollWidth,
        clientWidth: container.clientWidth,
        left: box.left,
        right: box.right,
        chipBoxes,
      };
    });

    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth);

    const clipped = layout.chipBoxes.filter((chip) => chip.left < layout.left || chip.right > layout.right);
    expect(clipped).toEqual([]);
  });
}
