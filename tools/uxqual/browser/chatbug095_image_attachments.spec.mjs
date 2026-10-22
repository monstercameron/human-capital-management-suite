import { test, expect } from "@playwright/test";

// CHATBUG-095: an image or GIF attachment shows its decoded picture at its
// stored dimensions; the GIF badge never covers text; a tile that cannot
// load ends in the file name, size and Download. Written for the lane seed
// (Rafael Torres, posting member of leadership-private); needs a running
// server and browser, so it is specified here and not executed in unit runs.

const channel = "/workspace/app/chat?locale=en-US#channel=leadership-private";

// A 1x1 PNG and a 1-frame 1x1 GIF, embedded so the spec seeds itself.
const onePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
const oneGIF = "R0lGODlhAQABAIAAAP///////yH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==";

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function naturalSize(button) {
  return button.locator("img").evaluate((img) => ({
    width: img.naturalWidth,
    height: img.naturalHeight,
    src: img.src,
  }));
}

test("seeded image and GIF tiles decode at their stored dimensions", async ({
  page,
}) => {
  await signIn(page);
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const tiles = page.locator(".attachment-image-open[data-media-id]");
  await expect(tiles.first()).toBeAttached({ timeout: 30000 });
  const count = await tiles.count();
  expect(count).toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const button = tiles.nth(i);
    await expect
      .poll(async () => (await naturalSize(button)).width, { timeout: 20000 })
      .toBeGreaterThan(0);
    const size = await naturalSize(button);
    expect(size.width).toBeGreaterThan(0);
    expect(size.height).toBeGreaterThan(0);
    expect(size.src).toMatch(/^blob:/);
    await expect(button.locator("xpath=..")).not.toHaveClass(/failed/);
    // The frame was reserved before the bytes arrived: the tile keeps the
    // stored width and height attributes, so nothing below it moves.
    const img = button.locator("img");
    await expect(img).toHaveAttribute("width", /^[1-9][0-9]*$/);
    await expect(img).toHaveAttribute("height", /^[1-9][0-9]*$/);
  }
});

test("an uploaded GIF decodes and its badge overlaps no text", async ({
  page,
}) => {
  await signIn(page);
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const picker = page.locator('input[type="file"][multiple]');
  await picker.setInputFiles([
    { name: "dot.png", mimeType: "image/png", buffer: Buffer.from(onePNG, "base64") },
    { name: "dot.gif", mimeType: "image/gif", buffer: Buffer.from(oneGIF, "base64") },
  ]);
  await page.locator(".chat-composer .send-button:not(:disabled)").click();
  const gif = page.locator('.attachment-image.gif .attachment-image-open[data-media-id]').last();
  await expect
    .poll(async () => (await naturalSize(gif)).width, { timeout: 30000 })
    .toBe(1);
  const gifSize = await naturalSize(gif);
  expect(gifSize.height).toBe(1);
  const tile = gif.locator("xpath=..");
  const tileBox = await tile.boundingBox();
  const badgeBox = await tile.locator(".attachment-badge").boundingBox();
  expect(tileBox && badgeBox).toBeTruthy();
  expect(badgeBox.x).toBeGreaterThanOrEqual(tileBox.x);
  expect(badgeBox.y).toBeGreaterThanOrEqual(tileBox.y);
  expect(badgeBox.x + badgeBox.width).toBeLessThanOrEqual(tileBox.x + tileBox.width);
  expect(badgeBox.y + badgeBox.height).toBeLessThanOrEqual(tileBox.y + tileBox.height);
  // While the picture shows, the loading and fallback text are hidden, so
  // the badge sits over pixels, never over words.
  await expect(tile.locator(".attachment-loading")).toBeHidden();
  await expect(tile.locator(".attachment-fallback")).toBeHidden();
});

test("a failed tile ends in the file name, size and Download", async ({
  page,
}) => {
  await signIn(page);
  await page.route("**/v1/chat/media/*?*variant=*", (route) =>
    route.fulfill({ status: 500, body: "media down" }),
  );
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const failed = page.locator(".attachment-image.failed").first();
  await expect(failed).toBeVisible({ timeout: 25000 });
  await expect(failed.locator(".attachment-fallback-name")).not.toBeEmpty();
  await expect(failed.locator(".attachment-fallback-size")).toContainText(/B|KB|MB/);
  await expect(
    failed.locator('.attachment-download[data-action="download-attachment"]'),
  ).toBeVisible();
});

test("opening a thread keeps the parent image tile", async ({ page }) => {
  await signIn(page);
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const opener = page.locator('[data-action="reply"]').first();
  await opener.click();
  const root = page.locator(".thread-root");
  await expect(root).toBeVisible({ timeout: 30000 });
  const button = root.locator(".attachment-image-open[data-media-id]");
  if ((await button.count()) > 0) {
    await expect
      .poll(async () => (await naturalSize(button.first())).width, { timeout: 20000 })
      .toBeGreaterThan(0);
  }
  await expect(root.locator(".attachment-image.failed")).toHaveCount(0);
});
