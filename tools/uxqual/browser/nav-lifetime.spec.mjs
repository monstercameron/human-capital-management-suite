/* global performance */

import { test, expect as baseExpect } from "@playwright/test";

const expect = baseExpect.configure({ timeout: 20000 });

test.setTimeout(120000);

const enrollment = "doc-e5a067c2-aafc-4b0b-8679-0eed24bf314a";

async function signIn(page, persona) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function pageOrigin(page) {
  return page.evaluate(() => performance.timeOrigin);
}

async function expectSameDocument(page, origin) {
  expect(await pageOrigin(page), "navigation replaced the browser document").toBe(
    origin,
  );
}

test("Docs reader links and browser history keep one page lifetime", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto(`/workspace/app/docs?document=${enrollment}`);
  await expect(page.locator("#page-title")).toHaveText(
    "Open enrollment 2027: questions and answers",
    { timeout: 20000 },
  );
  const origin = await pageOrigin(page);

  await page.getByRole("link", { name: "Open: Parental leave handout 2027" }).click();
  await expect(page.locator("#page-title")).toHaveText("Parental leave handout 2027");
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.locator("#page-title")).toHaveText(
    "Open enrollment 2027: questions and answers",
  );
  await expectSameDocument(page, origin);

  await page.goForward();
  await expect(page.locator("#page-title")).toHaveText("Parental leave handout 2027");
  await expectSameDocument(page, origin);

  await page.getByRole("link", { name: "Back to documents" }).click();
  await expect(page).toHaveURL(/\/workspace\/app\/docs(?:\?|$)/);
  await expect(page.locator(".docs-title-link").first()).toBeVisible();
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.locator("#page-title")).toHaveText("Parental leave handout 2027");
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.locator("#page-title")).toHaveText(
    "Open enrollment 2027: questions and answers",
  );
  await page.getByRole("link", { name: /Open channel #announcements/ }).click();
  await expect(page.locator(".conversation-heading h1")).toHaveText("announcements");
  await expectSameDocument(page, origin);
  await page.goBack();
  await expect(page.locator("#page-title")).toHaveText(
    "Open enrollment 2027: questions and answers",
  );
  await expectSameDocument(page, origin);
});

test("Chat channel changes and cross-app history keep one page lifetime", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat");
  await expect(
    page.locator('.chat-rail-row [data-action="select"]').first(),
  ).toBeVisible({ timeout: 20000 });
  const origin = await pageOrigin(page);
  const select = (name) =>
    page
      .locator('.chat-rail-row [data-action="select"]', { hasText: name })
      .first()
      .click();

  await select("announcements");
  await expect(page.locator(".conversation-heading h1")).toHaveText("announcements");
  await expect(page).toHaveURL(/#channel=announcements$/);
  await expectSameDocument(page, origin);

  await select("benefits");
  await expect(page.locator(".conversation-heading h1")).toHaveText("benefits");
  await expect(page).toHaveURL(/#channel=benefits$/);
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.locator(".conversation-heading h1")).toHaveText("announcements");
  await expectSameDocument(page, origin);
  await page.goForward();
  await expect(page.locator(".conversation-heading h1")).toHaveText("benefits");
  await expectSameDocument(page, origin);

  await page.getByRole("link", { name: "Docs", exact: true }).click();
  await expect(page).toHaveURL(/\/workspace\/app\/docs/);
  await expect(page.locator(".docs-title-link").first()).toBeVisible();
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.locator(".conversation-heading h1")).toHaveText("benefits");
  await expectSameDocument(page, origin);
  await page.goForward();
  await expect(page).toHaveURL(/\/workspace\/app\/docs/);
  await expectSameDocument(page, origin);

  await page.locator(".history-navigation-back").click();
  await expect(page.locator(".conversation-heading h1")).toHaveText("benefits");
  await expectSameDocument(page, origin);
  await page.locator(".history-navigation-forward").click();
  await expect(page.locator(".docs-title-link").first()).toBeVisible();
  await expectSameDocument(page, origin);
});

test("Docs list paging, folders, search and sort never reload the page", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/docs");
  await expect(page.locator(".docs-title-link").first()).toBeVisible();
  const origin = await pageOrigin(page);

  await page.locator('.docs-page-link[aria-label="Page 2"]').click();
  await expect(page).toHaveURL(/docs_page=2/);
  await expectSameDocument(page, origin);
  await page.locator(".history-navigation-back").click();
  await expect(page).not.toHaveURL(/docs_page=2/);
  await expectSameDocument(page, origin);
  await page.locator(".history-navigation-forward").click();
  await expect(page).toHaveURL(/docs_page=2/);
  await expectSameDocument(page, origin);

  await page.locator(".docs-nav-link", { hasText: "Archive" }).click();
  await expect(page).toHaveURL(/folder=/);
  await expectSameDocument(page, origin);

  const search = page.locator("#docs-browse-query");
  await search.fill("enrollment");
  await search.press("Enter");
  await expect(page).toHaveURL(/docs_q=enrollment/);
  await expectSameDocument(page, origin);

  await page.locator("#docs-sort").selectOption("title");
  await expect(page).toHaveURL(/docs_sort=title/);
  await expectSameDocument(page, origin);
  await page.locator(".history-navigation-back").click();
  await expect(page).not.toHaveURL(/docs_sort=title/);
  await expectSameDocument(page, origin);
});

test("inaccessible private link stays unavailable through reload and history", async ({
  page,
}) => {
  await signIn(page, "Thomas Baker");
  await page.goto("/workspace/app/chat#channel=qa-nav-recheck-0924");
  await expect(page.getByRole("alert")).toContainText(
    "This channel is no longer available",
    { timeout: 30000 },
  );
  await expect(page.locator(".conversation-heading h1")).not.toHaveText("general");
  const origin = await pageOrigin(page);

  await page
    .locator('.chat-rail-row [data-action="select"]', { hasText: "general" })
    .first()
    .click();
  await expect(page.locator(".conversation-heading h1")).toHaveText("general");
  await expect(page).toHaveURL(/#channel=general$/);
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page).toHaveURL(/#channel=qa-nav-recheck-0924$/);
  await expect(page.locator(".conversation-heading h1")).not.toHaveText("general");
  await expect(page.getByRole("alert")).toBeVisible();
  await expectSameDocument(page, origin);

  await page.goForward();
  await expect(page.locator(".conversation-heading h1")).toHaveText("general");
  await expectSameDocument(page, origin);

  await page.goBack();
  await expect(page.getByRole("alert")).toBeVisible();
  await page.reload();
  await expect(page.getByRole("alert")).toContainText(
    "This channel is no longer available",
    { timeout: 30000 },
  );
  await expect(page.locator(".conversation-heading h1")).not.toHaveText("general");

  await page.goto("/workspace/app/chat#channel=qa-browser-review");
  await expect(page.getByRole("alert")).toContainText(
    "This channel is no longer available",
    { timeout: 30000 },
  );
  await expect(page.locator(".conversation-heading h1")).not.toHaveText("general");
  await page.reload();
  await expect(page.getByRole("alert")).toContainText(
    "This channel is no longer available",
    { timeout: 30000 },
  );
  await expect(page.locator(".conversation-heading h1")).not.toHaveText("general");
});
