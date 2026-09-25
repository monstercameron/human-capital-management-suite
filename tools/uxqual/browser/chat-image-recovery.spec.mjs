/* global setTimeout */

import { test, expect } from "@playwright/test";

const channel = "/workspace/app/chat?locale=en-US#channel=leadership-private";
const visibleImage = "c2a8929ab11dcfe75b9ace09b4fb2fae6943035c4350a213db6e440a192b5baf";

test.setTimeout(90000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("a visible Chat thumbnail recovers after a temporary media failure", async ({
  page,
}) => {
  await signIn(page);
  let firstRequest = true;
  const statuses = [];
  await page.route(
    `**/v1/chat/media/${visibleImage}?*variant=thumbnail*`,
    async (route) => {
      if (firstRequest) {
        firstRequest = false;
        await route.fulfill({ status: 503, body: "temporary media failure" });
      } else {
        await route.continue();
      }
    },
  );
  page.on("response", (response) => {
    if (
      response.url().includes(`/v1/chat/media/${visibleImage}?`) &&
      response.url().includes("variant=thumbnail")
    ) {
      statuses.push(response.status());
    }
  });

  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const button = page.locator(
    `.attachment-image-open[data-media-id="${visibleImage}"]`,
  );
  await expect(button).toBeAttached({ timeout: 30000 });
  await expect
    .poll(() => button.locator("img").evaluate((img) => img.naturalWidth), {
      timeout: 20000,
    })
    .toBeGreaterThan(0);
  await expect(button.locator("img")).toHaveAttribute("src", /^blob:/);
  await expect(button.locator("xpath=..")).not.toHaveClass(/failed/);
  expect(statuses).toEqual([503, 200]);
});

test("a thumbnail upgrades in the viewer and reloads after switching channels", async ({
  page,
}) => {
  await signIn(page);
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const button = page.locator(
    `.attachment-image-open[data-media-id="${visibleImage}"]`,
  );
  await expect
    .poll(() => button.locator("img").evaluate((img) => img.naturalWidth), {
      timeout: 20000,
    })
    .toBeGreaterThan(0);
  await button.click();
  await expect(
    page.locator(".chat-image-viewer-original.chat-image-original-ready"),
  ).toBeVisible({ timeout: 20000 });
  await expect
    .poll(() =>
      page.locator(".chat-image-viewer-original").evaluate((img) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  await page.locator(".chat-image-viewer-close").click();

  await page
    .locator('.chat-rail-row [data-action="select"]', { hasText: "general" })
    .first()
    .click();
  await expect(page.locator(".conversation-heading h1")).toHaveText("general");
  await page
    .locator('.chat-rail-row [data-action="select"]', { hasText: "leadership-private" })
    .first()
    .click();
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
  );
  await expect
    .poll(() => button.locator("img").evaluate((img) => img.naturalWidth), {
      timeout: 20000,
    })
    .toBeGreaterThan(0);
  await expect(button.locator("xpath=..")).not.toHaveClass(/failed/);
});

test("a stalled thumbnail request recovers at the watchdog deadline", async ({
  page,
}) => {
  await signIn(page);
  let attempts = 0;
  await page.route(
    `**/v1/chat/media/${visibleImage}?*variant=thumbnail*`,
    async (route) => {
      attempts++;
      if (attempts === 1) {
        await new Promise((resolve) => setTimeout(resolve, 30000));
        await route.abort().catch(() => {});
      } else {
        await route.continue();
      }
    },
  );
  await page.goto(channel);
  await expect(page.locator(".conversation-heading h1")).toHaveText(
    "leadership-private",
    { timeout: 30000 },
  );
  const button = page.locator(
    `.attachment-image-open[data-media-id="${visibleImage}"]`,
  );
  await expect
    .poll(() => button.locator("img").evaluate((img) => img.naturalWidth), {
      timeout: 23000,
    })
    .toBeGreaterThan(0);
  await expect(button.locator("xpath=..")).not.toHaveClass(/failed/);
  expect(attempts).toBe(2);
});
