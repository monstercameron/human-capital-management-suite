// PROMOUX-015 live browser pass over the running dev cell.
//
// The Go half (TestTodo_PROMOUX_015 and its matrix in internal/application)
// drives the promotion itself through the production gRPC path with four
// separated personas. This spec is what a Go test cannot prove: the real
// rendered layout of each persona's review surfaces in a real browser. It
// signs in through the dev persona login form as each of the four personas
// and, for every width (1280, 390, 320 px), theme (light, dark, with reduced
// motion) and locale (en-US, de-DE, ar), loads the pages that persona may
// reach and asserts:
//   - the document publishes the resolved lang and dir;
//   - nothing scrolls horizontally;
//   - no work-row fact is hidden except the grade summary line
//     (TestWorkRowActionFactsSurviveNarrowWidths is the Go-side guard);
//   - the page logs no console error.
//
// Run it against the rebuilt, running dev cell:
//   npx playwright test --config=tools/uxqual/browser/playwright.config.mjs promoux015_multi_persona
import { test, expect } from "@playwright/test";

const personas = [
  { id: "hiring-manager", pages: ["work", "journeys", "history", "people"] },
  { id: "finance-partner", pages: ["work", "history", "myself"] },
  { id: "admin", pages: ["work", "journeys", "history", "people"] },
  { id: "individual-contributor", pages: ["myself"] },
];
const widths = [1280, 390, 320];
const themes = ["light", "dark"];
const locales = [
  { locale: "en-US", dir: "ltr" },
  { locale: "de-DE", dir: "ltr" },
  { locale: "ar", dir: "rtl" },
];

for (const persona of personas) {
  test.describe(`PROMOUX-015 ${persona.id}`, () => {
    for (const width of widths) {
      for (const theme of themes) {
        test(`${persona.id} at ${width}px ${theme}`, async ({ browser, baseURL }) => {
          test.setTimeout(240_000);
          const context = await browser.newContext({
            baseURL,
            viewport: { width, height: 900 },
            colorScheme: theme,
            reducedMotion: "reduce",
          });
          const page = await context.newPage();
          const errors = [];
          page.on("console", (msg) => {
            if (msg.type() === "error") errors.push(msg.text());
          });

          await page.goto("/workspace/login");
          await page
            .locator(
              `form:has(input[name="persona"][value="${persona.id}"]) button, button[name="persona"][value="${persona.id}"]`,
            )
            .first()
            .click();
          await page.waitForURL(/\/workspace\/app\//);
          // Settle the landing document before the matrix navigations: every
          // app page starts the enhancement-bundle download on parse, and
          // navigating away mid-download aborts it into a console error.
          // Real readers settle; the matrix must too, or it measures its own
          // interruption instead of the pages.
          await page.waitForLoadState("networkidle");

          for (const { locale, dir } of locales) {
            for (const name of persona.pages) {
              await page.goto(`/workspace/app/${name}?locale=${locale}`);
              await page.waitForLoadState("networkidle");
              const where = `${persona.id} ${name} ${locale} ${width}px ${theme}`;

              const documentFacts = await page.evaluate(() => ({
                lang: document.documentElement.getAttribute("lang"),
                dir: document.documentElement.getAttribute("dir"),
                scrollWidth: document.documentElement.scrollWidth,
                innerWidth: window.innerWidth,
                hiddenFacts: [...document.querySelectorAll(".work-row .row-main small")]
                  .filter(
                    (el) =>
                      window.getComputedStyle(el).display === "none" &&
                      !el.classList.contains("row-summary"),
                  )
                  .map((el) => el.className),
              }));
              expect(documentFacts.lang, `${where}: lang`).toBe(locale);
              expect(documentFacts.dir, `${where}: dir`).toBe(dir);
              expect(
                documentFacts.scrollWidth,
                `${where}: horizontal overflow (scrollWidth ${documentFacts.scrollWidth} > innerWidth ${documentFacts.innerWidth})`,
              ).toBeLessThanOrEqual(documentFacts.innerWidth + 1);
              expect(
                documentFacts.hiddenFacts,
                `${where}: hidden work-row facts`,
              ).toEqual([]);
            }
          }
          expect(errors, `${persona.id} ${width}px ${theme}: console errors`).toEqual(
            [],
          );
          await context.close();
        });
      }
    }
  });
}
