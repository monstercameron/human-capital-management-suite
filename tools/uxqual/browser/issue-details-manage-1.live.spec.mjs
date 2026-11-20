import { test, expect } from "@playwright/test";

// Issues #3893, #3889, #3888: Conversation details member list tab order,
// and the wording of settings and joining rows for a private group.

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

// The seeded group may be missing from the shared demo, so each group test
// makes its own private group (zz-details-manage-...), and works only there.
async function makeGroup(page) {
  const name = `zz-details-manage-${Math.random().toString(36).slice(2, 7)}`;
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await page.waitForSelector(".message[data-message-id]", { timeout: 60000 });
  await page
    .locator("nav[aria-label='Chat navigation']")
    .getByRole("button", { name: "New conversation" })
    .first()
    .click();
  const dlg = page.locator(".create-dialog");
  await dlg.waitFor({ timeout: 30000 });
  await dlg.getByText("Group message").click();
  await dlg.locator("input[placeholder='Optional']").fill(name);
  const search = dlg.locator("input[placeholder='Search by name']");
  for (const person of ["Evelyn", "Darius"]) {
    await search.click();
    await search.pressSequentially(person);
    await expect(dlg.locator("[data-action='create-pick']").first()).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(dlg.locator("[data-action='create-pick']")).toHaveCount(0);
  }
  await dlg.locator("button[type='submit']").click();
  await expect(dlg).toBeHidden({ timeout: 30000 });
  return name;
}

async function openDetails(page, channel) {
  if (channel.group) {
    await makeGroup(page);
  } else {
    await page.goto(`/workspace/app/chat?locale=en-US#channel=${channel.hash}`);
    await page.reload();
  }
  const header = page.locator("[data-action='header-details']").first();
  await expect(header).toBeVisible({ timeout: 45000 });
  await header.click();
  const details = page.locator("aside.chat-details");
  await expect(details).toBeVisible({ timeout: 15000 });
  return details;
}

test("#3893 Show all keeps the button before the newly revealed members", async ({
  page,
}) => {
  await signIn(page);
  const details = await openDetails(page, { hash: "engineering" });
  const more = details.locator(".details-members-more");
  await more.scrollIntoViewIfNeeded();
  await expect(more).toHaveText(/Show all/);
  await more.focus();
  await page.keyboard.press("Enter");
  const fewer = details.locator(".details-members-more");
  await expect(fewer).toHaveText(/Show fewer/);
  // Focus may have been lost by the re-render; restore it as a user would.
  const kept = await page.evaluate(() =>
    document.activeElement?.classList.contains("details-members-more"),
  );
  if (!kept) await fewer.focus();
  await page.keyboard.press("Tab");
  const info = await page.evaluate(() => {
    const el = document.activeElement;
    const cont = document.querySelector("aside.chat-details");
    const buttons = [...cont.querySelectorAll("button")];
    const moreBtn = cont.querySelector(".details-members-more");
    return {
      inDetails: cont.contains(el),
      text: (el?.textContent || "").trim(),
      afterMore: moreBtn.compareDocumentPosition(el) & 4,
      inMembers: !!el.closest("[data-group='members'], .details-members, ul, li"),
      idx: buttons.indexOf(el) - buttons.indexOf(moreBtn),
    };
  });
  expect(info.inDetails).toBeTruthy();
  expect(info.text).not.toMatch(/Manage channel|Show fewer/);
  expect(info.afterMore).toBeTruthy();
  // The ninth person's row is the next focusable control.
  const rows = details
    .locator(".details-members-more")
    .locator("xpath=following::button[1]");
  const nextText = ((await rows.textContent()) || "").trim();
  expect(nextText.replace(/\s+/g, " ")).toBe(info.text.replace(/\s+/g, " "));
  expect(nextText).not.toMatch(/Manage/);
  await fewer.focus();
  await page.keyboard.press("Enter");
  await expect(details.locator(".details-members-more")).toHaveText(/Show all/);
});

test("#3889 private group settings are not called Manage channel", async ({ page }) => {
  await signIn(page);
  const details = await openDetails(page, { group: true });
  await expect(details).toContainText(/Private group/);
  await expect(details.getByText("Manage group", { exact: true })).toBeVisible();
  await expect(details.getByText("Manage channel")).toHaveCount(0);
});

test("#3888 private group joining row does not say anyone can join", async ({
  page,
}) => {
  await signIn(page);
  const details = await openDetails(page, { group: true });
  await expect(details).toContainText(/Private group/);
  await expect(details).toContainText("By invitation");
  await expect(details).not.toContainText("Anyone can join");
});
