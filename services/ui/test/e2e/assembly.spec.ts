import { expect, test } from "@playwright/test";

const STORAGE_KEY = "opendepot:assembly:v1";

const nestedVariable = {
  id: "variable-1",
  type: "variable",
  position: { x: 120, y: 100 },
  data: {
    kind: "variable",
    name: "services",
    type: {
      kind: "map",
      element: {
        kind: "object",
        attributes: [
          {
            name: "settings",
            type: {
              kind: "map",
              element: {
                kind: "object",
                attributes: [{ name: "enabled", type: { kind: "bool" } }],
              },
            },
          },
        ],
      },
    },
    hasDefault: true,
    default: {
      kind: "map",
      entries: [
        {
          key: "existing",
          value: {
            kind: "object",
            entries: [
              {
                name: "settings",
                value: {
                  kind: "map",
                  entries: [
                    {
                      key: "primary",
                      value: {
                        kind: "object",
                        entries: [{ name: "enabled", value: { kind: "scalar", literal: "true" } }],
                      },
                    },
                  ],
                },
              },
            ],
          },
        },
      ],
    },
  },
};

test.describe("Assembly Line recursive variable defaults", () => {
  test.use({ viewport: { width: 1440, height: 1000 } });

  test("renders nested HCL and expands only the newest map entry", async ({ page }) => {
    await page.addInitScript(
      ({ key, nodes }) => window.localStorage.setItem(key, JSON.stringify(nodes)),
      { key: STORAGE_KEY, nodes: [nestedVariable] },
    );

    await page.goto("/assembly", { waitUntil: "networkidle" });
    await page
      .getByTestId("rf__node-variable-1")
      .locator('[role="button"]')
      .filter({ hasText: /^Type: map\(object/ })
      .click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toContainText("settings = map(object({");
    await expect(dialog).toContainText('"primary" = {');
    const preview = dialog.getByTestId("hcl-preview");
    await expect(preview).toBeVisible();
    await expect(preview).toBeInViewport();

    const nestedObject = dialog.getByRole("button", { name: "Object definition (1 attribute)" }).last();
    await nestedObject.click();
    await expect(nestedObject).toHaveAttribute("aria-expanded", "false");
    await expect(preview).toBeInViewport();

    const existing = dialog.getByRole("button", { name: "existing" });
    await existing.click();
    await expect(existing).toHaveAttribute("aria-expanded", "true");

    await dialog.getByRole("button", { name: "Add entry" }).last().click();
    await expect(existing).toHaveAttribute("aria-expanded", "false");
    await expect(preview).toBeInViewport();

    const newEntry = dialog.getByRole("button", { name: "New entry" });
    await expect(newEntry).toHaveAttribute("aria-expanded", "true");
    const newAccordion = newEntry.locator("xpath=ancestor::*[contains(@class, 'MuiAccordion-root')]");
    await newAccordion.getByLabel("Key").fill("new-service");
    await expect(dialog.getByRole("button", { name: "new-service" })).toHaveAttribute("aria-expanded", "true");
    await expect(dialog).toContainText('"new-service" = {}');
  });
});

test.describe("Assembly Line mobile menu", () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test("hides the resource menu until opened", async ({ page }) => {
    await page.goto("/assembly", { waitUntil: "networkidle" });

    const showMenu = page.getByRole("button", { name: "Show Assembly Line menu" });
    const search = page.getByRole("textbox", { name: "Search resources…" });
    await expect(showMenu).toBeVisible();
    await expect(search).toBeHidden();

    await showMenu.click();
    await expect(search).toBeVisible();
    await page.locator(".react-flow__pane").click({ position: { x: 300, y: 300 } });
    await expect(search).toBeHidden();
  });

  test("closes the resource menu when a node is clicked", async ({ page }) => {
    await page.goto("/assembly", { waitUntil: "networkidle" });

    const showMenu = page.getByRole("button", { name: "Show Assembly Line menu" });
    const search = page.getByRole("textbox", { name: "Search resources…" });
    await showMenu.click();
    await expect(search).toBeVisible();

    await page.getByRole("button", { name: "Add" }).click();
    await page.getByTestId(/rf__node-variable-/).first().dispatchEvent("click");
    await expect(search).toBeHidden();
  });

  test("hides the node map until explicitly enabled", async ({ page }) => {
    await page.goto("/assembly", { waitUntil: "networkidle" });

    const showMap = page.getByRole("button", { name: "Show node map" });
    const hideMap = page.getByRole("button", { name: "Hide node map" });
    await expect(showMap).toBeVisible();
    await expect(hideMap).toBeHidden();
    await expect(page.getByRole("img", { name: "React Flow mini map" })).toBeHidden();

    await showMap.click();
    await expect(hideMap).toBeVisible();
    await expect(page.getByRole("img", { name: "React Flow mini map" })).toBeVisible();
  });
});
