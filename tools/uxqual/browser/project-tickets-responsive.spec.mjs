import { test, expect } from "@playwright/test";

// Regression coverage for issue #66. This is a rendered, read-only check: it
// exercises the seeded empty-ticket state and never changes a filter or saves
// any project data.
test.setTimeout(90000);

async function signInAsRafael(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

for (const locale of ["en-US", "de-DE", "ar"]) {
  for (const colorScheme of ["light", "dark"]) {
    for (const width of [390, 320]) {
      test(`tickets empty state and filters fit ${width}px in ${locale}/${colorScheme}`, async ({
        page,
      }) => {
        await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
        await page.setViewportSize({ width, height: 844 });
        await signInAsRafael(page);
        await page.goto(`/workspace/app/projects?locale=${locale}&tab=tickets`);

        const tickets = page.locator("#project-tabpanel-tickets");
        await expect(tickets).toBeVisible({ timeout: 30000 });
        // This seeded Rafael fixture is intentionally empty; fail as a fixture
        // precondition if its data changes instead of silently testing another
        // state under the empty-state assertions below.
        await expect(tickets.locator(".projectui-tickets-loading")).toHaveCount(0, {
          timeout: 30000,
        });
        await expect(tickets.locator(".projectui-tickets-rows li")).toHaveCount(0);
        const filters = tickets.locator(".projectui-tickets-filters");
        await expect(filters).toBeVisible();
        const emptyCopies = tickets.locator(
          ".project-my-tickets-empty p, .projectui-tickets-empty p",
        );
        await expect(emptyCopies).toHaveCount(4);
        const filterGroups = filters.locator("details[data-group]");
        await expect(filterGroups).toHaveCount(3);
        const groupKeys = await filterGroups.evaluateAll((groups) =>
          groups.map((group) => group.getAttribute("data-group")),
        );
        expect(groupKeys).toEqual(["status", "priority", "due"]);

        const effectiveTheme = await page
          .locator(".project-all-tickets > .projectui-tickets-table")
          .evaluate((surface) => {
            const color = window.getComputedStyle(surface).backgroundColor;
            const channels = color.match(/[\d.]+/g)?.map(Number) ?? [];
            const alpha = channels.length === 4 ? channels[3] : 1;
            const luminance =
              channels.length >= 3
                ? channels
                    .slice(0, 3)
                    .map((channel) => channel / 255)
                    .reduce(
                      (sum, channel, index) =>
                        sum + channel * [0.2126, 0.7152, 0.0722][index],
                      0,
                    )
                : null;
            return {
              color,
              alpha,
              luminance,
              configuredMode:
                document.documentElement.getAttribute("data-hcm-color-mode"),
            };
          });
        expect(
          effectiveTheme.alpha,
          "All tickets surface must have an opaque background",
        ).toBeGreaterThanOrEqual(0.98);
        expect(
          effectiveTheme.luminance,
          "ticket surface must have a measurable background",
        ).not.toBeNull();
        expect(
          effectiveTheme.configuredMode,
          "seeded account appearance must follow the system color scheme for this media matrix",
        ).toBe("system");
        expect(
          effectiveTheme.luminance < 0.35 ? "dark" : "light",
          "rendered ticket surface must match the requested color scheme",
        ).toBe(colorScheme);

        const geometry = await page.evaluate(() => {
          const width = document.documentElement.clientWidth;
          const box = (element) => {
            const rect = element.getBoundingClientRect();
            return {
              left: rect.left,
              right: rect.right,
              width: rect.width,
              clientWidth: element.clientWidth,
              scrollWidth: element.scrollWidth,
              visible:
                window.getComputedStyle(element).display !== "none" &&
                window.getComputedStyle(element).visibility !== "hidden" &&
                rect.width > 0 &&
                rect.height > 0,
            };
          };
          const copies = [
            ...document.querySelectorAll(
              "#project-tabpanel-tickets .project-my-tickets-empty p, #project-tabpanel-tickets .projectui-tickets-empty p",
            ),
          ];
          const controls = [
            ...document.querySelectorAll(
              "#project-tabpanel-tickets .projectui-tickets-filters > a, #project-tabpanel-tickets .projectui-tickets-filters > details > summary",
            ),
          ];
          return {
            viewportWidth: width,
            documentWidth: document.documentElement.scrollWidth,
            copies: copies.map(box),
            controls: controls.map((element) => ({
              name: element.textContent.trim(),
              tabIndex: element.tabIndex,
              ...box(element),
            })),
          };
        });

        expect(geometry.viewportWidth).toBe(width);
        expect(geometry.copies).toHaveLength(4);
        for (const [index, copy] of geometry.copies.entries()) {
          expect(copy.visible, `empty copy ${index} should be visible`).toBe(true);
          expect(
            copy.left,
            `empty copy ${index} starts inside viewport`,
          ).toBeGreaterThanOrEqual(-1);
          expect(
            copy.right,
            `empty copy ${index} ends inside viewport`,
          ).toBeLessThanOrEqual(width + 1);
          expect(
            copy.scrollWidth,
            `empty copy ${index} text should wrap`,
          ).toBeLessThanOrEqual(copy.clientWidth + 1);
        }
        expect(
          geometry.documentWidth,
          "tickets page should not overflow horizontally",
        ).toBeLessThanOrEqual(width + 1);
        expect(geometry.controls.length).toBeGreaterThan(0);
        for (const [index, control] of geometry.controls.entries()) {
          expect(control.visible, `filter control ${index} should be visible`).toBe(
            true,
          );
          expect(
            control.tabIndex,
            `filter control ${index} should be keyboard reachable`,
          ).toBeGreaterThanOrEqual(0);
          expect(
            control.left,
            `filter control ${index} starts inside viewport`,
          ).toBeGreaterThanOrEqual(-1);
          expect(
            control.right,
            `filter control ${index} ends inside viewport`,
          ).toBeLessThanOrEqual(width + 1);
        }

        const dueFilter = filters.locator('details[data-group="due"] > summary');
        await expect(dueFilter).toBeVisible();
        const searchInput = tickets.locator(".projectui-tickets-search input");
        await searchInput.focus();
        let dueReceivedTabFocus = false;
        for (let step = 0; step < geometry.controls.length + 2; step++) {
          await page.keyboard.press("Tab");
          if (
            await dueFilter.evaluate((element) => element === document.activeElement)
          ) {
            dueReceivedTabFocus = true;
            break;
          }
        }
        expect(
          dueReceivedTabFocus,
          "Tab traversal from ticket search should reach Due filter",
        ).toBe(true);
        await expect(dueFilter).toBeFocused();
        await page.keyboard.press("Enter");
        const dueMenu = filters.locator('details[data-group="due"] [role="menu"]');
        await expect(dueMenu).toBeVisible();
        const menuBox = await dueMenu.boundingBox();
        expect(menuBox?.x).toBeGreaterThanOrEqual(-1);
        expect(menuBox?.x + menuBox?.width).toBeLessThanOrEqual(width + 1);
        await page.screenshot({
          path: test.info().outputPath(`tickets-${locale}-${colorScheme}-${width}.png`),
          fullPage: true,
          animations: "disabled",
        });
      });
    }
  }
}
