// HUB-034 browser evidence: the reviewer/publisher screen shows the exact
// diff, content hash, and deploy scope for a candidate version, and deploy
// requires current authority with the review/deploy forms only appearing
// when authorized, at desktop and 390px. Fixtures are the exact documents
// tools/uxqual/cmd/genfixtures writes to
// tools/uxqual/testdata/rendered/docs-review{,-stale}.html, rendered by
// productui.Render (docsReviewControls in docs_interactions.go) -- the
// served app's own component, not a hand-authored mirror of it.
import { test, expect } from "@playwright/test";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const renderedDir = path.resolve(here, "..", "testdata", "rendered");
const reviewUrl = pathToFileURL(path.join(renderedDir, "docs-review.html")).href;
const staleUrl = pathToFileURL(path.join(renderedDir, "docs-review-stale.html")).href;

test.describe("docs reviewer and publisher controls", () => {
  test("shows exact hash, scope, and diff, with working actions at desktop and 390px", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(reviewUrl);

    const item = page.locator(".docs-review-item");
    await expect(item).toContainText("9e1e4a7c");
    await expect(item).toContainText("People Ops (team)");
    await expect(item).toContainText("Old vacation policy text");
    await expect(item).toContainText("New vacation policy text");

    const reviewForm = page.locator('form[action="/docs/doc-team/review"]');
    const deployForm = page.locator('form[action="/docs/doc-team/deploy"]');
    await expect(reviewForm).toHaveCount(1);
    await expect(deployForm).toHaveCount(1);
    // The reviewed hash and the deployed hash must be the literal same
    // value, read from the same hidden field the review and deploy forms
    // each carry: this is what stops a UI from showing a reviewer one hash
    // and publishing a different one.
    const reviewHash = await reviewForm
      .locator('input[name="version_hash"]')
      .inputValue();
    const deployHash = await deployForm
      .locator('input[name="version_hash"]')
      .inputValue();
    expect(reviewHash).toBe("9e1e4a7c");
    expect(deployHash).toBe("9e1e4a7c");

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(item).toBeVisible();
    await expect(deployForm.locator('button[type="submit"]')).toBeVisible();

    expect(errors).toEqual([]);
  });

  test("renders no review or deploy form, and exposes no hash or diff, once authority is no longer current", async ({
    page,
  }) => {
    const errors = [];
    page.on("pageerror", (err) => errors.push(err));
    await page.goto(staleUrl);

    // docsReviewControls (docs_interactions.go) independently checks
    // CanReview/CanDeploy per render and drops an item entirely once both
    // are false, rather than rendering a disabled form: nothing about the
    // out-of-authority candidate version -- not its forms, not its exact
    // hash or diff -- reaches the page.
    await expect(page.locator("form.docs-action-form")).toHaveCount(0);
    await expect(page.locator('form[action="/docs/doc-team/review"]')).toHaveCount(0);
    await expect(page.locator('form[action="/docs/doc-team/deploy"]')).toHaveCount(0);
    await expect(page.locator(".docs-review-item")).toHaveCount(0);
    const bodyText = await page.textContent("body");
    expect(bodyText).not.toContain("9e1e4a7c");
    expect(bodyText).not.toContain("Old vacation policy text");

    expect(errors).toEqual([]);
  });
});
