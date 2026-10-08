import { expect, test } from "@playwright/test";

if (process.env.PLAYWRIGHT_AUTH_STORAGE_STATE) {
  test.use({ storageState: process.env.PLAYWRIGHT_AUTH_STORAGE_STATE });
}

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
                attributes: [
                  { name: "description", type: { kind: "string" } },
                  { name: "enabled", type: { kind: "bool" } },
                  {
                    name: "timeout",
                    type: { kind: "number" },
                    optional: true,
                    hasDefault: true,
                    default: { kind: "scalar", literal: "30" },
                  },
                ],
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
                        entries: [
                          { name: "description", value: { kind: "scalar", literal: "A service description" } },
                          { name: "enabled", value: { kind: "scalar", literal: "true" } },
                          { name: "timeout", value: { kind: "scalar", literal: "30" } },
                        ],
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
    const editor = dialog.getByTestId("variable-editor");
    const defaultLabel = editor.locator("p").filter({ hasText: /^default$/ });
    await expect(defaultLabel).toHaveCount(1);
    const defaultLabelStyle = await defaultLabel.evaluate((element) => {
      const style = getComputedStyle(element);
      return { fontFamily: style.fontFamily, fontSize: style.fontSize, color: style.color, paddingLeft: style.paddingLeft, paddingRight: style.paddingRight };
    });
    const previewWidth = (await preview.boundingBox())?.width ?? 0;
    const editorWidth = (await editor.boundingBox())?.width ?? 0;
    const expandPreview = dialog.getByRole("button", { name: "Expand HCL preview" });
    await expandPreview.click();
    await expect(dialog.getByRole("button", { name: "Restore HCL preview" })).toHaveAttribute("aria-pressed", "true");
    expect((await preview.boundingBox())?.width ?? 0).toBeGreaterThan(previewWidth);
    expect((await editor.boundingBox())?.width ?? 0).toBeLessThan(editorWidth);
    expect((await editor.boundingBox())?.width ?? 0).toBeGreaterThanOrEqual(360);
    const descriptionName = dialog.getByRole("textbox", { name: "Attribute name" }).nth(1);
    await expect(descriptionName).toHaveAttribute("title", "description");
    await expect(descriptionName).toHaveCSS("text-overflow", "ellipsis");
    await expect(defaultLabel).toHaveCSS("font-family", defaultLabelStyle.fontFamily);
    await expect(defaultLabel).toHaveCSS("font-size", defaultLabelStyle.fontSize);
    await expect(defaultLabel).toHaveCSS("color", defaultLabelStyle.color);
    await expect(defaultLabel).toHaveCSS("padding-left", defaultLabelStyle.paddingLeft);
    await expect(defaultLabel).toHaveCSS("padding-right", defaultLabelStyle.paddingRight);
    const nestedAttributeName = dialog.getByRole("textbox", { name: "Attribute name" }).last();
    const nestedAttributeType = dialog.getByRole("combobox", { name: "timeout type" });
    const nestedAttributeDefault = dialog.getByRole("textbox", { name: "timeout default value" });
    const nameBounds = await nestedAttributeName.boundingBox();
    const typeBounds = await nestedAttributeType.boundingBox();
    const defaultBounds = await nestedAttributeDefault.boundingBox();
    if (!nameBounds || !typeBounds || !defaultBounds) throw new Error("The nested attribute name, type, or default has no visible bounds.");
    expect(Math.abs((nameBounds.y + nameBounds.height / 2) - (typeBounds.y + typeBounds.height / 2))).toBeLessThan(2);
    expect(Math.abs(typeBounds.x - defaultBounds.x)).toBeLessThan(32);
    await dialog.getByRole("button", { name: "Restore HCL preview" }).click();
    await expect(dialog.getByRole("button", { name: "Expand HCL preview" })).toHaveAttribute("aria-pressed", "false");

    const nestedObject = dialog.getByRole("button", { name: "settings definition" });
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
    await dialog.getByRole("textbox", { name: "Key" }).last().fill("new-service");
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

test.describe("Assembly Line module code preview", () => {
  test("shows the configured module call at mobile and desktop widths", async ({ page }) => {
    await page.addInitScript(() => {
      if (window.sessionStorage.getItem("assembly-fixture-seeded")) return;
      window.localStorage.setItem("opendepot:assembly:v3", JSON.stringify([
        {
          id: "module-1",
          type: "module",
          position: { x: 120, y: 100 },
          data: {
            kind: "module",
            namespace: "platform",
            name: "worker",
            system: "aws",
            version: "v1.2.3",
            instanceName: "worker",
            grade: "full",
            variables: [
              { name: "region", type: "string", required: true },
              { name: "command_args", type: ["list", "string"], required: false },
              { name: "environment_variables", type: ["map", "string"], required: false },
              { name: "empty_environment_variables", type: ["map", "string"], required: false },
              {
                name: "settings",
                type: ["object", { environment: "string", labels: ["map", "string"] }],
                optionalAttributes: { labels: true },
                required: true,
              },
            ],
            outputs: [],
            requiredProviders: [],
            providerBindings: {},
            values: {
              region: { kind: "scalar", value: { mode: "literal", literal: "us-west-2" } },
              command_args: {
                kind: "list",
                items: [{ kind: "scalar", value: { mode: "literal", literal: "--debug" } }],
              },
              environment_variables: {
                kind: "map",
                entries: [{ key: "NODE_ENV", value: { kind: "scalar", value: { mode: "literal", literal: "dev" } } }],
              },
              settings: {
                kind: "object",
                entries: [
                  { name: "environment", value: { kind: "scalar", value: { mode: "literal", literal: "production" } } },
                  {
                    name: "labels",
                    value: { kind: "map", entries: [{ key: "owner", value: { kind: "scalar", value: { mode: "literal", literal: "platform" } } }] },
                  },
                ],
              },
            },
            multiplicity: { kind: "for_each", variableNodeId: null },
            loading: false,
            error: null,
          },
        },
      ]));
      window.sessionStorage.setItem("assembly-fixture-seeded", "true");
    });

    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/assembly", { waitUntil: "networkidle" });
    await page.getByTestId("rf__node-module-1").getByText("Inputs (5)").click();

    const dialog = page.getByRole("dialog");
    const environmentVariables = dialog.locator("p").filter({ hasText: /^environment_variables$/ });
    const optionalSection = dialog.getByText("Optional", { exact: true });
    const environmentVariablesBeforeOptional = await environmentVariables.evaluate(
      (element, heading) => element.compareDocumentPosition(heading as Node) & Node.DOCUMENT_POSITION_FOLLOWING,
      await optionalSection.elementHandle(),
    );
    expect(environmentVariablesBeforeOptional).toBeTruthy();
    const preview = dialog.getByTestId("module-hcl-preview");
    await expect(preview).toBeVisible();
    await expect(preview).toContainText('module "worker" {');
    await expect(preview).toContainText('region = "us-west-2"');
    await expect(preview).toContainText('labels = {');
    await expect(preview).toBeInViewport();
    const previewExpandButton = dialog.getByRole("button", { name: "Expand HCL preview" });
    const copyButton = dialog.getByRole("button", { name: "Copy to clipboard" });
    await expect(previewExpandButton).toHaveCSS("color", await copyButton.evaluate((element) => getComputedStyle(element).color));
    await expect(previewExpandButton).toHaveCSS("padding", await copyButton.evaluate((element) => getComputedStyle(element).padding));
    await expect(previewExpandButton.locator("svg")).toHaveCSS("font-size", "14px");
    const mobilePreviewHeight = (await preview.boundingBox())?.height ?? 0;
    await previewExpandButton.click();
    await expect(dialog.getByRole("button", { name: "Restore HCL preview" })).toHaveAttribute("aria-pressed", "true");
    expect((await preview.boundingBox())?.height ?? 0).toBeGreaterThan(mobilePreviewHeight);
    await dialog.getByRole("button", { name: "Restore HCL preview" }).click();

    const optionalFieldsToggle = dialog.getByRole("checkbox", { name: "Show optional fields" });
    await expect(optionalFieldsToggle).toBeChecked();
    await optionalFieldsToggle.uncheck();
    await expect(preview).not.toContainText("labels");
    await expect(optionalFieldsToggle).not.toBeChecked();

    const hclEditor = dialog.getByRole("textbox", { name: "HCL value" }).first();
    await expect(hclEditor).toBeVisible();
    const inputChip = dialog.getByRole("button", { name: "Use input" }).first();
    await expect(inputChip).toBeVisible();
    await expect(inputChip).toHaveClass(/MuiChip-colorPrimary/);
    await inputChip.click();
    const picker = page.locator(".MuiPopover-paper");
    await expect(picker.getByPlaceholder("Search module outputs")).toBeVisible();
    await expect(picker.getByRole("listbox")).toBeVisible();
    await expect(page.locator(".MuiPopover-paper")).toHaveCount(1);
    await page.getByRole("option", { name: "each.key" }).click();
    const editorBox = await hclEditor.boundingBox();
    const chipBox = await inputChip.boundingBox();
    const typeBadgeBox = await dialog.getByText("string", { exact: true }).first().boundingBox();
    if (!editorBox || !chipBox || !typeBadgeBox) throw new Error("The HCL editor, input chip, or type badge has no visible bounds.");
    expect(Math.abs((chipBox.y + chipBox.height / 2) - (typeBadgeBox.y + typeBadgeBox.height / 2))).toBeLessThan(2);
    expect(chipBox.x).toBeLessThan(typeBadgeBox.x);
    await hclEditor.fill('replace(each.key, "_", "-")');
    await expect(preview).toContainText('region = replace(each.key, "_", "-")');
    await expect(dialog.locator(".w-tc-editor .token.function")).toHaveText("replace");
    const oneLineHeight = await hclEditor.evaluate((element) => element.getBoundingClientRect().height);
    expect(oneLineHeight).toBeLessThan(40);
    await hclEditor.fill('replace(each.key, "_", "-")\nlength(var.enabled) > 0');
    const multilineHeight = await hclEditor.evaluate((element) => element.getBoundingClientRect().height);
    expect(multilineHeight).toBeGreaterThan(oneLineHeight);
    await hclEditor.fill("<<-EOT\nfirst line\nsecond line\nEOT");
    await expect(preview).toContainText("region = <<-EOT\n  first line\n  second line\n  EOT");

    await page.setViewportSize({ width: 1024, height: 900 });
    await expect(preview).toBeVisible();
    await expect(preview).toBeInViewport();
    const desktopPreviewWidth = (await preview.boundingBox())?.width ?? 0;
    await dialog.getByRole("button", { name: "Expand HCL preview" }).click();
    await expect(dialog.getByRole("button", { name: "Restore HCL preview" })).toHaveAttribute("aria-pressed", "true");
    expect((await preview.boundingBox())?.width ?? 0).toBeGreaterThan(desktopPreviewWidth);
    await expect(preview).toContainText('version = "~> 1.2.3"');
    const nodeEnvKey = dialog.getByRole("textbox", { name: "Key" }).first();
    const keyBounds = await nodeEnvKey.boundingBox();
    const keyRowBounds = await nodeEnvKey.locator("xpath=../../..").boundingBox();
    if (!keyBounds || !keyRowBounds) throw new Error("The NODE_ENV key or its input row has no visible bounds.");
    expect(keyBounds.x - keyRowBounds.x).toBeLessThan(20);
    const listIndex = dialog.getByText("[0]", { exact: true });
    const listRow = listIndex.locator("xpath=..");
    const listIndexBounds = await listIndex.boundingBox();
    const listValueBounds = await listRow.getByRole("textbox", { name: "HCL value" }).boundingBox();
    if (!listIndexBounds || !listValueBounds) throw new Error("The list index or value editor has no visible bounds.");
    expect(Math.abs((listIndexBounds.y + listIndexBounds.height / 2) - (listValueBounds.y + listValueBounds.height / 2))).toBeLessThan(2);
    const fieldLabel = dialog.getByText("environment", { exact: true }).first();
    await expect(fieldLabel).toHaveCSS("flex-basis", "120px");

    await page.waitForFunction(() => {
      const raw = window.localStorage.getItem("opendepot:assembly:v3");
      if (!raw) return false;
      const module = JSON.parse(raw).find((node: { id: string }) => node.id === "module-1");
      return module?.data?.optionalFieldVisibility?.["variable%3Asettings"] === false;
    });
    const savedVisibility = await page.evaluate(() => {
      const raw = window.localStorage.getItem("opendepot:assembly:v3");
      return raw ? JSON.parse(raw).find((node: { id: string }) => node.id === "module-1")?.data?.optionalFieldVisibility : undefined;
    });
    expect(savedVisibility).toEqual({ "variable%3Asettings": false });
    await page.reload({ waitUntil: "networkidle" });
    const reloadedVisibility = await page.evaluate(() => {
      const raw = window.localStorage.getItem("opendepot:assembly:v3");
      return raw ? JSON.parse(raw).find((node: { id: string }) => node.id === "module-1")?.data?.optionalFieldVisibility : undefined;
    });
    expect(reloadedVisibility).toEqual({ "variable%3Asettings": false });
    await page.getByTestId("rf__node-module-1").getByText("Inputs (5)").click();
    const reloadedDialog = page.getByRole("dialog");
    await expect(reloadedDialog.getByRole("checkbox", { name: "Show optional fields" })).not.toBeChecked();
    await expect(reloadedDialog.getByTestId("module-hcl-preview")).not.toContainText("labels");
    const storedInput = await page.evaluate(() => {
      const raw = window.localStorage.getItem("opendepot:assembly:v3");
      return raw ? JSON.parse(raw).find((node: { id: string }) => node.id === "module-1")?.data?.values?.settings : undefined;
    });
    expect(storedInput.entries.some((entry: { name: string }) => entry.name === "labels")).toBe(true);
  });

  test("wires a complex map input to another module output", async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("opendepot:assembly:v3", JSON.stringify([
        {
          id: "module-consumer",
          type: "module",
          position: { x: 120, y: 100 },
          data: {
            kind: "module",
            namespace: "platform",
            name: "consumer",
            system: "aws",
            version: "1.0.0",
            instanceName: "consumer",
            grade: "full",
            variables: [
              {
                name: "roles",
                type: ["map", ["object", { name: "string", description: "string" }]],
                required: true,
                description: "Execution roles to create, keyed by a stable caller-defined identifier.",
              },
              { name: "spec", type: ["object", { description: "string" }], required: true },
            ],
            outputs: [],
            requiredProviders: [],
            providerBindings: {},
            values: { roles: { kind: "map", entries: [] }, spec: { kind: "object", entries: [] } },
            multiplicity: { kind: "none" },
            loading: false,
            error: null,
          },
        },
        {
          id: "module-roles-source",
          type: "module",
          position: { x: 520, y: 100 },
          data: {
            kind: "module",
            namespace: "platform",
            name: "roles-source",
            system: "aws",
            version: "1.0.0",
            instanceName: "roles_source",
            grade: "full",
            variables: [],
            outputs: [{ name: "roles", type: ["map", ["object", { name: "string", description: "string" }]] }],
            requiredProviders: [],
            providerBindings: {},
            values: {},
            multiplicity: { kind: "none" },
            loading: false,
            error: null,
          },
        },
        {
          id: "variable-lambda-functions",
          type: "variable",
          position: { x: 880, y: 100 },
          data: {
            kind: "variable",
            name: "lambda_functions",
            type: {
              kind: "map",
              element: {
                kind: "object",
                attributes: [{ name: "spec", type: { kind: "object", attributes: [{ name: "description", type: { kind: "string" } }] } }],
              },
            },
            description: "",
            validations: [],
            hasDefault: false,
            default: { kind: "map", entries: [] },
          },
        },
      ]));
    });

    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/assembly", { waitUntil: "networkidle" });
    await page.getByTestId("rf__node-module-consumer").getByText("Inputs (2)").dispatchEvent("click");

    const dialog = page.getByRole("dialog");
    const typeBadge = dialog.getByText("map(object(2))", { exact: true });
    const useInputButton = dialog.getByRole("button", { name: "Use input" }).first();
    await expect(dialog.getByText("Map entries", { exact: true })).toBeVisible();
    const typeBadgeBounds = await typeBadge.boundingBox();
    const useInputBounds = await useInputButton.boundingBox();
    if (!typeBadgeBounds || !useInputBounds) throw new Error("The input type badge or Use input control has no visible bounds.");
    expect(Math.abs((typeBadgeBounds.y + typeBadgeBounds.height / 2) - (useInputBounds.y + useInputBounds.height / 2))).toBeLessThan(2);
    expect(useInputBounds.x).toBeLessThan(typeBadgeBounds.x);
    await dialog.getByRole("button", { name: "Add entry" }).click();
    const entryActions = dialog.getByTestId("module-input-row-actions").last();
    const entryInput = entryActions.getByRole("button", { name: "Use input" });
    const removeEntry = entryActions.getByRole("button", { name: "Remove entry" });
    const entryInputBounds = await entryInput.boundingBox();
    const removeEntryBounds = await removeEntry.boundingBox();
    if (!entryInputBounds || !removeEntryBounds) throw new Error("The map entry input or remove control has no visible bounds.");
    expect(Math.abs((entryInputBounds.y + entryInputBounds.height / 2) - (removeEntryBounds.y + removeEntryBounds.height / 2))).toBeLessThan(2);
    expect(entryInputBounds.x).toBeLessThan(removeEntryBounds.x);
    await useInputButton.click();
    await page.getByPlaceholder("Search module outputs").fill("module.roles_source.roles");
    await page.getByRole("option", { name: "module.roles_source.roles" }).click();
    await expect(dialog.getByTestId("module-hcl-preview")).toContainText("roles = module.roles_source.roles");
    const selectedReference = dialog.getByRole("button", { name: "Module output module.roles_source.roles" });
    await expect(selectedReference).toBeVisible();
    const selectedBounds = await selectedReference.boundingBox();
    const selectorLabel = dialog.getByText("roles is a map —", { exact: true });
    const selectorLabelBounds = await selectorLabel.boundingBox();
    const rolesNameBounds = await dialog.getByText("roles", { exact: true }).first().boundingBox();
    if (!selectedBounds || !selectorLabelBounds || !rolesNameBounds) throw new Error("The selected reference, collection selector, or input name has no visible bounds.");
    await expect(dialog.getByText("roles is a map —", { exact: true })).toHaveCount(1);
    expect(Math.abs((selectedBounds.y + selectedBounds.height / 2) - (selectorLabelBounds.y + selectorLabelBounds.height / 2))).toBeLessThan(2);
    expect(selectedBounds.x).toBeLessThan(selectorLabelBounds.x);
    await expect(selectorLabel).toBeVisible();
    const rolesDescriptionBounds = await dialog.getByText("Execution roles to create, keyed by a stable caller-defined identifier.", { exact: true }).boundingBox();
    if (!rolesDescriptionBounds) throw new Error("The input description has no visible bounds.");
    expect(rolesDescriptionBounds.y).toBeGreaterThan(rolesNameBounds.y);
    expect(rolesDescriptionBounds.y + rolesDescriptionBounds.height).toBeLessThan(selectorLabelBounds.y);

    await dialog.getByRole("button", { name: "One instance" }).first().click();
    const keyExpression = dialog.getByPlaceholder("key expression");
    const hclValue = dialog.getByRole("textbox", { name: "HCL value" }).first();
    const keySurface = keyExpression.locator("xpath=..");
    const hclSurface = dialog.locator(".w-tc-editor").first().locator("xpath=..");
    await expect(keyExpression).toHaveCSS("font-family", await hclValue.evaluate((element) => getComputedStyle(element).fontFamily));
    await expect(keyExpression).toHaveCSS("font-size", await hclValue.evaluate((element) => getComputedStyle(element).fontSize));
    await expect(keySurface).toHaveCSS("background-color", await hclSurface.evaluate((element) => getComputedStyle(element).backgroundColor));
    await expect(keySurface).toHaveCSS("border-radius", await hclSurface.evaluate((element) => getComputedStyle(element).borderRadius));

    await dialog.getByRole("button", { name: "Use input" }).first().click();
    await page.getByPlaceholder("Search module outputs").fill("var.lambda_functions");
    await page.getByRole("option", { name: "var.lambda_functions" }).click();
    await dialog.getByRole("button", { name: "One instance" }).last().click();
    const keyExpressionForDescendant = dialog.getByPlaceholder("key expression").last();
    await keyExpressionForDescendant.fill("each.key");
    const descendantSelector = dialog.getByRole("combobox", { name: "Select descendant" }).last();
    const wholeValueChip = descendantSelector.locator(".MuiChip-root");
    await descendantSelector.click();
    const themedMenu = page.locator(".MuiMenu-paper").last();
    const menuSurface = await themedMenu.evaluate((element) => {
      const style = getComputedStyle(element);
      return { backgroundColor: style.backgroundColor, borderRadius: style.borderRadius, boxShadow: style.boxShadow };
    });
    const wholeValueSelectionColor = await page.getByRole("option", { name: "Whole value" }).evaluate((element) => getComputedStyle(element).backgroundColor);
    await page.keyboard.press("Escape");
    await expect(wholeValueChip).toHaveCSS("outline-style", "none");

    await dialog.getByRole("button", { name: "Module output var.lambda_functions" }).click();
    const searchPicker = page.locator(".MuiPopover-paper").last();
    const searchInput = searchPicker.getByPlaceholder("Search module outputs");
    await expect(searchInput).toBeVisible();
    await expect(searchInput).toHaveValue("");
    const searchSurface = await searchPicker.evaluate((element) => {
      const style = getComputedStyle(element);
      return { backgroundColor: style.backgroundColor, borderRadius: style.borderRadius, boxShadow: style.boxShadow };
    });
    expect(searchSurface).toEqual(menuSurface);
    const selectedInputOption = searchPicker.getByRole("option", { name: "var.lambda_functions" });
    await expect(selectedInputOption).toHaveCSS("background-color", wholeValueSelectionColor);
    await page.locator(".MuiBackdrop-root").last().click({ position: { x: 2, y: 2 }, force: true });
    await expect(searchPicker).toBeHidden();

    await keyExpressionForDescendant.click();
    await keyExpressionForDescendant.press("Tab");
    await expect(descendantSelector).toBeFocused();
    await expect(wholeValueChip).toHaveCSS("outline-style", "solid");
    await descendantSelector.press("Enter");
    await page.getByRole("option", { name: "spec", exact: true }).click();
    await expect(dialog.getByTestId("module-hcl-preview")).toContainText("spec = var.lambda_functions[each.key].spec");
    await expect(page.getByText("spec [each.key]", { exact: true })).toBeVisible();
  });

  test("keeps inputs editable and clears export diagnostics after correcting HCL", async ({ page }) => {
    const diagnostic = "invalid HCL expression: test diagnostic";
    await page.route("**/api/assembly/export", (route) => route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        error: "invalid_assembly",
        message: "Assembly validation failed",
        diagnostics: [{ code: "invalid_hcl_expression", message: diagnostic, path: "modules[0].values.region", nodeId: "module-invalid" }],
      }),
    }));
    await page.addInitScript(() => {
      window.localStorage.setItem("opendepot:assembly:v3", JSON.stringify([
        {
          id: "module-invalid",
          type: "module",
          position: { x: 120, y: 100 },
          data: {
            kind: "module",
            namespace: "platform",
            name: "worker",
            system: "aws",
            version: "1.0.0",
            instanceName: "worker",
            grade: "full",
            variables: [{ name: "region", type: "string", required: true }],
            outputs: [],
            requiredProviders: [],
            providerBindings: {},
            values: { region: { kind: "scalar", value: { mode: "literal", literal: "" } } },
            multiplicity: { kind: "none" },
            loading: false,
            error: null,
          },
        },
      ]));
    });

    await page.goto("/assembly", { waitUntil: "networkidle" });
    await page.getByRole("button", { name: "Export" }).click();
    const exportDialog = page.getByRole("dialog");
    await expect(exportDialog).toContainText("Assembly validation failed");
    await exportDialog.getByRole("button", { name: "Close" }).click();

    const node = page.getByTestId("rf__node-module-invalid");
    await expect(node).toContainText(diagnostic);
    await expect(node).toContainText(`modules[0].values.region: ${diagnostic}`);
    const inputsButton = node.getByText("Inputs (1)");
    await expect(inputsButton).toBeVisible();
    await inputsButton.click();
    const inputsDialog = page.getByRole("dialog");
    await inputsDialog.getByRole("textbox", { name: "HCL value" }).fill('replace(var.region, "_", "-")');
    await expect(node).not.toContainText(diagnostic);
    await expect(inputsDialog).toBeVisible();
  });
});

test.describe("Assembly Line provider code preview", () => {
  test("shows and updates provider HCL at mobile and desktop widths", async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("opendepot:assembly:v3", JSON.stringify([
        {
          id: "provider-1",
          type: "provider",
          position: { x: 120, y: 100 },
          data: {
            kind: "provider",
            namespace: "hashicorp",
            name: "aws",
            version: "5.0.0",
            providerNamespace: "hashicorp",
            providerName: "aws",
            localName: "aws",
            alias: "west",
            schema: {
              attributes: { region: { type: "string", optional: true } },
              blocks: {
                assume_role: {
                  nesting: "list",
                  block: { attributes: { role_arn: { type: "string", optional: true } } },
                },
              },
            },
            configuration: {
              arguments: { region: { mode: "literal", literal: "us-west-2" } },
              blocks: {
                assume_role: [{
                  arguments: { role_arn: { mode: "literal", literal: "arn:aws:iam::123:role/deploy" } },
                  blocks: {},
                }],
              },
            },
            loading: false,
            error: null,
          },
        },
      ]));
    });

    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto("/assembly", { waitUntil: "networkidle" });
    await page.getByTestId("rf__node-provider-1").getByText("Configuration").click();

    const dialog = page.getByRole("dialog");
    const preview = dialog.getByTestId("provider-hcl-preview");
    await expect(preview).toBeVisible();
    await expect(preview).toContainText('provider "aws" {');
    await expect(preview).toContainText('alias = "west"');
    await expect(preview).toContainText('region = "us-west-2"');
    await expect(preview).toContainText('assume_role {');
    await expect(preview).toContainText('role_arn = "arn:aws:iam::123:role/deploy"');
    await expect(preview).toBeInViewport();

    await dialog.getByRole("textbox", { name: "HCL value" }).last().fill("us-east-1");
    await expect(preview).toContainText('region = "us-east-1"');
    const mobilePreviewHeight = (await preview.boundingBox())?.height ?? 0;
    await dialog.getByRole("button", { name: "Expand HCL preview" }).click();
    await expect(dialog.getByRole("button", { name: "Restore HCL preview" })).toHaveAttribute("aria-pressed", "true");
    expect((await preview.boundingBox())?.height ?? 0).toBeGreaterThan(mobilePreviewHeight);

    await page.setViewportSize({ width: 1024, height: 900 });
    await expect(preview).toBeInViewport();
    const mobileExpandedWidth = (await preview.boundingBox())?.width ?? 0;
    await dialog.getByRole("button", { name: "Restore HCL preview" }).click();
    await expect(dialog.getByRole("button", { name: "Expand HCL preview" })).toHaveAttribute("aria-pressed", "false");
    expect((await preview.boundingBox())?.width ?? 0).toBeLessThan(mobileExpandedWidth);
  });
});
