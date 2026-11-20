import { test, expect, devices } from "@playwright/test";

// Live checks for #3845 (hover bar stays in its own row), #3843 and #3773
// (the sticky day pill has its own strip, whole below the header).

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, name, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${name}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
}

async function checkHoverBars(page, name, locale) {
  await openChannel(page, name, locale);
  const rows = page.locator(".message-list .message[data-message-id]");
  const count = await rows.count();
  expect(count).toBeGreaterThan(2);
  let checked = 0;
  for (let i = 1; i < Math.min(count, 10); i++) {
    const row = rows.nth(i);
    await row.scrollIntoViewIfNeeded();
    await row.hover();
    const bar = row.locator(".message-actions").first();
    if (!(await bar.isVisible())) continue;
    const prev = await rows.nth(i - 1).boundingBox();
    const own = await row.boundingBox();
    const b = await bar.boundingBox();
    if (!prev || !own || !b) continue;
    // Inside its own row (1px tolerance), and never reaching the row above.
    expect(b.y, `bar ${i} top vs own row`).toBeGreaterThanOrEqual(own.y - 1);
    expect(b.y + b.height, `bar ${i} bottom vs own row`).toBeLessThanOrEqual(
      own.y + own.height + 1,
    );
    expect(b.y, `bar ${i} below previous row`).toBeGreaterThanOrEqual(
      prev.y + prev.height - 1,
    );
    checked++;
  }
  expect(checked).toBeGreaterThan(1);
}

for (const locale of ["en-US", "ar"]) {
  test(`#3845 hover toolbar stays inside its own message row (${locale})`, async ({
    browser,
  }) => {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
    const page = await ctx.newPage();
    await signIn(page);
    await checkHoverBars(page, "general", locale);
    await ctx.close();
  });
}

async function scrollDayChecks(page, label) {
  const list = page.locator(".message-list").first();
  await expect(page.locator(".message-list .day-divider").first()).toBeAttached();
  const info = await page.evaluate(() => {
    const d = document.querySelector(".message-list .day-divider");
    const cs = window.getComputedStyle(d);
    return {
      position: cs.position,
      bg: cs.backgroundColor,
      pe: cs.pointerEvents,
    };
  });
  expect(info.position, label).toBe("sticky");
  expect(info.bg, `${label} strip colour`).not.toMatch(
    /rgba\(0, 0, 0, 0\)|transparent/,
  );
  expect(info.pe, `${label} strip takes the pointer`).not.toBe("none");
  for (let s = 0; s < 6; s++) {
    await list.evaluate((el, step) => {
      el.scrollTop = Math.max(0, (el.scrollHeight - el.clientHeight) * (1 - step / 6));
    }, s);
    await page.waitForTimeout(150);
    const r = await page.evaluate(() => {
      const l = document.querySelector(".message-list");
      const lb = l.getBoundingClientRect();
      const all = [...l.querySelectorAll(".day-divider")];
      const stuck = all
        .map((d) => ({ d, b: d.getBoundingClientRect() }))
        .filter((x) => x.b.bottom > lb.top && x.b.top < lb.bottom)
        .sort((a, b) => a.b.top - b.b.top)[0];
      if (!stuck) return null;
      const pill = stuck.d.querySelector("span") || stuck.d;
      const pb = pill.getBoundingClientRect();
      const top = document.elementFromPoint(pb.x + pb.width / 2, pb.y + pb.height / 2);
      const sb = stuck.b;
      const covered = [];
      for (const fx of [0.05, 0.25, 0.5, 0.75, 0.95]) {
        const el = document.elementFromPoint(
          sb.x + sb.width * fx,
          sb.y + sb.height / 2,
        );
        if (el && !stuck.d.contains(el) && el.closest(".message"))
          covered.push(el.className);
      }
      const hdr = document.querySelector(
        ".chat-workspace .conversation-header, .chat-workspace .chat-header",
      );
      const hb = hdr ? hdr.getBoundingClientRect() : null;
      return {
        pillTop: pb.top,
        headerBottom: hb ? hb.bottom : null,
        pillOnTop: stuck.d.contains(top),
        coveredByMessage: covered,
      };
    });
    if (!r) continue;
    expect(r.pillOnTop, `${label} step ${s}: pill is not under a message`).toBe(true);
    expect(r.coveredByMessage, `${label} step ${s}: no message on the strip`).toEqual(
      [],
    );
    if (r.headerBottom != null)
      expect(r.pillTop, `${label} step ${s}: pill below header`).toBeGreaterThanOrEqual(
        r.headerBottom - 0.5,
      );
  }
}

for (const [label, viewport, locale, scheme] of [
  ["1440 light", { width: 1440, height: 900 }, "en-US", "light"],
  ["1440 dark", { width: 1440, height: 900 }, "en-US", "dark"],
  ["1440 Arabic", { width: 1440, height: 900 }, "ar", "light"],
  ["390 phone", { width: 390, height: 844 }, "en-US", "light"],
]) {
  test(`#3843 #3773 day pill has an opaque strip, whole below header (${label})`, async ({
    browser,
  }) => {
    const phone = viewport.width < 500;
    const ctx = await browser.newContext({
      viewport,
      colorScheme: scheme,
      ...(phone
        ? { hasTouch: true, isMobile: true, userAgent: devices["Pixel 7"].userAgent }
        : {}),
    });
    const page = await ctx.newPage();
    await signIn(page);
    await openChannel(page, "general", locale);
    await scrollDayChecks(page, label);
    await ctx.close();
  });
}
