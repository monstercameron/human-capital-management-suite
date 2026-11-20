import { test, expect } from "@playwright/test";

// Poll and to-do previews in the composer (#3905, #3861), against the live dev
// cell. Both tests work in their own channel so seeded conversations are not
// touched. Nothing is ever posted: every preview is cancelled.

test.setTimeout(180000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-poll-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  const field = page.locator("#new-chat-name");
  await field.fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function addVia(page, kind) {
  await page.getByRole("button", { name: "Add to your message" }).first().click();
  await page.locator(`.composer-add-menu [data-extra='${kind}']`).first().click();
  await expect(page.locator(".chatcmd003-preview")).toBeVisible({ timeout: 30000 });
}

test("#3905 cancelling a poll or to-do started from a typed message gives the message back", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const input = page.locator("textarea.composer-input").first();
  for (const [kind, prefix] of [
    ["poll", "/poll"],
    ["todo", "/todo"],
  ]) {
    const text = `Existing draft before ${kind} ${Date.now()}`;
    await input.fill(text);
    await addVia(page, kind);
    await expect(input).toHaveValue(new RegExp(`^${prefix}`));
    await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
    await expect(page.locator(".chatcmd003-preview")).toBeHidden();
    await expect(input).toHaveValue(text);
    // Escape cancels the same way.
    await addVia(page, kind);
    await input.focus();
    await page.keyboard.press("Escape");
    await expect(page.locator(".chatcmd003-preview")).toBeHidden();
    await expect(input).toHaveValue(text);
    await input.fill("");
  }
  // An empty composer still cancels to empty.
  await addVia(page, "poll");
  await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
  await expect(input).toHaveValue("");
});

test("#3861 a populated poll or to-do preview does not overlap the draft or toolbar", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1218, height: 943 });
  await signIn(page);
  await ownChannel(page);
  const input = page.locator("textarea.composer-input").first();
  const form = page.locator("form.chat-composer").first();
  const draft = [
    ["poll", "/poll Which time?\nMorning\nAfternoon\nEvening"],
    ["todo", "/todo Launch list\nWrite copy\nReview copy\nShip it"],
  ];
  for (const [kind, text] of draft) {
    await input.fill("");
    await addVia(page, kind);
    await input.focus();
    await input.fill(text);
    await expect(page.locator(".chatcmd003-preview")).toBeVisible();
    const boxes = async (sel) => page.locator(sel).first().boundingBox();
    const f = await form.boundingBox();
    const d = await boxes("form.chat-composer .composer-input");
    const preview = await boxes(".chatcmd003-preview");
    const tool = await boxes("form.chat-composer .composer-toolbar");
    const post = await boxes(".chatcmd003-preview .chatcmd003-actions");
    const vh = page.viewportSize().height;
    // Draft and toolbar are separate: no vertical overlap.
    expect(d.y + d.height).toBeLessThanOrEqual(tool.y + 1);
    // Everything sits inside the form and inside the window.
    for (const b of [d, tool, preview, post]) {
      expect(b.y).toBeGreaterThanOrEqual(f.y - 1);
      expect(b.y + b.height).toBeLessThanOrEqual(f.y + f.height + 1);
      expect(b.y + b.height).toBeLessThanOrEqual(vh + 1);
    }
    // Post and Cancel are reachable without scrolling the page.
    await expect(
      page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']"),
    ).toBeInViewport();
    await expect(page.locator("#chatcmd003-post")).toBeInViewport();
    // Preview and draft do not overlap.
    const overlap =
      Math.min(preview.y + preview.height, d.y + d.height) - Math.max(preview.y, d.y);
    expect(overlap).toBeLessThanOrEqual(1);
    await page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']").click();
    await expect(page.locator(".chatcmd003-preview")).toBeHidden();
  }
});
