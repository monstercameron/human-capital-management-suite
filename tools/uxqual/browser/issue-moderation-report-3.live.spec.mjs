import { test, expect, devices } from "@playwright/test";

// Live checks for #3762 (moderation tabs on a phone), #3760 (Close button not
// covered by the account strip) and #3697 (Dismiss/Restore ask first).

test.setTimeout(150000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openModerationOnPhone(page, locale) {
  await signIn(page);
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=general`);
  const rail = page.locator("[data-action=open-rail]").first();
  await rail.waitFor({ timeout: 90000 });
  await rail.tap();
  const mod = page.locator("[data-chatremove-open*='moderation']").first();
  await mod.waitFor({ timeout: 30000 });
  await mod.tap();
  await page.locator(".chatmod005-tabs").waitFor({ timeout: 30000 });
}

const phone = {
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
};

for (const [w, h] of [
  [375, 812],
  [320, 640],
]) {
  for (const locale of ["en-US", "de-DE", "ar"]) {
    test.describe(`phone ${w} ${locale}`, () => {
      test.use({ viewport: { width: w, height: h }, ...phone });

      test(`#3762 moderation tabs stay on one line (${w} ${locale})`, async ({
        page,
      }) => {
        await openModerationOnPhone(page, locale);
        const tabs = page.locator(".chatmod005-tabs .chatmod005-tab");
        const n = await tabs.count();
        expect(n).toBeGreaterThanOrEqual(2);
        const first = await tabs.first().boundingBox();
        const box = await page.locator(".chatmod005-tabs").boundingBox();
        for (let i = 0; i < n; i++) {
          const b = await tabs.nth(i).boundingBox();
          // single line: shorter than twice a line, same row as the first tab
          expect(Math.abs(b.y - first.y)).toBeLessThan(2);
          const lines = await tabs.nth(i).evaluate((el) => {
            const r = document.createRange();
            r.selectNodeContents(el);
            const tops = new Set(
              [...r.getClientRects()].map((q) => Math.round(q.top / 4)),
            );
            return tops.size;
          });
          expect(lines).toBeLessThanOrEqual(1);
          const fits = await tabs
            .nth(i)
            .evaluate((el) => el.scrollWidth <= el.clientWidth + 1);
          expect(fits).toBe(true);
        }
        // the row sits inside the viewport width (scrolls inside itself if wider)
        expect(box.x + box.width).toBeLessThanOrEqual(w + 1);
        expect(box.x).toBeGreaterThanOrEqual(-1);
      });

      test(`#3760 close button receives the tap (${w} ${locale})`, async ({ page }) => {
        await openModerationOnPhone(page, locale);
        const close = page.getByRole("button", {
          name: /^(Close moderation|Moderation schließen|إغلاق الإشراف)$/,
        });
        await expect(close).toBeVisible();
        const b = await close.boundingBox();
        expect(b.x).toBeGreaterThanOrEqual(0);
        expect(b.x + b.width).toBeLessThanOrEqual(w + 1);
        const handle = await close.elementHandle();
        const hit = await page.evaluate(
          ([el, x, y]) => {
            const top = document.elementFromPoint(x, y);
            return !!top && el.contains(top);
          },
          [handle, b.x + b.width / 2, b.y + b.height / 2],
        );
        expect(hit).toBe(true);
        await close.tap();
        await expect(page.locator(".chatmod005-tabs")).toBeHidden();
        expect(page.url()).not.toMatch(/account|profile/);
      });
    });
  }
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("#3697 Dismiss and Restore ask first and offer an optional note", async ({
    page,
  }) => {
    await signIn(page);
    await page.goto("/workspace/app/chat?locale=en-US#channel=general");
    const mod = page.locator("[data-chatremove-open*='moderation']").first();
    await mod.waitFor({ timeout: 90000 });
    await mod.click();
    await page.locator(".chatmod005-tabs").waitFor({ timeout: 20000 });
    const posts = [];
    page.on("request", (r) => {
      if (r.method() === "POST" && /moderation|decide/i.test(r.url()))
        posts.push(r.url());
    });
    const dismiss = page
      .locator(".chatremove")
      .getByRole("button", { name: "Dismiss", exact: true })
      .first();
    await expect(dismiss).toBeVisible();
    await dismiss.click();
    const dialog = page.locator("[data-chatremove-overlay=dialog]");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(/Dismiss this item?/);
    await expect(dialog.locator("textarea, input[type=text]").first()).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Dismiss", exact: true }),
    ).toBeVisible();
    expect(posts).toEqual([]);
    // Cancel leaves the item open and decides nothing.
    await dialog.getByRole("link", { name: /^cancel$/i }).click();
    await expect(dialog).toBeHidden();
    await expect(
      page
        .locator(".chatremove")
        .getByRole("button", { name: "Dismiss", exact: true })
        .first(),
    ).toBeVisible();
    expect(posts).toEqual([]);

    // Restore on a resolved removal also asks first.
    await page.getByRole("tab", { name: /^Resolved/ }).click();
    const restore = page
      .locator(".chatremove")
      .getByRole("button", { name: "Restore", exact: true })
      .first();
    {
      await expect(restore).toBeVisible({ timeout: 30000 });
      await restore.click();
      const restoreDialog = page.locator("[data-chatremove-overlay=dialog]");
      await expect(restoreDialog).toBeVisible();
      await expect(
        restoreDialog.locator("textarea, input[type=text]").first(),
      ).toBeVisible();
      await restoreDialog.getByRole("link", { name: /^cancel$/i }).click();
      await expect(restoreDialog).toBeHidden();
      expect(posts).toEqual([]);
    }
  });
});
