import { test, expect } from "@playwright/test";

// Live checks for the Browse channels dialog: #3922 (settled result status),
// #3921 (destination-specific Open and Join names), #3885 (focus returns to the
// Channels menu trigger on close). Read-only: nothing is created or changed.

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

async function openBrowse(page) {
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 90000 });
  const plus = page.getByRole("button", { name: "Add or find channels" }).first();
  await plus.click();
  await page.locator("[data-action='open-browse']:visible").first().click();
  await expect(page.locator(".browse-list")).toBeVisible({ timeout: 20000 });
}

test("#3922 filter announces settled results in a polite status and keeps focus", async ({
  page,
}) => {
  await signIn(page);
  await openBrowse(page);
  const filter = page.locator("#browse-filter");
  const status = page.locator(".browse-status");
  await expect(status).toHaveAttribute("role", "status");
  await expect(status).toHaveAttribute("aria-live", "polite");
  await expect(status).toHaveAttribute("aria-atomic", "true");

  await filter.click();
  await page.keyboard.type("eng", { delay: 40 });
  // While typing the region is empty (no per-keystroke announcement).
  expect((await status.textContent()).trim()).toBe("");
  await expect(status).toHaveText(/Results: 1 channel\b/, { timeout: 5000 });
  expect(await page.evaluate(() => document.activeElement.id)).toBe("browse-filter");

  await filter.fill("");
  await page.keyboard.type("zzqqnomatch", { delay: 30 });
  await expect(status).toHaveText(/zzqqnomatch/, { timeout: 5000 });
  expect(await page.evaluate(() => document.activeElement.id)).toBe("browse-filter");

  await filter.fill("");
  await page.keyboard.type("a");
  await page.keyboard.press("Backspace");
  await expect(status).toHaveText(/Results: \d+ channels/, { timeout: 5000 });
  expect(await page.evaluate(() => document.activeElement.id)).toBe("browse-filter");
});

test("#3921 Open and Join buttons name their channel", async ({ page }) => {
  await signIn(page);
  await openBrowse(page);
  const report = await page.evaluate(() => {
    const out = [];
    for (const row of document.querySelectorAll(".browse-row")) {
      const name = row.querySelector(".browse-text strong")?.textContent.trim();
      const btn = row.querySelector(
        ".browse-actions > .button[data-action='browse-open'], .browse-actions > .button[data-action='join'], .browse-actions > .button[data-action^='join']",
      );
      if (!btn) continue;
      const desc = btn.getAttribute("aria-describedby");
      out.push({
        name,
        visible: btn.textContent.trim(),
        label: btn.getAttribute("aria-label"),
        describedBy: desc,
        descText: desc ? document.getElementById(desc)?.textContent.trim() : "",
      });
    }
    return out;
  });
  expect(report.length).toBeGreaterThan(1);
  for (const r of report) {
    expect(r.label, `button in ${r.name}`).toContain(r.name);
    expect(r.label.toLowerCase()).toMatch(/^(open|join|answer)/);
    expect(r.describedBy, `describedby for ${r.name}`).toBeTruthy();
    expect(r.descText.length).toBeGreaterThan(0);
  }
  expect(report.some((r) => /^Open /.test(r.label))).toBe(true);
  expect(report.some((r) => /^Join /.test(r.label))).toBe(true);
  const labels = report.map((r) => r.label);
  expect(new Set(labels).size).toBe(labels.length);
  // Visible labels stay short.
  for (const r of report) expect(r.visible.length).toBeLessThan(r.label.length);
});

test("#3885 closing Browse returns focus to the Channels menu trigger", async ({
  page,
}) => {
  await signIn(page);
  await openBrowse(page);
  const focusInfo = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      return {
        tag: a.tagName,
        label: a.getAttribute("aria-label") || a.textContent.trim().slice(0, 40),
        title: a.getAttribute("title") || "",
        inDialog: !!a.closest("dialog, .browse-dialog"),
      };
    });
  // Close button.
  await page.locator("[data-action='close-browse']").first().click();
  await expect(page.locator(".browse-dialog")).toHaveCount(0);
  let info = await focusInfo();
  expect(info.tag, JSON.stringify(info)).not.toBe("BODY");
  expect(info.label + info.title).toMatch(/Add or find channels/i);

  // Escape.
  await page.getByRole("button", { name: "Add or find channels" }).first().click();
  await page.locator("[data-action='open-browse']:visible").first().click();
  await expect(page.locator(".browse-list")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".browse-dialog")).toHaveCount(0);
  info = await focusInfo();
  expect(info.tag, JSON.stringify(info)).not.toBe("BODY");
  expect(info.label + info.title).toMatch(/Add or find channels/i);
});
