import { test, expect, devices } from "@playwright/test";

// Live checks for #3845 (hover bar stays inside its own row), #3843 and #3773
// (the sticky day pill has its own strip, whole below the header).
//
// The message list is virtualised and long, and it re-anchors itself after a
// programmatic scroll, so every measurement below is taken in one synchronous
// page.evaluate right after the list is seen at its first day.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Darius Bennett/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, name, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${name}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await page.waitForTimeout(2500);
}

// Climb to the first day of the channel (where the day dividers are rendered)
// and run `measure` (a function body string) there, in one evaluate.
async function atFirstDay(page, measure) {
  for (let i = 0; i < 80; i++) {
    const result = await page.evaluate(`(() => {
      const l = document.querySelector(".message-list");
      if (!l.querySelector(".day-divider")) { l.scrollTop = 0; return { found: false }; }
      return { found: true, value: (${measure})(l) };
    })()`);
    if (result.found) return result.value;
    await page.waitForTimeout(600);
  }
  throw new Error("the first day divider never rendered");
}

const hoverMeasure = `(l) => {
  const lb = l.getBoundingClientRect();
  const rows = [...l.querySelectorAll(".message[data-message-id]")]
    .map((el) => ({ id: el.dataset.messageId, b: el.getBoundingClientRect() }))
    .filter((r) => r.b.bottom > lb.top + 20 && r.b.top < lb.bottom - 20);
  return rows.map((r) => ({ id: r.id, x: r.b.x + r.b.width / 2, y: r.b.y + Math.min(r.b.height / 2, 30) }));
}`;

async function checkHoverBars(page, name, locale) {
  await openChannel(page, name, locale);
  // The list opens at its newest messages and stays put there: hover those.
  const targets = await page.evaluate(
    `(${hoverMeasure})(document.querySelector('.message-list'))`,
  );
  expect(targets.length).toBeGreaterThan(2);
  let checked = 0;
  const skipped = [];
  for (const t of targets.slice(1)) {
    await page.mouse.move(t.x, t.y);
    await page
      .waitForFunction(
        ([x, y]) =>
          document
            .elementFromPoint(x, y)
            ?.closest(".message[data-message-id]")
            ?.querySelector(".message-actions"),
        [t.x, t.y],
        { timeout: 2500 },
      )
      .catch(() => {});
    const m = await page.evaluate(
      ([x, y]) => {
        const row = document
          .elementFromPoint(x, y)
          ?.closest(".message[data-message-id]");
        if (!row) return { why: "no row under the pointer" };
        const bar = row.querySelector(".message-actions");
        if (!bar) return { why: "row has no bar" };
        const cs = window.getComputedStyle(bar);
        const b = bar.getBoundingClientRect();
        if (b.width === 0 || cs.visibility === "hidden" || cs.opacity === "0")
          return { why: `bar hidden ${cs.visibility} ${cs.opacity} ${b.width}` };
        const all = [
          ...document.querySelectorAll(".message-list .message[data-message-id]"),
        ];
        const prev = all[all.indexOf(row) - 1];
        const own = row.getBoundingClientRect();
        const p = prev && prev.getBoundingClientRect();
        return {
          shown: true,
          barTop: b.top,
          barBottom: b.bottom,
          ownTop: own.top,
          ownBottom: own.bottom,
          prevBottom: p ? p.bottom : null,
        };
      },
      [t.x, t.y],
    );
    if (!m.shown) {
      skipped.push(`${t.id}: ${m.why}`);
      continue;
    }
    expect(m.barTop, `bar top vs own row (${t.id})`).toBeGreaterThanOrEqual(
      m.ownTop - 1,
    );
    expect(m.barBottom, `bar bottom vs own row (${t.id})`).toBeLessThanOrEqual(
      m.ownBottom + 1,
    );
    if (m.prevBottom != null && m.prevBottom <= m.ownTop + 1)
      expect(m.barTop, `bar below previous row (${t.id})`).toBeGreaterThanOrEqual(
        m.prevBottom - 1,
      );
    checked++;
  }
  expect(
    checked,
    `hover bars measured (skipped: ${skipped.join("; ")})`,
  ).toBeGreaterThan(1);
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

const dayMeasure = `(l) => {
  const out = [];
  const d0 = l.querySelector(".day-divider");
  const cs = window.getComputedStyle(d0);
  const style = { position: cs.position, bg: cs.backgroundColor, pe: cs.pointerEvents };
  for (const off of [0, 12, 30, 60, 100]) {
    const d = l.querySelector(".day-divider");
    // scroll so the divider's own row starts off px above the list's top edge: its natural
    // place is above the edge, so the strip has to be holding it there.
    l.scrollTop += d.parentElement.getBoundingClientRect().top - l.getBoundingClientRect().top - off;
    const lb = l.getBoundingClientRect();
    const stuck = [...l.querySelectorAll(".day-divider")]
      .map((x) => ({ x, b: x.getBoundingClientRect() }))
      .filter((s) => s.b.bottom > lb.top && s.b.top < lb.bottom)
      .sort((a, b) => a.b.top - b.b.top)[0];
    if (!stuck) { out.push({ off, none: true }); continue; }
    const pill = stuck.x.querySelector("span") || stuck.x;
    const pb = pill.getBoundingClientRect();
    const top = document.elementFromPoint(pb.x + pb.width / 2, pb.y + pb.height / 2);
    const sb = stuck.b;
    const covered = [];
    for (const fx of [0.05, 0.25, 0.5, 0.75, 0.95]) {
      const el = document.elementFromPoint(sb.x + sb.width * fx, sb.y + sb.height / 2);
      if (el && !stuck.x.contains(el) && el.closest(".message")) covered.push(el.className);
    }
    const hdr = document.querySelector(".chat-workspace .conversation-header");
    out.push({
      off,
      pillTop: pb.top,
      listTop: lb.top,
      headerBottom: hdr ? hdr.getBoundingClientRect().bottom : null,
      pillOnTop: stuck.x.contains(top),
      covered,
    });
  }
  return { style, out };
}`;

async function dayChecks(page, label) {
  const { style, out } = await atFirstDay(page, dayMeasure);
  expect(style.position, label).toBe("sticky");
  expect(style.bg, `${label} strip colour`).not.toMatch(
    /rgba\(0, 0, 0, 0\)|transparent/,
  );
  expect(style.pe, `${label} strip takes the pointer`).not.toBe("none");
  for (const r of out) {
    expect(r.none, `${label} offset ${r.off}: divider in view`).toBeUndefined();
    expect(r.pillOnTop, `${label} offset ${r.off}: pill not under a message`).toBe(
      true,
    );
    expect(r.covered, `${label} offset ${r.off}: no message on the strip`).toEqual([]);
    expect(
      r.pillTop - r.listTop,
      `${label} offset ${r.off}: pill sits 16px below the list top`,
    ).toBeGreaterThanOrEqual(15);
    if (r.headerBottom != null)
      expect(
        r.pillTop,
        `${label} offset ${r.off}: pill below header`,
      ).toBeGreaterThanOrEqual(r.headerBottom - 0.5);
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
    await dayChecks(page, label);
    await ctx.close();
  });
}
