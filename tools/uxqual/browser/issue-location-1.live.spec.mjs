import { test, expect } from "@playwright/test";

// Live checks for the Location sheet: #3908 (bound to its conversation),
// #3863 (duration hint), #3862 (typed address needs a resolved point).
// Nothing is sent and no seeded data is changed.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, name) {
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
}

async function openSheet(page) {
  await expect(page.locator("[data-chatmap-action='toggle']").first()).toBeAttached({
    timeout: 30000,
  });
  await expect(async () => {
    await page.locator(".composer-add-trigger").first().click({ timeout: 5000 });
    await page
      .getByRole("menuitem", { name: /^Location/ })
      .first()
      .click({ timeout: 5000 });
    await expect(page.locator("[data-chatmap-sheet]").first()).toBeVisible({
      timeout: 3000,
    });
  }).toPass({ timeout: 60000 });
  const sheet = page.locator("[data-chatmap-sheet]").first();
  await expect(sheet).toBeVisible();
  return sheet;
}

test("#3908 the Location sheet names its channel and is cleared when another conversation is selected", async ({
  page,
}) => {
  await signIn(page);
  await openChannel(page, "general");
  let sheet = await openSheet(page);
  await expect(sheet.locator("[data-chatmap-destination]")).toContainText(
    /Everyone in #?general can see this/i,
  );
  await sheet.locator("[data-chatmap-field='precision']").selectOption("exact");
  const marker = `zz-note-${Date.now()}`;
  await sheet.locator("[data-chatmap-field='note']").fill(marker);

  // Select another conversation from the list, as a person would.
  await page
    .locator("button.chat-row")
    .filter({ has: page.locator(".chat-row-name", { hasText: /^engineering$/ }) })
    .first()
    .click();
  await page.waitForTimeout(1500);
  // The sheet from #general must not still be showing the old choices.
  const visible = await page.locator("[data-chatmap-sheet]").first().isVisible();
  expect(visible, "sheet should be closed after switching conversation").toBe(false);

  sheet = await openSheet(page);
  await expect(sheet.locator("[data-chatmap-field='precision']")).toHaveValue(
    "approximate",
  );
  await expect(sheet.locator("[data-chatmap-field='note']")).toHaveValue("");
  await expect(sheet.locator("[data-chatmap-destination]")).toContainText(
    /Everyone in #?engineering can see this/i,
  );
});

test("#3863 the duration hint explains Keep with the message against the maximum and follows the selection", async ({
  page,
}) => {
  await signIn(page);
  await openChannel(page, "general");
  const sheet = await openSheet(page);
  const select = sheet.locator("[data-chatmap-field='duration']");
  const hint = sheet.locator("[data-chatmap-retention]");
  const describedBy = await select.getAttribute("aria-describedby");
  expect(describedBy).toBe(await hint.getAttribute("id"));

  await select.selectOption("hour");
  const hourText = ((await hint.textContent()) || "").trim();
  await select.selectOption("keep");
  await expect(hint).toBeVisible();
  const keepText = ((await hint.textContent()) || "").trim();
  expect(keepText).not.toBe("");
  expect(keepText).not.toBe(hourText);
  // Says the location can end before/independent of the message, with a limit.
  expect(keepText).toMatch(/message/i);
  expect(keepText).toMatch(/\d+\s*(hour|minute|day)|24/i);
  expect(keepText).toMatch(/even if|removed|ends|stays/i);

  await select.selectOption("live15");
  await expect(hint).toBeHidden();
  await select.selectOption("day");
  await expect(hint).toBeVisible();
  const dayText = ((await hint.textContent()) || "").trim();
  expect(dayText).not.toBe(keepText);
});

test("#3862 a typed address that is not resolved keeps Send disabled and offers an alternative", async ({
  page,
}) => {
  await signIn(page);
  await openChannel(page, "general");
  const sheet = await openSheet(page);
  const send = sheet.locator("[data-chatmap-action='send']");
  await sheet
    .locator("[data-chatmap-action='source'][data-source='typed_address']")
    .click();
  const field = sheet.locator("[data-chatmap-field='readable']");
  await field.fill("Seattle, WA");
  await expect(send).toBeDisabled();
  const find = sheet.locator("[data-chatmap-action='lookup']");
  if (await find.isEnabled()) {
    await find.click();
  }
  await page.waitForTimeout(2500);
  await expect(send).toBeDisabled();
  await field.fill("zzzz not a place 12345");
  await expect(send).toBeDisabled();
  const status = sheet.locator("[data-chatmap-status]");
  const hint = sheet.locator("[data-chatmap-unavailable='lookup']");
  const text = `${(await status.textContent()) || ""} ${
    (await hint.isVisible()) ? await hint.textContent() : ""
  }`;
  expect(text).toMatch(/Where I am|A job site|find the address|find address/i);
});
