// HUB-033 browser evidence: the candidate editor (internal/humanwork/productui
// docs_editor.go) mounts against the document's current version as its
// expected base, the conflict status line (docs_editor.go's shared
// docsEditorStatusNode) is a properly announced alert, and the compare view
// (docs_compare.go) renders two immutable versions side by side on desktop
// and stacked at 390px. Fixtures are the exact served productui output
// tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/docs-{editor,editor-conflict,compare}.html
// (productui.DocsEditorFixture, DocsEditorConflictFixture, DocsCompareFixture) --
// not the tools/uxqual/render/docs fixture renderer, which the G-L
// reachability audit found sits outside the shipped binary.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const editorUrl = pathToFileURL(path.join(renderedDir, "docs-editor.html")).href;
const editorConflictUrl = pathToFileURL(
  path.join(renderedDir, "docs-editor-conflict.html"),
).href;
const compareUrl = pathToFileURL(path.join(renderedDir, "docs-compare.html")).href;

test.describe("docs candidate editor", () => {
  test("mounts against the document's current version as its expected base", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(editorUrl);
    const editor = page.locator("#docs-editor");
    await expect(editor).toHaveAttribute("data-document-id", "doc-1");
    await expect(editor).toHaveAttribute("data-base-version-id", "docv-1");
    await expect(page.locator("#docs-editor-title")).toHaveAttribute("required", "");
    await expect(page.getByRole("toolbar")).toHaveAttribute("aria-label", /.+/);
    // Keyboard order: the title field is the editor's first stop.
    await page.keyboard.press("Tab");
    await expect(page.locator("#docs-editor-title")).toBeFocused();
    const save = page.locator("#docs-editor").getByRole("button", {
      name: "Save new version",
    });
    await expect(save).toBeVisible();
    await expect(save).toHaveAttribute("aria-keyshortcuts", /Control\+S/);
    expect(errors).toEqual([]);
  });

  test("announces a base-version conflict as an alert", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(editorConflictUrl);
    const alert = page.getByRole("alert");
    await expect(alert).toBeVisible();
    await expect(alert).toHaveAttribute("aria-live", "polite");
    await expect(alert).toContainText("changed while you were editing");
    expect(errors).toEqual([]);
  });
});

test.describe("docs version compare", () => {
  test("side by side on desktop, stacked at 390px", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(compareUrl);
    // The compare view is read-only: no form, textarea or toolbar command
    // anywhere in it, so a comparison can never touch deployed bytes.
    await expect(page.locator("form")).toHaveCount(0);
    await expect(page.locator("textarea")).toHaveCount(0);
    const sides = page.locator(".docs-compare-side");
    await expect(sides).toHaveCount(2);
    await expect(sides.nth(0)).toContainText("Guide");
    await expect(sides.nth(1)).toContainText("More.");
    const desktop = [
      await sides.nth(0).boundingBox(),
      await sides.nth(1).boundingBox(),
    ];
    expect(Math.abs(desktop[0].y - desktop[1].y)).toBeLessThan(2);
    expect(desktop[1].x).toBeGreaterThan(desktop[0].x);
    await page.setViewportSize({ width: 390, height: 844 });
    const narrow = [await sides.nth(0).boundingBox(), await sides.nth(1).boundingBox()];
    expect(narrow[1].y).toBeGreaterThanOrEqual(narrow[0].y + narrow[0].height - 1);
    expect(errors).toEqual([]);
  });
});
