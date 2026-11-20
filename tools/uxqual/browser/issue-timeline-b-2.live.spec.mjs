/* global getComputedStyle, URL */
import { test, expect } from "@playwright/test";

// Timeline fixes against the live dev cell: accent text in the dark theme
// (#3769), the reading width with a side panel open (#3771) and the shared work
// card (#3777). Writes happen only in channels created here.

test.setTimeout(240000);

async function signIn(page, persona = "Rafael Torres") {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: `Continue as ${persona}` }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function createChannel(page, name, query = "locale=en-US") {
  await page.goto(`/workspace/app/chat?${query}`);
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.waitForTimeout(3000);
  const dialog = page.locator(".create-dialog");
  for (let i = 0; i < 6 && (await dialog.count()) === 0; i++) {
    await page
      .locator("[data-action='open-create']")
      .first()
      .click({ timeout: 5000 })
      .catch(() => {});
    await dialog.waitFor({ timeout: 4000 }).catch(() => {});
  }
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(dialog).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
}

async function post(page, text) {
  const input = page.locator("textarea.composer-input").first();
  await input.fill(text);
  await input.press("Enter");
  await page.waitForTimeout(2000);
}

// Contrast of an element's text against what it sits on, read from the page.
async function contrastOf(locator) {
  return locator.evaluate((el) => {
    const px = (css) => {
      const c = document.createElement("canvas");
      c.width = c.height = 1;
      const g = c.getContext("2d", { willReadFrequently: true });
      g.clearRect(0, 0, 1, 1);
      g.fillStyle = "#000";
      g.fillStyle = css;
      g.fillRect(0, 0, 1, 1);
      const d = g.getImageData(0, 0, 1, 1).data;
      return [d[0], d[1], d[2], d[3] / 255];
    };
    const layers = [];
    for (let n = el; n; n = n.parentElement) {
      const bg = px(getComputedStyle(n).backgroundColor);
      if (bg[3] > 0) layers.push(bg);
      if (bg[3] >= 1) break;
    }
    let base = [255, 255, 255];
    for (const l of layers.reverse()) {
      base = base.map((v, i) => l[i] * l[3] + v * (1 - l[3]));
    }
    const fg = px(getComputedStyle(el).color);
    const lum = (c) => {
      const f = c.map((v) => {
        v /= 255;
        return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * f[0] + 0.7152 * f[1] + 0.0722 * f[2];
    };
    const a = lum(fg.slice(0, 3));
    const b = lum(base);
    return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
  });
}

function uniqueName() {
  return `zz-timeline-b-${Math.random().toString(36).slice(2, 8)}`;
}

test.describe("dark theme", () => {
  test.use({ colorScheme: "dark", viewport: { width: 1440, height: 900 } });

  test("#3769 accent links on messages and cards meet 4.5:1 in the dark theme", async ({
    page,
  }) => {
    const name = uniqueName();
    await signIn(page);
    await createChannel(page, name);
    const mark = `root ${name}`;
    await post(page, mark);
    await post(page, `/todo "Chores ${name}" 1="Sweep"`);
    const root = page.locator(".message[data-message-id]", { hasText: mark }).first();
    await expect(root).toBeVisible({ timeout: 30000 });
    await root.hover();
    await root.getByRole("button", { name: /reply in thread/i }).click();
    const input = page.locator("textarea.composer-input").last();
    await input.fill(`answer ${name}`);
    await input.press("Enter");
    const replies = page.getByRole("button", { name: /\d+ repl(y|ies)/i }).first();
    await expect(replies).toBeVisible({ timeout: 30000 });
    expect(await contrastOf(replies)).toBeGreaterThanOrEqual(4.5);

    const links = page.locator(".chatcmd002-link");
    expect(await links.count()).toBeGreaterThan(0);
    for (const link of await links.all()) {
      expect(await contrastOf(link)).toBeGreaterThanOrEqual(4.5);
    }
  });
});

for (const width of [1280, 1440]) {
  test.describe(`width ${width}`, () => {
    test.use({ viewport: { width, height: 900 } });

    test(`#3771 icon rail by default, remembered, readable column with a panel open at ${width}`, async ({
      page,
    }) => {
      const name = uniqueName();
      // Rafael's stored choice is changed by other specs; Darius keeps the default.
      await signIn(page, "Darius Bennett");
      await createChannel(page, name);
      const text = `a long message to read ${name}`;
      await post(page, text);

      // Product navigation is the icon rail inside Chat.
      const rail = page.locator(".sidebar").first();
      expect((await rail.boundingBox()).width).toBeLessThanOrEqual(80);

      // Open a side panel and measure the message column.
      const msg = page.locator(".message[data-message-id]", { hasText: text }).first();
      await msg.hover();
      await msg.getByRole("button", { name: /reply in thread/i }).click();
      await page.waitForTimeout(1000);
      const columns = await page.evaluate(() => {
        const l = document.querySelector(".chat-layout");
        return getComputedStyle(l).gridTemplateColumns.split(" ").map(parseFloat);
      });
      const message = (await msg.boundingBox()).width;
      // The middle track is the reading column; a message row sits inside its padding.
      expect(
        columns[1],
        `reading column at ${width}: ${columns}`,
      ).toBeGreaterThanOrEqual(600);
      expect(message, `message row at ${width}`).toBeGreaterThanOrEqual(560);

      // Expanding is possible and remembered across a reload.
      await page.getByRole("link", { name: "Expand navigation" }).click();
      await page.waitForLoadState("domcontentloaded");
      await page.reload();
      await page
        .locator(".message[data-message-id]")
        .first()
        .waitFor({ timeout: 80000 });
      expect(
        (await page.locator(".sidebar").first().boundingBox()).width,
      ).toBeGreaterThan(150);
      // Put the persona back on the default for other specs.
      await page.getByRole("link", { name: "Collapse navigation" }).click();
      await page.waitForURL(/nav=collapsed/);
      await page.waitForTimeout(4000);
    });
  });
}

test.describe("work cards", () => {
  test.use({ colorScheme: "dark", viewport: { width: 1440, height: 900 } });

  test("#3777 polls and documents share the work card; a poll waiting on you stands out", async ({
    page,
  }) => {
    const name = uniqueName();
    await signIn(page);
    // The demo tenant has no listed documents; a document reference to an id the
    // reader cannot open renders the locked state of the same card.
    const docUrl = `${new URL(page.url()).origin}/workspace/app/docs?document=11111111-2222-4333-8444-555555555555`;
    await createChannel(page, name);
    await post(page, `/poll "Lunch ${name}?" 1="Pizza" 2="Salad"`);
    await post(page, `see ${docUrl}`);

    const poll = page.locator(".chat-work-card[data-card='poll']").first();
    await expect(poll).toBeVisible({ timeout: 30000 });
    await expect(poll).toHaveAttribute("data-waits", "you");
    await expect(poll).toContainText(/waiting on you/i);
    expect(await poll.getAttribute("aria-label")).toMatch(/waiting on you/i);
    const waitingBorder = await poll.evaluate((e) => getComputedStyle(e).borderColor);

    const doc = page
      .locator(".chat-work-card", { has: page.locator(".chat-work-card-type") })
      .filter({ hasNot: page.locator("[data-card]") })
      .first();
    await expect(doc).toBeVisible({ timeout: 30000 });
    await expect(doc.locator(".chat-work-card-type svg").first()).toBeVisible();
    const docBorder = await doc.evaluate((e) => getComputedStyle(e).borderColor);
    expect(docBorder).not.toBe(waitingBorder);
    expect(await doc.getAttribute("data-waits")).toBeNull();

    // Voting clears the waiting state.
    await poll.getByRole("button", { name: /Vote: Pizza/ }).click();
    await expect(poll).not.toHaveAttribute("data-waits", "you", { timeout: 20000 });

    // Phone width keeps both cards inside the screen.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForTimeout(800);
    for (const card of [poll, doc]) {
      const box = await card.boundingBox();
      expect(box.x + box.width).toBeLessThanOrEqual(391);
    }
  });
});
