// TEMP PROBE (impl-ux-a) - deleted after diagnosis.
import { test } from "@playwright/test";

test("probe role derive", async ({ page }) => {
  test.setTimeout(90000);
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto("/workspace/app/admin/roles?locale=en-US");
  await page
    .locator(".role-create-disclosure > summary", { hasText: "Open role creation" })
    .click();
  const nameField = page.locator("#new-role-name");
  const idField = page.locator("#new-role-id");
  await nameField.waitFor({ state: "visible", timeout: 30000 });
  await nameField.click();
  const log = [];
  for (const ch of "QA Audit") {
    await page.keyboard.press(ch === " " ? "Space" : ch);
    await page.waitForTimeout(120);
    log.push({
      key: ch,
      name: await nameField.inputValue(),
      id: await idField.inputValue(),
    });
  }
  await page.keyboard.press("Tab");
  await page.waitForTimeout(500);
  log.push({
    key: "TAB",
    name: await nameField.inputValue(),
    id: await idField.inputValue(),
    focused: await page.evaluate(() => document.activeElement?.id),
  });
  // Direct ID entry: does the ID handler persist state?
  await idField.click();
  await idField.fill("site_lead");
  await page.waitForTimeout(300);
  await page.keyboard.press("Tab");
  await page.waitForTimeout(500);
  log.push({
    key: "ID-FILL-TAB",
    name: await nameField.inputValue(),
    id: await idField.inputValue(),
  });
  // Submit with visible text: does draft state hold it?
  await page.getByRole("button", { name: "Create role", exact: true }).click();
  await page.waitForTimeout(800);
  log.push({
    key: "SUBMIT",
    name: await nameField.inputValue(),
    id: await idField.inputValue(),
    nameError: await page.locator("#new-role-name-error").count(),
    idError: await page.locator("#new-role-id-error").count(),
    url: page.url(),
  });
  console.log("PROBE-RESULT " + JSON.stringify(log));
});
