// UXBLIND-100 live-browser regression for the fixed support footer and the
// independently scrolling primary navigation.
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const screenshotDir = resolve(
  process.cwd(),
  ".artifacts/lanes/ux_refinement/screenshots/uxblind100",
);

async function signInAsWalt(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
}

test("[UXBLIND-100] navigation scrolls clear of the pinned footer at desktop and phone sizes", async ({
  browser,
}) => {
  test.setTimeout(120000);
  mkdirSync(screenshotDir, { recursive: true });
  const diagnostics = [];
  const context = await browser.newContext({ colorScheme: "dark", reducedMotion: "reduce" });
  const page = await context.newPage();
  page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
  page.on("console", (message) => {
    if (message.type() === "error") diagnostics.push(`console: ${message.text()}`);
  });

  await page.setViewportSize({ width: 1280, height: 720 });
  await signInAsWalt(page);
  const violations = [];
  for (const height of [720, 732]) {
    await page.setViewportSize({ width: 1280, height });
    await page.waitForFunction((expectedHeight) => window.innerHeight === expectedHeight, height);
    await page.evaluate(() => new Promise((resolveFrame) => requestAnimationFrame(() => requestAnimationFrame(resolveFrame))));
    const state = await page.evaluate(() => {
      const nav = document.querySelector("#primary-nav");
      const footer = document.querySelector(".nav-bottom");
      if (!nav || !footer) return { missing: true };
      nav.scrollTop = 0;
      const navBox = nav.getBoundingClientRect();
      const footerBox = footer.getBoundingClientRect();
      const cue = getComputedStyle(nav, "::after");
      return {
        navBottom: navBox.bottom,
        footerTop: footerBox.top,
        clientHeight: nav.clientHeight,
        scrollHeight: nav.scrollHeight,
        cueOpacity: Number(cue.opacity),
      };
    });
    await page.screenshot({
      path: resolve(screenshotDir, `desktop-1280x${height}.png`),
      fullPage: false,
      animations: "disabled",
    });
    if (state.missing) {
      violations.push(`${height}px: primary navigation or support footer is missing`);
      continue;
    }
    if (state.footerTop < state.navBottom - 1) {
      violations.push(`${height}px: footer overlaps primary navigation (${JSON.stringify(state)})`);
    }
    if (state.scrollHeight > state.clientHeight && state.cueOpacity < 0.1) {
      violations.push(`${height}px: overflowing navigation has no visible scroll cue (${JSON.stringify(state)})`);
    }
  }

  for (const width of [390, 320]) {
    const mobileContext = await browser.newContext({
      viewport: { width, height: 844 },
      colorScheme: "dark",
      reducedMotion: "reduce",
    });
    const mobilePage = await mobileContext.newPage();
    mobilePage.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
    mobilePage.on("console", (message) => {
      if (message.type() === "error") diagnostics.push(`console: ${message.text()}`);
    });
    await signInAsWalt(mobilePage);
    await mobilePage.getByRole("button", { name: "Open navigation menu" }).click();
    const sidebar = mobilePage.locator(".sidebar.nav-drawer-open");
    await expect(sidebar).toBeVisible();
    await mobilePage.waitForFunction(
      () => document.querySelector(".sidebar.nav-drawer-open")?.getBoundingClientRect().left === 0,
      null,
      { timeout: 5000, polling: 50 },
    );
    const mobile = await mobilePage.evaluate(() => {
      const sidebar = document.querySelector(".sidebar.nav-drawer-open");
      const nav = sidebar?.querySelector("#primary-nav");
      const footer = sidebar?.querySelector(".nav-bottom");
      if (!sidebar || !nav) return { missing: true };
      const rect = (element) => {
        const box = element.getBoundingClientRect();
        return { top: box.top, bottom: box.bottom, left: box.left, right: box.right };
      };
      return {
        viewportWidth: document.documentElement.clientWidth,
        documentWidth: document.documentElement.scrollWidth,
        sidebar: rect(sidebar),
        nav: rect(nav),
        footer: footer ? { ...rect(footer), display: getComputedStyle(footer).display } : null,
      };
    });
    await mobilePage.screenshot({
      path: resolve(screenshotDir, `mobile-${width}.png`),
      fullPage: false,
      animations: "disabled",
    });
    if (mobile.missing) {
      violations.push(`${width}px: mobile navigation drawer is missing its nav`);
    } else {
      if (mobile.documentWidth > width) violations.push(`${width}px: document has horizontal overflow`);
      if (mobile.sidebar.left < -1 || mobile.sidebar.right > width + 1) {
        violations.push(`${width}px: open navigation drawer escapes the viewport (${JSON.stringify(mobile)})`);
      }
    }
    await mobileContext.close();
  }

  await test.info().attach("uxblind100-sidebar-geometry", {
    body: JSON.stringify({ violations, diagnostics }, null, 2),
    contentType: "application/json",
  });
  expect(diagnostics, "the navigation journey should not emit browser diagnostics").toEqual([]);
  expect(violations, "the navigation list must stay fully visible and scroll above its pinned footer").toEqual([]);
  await context.close();
});
