// HUB-033 browser evidence: the candidate editor submits new candidates
// with their expected base and announces base conflicts, and the compare
// flow renders two immutable versions side by side on desktop and stacked
// at 390px. Fixtures are the exact documents tools/uxqual/cmd/genfixtures
// writes to tools/uxqual/testdata/rendered/docs-{editor,compare}.html.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const editorUrl = pathToFileURL(path.join(renderedDir, "docs-editor.html")).href;
const compareUrl = pathToFileURL(path.join(renderedDir, "docs-compare.html")).href;

test.describe("docs candidate editor", () => {
  test("submits candidates with expected base and announces conflict", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(editorUrl);
    await expect(page.locator("form")).toHaveAttribute(
      "action",
      "/docs/doc-1/candidates",
    );
    await expect(page.locator('input[name="base_version"]')).toHaveValue("docv-1");
    await expect(page.locator('input[name="base_hash"]')).toHaveValue("9e1e");
    const alert = page.getByRole("alert");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("docv-2");
    // Keyboard order: body first, then submit.
    await page.keyboard.press("Tab");
    await expect(page.locator("#markdown")).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(page.locator('button[type="submit"]')).toBeFocused();
    expect(errors).toEqual([]);
  });
});

test.describe("docs version compare", () => {
  test("side by side on desktop, stacked at 390px", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(compareUrl);
    await expect(page.locator("form")).toHaveCount(0);
    const panes = page.locator(".doc-compare > section");
    await expect(panes).toHaveCount(2);
    const desktop = [
      await panes.nth(0).boundingBox(),
      await panes.nth(1).boundingBox(),
    ];
    expect(Math.abs(desktop[0].y - desktop[1].y)).toBeLessThan(2);
    expect(desktop[1].x).toBeGreaterThan(desktop[0].x);
    await page.setViewportSize({ width: 390, height: 844 });
    const narrow = [await panes.nth(0).boundingBox(), await panes.nth(1).boundingBox()];
    expect(narrow[1].y).toBeGreaterThanOrEqual(narrow[0].y + narrow[0].height - 1);
    expect(errors).toEqual([]);
  });
});
