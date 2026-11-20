import { test, expect } from "@playwright/test";

// Timeline, lane A chunk 1: #3987 Jump to newest after a pinned jump,
// #3871 sticky day pills never stack, #3847 grouped follow-ups sit close.
// Reads seeded #engineering; everything that writes happens in a zz- channel.

test.setTimeout(240000);

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: `Continue as ${persona}` })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, name, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${name}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 80000,
  });
}

const shortcutState = (page) =>
  page.evaluate(() => {
    const j = document.querySelector(".jump-newest");
    const f = document.querySelector(".timeline-frame");

    if (!j) return { present: false };
    const cs = window.getComputedStyle(j);
    const b = j.getBoundingClientRect();
    const fb = { top: 0, bottom: window.innerHeight };
    return {
      present: true,
      away: !!f && f.classList.contains("away"),
      visible:
        cs.visibility === "visible" &&
        cs.display !== "none" &&
        Number(cs.opacity) > 0.5 &&
        cs.pointerEvents !== "none" &&
        b.width > 0 &&
        b.height > 0 &&
        b.bottom > fb.top &&
        b.top < fb.bottom,
      label: j.getAttribute("aria-label"),
      targets: document.querySelectorAll(".message.search-target").length,
    };
  });

const scrollList = (page, where) =>
  page
    .locator(".message-list")
    .first()
    .evaluate((el, w) => {
      el.scrollTop = w === "top" ? 0 : el.scrollHeight;
    }, where);

async function pinCycle(page, label) {
  await openChannel(page, "engineering");
  const headerPins = page.locator("[data-action='header-pinned']");
  if (await headerPins.isVisible()) await headerPins.click();
  else await page.locator("button[data-action='details']").first().click();
  const pin = page.locator("[data-action='pin-jump']").first();
  await expect(pin).toBeVisible({ timeout: 20000 });
  await pin.click();
  // The destination is highlighted when the reader lands on it.
  await expect(page.locator(".message.search-target").first()).toBeAttached({
    timeout: 20000,
  });

  for (let cycle = 1; cycle <= 2; cycle++) {
    // Leave the newest position: scroll into history.
    await scrollList(page, "top");
    await page.locator(".message-list").first().hover();
    await page.mouse.wheel(0, -400);
    await expect
      .poll(async () => (await shortcutState(page)).away, {
        message: `${label} cycle ${cycle}: timeline is away from newest`,
        timeout: 15000,
      })
      .toBe(true);
    await expect
      .poll(async () => (await shortcutState(page)).visible, {
        message: `${label} cycle ${cycle}: Jump to newest is visible in history`,
        timeout: 10000,
      })
      .toBe(true);
    const s = await shortcutState(page);
    expect(s.label, `${label} accessible name`).toMatch(/newest|latest/i);

    // Operate it: by keyboard on the second cycle, by click on the first.
    const jump = page.locator("button.jump-newest").first();
    if (cycle === 2) {
      await jump.focus();
      await page.keyboard.press("Enter");
    } else {
      await jump.click();
    }
    await expect
      .poll(
        () =>
          page
            .locator(".message-list")
            .first()
            .evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop),
        { message: `${label} cycle ${cycle}: reached newest`, timeout: 15000 },
      )
      .toBeLessThan(4);
    // The old pin highlight is not carried back to the newest messages.
    await expect(page.locator(".message.search-target")).toHaveCount(0, {
      timeout: 8000,
    });
  }
}

test("#3987 Jump to newest stays available after pin -> newest -> history (desktop)", async ({
  browser,
}) => {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await signIn(page);
  await pinCycle(page, "1440");
  await ctx.close();
});

test("#3987 Jump to newest stays available after pin -> newest -> history (390 phone)", async ({
  browser,
}) => {
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  const page = await ctx.newPage();
  await signIn(page);
  await pinCycle(page, "390");
  const s = await shortcutState(page);
  expect(s.present).toBe(true);
  await ctx.close();
});

async function dayPillChecks(page, label) {
  const groups = await page.locator(".message-list .day-group").count();
  expect(groups, `${label}: days are wrapped in groups`).toBeGreaterThan(2);
  const orphan = await page.evaluate(
    () =>
      [...document.querySelectorAll(".message-list .day-divider")].filter(
        (d) => !d.parentElement.classList.contains("day-group"),
      ).length,
  );
  expect(orphan, `${label}: every pill lives in its own day group`).toBe(0);
  const list = page.locator(".message-list").first();
  for (let step = 0; step <= 8; step++) {
    await list.evaluate((el, s) => {
      el.scrollTop = ((el.scrollHeight - el.clientHeight) * s) / 8;
    }, step);
    await page.waitForTimeout(200);
    const r = await page.evaluate(() => {
      const l = document.querySelector(".message-list").getBoundingClientRect();
      const pills = [...document.querySelectorAll(".message-list .day-divider")]
        .map((d) => ({ t: d.innerText.trim(), b: d.getBoundingClientRect() }))
        .filter((x) => x.b.bottom > l.top && x.b.top < l.bottom);
      const overlaps = [];
      for (let i = 0; i < pills.length; i++)
        for (let j = i + 1; j < pills.length; j++) {
          const a = pills[i].b;
          const b = pills[j].b;
          const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
          const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
          if (w > 1 && h > 1) overlaps.push(`${pills[i].t} / ${pills[j].t}`);
        }
      return overlaps;
    });
    expect(r, `${label} scroll step ${step}: no two day pills overlap`).toEqual([]);
  }
}

for (const [label, locale, scheme, viewport] of [
  ["en-US dark 1440", "en-US", "dark", { width: 1440, height: 900 }],
  ["ar 1440", "ar", "light", { width: 1440, height: 900 }],
  ["en-US 390", "en-US", "light", { width: 390, height: 844 }],
]) {
  test(`#3871 only one sticky day pill at a time (${label})`, async ({ browser }) => {
    const ctx = await browser.newContext({
      viewport,
      colorScheme: scheme,
      hasTouch: viewport.width < 500,
      isMobile: viewport.width < 500,
    });
    const page = await ctx.newPage();
    await signIn(page);
    await openChannel(page, "engineering", locale);
    await dayPillChecks(page, label);
    await ctx.close();
  });
}

test("#3871 day pills still do not stack after opening a pinned message", async ({
  browser,
}) => {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await signIn(page);
  await openChannel(page, "engineering");
  await page.locator("[data-action='header-pinned']").click();
  await page.locator("[data-action='pin-jump']").first().click();
  await expect(page.locator(".message.search-target").first()).toBeAttached({
    timeout: 20000,
  });
  await dayPillChecks(page, "after pin jump");
  await ctx.close();
});

test("#3847 a grouped follow-up sits a few pixels under the first message", async ({
  browser,
}) => {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await signIn(page);
  const name = `zz-timeline-a-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  const input = page.locator("textarea.composer-input").first();
  await input.waitFor({ timeout: 40000 });
  const marker = Date.now();
  const rows = [];
  for (const word of ["one", "two", "three"]) {
    const text = `zz-3847 grp ${word} ${marker}`;
    await input.fill(text);
    await input.press("Enter");
    const row = page.locator(".message[data-message-id]", { hasText: text }).first();
    await expect(row).toBeVisible({ timeout: 30000 });
    rows.push(row);
  }
  await expect(rows[1]).toHaveClass(/continued/);
  const m = async (row) =>
    row.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const body = el.querySelector(".message-body, .message-text, p");
      const br = (body || el).getBoundingClientRect();
      const meta = el.querySelector(".message-meta");
      return {
        top: r.top,
        bottom: r.bottom,
        bodyTop: br.top,
        bodyBottom: br.bottom,
        metaHeight: meta ? meta.getBoundingClientRect().height : 0,
      };
    });
  const a = await m(rows[0]);
  const b = await m(rows[1]);
  const c = await m(rows[2]);
  // No empty author band; the follow-up's text starts a few px below the first's.
  expect(b.metaHeight, "no empty author band in the follow-up").toBeLessThanOrEqual(8);
  expect(b.bodyTop - a.bodyBottom, "gap between text of 1 and 2").toBeLessThanOrEqual(
    12,
  );
  expect(c.bodyTop - b.bodyBottom, "gap between text of 2 and 3").toBeLessThanOrEqual(
    12,
  );
  expect(
    b.bodyTop - b.top,
    "follow-up text starts near its row top",
  ).toBeLessThanOrEqual(10);
  await ctx.close();
});
