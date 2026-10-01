// AGENTP-018 served persona-admin qualification.
//
// Run against the local fake cell with:
//   HCMNEXT_DEV_URL=http://127.0.0.1:8288 npx playwright test \
//     --config=tools/uxqual/browser/playwright.config.mjs \
//     tools/uxqual/persona_admin_browser/persona_admin_browser.spec.mjs
//
// The page is read-only: it never publishes, suspends, retires, or calls a
// model provider. A missing served catalogue is deliberately reported as a
// RED contract failure instead of being mistaken for an empty catalogue.
import { test, expect } from "@playwright/test";

const ADMIN_ROUTE = "/workspace/app/admin/personas";
const WIDTHS = [1280, 1920, 390, 320];
const LOCALES = [
  { value: "en-US", dir: "ltr", title: "Personas", starter: "Starter", personaId: "Persona ID", handle: "Handle", purpose: "Purpose", instructions: "Instructions", channels: "Allowed channels" },
  { value: "de-DE", dir: "ltr", title: "Personas", starter: "Vorlage", personaId: "Persona-ID", handle: "Handle", purpose: "Zweck", instructions: "Anweisungen", channels: "Zulässige Kanäle" },
  { value: "ar", dir: "rtl", title: "الشخصيات", starter: "القالب", personaId: "معرّف الشخصية", handle: "المعرّف", purpose: "الغرض", instructions: "التعليمات", channels: "القنوات المسموح بها" },
];

async function signIn(page, persona) {
  await page.goto("/workspace/login?company=ironridge-demo");
  const quickPick = {
    admin: "Continue as Walt Brennan",
    "individual-contributor": "Continue as Ana Flores",
  }[persona];
  const button = page.getByRole("button", { name: quickPick, exact: true });
  await expect(button, `no local fake login for ${persona}`).toBeVisible();
  await button.click();
  await page.waitForURL(/\/workspace\/app\//, { timeout: 30000 });
  await page.waitForLoadState("networkidle");
}

async function expectServedCatalog(page, where) {
  const catalog = page.locator(".persona-admin-page");
  const state = await catalog.getAttribute("data-persona-admin-state").catch(() => null);
  if (!(await catalog.count()) || state === "unavailable") {
    const body = (await page.locator("body").innerText().catch(() => ""))
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 240);
    throw new Error(
      `RED AGENTP-018 served catalog missing at ${where} (${page.url()}): ` +
        `expected .persona-admin-page with catalog projection; body=${JSON.stringify(body)}`,
    );
  }
  return catalog;
}

async function assertCatalog(page, locale, width) {
  const where = `${locale.value} ${width}px`;
  const catalog = await expectServedCatalog(page, where);
  await expect(catalog.locator("#persona-admin-title")).toHaveText(locale.title);
  await expect(catalog.locator("[data-lifecycle]").first()).toBeVisible();
  await expect(catalog.locator("[data-lifecycle='PUBLISHED']")).toHaveCount(1);
  await expect(catalog.getByText("Owner", { exact: true })).toBeVisible();
  await expect(catalog.getByText("Read compensation", { exact: true })).toBeVisible();
  await expect(catalog.getByText("COMPENSATION", { exact: true })).toBeVisible();
  await expect(catalog.getByText("#hr-ops", { exact: true })).toBeVisible();
  await expect(catalog.getByText("Effective access preview", { exact: true })).toBeVisible();
  await expect(catalog.getByRole("button", { name: "Publish", exact: true })).toBeVisible();
  await expect(catalog.getByRole("button", { name: "Suspend", exact: true })).toBeVisible();
  await expect(catalog.locator("[data-review-approved='false']").getByRole("button", { name: "Publish", exact: true })).toBeDisabled();

  const editor = page.locator("#persona-admin-create-form");
  await expect(editor).toBeVisible();
  await expect(editor.getByLabel(locale.starter)).toBeVisible();
  await expect(editor.getByLabel(locale.personaId)).toBeVisible();
  await expect(editor.getByLabel(locale.handle)).toHaveAttribute("readonly");
  await expect(editor.getByLabel(locale.purpose)).toHaveAttribute("readonly");
  await expect(editor.getByLabel(locale.instructions)).toHaveAttribute("readonly");
  await expect(editor.getByRole("group", { name: locale.channels })).toBeVisible();
  await expect(editor.locator("#persona-admin-channel-external")).toBeDisabled();
  const starter = editor.locator("#persona-admin-starter");
  const availableStarterOptions = starter.locator("option:not([disabled])");
  if (await availableStarterOptions.count()) {
    const starterId = await availableStarterOptions.first().getAttribute("value");
    await starter.selectOption(starterId);
    await expect(editor.locator("#persona-admin-manifest")).toHaveAttribute("readonly");
    await expect(editor.locator("#persona-admin-create-submit")).toBeEnabled();
  } else {
    await expect(starter).toBeDisabled();
    await expect(editor.locator("#persona-admin-create-submit")).toBeDisabled();
    await expect(editor.locator("#persona-admin-editor-status")).toContainText(/starter|template|Vorlage|قالب/i);
  }

  const nameLayout = await page.evaluate(() => {
    const label = document.querySelector('label[for="persona-admin-name"]');
    const input = document.querySelector("#persona-admin-name");
    if (!label || !input) return null;
    return {labelBottom: label.getBoundingClientRect().bottom, inputTop: input.getBoundingClientRect().top, inputWidth: input.getBoundingClientRect().width};
  });
  expect(nameLayout, `${where}: name label and control are rendered`).not.toBeNull();
  expect(nameLayout.inputTop, `${where}: label/control spacing`).toBeGreaterThan(nameLayout.labelBottom + 2);
  expect(nameLayout.inputWidth, `${where}: readable name input width`).toBeGreaterThan(200);

  const facts = await page.evaluate(() => ({
    lang: document.documentElement.lang,
    dir: document.documentElement.dir,
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
    taskContent: [...document.querySelectorAll(".persona-admin-page")]
      .some((node) => /task-secret|prompt/i.test(node.textContent ?? "")),
  }));
  expect(facts.lang, `${where}: lang`).toBe(locale.value);
  expect(facts.dir, `${where}: dir`).toBe(locale.dir);
  expect(facts.scrollWidth, `${where}: horizontal overflow`).toBeLessThanOrEqual(facts.innerWidth + 1);
  expect(facts.taskContent, `${where}: task content leaked`).toBe(false);
}

test.describe("AGENTP-018 persona admin served boundary", () => {
  test("admin sees lifecycle, reach, placements and explicit controls", async ({ page }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width: 1280, height: 900 });
    await signIn(page, "admin");
    await page.goto(`${ADMIN_ROUTE}?locale=en-US`);
    await page.waitForLoadState("networkidle");
    await assertCatalog(page, LOCALES[0], 1280);
  });

  test("non-admin cannot discover the persona catalogue", async ({ page }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width: 1280, height: 900 });
    await signIn(page, "individual-contributor");
    await page.goto(`${ADMIN_ROUTE}?locale=en-US`);
    await page.waitForLoadState("networkidle");
    const body = await page.locator("body").innerText();
    expect(body).not.toContain("Comp Analyst");
    expect(body).not.toContain("COMPENSATION");
    expect(body).not.toContain("Publish");
  });

  for (const width of WIDTHS) {
    for (const locale of LOCALES) {
      test(`admin ${locale.value} at ${width}px`, async ({ page }) => {
        test.setTimeout(90000);
        await page.setViewportSize({ width, height: 900 });
        await signIn(page, "admin");
        await page.goto(`${ADMIN_ROUTE}?locale=${locale.value}`);
        await page.waitForLoadState("networkidle");
        await assertCatalog(page, locale, width);
      });
    }
  }

  test("catalog controls are keyboard reachable", async ({ page }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width: 1280, height: 900 });
    await signIn(page, "admin");
    await page.goto(`${ADMIN_ROUTE}?locale=en-US`);
    await page.waitForLoadState("networkidle");
    const catalog = await expectServedCatalog(page, "keyboard");
    const controls = catalog.locator("select, button, input:not([type='hidden']), textarea");
    expect(await controls.count(), "catalog needs keyboard controls").toBeGreaterThan(0);
    for (const control of await controls.all()) {
      await control.focus();
      expect(await control.evaluate((node) => document.activeElement === node)).toBe(true);
    }
  });
});
