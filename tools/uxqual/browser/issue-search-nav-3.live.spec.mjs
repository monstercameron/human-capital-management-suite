import { test, expect } from "@playwright/test";

// Live checks for #3897, #3896 and #3857 (Chat search empty state, return to
// results, and focus after a filter change) against the running dev cell.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChat(page, hash = "") {
  await page.goto(`/workspace/app/chat?locale=en-US${hash}`);
  await expect(page.locator(".chat-rail #chat-search")).toBeVisible({
    timeout: 45000,
  });
  await expect(page.getByText("Loading conversation")).toHaveCount(0, {
    timeout: 90000,
  });
}

async function search(page, query) {
  const input = page.locator(".chat-rail #chat-search");
  await input.fill(query);
  await input.press("Enter");
  await expect(page.locator("#chatsearch-summary")).toBeVisible({ timeout: 30000 });
  await expect(
    page
      .getByText(/No matches for|\d+ results?/)
      .or(page.getByRole("button", { name: /^Open message/ }).first())
      .first(),
  ).toBeVisible({ timeout: 60000 });
}

const active = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    return {
      tag: el?.tagName,
      id: el?.id ?? "",
      text: (el?.innerText ?? "").trim().slice(0, 80),
      label: el?.getAttribute?.("aria-label") ?? "",
    };
  });

test("#3897 an unfiltered empty search has no Widen search button; a filtered one keeps it", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page);
  await search(page, "zzqnomatch");
  const widen = page.getByRole("button", { name: "Widen search" });
  await expect(widen).toHaveCount(0);
  const body = await page.locator("body").innerText();
  expect(body).toContain("Try different words");
  expect(body).not.toContain("Remove a filter");

  await openChat(page);
  await search(page, "zzqnomatch in:#people-ops");
  await expect(page.getByRole("button", { name: "Widen search" })).toBeVisible();
  await page.getByRole("button", { name: "Widen search" }).click();
  await expect(page.getByRole("button", { name: /Remove filter/ })).toHaveCount(0);
});

test("#3896 Return to results restores the opened result, its scroll offset, and recent-search focus", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await signIn(page);
  await openChat(page);
  await search(page, "review");
  const opens = page.locator("button.chatsearch-open");
  const n = await opens.count();
  expect(n).toBeGreaterThan(1);
  const target = opens.nth(n - 1);
  await target.scrollIntoViewIfNeeded();
  const scroller = await page.evaluateHandle(() => {
    return document.querySelector(".chat-search-results");
  });
  const before = await scroller.evaluate((el) => el?.scrollTop ?? 0);
  const targetKey = await target.getAttribute("data-target");
  await target.click();
  const back = page.getByRole("button", { name: "Return to results" }).first();
  await expect(back).toBeVisible({ timeout: 30000 });
  await back.click();
  await expect(page.locator("#chatsearch-summary")).toBeVisible({ timeout: 30000 });
  await expect
    .poll(async () => (await active(page)).tag, { timeout: 5000 })
    .not.toBe("BODY");
  const focused = await page.evaluate(() =>
    document.activeElement?.getAttribute("data-target"),
  );
  expect(focused).toBe(targetKey);
  const sc2 = await page.evaluateHandle(() => {
    return document.querySelector(".chat-search-results");
  });
  const after = await sc2.evaluate((el) => el?.scrollTop ?? 0);
  expect(Math.abs(after - before)).toBeLessThan(40);

  // Recent searches path.
  await openChat(page);
  await page.locator(".chat-rail #chat-search").focus();
  const recent = page.locator(".rail-search-recent-item").first();
  await expect(recent).toBeVisible({ timeout: 15000 });
  await recent.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#chatsearch-summary")).toBeVisible({ timeout: 30000 });
  await expect(page.getByRole("button", { name: /^Open result/ }).first()).toBeVisible({
    timeout: 30000,
  });
  await page.waitForTimeout(1500);
  const landed = await active(page);
  expect(landed, JSON.stringify(landed)).toMatchObject({ id: "chatsearch-summary" });
});

test("#3857 focus stays on the results line after adding and removing the channel filter", async ({
  page,
}) => {
  await signIn(page);
  await openChat(page, "#channel=general");
  await search(page, "Portland");
  const only = page.getByRole("button", { name: /Search only in #general/ });
  await expect(only).toBeVisible();
  await only.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: /Remove filter: In a channel/ }),
  ).toBeVisible({
    timeout: 30000,
  });
  await expect
    .poll(async () => (await active(page)).tag, { timeout: 5000 })
    .not.toBe("BODY");
  expect((await active(page)).id).toBe("chatsearch-summary");

  const remove = page.getByRole("button", { name: /Remove filter: In a channel/ });
  await remove.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("button", { name: /Remove filter: In a channel/ }),
  ).toHaveCount(0, {
    timeout: 30000,
  });
  await expect
    .poll(async () => (await active(page)).tag, { timeout: 5000 })
    .not.toBe("BODY");
  expect((await active(page)).id).toBe("chatsearch-summary");
});
