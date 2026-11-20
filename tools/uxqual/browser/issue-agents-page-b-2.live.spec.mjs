import { test, expect } from "@playwright/test";

// Agents page, chunk 2: #3681 (no-answer runs), #3846 (shared Chat navigation
// state) and #3610 (Agent setup with no template and no agent). Runs against
// the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.setTimeout(120000);

async function signIn(page, name) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${name}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function shellClass(page, url) {
  await page.goto(url);
  await page.locator("#main-content").waitFor({ timeout: 60000 });
  await page.waitForFunction(
    () => {
      const shell = document.querySelector(".app-shell");
      return shell && !shell.classList.contains("is-content-loading");
    },
    null,
    { timeout: 60000 },
  );
  const classes = await page.locator(".app-shell").getAttribute("class");
  return {
    collapsed: classes.includes("nav-collapsed"),
    expanded: classes.includes("nav-expanded"),
  };
}

test("#3681 the Agents page never offers a no-answer placeholder as an answer", async ({
  page,
}) => {
  await signIn(page, "Rafael Torres");
  await page.goto("/workspace/app/chat/agents?locale=en-US");
  await page.locator("#main-content").waitFor({ timeout: 60000 });
  await expect(
    page.getByRole("heading", { name: "Agents", exact: true }).first(),
  ).toBeVisible({ timeout: 60000 });
  const main = page.locator("#main-content");
  // Agents are switched off in this demo, so the page says so instead of
  // accepting a question that could never be answered.
  await expect(main).toContainText("Agents are turned off for your organization");
  // The placeholder is never shown as an answer and nothing offers to copy it.
  await expect(main).not.toContainText("No answer was produced");
  await expect(main.getByRole("button", { name: /Copy answer/ })).toHaveCount(0);
  await expect(main.getByText(/^Completed/)).toHaveCount(0);
});

test("#3846 Conversations and Agents share one navigation state", async ({ page }) => {
  await signIn(page, "Linh Tran");
  const chat = "/workspace/app/chat?locale=en-US";
  const agents = "/workspace/app/chat/agents?locale=en-US";

  // Default: both Chat pages open the same way (the rail), so moving between
  // them does not shift the layout.
  const a = await shellClass(page, chat);
  const b = await shellClass(page, agents);
  expect(b).toEqual(a);
  expect(a.collapsed).toBe(true);

  // A person's own choice (the navigation toggle) is stored once and read by
  // both Chat pages. Page loads from an address do not store anything, so the
  // choice is made with the toggle itself.
  await shellClass(page, chat);
  await page.getByRole("link", { name: "Expand navigation" }).click();
  await page
    .locator(".app-shell.nav-expanded")
    .waitFor({ state: "attached", timeout: 30000 });
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.waitForTimeout(3000);

  // In-app move to Agents keeps it expanded; fresh loads of either page agree.
  const after = await shellClass(page, chat);
  expect(after.expanded).toBe(true);
  const back = await shellClass(page, agents);
  expect(back).toEqual(after);

  // Restore the person's default: collapse again with the toggle.
  await page.getByRole("link", { name: "Collapse navigation" }).click();
  await page
    .locator(".app-shell.nav-collapsed")
    .waitFor({ state: "attached", timeout: 30000 });
  await page.waitForTimeout(3000);
  expect((await shellClass(page, chat)).collapsed).toBe(true);
  expect((await shellClass(page, agents)).collapsed).toBe(true);
});

test("#3846 a no-answer task shows the muted no-answer mark, not a tick", async ({
  page,
}) => {
  test.skip(true, "Agents are switched off in the demo, so no task list is rendered");
  await signIn(page, "Rafael Torres");
});

for (const [locale, name] of [
  ["de-DE", "Agent setup in German"],
  ["en-US", "Agent setup in English"],
  ["ar", "Agent setup in Arabic"],
]) {
  test(`#3610 ${name} at 320px dark: no zero-agent guidance names a missing control`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      viewport: { width: 320, height: 740 },
      colorScheme: "dark",
      hasTouch: true,
      isMobile: true,
    });
    const page = await context.newPage();
    await signIn(page, "Rafael Torres");
    await page.goto(`/workspace/app/admin/personas?locale=${locale}&nav=collapsed`);
    const main = page.locator("#main-content");
    await expect(main.locator(".notice, [role='status'], p").first()).toBeVisible({
      timeout: 60000,
    });
    // Wait for the loaded page, not the skeleton.
    await expect(main.locator("select").first()).toBeVisible({ timeout: 60000 });
    const text = await main.innerText();
    const forbidden = {
      "en-US": [/choose new agent/i, /choose edit/i, /edit any agent/i, /on its card/i],
      "de-DE": [/Neuer Agent/, /Bearbeiten/],
      ar: [/وكيل جديد/, /تعديل/],
    }[locale];
    for (const pattern of forbidden) {
      expect(text, `must not mention ${pattern}`).not.toMatch(pattern);
    }
    // No New agent / Edit control exists either, so nothing points to one.
    await expect(
      main.getByRole("button", { name: /New agent|Neuer Agent|وكيل جديد/ }),
    ).toHaveCount(0);
    await expect(
      main.getByRole("link", { name: /New agent|Neuer Agent|وكيل جديد/ }),
    ).toHaveCount(0);
    // One notice, explaining that no template is available.
    if (locale === "en-US") {
      await expect(main).toContainText(
        "none can be created in this workspace right now",
      );
      await expect(main).toContainText("Nothing on this page adds one");
    }
    // Readable at 320px: nothing forces a horizontal scroll.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow).toBeLessThanOrEqual(1);
    await context.close();
  });
}
