import { test, expect } from "@playwright/test";

// Saved lane, chunk 2: #3910 (note draft survives a filter change), #3879
// (Open in the conversation is a named button, not a focusable list item),
// #3878 (Show To do from an empty tab keeps keyboard focus). Runs against the
// dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default). Writes only to its
// own zz-saved channel and removes its saved item at the end.

test.describe.configure({ mode: "serial" });
test.setTimeout(180000);

const marker = `zz-saved-${Math.random().toString(36).slice(2, 7)}`;
let context;
let page;

async function openSaved() {
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

const tab = (name) => page.getByRole("tab", { name: new RegExp(`^${name}`) });

test.beforeAll(async ({ browser }) => {
  test.setTimeout(150000);
  context = await browser.newContext({
    baseURL: process.env.HCMNEXT_DEV_URL || "http://127.0.0.1:8080",
    viewport: { width: 1440, height: 900 },
  });
  page = await context.newPage();
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
  const composerSel = "textarea[name='message']";
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
  await expect(page.locator(composerSel)).toHaveValue(marker + " body");
  await page.locator(composerSel).press("Enter");
  await page.waitForTimeout(3000);
  if (await page.locator(composerSel).inputValue()) {
    await page.locator(composerSel).press("Enter");
  }
  const message = page
    .locator(".message[data-message-id]", { hasText: `${marker} body` })
    .first();
  await expect(message).toBeVisible({ timeout: 30000 });
  await message.hover();
  await message.getByRole("button", { name: "Save for later" }).first().click();
  await expect(
    message.getByRole("button", { name: /Remove from Saved|Saved/ }).first(),
  ).toBeVisible({ timeout: 15000 });
  // Let the save reach the server before any full page load cancels it.
  await page.waitForTimeout(4000);
});

test.afterAll(async () => {
  test.setTimeout(90000);
  try {
    await openSaved();
    await page
      .locator(".chatsave-item", { hasText: `${marker} body` })
      .first()
      .hover();
    const rm = page
      .locator(".chatsave-item", { hasText: `${marker} body` })
      .getByRole("button", { name: /Remove/ })
      .first();
    if (await rm.isVisible().catch(() => false)) await rm.click();
  } catch (error) {
    test
      .info()
      .annotations.push({ type: "cleanup", description: String(error).slice(0, 120) });
  }
  await context?.close();
});

test("#3910 a note being typed survives switching the Saved filter", async () => {
  await openSaved();
  const item = () =>
    page.locator(".chatsave-item", { hasText: `${marker} body` }).first();
  await item().hover();
  await item()
    .getByRole("button", { name: /Add a note/ })
    .first()
    .click();
  const input = page.locator(".chatsave-note-input").first();
  await expect(input).toBeVisible();
  await input.pressSequentially(`${marker} draft note`);
  await tab("All").click();
  // The item is still listed on All and its editor is still open with the text.
  await expect(item()).toBeVisible();
  await expect(page.locator(".chatsave-note-input").first()).toHaveValue(
    `${marker} draft note`,
  );
  // And back on To do, then Done (item hidden) and back again.
  await tab("Done").click();
  await tab("To do").click();
  await expect(page.locator(".chatsave-note-input").first()).toHaveValue(
    `${marker} draft note`,
  );
  // Escape / Cancel still discards.
  await page
    .getByRole("button", { name: /^Cancel/ })
    .first()
    .click();
  await expect(page.locator(".chatsave-note-input")).toHaveCount(0);
});

test("#3879 Saved open action is a named button, the row is not a focusable list item", async () => {
  await openSaved();
  const li = page.locator("li.chatsave-item", { hasText: `${marker} body` }).first();
  await expect(li).toBeVisible();
  expect(await li.getAttribute("tabindex")).toBeNull();
  expect(await li.getAttribute("title")).toBeNull();
  const open = li.getByRole("button", { name: "Open in the conversation" });
  await expect(open).toHaveCount(1);
  // Not nested inside another interactive control.
  expect(
    await open.evaluate((el) => {
      const hit = el.parentElement.closest("button, a, [role=button], [tabindex]");
      return hit && el.closest("li").contains(hit) ? hit.outerHTML.slice(0, 80) : null;
    }),
  ).toBeNull();
  // Keyboard reaches it and Enter opens the conversation.
  await open.focus();
  expect(await page.evaluate(() => document.activeElement?.className)).toContain(
    "chatsave-jump",
  );
  await page.keyboard.press("Enter");
  await expect(
    page.locator(".message[data-message-id]", { hasText: `${marker} body` }).first(),
  ).toBeVisible({ timeout: 30000 });
  // Other controls stay separate buttons.
  await openSaved();
  const names = await page
    .locator("li.chatsave-item", { hasText: `${marker} body` })
    .first()
    .getByRole("button")
    .allInnerTexts();
  expect(names.length).toBeGreaterThanOrEqual(4);
});

test("#3878 Show To do from an empty Done tab leaves focus on the To do tab", async () => {
  await openSaved();
  await tab("Done").click();
  const link = page.locator(".chatsave-empty [data-saved-tab]").first();
  await expect(link).toBeVisible();
  await link.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.locator(".chatsave-item", { hasText: `${marker} body` }),
  ).toBeVisible();
  const active = await page.evaluate(() => ({
    id: document.activeElement?.id,
    tag: document.activeElement?.tagName,
  }));
  expect(active.tag).not.toBe("BODY");
  expect(active.id).toBe("chatsave-tab-todo");
});
