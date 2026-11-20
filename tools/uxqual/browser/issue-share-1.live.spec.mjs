import { test, expect } from "@playwright/test";

// Live checks for #3918 (share dialog keeps a checked destination while the
// filter hides it) and #3866 (closing the share dialog returns focus to the
// source message). Nothing is shared: the dialog is only opened and cancelled.

const channel = "/workspace/app/chat?locale=en-US#channel=engineering";
test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openShare(page) {
  await page.goto(channel);
  const message = page.locator(".message[data-message-id]").last();
  await expect(message).toBeVisible({ timeout: 45000 });
  const id = await message.getAttribute("data-message-id");
  await message.hover();
  await message.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: /Share to channel/i }).click();
  const dialog = page.locator(".share-dialog");
  await expect(dialog).toBeVisible();
  return { id, dialog };
}

async function activeInfo(page) {
  return page.evaluate(() => {
    const a = document.activeElement;
    return {
      tag: a && a.tagName,
      id: a && a.id,
      messageId:
        a && a.closest("[data-message-id]")
          ? a.closest("[data-message-id]").getAttribute("data-message-id")
          : "",
      action: a && a.getAttribute("data-action"),
      isComposer: !!(a && a.id === "chat-composer"),
    };
  });
}

test("#3918 a checked destination survives the filter, is shown and removable", async ({
  page,
}) => {
  await signIn(page);
  const { dialog } = await openShare(page);
  const submit = dialog.locator("[data-action='share-submit']");
  const choices = dialog.locator("[data-action='share-destination']");
  await expect(choices.first()).toBeVisible({ timeout: 30000 });
  await expect(submit).toBeDisabled();
  const pick = choices.first();
  const name = (await pick.locator("span:not([class])").first().innerText()).trim();
  await pick.click();
  await expect(pick).toHaveAttribute("aria-pressed", "true");
  await expect(submit).toBeEnabled();

  const filter = dialog.locator("#share-filter");
  await filter.fill("zzqqnomatch");
  await expect(choices).toHaveCount(0);
  // Still shown and removable, Share still enabled.
  await expect(dialog.locator(".share-selected")).toContainText(name);
  await expect(submit).toBeEnabled();
  const remove = dialog.locator("[data-action='share-clear']");
  await expect(remove).toBeVisible();

  await filter.fill("");
  await expect(
    dialog.locator("[data-action='share-destination'][aria-pressed='true']"),
  ).toHaveCount(1);

  await filter.fill("zzqqnomatch");
  await dialog.locator("[data-action='share-clear']").click();
  await expect(dialog.locator(".share-selected")).toHaveCount(0);
  await expect(submit).toBeDisabled();
  // Focus is not lost to the page body after Remove.
  expect((await activeInfo(page)).id).toBe("share-filter");
  await filter.fill("");
  await expect(choices.first()).toBeVisible();
  await expect(
    dialog.locator("[data-action='share-destination'][aria-pressed='true']"),
  ).toHaveCount(0);
});

for (const how of ["Escape", "Cancel"]) {
  test(`#3866 ${how} returns focus to the source message, not the composer`, async ({
    page,
  }) => {
    await signIn(page);
    const { id, dialog } = await openShare(page);
    const before = await page.evaluate(() => window.scrollY);
    if (how === "Escape") await page.keyboard.press("Escape");
    else await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog).toBeHidden();
    await expect
      .poll(async () => (await activeInfo(page)).isComposer, { timeout: 5000 })
      .toBe(false);
    const info = await activeInfo(page);
    expect(info.messageId).toBe(id);
    expect(info.tag).not.toBe("BODY");
    expect(Math.abs((await page.evaluate(() => window.scrollY)) - before)).toBeLessThan(
      5,
    );
  });
}
