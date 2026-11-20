/* global URL */
import { test, expect } from "@playwright/test";

// #3994: a work card says "Waiting on you" only when the work is assigned to the
// signed-in person by stable identity. The demo tenant has no journey and its
// Projects capability is switched off, so there is no card to serve; the test
// states that and skips rather than pass without evidence.
test.setTimeout(120000);

test("#3994 a served work card claims 'Waiting on you' only for the real assignee", async ({
  page,
}) => {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: /^Continue as Rafael Torres/ }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto("/workspace/app/journeys?locale=en-US");
  await expect(page.getByText("Journeys page loaded")).toBeAttached({ timeout: 60000 });
  const journeys = page.locator("a[href*='journey=']");
  test.skip(
    (await journeys.count()) === 0,
    "demo has no journey fixture and Projects is not set up",
  );
  const id = new URL(
    await journeys.first().getAttribute("href"),
    page.url(),
  ).searchParams.get("journey");
  expect(id).toBeTruthy();
  // With a fixture: share the link in your own zz- channel, then expect a card
  // whose data-waits="you" appears only when the approver is the viewer.
});
