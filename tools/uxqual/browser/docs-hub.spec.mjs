// HUB-032 browser evidence: the workspace document hub distinguishes
// private drafts, shared pages, and team/channel official guidance by
// class and visible text (owner, deployed version, official scope, review
// date, sharing state), at desktop and 390px. Fixture is the exact document
// tools/uxqual/cmd/genfixtures writes to tools/uxqual/testdata/rendered/docs-hub.html,
// rendered by productui.Render (docsPage/docsLibrary) -- the served app's
// own component tree, not a hand-authored mirror of it.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const hubUrl = pathToFileURL(path.join(renderedDir, "docs-hub.html")).href;

test.describe("docs workspace hub", () => {
  test("distinguishes private drafts from official guidance at desktop and 390px", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(hubUrl);

    // One row per document, plus the column-header row.
    const rows = page.locator(".docs-row:not(.docs-row-head)");
    await expect(rows).toHaveCount(2);

    const privateItem = page.locator(".docs-row.docs-kind-private");
    await expect(privateItem).toBeVisible();
    await expect(privateItem).toContainText("Private draft");
    // The signed-in fixture principal (reader-a) owns this draft, so the
    // access column reads "You"/"Only you" rather than a bare subject id --
    // the same private-vs-mine distinction TestTodo_HUB_032 asserts on.
    await expect(privateItem).toContainText("Only you");

    const officialItem = page.locator(".docs-row.docs-kind-team_official");
    await expect(officialItem).toBeVisible();
    await expect(officialItem).toContainText("Team handbook");
    await expect(officialItem).toContainText("People Ops");
    await expect(officialItem).toContainText("v7");
    await expect(officialItem).toContainText("2026-10-01");
    await expect(officialItem).toContainText("Team guidance");

    await expect(
      page.locator('a[href="/workspace/app/docs?document=doc-private"]'),
    ).toBeVisible();
    await expect(
      page.locator('a[href="/workspace/app/docs?document=doc-team"]'),
    ).toBeVisible();

    // The two kinds must be visually distinct, not just textually: distinct
    // border-inline-start colors driven by the docs-kind-* class, not color
    // alone deciding meaning (the text above already carries that).
    const privateBorder = await privateItem.evaluate(
      (el) => window.getComputedStyle(el).borderInlineStartColor,
    );
    const officialBorder = await officialItem.evaluate(
      (el) => window.getComputedStyle(el).borderInlineStartColor,
    );
    expect(privateBorder).not.toBe(officialBorder);

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(privateItem).toBeVisible();
    await expect(officialItem).toBeVisible();
    await expect(officialItem).toContainText("Team handbook");

    expect(errors).toEqual([]);
  });
});
