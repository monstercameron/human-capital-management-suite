import { test, expect } from "@playwright/test";

// Saved lane, chunk 1: #3933 (Saved says it is private), #3912 (custom
// reminder form fits the panel), #3911 (note editor has Save and Cancel on a
// phone). Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by
// default). Writes only to its own zz-saved channel and removes its saved item
// at the end.

test.describe.configure({ mode: "serial" });
test.setTimeout(180000);

const baseURL = process.env.HCMNEXT_DEV_URL || "http://127.0.0.1:8080";
const marker = `zz-saved-${Math.random().toString(36).slice(2, 7)}`;
let desktopContext;
let desktop;
let storage;

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openSaved(page) {
  const mine = () =>
    page.locator(".chatsave-item", { hasText: `${marker} body` }).first();
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto("/workspace/app/chat?locale=en-US#saved");
    await page.reload();
    const found = await mine()
      .waitFor({ timeout: 40000 })
      .then(
        () => true,
        () => false,
      );
    if (found) return;
  }
  await expect(mine()).toBeVisible({ timeout: 1000 });
}

async function phonePage(browser, width, height) {
  const context = await browser.newContext({
    baseURL,
    viewport: { width, height },
    hasTouch: true,
    isMobile: true,
    storageState: storage,
  });
  return { context, page: await context.newPage() };
}

test.beforeAll(async ({ browser }) => {
  test.setTimeout(150000);
  desktopContext = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 900 },
  });
  desktop = await desktopContext.newPage();
  await signIn(desktop);
  const composerSel = "textarea[name='message']";
  await desktop.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(desktop.locator(composerSel)).toBeVisible({ timeout: 60000 });
  await desktop
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const dialog = desktop.locator(".create-dialog");
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await dialog.getByRole("textbox").first().fill(marker);
  await dialog.locator("button[type=submit]").click();
  await expect(desktop.locator(composerSel)).toHaveAttribute(
    "placeholder",
    new RegExp(marker),
    { timeout: 30000 },
  );
  await desktop.locator(composerSel).fill(marker + " body");
  await expect(desktop.locator(composerSel)).toHaveValue(marker + " body");
  await desktop.locator(composerSel).press("Enter");
  await desktop.waitForTimeout(3000);
  if (await desktop.locator(composerSel).inputValue()) {
    await desktop.locator(composerSel).press("Enter");
  }
  const message = desktop
    .locator(".message[data-message-id]", { hasText: `${marker} body` })
    .first();
  await expect(message).toBeVisible({ timeout: 30000 });
  await message.hover();
  await message.getByRole("button", { name: "Save for later" }).first().click();
  await expect(
    message.getByRole("button", { name: /Remove from Saved|Saved/ }).first(),
  ).toBeVisible({ timeout: 15000 });
  await desktop.waitForTimeout(4000);
  storage = await desktopContext.storageState();
});

test.afterAll(async () => {
  test.setTimeout(90000);
  try {
    await openSaved(desktop);
    await desktop
      .locator(".chatsave-item", { hasText: `${marker} body` })
      .first()
      .hover();
    const rm = desktop
      .locator(".chatsave-item", { hasText: `${marker} body` })
      .getByRole("button", { name: /Remove/ })
      .first();
    if (await rm.isVisible().catch(() => false)) await rm.click();
  } catch (error) {
    test
      .info()
      .annotations.push({ type: "cleanup", description: String(error).slice(0, 120) });
  }
  await desktopContext?.close();
});

const item = (page) =>
  page.locator(".chatsave-item", { hasText: `${marker} body` }).first();

test("#3933 Saved and its note editor say that saved items and notes are private", async () => {
  await openSaved(desktop);
  const privacy = desktop.locator("#chatsave-privacy");
  await expect(privacy).toBeVisible();
  await expect(privacy).toContainText(/only you can see/i);
  await expect(privacy).toContainText(/does not post a message or notify/i);
  await item(desktop).hover();
  await item(desktop)
    .getByRole("button", { name: /Add a note/ })
    .first()
    .click();
  const input = desktop.locator(".chatsave-note-input").first();
  await expect(input).toBeVisible();
  const line = desktop.locator(".chatsave-note-private").first();
  await expect(line).toBeVisible();
  await expect(line).toContainText(/not posted to the conversation/i);
  const describedby = await input.getAttribute("aria-describedby");
  expect(describedby).toContain(await line.getAttribute("id"));
  // A path back to the conversation exists and nothing was sent.
  await expect(
    item(desktop).getByRole("button", { name: "Open in the conversation" }),
  ).toHaveCount(1);
  await desktop
    .getByRole("button", { name: /^Cancel/ })
    .first()
    .click();
  await expect(desktop.locator(".chatsave-note-input")).toHaveCount(0);
});

async function customReminderFits(page) {
  await item(page).hover();
  await item(page)
    .getByRole("button", { name: /Remind me/ })
    .first()
    .click();
  const menu = page.locator(".chatsave-menu").first();
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: /Pick a date and time/ }).click();
  const input = page.locator(".chatsave-due-input").first();
  const submit = page.locator(".chatsave-pick button[type=submit]").first();
  await expect(input).toBeVisible();
  await expect(submit).toBeVisible();
  const vw = page.viewportSize().width;
  const ib = await input.boundingBox();
  const sb = await submit.boundingBox();
  const mb = await page.locator(".chatsave-pick").first().boundingBox();
  // Stacked: the button is below the input, not beside it.
  expect(sb.y).toBeGreaterThanOrEqual(ib.y + ib.height - 1);
  // The input fills the form width (time segments have room).
  expect(ib.width).toBeGreaterThanOrEqual(mb.width * 0.85);
  // Both are fully inside the viewport and inside the form.
  for (const b of [ib, sb]) {
    expect(b.x).toBeGreaterThanOrEqual(0);
    expect(b.x + b.width).toBeLessThanOrEqual(vw + 0.5);
    expect(b.x + b.width).toBeLessThanOrEqual(mb.x + mb.width + 1);
  }
  // No horizontal page scroll.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  // The Saved panel itself does not scroll sideways either.
  const panel = await page.evaluate(() => {
    let node = document.querySelector(".chatsave-content");
    while (node) {
      if (node.scrollWidth > node.clientWidth + 1) {
        const ox = window.getComputedStyle(node).overflowX;
        if (ox === "auto" || ox === "scroll") return node.className;
      }
      node = node.parentElement;
    }
    return null;
  });
  expect(panel).toBeNull();
}

test("#3912 custom reminder form fits the Saved panel at desktop width", async () => {
  await openSaved(desktop);
  await customReminderFits(desktop);
});

test("#3912 custom reminder form fits the Saved panel at 320px", async ({
  browser,
}) => {
  const { context, page } = await phonePage(browser, 320, 640);
  try {
    await openSaved(page);
    await customReminderFits(page);
  } finally {
    await context.close();
  }
});

test("#3911 note editor has touch-sized Save and Cancel at 390px; Cancel closes and returns focus", async ({
  browser,
}) => {
  const { context, page } = await phonePage(browser, 390, 844);
  try {
    await openSaved(page);
    await item(page)
      .getByRole("button", { name: /Add a note/ })
      .first()
      .tap();
    const input = page.locator(".chatsave-note-input").first();
    await expect(input).toBeVisible();
    const save = page.locator(".chatsave-note-form button[type=submit]").first();
    const cancel = page
      .locator(".chatsave-note-form [data-saved-action=note-cancel]")
      .first();
    await expect(save).toBeVisible();
    await expect(cancel).toBeVisible();
    await expect(save).toHaveText(/Save/);
    await expect(cancel).toHaveText(/Cancel/);
    for (const b of [save, cancel]) {
      const box = await b.boundingBox();
      expect(box.height).toBeGreaterThanOrEqual(44);
      expect(box.x + box.width).toBeLessThanOrEqual(390.5);
    }
    // Cancel discards and closes, focus lands on a control of that item.
    await input.pressSequentially(`${marker} discarded`);
    await cancel.tap();
    await expect(page.locator(".chatsave-note-input")).toHaveCount(0);
    await expect(item(page)).not.toContainText(`${marker} discarded`);
    // Cancelled from the keyboard, focus returns to the note control.
    await item(page)
      .getByRole("button", { name: /Add a note/ })
      .first()
      .tap();
    await page
      .locator(".chatsave-note-form [data-saved-action=note-cancel]")
      .first()
      .focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".chatsave-note-input")).toHaveCount(0);
    const focus = await page.evaluate(() => {
      const a = document.activeElement;
      return { id: a?.id || "", inItem: !!a?.closest?.(".chatsave-item") };
    });
    expect(focus.id).toMatch(/^chatsave-note-/);
    expect(focus.inItem).toBe(true);
    // Save stores the note.
    await item(page)
      .getByRole("button", { name: /Add a note/ })
      .first()
      .tap();
    await page.locator(".chatsave-note-input").first().fill(`${marker} kept`);
    await page.locator(".chatsave-note-form button[type=submit]").first().tap();
    await expect(page.locator(".chatsave-note-input")).toHaveCount(0);
    await expect(item(page)).toContainText(`${marker} kept`, { timeout: 15000 });
  } finally {
    await context.close();
  }
});
