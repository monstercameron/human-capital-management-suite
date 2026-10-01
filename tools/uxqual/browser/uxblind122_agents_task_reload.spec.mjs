// UXBLIND-122 regression: a completed synthetic task stays selected after a
// click, a document reload, and a first-load deep link in a fresh context.
// Run against the controlled local fake preview through HCMNEXT_DEV_URL.
// This journey is read-only and never starts a task or calls a model provider.
import { test, expect } from "@playwright/test";

test.setTimeout(90000);

async function signIn(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function waitForHydration(page) {
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
  await page.waitForFunction(
    () =>
      performance
        .getEntriesByType("mark")
        .some((mark) => mark.name === "hcm:load-done"),
    null,
    { timeout: 30000, polling: 50 },
  );
}

async function expectCompletedTask(page, taskID, title) {
  await waitForHydration(page);
  await expect(page.locator(".agents-page")).toBeVisible({ timeout: 30000 });
  await expect(page).toHaveURL(
    new RegExp(`[?&]task=${encodeURIComponent(taskID)}(?:&|$)`),
  );
  await expect(page.locator(".agents-task-view")).toHaveAttribute(
    "data-task-view",
    taskID,
  );
  await expect(page.locator("#agents-task-title")).toHaveText(title);
  const plan = page.locator(".agents-task-plan");
  await expect(
    plan.getByRole("heading", { name: "Confirmed plan", exact: true }),
  ).toBeVisible();
  await expect(plan.locator("li.agents-plan-step").first()).toBeVisible();
  const answer = page.locator(".agents-task-answer");
  await expect(
    answer.getByRole("heading", { name: "Answer", exact: true }),
  ).toBeVisible();
  await expect(answer.locator("p")).not.toBeEmpty();
}

test("[UXBLIND-122] completed task selection survives reload and a cold task deep link", async ({
  browser,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await signIn(page);
  await waitForHydration(page);
  await page.goto("/workspace/app/chat");
  await waitForHydration(page);
  await expect(page.locator(".chat-rail")).toBeVisible({ timeout: 30000 });

  const agentsLink = page.getByRole("link", { name: "Agents", exact: true });
  await expect(agentsLink).toBeVisible();
  await agentsLink.click();
  await expect(page.locator(".agents-page")).toBeVisible({ timeout: 30000 });

  const taskRow = page
    .locator('.agents-task-group[data-task-state="completed"] .agents-task-row')
    .first();
  await expect(taskRow).toBeVisible({ timeout: 30000 });
  const taskID = await taskRow.getAttribute("data-task-id");
  const taskTitle = (await taskRow.locator("strong").textContent())?.trim();
  expect(taskID, "the synthetic completed task must expose its task id").toBeTruthy();
  expect(taskTitle, "the synthetic completed task must have a title").toBeTruthy();

  const openTask = taskRow.getByRole("link", { name: "Open task", exact: true });
  const deepLink = new URL(await openTask.getAttribute("href"), page.url()).toString();
  await openTask.click();
  await expectCompletedTask(page, taskID, taskTitle);

  await page.reload({ waitUntil: "domcontentloaded" });
  await expectCompletedTask(page, taskID, taskTitle);

  const coldContext = await browser.newContext({
    viewport: { width: 1280, height: 900 },
  });
  try {
    const coldPage = await coldContext.newPage();
    await signIn(coldPage);
    await coldPage.goto(deepLink, { waitUntil: "domcontentloaded" });
    await expectCompletedTask(coldPage, taskID, taskTitle);
  } finally {
    await coldContext.close();
  }
});
