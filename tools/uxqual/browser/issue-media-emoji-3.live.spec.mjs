import { test, expect } from "@playwright/test";

// Live check for #3770: each person has a stable palette colour behind their
// initials, in a round tile, in both themes. Read-only: nothing is posted.

test.setTimeout(120000);
test.use({ viewport: { width: 1440, height: 900 } });

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function readAvatars(page, locale) {
  const all = [];
  for (const channel of ["engineering"]) {
    all.push(...(await readChannel(page, locale, channel)));
  }
  return all;
}

async function readChannel(page, locale, channel) {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  await page.reload();
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 40000 });
  await page.locator("[data-avatar-hue]").first().waitFor({ timeout: 20000 });
  return page.evaluate(() =>
    [...document.querySelectorAll(".message [data-avatar-hue]")].map((el) => {
      const cs = window.getComputedStyle(el);
      const msg = el.closest(".message");
      return {
        hue: el.getAttribute("data-avatar-hue"),
        text: el.textContent.trim(),
        radius: cs.borderTopLeftRadius,
        width: el.getBoundingClientRect().width,
        bg: cs.backgroundColor,
        color: cs.color,
        author: msg
          ? (msg
              .querySelector("[class*='author'], .message-author")
              ?.textContent?.trim() ?? "")
          : "",
      };
    }),
  );
}

function checkDistinct(avatars) {
  expect(avatars.length).toBeGreaterThan(2);
  const byText = new Map();
  for (const a of avatars) {
    // Same person (same initials) keeps one hue and one colour.
    const prev = byText.get(a.text);
    if (prev) {
      expect(a.hue).toBe(prev.hue);
      expect(a.bg).toBe(prev.bg);
    }
    byText.set(a.text, a);
    expect(Number(a.hue)).toBeGreaterThanOrEqual(1);
    expect(a.radius).not.toBe("0px");
  }
  const people = [...byText.values()];
  expect(people.length).toBeGreaterThan(1);
  // More than one distinct colour among different people.
  expect(new Set(people.map((p) => p.bg)).size).toBeGreaterThan(1);
  // Not the old single teal for everyone, and ink differs from background.
  for (const p of people) expect(p.bg).not.toBe(p.color);
}

test("#3770 people get a stable per-person round colour tile (light)", async ({
  browser,
}) => {
  const context = await browser.newContext({
    colorScheme: "light",
    viewport: { width: 1440, height: 900 },
  });
  const page = await context.newPage();
  await signIn(page);
  const first = await readAvatars(page, "en-US");
  checkDistinct(first);
  // Stable across a reload.
  const second = await readAvatars(page, "en-US");
  expect(second.map((a) => [a.text, a.hue])).toEqual(first.map((a) => [a.text, a.hue]));
  await context.close();
});

test("#3770 the palette holds in dark, differs from light, and in Arabic", async ({
  browser,
}) => {
  const light = await browser.newContext({
    colorScheme: "light",
    viewport: { width: 1440, height: 900 },
  });
  const lp = await light.newPage();
  await signIn(lp);
  const lightAv = await readAvatars(lp, "en-US");
  await light.close();

  const dark = await browser.newContext({
    colorScheme: "dark",
    viewport: { width: 1440, height: 900 },
  });
  const dp = await dark.newPage();
  await signIn(dp);
  const darkAv = await readAvatars(dp, "en-US");
  checkDistinct(darkAv);
  expect(darkAv.map((a) => a.bg)).not.toEqual(lightAv.map((a) => a.bg));
  const arabic = await readAvatars(dp, "ar");
  checkDistinct(arabic);
  await dark.close();
});

test("#3770 phone keeps the person colours", async ({ browser }) => {
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });
  const page = await context.newPage();
  await signIn(page);
  checkDistinct(await readAvatars(page, "en-US"));
  await context.close();
});
