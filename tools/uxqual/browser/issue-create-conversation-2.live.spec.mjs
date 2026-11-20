import { test, expect } from "@playwright/test";

// Live checks for the New conversation dialog: #3875 (name hint after a
// removed character), #3864 (focus after picking or removing a recipient),
// #3849 (no conversation type preselected).

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

const activeInfo = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    return {
      tag: a?.tagName,
      cls: a?.className || "",
      id: a?.id || "",
      inDialog: !!a?.closest?.(".create-dialog"),
    };
  });

test("#3849 the dialog opens with no type selected and no fields", async ({ page }) => {
  await signIn(page);
  const dialog = await openCreate(page);
  const cards = dialog.locator(".kind-card");
  await expect(cards).toHaveCount(4);
  for (const card of await cards.all()) {
    await expect(card).toHaveAttribute("aria-checked", "false");
    expect((await card.innerText()).trim().length).toBeGreaterThan(10);
  }
  await expect(dialog.locator("#new-chat-name")).toHaveCount(0);
  await expect(dialog.locator("#new-chat-member-search")).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "Start conversation" }),
  ).toBeDisabled();
  // Exactly one tab stop among the type cards.
  const stops = await cards.evaluateAll(
    (els) => els.filter((e) => e.tabIndex === 0).length,
  );
  expect(stops).toBe(1);
  await expect(dialog).toContainText(/Choose a type/);
  // Choosing a type shows its fields.
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await expect(dialog.locator("#new-chat-name")).toBeVisible();
  await expect(dialog.getByRole("radio", { name: /Public channel/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );
});

test("#3875 a removed character leaves the name valid and says so", async ({
  page,
}) => {
  await signIn(page);
  const dialog = await openCreate(page);
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  const name = dialog.locator("#new-chat-name");
  await name.click();
  await name.pressSequentially("zz-hello-worldx");
  await name.pressSequentially("!");
  await expect(name).toHaveValue("zz-hello-worldx");
  const hint = dialog.locator("#new-chat-name-hint");
  await expect(hint).toContainText(/removed/i);
  await expect(hint).not.toHaveClass(/field-error/);
  await expect(hint).toHaveAttribute("role", "status");
  const invalid = await name.getAttribute("aria-invalid");
  expect(invalid === null || invalid === "false").toBe(true);
  await expect(dialog.getByRole("button", { name: /Create channel/ })).toBeEnabled();
});

test("#3864 direct message pick keeps focus in the dialog and announces", async ({
  page,
}) => {
  await signIn(page);
  const dialog = await openCreate(page);
  await dialog.getByRole("radio", { name: /Direct message/ }).click();
  const search = dialog.locator("#new-chat-member-search");
  await expect(search).toBeVisible();
  await search.click();
  await search.pressSequentially("Darius");
  await expect(dialog.locator("[role=option]").first()).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(dialog.locator(".chip-remove")).toHaveCount(1);
  await expect
    .poll(async () => (await activeInfo(page)).inDialog, { timeout: 5000 })
    .toBe(true);
  const info = await activeInfo(page);
  expect(info.tag).not.toBe("BODY");
  expect(info.cls).toContain("chip-remove");
  await expect(dialog.locator("#new-chat-pick-status")).not.toBeEmpty();
});

test("#3864 group recipient removal returns focus to the search box", async ({
  page,
}) => {
  await signIn(page);
  const dialog = await openCreate(page);
  await dialog.getByRole("radio", { name: /Group message/ }).click();
  const search = dialog.locator("#new-chat-member-search");
  await expect(search).toBeVisible();
  await search.click();
  await search.pressSequentially("Darius");
  await expect(dialog.locator("[role=option]").first()).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(dialog.locator(".chip-remove")).toHaveCount(1);
  expect((await activeInfo(page)).inDialog).toBe(true);
  await dialog.locator(".chip-remove").first().focus();
  await page.keyboard.press("Enter");
  await expect(dialog.locator(".chip-remove")).toHaveCount(0);
  await expect
    .poll(async () => (await activeInfo(page)).id, { timeout: 5000 })
    .toBe("new-chat-member-search");
  // Escape still closes the dialog and returns focus to New conversation.
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  const back = await page.evaluate(
    () =>
      document.activeElement?.getAttribute("aria-label") ||
      document.activeElement?.textContent,
  );
  expect(back).toMatch(/New conversation/);
});
