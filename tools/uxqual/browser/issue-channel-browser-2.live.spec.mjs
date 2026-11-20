import { test, expect, devices } from "@playwright/test";

// Live checks for #3850 (Browse channels: Join visible, rows reflow on phones)
// and #3767 (a #channel=<name> link that matches two channels still opens one).
// Reads only, except one throw-away channel the #3767 test creates twice-named.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openBrowse(page) {
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
  if (page.viewportSize().width < 900) {
    await page.locator("[data-action='open-rail']").first().click();
  }
  await page.getByRole("button", { name: "Add or find channels" }).first().click();
  await page.locator("[data-action='open-browse']:visible").first().click();
  await expect(page.locator(".browse-list")).toBeVisible({ timeout: 20000 });
}

async function checkBrowse(page, phone) {
  await openBrowse(page);
  const rows = page.locator(".browse-row");
  expect(await rows.count()).toBeGreaterThan(0);
  const report = await page.evaluate(() => {
    const list = document.querySelector(".browse-list");
    const out = [];
    for (const row of document.querySelectorAll(".browse-row")) {
      const text = row.querySelector(".browse-text strong");
      const btn = row.querySelector(".browse-actions > .button");
      out.push({
        name: text ? text.textContent : "",
        truncated: text ? text.scrollWidth > text.clientWidth + 1 : false,
        action: btn ? btn.textContent.trim() : "",
        opacity: btn ? window.getComputedStyle(btn).opacity : null,
        h: btn ? btn.getBoundingClientRect().height : 0,
      });
    }
    return { rows: out, client: list.clientWidth, scroll: list.scrollWidth };
  });
  expect(report.scroll).toBeLessThanOrEqual(report.client + 1);
  const joins = report.rows.filter((r) => /^join/i.test(r.action));
  expect(
    joins.length,
    "an unjoined public channel with a visible Join",
  ).toBeGreaterThan(0);
  for (const r of report.rows) {
    if (r.opacity !== null) expect(Number(r.opacity)).toBe(1);
    expect(r.truncated, `name ${r.name} truncated`).toBe(false);
    if (phone && r.opacity !== null) expect(r.h).toBeGreaterThanOrEqual(43.5);
  }
  return joins;
}

test.describe("desktop", () => {
  test.use({ viewport: { width: 1440, height: 900 } });
  test("#3850 Join is visible without hover and names are not truncated", async ({
    page,
  }) => {
    await signIn(page);
    await checkBrowse(page, false);
  });
});

test.describe("phone", () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    userAgent: devices["Pixel 7"].userAgent,
  });
  test("#3850 phone: rows reflow, no sideways scroll, 44px actions", async ({
    page,
  }) => {
    await signIn(page);
    await checkBrowse(page, true);
  });
});

test.describe("deep link", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });
  test("#3767 a name shared by two channels opens one, not 'no longer available'", async ({
    page,
  }) => {
    await signIn(page);
    const name = `zz-channel-browser-${Math.random().toString(36).slice(2, 8)}`;
    await page.goto("/workspace/app/chat?locale=en-US");
    await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
    for (let i = 0; i < 2; i++) {
      const drawer = page.locator("[data-action='open-rail']").first();
      if (await drawer.isVisible()) await drawer.click();
      await page.getByRole("button", { name: "New conversation" }).first().click();
      const dialog = page.locator(".create-dialog");
      await expect(dialog).toBeVisible();
      await dialog
        .getByRole("radio", { name: i === 0 ? /^Public channel/ : /^Private channel/ })
        .click();
      await dialog.locator("#new-chat-name").fill(name);
      await dialog.locator("button[type='submit']").click();
      await dialog.waitFor({ state: "hidden", timeout: 15000 }).catch(async () => {
        /* the explicit hidden check below reports it */
      });
      await expect(dialog).toBeHidden({ timeout: 5000 });
    }
    // Fresh load by name, as a pasted link would.
    await page.goto(`/workspace/app/chat?locale=en-US#channel=${name}`);
    await page.reload();
    await page.waitForTimeout(500);
    const composer = page.getByRole("textbox").last();
    await expect(composer).toBeVisible({ timeout: 90000 });
    await expect(page.locator(".chat-workspace")).not.toContainText(
      /no longer available/i,
    );
    await expect(composer).toBeEnabled();
    expect(await page.locator(".chat-workspace").innerText()).toContain(name);
  });
});
