import { test, expect } from "@playwright/test";

// To-do previews (#3952, #3951, #3940) against the live dev cell. Each test
// works in its own channel; every preview is cancelled and nothing is posted.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

// Each test works in its own channel (zz-todo-...) with two members, so a draft
// typed by another agent in a seeded channel cannot reach this composer.
async function openGeneral(page, locale = "en-US") {
  const name = `zz-todo-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto(`/workspace/app/chat?locale=${locale}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  for (const person of ["Amina Rahman", "Anika Desai"]) {
    const search = page.locator("#new-chat-member-search");
    await search.fill(person);
    await page
      .locator(`[data-action='create-pick']`, { hasText: person })
      .first()
      .click();
  }
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
}

async function previewOf(page, text) {
  const input = page.locator("textarea.composer-input").first();
  const preview = page.locator(".chatcmd003-preview");
  // A draft saved by an earlier visit can bring its own preview back: put it
  // away so only the preview of this line is read.
  await page.waitForTimeout(1500);
  if (await preview.isVisible()) await cancel(page);
  // A saved draft can be restored after the first fill, so fill until the box
  // holds exactly this line, then send it.
  await expect(async () => {
    await expect(input).toBeEnabled({ timeout: 5000 });
    await input.fill(text);
    await page.waitForTimeout(400);
    await expect(input).toHaveValue(text, { timeout: 2000 });
  }).toPass({ timeout: 60000 });
  // The preview appears as the line is typed; Enter would post it.
  await expect(preview).toBeVisible({ timeout: 30000 });
  return preview;
}

async function cancel(page) {
  await expect(async () => {
    await page.locator("textarea.composer-input").first().focus();
    await page.keyboard.press("Escape");
    await expect(page.locator(".chatcmd003-preview")).toBeHidden({ timeout: 3000 });
  }).toPass({ timeout: 30000 });
}

test("#3951 timed deadlines show time and zone; date-only shows the day; due/by warn on unreadable dates", async ({
  page,
}) => {
  await signIn(page);
  await openGeneral(page);

  let preview = await previewOf(page, "/todo Review the plan due in an hour");
  const timed = await preview.innerText();
  expect(timed).toMatch(/\d{4}-\d{2}-\d{2}[ T]\d{1,2}:\d{2}/);
  expect(timed).toMatch(/\d{2}:\d{2} UTC/);
  // The "Deadline for ..." choice that explains the reading names the time too.
  expect.soft(timed).toMatch(/Due \d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC[^\n]*read from/);
  await cancel(page);

  preview = await previewOf(page, "/todo Review the plan due 2027-03-13");
  const dateOnly = await preview.innerText();
  expect(dateOnly).toContain("2027-03-13");
  expect(dateOnly).not.toMatch(/2027-03-13[ T]\d{1,2}:\d{2}/);
  expect(dateOnly).not.toMatch(/23:59/);
  await cancel(page);

  preview = await previewOf(
    page,
    "/todo Review the plan due 2027-03-13T12:30:00-05:00",
  );
  const stamped = await preview.innerText();
  expect(stamped).toMatch(/2027-03-13 12:30 UTC-5/);
  expect.soft(stamped).toMatch(/Due 2027-03-13 12:30 UTC-5[^\n]*read from/);
  await cancel(page);

  for (const lead of ["by", "due"]) {
    preview = await previewOf(page, `/todo Submit timecard ${lead} 03/04`);
    const text = await preview.innerText();
    expect(text).toMatch(/was not understood/i);
    expect(text).not.toMatch(/Due \d{4}-\d{2}-\d{2}/);
    await cancel(page);
  }
});

test("#3952 a second @mentioned person is named as not assigned and an owner choice is offered", async ({
  page,
}) => {
  await signIn(page);
  await openGeneral(page);
  const preview = await previewOf(
    page,
    "/todo @Amina Rahman @Anika Desai review the plan",
  );
  const text = await preview.innerText();
  expect(text).toMatch(/assigned to Amina Rahman/);
  expect(text).toMatch(/@Anika Desai is named too|Anika Desai.*not assigned/);
  const group = preview.locator("[role='radiogroup'][id^='chatcmd003-assign-']");
  await expect(group).toBeVisible();
  const pick = group.locator("button[role='radio']", { hasText: "Anika Desai" });
  await expect(pick).toBeEnabled({ timeout: 30000 });
  await expect(async () => {
    await pick.dispatchEvent("click", {}, { timeout: 4000 });
    await expect(pick).toHaveAttribute("aria-checked", "true", { timeout: 2000 });
  }).toPass({ timeout: 40000 });
  await expect(preview).toContainText("Anika Desai");
  await expect(preview.locator(".chatcmd002-task-text")).not.toContainText("@Amina");
  // Nobody is added to the channel and nothing is posted: only a preview.
  await cancel(page);
});

test("#3940 Arabic-Indic digits read as the same due date as Western digits", async ({
  page,
}) => {
  await signIn(page);
  await openGeneral(page, "ar");
  const western = await (
    await previewOf(page, "/todo مراجعة الملف 9 أكتوبر")
  ).innerText();
  await cancel(page);
  const indic = await (
    await previewOf(page, "/todo مراجعة الملف ٩ أكتوبر")
  ).innerText();
  const due = (s) => s.match(/\d{4}-\d{2}-\d{2}/)?.[0];
  expect(due(western)).toBeTruthy();
  expect(due(indic)).toBe(due(western));
  await cancel(page);
});
