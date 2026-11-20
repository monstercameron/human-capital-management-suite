import { test, expect } from "@playwright/test";

// Issue #3909: an unsaved channel purpose is kept per channel when the viewer
// switches conversations and returns. Nothing is saved implicitly.

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openDetails(page, hash) {
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${hash}`);
  const header = page.locator("[data-action='header-details']").first();
  await expect(header).toBeVisible({ timeout: 60000 });
  const details = page.locator("aside.chat-details");
  if (!(await details.isVisible())) await header.click();
  await expect(details).toBeVisible({ timeout: 15000 });
  return details;
}

test("#3909 an unsaved channel purpose survives switching channels and is not saved", async ({
  page,
}) => {
  await signIn(page);
  const details = await openDetails(page, "engineering");
  const before = (
    await details.locator(".details-purpose-value").first().innerText()
  ).trim();

  await details.locator("[data-action='purpose-edit']").click();
  const field = details
    .locator(
      "input[data-action][data-id='purpose'], #details-purpose-field, .details-purpose-form input",
    )
    .first();
  await expect(field).toBeVisible();
  const marker = `zz unsaved purpose ${Math.random().toString(36).slice(2, 7)}`;
  await field.fill(marker);
  await expect(
    details.locator(".details-purpose-form button[type='submit']"),
  ).toBeEnabled();

  // Switch to another channel through the sidebar, as a user would.
  const nav = page.locator("nav[aria-label='Chat navigation']");
  await nav.getByText("general", { exact: true }).first().click();
  await expect(page).toHaveURL(/#channel=general/);
  await page.waitForSelector(".message[data-message-id]", { timeout: 30000 });
  // The other channel does not inherit the editor or the text.
  const otherDetails = page.locator("aside.chat-details");
  if (await otherDetails.isVisible()) {
    await expect(otherDetails.locator(".details-purpose-form")).toHaveCount(0);
  }

  await nav.getByText("engineering", { exact: true }).first().click();
  await expect(page).toHaveURL(/#channel=engineering/);
  const back = page.locator("aside.chat-details");
  if (!(await back.isVisible())) {
    await page.locator("[data-action='header-details']").first().click();
  }
  const restored = page
    .locator("aside.chat-details .details-purpose-form input")
    .first();
  await expect(restored).toBeVisible({ timeout: 15000 });
  await expect(restored).toHaveValue(marker);
  await expect(
    page.locator("aside.chat-details .details-purpose-form button[type='submit']"),
  ).toBeEnabled();

  // Nothing was saved: Cancel returns to the original purpose.
  await page.locator("aside.chat-details [data-action='purpose-cancel']").click();
  await expect(page.locator("aside.chat-details .details-purpose-form")).toHaveCount(0);
  await expect(
    page.locator("aside.chat-details .details-purpose-value").first(),
  ).toHaveText(before);

  // A cancelled editor is not restored after another switch.
  await nav.getByText("general", { exact: true }).first().click();
  await expect(page).toHaveURL(/#channel=general/);
  await nav.getByText("engineering", { exact: true }).first().click();
  await expect(page).toHaveURL(/#channel=engineering/);
  await expect(page.locator("aside.chat-details .details-purpose-form")).toHaveCount(0);
});
