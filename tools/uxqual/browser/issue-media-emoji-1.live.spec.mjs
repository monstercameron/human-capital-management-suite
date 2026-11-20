import { Buffer } from "node:buffer";
import { test, expect } from "@playwright/test";

const onePNG =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
const oneGIF = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
const composerSel = "textarea[name='message']";

// Chat issue sweep, lane media-emoji chunk 1: #3880 avatar focus ring,
// #3853 viewer focus return, #3852 viewer position label.

test.setTimeout(420000);
test.use({ viewport: { width: 1440, height: 900 } });

async function open(page, channel) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto(`/workspace/app/chat?locale=en-US#channel=${channel}`);
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 45000,
  });
}

test("#3880 message avatar button hugs the avatar instead of stretching", async ({
  page,
}) => {
  await open(page, "general");
  const sizes = await page.evaluate(() => {
    const out = [];
    for (const row of document.querySelectorAll(".message[data-message-id]")) {
      const button = row.querySelector(".person-avatar-button");
      if (!button) continue;
      const b = button.getBoundingClientRect();
      const r = row.getBoundingClientRect();
      const avatar = button.querySelector(".avatar, img, span");
      const a = (avatar || button).getBoundingClientRect();
      out.push({ bw: b.width, bh: b.height, aw: a.width, ah: a.height, rh: r.height });
    }
    return out;
  });
  expect(sizes.length).toBeGreaterThan(0);
  for (const s of sizes) {
    expect(s.bh).toBeLessThanOrEqual(s.ah + 4);
    expect(s.bw).toBeLessThanOrEqual(s.aw + 4);
  }
  // At least one row is clearly taller than the avatar, so the check bites.
  expect(sizes.some((s) => s.rh > s.ah + 20)).toBe(true);
  // Blank space well below the avatar (beside a tall message) is not part of
  // the button, on desktop and on a phone; the phone keeps its 44 px hit area.
  const probe = () =>
    page.evaluate(() => {
      const button = document.querySelector(".message .person-avatar-button");
      button.scrollIntoView({ block: "center" });
      const r = button.getBoundingClientRect();
      const cx = r.left + r.width / 2;
      const at = (y) => {
        const el = document.elementFromPoint(cx, y);
        return !!(el && el.closest(".person-avatar-button"));
      };
      return {
        inside: at(r.top + 5),
        near: at(r.bottom + 5),
        far: at(r.bottom + 40),
        width: r.width,
        height: r.height,
      };
    });
  const desktop = await probe();
  expect(desktop.inside).toBe(true);
  expect(desktop.far).toBe(false);
  expect(Math.min(desktop.width, desktop.height)).toBeGreaterThanOrEqual(24);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".message .person-avatar-button").first()).toBeAttached();
  const phone = await probe();
  const area = await page.evaluate(() => {
    const button = document.querySelector(".message .person-avatar-button");
    const after = window.getComputedStyle(button, "::after");
    return {
      position: after.position,
      minWidth: parseFloat(after.minWidth),
      minHeight: parseFloat(after.minHeight),
    };
  });
  expect(area.position).toBe("absolute");
  expect(area.minWidth).toBeGreaterThanOrEqual(44);
  expect(area.minHeight).toBeGreaterThanOrEqual(44);
  // On a phone the button follows the 28 px avatar, not the row.
  expect(phone.width).toBeLessThanOrEqual(32);
  expect(phone.height).toBeLessThanOrEqual(32);
});

async function ownChannel(page) {
  const name = `zz-mediaemoji1-${Math.random().toString(36).slice(2, 7)}`;
  await open(page, "general");
  await page
    .getByRole("button", { name: /New conversation/i })
    .first()
    .click();
  const dialog = page.locator(".create-dialog");
  await dialog.getByRole("radio", { name: /Public channel/ }).click();
  await dialog.getByRole("textbox").first().fill(name);
  await dialog.locator("button[type=submit]").click();
  await expect(page.locator(composerSel)).toHaveAttribute(
    "placeholder",
    new RegExp(name),
    { timeout: 30000 },
  );
  return name;
}

async function sendImage(page, file, mimeType, b64, expectedCount) {
  await page.locator(composerSel).fill(`zz picture ${file}`);
  const uploaded = page.waitForResponse((r) => /media\/upload/.test(r.url()), {
    timeout: 60000,
  });
  await page
    .locator('input[type="file"][multiple]')
    .setInputFiles([{ name: file, mimeType, buffer: Buffer.from(b64, "base64") }]);
  await uploaded;
  await page.waitForTimeout(1000);
  await page.locator(".chat-composer .send-button:not(:disabled)").click();
  await expect(page.locator(".message-list .attachment-image-open")).toHaveCount(
    expectedCount,
    { timeout: 60000 },
  );
}

const newline = String.fromCharCode(10);

async function sendText(page, text) {
  await page.locator(composerSel).fill(text);
  await page.locator(composerSel).press("Enter");
  await expect(
    page.locator(".message[data-message-id]", {
      hasText: text.split(String.fromCharCode(10))[0],
    }),
  ).toBeVisible({ timeout: 30000 });
}

// An image, a tall text message, then two more images: the later images
// are well below the first one's viewport.
async function seedGallery(page) {
  const name = await ownChannel(page);
  await sendImage(page, "zz-first.png", "image/png", onePNG, 1);
  const tall = Array.from({ length: 30 }, (_, i) => `line ${i}`).join(newline);
  for (let i = 0; i < 1; i++) await sendText(page, `zz-filler-${i}${newline}${tall}`);
  await sendImage(page, "zz-second.gif", "image/gif", oneGIF, 2);
  await sendImage(page, "zz-third.png", "image/png", onePNG, 3);
  const thumbs = page.locator(".message-list .attachment-image-open");
  await expect(thumbs).toHaveCount(3, { timeout: 30000 });
  return name;
}

const thumbState = () => {
  const el = document.activeElement;
  const r = el.getBoundingClientRect();
  const all = [...document.querySelectorAll(".message-list .attachment-image-open")];
  return {
    isThumb: el.matches(".attachment-image-open"),
    index: all.indexOf(el),
    top: r.top,
    bottom: r.bottom,
    vh: window.innerHeight,
    listTop: document.querySelector(".message-list").getBoundingClientRect().top,
    scrollTop:
      (document.scrollingElement || document.body).scrollTop +
      document.querySelector(".message-list").scrollTop,
  };
};

test("#3853 closing the viewer after stepping returns focus to the opening thumbnail", async ({
  page,
}) => {
  await open(page, "general");
  await seedGallery(page);
  const thumbs = page.locator(".message-list .attachment-image-open");
  // The first picture is the one that opens the viewer; bring it on screen.
  await expect
    .poll(async () => {
      await thumbs.first().evaluate((el) => {
        el.scrollIntoView({ block: "center" });
        el.focus({ preventScroll: true });
      });
      const state = await page.evaluate(thumbState);
      return state.isThumb && state.top >= 0 && state.bottom <= state.vh;
    })
    .toBe(true);
  const before = await page.evaluate(thumbState);
  await page.keyboard.press("Enter");
  const viewer = page.locator(".chat-image-viewer");
  await expect(viewer).toBeVisible();
  const next = viewer.locator(".chat-image-viewer-next");
  await next.click();
  await next.click();
  await expect(next).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(viewer).toBeHidden();
  const active = await page.evaluate(thumbState);
  expect(active.isThumb).toBe(true);
  expect(active.index).toBe(0);
  // The reader's place is kept and focus is on screen, inside the list.
  expect.soft(Math.abs(active.scrollTop - before.scrollTop)).toBeLessThan(5);
  expect(active.top).toBeGreaterThanOrEqual(0);
  expect(active.bottom).toBeLessThanOrEqual(active.vh);
});

test("#3853 direct open then Escape returns to the same thumbnail", async ({
  page,
}) => {
  await open(page, "general");
  await seedGallery(page);
  const thumbs = page.locator(".message-list .attachment-image-open");
  await thumbs.nth(1).scrollIntoViewIfNeeded();
  await thumbs.nth(1).focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".chat-image-viewer")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".chat-image-viewer")).toBeHidden();
  const active = await page.evaluate(thumbState);
  expect(active.isThumb).toBe(true);
  expect(active.index).toBe(1);
});

test("#3852 position label does not imply the loaded images are the whole conversation", async ({
  page,
}) => {
  await open(page, "general");
  await seedGallery(page);
  const thumbs = page.locator(".message-list .attachment-image-open");
  await thumbs.first().scrollIntoViewIfNeeded();
  await thumbs.first().focus();
  await page.keyboard.press("Enter");
  const viewer = page.locator(".chat-image-viewer");
  await expect(viewer).toBeVisible();
  const text = await viewer.locator(".chat-image-viewer-caption").innerText();
  const partial = await page.evaluate(
    () =>
      !!document.querySelector(
        ".message-list .load-older, .message-list .load-newer",
      ) ||
      [...document.querySelectorAll(".message-list [data-virtual-spacer]")].some(
        (e) => e.offsetHeight > 0,
      ),
  );
  if (partial) {
    expect(text).toMatch(/\d+ of \d+ loaded images/);
    // Expected also asks for a way to load more from the viewer when limited.
    await expect(
      viewer.getByRole("button", { name: /load (more|older)/i }),
    ).toBeVisible();
  } else {
    // Every image of this short conversation is loaded: a plain count.
    expect(text).toMatch(/1 of 3/);
    expect(text).not.toMatch(/loaded images/);
  }
});
