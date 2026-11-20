import { test, expect } from "@playwright/test";

// Live checks for #3622 (mention footer matches Tab), #87 (empty desktop
// composer height) and #86 (coarse-pointer tool targets). Writes happen only
// in a private zz-composer channel; nothing is sent.

test.setTimeout(120000);

const composerSel = "textarea[name='message']";

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

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
    { timeout: 30000 },
  );
  return name;
}

async function openComposer(page, locale, channel) {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  const box = page.locator(composerSel);
  await expect(box).toBeVisible({ timeout: 60000 });
  return box;
}

test.describe("desktop fine pointer", () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test("#3622 the mention footer only promises Tab for details on an agent", async ({
    page,
  }) => {
    await signIn(page);
    const channel = await ownChannel(page);
    const box = await openComposer(page, "en-US", channel);
    const posts = [];
    page.on("request", (r) => {
      if (
        r.method() === "POST" &&
        /message/i.test(r.url()) &&
        !/locations|presence|typing|read/i.test(r.url())
      )
        posts.push(r.url());
    });
    await page
      .getByRole("button", { name: /Mention someone/i })
      .first()
      .click();
    const menu = page.locator(".mention-menu").first();
    await expect(menu).toBeVisible({ timeout: 15000 });
    const option = menu.getByRole("option").first();
    await expect(option).toBeVisible();
    const footer = (await menu.innerText()).replace(/\s+/g, " ");
    // Agents are off, so a person is highlighted: no Tab-for-details promise.
    expect(footer).toMatch(/Enter to insert/);
    expect(footer).not.toMatch(/Tab for details/);
    await box.focus();
    await page.keyboard.press("Tab");
    await expect(menu).toBeHidden();
    // Tab inserted the person.
    await expect.poll(async () => await box.inputValue()).toMatch(/@\S+/);
    expect(posts).toHaveLength(0);
    await box.fill("");
    // Escape closes without inserting.
    await page
      .getByRole("button", { name: /Mention someone/i })
      .first()
      .click();
    await expect(menu).toBeVisible();
    await box.focus();
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();
    expect(await box.inputValue()).not.toMatch(/@\S+/);
    expect(posts).toHaveLength(0);
  });

  test("#3622 de-DE and ar footers do not promise Tab for details either", async ({
    page,
  }) => {
    await signIn(page);
    const channel = await ownChannel(page);
    for (const locale of ["de-DE", "ar"]) {
      await openComposer(page, locale, channel);
      await page.locator(".composer-tools button").nth(1).waitFor({ timeout: 30000 });
      await page
        .locator(
          "[data-action='mention-open'], button[aria-label*='ention'], button[aria-label*='إشارة'], button[aria-label*='rwähn']",
        )
        .first()
        .click();
      const menu = page.locator(".mention-menu").first();
      await expect(menu).toBeVisible({ timeout: 15000 });
      const text = await menu.innerText();
      expect(text).not.toMatch(/Tab f/);
      expect(text).not.toMatch(/Tab ل/);
      expect(text).toMatch(/Enter/);
    }
  });

  test("#87 an empty desktop composer stays within 56-80 px and keeps Aa", async ({
    page,
  }) => {
    await signIn(page);
    const channel = await ownChannel(page);
    const box = await openComposer(page, "en-US", channel);
    const form = page.locator("form.composer, .composer").filter({ has: box }).first();
    await box.evaluate((el) => el.blur());
    const heightOf = async () => (await form.boundingBox()).height;
    const unfocused = await heightOf();
    await box.focus();
    const focused = await heightOf();
    expect(unfocused).toBeGreaterThanOrEqual(56);
    expect(unfocused).toBeLessThanOrEqual(80);
    expect(focused).toBeGreaterThanOrEqual(56);
    expect(focused).toBeLessThanOrEqual(80);
    // Aa is still there and opens the formatting row.
    const aa = page.getByRole("button", { name: /formatting/i }).first();
    await expect(aa).toBeVisible();
    await aa.click();
    await expect(page.getByRole("button", { name: "Bold", exact: true })).toBeVisible();
    // Typing text shows the row too.
    await aa.click();
    await box.focus();
    await box.pressSequentially("x");
    await expect(page.getByRole("button", { name: "Bold", exact: true })).toBeVisible();
    await box.fill("");
  });
});

test.describe("coarse pointer", () => {
  test.use({ viewport: { width: 1440, height: 900 }, hasTouch: true });

  test("#86 composer tools measure at least 44x44 under pointer:coarse", async ({
    page,
  }) => {
    await signIn(page);
    const channel = await ownChannel(page);
    await openComposer(page, "en-US", channel);
    const coarse = await page.evaluate(() => matchMedia("(pointer:coarse)").matches);
    test.skip(!coarse, "browser context does not report pointer:coarse");
    const names = [/^Add/i, /Mention someone/i, /Insert emoji/i, /GIF/i, /formatting/i];
    for (const name of names) {
      const b = page.getByRole("button", { name }).first();
      await expect(b).toBeVisible();
      const r = await b.boundingBox();
      expect(r.width, String(name)).toBeGreaterThanOrEqual(43.5);
      expect(r.height, String(name)).toBeGreaterThanOrEqual(43.5);
    }
  });
});

test.describe("fine pointer controls", () => {
  test.use({ viewport: { width: 1440, height: 900 } });
  test("#86 fine pointer tools stay compact (about 32 px)", async ({ page }) => {
    await signIn(page);
    const channel = await ownChannel(page);
    await openComposer(page, "en-US", channel);
    const b = page.getByRole("button", { name: /Mention someone/i }).first();
    await expect(b).toBeVisible();
    const r = await b.boundingBox();
    expect(r.height).toBeLessThanOrEqual(36);
  });
});
