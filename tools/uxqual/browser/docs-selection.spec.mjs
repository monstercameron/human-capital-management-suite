/* global Event, NodeFilter */

import { test, expect } from "@playwright/test";

test.setTimeout(90000);

const highlights = "doc-1f18bf84-c81c-415f-960e-5f2472eeface";

async function signIn(page, persona) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("a source selection becomes the link label without saving the draft", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto(`/workspace/app/docs?document=${highlights}`);
  await expect(page.locator("#page-title")).toHaveText(
    "People Ops chat highlights: this month",
    { timeout: 20000 },
  );
  await page.getByRole("button", { name: "Edit document" }).click();
  const source = page.locator("#docs-editor-source");
  await expect(source).toBeVisible({ timeout: 20000 });
  await expect(source).toHaveValue(/The conversations worth keeping/);
  const label = "The conversations worth keeping";
  await source.evaluate((element, value) => {
    const start = element.value.indexOf(value);
    if (start < 0) throw new Error("link label not found in source");
    element.focus();
    element.setSelectionRange(start, start + value.length);
  }, label);
  await page.locator('[data-editor-cmd="link"]').click();
  await expect(page.locator("#docs-editor-link")).toBeVisible();
  await page.locator("#docs-editor-link-url").fill("https://example.com/context");
  await page.getByRole("button", { name: "Add link" }).click();
  await expect(source).toHaveValue(
    new RegExp(`\\[${label}\\]\\(https://example.com/context\\)`),
  );
});

test("a formatted-text selection becomes the link label without saving the draft", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto(`/workspace/app/docs?document=${highlights}`);
  await expect(page.locator("#page-title")).toHaveText(
    "People Ops chat highlights: this month",
    { timeout: 20000 },
  );
  await page.getByRole("button", { name: "Edit document" }).click();
  await expect(page.locator("#docs-editor-rich")).toContainText(
    "The conversations worth keeping",
    { timeout: 20000 },
  );
  const label = "The conversations worth keeping";
  await page.evaluate((value) => {
    const root = document.querySelector("#docs-editor-rich");
    root.focus();
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
      if (node.textContent.includes(value)) break;
    }
    if (!node) throw new Error("link label not found in formatted pane");
    const start = node.textContent.indexOf(value);
    const range = document.createRange();
    range.setStart(node, start);
    range.setEnd(node, start + value.length);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  }, label);
  await page.locator('[data-editor-cmd="link"]').click();
  await page.locator("#docs-editor-link-url").fill("https://example.com/context");
  await page.getByRole("button", { name: "Add link" }).click();
  await expect(page.locator("#docs-editor-source")).toHaveValue(
    new RegExp(`\\[${label}\\]\\(https://example.com/context\\)`),
  );
});

test("a direct edit link loads the document before editing", async ({ page }) => {
  await signIn(page, "Rafael Torres");
  await page.goto(`/workspace/app/docs?document=${highlights}&docs_edit=1`);
  const source = page.locator("#docs-editor-source");
  await expect(source).toBeVisible({ timeout: 20000 });
  await expect(source).toHaveValue(/The conversations worth keeping/, {
    timeout: 20000,
  });
  await expect(page.locator("#docs-editor-rich")).toContainText(
    "The conversations worth keeping",
  );
});

test("selecting an embedded Chat excerpt offers a whole-document comment with context", async ({
  page,
}) => {
  await signIn(page, "Thomas Baker");
  await page.goto(`/workspace/app/docs?document=${highlights}`);
  await expect(page.locator(".docs-chat-quote-body").first()).toContainText(
    "The team charter is published",
    { timeout: 20000 },
  );
  await page.evaluate(() => {
    const root = document.querySelector(".docs-chat-quote-body");
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
      if (node.textContent.includes("The team charter is published")) break;
    }
    if (!node) throw new Error("embedded Chat excerpt is missing");
    const range = document.createRange();
    range.setStart(node, 0);
    range.setEnd(node, "The team charter is published".length);
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));
  });
  const commentSelection = page.locator("#docs-select-comment");
  await expect(commentSelection).toHaveClass(/is-visible/);
  await commentSelection.click();
  await expect(page.locator(".docs-compose-quote-label")).toHaveText(
    "About selected Chat text",
  );
  await expect(page.locator(".docs-compose-quote")).toContainText(
    "The team charter is published",
  );
  await expect(page.locator(".docs-comments")).toContainText(
    "The comment applies to the whole document",
  );
});
