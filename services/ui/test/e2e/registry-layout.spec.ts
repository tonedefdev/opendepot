import { test, expect } from "@playwright/test";
import { LAYOUT_UI_URL } from "./support/layout-stack";

/**
 * Registry layout tests: Kind, Version, Source and Status keep their fixed widths, Name is capped at 320px,
 * and Risk keeps a 472px floor and takes the leftover width. Every severity chip stays fully inside its cell.
 * Source is shown at lg+ (1200px and up) when a row has a sourceRef. Runs against the stub-backed UI server from playwright.config.ts.
 */

test.use({ baseURL: LAYOUT_UI_URL });

const WIDTHS = [1200, 1440, 1628, 1920];
const FIXED_WIDTHS: Record<string, number> = { Kind: 132, Version: 104, Source: 128, Status: 156 };
const NAME_MAX_WIDTH = 320;
const RISK_MIN_WIDTH = 472;
const COLUMN_TOLERANCE = 1;
const SEVERITY_CHIP_PATTERN = /^[CHMLUE] 9999$/;

for (const width of WIDTHS) {
  test(`Registry columns hold their widths and Risk fits its chips at ${width}px`, async ({ page }, testInfo) => {
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
      const columns = headers.map((th) => ({
        label: th.textContent?.trim() ?? "",
        width: th.getBoundingClientRect().width,
        display: getComputedStyle(th).display,
      }));
      const riskIndex = headers.findIndex((th) => th.textContent?.trim() === "Risk");
      if (riskIndex < 0) throw new Error("registry table has no Risk column");
      const riskCells = Array.from(el.querySelectorAll(`tbody tr td:nth-child(${riskIndex + 1})`));
      const riskRects = riskCells.map((cell) => {
        const box = cell.getBoundingClientRect();
        // The Risk cell's only child is the overflow-hidden flex box that holds the meter and chips.
        const inner = (cell.firstElementChild ?? cell).getBoundingClientRect();
        const chipRects = Array.from(cell.querySelectorAll(".MuiChip-root")).map((chip) => {
          const chipBox = chip.getBoundingClientRect();
          const label = chip.querySelector(".MuiChip-label");
          const labelClipped = !!label && label.scrollWidth > label.clientWidth;
          return { left: chipBox.left, right: chipBox.right, top: chipBox.top, labelClipped };
        });
        return { left: box.left, right: box.right, width: box.width, inner: { left: inner.left, right: inner.right }, chips: chipRects };
      });
      return {
        scrollWidth: container.scrollWidth,
        clientWidth: container.clientWidth,
        columns,
        riskRects,
      };
    });

    // Recorded, not asserted: at 1200px the table's minimum width can exceed the container, so the registry scrolls.
    if (width === 1200) {
      testInfo.annotations.push({
        type: "measurement",
        description: `scrollWidth=${layout.scrollWidth} clientWidth=${layout.clientWidth} overflow=${layout.scrollWidth - layout.clientWidth}px`,
      });
    } else {
      expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth);
    }

    const column = (label: string) => {
      const match = layout.columns.find((c) => c.label === label);
      if (!match) throw new Error(`registry table has no ${label} column`);
      return match;
    };

    for (const [label, expected] of Object.entries(FIXED_WIDTHS)) {
      const measured = column(label);
      if (label === "Source") {
        expect(measured.display).not.toBe("none");
      }
      expect(Math.abs(measured.width - expected)).toBeLessThanOrEqual(COLUMN_TOLERANCE);
    }

    expect(column("Name").width).toBeLessThanOrEqual(NAME_MAX_WIDTH);

    for (const risk of layout.riskRects) {
      expect(risk.width).toBeGreaterThanOrEqual(RISK_MIN_WIDTH);
      const clipped = risk.chips.filter(
        (chip) => chip.left < risk.inner.left || chip.right > risk.inner.right || chip.labelClipped,
      );
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
