import { test, expect } from "@playwright/test";
/* global URL */

// Live checks for #3981, #3975 and #3974 against the running dev cell
// (HCMNEXT_DEV_URL, 127.0.0.1:8080 by default). The demo tenant has agents
// switched off, so the Agents page shows only its "turned off" notice: there is
// no task list, no composer and no General agent description. Each test says so
// and skips instead of passing on nothing; with agents on they assert the
// Expected behaviour.

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

const rowsOf = (page) => page.locator("[data-task-id], .agents-task-row, .agents-task");
const detailOf = (page) => page.locator("[data-selected-task-id]");

test("#3981 Close, Back and Forward keep the visible task in step with the address", async ({
  page,
}) => {
  const on = await openAgents(page);
  test.skip(!on, "agents are turned off for this tenant: no task list renders");
  await page.getByRole("tab", { name: /^Completed/ }).click();
  const rows = rowsOf(page);
  test.skip((await rows.count()) < 2, "fewer than two completed tasks");

  await rows.nth(0).click();
  await expect(page).toHaveURL(/task=/);
  const first = new URL(page.url()).searchParams.get("task");
  await rows.nth(1).click();
  await expect.poll(() => new URL(page.url()).searchParams.get("task")).not.toBe(first);
  const second = new URL(page.url()).searchParams.get("task");

  await page.goBack();
  await expect.poll(() => new URL(page.url()).searchParams.get("task")).toBe(first);
  await expect(detailOf(page)).toHaveAttribute("data-selected-task-id", first);
  await page.goForward();
  await expect(detailOf(page)).toHaveAttribute("data-selected-task-id", second);

  // Close clears the detail, collapses the row and returns focus to the row.
  const close = page.getByRole("button", { name: /close/i }).first();
  await close.click();
  await expect(page).not.toHaveURL(/task=/);
  await expect(detailOf(page)).toHaveCount(0);
  await expect(page.locator("[aria-expanded='true'][data-task-id]")).toHaveCount(0);
  const focusedRow = await page.evaluate(
    () =>
      !!document.activeElement?.closest(
        "[data-task-id], .agents-task-row, .agents-task",
      ),
  );
  expect(focusedRow).toBe(true);

  // Switching filter after Close leaves no stale detail beside the empty state.
  await page.getByRole("tab", { name: /^Active/ }).click();
  await expect(detailOf(page)).toHaveCount(0);
});

test("#3975 Ask a follow-up keeps the request and the draft, and the draft survives a round trip", async ({
  page,
}) => {
  const on = await openAgents(page);
  test.skip(!on, "agents are turned off for this tenant: no composer renders");
  const box = page.getByRole("textbox").first();
  const marker = `zz-draft-${Date.now()}`;
  await box.fill(marker);
  await page.getByRole("tab", { name: /^Completed/ }).click();
  const rows = rowsOf(page);
  test.skip((await rows.count()) < 1, "no completed task to follow up");
  await rows.first().click();
  await page
    .getByRole("button", { name: /ask a follow-up/i })
    .first()
    .click();
  const composer = page.getByRole("textbox").first();
  await expect(composer).toBeFocused();
  await expect(composer).toHaveValue(new RegExp(marker));
  expect((await composer.inputValue()).length).toBeGreaterThan(marker.length);
});

test("#3974 General agent description does not point at a control that is not drawn", async ({
  page,
}) => {
  const on = await openAgents(page);
  test.skip(
    !on,
    "agents are turned off for this tenant: no General agent description renders",
  );
  const picker = page.getByRole("button", { name: /add document/i });
  const text = await page.locator("main").innerText();
  if ((await picker.count()) === 0) {
    expect(text).not.toMatch(/unless you add them below/i);
  } else {
    await expect(picker.first()).toBeVisible();
  }
});
