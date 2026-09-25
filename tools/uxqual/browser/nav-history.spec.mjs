// NAV-01 (P0): URL, visible resource, history and reload must agree across
// Chat and Docs.
//
// This spec runs against the LIVE dev server at http://127.0.0.1:8280 (see
// playwright.config.mjs for baseURL) and replays every row of the audit
// table in .artifacts/tmp/ui9/fix-nav.md, asserting after each step that
// location.href, the visible heading/selection and any relevant control
// value all agree -- including after a hard reload.
//
// Seed data substitutions (the audit's row text names generic examples; the
// dev seed in cmd/migrate/chat_seed.go and cmd/migrate/document_seed_demo.go
// has concrete rooms/documents/folders):
//   - "#announcements" / "#benefits" are real seeded public channels
//     (cmd/migrate/chat_seed.go) the Rafael Torres persona is a member of.
//   - the audit's "Archive folder" row uses the live "Archive" folder
//     (the review database has one with 8 documents).
//   - the seed posts no document link into chat, so the Chat -> linked
//     document row posts one into #qa-browser-review itself.
//   - chat rooms are addressed as "#channel=<name>" for uniquely named
//     channels (the audit's readable form); ids remain accepted.
//   - "enrollment" is a real seeded document/chat topic keyword ("Open
//     enrollment 2027: questions and answers").
//
// Required outcome (fix-nav.md): the URL is the source of truth for every
// navigable resource; popstate (browser Back/Forward and the shell's global
// Back/Forward buttons) re-derives the whole view from the URL; a
// cross-surface navigation swaps the page component, not only the URL;
// reload restores exactly the visible resource.
/* global URL, URLSearchParams */

import { test, expect } from "@playwright/test";

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

// The global history controls beside search; see
// internal/humanwork/productui/history_navigation.go.
function globalBack(page) {
  return page.locator(".history-navigation-back");
}
function globalForward(page) {
  return page.locator(".history-navigation-forward");
}

// The chat room heading; see internal/humanwork/chatui/render.go's timeline().
function chatHeading(page) {
  return page.locator(".conversation-heading h1");
}

function selectChatRoom(page, name) {
  return page
    .locator('.chat-rail-row [data-action="select"]', { hasText: name })
    .first()
    .click();
}

test.describe("NAV-01 chat: URL, selection, history and reload agree", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await signInAsRafael(page);
    await page.goto("/workspace/app/chat");
    await page.waitForLoadState("networkidle");
  });

  test("selecting a room writes the URL, and Back/Forward/reload all agree with what is visible", async ({
    page,
  }) => {
    await selectChatRoom(page, "announcements");
    await expect(page).toHaveURL(/#channel=announcements$/);
    await expect(chatHeading(page)).toHaveText("announcements");

    await selectChatRoom(page, "benefits");
    await expect(page).toHaveURL(/#channel=benefits$/);
    await expect(chatHeading(page)).toHaveText("benefits");

    // Global Back must change BOTH the address and the visible room.
    await globalBack(page).click();
    await expect(page).toHaveURL(/#channel=announcements$/);
    await expect(chatHeading(page)).toHaveText("announcements");

    // Global Forward must restore the later room, in both places too.
    await globalForward(page).click();
    await expect(page).toHaveURL(/#channel=benefits$/);
    await expect(chatHeading(page)).toHaveText("benefits");

    // A reload must restore exactly the visible resource the URL now names
    // (#benefits) -- not fall back to a default room.
    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(/#channel=benefits$/);
    await expect(chatHeading(page)).toHaveText("benefits");

    // After the reload, Back must still restore the room the URL had before
    // it (#announcements) -- browser session history survives the reload.
    await globalBack(page).click();
    await expect(page).toHaveURL(/#channel=announcements$/);
    await expect(chatHeading(page)).toHaveText("announcements");
  });

  test("opening a search result selects and addresses its room, and reload keeps it", async ({
    page,
  }) => {
    // The chat rail's own search box (chatui render.go, #chat-search) -- not
    // the shell's global search combobox, which is also type=search. Typed
    // key by key: fill() does not run a GWC OnInput handler.
    // Wait for the WASM app to mount the rail: the server-rendered search box
    // is replaced when it mounts (measured ~5 s after networkidle), and text
    // typed before that is lost with the old element.
    await expect(
      page.locator('.chat-rail-row [data-action="select"]').first(),
    ).toBeVisible({
      timeout: 15000,
    });
    const search = page.locator("#chat-search");
    await search.click();
    await search.pressSequentially("enrollment");
    await expect(search).toHaveValue("enrollment");
    const results = page.locator(
      '.search-message-result[data-action="open-search-message"]',
    );
    await expect(results.first()).toBeVisible({ timeout: 10000 });
    // Prefer a hit in #benefits (the audit's row); any message hit will do.
    const inBenefits = results.filter({ hasText: "benefits" });
    const result =
      (await inBenefits.count()) > 0 ? inBenefits.first() : results.first();
    await result.click();

    // The room the header now shows must be exactly the room the address
    // names -- not the generic Chat page URL the audit found stuck here.
    await expect(page).toHaveURL(/#channel=[^&]+$/);
    await expect(chatHeading(page)).not.toHaveText(/enrollment/);
    const heading = (await chatHeading(page).textContent())?.trim() ?? "";
    const channel = new URLSearchParams(new URL(page.url()).hash.slice(1)).get(
      "channel",
    );
    expect(channel, "opening a search result must address its room by name").toBe(
      heading,
    );

    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(new RegExp(`#channel=${channel}$`));
    await expect(chatHeading(page)).toHaveText(heading);
  });

  test("Chat -> a linked document -> global Back swaps the page component back to Chat, not only the URL", async ({
    page,
  }) => {
    test.setTimeout(90000);
    // The dev seed posts no document links into chat, so this row posts one:
    // it takes a real document's canonical URL from the Docs list and sends
    // it into the QA review room (a private channel Rafael belongs to), where
    // chatui renders it as an "Open document" reference/unfurl.
    await page.goto("/workspace/app/docs");
    const firstDoc = page.locator("a.docs-title-link").first();
    await expect(firstDoc).toBeVisible({ timeout: 15000 });
    const docTitle = ((await firstDoc.textContent()) ?? "").trim();
    const docHref = new URL((await firstDoc.getAttribute("href")) ?? "", page.url());
    const documentID = docHref.searchParams.get("document");
    expect(documentID, "the Docs list must link documents by ?document=").toBeTruthy();

    await page.goto("/workspace/app/chat");
    await selectChatRoom(page, "qa-browser-review");
    await expect(chatHeading(page)).toHaveText("qa-browser-review");
    const docURL = `${new URL(page.url()).origin}/workspace/app/docs?document=${encodeURIComponent(documentID)}`;
    const composer = page.locator("#chat-composer");
    await composer.click();
    await composer.pressSequentially(`NAV-01 link check ${docURL}`);
    await composer.press("Enter");

    const docLink = page
      .locator(`a[data-action="open-doc-reference"][data-id="${documentID}"]`)
      .last();
    await expect(docLink).toBeVisible({ timeout: 10000 });
    await docLink.click();
    await page.waitForURL(/\/workspace\/app\/docs\?document=/);
    await expect(page.locator("#page-title")).toHaveText(docTitle, {
      useInnerText: true,
    });
    await expect(page.locator(".chat-rail")).toHaveCount(0);

    await globalBack(page).click();
    await expect(page).toHaveURL(/\/workspace\/app\/chat#channel=qa-browser-review$/);
    // The page component itself must have swapped back to Chat: the chat
    // rail and room heading must be mounted, and the Docs reader must not
    // still be showing underneath a URL that now says Chat.
    await expect(page.locator(".chat-rail")).toBeVisible();
    await expect(chatHeading(page)).toHaveText("qa-browser-review");
    await expect(page.locator("#page-title")).toHaveCount(0);
  });
});

test.describe("NAV-01 docs: URL, list/reader identity, history and reload agree", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await signInAsRafael(page);
    await page.goto("/workspace/app/docs");
    await page.waitForLoadState("networkidle");
  });

  test("All documents -> a document -> global Back returns to the list, not just the list URL", async ({
    page,
  }) => {
    const firstDoc = page.locator("a.docs-title-link").first();
    const title = await firstDoc.textContent();
    await firstDoc.click();
    await page.waitForURL(/document=/);
    await expect(page.locator("#page-title")).toHaveText(title?.trim() ?? "", {
      useInnerText: true,
    });

    await globalBack(page).click();
    await expect(page).not.toHaveURL(/document=/);
    // The reader must not still be visible under the list's URL.
    await expect(page.locator("a.docs-title-link").first()).toBeVisible();
    await expect(page.locator(".docs-back")).toHaveCount(0);
  });

  test("page 1 -> page 2 -> global Back shows page 1's rows again, not page 2's", async ({
    page,
  }) => {
    const rows = page.locator("a.docs-title-link");
    await expect(rows.first()).toBeVisible();
    const page2 = page.locator('.docs-page-link[aria-label="Page 2"]');
    // The seed has 176 documents at 50 rows a page; a missing pager is a bug.
    await expect(page2).toBeVisible();
    const firstRowPage1 = await rows.first().textContent();
    await page2.click();
    await expect(page).toHaveURL(/docs_page=2/);
    await expect
      .poll(() => rows.first().textContent(), {
        message: "page 2 must show different rows than page 1",
      })
      .not.toBe(firstRowPage1);
    await expect(page.locator('.docs-page-link[aria-current="page"]')).toHaveText("2");

    await globalBack(page).click();
    await expect(page).not.toHaveURL(/docs_page=2/);
    await expect.poll(() => rows.first().textContent()).toBe(firstRowPage1);
    await expect(page.locator('.docs-page-link[aria-current="page"]')).toHaveText("1");

    // Forward and reload must agree with the address too.
    await globalForward(page).click();
    await expect(page).toHaveURL(/docs_page=2/);
    await expect.poll(() => rows.first().textContent()).not.toBe(firstRowPage1);
    const firstRowPage2 = await rows.first().textContent();
    await page.reload();
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(/docs_page=2/);
    await expect.poll(() => rows.first().textContent()).toBe(firstRowPage2);
  });

  test("the Archive folder -> global Back returns to All documents, not the folder's rows under the base URL", async ({
    page,
  }) => {
    const rows = page.locator("a.docs-title-link");
    await expect(rows.first()).toBeVisible();
    const allFirstRow = await rows.first().textContent();
    const folderLink = page.locator(".docs-nav-link", { hasText: "Archive" });
    await expect(folderLink).toBeVisible();
    await folderLink.click();
    await expect(page).toHaveURL(/folder=/);
    await expect(
      page.locator('.docs-nav-link[aria-current="page"]', { hasText: "Archive" }),
    ).toBeVisible();
    await expect(page.locator("#page-title")).toContainText("Archive");

    await globalBack(page).click();
    await expect(page).not.toHaveURL(/folder=/);
    // aria-current="page" on "All documents" must move back with the URL,
    // and the heading and rows must be All documents' again.
    await expect(
      page.locator('.docs-nav-link[aria-current="page"]', { hasText: "All" }),
    ).toBeVisible();
    await expect(page.locator("#page-title")).not.toContainText("Archive");
    await expect.poll(() => rows.first().textContent()).toBe(allFirstRow);
  });

  test("search + Title A-Z sort -> global Back drops docs_sort from the URL AND resets the Sort control", async ({
    page,
  }) => {
    const search = page.locator("#docs-browse-query");
    await search.fill("enrollment");
    await search.press("Enter");
    await page.waitForURL(/docs_q=enrollment/);

    const sortSelect = page.locator("#docs-sort");
    await sortSelect.selectOption("title");
    await expect(page).toHaveURL(/docs_sort=title/);
    await expect(sortSelect).toHaveValue("title");

    await globalBack(page).click();
    await expect(page).not.toHaveURL(/docs_sort=title/);
    // The audit's RED clause: "Sort control still showed Title A-Z" after
    // Back dropped docs_sort from the URL. The <select> must be controlled
    // from the (now reverted) URL state, not left showing the old DOM value.
    await expect(sortSelect).not.toHaveValue("title");
  });
});
