import { test, expect } from "@playwright/test";

// Chat search pagination and file results (#3900, #3899, #3898), against the
// live dev cell. Search is read-only, so nothing is written. The demo has only
// six ".png" files, so the paging checks use the one-letter query "a", which
// matches more than one page.

test.setTimeout(240000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function search(page, query) {
  await page.goto("/workspace/app/chat?locale=en-US");
  // The Go/WASM client must be running before Enter does anything.
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  const input = page.locator(".chat-rail #chat-search");
  await input.fill(query);
  await input.press("Enter");
  await expect(page.locator(".chatsearch-result").first()).toBeVisible({
    timeout: 60000,
  });
  return page.locator("#chatsearch-results");
}

const headCount = (page) => page.locator(".conversation-header .search-head-count");

test("#3900 the search count says results are shown so far while more are available", async ({
  page,
}) => {
  await signIn(page);
  const view = await search(page, "a");
  await expect(view.locator("[data-chatsearch-action='more']")).toBeVisible();
  await expect(headCount(page)).toHaveText(/^\d+ results shown, more available$/);
  const first = Number((await headCount(page).innerText()).match(/\d+/)[0]);
  let guard = 0;
  while (
    (await view.locator("[data-chatsearch-action='more']").count()) > 0 &&
    guard++ < 20
  ) {
    const before = await view.locator(".chatsearch-result").count();
    await view.locator("[data-chatsearch-action='more']").click();
    await expect
      .poll(() => view.locator(".chatsearch-result").count(), { timeout: 40000 })
      .toBeGreaterThan(before);
    await expect(view.locator("button.chatsearch-more[aria-busy='true']")).toHaveCount(
      0,
      {
        timeout: 40000,
      },
    );
  }
  // At the last page the count is a plain total, larger than the first page.
  await expect(headCount(page)).toHaveText(/^\d+ results$/);
  const total = Number((await headCount(page).innerText()).match(/\d+/)[0]);
  expect(total).toBeGreaterThan(first);
  expect(total).toBe(await view.locator(".chatsearch-result").count());
});

test("#3899 loading more keeps the list, moves focus to the first new result and announces it", async ({
  page,
}) => {
  await signIn(page);
  const view = await search(page, "a");
  const results = view.locator(".chatsearch-result");
  const before = await results.count();
  const firstText = await results.first().innerText();
  const more = view.locator("[data-chatsearch-action='more']");
  await more.scrollIntoViewIfNeeded();
  await more.focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => results.count(), { timeout: 40000 }).toBeGreaterThan(before);
  // The reviewed results stayed (same first row) and focus did not fall to BODY.
  expect(await results.first().innerText()).toBe(firstText);
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const el = document.activeElement;
          return el ? el.tagName : "";
        }),
      { timeout: 10000 },
    )
    .not.toBe("BODY");
  const where = await page.evaluate((count) => {
    const el = document.activeElement;
    const all = [...document.querySelectorAll(".chatsearch-result")];
    return {
      id: el && el.id,
      index: all.findIndex((r) => r === el || r.contains(el)),
      count,
    };
  }, before);
  // Focus is on a result that was not there before, or on the announcement seam.
  if (where.index >= 0) expect(where.index).toBeGreaterThanOrEqual(before);
  else expect(where.id).toBe("chatsearch-boundary");
  const status = view.locator("#chatsearch-boundary");
  await expect(status).toHaveCount(1);
  await expect(status).toHaveAttribute("role", "status");
  expect(await status.innerText()).toMatch(/more results added|every result/i);
});

test("#3898 file results name the file in the open control's accessible name", async ({
  page,
}) => {
  await signIn(page);
  const view = await search(page, ".png");
  const buttons = view.locator("button.chatsearch-open[data-kind='file']");
  expect(await buttons.count()).toBeGreaterThan(0);
  for (const label of await buttons.evaluateAll((els) =>
    els.map((e) => e.getAttribute("aria-label")),
  )) {
    expect(label).toMatch(/^Open result: File · .+\.png · in #?.+/);
  }
  // The accessibility tree agrees: the role=button name carries the file name.
  const named = view.getByRole("button", {
    name: /Open result: File · comp-bands\.png/,
  });
  expect(await named.count()).toBeGreaterThan(0);
});
