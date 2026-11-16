import { test, expect } from "@playwright/test";
import { LAYOUT_UI_URL } from "./support/layout-stack";

/**
 * Registry layout tests: at 1200px and wider the Risk column must hold five
 * severity chips with four-digit counts on one row, every chip fully inside its
 * cell, with no horizontal overflow. Source stays visible at lg+ when a row has a
 * sourceRef. Runs against the stub-backed UI server from playwright.config.ts.
 */

test.use({ baseURL: LAYOUT_UI_URL });

const WIDTHS = [1200, 1440, 1628, 1920];
const RISK_CELL_MIN_WIDTH = 312;
const SEVERITY_CHIP_PATTERN = /^[CHMLUE] 9999$/;

for (const width of WIDTHS) {
  test(`Risk column fits five 4-digit chips inside its cell at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 });
    await page.goto("/", { waitUntil: "domcontentloaded" });

    const table = page.getByRole("table", { name: "Registry resources" });
    await expect(table).toBeVisible();

    const chips = table.locator(".MuiChip-root");
    await expect(chips).toHaveCount(11);
    for (let i = 0; i < 11; i++) {
      await expect(chips.nth(i)).toHaveText(SEVERITY_CHIP_PATTERN);
    }

    const layout = await table.evaluate((el) => {
      const container = el.parentElement;
      if (!container) throw new Error("registry table has no container");
      const headers = Array.from(el.querySelectorAll("thead th"));
      const riskIndex = headers.findIndex((th) => th.textContent?.trim() === "Risk");
      if (riskIndex < 0) throw new Error("registry table has no Risk column");
      const riskCells = Array.from(el.querySelectorAll(`tbody tr td:nth-child(${riskIndex + 1})`));
      const riskRects = riskCells.map((cell) => {
        const box = cell.getBoundingClientRect();
        const chipRects = Array.from(cell.querySelectorAll(".MuiChip-root")).map((chip) => {
          const chipBox = chip.getBoundingClientRect();
          return { left: chipBox.left, right: chipBox.right, top: chipBox.top };
        });
        return { left: box.left, right: box.right, width: box.width, chips: chipRects };
      });
      const sourceHeader = headers.find((th) => th.textContent?.trim() === "Source");
      const sourceVisible = !!sourceHeader && getComputedStyle(sourceHeader).display !== "none";
      return {
        scrollWidth: container.scrollWidth,
        clientWidth: container.clientWidth,
        riskRects,
        sourceVisible,
      };
    });

    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth);
    expect(layout.sourceVisible).toBe(true);

    for (const risk of layout.riskRects) {
      expect(risk.width).toBeGreaterThanOrEqual(RISK_CELL_MIN_WIDTH);
      const clipped = risk.chips.filter((chip) => chip.left < risk.left || chip.right > risk.right);
      expect(clipped).toEqual([]);
      const tops = new Set(risk.chips.map((chip) => Math.round(chip.top)));
      expect(tops.size).toBe(1);
    }

    const providerRisk = layout.riskRects[layout.riskRects.length - 1];
    expect(providerRisk.chips).toHaveLength(5);
    const providerSpan = providerRisk.chips[4].right - providerRisk.chips[0].left;
    expect(providerSpan).toBeLessThanOrEqual(providerRisk.width);
  });
}

test("Name column is capped so Risk gets leftover width at 1920px", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 800 });
  await page.goto("/", { waitUntil: "domcontentloaded" });

  const table = page.getByRole("table", { name: "Registry resources" });
  await expect(table).toBeVisible();

  const widths = await table.evaluate((el) =>
    Array.from(el.querySelectorAll("thead th")).map((th) => th.getBoundingClientRect().width),
  );
  expect(widths[1]).toBeLessThanOrEqual(365);
  expect(widths[5]).toBeGreaterThanOrEqual(RISK_CELL_MIN_WIDTH);
});

test("Source is visible at 1440px when a row has a sourceRef", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 800 });
  await page.goto("/", { waitUntil: "domcontentloaded" });

  const table = page.getByRole("table", { name: "Registry resources" });
  await expect(table).toBeVisible();
  await expect(table.getByRole("columnheader", { name: /Source/ })).toBeVisible();
});
