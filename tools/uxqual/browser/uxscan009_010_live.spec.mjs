// Live Chromium scenarios for UXSCAN-009 and UXSCAN-010. These intentionally
// visit the running workspace so route state, hydrated disclosure controls,
// computed layout, and actual scrolling are exercised together.
import { test, expect } from "@playwright/test";

const home = "/workspace/app/home";

async function signInAsAdmin(page) {
  // The local workspace requires an explicit persona session. Each Playwright
  // test has an isolated context, so establish it here as the live persona
  // harness does instead of relying on a browser's pre-existing cookie.
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

test.describe("UXSCAN-009 administrative and personal task labels", () => {
  test.beforeEach(async ({ page }) => {
    await signInAsAdmin(page);
  });

  test("[UXSCAN-009] settings separates personal security from organization appearance", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/workspace/app/settings");

    const account = page.locator('[data-hcm-setting-group="account-security"]');
    const organization = page.locator(
      '[data-hcm-setting-group="organization-configuration"]',
    );
    const preferences = page.locator('[data-hcm-setting-group="personal-preferences"]');
    await expect(
      account.getByRole("heading", { name: "Account & security" }),
    ).toBeVisible();
    await expect(
      organization.getByRole("heading", { name: "Organization settings" }),
    ).toBeVisible();
    await expect(preferences).toBeVisible();
    const order = await page.evaluate(() => {
      const account = document.querySelector(
        '[data-hcm-setting-group="account-security"]',
      );
      const organization = document.querySelector(
        '[data-hcm-setting-group="organization-configuration"]',
      );
      return Boolean(
        account &&
        organization &&
        account.compareDocumentPosition(organization) &
          window.Node.DOCUMENT_POSITION_FOLLOWING,
      );
    });
    expect(order, "organization settings must follow account security").toBe(true);
    await expect(organization).toContainText("Changes here affect everyone");
    await expect(account).not.toContainText("Organization appearance");
  });

  test("[UXSCAN-009] Admin describes planned capabilities without promising availability", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/workspace/app/admin");
    await expect(page.getByRole("heading", { name: "Admin", level: 1 })).toBeVisible();
    await expect(
      page.getByText("Manage available settings and see what is planned", {
        exact: false,
      }),
    ).toBeVisible();
    await expect(page.getByText("Planned", { exact: true }).first()).toBeVisible();
    await expect(
      page.locator("[data-action-state='unavailable']").first(),
    ).toBeVisible();
    await expect(page.locator("main")).not.toContainText(
      "only shows the capabilities available",
    );
  });

  test("[UXSCAN-009] visibility asks which people a role's holders may find", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/workspace/app/admin/organization-visibility");
    await page.waitForLoadState("networkidle");
    await expect(
      page
        .getByText("Which people can members of this role find?", { exact: true })
        .first(),
    ).toBeVisible();
    await expect(
      page.getByText("Who can people discover?", { exact: true }),
    ).toHaveCount(0);
  });
});

const navViewports = [
  { name: "desktop", width: 1280, height: 900, mobile: false },
  { name: "390px drawer", width: 390, height: 844, mobile: true },
  { name: "320px drawer", width: 320, height: 720, mobile: true },
];

for (const viewport of navViewports) {
  test.describe(`UXSCAN-010 navigation reachability at ${viewport.name}`, () => {
    test.beforeEach(async ({ page }) => {
      await signInAsAdmin(page);
    });

    test("[UXSCAN-010] one scroll region reaches the final destination by wheel and keyboard", async ({
      page,
    }) => {
      await page.setViewportSize({ width: viewport.width, height: viewport.height });
      // The signed-in persona may have a persisted compact navigation
      // preference. This scenario specifically qualifies the expanded tree.
      await page.goto(`${home}?nav=expanded`);

      if (viewport.mobile) {
        // Wait for the browser enhancement to convert the static sidebar into
        // its off-canvas drawer before exercising the trigger.
        await expect
          .poll(() =>
            page.locator("#workspace-navigation").evaluate((el) => el.style.position),
          )
          .toBe("fixed");
        await page.locator("#nav-drawer-trigger").click();
        await expect(page.locator("#workspace-navigation")).toHaveAttribute(
          "aria-modal",
          "true",
        );
        await page.waitForTimeout(220);
      }
      const groups = page.locator(".nav-group");
      const scrollOwner = page.locator(
        viewport.mobile ? "#workspace-navigation" : "#primary-nav",
      );
      for (let index = 0; index < (await groups.count()); index += 1) {
        const group = groups.nth(index);
        if (!(await group.evaluate((el) => el.open))) {
          // Reach each disclosure through the same region users scroll. This
          // avoids Playwright's implicit page-level scroll when a later group
          // sits below the visible portion of the navigation.
          for (let attempt = 0; attempt < 6; attempt += 1) {
            const visible = await group.locator(".nav-group-summary").evaluate((el) => {
              const box = el.getBoundingClientRect();
              const owner = document
                .querySelector(
                  window.innerWidth <= 640 ? "#workspace-navigation" : "#primary-nav",
                )
                ?.getBoundingClientRect();
              return Boolean(
                owner && box.top >= owner.top && box.bottom <= owner.bottom,
              );
            });
            if (visible) break;
            await scrollOwner.hover();
            await page.mouse.wheel(0, Math.floor(viewport.height / 3));
          }
          await group.locator(".nav-group-summary").click();
        }
      }

      await scrollOwner.evaluate((el) => el.scrollTo(0, 0));
      const initial = await scrollOwner.evaluate((el) => ({
        top: el.scrollTop,
        height: el.clientHeight,
        content: el.scrollHeight,
        overflow: window.getComputedStyle(el).overflowY,
        scrollbarWidth: window.getComputedStyle(el).scrollbarWidth,
      }));
      expect(initial.overflow).toBe("auto");
      expect(initial.scrollbarWidth).toBe("thin");
      expect(initial.content).toBeGreaterThan(initial.height);

      const cdp = await page.context().newCDPSession(page);
      const navBox = await scrollOwner.boundingBox();
      expect(navBox.x).toBeGreaterThanOrEqual(0);
      expect(navBox.y).toBeGreaterThanOrEqual(0);
      expect(navBox.x + navBox.width).toBeLessThanOrEqual(viewport.width);
      expect(navBox.y + navBox.height).toBeLessThanOrEqual(viewport.height);
      const touchX = Math.round(navBox.x + navBox.width / 2);
      const touchStartY = Math.round(navBox.y + navBox.height / 2);
      const touchEndY = touchStartY - Math.min(180, Math.floor(initial.height / 2));
      await cdp.send("Emulation.setTouchEmulationEnabled", {
        enabled: true,
        maxTouchPoints: 1,
      });
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchStart",
        touchPoints: [{ x: touchX, y: touchStartY, radiusX: 1, radiusY: 1, force: 1 }],
      });
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [{ x: touchX, y: touchEndY, radiusX: 1, radiusY: 1, force: 1 }],
      });
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchEnd",
        touchPoints: [],
      });
      await expect
        .poll(() => scrollOwner.evaluate((el) => el.scrollTop))
        .toBeGreaterThan(initial.top);
      await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: false });
      await scrollOwner.evaluate((el) => el.scrollTo(0, 0));

      await page.mouse.move(
        Math.round(navBox.x + navBox.width / 2),
        Math.round(navBox.y + navBox.height / 2),
      );
      await page.mouse.wheel(0, initial.height);
      await expect
        .poll(() => scrollOwner.evaluate((el) => el.scrollTop))
        .toBeGreaterThan(initial.top);

      await scrollOwner.focus();
      await page.keyboard.press("End");
      const lastDestination = page.locator("#primary-nav a[href]").last();
      await expect(lastDestination).toBeVisible();
      const geometry = await page.evaluate(() => {
        const navEl = document.querySelector("#primary-nav");
        const owner = document.querySelector(
          window.innerWidth <= 640 ? "#workspace-navigation" : "#primary-nav",
        );
        const target = Array.from(navEl?.querySelectorAll("a[href]") ?? []).at(-1);
        const fixed = [
          document.querySelector(".menu-filter"),
          document.querySelector(".nav-search"),
          document.querySelector(".nav-bottom"),
        ]
          .filter(Boolean)
          .map((el) => el.getBoundingClientRect());
        const box = target?.getBoundingClientRect();
        return {
          targetVisible: Boolean(
            box &&
            owner &&
            box.top >= 0 &&
            box.bottom <= window.innerHeight &&
            box.top >= owner.getBoundingClientRect().top &&
            box.bottom <= owner.getBoundingClientRect().bottom,
          ),
          targetCovered: Boolean(
            box &&
            fixed.some((region) => box.top < region.bottom && box.bottom > region.top),
          ),
          nestedScrollers: Array.from(navEl?.querySelectorAll("*") ?? []).filter(
            (el) => {
              const style = window.getComputedStyle(el);
              return (
                ["auto", "scroll"].includes(style.overflowY) &&
                el.scrollHeight > el.clientHeight + 1
              );
            },
          ).length,
        };
      });
      expect(
        geometry.targetVisible,
        "the final destination must fit in the viewport after keyboard scrolling",
      ).toBe(true);
      expect(
        geometry.targetCovered,
        "search and support controls must not cover the destination",
      ).toBe(false);
      expect(
        geometry.nestedScrollers,
        "navigation groups must not introduce inner scrollbars",
      ).toBe(0);
    });
  });
}
