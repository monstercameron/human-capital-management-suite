import { test, expect } from "@playwright/test";

// Chat issue sweep, lane thread-a chunk 2: #3901 (Also-send choice per
// thread), #3904 (thread draft survives channel switch and search), #3895
// (thread formatting survives Preview and collapsing Formatting).
// All writes go to a private channel named zz-thread-a-<random>. No reply is sent.

test.setTimeout(240000);

const rand = () => Math.random().toString(36).slice(2, 7);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function makeChannel(page, name) {
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
}

async function post(page, text) {
  const composer = page.locator("form.chat-composer textarea.composer-input").first();
  await expect(composer).toBeEnabled({ timeout: 60000 });
  await composer.click();
  await composer.fill(text);
  await page.keyboard.press("Enter");
  await expect(
    page.locator(".message[data-message-id]", { hasText: text }).first(),
  ).toBeVisible({ timeout: 30000 });
}

async function openThreadOf(page, marker) {
  const message = page
    .locator(".message[data-message-id]", { hasText: marker })
    .first();
  await expect(message).toBeVisible({ timeout: 30000 });
  const root = page.locator(".thread-scroll, .thread-root").first();
  await expect(async () => {
    await message.hover();
    await message.locator(".message-body").first().hover();
    await message
      .getByRole("button", { name: "Reply in thread" })
      .first()
      .click({ force: true, timeout: 5000 });
    await expect(root).toContainText(marker, { timeout: 6000 });
  }).toPass({ timeout: 60000 });
  await expect(page.locator("form.thread-composer #thread-composer")).toBeVisible({
    timeout: 15000,
  });
  // The thread pane has settled on this root (the form is redrawn after the root).
  await page.waitForTimeout(1500);
}

async function setup(page, tag) {
  await signIn(page);
  const name = `zz-thread-a-${rand()}`;
  await makeChannel(page, name);
  await post(page, `${name} ${tag} rootA`);
  await post(page, `${name} ${tag} rootB`);
  return name;
}

const also = (page) => page.locator("#thread-also-channel");
const box = (page) => page.locator("form.thread-composer #thread-composer");

test("#3901 Also-send choice belongs to its own thread, returns with the draft and survives an Agents round trip", async ({
  page,
}) => {
  const name = await setup(page, "t3901");
  await openThreadOf(page, `${name} t3901 rootA`);
  await box(page).click();
  await box(page).fill(`${name} draftA`);
  await also(page).check();
  await expect.soft(also(page), "also checked").toBeChecked();

  // Another thread in the same channel, pane left open: unchecked.
  await openThreadOf(page, `${name} t3901 rootB`);
  await expect.soft(also(page), "also unchecked").not.toBeChecked();

  // Back to the first thread: draft and choice return.
  await openThreadOf(page, `${name} t3901 rootA`);
  await expect.soft(box(page), "draft").toHaveValue(`${name} draftA`);
  await expect.soft(also(page), "also checked").toBeChecked();

  // Close and reopen the same thread.
  await page
    .getByRole("button", { name: /close thread/i })
    .first()
    .click();
  await expect(page.locator("form.thread-composer")).toHaveCount(0);
  await openThreadOf(page, `${name} t3901 rootA`);
  await expect.soft(box(page), "draft").toHaveValue(`${name} draftA`);
  await expect.soft(also(page), "also checked").toBeChecked();

  // Agents round trip through the breadcrumb.
  await page.locator("a[href^='/workspace/app/chat/agents']").first().click();
  await page.waitForURL(/chat\/agents/);
  await page.getByRole("link", { name: "Conversations", exact: true }).first().click();
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  await expect(box(page)).toBeVisible({ timeout: 60000 });
  await expect.soft(box(page), "draft").toHaveValue(`${name} draftA`);
  await expect(also(page)).toBeChecked();
});

test("#3904 an unsent thread reply survives switching channel and a search", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 1000 });
  const name = await setup(page, "t3904");
  await openThreadOf(page, `${name} t3904 rootA`);
  await box(page).click();
  await box(page).fill(`${name} unsent reply`);

  // The conversation list is collapsed while a thread is open, so switch the
  // way a shared link does: by the address of the other conversation.
  const rail = (label) => ({
    click: () =>
      page.evaluate((l) => {
        window.location.hash = "channel=" + l;
      }, label),
  });
  const pause = () => page.waitForTimeout(1500);
  await pause();

  // Switch to another conversation in the list, then come back (no reload).
  await rail("general").click();
  await expect(page).toHaveURL(/channel=general/, { timeout: 30000 });
  await pause();
  await rail(name).click();
  await expect(page).toHaveURL(new RegExp(`channel=${name}`), { timeout: 30000 });
  await openThreadOf(page, `${name} t3904 rootA`);
  await expect(box(page), "draft after channel switch").toHaveValue(
    `${name} unsent reply`,
  );

  // Submit a chat search with the thread open, then come back.
  const search = page.locator("#chat-search");
  await search.fill("zzzz-no-such-text");
  await search.press("Enter");
  await pause();
  await rail("general").click();
  await pause();
  await rail(name).click();
  await expect(page).toHaveURL(new RegExp(`channel=${name}`), { timeout: 30000 });
  await openThreadOf(page, `${name} t3904 rootA`);
  await expect(box(page), "draft after search").toHaveValue(`${name} unsent reply`);
});

test("#3895 thread formatting survives Preview and collapsing Formatting; fast edit then Preview shows new text", async ({
  page,
}) => {
  const name = await setup(page, "t3895");
  await openThreadOf(page, `${name} t3895 rootA`);
  const form = page.locator("form.thread-composer");
  await box(page).click();
  await box(page).fill("Review draft");
  await box(page).press("Control+A");
  await form
    .getByRole("button", { name: /formatting/i })
    .first()
    .click();
  await form.getByRole("button", { name: /^bold/i }).first().click();
  await expect(box(page)).toHaveValue("**Review draft**");

  const toggle = form.locator(".composer-preview-toggle").first();
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await expect(form).toContainText("Review draft");
  await expect(form.locator("strong", { hasText: "Review draft" })).toBeVisible();
  await page.locator("form.thread-composer .composer-preview-toggle").first().click();
  await expect(box(page)).toHaveValue("**Review draft**");

  // Collapse the Formatting toolbar.
  await form
    .getByRole("button", { name: /formatting/i })
    .first()
    .click();
  await expect(box(page)).toHaveValue("**Review draft**");

  // Rapid edit then Preview.
  await box(page).fill("Temporary QA: check the revised deadline");
  await page.locator("form.thread-composer .composer-preview-toggle").first().click();
  await expect(form).toContainText("check the revised deadline");
  await expect(form).not.toContainText("Review draft");
  await page.locator("form.thread-composer .composer-preview-toggle").first().click();
  await expect(box(page)).toHaveValue("Temporary QA: check the revised deadline");
});
