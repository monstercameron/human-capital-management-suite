import { test, expect, devices } from "@playwright/test";

// #3682 reading language shows the person's own language, #3630 Chat follows a
// soft locale switch, #3607 phone Chat list bar does not sit under the brand
// link. Read-only: nothing here writes chat data.

test.setTimeout(180000);

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("#3682 Chat preferences reading language shows the interface language, not Hindi", async ({
  page,
}) => {
  await signIn(page, "Linh Tran");
  await page.goto("/workspace/app/chat?locale=en-US");
  await page
    .getByRole("button", { name: /Chat preferences/i })
    .first()
    .waitFor({ timeout: 80000 });
  await page
    .getByRole("button", { name: /Chat preferences/i })
    .first()
    .click();
  const layer = page.locator(".chat-settings-layer:visible").first();
  await expect(layer).toBeVisible();
  const select = layer.getByRole("combobox", { name: "Reading language" });
  await expect(select).toBeVisible({ timeout: 30000 });
  const state = await select.evaluate((s) => ({
    value: s.value,
    label: s.selectedOptions[0]?.textContent.trim(),
    hasSelectedAttr: [...s.options].some((o) => o.hasAttribute("selected")),
  }));
  expect(state.value, JSON.stringify(state)).toBe("en");
  expect(state.label).toMatch(/English/);
});

test("#3630 Chat follows a soft switch from German to Arabic without a reload", async ({
  page,
}) => {
  await signIn(page);
  await page.goto("/workspace/app/settings?locale=de-DE");
  await page.locator("main, [role=main]").first().waitFor({ timeout: 80000 });
  await expect(page.locator("html")).toHaveAttribute("lang", /de/);
  // Soft switch to Arabic through the language card.
  await page.evaluate(() => (window.__firstMarker = "kept"));
  await page.locator("[aria-label='Language'], summary:has-text('DE')").first().click();
  const card = page.locator("a.locale-option[lang=ar]").first();
  await card.click({ timeout: 30000 });
  await expect(page.locator("html")).toHaveAttribute("lang", /^ar/, { timeout: 40000 });
  // Soft navigation to Chat through the sidebar (no page reload).
  await page.evaluate(() => (window.__softMarker = "kept"));
  // Wait for the Arabic Settings content to finish loading: the sidebar links carry the new locale.
  await expect(
    page.locator("nav a[href*='/workspace/app/chat']").first(),
  ).toHaveAttribute("href", /locale=ar/, { timeout: 40000 });
  await page.locator("nav a[href*='/workspace/app/chat']").first().click();
  await page
    .locator("[data-action='open-create']")
    .first()
    .waitFor({ state: "visible", timeout: 80000 });
  await page
    .locator("[data-action='open-create'], .chat-workspace textarea")
    .first()
    .waitFor({ timeout: 60000 });
  test.info().annotations.push({
    type: "settings switch was soft",
    description: String(await page.evaluate(() => window.__firstMarker === "kept")),
  });
  expect(await page.evaluate(() => window.__softMarker)).toBe("kept");
  const text = await page.evaluate(() => document.body.innerText);
  const names = await page.evaluate(() =>
    [...document.querySelectorAll("[aria-label]")].map((e) =>
      e.getAttribute("aria-label"),
    ),
  );
  const all = text + "\n" + names.join("\n");
  for (const german of [
    "Chat durchsuchen",
    "Mitglieder",
    "Umfrage",
    "Kanal verwalten",
  ]) {
    expect(all, `German string ${german} left after soft switch`).not.toContain(german);
  }
  expect(await page.locator(".chat-workspace").first().getAttribute("dir")).toBe("rtl");
});

test.describe("#3607 phone list bar", () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    userAgent: devices["Pixel 7"].userAgent,
  });
  for (const [w, h] of [
    [390, 844],
    [320, 640],
  ]) {
    test(`#3607 at ${w}px the preferences gear is not under the brand link and opens its menu`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: w, height: h });
      await signIn(page);
      await page.goto("/workspace/app/chat?locale=en-US&nav=collapsed");
      const hit = (box) =>
        page.evaluate(
          ([x, y]) => {
            const e = document.elementFromPoint(x, y);
            return e
              ? {
                  tag: e.tagName,
                  label:
                    e.closest("button,a,summary")?.getAttribute("aria-label") || "",
                  href: e.closest("a")?.getAttribute("href") || "",
                }
              : null;
          },
          [box.x + box.width / 2, box.y + box.height / 2],
        );
      const open = page.getByRole("button", { name: "Open conversations" });
      await open.waitFor({ timeout: 80000 });
      // A native tap at the button's centre must land on the button, not on the brand link.
      let box = await open.boundingBox();
      await expect
        .poll(async () => (await hit((box = await open.boundingBox())))?.label, {
          timeout: 20000,
        })
        .toBe("Open conversations");
      await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2);
      const gear = page.getByRole("button", { name: "Chat preferences" });
      await expect(gear).toBeVisible({ timeout: 30000 });
      const heading = page.getByRole("heading", { name: "Conversations", exact: true });
      const bell = page.locator("summary[aria-label^='Notifications']").first();
      const rects = [];
      for (const [name, loc] of [
        ["menu", page.getByRole("button", { name: "Open workspace menu" })],
        ["heading", heading],
        ["gear", gear],
        ["new", page.getByRole("button", { name: "New conversation" })],
        ["bell", bell],
      ]) {
        const b = await loc.boundingBox();
        expect(b, `${name} has a box`).not.toBeNull();
        expect(b.x, `${name} inside viewport`).toBeGreaterThanOrEqual(0);
        expect(b.x + b.width, `${name} inside viewport`).toBeLessThanOrEqual(w);
        if (name !== "heading") {
          expect(b.width, `${name} touch width`).toBeGreaterThanOrEqual(43.5);
          expect(b.height, `${name} touch height`).toBeGreaterThanOrEqual(43.5);
        }
        rects.push([name, b]);
      }
      for (let a = 0; a < rects.length; a++) {
        for (let c = a + 1; c < rects.length; c++) {
          const [na, ra] = rects[a];
          const [nc, rc] = rects[c];
          const overlap =
            ra.x < rc.x + rc.width - 0.5 &&
            rc.x < ra.x + ra.width - 0.5 &&
            ra.y < rc.y + rc.height - 0.5 &&
            rc.y < ra.y + ra.height - 0.5;
          expect(overlap, `${na} overlaps ${nc}: ${JSON.stringify([ra, rc])}`).toBe(
            false,
          );
        }
      }
      const gb = await gear.boundingBox();
      const top = await hit(gb);
      expect(top.href, JSON.stringify(top)).not.toMatch(/\/workspace\/app\/home/);
      expect(top.label).toBe("Chat preferences");
      await page.touchscreen.tap(gb.x + gb.width / 2, gb.y + gb.height / 2);
      await expect(page).toHaveURL(/\/workspace\/app\/chat/);
      await expect(page.locator(".chat-settings-layer:visible").first()).toBeVisible({
        timeout: 20000,
      });
    });
  }
});
