// HUB-035 browser evidence: the "[[" document picker
// (internal/humanwork/productui docs_editor_suggest.go's docsSuggestList)
// renders its options as an accessible listbox whose picks write stable
// doc:<id> references (DocsSuggestDocInsert), and the backlinks panel
// (docs_backlinks.go's docsBacklinksPanel) shows only jointly readable
// sources with their state read as text, at desktop and 390px. Fixtures are
// the exact served productui output tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/docs-{picker,backlinks}.html
// (productui.DocsSuggestFixture, DocsBacklinksFixture) -- not the
// tools/uxqual/render/docs fixture renderer, which the G-L reachability
// audit found sits outside the shipped binary.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const pickerUrl = pathToFileURL(path.join(renderedDir, "docs-picker.html")).href;
const backlinksUrl = pathToFileURL(path.join(renderedDir, "docs-backlinks.html")).href;

test.describe("docs link picker", () => {
  test("keyboard picker lists options that carry stable document ids", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(pickerUrl);
    const listbox = page.getByRole("listbox");
    await expect(listbox).toBeVisible();
    await expect(listbox).toHaveAttribute("aria-labelledby", /.+/);
    const options = listbox.getByRole("option");
    await expect(options).toHaveCount(2);
    await expect(options.nth(0)).toContainText("Bee");
    await expect(options.nth(1)).toContainText("Sea");
    await expect(options.nth(0)).toHaveAttribute("aria-selected", "true");
    // The option shown carries only the document's authorized title; the
    // stable id a pick writes (DocsSuggestDocInsert -> "[Bee](doc:doc-b)")
    // is proven directly, byte for byte, by TestTodo_HUB_035
    // (internal/humanwork/productui/hub_035_served_test.go), which calls
    // this exact function and asserts on its result.
    expect(errors).toEqual([]);
  });
});

test.describe("docs backlinks", () => {
  test("states read as text at desktop and 390px", async ({ page }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(backlinksUrl);
    const nav = page.getByRole("navigation", { name: "Linked from" });
    await expect(nav).toBeVisible();
    const items = nav.locator("li");
    await expect(items).toHaveCount(2);
    await expect(items.nth(0)).toContainText("Ay");
    await expect(items.nth(1)).toContainText("Restricted source");
    await expect(items.nth(1)).toContainText("may be out of date");
    await expect(items.nth(0)).not.toContainText("may be out of date");
    // Every link routes to the stable per-document address, not a title.
    await expect(items.nth(0).locator("a")).toHaveAttribute(
      "href",
      "/workspace/app/docs?document=doc-a",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(items.nth(1)).toBeVisible();
    expect(errors).toEqual([]);
  });
});
