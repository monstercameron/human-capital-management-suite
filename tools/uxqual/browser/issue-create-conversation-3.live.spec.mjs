import { test, expect } from "@playwright/test";

// Live check for #3865: changing the conversation type in New conversation
// keeps each type's draft details and restores them on return.

const chat = "/workspace/app/chat?locale=en-US#channel=general";
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openCreate(page) {
  await page.goto(chat);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  const drawer = page.getByRole("button", { name: "Open conversations" });
  if (await drawer.isVisible()) await drawer.click();
  await page.getByRole("button", { name: "New conversation" }).first().click();
  const dialog = page.locator(".create-dialog");
  await expect(dialog).toBeVisible();
  return dialog;
}

async function pick(dialog, query) {
  const search = dialog.locator("#new-chat-member-search");
  await search.click();
  await search.fill("");
  await search.pressSequentially(query);
  const option = dialog
    .locator("[data-action='create-pick']")
    .filter({ hasText: query })
    .first();
  await expect(option).toBeVisible();
  const before = await chips(dialog).count();
  await option.click();
  await expect(chips(dialog)).toHaveCount(before + 1);
}

const chips = (dialog) => dialog.locator(".person-chip");

test("#3865 type changes keep group recipients and name", async ({ page }) => {
  await signIn(page);
  const dialog = await openCreate(page);
  await dialog.getByRole("radio", { name: /Group message/ }).click();
  await pick(dialog, "Darius");
  await pick(dialog, "Evelyn");
  await expect(chips(dialog)).toHaveCount(2);
  const name = dialog.locator("#new-chat-name");
  await name.click();
  await name.pressSequentially("planning draft zz3865");
  await expect(name).toHaveValue("planning draft zz3865");
  const start = dialog.getByRole("button", { name: "Start conversation" });
  await expect(start).toBeEnabled();

  // Direct message: at most one recipient.
  await dialog.getByRole("radio", { name: /Direct message/ }).click();
  await expect(chips(dialog)).toHaveCount(1);
  expect(
    (await dialog.locator("#new-chat-members").inputValue()).split(",").filter(Boolean),
  ).toHaveLength(1);

  // Back to Group: both recipients and the name are restored.
  await dialog.getByRole("radio", { name: /Group message/ }).click();
  await expect(chips(dialog)).toHaveCount(2);
  await expect(dialog.locator("#new-chat-name")).toHaveValue("planning draft zz3865");
  await expect(start).toBeEnabled();

  // Public channel: the private group name must not leak into it.
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await expect(dialog.locator("#new-chat-name")).toHaveValue("");

  // Back to Group again: name and both recipients return.
  await dialog.getByRole("radio", { name: /Group message/ }).click();
  await expect(dialog.locator("#new-chat-name")).toHaveValue("planning draft zz3865");
  await expect(chips(dialog)).toHaveCount(2);
  await expect(start).toBeEnabled();
});
