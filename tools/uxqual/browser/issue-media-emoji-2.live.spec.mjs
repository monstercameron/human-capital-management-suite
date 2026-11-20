import { Buffer } from "node:buffer";
import { test, expect } from "@playwright/test";

const onePNG =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

// Live checks for #3851 (emoji picker heading follows the category), #3720
// (the rocket emoji is findable and selectable) and #3675 (a missing media
// object is shown unavailable once, with no retry). Nothing is posted: pickers
// are opened and closed, and the missing media is simulated by answering the
// grant request with 404 in the browser only.

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

const composerSel = "textarea[name='message']";

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, channel, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  await expect(page.locator(composerSel)).toBeVisible({ timeout: 60000 });
}

async function ownChannel(page) {
  const name = `zz-mediaemoji-${Math.random().toString(36).slice(2, 7)}`;
  await openChannel(page, "general");
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
  return name;
}

test("#3851 sticky heading and marked tab name the clicked category", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await page.getByRole("button", { name: "Insert emoji" }).first().click();
  const pop = page.locator(".emoji-pop").first();
  await expect(pop).toBeVisible();
  const tabs = pop.locator(".emoji-pop-tab");
  await expect(tabs).toHaveCount(9);
  const names = [];
  for (let i = 0; i < 9; i++) names.push(await tabs.nth(i).getAttribute("aria-label"));
  for (const name of names) {
    await pop.locator(`.emoji-pop-tab[aria-label='${name}']`).click();
    const read = () =>
      pop.evaluate((el) => ({
        sticky: el.querySelector(".emoji-pop-sticky")?.textContent.trim(),
        current: el.querySelector(".emoji-pop-tab.current")?.getAttribute("aria-label"),
      }));
    await expect
      .poll(read, { timeout: 5000, message: name })
      .toEqual({ sticky: name, current: name });
  }
});

test("#3720 rocket is findable by name and listed in Travel & Places", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  await page.getByRole("button", { name: "Insert emoji" }).first().click();
  const pop = page.locator(".emoji-pop").first();
  const search = pop.getByPlaceholder(/search emoji/i);
  await expect(search).toBeVisible();
  await search.fill("rocket");
  await expect(pop.locator(".emoji-pop-btn", { hasText: "🚀" }).first()).toBeVisible({
    timeout: 10000,
  });
  await expect(pop).not.toContainText(/No emoji match/i);
  await search.fill("");
  await pop.locator(".emoji-pop-tab[aria-label='Travel & Places']").click();
  await expect
    .poll(() => pop.locator(".emoji-pop-scroll").evaluate((el) => el.textContent), {
      timeout: 5000,
    })
    .toContain("🚀");
});

test("#3675 a missing media object is shown unavailable once, with no retry", async ({
  page,
}) => {
  await signIn(page);
  const channel = await ownChannel(page);
  await page.locator(composerSel).fill("media-emoji check");
  const uploaded = page.waitForResponse((r) => /media\/upload/.test(r.url()));
  await page
    .locator('input[type="file"][multiple]')
    .setInputFiles([
      { name: "dot.png", mimeType: "image/png", buffer: Buffer.from(onePNG, "base64") },
    ]);
  await uploaded;
  await page.waitForTimeout(1000);
  await page.locator(".chat-composer .send-button:not(:disabled)").click();
  await expect(page.locator(".attachment-image-open").first()).toBeAttached({
    timeout: 30000,
  });
  // Reload with the grant answered 404, as a missing stored file is.
  const grants = [];
  await page.route(/\/v1\/chat\/media\/[^/]+\/grant/, (route) => {
    grants.push(route.request().url());
    return route.fulfill({
      status: 404,
      contentType: "application/json",
      body: '{"error":"not found"}',
    });
  });
  await page.goto("about:blank");
  await openChannel(page, channel);
  await expect(page.getByText(/Preview unavailable/i).first()).toBeVisible({
    timeout: 30000,
  });
  expect(grants.length).toBeGreaterThan(0);
  // Once it is shown unavailable nothing asks again (the old retry came every
  // 20 seconds), so wait past that and count.
  await page.waitForTimeout(3000);
  const settled = grants.length;
  await page.waitForTimeout(25000);
  expect(grants.length, "grant requests after the unavailable state").toBe(settled);
  await expect(page.getByText(/Preview unavailable/i).first()).toBeVisible();
});
