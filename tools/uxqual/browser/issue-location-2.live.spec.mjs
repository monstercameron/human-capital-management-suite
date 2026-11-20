import { test, expect } from "@playwright/test";

// Live checks for #3676 (batched location/reader requests) and #58 (location
// settings load for the administrator). Read-only: no seeded data is changed.

test.setTimeout(120000);

async function signIn(page, persona = /^Continue as Rafael Torres/) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: persona }).first().click();
  await page.waitForURL(/\/workspace\/app\//);
}

test("#3676 opening a conversation sends batched location and reader requests", async ({
  page,
}) => {
  await signIn(page);
  const locationPosts = [];
  const readerGets = [];
  page.on("request", (request) => {
    const url = request.url();
    if (request.method() === "POST" && url.includes("/api/chat/locations/v1/message")) {
      locationPosts.push(url);
    }
    if (url.includes("/api/chat/renderings/v1/reader")) {
      readerGets.push(url);
    }
  });
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  const messages = page.locator(".message[data-message-id]");
  await expect(messages.first()).toBeVisible({ timeout: 45000 });
  await page.waitForLoadState("networkidle", { timeout: 45000 }).catch(() => {});
  await page.waitForTimeout(3000);
  const shown = await messages.count();
  // One batched request per page of messages, never one per message.
  expect(locationPosts.length).toBeLessThanOrEqual(2);
  if (shown > 3) {
    expect(locationPosts.length).toBeLessThan(shown);
  }
  // The reader request is not restarted by unrelated rerenders.
  expect(readerGets.length).toBeLessThanOrEqual(2);
});

test("#58 Location sharing settings load with controls for the administrator", async ({
  page,
}) => {
  await signIn(page);
  const statuses = [];
  page.on("response", (response) => {
    if (response.url().includes("/api/chat/locations/v1/settings")) {
      statuses.push(response.status());
    }
  });
  await page.goto("/workspace/app/admin/chat-settings?locale=en-US");
  const heading = page.getByText(/Location sharing/i).first();
  await expect(heading).toBeVisible({ timeout: 45000 });
  await page.waitForTimeout(3000);
  await expect(
    page.getByText("Location settings could not load or save. Try again."),
  ).toHaveCount(0);
  expect(statuses.length).toBeGreaterThan(0);
  expect(statuses.every((status) => status === 200)).toBe(true);
  const panel = heading.locator("xpath=ancestor::*[self::section or self::div][1]");
  const controls = page.locator(
    "input[type=checkbox], select, [role=switch], [role=checkbox]",
  );
  expect(await controls.count()).toBeGreaterThan(0);
  await expect(panel).toBeVisible();
});
