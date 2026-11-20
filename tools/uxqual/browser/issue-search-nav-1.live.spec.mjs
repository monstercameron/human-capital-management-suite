import { test, expect } from "@playwright/test";

// Live checks for the search-nav lane, chunk 1: #3989, #3988, #3941.
// Read-only: the specs only search and open seeded conversations.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function search(page, locale, channel, query) {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 90000,
  });
  const input = page.locator(".chat-rail #chat-search");
  await expect(input).toBeVisible({ timeout: 60000 });
  await input.fill(query);
  await input.press("Enter");
  await expect(page.locator(".chatsearch-result").first()).toBeVisible({
    timeout: 90000,
  });
}

// Where the element sits inside its own scrolling timeline, and who has focus.
async function placement(page, selector) {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) return { found: false };
    const r = el.getBoundingClientRect();
    const a = document.activeElement;
    let sc = el.parentElement;
    while (
      sc &&
      !(
        sc.scrollHeight > sc.clientHeight + 1 &&
        /(auto|scroll)/.test(window.getComputedStyle(sc).overflowY)
      )
    )
      sc = sc.parentElement;
    const sr = sc ? sc.getBoundingClientRect() : { top: 0, bottom: window.innerHeight };
    return {
      found: true,
      top: r.top - sr.top,
      bottom: r.bottom - sr.top,
      height: sr.bottom - sr.top,
      outline: window.getComputedStyle(el).outlineStyle,
      focused: a === el || el.contains(a),
      active: a ? a.tagName + "." + a.className : "",
    };
  }, selector);
}

async function openResult(page, where, kind) {
  const results = page.locator(".chatsearch-result");
  const n = await results.count();
  for (let i = 0; i < n; i++) {
    const row = results.nth(i);
    const label =
      (await row.locator(".chatsearch-open").getAttribute("aria-label")) ?? "";
    const k = await row.getAttribute("data-kind");
    if (where.test(label) && (!kind || k === kind)) {
      await row.locator(".chatsearch-open").click();
      return;
    }
  }
  throw new Error(`no ${kind ?? "any"} result matching ${where} among ${n}`);
}

async function expectTargetShown(page, selector) {
  // The highlight class lasts about three seconds, so judge it as soon as it appears.
  await expect(page.locator(selector)).toHaveCount(1, { timeout: 60000 });
  const id = await page.locator(selector).getAttribute("data-message-id");
  await expect
    .poll(async () => (await placement(page, selector)).focused, {
      timeout: 3000,
      intervals: [50],
    })
    .toBe(true);
  const st = await placement(page, selector);
  expect(st.found, "the highlight is still on the target").toBe(true);
  expect(st.top, "target top inside its timeline").toBeGreaterThanOrEqual(0);
  expect(st.bottom, "target bottom inside its timeline").toBeLessThanOrEqual(st.height);
  expect(st.focused, `focus on target, was ${st.active}`).toBe(true);
  expect(st.outline, "visible outline").not.toBe("none");
  // Later layout (images) must not push the hit out of view.
  await page.waitForTimeout(5000);
  const late = await placement(page, `.message[data-message-id="${id}"]`);
  expect(late.top).toBeGreaterThanOrEqual(0);
  expect(late.bottom).toBeLessThanOrEqual(late.height);
}

test("#3989 cross-channel hit (promotion, general to people-ops) is in view, outlined and focused; Return to results works", async ({
  page,
}) => {
  await signIn(page);
  await search(page, "en-US", "general", "promotion");
  await openResult(page, /people-ops/i, "message");
  await expectTargetShown(page, ".message.search-target");
  const back = page.getByRole("button", { name: /Return to results/i }).first();
  await expect(back).toBeVisible();
  await back.click();
  await expect(page.locator(".chatsearch-result").first()).toBeVisible({
    timeout: 60000,
  });
});

test("#3989 second cold destination (deadline, general to engineering) and the already-open control", async ({
  page,
}) => {
  await signIn(page);
  await search(page, "en-US", "general", "deadline");
  await openResult(page, /engineering/i, "message");
  await expectTargetShown(page, ".message.search-target");
  // Control: reopen the same hit while the channel is already loaded.
  await page
    .getByRole("button", { name: /Return to results/i })
    .first()
    .click();
  await expect(page.locator(".chatsearch-result").first()).toBeVisible({
    timeout: 60000,
  });
  await openResult(page, /engineering/i, "message");
  await expectTargetShown(page, ".message.search-target");
});

for (const channel of ["general", "people-ops"]) {
  test(`#3988 thread-reply hit in #${channel} marks, names and focuses the reply`, async ({
    page,
  }) => {
    await signIn(page);
    await search(page, "en-US", "general", "Regions can go earlier");
    await openResult(page, new RegExp(`in #${channel}`, "i"), "thread");
    const sel = ".thread-message.search-target";
    await expect(page.locator(sel)).toHaveCount(1, { timeout: 60000 });
    await expect
      .poll(async () => (await placement(page, sel)).focused, {
        timeout: 3000,
        intervals: [50],
      })
      .toBe(true);
    const st = await placement(page, sel);
    expect(st.focused, `focus on reply, was ${st.active}`).toBe(true);
    const reply = page.locator(sel);
    expect(await reply.getAttribute("tabindex")).toBe("0");
    expect(await reply.getAttribute("aria-label")).toMatch(/Search result/i);
    expect(
      await reply.evaluate((e) => window.getComputedStyle(e).outlineStyle),
    ).not.toBe("none");
    // Only the matching reply is marked.
    expect(await page.locator(".thread-message.search-target").count()).toBe(1);
    await expect(
      page.getByRole("button", { name: /Return to results/i }).first(),
    ).toBeVisible();
  });
}

test("#3941 result buttons are described by their own excerpt (ar)", async ({
  page,
}) => {
  await signIn(page);
  await search(page, "ar", "general", "the");
  const data = await page.evaluate(() =>
    [...document.querySelectorAll(".chatsearch-open[aria-describedby]")].map((b) => {
      const d = document.getElementById(b.getAttribute("aria-describedby"));
      return {
        id: b.getAttribute("aria-describedby"),
        desc: d ? d.textContent.trim() : null,
        inRow: d ? b.parentElement.contains(d) : false,
        label: b.getAttribute("aria-label"),
      };
    }),
  );
  expect(data.length).toBeGreaterThan(1);
  for (const d of data) {
    expect(d.desc, `description for ${d.id}`).toBeTruthy();
    expect(d.inRow).toBe(true);
    expect(d.desc.length).toBeLessThanOrEqual(400);
    expect(d.label).not.toContain(d.desc);
  }
  expect(new Set(data.map((d) => d.id)).size).toBe(data.length);
  expect(new Set(data.map((d) => d.desc)).size).toBeGreaterThan(1);
});
