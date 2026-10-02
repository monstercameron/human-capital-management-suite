import { test, expect, devices } from "@playwright/test";

// CHATBUG-064: on a phone a message has no hover bar, so its More button opens
// a sheet at the foot of the screen. The sheet lists a row of reactions, then
// Reply in thread and Save, ahead of the rest of the menu. This is the real
// browser's view of that: a 390 px touch screen, a tap on More, and what the
// sheet holds and where it sits. It runs against the dev cell the other live
// specs use (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

const channel = "/workspace/app/chat?locale=en-US#channel=general";

test.use({
  viewport: { width: 390, height: 844 },
  hasTouch: true,
  isMobile: true,
  userAgent: devices["Pixel 7"].userAgent,
});
test.setTimeout(90000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: /^Continue as / }).first().click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("a message's More button opens a bottom sheet with reactions, Reply in thread and Save", async ({
  page,
}) => {
  await signIn(page);
  await page.goto(channel);
  const message = page.locator(".message[data-message-id]").first();
  await expect(message).toBeVisible({ timeout: 30000 });

  // A tap, as a finger sends it: there is no hover. Touching the message is what
  // draws its action bar (and only its own), so the first tap lands on the text.
  await message.locator(".message-body").first().tap();
  const more = message.getByRole("button", { name: "More", exact: true });
  await expect(more).toBeVisible();
  await more.tap();

  const sheet = page.locator(".message-menu").first();
  await expect(sheet).toBeVisible();

  // It is a sheet: the width of the screen, against the bottom edge.
  const box = await sheet.boundingBox();
  const viewport = page.viewportSize();
  expect(Math.round(box.x)).toBe(0);
  expect(Math.round(box.width)).toBe(viewport.width);
  expect(Math.round(box.y + box.height)).toBe(viewport.height);

  // The reaction row comes first and holds the quick reactions and Add reaction.
  const reactions = sheet.locator(".menu-reaction-row");
  await expect(reactions).toBeVisible();
  const buttons = reactions.getByRole("menuitem");
  expect(await buttons.count()).toBeGreaterThanOrEqual(2);
  await expect(reactions.locator("[data-action='react-pick']")).toBeVisible();

  // Reply in thread and Save follow it, ahead of Copy link.
  const names = (await sheet.getByRole("menuitem").allInnerTexts()).map((text) =>
    text.trim(),
  );
  const reply = names.findIndex((text) => /reply/i.test(text));
  const save = names.findIndex((text) => /save/i.test(text));
  const copy = names.findIndex((text) => /copy link/i.test(text));
  expect(reply).toBeGreaterThanOrEqual(0);
  expect(save).toBeGreaterThanOrEqual(0);
  expect(copy).toBeGreaterThan(Math.max(reply, save));

  // Every row is a touch target.
  for (const row of await sheet.locator(".menu-item").all()) {
    const rowBox = await row.boundingBox();
    expect(rowBox.height).toBeGreaterThanOrEqual(44);
  }

  // Reply in thread opens the thread and puts the sheet away.
  await sheet.locator("[data-action='reply']").tap();
  await expect(sheet).toBeHidden();
  await expect(page.locator(".thread-pane")).toBeVisible();
});
