import { test, expect, devices } from "@playwright/test";

// Chat issue sweep, lane search-visual chunk 2: #61 (phone search results),
// #59 (dark search result contrast) and #57 (dark chat settings panels).
// Runs against the live dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080). Read only:
// nothing is posted or changed.

test.setTimeout(240000);

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

// Contrast helpers run in the page: resolve the first non-transparent
// background up the ancestor chain and compute the WCAG ratio.
const contrastProbe = `(el) => {
  const parse = (c) => {
    const m = c.match(/rgba?\\(([^)]+)\\)/);
    if (!m) return null;
    const p = m[1].split(/[ ,\\/]+/).filter(Boolean).map(Number);
    return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
  };
  const lum = ({ r, g, b }) => {
    const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); };
    return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
  };
  let bg = null;
  for (let n = el; n; n = n.parentElement) {
    const c = parse(getComputedStyle(n).backgroundColor);
    if (c && c.a > 0.9) { bg = c; break; }
  }
  bg = bg || { r: 255, g: 255, b: 255 };
  const fg = parse(getComputedStyle(el).color);
  const l1 = lum(fg), l2 = lum(bg);
  const ratio = (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
  return { ratio, fg: getComputedStyle(el).color, bg: \`rgb(\${bg.r}, \${bg.g}, \${bg.b})\`, bgLum: l2 };
}`;

for (const width of [390, 320]) {
  test.describe(`phone ${width}`, () => {
    test.use({
      viewport: { width, height: 740 },
      hasTouch: true,
      isMobile: true,
      userAgent: devices["Pixel 7"].userAgent,
    });

    test(`#61 phone search results are painted, exposed, hit-testable and open the tapped message (${width}px)`, async ({
      page,
    }) => {
      await signInAsRafael(page);
      await page.goto("/workspace/app/chat?locale=en-US");
      await expect(page.locator(".chat-workspace")).toBeVisible({ timeout: 90000 });
      await expect(page.locator(".chat-row").first()).toBeAttached({ timeout: 90000 });
      // Wait for the Go/WASM client to attach its handlers: text typed before
      // that is wiped by the first render. Retry until the query sticks.
      await page.waitForTimeout(30000); // WASM hydration settle
      const input = page.locator(".chat-rail #chat-search");
      await expect
        .poll(
          async () => {
            if (page.url().includes("#search=meeting")) return true;
            if (!(await input.isVisible())) {
              await page.getByRole("button", { name: "Open conversations" }).tap();
            }
            await input.tap();
            await page.keyboard.type("meeting", { delay: 60 });
            await page.keyboard.press("Enter");
            await page.waitForTimeout(3000);
            return page.url().includes("#search=meeting");
          },
          { timeout: 120000, intervals: [3000] },
        )
        .toBe(true);

      const results = page.getByRole("region", { name: "Results for “meeting”" });
      await expect(results).toBeVisible({ timeout: 90000 });
      const box = await results.boundingBox();
      expect(box.width).toBeGreaterThan(100);
      expect(box.height).toBeGreaterThan(100);

      const first = results.getByRole("button", { name: /^Open result/ }).first();
      await expect(first).toBeVisible({ timeout: 90000 });
      const label = await first.getAttribute("aria-label");
      const channelMatch = label.match(/in #?([\w-]+)/);

      // Hit-testing: the centre of the first result lands on the result, not on
      // the conversation list underneath.
      const rb = await first.boundingBox();
      const hit = await page.evaluate(
        ({ x, y }) => {
          const el = document.elementFromPoint(x, y);
          return !!el && !!el.closest(".chat-search-results");
        },
        { x: rb.x + rb.width / 2, y: rb.y + Math.min(rb.height / 2, 20) },
      );
      expect(hit, "results are on top at the tap point").toBe(true);

      // The rail must not be advertised as a modal dialog that hides the results.
      const modal = await page.evaluate(() => {
        const rail = document.querySelector(".chat-rail");
        return rail ? rail.getAttribute("aria-modal") : null;
      });
      expect(modal).not.toBe("true");

      await first.tap();
      await expect
        .poll(() => page.url(), { timeout: 15000 })
        .not.toMatch(/#search=meeting$/);
      if (channelMatch) {
        expect(page.url()).toContain(channelMatch[1]);
      }
      // The conversation pane is on screen with the opened conversation in it.
      await expect
        .poll(
          () =>
            page.evaluate(() => {
              const main = document.querySelector(".chat-main");
              const r = main && main.getBoundingClientRect();
              const results = document.querySelector(".chat-search-results");
              return !!r && r.width > 100 && r.height > 100 && !results;
            }),
          { timeout: 60000 },
        )
        .toBe(true);
    });

    test(`#61 search launched from a selected conversation shows results (${width}px)`, async ({
      page,
    }) => {
      await signInAsRafael(page);
      await page.goto("/workspace/app/chat?locale=en-US#channel=general");
      await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
        timeout: 90000,
      });
      const trigger = page.getByRole("button", { name: /^Search messages/ }).first();
      await expect(trigger).toBeVisible();
      await trigger.tap();
      const field = page
        .getByRole("searchbox")
        .or(page.locator("#chat-search"))
        .first();
      await expect(field).toBeVisible();
      await field.fill("deadline");
      await field.press("Enter");
      const results = page.getByRole("region", { name: /^Results for/ });
      await expect(results).toBeVisible({ timeout: 90000 });
      const box = await results.boundingBox();
      expect(box.width).toBeGreaterThan(100);
      expect(box.height).toBeGreaterThan(100);
    });
  });
}

test.describe("dark shell", () => {
  test.use({ colorScheme: "dark", viewport: { width: 1280, height: 720 } });

  for (const query of ["meeting", "calibration"]) {
    test(`#59 search result author, date and snippet are readable in dark mode (${query})`, async ({
      page,
    }) => {
      await signInAsRafael(page);
      await page.goto(`/workspace/app/chat?locale=en-US#search=${query}`);
      const results = page.getByRole("region", { name: `Results for “${query}”` });
      await expect(results).toBeVisible({ timeout: 90000 });
      const cards = results.locator(".chatsearch-result");
      await expect(cards.first()).toBeVisible({ timeout: 90000 });
      const n = Math.min(await cards.count(), 6);
      expect(n).toBeGreaterThan(0);
      for (let i = 0; i < n; i++) {
        const card = cards.nth(i);
        for (const sel of [
          ".chatsearch-author",
          ".chatsearch-time",
          ".chatsearch-text",
        ]) {
          const el = card.locator(sel).first();
          const r = await el.evaluate(new Function(`return (${contrastProbe})`)());
          expect(
            r.ratio,
            `${query} card ${i} ${sel}: ${r.fg} on ${r.bg}`,
          ).toBeGreaterThanOrEqual(4.5);
          expect(
            r.bgLum,
            `${query} card ${i} ${sel} sits on a dark surface`,
          ).toBeLessThan(0.35);
        }
      }
    });
  }

  for (const size of [
    { width: 1280, height: 720 },
    { width: 390, height: 844 },
  ]) {
    test(`#57 chat settings panels stay dark when scrolled and after reload (${size.width}px)`, async ({
      page,
    }) => {
      await page.setViewportSize(size);
      await signInAsRafael(page);
      const check = async () => {
        await page.goto("/workspace/app/admin/chat-settings?locale=en-US");
        const panels = page.locator(".chatlangadmin");
        await expect(panels.first()).toBeVisible({ timeout: 90000 });
        const count = await panels.count();
        expect(count).toBeGreaterThan(0);
        for (const heading of [/Location sharing/i, /Voice messages/i]) {
          const h = page.getByText(heading).first();
          await h.scrollIntoViewIfNeeded();
          await expect(h).toBeVisible();
        }
        for (let i = 0; i < count; i++) {
          const panel = panels.nth(i);
          await panel.scrollIntoViewIfNeeded();
          const r = await panel.evaluate(new Function(`return (${contrastProbe})`)());
          expect(r.bgLum, `panel ${i} background ${r.bg} must be dark`).toBeLessThan(
            0.35,
          );
          expect(r.ratio, `panel ${i} text ${r.fg} on ${r.bg}`).toBeGreaterThanOrEqual(
            4.5,
          );
        }
      };
      await check();
      // away and back, then reload
      await page.evaluate(() => window.scrollTo(0, 0));
      await check();
      await page.reload();
      await check();
    });
  }
});

test("#59 light mode search results keep dark text on light cards", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await signInAsRafael(page);
  await page.goto("/workspace/app/chat?locale=en-US#search=meeting");
  const card = page
    .getByRole("region", { name: "Results for “meeting”" })
    .locator(".chatsearch-result")
    .first();
  await expect(card).toBeVisible({ timeout: 90000 });
  const r = await card
    .locator(".chatsearch-text")
    .first()
    .evaluate(new Function(`return (${contrastProbe})`)());
  expect(r.bgLum).toBeGreaterThan(0.5);
  expect(r.ratio).toBeGreaterThanOrEqual(4.5);
});
