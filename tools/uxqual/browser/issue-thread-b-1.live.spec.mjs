import { test, expect, devices } from "@playwright/test";

// Chat issue sweep, lane thread-b chunk 1: #3894 (View in channel on a phone),
// #3891 (thread reply actions reachable by keyboard at phone width),
// #3873 (thread preview keeps its toggle). Runs against the dev cell
// (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openThread(page, channel) {
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
  const link = page
    .getByRole("button", { name: /[0-9]+\s*repl|repl\S*\s*[0-9]+/i })
    .first();
  await expect(link).toBeVisible({ timeout: 15000 });
  await link.click();
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
}

const phone = {
  viewport: { width: 390, height: 844 },
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
};

test("#3894 View in channel on a phone shows the channel and a way back", async ({
  browser,
}) => {
  const context = await browser.newContext(phone);
  const page = await context.newPage();
  await signIn(page);
  await openThread(page, "engineering");
  const pane = page.locator(".thread-pane").first();
  await expect(pane).toBeVisible();
  const view = page.getByRole("button", { name: /View in channel/i }).first();
  await view.click();
  // The thread overlay is set aside and the channel shows.
  await expect(pane).toBeHidden({ timeout: 10000 });
  const back = page.getByRole("button", { name: "Back to thread" });
  await expect(back).toBeVisible();
  const bb = await back.boundingBox();
  expect(bb.height).toBeGreaterThanOrEqual(44);
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: /channel/i })
      .first(),
  ).toBeVisible();
  // The root message is in view.
  const inView = await page.evaluate(() => {
    const el =
      document.querySelector(".message.thread-active") ||
      document.querySelector(
        ".message.highlight, .message.is-highlighted, .message:target",
      );
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return r.bottom > 0 && r.top < window.innerHeight;
  });
  expect(inView).toBe(true);
  // Back returns to the thread with focus on View in channel.
  await back.click();
  await expect(pane).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() =>
        /view in channel/i.test(
          document.activeElement?.textContent ||
            document.activeElement?.getAttribute("aria-label") ||
            "",
        ),
      ),
    )
    .toBe(true);
  await context.close();
});

test("#3894 View in channel keeps thread and channel side by side on desktop", async ({
  browser,
}) => {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await context.newPage();
  await signIn(page);
  await openThread(page, "engineering");
  // The control is revealed on hover over the thread root.
  await page.locator(".thread-root").first().hover();
  await page
    .getByRole("button", { name: /View in channel/i })
    .first()
    .click();
  await expect(page.locator(".thread-pane").first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Back to thread" })).toHaveCount(0);
  await context.close();
});

test("#3891 thread reply actions are reachable by Tab at phone width", async ({
  browser,
}) => {
  const context = await browser.newContext({
    ...phone,
    viewport: { width: 354, height: 767 },
  });
  const page = await context.newPage();
  await signIn(page);
  await openThread(page, "engineering");
  const actions = page.locator(".thread-pane .thread-message-actions");
  expect(await actions.count()).toBeGreaterThan(0);
  const save = actions.first().getByRole("button", { name: /Save/i }).first();
  const more = actions.first().getByRole("button", { name: /More/i }).first();
  await expect(save).toBeVisible();
  await expect(more).toBeVisible();
  for (const b of [save, more]) {
    const box = await b.boundingBox();
    expect(box.height).toBeGreaterThanOrEqual(44);
    expect(box.width).toBeGreaterThanOrEqual(44);
  }
  // Tab from the save button reaches More.
  await save.focus();
  await page.keyboard.press("Tab");
  expect(
    await page.evaluate(() =>
      /more/i.test(
        document.activeElement?.getAttribute("aria-label") ||
          document.activeElement?.textContent ||
          "",
      ),
    ),
  ).toBe(true);
  await context.close();
});

for (const [name, viewport] of [
  ["phone 354", { width: 354, height: 767 }],
  ["intermediate 909", { width: 909, height: 727 }],
]) {
  test(`#3873 thread preview keeps its toggle and returns to editing (${name})`, async ({
    browser,
  }) => {
    const context = await browser.newContext({ viewport });
    const page = await context.newPage();
    await signIn(page);
    await openThread(page, "engineering");
    const input = page.locator("#thread-composer").first();
    await input.click();
    await page.keyboard.type("zz-3873 Test **thread**");
    const toggle = page
      .locator("form.thread-composer .composer-preview-toggle")
      .first();
    await expect(toggle).toBeVisible();
    await toggle.click();
    // Focus leaves the form (as a real reader clicking elsewhere would).
    await page.evaluate(() => document.activeElement && document.activeElement.blur());
    await expect(toggle).toBeVisible();
    const box = await toggle.boundingBox();
    expect(box.height).toBeGreaterThanOrEqual(44);
    await toggle.click();
    await expect(input).toBeVisible({ timeout: 15000 });
    await input.fill("");
    await context.close();
  });
}
