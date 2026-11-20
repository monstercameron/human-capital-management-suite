import { test, expect, devices } from "@playwright/test";

// Chat issue sweep, lane thread-b chunk 3: #3722 (focus returns to the opener
// when the thread closes) and #3598 (thread header keeps its title on phones).
// Runs against the dev cell (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default).

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openChannel(page, channel, locale = "en-US") {
  await page.goto(`/workspace/app/chat?locale=${locale}#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
}

async function describeActive(page) {
  return page.evaluate(() => {
    const el = document.activeElement;
    if (!el) return { tag: "none" };
    return {
      tag: el.tagName,
      action: el.getAttribute("data-action") || "",
      connected: el.isConnected,
    };
  });
}

test("#3722 closing the thread returns focus to the control that opened it", async ({
  page,
}) => {
  await signIn(page);
  await openChannel(page, "engineering");

  // Opener 1: the thread-count link.
  const link = page.locator("button.message-stats[data-action='stats']").last();
  await expect(link).toBeVisible({ timeout: 15000 });
  await link.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
  await page.getByRole("button", { name: "Close thread", exact: true }).click();
  await expect(page.locator(".thread-composer")).toHaveCount(0);
  await expect
    .poll(async () => (await describeActive(page)).tag, { timeout: 5000 })
    .not.toBe("BODY");
  let active = await describeActive(page);
  expect(active.action, "focus after closing via thread-count link").toBe("stats");

  // Opener 2: Reply in thread from the hover bar.
  const message = page.locator(".message[data-message-id]").last();
  const box = await message.boundingBox();
  await page.mouse.move(box.x + 50, box.y + 20);
  await page.mouse.move(box.x + 120, box.y + 25);
  const reply = message.locator("[data-action='reply']").first();
  await expect(reply).toBeVisible();
  const replyId = await message.getAttribute("data-message-id");
  const rb = await reply.boundingBox();
  await page.mouse.click(rb.x + rb.width / 2, rb.y + rb.height / 2);
  await expect(page.locator(".thread-composer").first()).toBeVisible({
    timeout: 15000,
  });
  await page.getByRole("button", { name: "Close thread", exact: true }).click();
  await expect(page.locator(".thread-composer")).toHaveCount(0);
  await expect
    .poll(async () => (await describeActive(page)).tag, { timeout: 5000 })
    .not.toBe("BODY");
  active = await describeActive(page);
  expect(active.connected).toBe(true);
  const owner = await page.evaluate(
    () =>
      document.activeElement.closest(".message")?.getAttribute("data-message-id") || "",
  );
  expect(owner, "focus returns inside the message that was replied to").toBe(replyId);
});

const phones = [
  { width: 320, height: 640 },
  { width: 390, height: 844 },
];
for (const locale of ["en-US", "de-DE", "ar"]) {
  for (const size of phones) {
    test(`#3598 thread header keeps its title readable (${locale} ${size.width}px)`, async ({
      browser,
    }) => {
      const context = await browser.newContext({
        viewport: size,
        hasTouch: true,
        isMobile: true,
        userAgent: devices["Pixel 7"].userAgent,
      });
      const page = await context.newPage();
      try {
        await signIn(page);
        await openChannel(page, "engineering", locale);
        const link = page.locator("button.message-stats[data-action='stats']").last();
        await expect(link).toBeAttached({ timeout: 15000 });
        // The list sits scrolled to its newest messages on a phone, so older
        // thread links are off screen: press the link through the DOM.
        await link.evaluate((el) => el.click());
        const head = page.locator(".thread-heading").first();
        await expect(head).toBeVisible({ timeout: 15000 });
        const geo = await head.evaluate((el) => {
          const box = (n) => {
            if (!n) return null;
            const r = n.getBoundingClientRect();
            return {
              left: r.left,
              right: r.right,
              top: r.top,
              bottom: r.bottom,
              w: r.width,
              h: r.height,
              clip: n.scrollWidth - n.clientWidth,
            };
          };
          const label = el.querySelector(".thread-back-label");
          return {
            title: box(el.querySelector("h2")),
            channel: box(el.querySelector(".thread-channel-link")),
            actions: [...el.querySelectorAll(".side-heading-actions button")].map(
              (b) => ({
                ...box(b),
                name: b.getAttribute("aria-label") || b.textContent.trim(),
              }),
            ),
            labelShown: label
              ? el.ownerDocument.defaultView.getComputedStyle(label).display
              : "none",
            dir: el.ownerDocument.defaultView.getComputedStyle(document.documentElement)
              .direction,
            vw: window.innerWidth,
            scrollW: document.documentElement.scrollWidth,
          };
        });
        // Title is whole: not clipped, wider than an ellipsis stub.
        expect(geo.title.clip, "title is clipped").toBeLessThanOrEqual(1);
        expect(geo.title.w).toBeGreaterThan(30);
        // Close label hidden on phones, but every action keeps a name.
        expect(geo.labelShown).toBe("none");
        expect(geo.actions.length).toBeGreaterThanOrEqual(2);
        for (const a of geo.actions) expect(a.name.length).toBeGreaterThan(0);
        // No overlaps between title, channel link and actions.
        const parts = [geo.title, geo.channel, ...geo.actions].filter(Boolean);
        for (let i = 0; i < parts.length; i++) {
          for (let j = i + 1; j < parts.length; j++) {
            const a = parts[i];
            const b = parts[j];
            const overlap =
              Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 &&
              Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1;
            expect(overlap, `header parts ${i} and ${j} overlap`).toBe(false);
          }
        }
        // Actions inside the viewport, no horizontal page scroll.
        for (const a of geo.actions) {
          expect(a.left).toBeGreaterThanOrEqual(-1);
          expect(a.right).toBeLessThanOrEqual(geo.vw + 1);
          expect(a.w).toBeGreaterThanOrEqual(32);
        }
        expect(geo.scrollW).toBeLessThanOrEqual(geo.vw + 1);
        // RTL order: in ar the title sits to the right of the actions.
        if (locale === "ar") {
          expect(geo.dir).toBe("rtl");
          expect(geo.title.left).toBeGreaterThan(
            Math.max(...geo.actions.map((a) => a.right)) - 2,
          );
        } else {
          expect(geo.title.right).toBeLessThan(
            Math.min(...geo.actions.map((a) => a.left)) + 2,
          );
        }
        // Escape closes the thread.
        await page.keyboard.press("Escape");
        await expect(page.locator(".thread-composer")).toHaveCount(0, {
          timeout: 5000,
        });
      } finally {
        await context.close();
      }
    });
  }
}
