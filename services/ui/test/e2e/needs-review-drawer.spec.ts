import { test, expect, type Page } from "@playwright/test";

/**
 * Needs review drawer tests: the Sidebar "Needs review" entry opens a
 * right-side drawer that lists Skills and Agent Versions flagged for review.
 * The /api/review-queue route is mocked so the tests do not depend on a live
 * OpenDepot server or Jev verdicts.
 */

const REVIEW_QUEUE_PATTERN = "**/api/review-queue";

const QUEUE_ITEMS = [
  {
    kind: "skill",
    namespace: "platform",
    name: "terraform-skill",
    riskLevel: "Critical",
    blocked: true,
    exempted: false,
  },
  {
    kind: "agent",
    namespace: "ai",
    name: "review-agent",
    riskLevel: "High",
    blocked: false,
    exempted: true,
  },
];

async function openNeedsReview(page: Page) {
  await page.goto("/", { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Needs review" }).click();
  await expect(page.getByRole("heading", { name: "Needs review" })).toBeVisible();
}

test.describe("needs review drawer — desktop", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("lists flagged items with name, namespace, kind, risk, blocked and exempted state", async ({
    page,
  }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: QUEUE_ITEMS }) }),
    );

    await openNeedsReview(page);

    const table = page.getByRole("table", { name: "Items that need review" });
    await expect(table).toBeVisible();

    const skillRow = table.getByRole("row", { name: /terraform-skill/ });
    await expect(skillRow).toContainText("platform");
    await expect(skillRow).toContainText("Skill");
    await expect(skillRow).toContainText("Critical");
    await expect(skillRow).toContainText("Blocked");

    const agentRow = table.getByRole("row", { name: /review-agent/ });
    await expect(agentRow).toContainText("ai");
    await expect(agentRow).toContainText("Agent");
    await expect(agentRow).toContainText("High");
    await expect(agentRow).toContainText("No");
    await expect(agentRow).toContainText("Exempted");
  });

  test("item names link to the resource detail page", async ({ page }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: QUEUE_ITEMS }) }),
    );

    await openNeedsReview(page);

    const link = page.getByRole("link", { name: "terraform-skill" });
    await expect(link).toHaveAttribute("href", "/platform/skill/terraform-skill");
  });

  test("shows the empty state when nothing needs review", async ({ page }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [] }) }),
    );

    await openNeedsReview(page);

    await expect(page.getByText("Nothing needs review right now.")).toBeVisible();
  });

  test("shows the server error when the queue cannot be loaded", async ({ page }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({
        status: 502,
        contentType: "application/json",
        body: JSON.stringify({ code: "upstream_error", message: "upstream unavailable" }),
      }),
    );

    await openNeedsReview(page);

    await expect(page.getByText("upstream unavailable")).toBeVisible();
  });

  test("closes from the close button", async ({ page }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [] }) }),
    );

    await openNeedsReview(page);
    await page.getByRole("button", { name: "Close needs review" }).click();

    await expect(page.getByRole("heading", { name: "Needs review" })).toBeHidden();
  });
});

test.describe("needs review drawer — mobile", () => {
  test.use({ viewport: { width: 375, height: 800 } });

  test("opens full width and still lists flagged items", async ({ page }) => {
    await page.route(REVIEW_QUEUE_PATTERN, (route) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: QUEUE_ITEMS }) }),
    );

    await page.goto("/", { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "open sidebar" }).click();
    await page.getByRole("button", { name: "Needs review" }).click();

    await expect(page.getByRole("table", { name: "Items that need review" })).toBeVisible();
    await expect(page.getByRole("link", { name: "review-agent" })).toBeVisible();
  });
});
