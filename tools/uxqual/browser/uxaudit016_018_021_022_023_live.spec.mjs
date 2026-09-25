// UXAUDIT-016/018/021/022/023 real-browser acceptance against the live
// development cell. These checks intentionally use the authenticated served
// application; productui render tests cannot establish route composition,
// computed layout, real keyboard focus, or the effective server data path.
import { test, expect } from "@playwright/test";

async function signInAsAdmin(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

test.describe("UXAUDIT live experience acceptance", () => {
  test.beforeEach(async ({ page }) => {
    await signInAsAdmin(page);
  });

  test("TestTodo_UXAUDIT_016_Browser authorized profile identity is consistent", async ({
    page,
  }) => {
    await page.goto("/workspace/app/people");
    const personLink = page.locator('a[href^="/workspace/app/person?"]').first();
    await expect(personLink).toBeVisible();
    await personLink.click();
    await expect(page).toHaveURL(/\/workspace\/app\/person\?/);

    const identityHeading = page
      .locator("main :is(h1,h2,h3)")
      .filter({ hasText: /HC-\d+/ })
      .first();
    await expect(identityHeading).toBeVisible();
    const name = (await identityHeading.innerText()).trim();
    expect(
      name,
      "the profile heading must include the authorized worker identity",
    ).toMatch(/HC-\d+/);
    await expect(page).toHaveTitle(
      new RegExp(name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    );
  });

  test("TestTodo_UXAUDIT_016_Accessibility profile field states have accessible names", async ({
    page,
  }) => {
    await page.goto("/workspace/app/people");
    await page.locator('a[href^="/workspace/app/person?"]').first().click();
    const main = page.locator("main");
    await expect(main.locator("h1")).toBeVisible();
    const states = main.locator(".profile-fact-status");
    expect(
      await states.count(),
      "missing or withheld facts must identify their state",
    ).toBeGreaterThan(0);
    for (const state of await states.all()) await expect(state).toBeVisible();
  });

  test("TestTodo_UXAUDIT_018_Browser Insights shows sourced scope and a useful empty state", async ({
    page,
  }) => {
    await page.goto("/workspace/app/insights");
    await expect(page.locator("main h1")).toBeVisible();
    const main = page.locator("main");
    await expect(
      main.getByText(/Workflow summary|Workflow insights|Insights/i).first(),
    ).toBeVisible();
    await expect(
      main
        .locator("[data-hcm-insights-evidence], .insights-evidence, .insights-empty")
        .first(),
    ).toBeVisible();
    await expect(main).not.toContainText(/\bNaN\b|\bundefined\b/);
    const visibleMetrics = await main.locator(".metric, [data-hcm-metric]").count();
    if (visibleMetrics > 0) {
      for (const metric of await main.locator(".metric, [data-hcm-metric]").all()) {
        await expect(
          metric.locator(":is(h2,h3,dt,[data-metric-label])").first(),
        ).toBeVisible();
      }
    }
  });

  test("TestTodo_UXAUDIT_018_Integration Insights is served from the authenticated application", async ({
    page,
  }) => {
    const response = await page.goto("/workspace/app/insights");
    expect(response?.status(), "Insights route must be served successfully").toBe(200);
    await expect(page.locator("main h1")).toBeVisible();
    await expect(page.locator("main")).toContainText(
      /snapshot|current|workflow|journey/i,
    );
  });

  test("TestTodo_UXAUDIT_021_Browser worker ID editor keeps grouped fields and Save in view", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/workspace/app/admin/worker-ids");
    const groups = page.locator("[data-field-group]");
    // The shell renders before the page data RPC settles. Wait on this
    // route's actual editor marker before measuring sticky geometry.
    await expect(groups).toHaveCount(4, { timeout: 20_000 });
    const actions = page.locator(".worker-id-actions");
    const save = actions.getByRole("button", { name: /save/i });
    await expect(save).toBeVisible();
    const scrollOwner = await page.locator(".worker-id-actions").evaluate((actions) => {
      let current = actions.parentElement;
      while (current && current !== document.body) {
        const style = window.getComputedStyle(current);
        if (
          /(auto|scroll)/.test(style.overflowY) &&
          current.scrollHeight > current.clientHeight + 1
        ) {
          current.scrollTop = current.scrollHeight;
          return current.id || current.className || current.tagName;
        }
        current = current.parentElement;
      }
      document.scrollingElement.scrollTop = document.scrollingElement.scrollHeight;
      return "document";
    });
    expect(scrollOwner, "the editor must have a scroll owner").not.toBe("");
    await page.waitForTimeout(100);
    const bounds = await actions.boundingBox();
    const stickyEvidence = await actions.evaluate((element) => {
      const ancestors = [];
      let current = element;
      while (current && ancestors.length < 8) {
        const style = window.getComputedStyle(current);
        ancestors.push({
          tag: current.tagName,
          className: typeof current.className === "string" ? current.className : "",
          overflowY: style.overflowY,
          position: style.position,
          top: current.getBoundingClientRect().top,
          bottom: current.getBoundingClientRect().bottom,
          scrollTop: current.scrollTop,
          scrollHeight: current.scrollHeight,
          clientHeight: current.clientHeight,
        });
        current = current.parentElement;
      }
      return {
        viewport: { width: window.innerWidth, height: window.innerHeight },
        scrollY: window.scrollY,
        ancestors,
      };
    });
    window.console.log(
      `UXAUDIT-021 sticky evidence: ${JSON.stringify(stickyEvidence)}`,
    );
    await page.screenshot({ path: ".artifacts/lanes/uxaudit021-mobile-sticky.png" });
    expect(bounds, "the action bar must remain rendered").not.toBeNull();
    expect(
      bounds.y,
      "the Save action must remain inside the mobile viewport",
    ).toBeGreaterThanOrEqual(0);
    expect(
      bounds.y + bounds.height,
      "the Save action must fit in the mobile viewport",
    ).toBeLessThanOrEqual(844);
    const status = actions.locator('[role="status"][aria-live="polite"]');
    await expect(status).toBeVisible();
  });

  test("TestTodo_UXAUDIT_021_Accessibility worker ID groups and inputs are keyboard reachable", async ({
    page,
  }) => {
    await page.goto("/workspace/app/admin/worker-ids");
    for (const group of await page.locator("fieldset[data-field-group]").all()) {
      await expect(group.locator("legend")).toBeVisible();
    }
    await page.keyboard.press("Tab");
    const active = await page.evaluate(
      () =>
        document.activeElement?.getAttribute("id") || document.activeElement?.tagName,
    );
    expect(active, "keyboard Tab must focus a real editor control").not.toBe("BODY");
  });

  test("TestTodo_UXAUDIT_022_Browser Appearance exposes the governed asset picker", async ({
    page,
  }) => {
    await page.goto("/workspace/app/appearance");
    const picker = page.locator('[data-hcm-brand-asset-picker="true"]');
    await expect(picker).toBeVisible();
    await expect(picker).toHaveAttribute("data-hcm-brand-asset-governed", "true");
    const upload = picker.locator('[data-hcm-asset-action="upload"]');
    await expect(upload).toBeEnabled();
    await expect(picker.locator('[data-hcm-asset-action="preview"]')).toBeVisible();
    await expect(picker.locator('[data-hcm-asset-action="remove"]')).toBeVisible();
    const rollback = picker.locator('[data-hcm-asset-action="rollback"]');
    if (await rollback.count()) await expect(rollback.first()).toBeEnabled();
    await expect(picker.locator("[data-hcm-asset-variants]")).toBeVisible();
    await expect(picker.locator("[data-hcm-brand-asset-diff]")).toBeVisible();
    await expect(page.locator("main")).not.toContainText(/file path|filesystem path/i);
    await expect(page.locator("[data-hcm-preview-surface]")).toBeVisible();
  });

  test("TestTodo_UXAUDIT_022_Accessibility asset upload is labelled and preview is named", async ({
    page,
  }) => {
    await page.goto("/workspace/app/appearance");
    const picker = page.locator('[data-hcm-brand-asset-picker="true"]');
    const upload = picker.locator('[data-hcm-asset-action="upload"]');
    await expect(upload).toHaveAttribute(
      "aria-describedby",
      /appearance-brand-logo-help/,
    );
    await expect(upload).toHaveAccessibleName(/upload image/i);
    await expect(page.locator("#appearance-brand-logo-help")).toBeVisible();
    await expect(page.locator("[data-hcm-preview-surface]")).toHaveAttribute(
      "aria-label",
      /.+/,
    );
  });

  test("TestTodo_UXAUDIT_023_Browser personal Settings groups and profile links work", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/workspace/app/settings?locale=en-US&nav=collapsed");
    for (const group of [
      "profile",
      "language",
      "accessibility",
      "notifications",
      "navigation",
      "account-security",
      "signout",
    ]) {
      await expect(page.locator(`[data-hcm-setting-group="${group}"]`)).toBeVisible();
    }
    await expect(
      page.locator(
        '[data-hcm-setting-group="profile"] a[href*="/workspace/app/myself"]',
      ),
    ).toBeVisible();
    await expect(
      page.locator('[data-hcm-setting-group="signout"] .danger'),
    ).toBeVisible();
    const pageWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(
      pageWidth,
      "personal Settings must not overflow at 390px",
    ).toBeLessThanOrEqual(390);
  });

  test("TestTodo_UXAUDIT_023_Accessibility Settings tasks have headings and keyboard focus", async ({
    page,
  }) => {
    await page.goto("/workspace/app/settings");
    const groups = page.locator("[data-hcm-setting-group]");
    expect(await groups.count()).toBeGreaterThanOrEqual(7);
    for (const group of await groups.all()) {
      await expect(group.locator(":is(h2,h3)").first()).toBeVisible();
    }
    await page.keyboard.press("Tab");
    expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe("BODY");
  });

  test("TestTodo_UXAUDIT_023_I18N Settings applies the requested document language", async ({
    page,
  }) => {
    await page.goto("/workspace/app/settings?locale=de-DE");
    await expect(page.locator("html")).toHaveAttribute("lang", "de-DE");
    await expect(page.locator("main h1")).toBeVisible();
    await expect(
      page.locator('[data-hcm-setting-group="account-security"]'),
    ).toBeVisible();
  });

  test("TestTodo_UXAUDIT_023_Integration Settings navigation preserves shell state", async ({
    page,
  }) => {
    await page.goto("/workspace/app/settings?locale=en-US&nav=collapsed");
    const profile = page.locator(
      '[data-hcm-setting-group="profile"] a[href*="/workspace/app/myself"]',
    );
    await expect(profile).toBeVisible();
    await profile.click();
    await expect(page).toHaveURL(
      /\/workspace\/app\/myself\?.*locale=en-US.*nav=collapsed/,
    );
    await expect(page.locator("main h1")).toBeVisible();
  });
});
