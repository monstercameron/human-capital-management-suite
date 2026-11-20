import { test, expect } from "@playwright/test";

// Drafts, chunk 3 (#3886): while the draft is shown as read-only formatted
// text, the formatting buttons are disabled and say why; with Preview off they
// format the draft again. Work happens in the test's own zz-drafts channel.

test.setTimeout(240000);
test.use({ locale: "en-US", viewport: { width: 1440, height: 900 } });

const composerSel = "textarea.composer-input";

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Darius Bennett" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-drafts-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator(composerSel).first().waitFor({ timeout: 40000 });
  return name;
}

test("#3886 formatting buttons are disabled with a reason in Preview and work again with Preview off", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);
  const form = page.locator("form.chat-composer").first();
  const input = form.locator(composerSel);
  const draft = "**A sample draft** zz3886";
  await expect(async () => {
    await input.click();
    await input.fill(draft);
    await expect(input).toHaveValue(draft);
  }).toPass({ timeout: 60000 });

  const bold = form.locator(".composer-format-row .format-button").first();
  if (!(await bold.isVisible())) {
    await form.locator(".composer-format-toggle").click();
  }
  await expect(bold).toBeVisible();
  await expect(bold).toBeEnabled();

  await form.locator(".composer-preview-toggle").click();
  await expect(form.locator(".composer-preview-body")).toBeVisible();

  const buttons = form.locator(".composer-format-row .format-button");
  expect(await buttons.count()).toBeGreaterThan(1);
  for (const b of await buttons.all()) {
    await expect(b).toBeDisabled();
    const title = (await b.getAttribute("title")) || "";
    const label = (await b.getAttribute("aria-label")) || "";
    expect(title).toContain("Turn off Preview");
    expect(label).toContain("Turn off Preview");
  }

  // Preview off: enabled again, and Bold edits the draft.
  await form.locator(".composer-preview-toggle").click();
  await expect(form.locator(".composer-preview-body")).toBeHidden();
  await expect(bold).toBeEnabled();
  const before = await input.inputValue();
  await input.focus();
  await input.press("Control+A");
  await bold.click();
  await expect(input).not.toHaveValue(before);
  await input.fill("");
});
