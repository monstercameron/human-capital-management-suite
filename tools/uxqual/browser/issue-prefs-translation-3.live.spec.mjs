import { test, expect } from "@playwright/test";

// #3605 message-language editor focus, #82 preferences popover in the viewport,
// #3596 shell header disclosures on Chat. Runs against the dev cell. Writes only
// in its own zz-prefs channel.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function loadChat(page, locale = "en-US", hash = "#channel=general") {
  await page.goto(`/workspace/app/chat?locale=${locale}${hash}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
}

async function ownChannel(page) {
  const name = `zz-prefs-${Math.random().toString(36).slice(2, 8)}`;
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

const active = (page) =>
  page.evaluate(() => {
    const e = document.activeElement;
    return e
      ? {
          tag: e.tagName,
          label: e.getAttribute("aria-label") || e.textContent.trim().slice(0, 40),
          action: e.getAttribute("data-action") || "",
          cls: e.className || "",
        }
      : null;
  });

test("#3605 message language editor takes focus, is reachable, and Cancel/Escape return to More actions", async ({
  page,
}) => {
  await signIn(page);
  await loadChat(page);
  await ownChannel(page);
  const marker = `lang check ${Math.random().toString(36).slice(2, 8)}`;
  const composer = page.locator("textarea.composer-input").first();
  await composer.fill(marker);
  await composer.press("Enter");
  const message = page.locator(".message[data-message-id]", { hasText: marker });
  await expect(message).toBeVisible({ timeout: 30000 });

  for (let round = 0; round < 3; round++) {
    await message.hover();
    const more = message.getByRole("button", { name: /^More/ }).first();
    await more.focus();
    await more.press("Enter");
    const item = page.locator(".message-menu [data-action='chatlang-fix']");
    await expect(
      item,
      "Message language entry should be offered on own message",
    ).toBeVisible({ timeout: 20000 });
    await item.focus();
    await item.press("Enter");
    const editor = page.locator(".chatlang-fix");
    await expect(editor).toBeVisible();
    // Focus is inside the editor, not on the page.
    await expect
      .poll(
        () => page.evaluate(() => !!document.activeElement?.closest(".chatlang-fix")),
        {
          timeout: 5000,
        },
      )
      .toBe(true);
    // Tab reaches Cancel, still inside the editor.
    let reachedCancel = false;
    for (let i = 0; i < 14 && !reachedCancel; i++) {
      await page.keyboard.press("Tab");
      reachedCancel = (await active(page))?.action === "chatlang-fix-cancel";
    }
    expect(reachedCancel).toBe(true);
    if (round % 2 === 0) await page.keyboard.press("Enter");
    else await page.keyboard.press("Escape");
    await expect(editor).toBeHidden();
    await expect
      .poll(async () => {
        const a = await active(page);
        return a && /^More/.test(a.label) ? "more" : JSON.stringify(a);
      })
      .toBe("more");
  }
  // The message text is unchanged.
  await expect(message).toContainText(marker);
});

async function prefsBox(page) {
  await page.locator(".rail-prefs-control .chatux002-gear").click();
  const layer = page.locator('[data-chat-layer="chat-prefs"]');
  await expect(layer).toBeVisible({ timeout: 10000 });
  await page.waitForTimeout(400);
  return layer.boundingBox();
}

for (const [w, h, locale] of [
  [800, 900, "ar"],
  [800, 900, "en-US"],
  [1440, 900, "ar"],
  [1440, 900, "en-US"],
  [390, 844, "ar"],
  [320, 640, "en-US"],
]) {
  test(`#82 preferences popover stays inside the viewport at ${w}px ${locale}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: w, height: h });
    await signIn(page);
    await loadChat(page, locale);
    if (w < 700) {
      const toggle = page.locator(".mobile-chat-toggle");
      if (await toggle.isVisible()) await toggle.click();
    }
    const box = await prefsBox(page);
    expect(box).toBeTruthy();
    expect(box.x).toBeGreaterThanOrEqual(-0.5);
    expect(box.y).toBeGreaterThanOrEqual(-0.5);
    expect(box.x + box.width).toBeLessThanOrEqual(w + 0.5);
    if (w >= 700) {
      const rail = await page.locator("nav.chat-rail").first().boundingBox();
      if (rail) {
        expect(box.x).toBeGreaterThanOrEqual(rail.x - 1);
        expect(box.x + box.width).toBeLessThanOrEqual(rail.x + rail.width + 1);
      }
    }
  });
}

for (const [w, h] of [
  [1280, 720],
  [390, 844],
  [320, 640],
]) {
  for (const locale of ["en-US", "de-DE", "ar"]) {
    test(`#3596 account, language and launcher menus open and close on Chat at ${w}px ${locale}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: w, height: h });
      await signIn(page);
      await loadChat(page, locale);
      // On a phone the workspace bar is hidden while a conversation is open
      // (CHATUX-033); the conversation list keeps the Account menu only.
      const phone = w < 761;
      if (phone) await page.locator(".mobile-chat-toggle").click();
      const menus = phone
        ? ["details.account-menu"]
        : ["details.account-menu", "details.locale-menu"];
      for (const sel of menus) {
        const root = page.locator(sel);
        const summary = root.locator("summary").first();
        await summary.click();
        await expect(root).toHaveJSProperty("open", true);
        // Stays open (no immediate re-close from the composer's closed popover).
        await page.waitForTimeout(800);
        await expect(root).toHaveJSProperty("open", true);
        await expect(root.locator(".popover-surface")).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(root).toHaveJSProperty("open", false);
        await expect
          .poll(() =>
            page.evaluate(
              (s) => document.activeElement === document.querySelector(s + " summary"),
              sel,
            ),
          )
          .toBe(true);
        // Outside click closes.
        await summary.click();
        await expect(root).toHaveJSProperty("open", true);
        await page.mouse.click(w / 2, h - 5);
        await expect(root).toHaveJSProperty("open", false);
      }
      // Sign out is reachable from the Account menu on Chat.
      await page.locator("details.account-menu summary").first().click();
      await expect(page.locator(".account-menu-signout")).toBeVisible();
      await page.keyboard.press("Escape");
      // Launcher.
      const trigger = page.locator(".action-launcher-trigger");
      if (!phone) {
        await trigger.click();
        await expect(page.locator("#action-launcher-dialog")).toBeVisible();
        await page.waitForTimeout(600);
        await expect(page.locator("#action-launcher-dialog")).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.locator("#action-launcher-dialog")).toBeHidden();
      }
    });
  }
}
