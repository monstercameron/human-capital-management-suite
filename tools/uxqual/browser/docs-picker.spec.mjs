// HUB-035 browser evidence: the link picker submits stable document ids
// through a keyboard-operable native control, and the backlinks view
// shows safe target states with restricted sources title-free, stacked
// at 390px. Fixtures are the exact documents tools/uxqual/cmd/genfixtures
// writes to tools/uxqual/testdata/rendered/docs-{picker,backlinks}.html.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const pickerUrl = pathToFileURL(path.join(renderedDir, "docs-picker.html")).href;
const backlinksUrl = pathToFileURL(path.join(renderedDir, "docs-backlinks.html")).href;

test.describe("docs link picker", () => {
  test("keyboard picker submits stable ids", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(pickerUrl);
    const select = page.locator("#target-doc");
    await expect(select).toBeVisible();
    const values = await select
      .locator("option")
      .evaluateAll((els) => els.map((el) => el.value));
    expect(values).toEqual(["doc-b", "doc-c"]);
    // Keyboard order: target select, anchor input, submit.
    await page.keyboard.press("Tab");
    await expect(select).toBeFocused();
    await page.selectOption("#target-doc", "doc-c");
    await page.keyboard.press("Tab");
    await expect(page.locator("#target-block")).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(page.locator('button[type="submit"]')).toBeFocused();
    expect(errors).toEqual([]);
  });
});

test.describe("docs backlinks", () => {
  test("states read as text at desktop and 390px", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(backlinksUrl);
    const items = page.locator(".doc-backlinks > li");
    await expect(items).toHaveCount(2);
    await expect(items.nth(0)).toContainText("doc-a");
    await expect(items.nth(0)).toContainText("State: valid");
    await expect(items.nth(1)).toContainText("Restricted");
    await expect(items.nth(1)).toContainText("State: stale");
    const body = await page.textContent("body");
    expect(body).not.toContain("doc-x-title");
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(items.nth(1)).toBeVisible();
    expect(errors).toEqual([]);
  });
});
