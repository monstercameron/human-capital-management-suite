// UXBLIND-125: exercise the real hydrated header on the pages that expose
// Page utilities. This is read-only: it signs in as the seeded Ironridge
// user, opens routes, and types only into global search.
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const viewports = [320, 390, 430, 800, 1100];
const pages = [
  { name: "agents", path: "/workspace/app/chat/agents" },
  { name: "workflow-history", path: "/workspace/app/workflows/history" },
];
const screenshotDir = resolve(
  process.cwd(),
  ".artifacts/lanes/ux_refinement/screenshots/uxblind125",
);

async function waitForHydration(page) {
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
}

async function readHeaderGeometry(page) {
  return page.evaluate(() => {
    const rect = (element) => {
      const box = element.getBoundingClientRect();
      return {
        left: box.left,
        right: box.right,
        top: box.top,
        bottom: box.bottom,
        width: box.width,
        height: box.height,
      };
    };
    const visible = (element) => {
      const style = getComputedStyle(element);
      const box = element.getBoundingClientRect();
      return (
        style.display !== "none" &&
        style.visibility !== "hidden" &&
        box.width > 0 &&
        box.height > 0
      );
    };
    const topbar = document.querySelector(".topbar");
    const search = document.querySelector("#global-search-input");
    const utilities = document.querySelector(".utility-drawer-trigger");
    const menu = document.querySelector("#nav-drawer-trigger");
    const actions = [topbar, search, utilities, menu].filter(Boolean);
    const boxes = Object.fromEntries(
      actions.map((element) => [
        element.id || element.className || element.tagName,
        { ...rect(element), visible: visible(element) },
      ]),
    );
    const tools = document.querySelector(".header-navigation-tools");
    const visibleControlChildren = tools
      ? [...tools.children].filter(visible).map((element) => ({
          name: element.id || String(element.className) || element.tagName,
          ...rect(element),
        }))
      : [];
    const overlaps = [];
    for (let i = 0; i < visibleControlChildren.length; i++) {
      for (let j = i + 1; j < visibleControlChildren.length; j++) {
        const a = visibleControlChildren[i];
        const b = visibleControlChildren[j];
        if (
          a.left < b.right - 1 &&
          a.right > b.left + 1 &&
          a.top < b.bottom - 1 &&
          a.bottom > b.top + 1
        ) {
          overlaps.push([a.name, b.name]);
        }
      }
    }
    const surfaces = [
      document.querySelector(".workflow-history-page"),
      document.querySelector("main"),
      document.body,
      document.documentElement,
    ].filter(Boolean);
    const sampledBackground = surfaces
      .map((element) => ({
        selector: element.tagName.toLowerCase() + (element.className ? `.${String(element.className).trim().replaceAll(" ", ".")}` : ""),
        color: getComputedStyle(element).backgroundColor,
      }))
      .find(({ color }) => color !== "rgba(0, 0, 0, 0)" && color !== "transparent");
    const rgb = sampledBackground?.color.match(/[\d.]+/g)?.slice(0, 3).map(Number);
    const luminance = rgb
      ? rgb.map((channel) => channel / 255).reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index], 0)
      : null;
    return {
      viewportWidth: document.documentElement.clientWidth,
      documentWidth: document.documentElement.scrollWidth,
      boxes,
      visibleControlChildren,
      overlaps,
      searchLabel:
        search?.getAttribute("aria-label") ||
        search?.labels?.[0]?.textContent?.trim() ||
        "",
      utilityLabel:
        utilities?.getAttribute("aria-label") || utilities?.textContent?.trim() || "",
      utilityTitle: utilities?.getAttribute("title") || "",
      visualTheme: {
        rootColorMode: document.documentElement.getAttribute("data-hcm-color-mode"),
        rootTheme: document.documentElement.getAttribute("data-theme"),
        cssColorScheme: getComputedStyle(document.documentElement).colorScheme,
        sampledBackground,
        estimatedMode: luminance === null ? "unknown" : luminance < 0.35 ? "dark" : "light",
      },
    };
  });
}

test("[UXBLIND-125] header utilities and search remain usable at responsive widths", async ({
  browser,
}) => {
  test.setTimeout(180000);
  mkdirSync(screenshotDir, { recursive: true });
  const diagnostics = [];
  const evidence = [];
  const violations = [];
  for (const target of pages) {
    const context = await browser.newContext({
      colorScheme: "dark",
      reducedMotion: "reduce",
    });
    const page = await context.newPage();
    let phase = "new context";
    const recordDiagnostic = (type, detail) =>
      diagnostics.push({
        at: new Date().toISOString(),
        route: new URL(page.url()).pathname,
        target: target.name,
        phase,
        type,
        detail,
      });
    page.on("pageerror", (error) =>
      recordDiagnostic("pageerror", { message: error.message, stack: error.stack }),
    );
    page.on("console", (message) => {
      if (message.type() === "error") recordDiagnostic("console", message.text());
    });
    page.on("requestfailed", (request) =>
      recordDiagnostic("requestfailed", {
        url: request.url(),
        error: request.failure()?.errorText ?? "",
      }),
    );

    try {
      phase = "sign in";
      await page.setViewportSize({ width: 1280, height: 900 });
      await page.goto("/workspace/login?company=ironridge-demo");
      await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
      await page.waitForURL(/\/workspace\/app\//);
      phase = "hydrating home after sign in";
      await waitForHydration(page);

      phase = `document navigation to ${target.path}`;
      await page.goto(target.path);
      phase = `hydrating ${target.path}`;
      await waitForHydration(page);
      await expect(page.locator(".topbar")).toBeVisible();
      await expect(page.locator("#global-search-input")).toBeVisible();
      const utility = page.locator(".utility-drawer-trigger");
      await expect(utility).toBeVisible();
      await expect(utility).toHaveAttribute("aria-label", "Page utilities");
      await expect(utility).toHaveAttribute("title", "Page utilities");

      for (const width of viewports) {
        phase = `resize to ${width}px`;
        await page.setViewportSize({ width, height: 800 });
        await page.waitForTimeout(100);
        const before = await readHeaderGeometry(page);
        const screenshotTheme = before.visualTheme.estimatedMode;
        const context = `${target.name} at ${width}px`;
        await page.screenshot({
          path: resolve(screenshotDir, `${target.name}-${width}px-${screenshotTheme}.png`),
          fullPage: false,
          animations: "disabled",
        });
        if (before.viewportWidth !== width)
          violations.push(`${context}: viewport applied as ${before.viewportWidth}px`);
        if (before.documentWidth > width)
          violations.push(
            `${context}: horizontal overflow ${before.documentWidth - width}px`,
          );
        if (before.overlaps.length > 0)
          violations.push(
            `${context}: overlapping header groups ${JSON.stringify(before.overlaps)}`,
          );
        if (!before.boxes["topbar"]?.visible)
          violations.push(`${context}: header is hidden`);
        if (!before.boxes["global-search-input"]?.visible)
          violations.push(`${context}: search is hidden`);
        if (!before.boxes["utility-drawer-trigger"]?.visible)
          violations.push(`${context}: Page utilities is hidden`);
        if (!before.searchLabel.includes("Search"))
          violations.push(
            `${context}: search accessible name is ${JSON.stringify(before.searchLabel)}`,
          );
        if (before.utilityLabel !== "Page utilities")
          violations.push(
            `${context}: utility accessible name is ${JSON.stringify(before.utilityLabel)}`,
          );
        if (before.utilityTitle !== "Page utilities")
          violations.push(
            `${context}: utility tooltip is ${JSON.stringify(before.utilityTitle)}`,
          );

        const search = page.locator("#global-search-input");
        phase = `focus search at ${width}px`;
        const diagnosticsBeforeFocus = diagnostics.length;
        await search.click();
        await page
          .waitForFunction(
            () =>
              document
                .querySelector("#global-search-input")
                ?.closest(".global-search")
                ?.getBoundingClientRect().width >= 200,
            null,
            { timeout: 3000, polling: 50 },
          )
          .catch(() => {});
        const expanded = await search.evaluate((input) => {
          const box = input.closest(".global-search").getBoundingClientRect();
          return { x: box.x, y: box.y, width: box.width, height: box.height };
        });
        const minimumSearchWidth = Math.min(width - 48, 240);
        if (!expanded) {
          violations.push(`${context}: focused search has no layout box`);
        } else if (expanded.width < minimumSearchWidth) {
          violations.push(
            `${context}: focused search is ${Math.round(expanded.width)}px wide; need ${minimumSearchWidth}px`,
          );
        }
        const diagnosticsAfterFocus = diagnostics.length;
        phase = `type in search at ${width}px`;
        await search.fill("uxblind125-header-check");
        if ((await search.inputValue()) !== "uxblind125-header-check")
          violations.push(`${context}: search rejected typed input`);
        const diagnosticsAfterTyping = diagnostics.length;
        await page.screenshot({
          path: resolve(
            screenshotDir,
            `${target.name}-${width}px-${screenshotTheme}-search-focused.png`,
          ),
          fullPage: false,
          animations: "disabled",
        });
        await search.fill("");
        await search.blur();

        evidence.push({
          page: target.name,
          width,
          before,
          focusedSearch: expanded,
          diagnosticsBeforeFocus,
          diagnosticsAfterFocus,
          diagnosticsAfterTyping,
          screenshot: `${target.name}-${width}px-${screenshotTheme}.png`,
        });
      }

      phase = "navigation availability check";
      const navigation = page.locator("#primary-nav");
      const navDrawer = page.locator("#nav-drawer-trigger");
      const hasUsableNavigation =
        (await navigation.isVisible().catch(() => false)) ||
        (await navDrawer.isVisible().catch(() => false));
      if (!hasUsableNavigation)
        violations.push(
          "workspace navigation or its mobile drawer trigger is unavailable",
        );
    } finally {
      await context.close();
    }
  }
  const diagnosticGroups = new Map();
  for (const entry of diagnostics) {
    const key = `${entry.target}|${entry.route}|${entry.phase}|${entry.type}|${entry.detail?.message || entry.detail}`;
    const summary = diagnosticGroups.get(key);
    if (summary) {
      summary.count++;
      summary.lastAt = entry.at;
    } else {
      diagnosticGroups.set(key, {
        target: entry.target,
        route: entry.route,
        phase: entry.phase,
        type: entry.type,
        message: entry.detail?.message || entry.detail,
        count: 1,
        firstAt: entry.at,
        lastAt: entry.at,
        stack: entry.detail?.stack,
      });
    }
  }
  const diagnosticSummary = [...diagnosticGroups.values()];
  await test.info().attach("uxblind125-responsive-header", {
    body: JSON.stringify(
      {
        viewports,
        pages: pages.map(({ name, path }) => ({ name, path })),
        evidence,
        diagnostics: diagnosticSummary,
      },
      null,
      2,
    ),
    contentType: "application/json",
  });
  expect(violations, "responsive header contract violations").toEqual([]);
  expect(diagnosticSummary, "browser console, page or network errors").toEqual([]);
});
