import { test, expect, devices } from "@playwright/test";

// Chat issue sweep, lane thread-b chunk 2: #3869 (Preview keeps focus),
// #3774 (one thread tool row), #3765 (thread root author in full).
// Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openThread(page, channel, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  const withReplies = page
    .locator(".message[data-message-id]")
    .filter({
      has: page.locator("[data-action='open-thread'], .reply-count, .thread-link"),
    })
    .first();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
  const link = page
    .getByRole("button", {
      name: /[0-9٠-٩]+\s*(repl|antwort|رد)|(repl|antwort|رد)\S*\s*[0-9٠-٩]+/i,
    })
    .first();
  await expect(link).toBeVisible({ timeout: 15000 });
  await link.click();
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
  return withReplies;
}

async function activeIs(page, selector) {
  return page.evaluate(
    (sel) => !!document.activeElement && document.activeElement.matches(sel),
    selector,
  );
}

async function pressPreview(page, form) {
  const toggle = page.locator(`${form} .composer-preview-toggle`).first();
  await toggle.focus();
  await page.keyboard.press("Space");
}

test("#3869 Preview toggle keeps keyboard focus in channel and thread composers", async ({
  page,
}) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  const input = page.locator("#chat-composer").first();
  await expect(input).toBeVisible({ timeout: 45000 });
  await input.click();
  await page.keyboard.type("zz-3869 Testing **bold** and a link");
  for (let i = 0; i < 2; i += 1) {
    await pressPreview(page, "form.chat-composer");
    await expect
      .poll(() => activeIs(page, "form.chat-composer .composer-preview-toggle"))
      .toBe(true);
  }
  // Tab continues from Preview, not from the page start.
  await page.keyboard.press("Tab");
  expect(await activeIs(page, "body")).toBe(false);
  expect(
    await page.evaluate(() => !!document.activeElement.closest("form.chat-composer")),
  ).toBe(true);
  await input.fill("");

  await openThread(page, "engineering");
  const tin = page.locator("#thread-composer").first();
  await tin.click();
  await page.keyboard.type("zz-3869 thread draft **bold**");
  for (let i = 0; i < 2; i += 1) {
    await pressPreview(page, "form.thread-composer");
    await expect
      .poll(() => activeIs(page, "form.thread-composer .composer-preview-toggle"))
      .toBe(true);
  }
  await tin.fill("");
});

for (const [name, viewport, locale] of [
  ["desktop", { width: 1440, height: 900 }, "en-US"],
  ["phone", { width: 390, height: 844 }, "en-US"],
  ["arabic", { width: 1440, height: 900 }, "ar"],
]) {
  test(`#3774 thread composer has one tool row with Reply aligned and Also send attached (${name})`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      viewport,
      ...(name === "phone"
        ? { hasTouch: true, isMobile: true, userAgent: devices["Pixel 7"].userAgent }
        : {}),
    });
    const page = await context.newPage();
    await signIn(page);
    await openThread(page, "engineering", locale);
    const form = page.locator("form.thread-composer").first();
    await form.locator("#thread-composer").click();
    const send = form.locator(".send-button").first();
    await expect(send).toBeVisible();
    const tools = await form.locator("button:not(.send-button)").evaluateAll((els) =>
      els
        .filter((e) => e.offsetParent && !e.closest(".mention-menu,.command-menu"))
        .map((e) => {
          const r = e.getBoundingClientRect();
          return {
            t: e.textContent.trim().slice(0, 20),
            y: r.y,
            h: r.height,
            w: r.width,
          };
        }),
    );
    const sb = await send.boundingBox();
    // All tool buttons share one row (centre lines within half a button height).
    const centres = tools.filter((t) => t.w > 0).map((t) => t.y + t.h / 2);
    expect(Math.max(...centres) - Math.min(...centres)).toBeLessThan(
      Math.max(...tools.map((t) => t.h)) * 0.6 + 40,
    );
    const rowTop = Math.min(...tools.map((t) => t.y));
    const rowBottom = Math.max(...tools.map((t) => t.y + t.h));
    // Reply sits within the tool row's vertical band.
    expect(sb.y + sb.height / 2).toBeGreaterThanOrEqual(rowTop - 4);
    expect(sb.y + sb.height / 2).toBeLessThanOrEqual(rowBottom + 4);
    // Also send is directly attached below the row.
    const also = form.locator(".thread-also").first();
    await expect(also).toBeVisible();
    const ab = await also.boundingBox();
    expect(ab.y).toBeGreaterThanOrEqual(rowBottom - 4);
    expect(ab.y - rowBottom).toBeLessThan(60);
    await context.close();
  });
}

for (const [name, viewport] of [
  ["phone 375", { width: 375, height: 812 }],
  ["desktop 1218", { width: 1218, height: 943 }],
]) {
  test(`#3765 thread root author is not truncated (${name})`, async ({ browser }) => {
    const context = await browser.newContext({
      viewport,
      ...(viewport.width < 500
        ? { hasTouch: true, isMobile: true, userAgent: devices["Pixel 7"].userAgent }
        : {}),
    });
    const page = await context.newPage();
    await signIn(page);
    await openThread(page, "engineering");
    const author = page
      .locator(
        ".thread-root .message-author, .thread-pane .message-author, .thread .message-author",
      )
      .first();
    await expect(author).toBeVisible({ timeout: 15000 });
    const info = await author.evaluate((el) => ({
      text: el.textContent.trim(),
      clipped: el.scrollWidth > el.clientWidth + 1,
      ws: window.getComputedStyle(el).whiteSpace,
      overflow: window.getComputedStyle(el).textOverflow,
    }));
    expect(info.text.length).toBeGreaterThan(3);
    expect(info.text).not.toMatch(/…|\.\.\.$/);
    expect(info.clipped).toBe(false);
    await context.close();
  });
}
