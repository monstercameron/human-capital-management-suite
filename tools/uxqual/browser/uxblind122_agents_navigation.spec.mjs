// UXBLIND-122: the Agents sidebar entry and each task link must drive the
// hydrated product router, not merely render a plausible server-side href.
// This read-only journey uses the seeded Ironridge persona and does not start
// tasks or post chat messages.
import { test, expect } from "@playwright/test";

test("[UXBLIND-122] Chat -> Agents -> task keeps URL and selected detail in sync", async ({
  page,
}) => {
  const diagnostics = [];
  page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
  page.on("requestfailed", (request) =>
    diagnostics.push(
      `requestfailed: ${request.url()} ${request.failure()?.errorText ?? ""}`,
    ),
  );
  page.on("console", (message) => {
    if (message.type() === "error") diagnostics.push(`console: ${message.text()}`);
  });
  test.setTimeout(90000);

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );

  await page.goto("/workspace/app/chat");
  // Route-loader completion is not proof that the interactive tree mounted.
  // Give hydration a bounded window, then attach the boot state even when it
  // is missing so silent loader/bootstrap failures remain diagnosable.
  await page
    .waitForFunction(
      () =>
        performance
          .getEntriesByType("mark")
          .some((mark) => mark.name === "hcm:hydrated"),
      null,
      { timeout: 15000, polling: 50 },
    )
    .catch(() => {});
  await page
    .waitForFunction(
      () =>
        performance
          .getEntriesByType("mark")
          .some((mark) => mark.name === "hcm:load-done"),
      null,
      { timeout: 15000, polling: 50 },
    )
    .catch(() => {});
  const bootMarks = await page.evaluate(() =>
    performance
      .getEntriesByType("mark")
      .map((mark) => mark.name)
      .filter((name) => name.startsWith("hcm:")),
  );
  const bootState = await page.locator("body").innerText();
  await test.info().attach("agents-navigation-boot", {
    body: JSON.stringify(
      {
        url: page.url(),
        bootMarks,
        loadingProxyVisible: await page
          .locator(".loading-proxy")
          .isVisible()
          .catch(() => false),
        bootState: bootState.slice(0, 1200),
        diagnostics,
      },
      null,
      2,
    ),
    contentType: "application/json",
  });
  console.log(
    "[UXBLIND-122 boot]",
    JSON.stringify({
      bootMarks,
      loadingProxyVisible: await page
        .locator(".loading-proxy")
        .isVisible()
        .catch(() => false),
      diagnostics,
    }),
  );
  expect(
    bootMarks,
    "the Go/WASM client must complete hydration before the click journey",
  ).toContain("hcm:hydrated");
  expect(
    bootMarks,
    "the product route loader must finish before the click journey",
  ).toContain("hcm:load-done");
  await expect(page.locator(".chat-rail")).toBeVisible({ timeout: 30000 });
  const agentsLink = page.getByRole("link", { name: "Agents", exact: true });
  await expect(agentsLink).toBeVisible();
  await agentsLink.click();
  await expect(page).toHaveURL(/\/workspace\/app\/chat\/agents(?:\?|$)/, {
    timeout: 30000,
  });
  await expect(page.locator(".agents-page")).toBeVisible();

  const taskRow = page.locator(".agents-task-row").first();
  await expect(taskRow).toBeVisible({ timeout: 30000 });
  const taskID = await taskRow.getAttribute("data-task-id");
  const taskTitle = (await taskRow.locator("strong").textContent())?.trim();
  expect(taskID, "the seeded Agents task row must identify its task").toBeTruthy();
  expect(taskTitle, "the seeded Agents task must have a visible title").toBeTruthy();

  const openTask = taskRow.getByRole("link", { name: "Open task", exact: true });
  await expect(openTask).toHaveAttribute(
    "href",
    new RegExp(`[?&]task=${encodeURIComponent(taskID)}(?:&|$)`),
  );
  await openTask.click();
  await expect(page).toHaveURL(
    new RegExp(
      `/workspace/app/chat/agents\\?.*[?&]task=${encodeURIComponent(taskID)}(?:&|$)`,
    ),
    { timeout: 30000 },
  );
  await expect(page.locator("#agents-task-title")).toHaveText(taskTitle, {
    timeout: 30000,
  });

  await test.info().attach("agents-navigation-diagnostics", {
    body: JSON.stringify(
      { url: page.url(), taskID, taskTitle, bootMarks, diagnostics },
      null,
      2,
    ),
    contentType: "application/json",
  });
});
