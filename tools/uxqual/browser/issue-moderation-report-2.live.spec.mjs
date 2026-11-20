import { test, expect, devices } from "@playwright/test";

// Live checks for the Moderation page: #3856 (empty and explanatory text has
// the content inset), #3855 (context shows the message's own timeline) and
// #3854 (permission matrix readable at phone width). Nothing is changed.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openModeration(page, locale, isPhone) {
  await signIn(page);
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=general`);
  if (isPhone) {
    const rail = page.locator("[data-action=open-rail]").first();
    await rail.waitFor({ timeout: 90000 });
    await rail.tap();
  } else {
    await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
  }
  const mod = page.locator("[data-chatremove-open*='moderation']").first();
  await mod.waitFor({ timeout: 60000 });
  await (isPhone ? mod.tap() : mod.click());
  await page.locator(".chatmod005-tabs").waitFor({ timeout: 60000 });
}

async function tabByIndex(page, index, isPhone) {
  const tab = page.locator(".chatmod005-tabs .chatmod005-tab").nth(index);
  await expect(tab).toBeEnabled({ timeout: 90000 });
  await (isPhone ? tab.tap() : tab.click());
}

const phone = {
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
};

for (const [name, opts, isPhone] of [
  ["desktop", { viewport: { width: 1440, height: 900 } }, false],
  ["phone 354", { viewport: { width: 354, height: 767 }, ...phone }, true],
]) {
  test.describe(`#3856 ${name}`, () => {
    test.use(opts);
    for (const locale of ["en-US", "ar"]) {
      test(`#3856 no-results and Permissions text share the inset of the search field (${name} ${locale})`, async ({
        page,
      }) => {
        await openModeration(page, locale, isPhone);
        // Second tab is Resolved, third is Permissions.
        await tabByIndex(page, 1, isPhone);
        const search = page.locator("#chatremove-search");
        await search.waitFor({ timeout: 60000 });
        await search.fill("zzqxj-no-such-thing");
        const empty = page
          .locator(".chatmod005-nomatch:visible, .chatmod005-empty:visible")
          .first();
        await expect(empty).toBeVisible({ timeout: 15000 });
        const field = await page.locator(".chatmod005-search").boundingBox();
        const text = await empty.boundingBox();
        const rtl = locale === "ar";
        const page0 = await page.locator("#chatremove-moderation").boundingBox();
        const edge = (b) =>
          rtl ? page0.x + page0.width - (b.x + b.width) : b.x - page0.x;
        const inset = edge(text);
        const fieldInset = edge(field);
        expect(inset).toBeGreaterThanOrEqual(12);
        expect(Math.abs(inset - fieldInset)).toBeLessThanOrEqual(2);

        await tabByIndex(page, 2, isPhone);
        await page
          .locator(".chatmod005-perm-scroll")
          .first()
          .waitFor({ timeout: 60000 });
        const tabs = await page.locator(".chatmod005-tabs").boundingBox();
        const page1 = await page.locator("#chatremove-moderation").boundingBox();
        const edge1 = (b) =>
          rtl ? page1.x + page1.width - (b.x + b.width) : b.x - page1.x;
        const paras = page.locator("#chatremove-moderation > p.chatmod005-muted");
        const count = await paras.count();
        expect(count).toBeGreaterThan(0);
        for (let i = 0; i < count; i++) {
          const b = await paras.nth(i).boundingBox();
          const pi = edge1(b);
          const ti = edge1(tabs);
          expect(pi).toBeGreaterThanOrEqual(12);
          expect(Math.abs(pi - ti)).toBeLessThanOrEqual(2);
        }
      });
    }
  });
}

test.describe("#3855", () => {
  test.use({ viewport: { width: 1440, height: 900 } });
  test("#3855 moderation context only lists messages from the moderated message's own timeline", async ({
    page,
  }) => {
    await openModeration(page, "en-US", false);
    await tabByIndex(page, 1, false);
    await page.locator(".chatmod005-item").first().waitFor({ timeout: 60000 });
    const items = page.locator(".chatmod005-item:has(.chatmod005-context)");
    const n = await items.count();
    expect(n).toBeGreaterThan(0);
    const contexts = [];
    for (let i = 0; i < n; i++) {
      const item = items.nth(i);
      const channel =
        (await item.locator(".chatmod005-channel").first().textContent()) || "";
      if (channel.trim() !== "#general") continue;
      await item.locator(".chatmod005-context > summary").click();
      const bodies = await item
        .locator(".chatmod005-context .message-body")
        .allTextContents();
      contexts.push(bodies.map((b) => b.trim()).filter(Boolean));
    }
    expect(contexts.length).toBeGreaterThan(0);
    // In the seeded #general these sentences only ever appear as thread
    // replies (cmd/migrate/chat_seed_corpus.go threadReplies); a context made of
    // the channel's own timeline must not contain them.
    const replies = [
      "Agreed. I will take the first part.",
      "One correction: the date is the 16th, not the 14th.",
      "Can you say more about why we are holding the band rather than re-pointing it?",
      "This matches what I heard in the regional review.",
      "I will own the write-up and have a draft by Friday.",
      "Small risk: that timing collides with open enrollment comms.",
      "Done — link is in the thread above.",
      "Not sure I follow. Is this per region or company-wide?",
      "Company-wide. Regions can go earlier, not later.",
      "Thanks, that answers it.",
    ];
    const total = contexts.flat().length;
    expect(total).toBeGreaterThan(0);
    for (const bodies of contexts) {
      for (const body of bodies) {
        const hit = replies.find((r) => body.includes(r));
        expect(
          hit,
          `thread reply in the channel context: ${body.slice(0, 80)}`,
        ).toBeUndefined();
      }
    }
  });
});

test.describe("#3854", () => {
  for (const [w, h] of [
    [354, 767],
    [320, 640],
  ]) {
    test.describe(`phone ${w}`, () => {
      test.use({ viewport: { width: w, height: h }, ...phone });
      test(`#3854 permission matrix is readable role cards at ${w}px`, async ({
        page,
      }) => {
        await openModeration(page, "en-US", true);
        await tabByIndex(page, 2, true);
        const table = page.locator(".chatmod005-perm-table");
        await table.waitFor({ timeout: 60000 });
        const box = await table.boundingBox();
        expect(box.x).toBeGreaterThanOrEqual(-1);
        expect(box.x + box.width).toBeLessThanOrEqual(w + 1);
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= window.innerWidth + 1,
          ),
        ).toBe(true);
        const broken = await table.evaluate((t) => {
          const bad = [];
          for (const el of t.querySelectorAll("th, td, label, span")) {
            const s = window.getComputedStyle(el);
            if (
              /anywhere|break-word/.test(s.overflowWrap) ||
              s.wordBreak === "break-all"
            )
              bad.push(el.className || el.tagName);
          }
          return bad;
        });
        expect(broken).toEqual([]);
        const cells = table.locator("tbody td");
        const count = await cells.count();
        expect(count).toBeGreaterThan(0);
        for (let i = 0; i < count; i++) {
          const label = await cells
            .nth(i)
            .evaluate((c) => window.getComputedStyle(c, "::before").content);
          expect(label.replace(/["']/g, "").trim().length).toBeGreaterThan(2);
          const cb = await cells.nth(i).boundingBox();
          expect(cb.width).toBeGreaterThan(w * 0.6);
        }
        const th = await table.locator("tbody th").first().boundingBox();
        expect(th.height).toBeLessThan(120);
        const lines = await table
          .locator("tbody th")
          .first()
          .evaluate((el) => {
            const r = document.createRange();
            r.selectNodeContents(el);
            return new Set([...r.getClientRects()].map((q) => Math.round(q.top / 4)))
              .size;
          });
        expect(lines).toBeLessThanOrEqual(3);
      });
    });
  }
  test.describe("desktop", () => {
    test.use({ viewport: { width: 1440, height: 900 } });
    test("#3854 desktop keeps the table and no mid-word breaks", async ({ page }) => {
      await openModeration(page, "en-US", false);
      await tabByIndex(page, 2, false);
      const table = page.locator(".chatmod005-perm-table");
      await table.waitFor({ timeout: 60000 });
      expect(await table.locator("thead th").count()).toBeGreaterThanOrEqual(5);
      const th = await table.locator("tbody th").first().boundingBox();
      expect(th.height).toBeLessThan(100);
    });
  });
});
