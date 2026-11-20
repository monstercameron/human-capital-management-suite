import { test, expect, devices } from "@playwright/test";

// Saved lane, chunk 3: #3766 (touch-neutral empty text), #3719 (Save for later
// is a visible row in the message menu), #3597 (44px phone targets). Runs
// against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default). Writes
// only to its own zz-saved channel and removes its saved item at the end.

test.describe.configure({ mode: "serial" });
test.setTimeout(180000);

const marker = `zz-saved-${Math.random().toString(36).slice(2, 7)}`;
const baseURL = process.env.HCMNEXT_DEV_URL || "http://127.0.0.1:8080";
const composerSel = "textarea[name='message']";
let desk;
let deskPage;

async function signIn(page, persona) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: persona }).first().click();
  await page.waitForURL(/\/workspace\/app\//);
}

const phone = (width, height) => ({
  baseURL,
  viewport: { width, height },
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
});

// Opens the Saved page the way a person does on a phone: the URL hash alone
// restores the last conversation, so go back to the list and press Saved.
async function openSavedOnPhone(page, locale) {
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await page.reload();
  const saved = page.locator("[data-saved-action='toggle']").first();
  const panel = page.locator(".chatsave-panel:not([hidden])");
  for (let i = 0; i < 60; i++) {
    if (await panel.isVisible().catch(() => false)) return;
    if (await saved.isVisible().catch(() => false)) {
      await saved.click({ timeout: 3000 }).catch(() => {});
    } else {
      const open = page.locator("[data-action='open-rail']").first();
      if (await open.isVisible().catch(() => false)) {
        await open.click({ timeout: 3000 }).catch(() => {});
      }
    }
    await page.waitForTimeout(1000);
  }
  await expect(panel).toBeVisible({ timeout: 1000 });
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(150000);
  desk = await browser.newContext({ baseURL, viewport: { width: 1440, height: 900 } });
  deskPage = await desk.newPage();
  const page = deskPage;
  await signIn(page, /^Continue as Rafael Torres/);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(composerSel)).toBeVisible({ timeout: 60000 });
  await page
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const dialog = page.locator(".create-dialog");
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await dialog.getByRole("textbox").first().fill(marker);
  await dialog.locator("button[type=submit]").click();
  await expect(page.locator(composerSel)).toHaveAttribute(
    "placeholder",
    new RegExp(marker),
    { timeout: 30000 },
  );
  await page.locator(composerSel).fill(marker + " body");
  await page.locator(composerSel).press("Enter");
  await page.waitForTimeout(3000);
  if (await page.locator(composerSel).inputValue()) {
    await page.locator(composerSel).press("Enter");
  }
  await expect(
    page.locator(".message[data-message-id]", { hasText: `${marker} body` }).first(),
  ).toBeVisible({ timeout: 30000 });
});

test.afterAll(async () => {
  test.setTimeout(90000);
  try {
    const page = deskPage;
    await page.goto("/workspace/app/chat?locale=en-US#saved");
    await page.reload();
    const item = page.locator(".chatsave-item", { hasText: `${marker} body` }).first();
    if (await item.isVisible({ timeout: 40000 }).catch(() => false)) {
      await item.hover();
      await item
        .getByRole("button", { name: /Remove/ })
        .first()
        .click();
    }
  } catch (error) {
    test
      .info()
      .annotations.push({ type: "cleanup", description: String(error).slice(0, 120) });
  }
  await desk?.close();
});

test("#3766 Saved empty state on a phone does not ask for hover", async ({
  browser,
}) => {
  for (const [locale, banned] of [
    ["en-US", /hover/i],
    ["de-DE", /hover|dar[üu]ber fahren|mit der maus/i],
    ["ar", /تمرير|مرر/],
  ]) {
    const context = await browser.newContext(phone(375, 812));
    const page = await context.newPage();
    // Linh Tran has nothing saved in the demo, so Saved shows its first-use text.
    await signIn(page, /^Continue as Linh Tran/);
    await openSavedOnPhone(page, locale);
    const empty = page.locator(".chatsave-empty").first();
    await expect(empty).toBeVisible({ timeout: 40000 });
    const text = (await empty.innerText()).trim();
    expect(text.length, locale).toBeGreaterThan(5);
    expect(text, locale).not.toMatch(banned);
    if (locale === "en-US") expect(text).toMatch(/bookmark/i);
    test.info().annotations.push({ type: `3766 ${locale}`, description: text });
    await context.close();
  }
});

test("#3719 Save for later is a visible, clickable, keyboard-reachable row in the message menu", async () => {
  const page = deskPage;
  // The page is still on the channel the setup created and posted in; a busy
  // server can reload it, so reopen the menu with a reload as the fallback.
  const message = page
    .locator(".message[data-message-id]", { hasText: `${marker} body` })
    .first();
  for (let attempt = 0; attempt < 4; attempt++) {
    const ok = await (async () => {
      await message.waitFor({ timeout: 45000 });
      await message.hover();
      await message
        .getByRole("button", { name: /^More( actions)?$/ })
        .click({ timeout: 10000 });
      return true;
    })().catch(() => false);
    if (ok) break;
    await page.reload();
  }
  const menu = page.locator(".message-menu").first();
  await expect(menu).toBeVisible();
  const row = menu.getByRole("menuitem", { name: /^Save for later/ }).first();
  await expect(row).toBeVisible();
  const b = await row.boundingBox();
  expect(b.width).toBeGreaterThan(40);
  expect(b.height).toBeGreaterThan(20);

  // Arrow keys walk the menu: every stop is a visible, sized item, and one of
  // them is Save for later (no invisible stop for a keyboard or screen reader).
  const labels = [];
  const invisibleStops = [];
  for (let i = 0; i < 16; i++) {
    const stop = await page.evaluate(() => {
      const el = document.activeElement;
      const r = el ? el.getBoundingClientRect() : { width: 0, height: 0 };
      return {
        label: el?.getAttribute("aria-label") || el?.textContent?.trim() || "",
        inMenu: !!el?.closest(".message-menu"),
        w: r.width,
        h: r.height,
      };
    });
    labels.push(stop.label);
    if (stop.inMenu && (stop.w === 0 || stop.h === 0)) invisibleStops.push(stop.label);
    await page.keyboard.press("ArrowDown");
  }
  expect(invisibleStops, "keyboard stops with no size").toEqual([]);
  expect(labels.join("|")).toMatch(/Save for later/);

  // Clicking it saves, and the row then reads Remove from Saved.
  await row.click();
  await page.waitForTimeout(1500);
  if (!(await page.locator(".message-menu").first().isVisible())) {
    await message.hover();
    await message.getByRole("button", { name: /^More( actions)?$/ }).click();
  }
  await expect(
    page
      .locator(".message-menu")
      .first()
      .getByRole("menuitem", { name: /Remove from Saved/ })
      .first(),
  ).toBeVisible({ timeout: 15000 });
  await page.keyboard.press("Escape");
  // Let the save reach the server before the next full page load.
  await page.waitForTimeout(4000);
});

test("#3597 Saved phone Back and item actions are 44px at 320 and 390, en and ar", async ({
  browser,
}) => {
  const context = await browser.newContext(phone(390, 844));
  const page = await context.newPage();
  await signIn(page, /^Continue as Rafael Torres/);
  for (const [w, h] of [
    [390, 844],
    [320, 740],
  ]) {
    for (const locale of ["en-US", "ar"]) {
      await page.setViewportSize({ width: w, height: h });
      await openSavedOnPhone(page, locale);
      const item = page
        .locator(".chatsave-item", { hasText: `${marker} body` })
        .first();
      await expect(item).toBeVisible({ timeout: 40000 });
      const back = page.locator(".chatsave-panel:not([hidden]) .chatsave-back").first();
      const targets = [back, ...(await item.locator(".chatsave-actions button").all())];
      expect(targets.length, `${locale} ${w} targets`).toBeGreaterThanOrEqual(5);
      for (const t of targets) {
        const b = await t.boundingBox();
        const name = await t.getAttribute("aria-label");
        expect(b.width, `${locale} ${w} ${name} width`).toBeGreaterThanOrEqual(43.5);
        expect(b.height, `${locale} ${w} ${name} height`).toBeGreaterThanOrEqual(43.5);
      }
      // Header title and Back do not overlap or leave the screen.
      const title = page
        .locator(".chatsave-panel:not([hidden]) :is(h1,h2,.chatsave-title)")
        .first();
      const tb = await title.boundingBox();
      const bb = await back.boundingBox();
      expect(tb.x, `${locale} ${w} title left`).toBeGreaterThanOrEqual(-0.5);
      expect(tb.x + tb.width, `${locale} ${w} title right`).toBeLessThanOrEqual(
        w + 0.5,
      );
      const overlap =
        Math.min(tb.x + tb.width, bb.x + bb.width) - Math.max(tb.x, bb.x) > 1 &&
        Math.min(tb.y + tb.height, bb.y + bb.height) - Math.max(tb.y, bb.y) > 1;
      expect(overlap, `${locale} ${w} title overlaps Back`).toBe(false);
    }
  }
  await context.close();
});
