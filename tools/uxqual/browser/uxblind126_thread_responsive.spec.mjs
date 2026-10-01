// UXBLIND-126: prove the hydrated thread layout keeps provenance and actions
// readable at the two narrow phone widths from the acceptance scan.
import { test, expect } from "@playwright/test";

async function signInAsWalt(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForLoadState("networkidle");
}

async function gotoChat(page) {
  await page.goto("/workspace/app/chat#channel=engineering");
  await page.waitForLoadState("networkidle");
  await expect(
    page.locator('.chat-rail-row [data-action="select"]').first(),
  ).toBeVisible({
    timeout: 20000,
  });
  await expect(page.locator(".conversation-heading h1")).toHaveText("engineering", {
    timeout: 20000,
  });
  await page.waitForTimeout(600);
}

function overlaps(a, b) {
  return (
    a.left < b.right - 1 &&
    a.right > b.left + 1 &&
    a.top < b.bottom - 1 &&
    a.bottom > b.top + 1
  );
}

async function readThreadGeometry(page) {
  return page.evaluate(() => {
    const rect = (element) => {
      const box = element.getBoundingClientRect();
      return {
        left: box.left,
        right: box.right,
        top: box.top,
        bottom: box.bottom,
        width: box.width,
        height: box.height,
      };
    };
    const metadata = [
      ...document.querySelectorAll(
        ".thread-root .message-meta, .thread-message .message-meta",
      ),
    ].map((meta) => ({
      children: [...meta.children]
        .filter((child) => {
          const box = child.getBoundingClientRect();
          return (
            getComputedStyle(child).display !== "none" &&
            box.width > 0 &&
            box.height > 0
          );
        })
        .map(rect),
      time: (() => {
        const element = meta.querySelector(".message-time");
        return element
          ? {
              ...rect(element),
              lineHeight: parseFloat(getComputedStyle(element).lineHeight),
            }
          : null;
      })(),
    }));
    const view = document.querySelector(".thread-view-in-channel");
    return {
      rootTime: (() => {
        const element = document.querySelector(".thread-root .message-time");
        return element ? rect(element) : null;
      })(),
      view: view ? rect(view) : null,
      metadata,
    };
  });
}

test("TestTodo_UXBLIND_126_Browser thread provenance reflows at 320px and 390px", async ({
  page,
}) => {
  test.setTimeout(120000);
  await signInAsWalt(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await gotoChat(page);

  const message = page
    .locator("article.message")
    .filter({ has: page.locator(".message-stats") })
    .last();
  await expect(message).toBeVisible({ timeout: 20000 });
  await message.locator(".message-stats").first().click();
  await expect(page.locator(".thread-root")).toBeVisible({ timeout: 15000 });
  await expect(
    page.getByRole("button", { name: "View in channel", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".thread-message").first()).toBeVisible({ timeout: 15000 });

  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 700 });
    await expect(page.locator(".thread-root")).toBeVisible();
    const geometry = await readThreadGeometry(page);
    expect(geometry.rootTime, `${width}px root timestamp is missing`).toBeTruthy();
    expect(geometry.view, `${width}px View in channel is missing`).toBeTruthy();
    expect(
      overlaps(geometry.rootTime, geometry.view),
      `${width}px root timestamp overlaps View in channel`,
    ).toBe(false);
    for (const [index, metadata] of geometry.metadata.entries()) {
      expect(
        metadata.time,
        `${width}px metadata ${index} timestamp is missing`,
      ).toBeTruthy();
      expect(
        metadata.time.height,
        `${width}px metadata ${index} timestamp wraps`,
      ).toBeLessThanOrEqual(metadata.time.lineHeight * 1.25);
      for (let i = 0; i < metadata.children.length; i++) {
        for (let j = i + 1; j < metadata.children.length; j++) {
          expect(
            overlaps(metadata.children[i], metadata.children[j]),
            `${width}px metadata ${index} children overlap`,
          ).toBe(false);
        }
      }
    }
  }

  await page.getByRole("button", { name: "Close thread", exact: true }).click();
  await expect(page.locator(".thread-root")).toHaveCount(0);
});
