import { test, expect } from "@playwright/test";

// To-do and poll previews in the composer (#3984, #3976, #3970), against the
// live dev cell. Previews that write are made in the test's own channel; the
// rest are cancelled and nothing is posted to seeded conversations.

test.setTimeout(180000);
test.use({ timezoneId: "America/New_York", locale: "en-US" });

async function signIn(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

async function ownChannel(page) {
  const name = `zz-todo-${Math.random().toString(36).slice(2, 8)}`;
  await page.goto("/workspace/app/chat?locale=en-US");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("[data-action='open-create']").first().click();
  await page.locator("#create-kind-public-channel").click();
  await page.locator("#new-chat-name").fill(name);
  await page.locator(".create-dialog button[type='submit']").click();
  await expect(page.locator(".create-dialog")).toBeHidden({ timeout: 40000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
  return name;
}

async function openGeneral(page) {
  await page.goto("/workspace/app/chat?locale=en-US#channel=general");
  await page.locator(".message[data-message-id]").first().waitFor({ timeout: 80000 });
  await page.locator("textarea.composer-input").first().waitFor({ timeout: 40000 });
}

async function preview(page, text) {
  const input = page.locator("textarea.composer-input").first();
  await input.fill("");
  await input.fill(text);
  const box = page.locator(".chatcmd003-preview");
  await expect(box).toBeVisible({ timeout: 30000 });
  // Let the preview settle on the typed text.
  await page.waitForTimeout(500);
  return box;
}

async function cancel(page) {
  const c = page.locator(".chatcmd003-preview [data-action='chatcmd003-cancel']");
  if (await c.count()) await c.first().click();
  await expect(page.locator(".chatcmd003-preview")).toBeHidden();
}

test("#3984 an unresolved @name is flagged as unassigned and offers a picker or keep-as-text", async ({
  page,
}) => {
  await signIn(page);
  await openGeneral(page);
  const box = await preview(page, "/todo Review the budget @Dariuss by 2027-03-15");
  const note = box.locator(".chatcmd003-unassigned");
  await expect(note).toBeVisible();
  await expect(note).toContainText("@Dariuss");
  await expect(note).toContainText(/no owner/i);
  const group = box.locator("[role='radiogroup'][id^='chatcmd003-assign-']").first();
  await expect(group).toBeVisible();
  const radios = group.getByRole("radio");
  expect(await radios.count()).toBeGreaterThanOrEqual(2);
  // Nobody is guessed: nothing is checked and the card shows no assignee.
  await expect(group.locator("[role='radio'][aria-checked='true']")).toHaveCount(1);
  await expect(group.locator("[role='radio'][aria-checked='true']")).toContainText(
    /Keep @Dariuss as text/,
  );
  await expect(box).not.toContainText(/Assigned to/i);
  await expect(
    box.getByRole("radio", { name: /Keep @Dariuss as text/i }),
  ).toBeVisible();
  const darius = group.getByRole("radio", { name: /Darius/ });
  if (await darius.count()) {
    await darius.first().click();
    await expect(box).toContainText(/Assigned to Darius/i);
    await expect(box.locator(".chatcmd003-unassigned")).toHaveCount(0);
  }
  await cancel(page);

  // Partial-name control.
  const box2 = await preview(page, "/todo Review plan @Ma by 2027-03-15");
  await expect(box2.locator(".chatcmd003-unassigned")).toContainText("@Ma");
  await expect(box2).not.toContainText(/Assigned to/i);
  await cancel(page);

  // A valid mention is unchanged.
  const box3 = await preview(page, "/todo Review the budget @Darius by 2027-03-15");
  await expect(box3).toContainText(/Assigned to Darius/i);
  await expect(box3.locator(".chatcmd003-unassigned")).toHaveCount(0);
  await cancel(page);
});

test("#3976 a comma does not split one task, Keep as one task works, poll options keep Boston, MA", async ({
  page,
}) => {
  await signIn(page);
  await ownChannel(page);

  // Dollar amount: one task, no fragments.
  let box = await preview(page, "/todo Review $5,000 training budget");
  await expect(box).toContainText("Review $5,000 training budget");
  await cancel(page);

  // Plain comma split can be undone with Keep as one task.
  box = await preview(page, "/todo Review wage, tax and benefit changes");
  const keep = box.getByRole("radio", { name: /Keep as one task/i });
  await expect(keep).toBeVisible();
  await keep.click();
  await expect(keep).toHaveAttribute("aria-checked", "true");
  await expect(box).not.toContainText("One line became 2 tasks");
  await expect(page.locator("#chatcmd003-post")).toBeEnabled();
  await cancel(page);

  // Quoted line is one task with no quote marks around it.
  box = await preview(page, '/todo "Review wage, tax and benefit changes"');
  await expect(box).toContainText("Review wage, tax and benefit changes");
  await expect(box).not.toContainText('"Review wage');
  await cancel(page);

  // Post one task and check the posted card agrees.
  const marker = `m${Date.now()}`;
  box = await preview(page, `/todo Review wage, tax ${marker} and benefit changes`);
  await box.getByRole("radio", { name: /Keep as one task/i }).click();
  await page.locator("#chatcmd003-post").click();
  await expect(page.locator(".chatcmd003-preview")).toBeHidden({ timeout: 30000 });
  const card = page
    .locator(".message[data-message-id]")
    .filter({ hasText: `wage, tax ${marker} and benefit changes` })
    .first();
  await expect(card).toBeVisible({ timeout: 30000 });

  // The intentional shorthand still gives two tasks.
  box = await preview(
    page,
    "/todo Submit timesheets by 2027-03-12, Review budget by 2027-03-15",
  );
  await expect(box).toContainText("Submit timesheets");
  await expect(box).toContainText("Review budget");
  await expect(box).toContainText("2027-03-12");
  await expect(box).toContainText("2027-03-15");
  await cancel(page);

  // Poll: city and state stay together.
  box = await preview(page, "/poll Where should we meet? Boston, MA or Portland, ME");
  await expect(box).toContainText("Boston, MA");
  await expect(box).toContainText("Portland, ME");
  await cancel(page);
  box = await preview(
    page,
    '/poll Where should we meet? "Boston, MA" or "Portland, ME"',
  );
  await expect(box).toContainText("Boston, MA");
  await expect(box).not.toContainText('"Boston');
  await cancel(page);
});

test("#3970 an ISO deadline stays on the typed date across daylight-saving changes", async ({
  page,
}) => {
  await signIn(page);
  await openGeneral(page);
  for (const date of [
    "2027-03-13",
    "2027-03-14",
    "2027-03-15",
    "2027-11-06",
    "2027-11-07",
  ]) {
    const box = await preview(page, `/todo Review schedule by ${date}`);
    await expect(box).toContainText(date);
    const text = await box.innerText();
    const dates = text.match(/\b20\d\d-\d\d-\d\d\b/g) || [];
    expect(dates.length, `preview for ${date}: ${text}`).toBeGreaterThan(0);
    for (const d of dates) expect(d, `preview for ${date}: ${text}`).toBe(date);
    await cancel(page);
  }
});
