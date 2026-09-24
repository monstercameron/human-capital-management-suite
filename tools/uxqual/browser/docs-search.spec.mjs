// HUB-036 browser evidence: the document search screen labels result
// provenance, offers filters, renders snippets as safe text, and shows a
// visible lexical-fallback notice instead of blanking the page when
// semantic search is unavailable, at desktop and 390px. Fixture is the
// exact document tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/docs-search.html, rendered by
// productui.Render (docsSearch in docs_interactions.go) -- the served
// app's own component, not a hand-authored mirror of it.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const searchUrl = pathToFileURL(path.join(renderedDir, "docs-search.html")).href;

test.describe("docs keyword and semantic search", () => {
  test("labels provenance, offers filters, and shows the fallback notice with results intact", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(searchUrl);

    await expect(
      page.locator('#docs-search-mode option[value="semantic"]'),
    ).toBeDisabled();

    // Vector outage: fallback notice visible, and results are NOT blanked --
    // the RED case this test forbids.
    const fallback = page.locator(".docs-notice[role=status]");
    await expect(fallback).toBeVisible();
    await expect(fallback).toContainText("Semantic search is unavailable");

    const results = page.locator(".docs-search-result");
    await expect(results).toHaveCount(2);
    await expect(results.nth(0)).toContainText("Keyword match");
    await expect(results.nth(1)).toContainText("Semantic match");

    // Filters are present and keyboard-reachable native controls.
    await expect(page.locator("#docs-search-team")).toHaveValue("People Ops");
    await expect(page.locator("#docs-search-status")).toHaveValue("team_official");
    for (const id of [
      "docs-search-channel",
      "docs-search-owner",
      "docs-search-date-from",
      "docs-search-date-to",
    ]) {
      await expect(page.locator(`#${id}`)).toBeVisible();
    }

    // The snippet contains markup-shaped text but it must render as inert
    // text, not an executable script or unescaped tag.
    const snippetHTML = await page
      .locator(".docs-search-result")
      .first()
      .locator(".docs-snippet")
      .innerHTML();
    expect(snippetHTML).not.toContain("<script>");
    expect(snippetHTML).toContain("&lt;script&gt;");
    const bodyText = await page.textContent("body");
    expect(bodyText).toContain("<handbook>");

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(fallback).toBeVisible();
    await expect(results.nth(0)).toBeVisible();

    expect(errors).toEqual([]);
  });
});
