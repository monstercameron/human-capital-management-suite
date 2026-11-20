/* global console, getComputedStyle, MutationObserver */
import { test, expect, devices } from "@playwright/test";

// Phone lane, chunk 2: #3680 loading gutter, #3761 title/bell overlap,
// #3868 whole link address on a phone. Runs against the dev cell
// (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.use({
  viewport: { width: 375, height: 812 },
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
});
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("#3680 the loading list keeps a 16px side gutter", async ({ page }) => {
  await signIn(page);
  // Record the first geometry of the list's search box and skeleton rows the
  // moment they are drawn, before the real list replaces them.
  await page.addInitScript(() => {
    window.__first = {};
    const probe = () => {
      for (const sel of [".chat-rail .rail-search", ".chat-rail .rail-skeleton"]) {
        const el = document.querySelector(sel);
        if (el && !window.__first[sel]) {
          const r = el.getBoundingClientRect();
          window.__first[sel] = {
            left: r.left,
            right: window.innerWidth - r.right,
            padStart: parseFloat(getComputedStyle(el).paddingInlineStart),
            padEnd: parseFloat(getComputedStyle(el).paddingInlineEnd),
          };
        }
      }
    };
    new MutationObserver(probe).observe(document, { childList: true, subtree: true });
  });
  await page.goto("/workspace/app/chat?locale=en-US");
  await expect(page.locator(".chat-rail .rail-search").first()).toBeVisible({
    timeout: 60000,
  });
  const first = await page.evaluate(() => window.__first);
  console.log("first-seen geometry", JSON.stringify(first));
  const search = first[".chat-rail .rail-search"];
  expect(search, "search box was drawn").toBeTruthy();
  expect(search.padStart).toBeGreaterThanOrEqual(16);
  expect(search.padEnd).toBeGreaterThanOrEqual(16);
  if (first[".chat-rail .rail-skeleton"]) {
    expect(first[".chat-rail .rail-skeleton"].padStart).toBeGreaterThanOrEqual(16);
  }
  // Loaded list, no conversation chosen: same gutter and no slide offset.
  await page.waitForTimeout(1500);
  const loaded = await page.evaluate(() => {
    const rail = document.querySelector(".chat-rail");
    const s = document.querySelector(".chat-rail .rail-search");
    const input = s.querySelector("input");
    const ir = input.getBoundingClientRect();
    return {
      railLeft: rail.getBoundingClientRect().left,
      inputLeft: ir.left,
      inputRight: window.innerWidth - ir.right,
    };
  });
  console.log("loaded geometry", JSON.stringify(loaded));
  expect(Math.round(loaded.railLeft)).toBe(0);
  expect(loaded.inputLeft).toBeGreaterThanOrEqual(15.5);
  expect(loaded.inputRight).toBeGreaterThanOrEqual(15.5);
});

async function titleState(page) {
  return page.evaluate(() => {
    const title = document.querySelector(".chat-rail .rail-title");
    const range = document.createRange();
    range.selectNodeContents(title);
    const rects = [...range.getClientRects()];
    const tops = new Set(rects.map((r) => Math.round(r.top)));
    const tr = title.getBoundingClientRect();
    const hit = [...document.querySelectorAll("button, a, [role=button]")]
      .filter((b) => !title.contains(b) && b.offsetParent !== null)
      .map((b) => ({
        n: (b.getAttribute("aria-label") || b.textContent || "").trim().slice(0, 20),
        r: b.getBoundingClientRect(),
      }))
      .filter(
        ({ r }) =>
          r.width > 0 &&
          r.left < tr.right &&
          r.right > tr.left &&
          r.top < tr.bottom &&
          r.bottom > tr.top,
      )
      .map(({ n }) => n);
    return { text: title.textContent, lines: tops.size, overlaps: hit };
  });
}

for (const locale of ["en-US", "de-DE"]) {
  test(`#3761 list title is whole at 320px (${locale})`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await signIn(page);
    await page.goto(`/workspace/app/chat?locale=${locale}`);
    await expect(page.locator(".chat-rail .rail-title").first()).toBeVisible({
      timeout: 60000,
    });
    await page.waitForTimeout(1500);
    const s = await titleState(page);
    console.log(locale, JSON.stringify(s));
    expect(s.lines, "title on one line (never broken inside the word)").toBe(1);
    expect(s.overlaps, "nothing sits over the title").toEqual([]);
  });
}

test("#3761 search results heading is clear of the bell at 375px", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await page.locator("[data-action='open-rail']").first().tap();
  const box = page.locator(".chat-rail .rail-search input").first();
  await expect(box).toBeVisible({ timeout: 60000 });
  await box.fill("deploy");
  await box.press("Enter");
  const head = page.locator(".search-results-head").first();
  await expect(head).toBeVisible({ timeout: 30000 });
  await page.waitForTimeout(1000);
  const probe = () =>
    page.evaluate(() => {
      const h = document.querySelector(".search-results-head");
      const h2 = h.querySelector("h2");
      const r = h2.getBoundingClientRect();
      const y = r.top + r.height / 2;
      // What is on top at points along the visible heading and at its far end.
      const xs = [r.left + 2, r.left + r.width / 2, r.right - 2];
      const tops = xs.map((x) => {
        const el = document.elementFromPoint(x, y);
        return {
          x,
          inHead: !!el && h.contains(el),
          what: el ? el.tagName + "." + el.className : null,
        };
      });
      const bell = [...document.querySelectorAll("button, a, [role=button]")].find(
        (b) => /^Notifications/.test(b.getAttribute("aria-label") || ""),
      );
      const br = bell ? bell.getBoundingClientRect() : null;
      return {
        text: h2.textContent,
        left: r.left,
        right: r.right,
        bellLeft: br && br.left,
        tops,
      };
    });
  // The panel slides in over the list; give the transition time to settle.
  let res = await probe();
  for (let i = 0; i < 20 && !res.tops.every((t) => t.inHead); i++) {
    await page.waitForTimeout(500);
    res = await probe();
  }
  await page.screenshot({ path: ".artifacts/tmp/phone2-results.png" });
  console.log("results head", JSON.stringify(res));
  for (const t of res.tops)
    expect(t.inHead, `something covers the heading at x=${t.x}: ${t.what}`).toBe(true);
  if (res.bellLeft != null) expect(res.right).toBeLessThanOrEqual(res.bellLeft);
});

test.describe("#3868", () => {
  test.use({
    isMobile: false,
    hasTouch: false,
    viewport: { width: 1440, height: 900 },
  });
  test("#3868 a phone shows the whole address of a message link", async ({ page }) => {
    await signIn(page);
    // Own channel, made at desktop width, so no seeded conversation is written to.
    await page.setViewportSize({ width: 1440, height: 900 });
    const name = `zz-phone-${Math.random().toString(36).slice(2, 7)}`;
    const composerSel = "textarea[name='message']";
    await page.goto("/workspace/app/chat?locale=en-US#channel=general");
    await expect(page.locator(composerSel)).toBeVisible({ timeout: 60000 });
    await page
      .getByRole("button", { name: /New conversation/i })
      .first()
      .click();
    const dialog = page.locator(".create-dialog");
    await dialog.getByRole("radio", { name: /Public channel/ }).click();
    await dialog.getByRole("textbox").first().fill(name);
    await dialog.locator("button[type=submit]").click();
    await expect(page.locator(composerSel)).toHaveAttribute(
      "placeholder",
      new RegExp(name),
      {
        timeout: 30000,
      },
    );
    const url = `http://gift-rewards.example/claim/${name}/prize-details-page`;
    await page.locator(composerSel).fill(`please check ${url} today`);
    await page.getByRole("button", { name: /^Send/ }).first().click();
    await expect(
      page.locator(".message-body", { hasText: "prize-details-page" }).first(),
    ).toBeVisible({
      timeout: 30000,
    });
    console.log(
      "posted",
      await page.evaluate(() =>
        [...document.querySelectorAll(".message-body")].map((m) =>
          m.innerHTML.slice(0, 400),
        ),
      ),
    );

    await page.setViewportSize({ width: 291, height: 636 });
    const link = page.locator("a.chat-link[href*='gift-rewards.example']").first();
    await expect(link).toBeVisible({ timeout: 60000 });
    await link.scrollIntoViewIfNeeded();
    const g = await link.evaluate((a) => {
      const cs = getComputedStyle(a);
      const msg = a.closest(".message-body") || a.closest(".message");
      const rects = [...a.getClientRects()];
      return {
        text: a.textContent,
        textOverflow: cs.textOverflow,
        whiteSpace: cs.whiteSpace,
        maxRight: Math.max(...rects.map((r) => r.right)),
        msgRight: msg.getBoundingClientRect().right,
        vw: window.innerWidth,
      };
    });
    console.log("link", JSON.stringify(g));
    expect(g.text).toContain("gift-rewards.example/claim");
    expect(g.text).toContain("prize-details-page");
    expect(g.text).not.toContain("…");
    expect(g.whiteSpace).not.toBe("nowrap");
    expect(g.textOverflow).not.toBe("ellipsis");
    expect(g.maxRight).toBeLessThanOrEqual(g.msgRight + 1);
    expect(g.maxRight).toBeLessThanOrEqual(g.vw);
  });
});
