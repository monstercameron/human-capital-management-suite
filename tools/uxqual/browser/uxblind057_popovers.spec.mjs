// Live Chromium acceptance checks for UXBLIND-057. These cover the hydrated
// product shell menus and the journey Share disclosure on the running dev cell.
import { test, expect } from "@playwright/test";

const home = "/workspace/app/home";

async function signInAsWalt(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

async function waitForHydration(page) {
  await page.waitForFunction(
    () => performance.getEntriesByName("hcm:hydrated").length > 0,
    null,
    { timeout: 30_000 },
  );
}

function shellPopover(page, kind) {
  if (kind === "action-launcher") return page.locator("#action-launcher");
  return page.locator(`details[data-hcm-transient-popover="${kind}"]`);
}

async function openPopover(root) {
  if ((await root.getAttribute("id")) === "action-launcher") {
    const trigger = root.locator("#action-launcher-trigger");
    // GoWebComponents installs OnClick through the DOM `onclick` property,
    // not addEventListener. Wait for that actual property before clicking;
    // otherwise a visible SSR button can race hydration's event binding.
    await trigger.page().waitForFunction(
      () => typeof document.querySelector("#action-launcher-trigger")?.onclick === "function",
      null,
      { timeout: 10_000 },
    );
    await trigger.evaluate((element) => {
      window.__uxblind057TriggerClicks = 0;
      window.__uxblind057ClickTimeline = [];
      const root = element.closest("#action-launcher");
      const record = (phase) =>
        window.__uxblind057ClickTimeline.push({
          phase,
          at: performance.now(),
          className: root.className,
          expanded: element.getAttribute("aria-expanded"),
          dialogHidden: root.querySelector("#action-launcher-dialog")?.hasAttribute("hidden"),
        });
      const managedClick = element.onclick;
      window.__uxblind057ManagedClickType = typeof managedClick;
      if (typeof managedClick === "function") {
        element.onclick = function (event) {
          record("managed-handler-enter");
          try {
            const result = managedClick.call(this, event);
            record("managed-handler-return");
            return result;
          } catch (error) {
            window.__uxblind057ManagedClickError = String(error);
            record("managed-handler-throw");
            throw error;
          }
        };
      }
      const observeTrigger = (event, phase) => {
        if (event.target.closest?.("#action-launcher-trigger")) record(phase);
      };
      document.addEventListener("click", (event) => observeTrigger(event, "capture"), true);
      document.addEventListener("click", (event) => observeTrigger(event, "bubble"));
      new MutationObserver(() => record("mutation")).observe(root, {
        attributes: true,
        subtree: true,
        attributeFilter: ["class", "aria-expanded", "hidden"],
      });
      element.addEventListener("click", () => window.__uxblind057TriggerClicks++);
    });
    await trigger.click();
    try {
      await expect
        .poll(async () =>
        JSON.stringify(
          await root.evaluate((element) => ({
            className: element.className,
            clicks: window.__uxblind057TriggerClicks,
            expanded: element
              .querySelector("#action-launcher-trigger")
              ?.getAttribute("aria-expanded"),
            dialogHidden: element
              .querySelector("#action-launcher-dialog")
              ?.hasAttribute("hidden"),
            errors: window.__uxblind057Errors,
            consoleErrors: window.__uxblind057ConsoleErrors,
            runtimePanics: window.__uxblind057RuntimePanics,
            clickTimeline: window.__uxblind057ClickTimeline,
            managedClickType: window.__uxblind057ManagedClickType,
            managedClickError: window.__uxblind057ManagedClickError,
            bootMarks: performance
              .getEntriesByType("mark")
              .filter((entry) => entry.name.startsWith("hcm:"))
              .map((entry) => ({ name: entry.name, at: entry.startTime })),
            listenerBindings: window.__uxblind057ListenerBindings,
          })),
        ),
        )
        .toContain("action-launcher-open");
    } catch (error) {
      const firstClick = await root.evaluate((element) => ({
        className: element.className,
        expanded: element.querySelector("#action-launcher-trigger")?.getAttribute("aria-expanded"),
      }));
      if (!firstClick.className.includes("action-launcher-open")) {
        await trigger.click();
        await root.page().waitForTimeout(100);
      }
      const secondClickDiagnostic = await root.evaluate((element) => ({
        className: element.className,
        expanded: element.querySelector("#action-launcher-trigger")?.getAttribute("aria-expanded"),
        clickTimeline: window.__uxblind057ClickTimeline,
      }));
      throw new Error(`${error.message}; second-click diagnostic=${JSON.stringify(secondClickDiagnostic)}`);
    }
    const diagnostic = await root.evaluate((element) => ({
      clicks: window.__uxblind057TriggerClicks,
      expanded: element
        .querySelector("#action-launcher-trigger")
        ?.getAttribute("aria-expanded"),
      dialogHidden: element
        .querySelector("#action-launcher-dialog")
        ?.hasAttribute("hidden"),
    }));
    if (diagnostic.clicks !== 1)
      throw new Error(
        `action trigger click was not received: ${JSON.stringify(diagnostic)}`,
      );
    return;
  }
  await root.locator(":scope > summary").click();
  await expect(root).toHaveAttribute("open", "");
}

async function expectPopoverClosed(root) {
  if ((await root.getAttribute("id")) === "action-launcher") {
    await expect(root).not.toHaveClass(/action-launcher-open/);
    return;
  }
  await expect(root).not.toHaveAttribute("open");
}

const shellKinds = ["locale", "action-launcher", "notification"];

test.describe("UXBLIND-057 transient popovers", () => {
  test.beforeEach(async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.addInitScript(() => {
      window.__uxblind057Errors = [];
      window.__uxblind057ConsoleErrors = [];
      window.__uxblind057RuntimePanics = [];
      window.__uxblind057ListenerBindings = [];
      const addEventListener = EventTarget.prototype.addEventListener;
      EventTarget.prototype.addEventListener = function (type, listener, options) {
        if (
          type === "click" &&
          this === document
        ) {
          window.__uxblind057ListenerBindings.push({
            target: "document",
            at: performance.now(),
            capture: typeof options === "boolean" ? options : options?.capture ?? false,
          });
        }
        return addEventListener.call(this, type, listener, options);
      };
      window.addEventListener("error", (event) => {
        window.__uxblind057Errors.push(event.message || "window error");
      });
      window.addEventListener("unhandledrejection", (event) => {
        window.__uxblind057Errors.push(String(event.reason));
      });
      window.addEventListener("gwc:runtime-panic", (event) => {
        window.__uxblind057RuntimePanics.push(event.detail);
      });
      const originalConsoleError = console.error.bind(console);
      console.error = (...args) => {
        window.__uxblind057ConsoleErrors.push(
          args.map((value) => value?.stack || String(value)).join(" ").slice(0, 1000),
        );
        originalConsoleError(...args);
      };
    });
    await signInAsWalt(page);
    await page.goto(home);
    await page.waitForLoadState("networkidle");
    await waitForHydration(page);
    await expect(page.locator("main")).toBeVisible();
  });

  for (const kind of shellKinds) {
    test(`${kind} closes on Escape and restores focus to its trigger`, async ({
      page,
    }) => {
      const root = shellPopover(page, kind);
      await expect(root).toBeVisible();
      await openPopover(root);
      await page.keyboard.press("Escape");
      await expectPopoverClosed(root);
      const trigger =
        kind === "action-launcher"
          ? root.locator("#action-launcher-trigger")
          : root.locator(":scope > summary");
      await expect(trigger).toBeFocused();
    });

    test(`${kind} closes after an outside pointer action`, async ({ page }) => {
      const root = shellPopover(page, kind);
      await openPopover(root);
      await page.locator("main").click({ position: { x: 12, y: 12 } });
      await expectPopoverClosed(root);
    });
  }

  test("selecting a locale option closes the language menu", async ({ page }) => {
    const root = shellPopover(page, "locale");
    await openPopover(root);
    const option = root.locator('.popover-surface a[aria-current="true"]');
    await expect(option).toBeVisible();
    await option.click();
    await expectPopoverClosed(root);
  });

  test("selecting an action closes quick actions", async ({ page }) => {
    const root = shellPopover(page, "action-launcher");
    await openPopover(root);
    const result = root
      .locator(".action-launcher-result:not([aria-disabled='true'])")
      .first();
    await expect(result).toBeVisible();
    await result.click();
    await expectPopoverClosed(root);
  });

  test("selecting a notification destination closes Notifications", async ({
    page,
  }) => {
    const root = shellPopover(page, "notification");
    await openPopover(root);
    const destination = root.locator(".popover-surface a").last();
    await expect(destination).toBeVisible();
    await destination.click();
    await expectPopoverClosed(root);
  });

  test("opening one shell popover closes its sibling", async ({ page }) => {
    const locale = shellPopover(page, "locale");
    const notifications = shellPopover(page, "notification");
    await openPopover(locale);
    await openPopover(notifications);
    await expectPopoverClosed(locale);
    await expect(notifications).toHaveAttribute("open", "");
  });

  test("opening a dialog closes an already-open menu", async ({ page }) => {
    const locale = shellPopover(page, "locale");
    const actions = shellPopover(page, "action-launcher");
    await openPopover(locale);
    await openPopover(actions);
    await expect(page.locator('#action-launcher-dialog[role="dialog"]')).toBeVisible();
    await expectPopoverClosed(locale);
  });

  test("journey Share closes after Copy link, Escape and an outside click", async ({
    page,
  }) => {
    await page.goto("/workspace/app/journeys");
    await waitForHydration(page);
    const detail = page.locator('a[href*="journey="]').first();
    await expect(
      detail,
      "the seeded journey list must expose a detail route",
    ).toBeVisible();
    await detail.click();

    const share = page.locator('details[data-hcm-transient-popover="share"]');
    await expect(
      share,
      "journey Share must use the shared transient-popover contract",
    ).toBeVisible();
    await openPopover(share);
    await page.getByRole("button", { name: "Copy link" }).click();
    await expectPopoverClosed(share);

    await openPopover(share);
    await page.keyboard.press("Escape");
    await expectPopoverClosed(share);
    await expect(share.locator(":scope > summary")).toBeFocused();

    await openPopover(share);
    await page.locator("main").click({ position: { x: 12, y: 12 } });
    await expectPopoverClosed(share);
  });
});
