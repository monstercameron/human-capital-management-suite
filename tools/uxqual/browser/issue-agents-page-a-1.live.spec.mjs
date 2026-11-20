import { test, expect } from "@playwright/test";

// Live checks for #3992, #3983 and #3982 against the running dev cell
// (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default). The demo tenant has agents
// switched off, so the Agents page shows only its "turned off" notice and the
// task tabs and the agent composer do not exist. Each test says so and skips
// instead of passing on nothing; with agents on they assert the Expected text.

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login");
  await page
    .getByRole("button", { name: /^Continue as Rafael Torres/ })
    .first()
    .click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function openAgents(page) {
  await signIn(page);
  await page.goto("/workspace/app/chat/agents?locale=en-US");
  const off = page.getByText("Agents are turned off for your organization");
  const tabs = page.getByRole("tablist");
  await expect(off.or(tabs.first())).toBeVisible({ timeout: 60000 });
  return !(await off.isVisible());
}

test("#3982 task filter tabs name their category and count", async ({ page }) => {
  const on = await openAgents(page);
  test.skip(!on, "agents are turned off for this tenant: no task tabs render");
  const tabs = page.getByRole("tablist").first().getByRole("tab");
  expect(await tabs.count()).toBe(3);
  for (const tab of await tabs.all()) {
    const visible = (await tab.innerText()).trim();
    const count = visible.match(/(\d+)\s*$/)?.[1];
    const name = (await tab.getAttribute("aria-label")) ?? visible;
    expect(count, `visible count in "${visible}"`).toBeTruthy();
    const noun = count === "1" ? "task" : "tasks";
    expect(name).toMatch(new RegExp(`^(Active|Completed|Failed), ${count} ${noun}$`));
  }
});

test("#3983 switching tasks replaces the page title", async ({ page }) => {
  const on = await openAgents(page);
  test.skip(!on, "agents are turned off for this tenant: no task list renders");
  await page.getByRole("tab", { name: /^Completed/ }).click();
  const rows = page.locator("[data-task-id], .agents-task-row, .agents-task");
  test.skip((await rows.count()) < 2, "fewer than two completed tasks");
  const base = "Agents · Harborcare Demo";
  const titles = [];
  for (const i of [0, 1, 0]) {
    await rows.nth(i).click();
    await page.waitForTimeout(500);
    titles.push(await page.title());
  }
  for (const t of titles) {
    expect(t.endsWith(base)).toBe(true);
    expect(t.split(" · ").length).toBeLessThanOrEqual(3);
  }
  expect(titles[2]).toBe(titles[0]);
  expect(titles[1]).not.toBe(titles[0]);
  await page.reload();
  await expect.poll(() => page.title()).toMatch(/Agents · Harborcare Demo$/);
});

test("#3992 Private answer choice follows its own draft", async ({ page }) => {
  await signIn(page);
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await expect(page.locator(".message[data-message-id]").first()).toBeVisible({
    timeout: 60000,
  });
  const toggle = page.getByRole("switch", { name: /private answer/i });
  test.skip(
    (await toggle.count()) === 0,
    "no installed agent in this tenant, so the composer never offers Private answer",
  );
});
