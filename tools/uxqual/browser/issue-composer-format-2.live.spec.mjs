import { test, expect } from "@playwright/test";

// Live checks for #3902 (undo of composer controls), #3715 (over-long body is
// explained) and #3677 (Arabic placeholder and hint keep bidi order). Writes
// happen only in a private zz-composer channel; nothing is sent in the others.

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

const composerSel = "textarea[name='message']";

// Creates a private zz-composer channel so no seeded conversation is touched.
async function ownChannel(page) {
  const name = `zz-composer-${Math.random().toString(36).slice(2, 7)}`;
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(composerSel)).toBeVisible({ timeout: 60000 });
  await page
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const dialog = page.locator(".create-dialog");
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await dialog.getByRole("textbox").first().fill(name);
  await dialog.locator("button[type=submit]").click();
  await expect(page.locator(composerSel)).toHaveAttribute(
    "placeholder",
    new RegExp(name),
    {
      timeout: 30000,
    },
  );
  return name;
}

async function openComposer(page, locale, channel) {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  const box = page.locator(composerSel);
  await expect(box).toBeVisible({ timeout: 60000 });
  return box;
}

test("#3902 Ctrl+Z reverses Bold and an inserted emoji", async ({ page }) => {
  await signIn(page);
  const channel = await ownChannel(page);
  const box = await openComposer(page, "en-US", channel);
  await box.click();
  await box.pressSequentially("Please review this draft");
  const original = await box.inputValue();
  // Select "review".
  await box.evaluate((el) => {
    el.focus();
    el.setSelectionRange(7, 13);
  });
  await page.getByRole("button", { name: "Bold", exact: true }).click();
  await expect(box).toHaveValue("Please **review** this draft");
  await box.focus();
  await page.keyboard.press("Control+z");
  await expect(box).toHaveValue(original);

  // Emoji
  await box.evaluate((el) => {
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  });
  const before = await box.inputValue();
  await page.getByRole("button", { name: "Insert emoji" }).first().click();
  await page
    .getByPlaceholder(/search emoji/i)
    .first()
    .fill("pizza");
  await page.waitForTimeout(500);
  await page.keyboard.press("Enter");
  await expect
    .poll(async () => (await box.inputValue()).length)
    .toBeGreaterThan(before.length);
  await box.focus();
  await page.keyboard.press("Control+z");
  await expect(box).toHaveValue(before);
});

test("#3715 a body over 4000 characters is explained, not silent", async ({ page }) => {
  await signIn(page);
  const channel = await ownChannel(page);
  const box = await openComposer(page, "en-US", channel);
  const posts = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && /chat/i.test(r.url())) posts.push(r.url());
  });
  await box.click();
  await box.evaluate((el) => {
    el.value = "L".repeat(4500);
    el.dispatchEvent(new window.Event("input", { bubbles: true }));
  });
  await box.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText(/4000/).first()).toBeVisible({ timeout: 10000 });
  expect((await box.inputValue()).length).toBe(4500);
  // Background location polling also POSTs to a "message" path; only a real
  // message post counts.
  expect(posts.filter((u) => !/locations|presence|typing|read/i.test(u))).toHaveLength(
    0,
  );
  // Typing clears it.
  await box.pressSequentially("x");
  await expect(page.getByText(/limit is 4000/)).toBeHidden();
  await box.evaluate((el) => {
    el.value = "";
    el.dispatchEvent(new window.Event("input", { bubbles: true }));
  });
});

test("#3677 Arabic placeholder and key hint are bidi-isolated", async ({ page }) => {
  await signIn(page);
  const channel = await ownChannel(page);
  const box = await openComposer(page, "ar", channel);
  const ph = await box.getAttribute("placeholder");
  expect(ph).toContain(`#${channel}`);
  expect(ph).toContain(`\u2068#${channel}\u2069`);
  const hint = page.locator("bdi[dir='ltr']").filter({ hasText: /Enter/ });
  expect(await hint.count()).toBeGreaterThanOrEqual(2);
  await expect(hint.first()).toBeVisible();
});
