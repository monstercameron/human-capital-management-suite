// UXBLIND-069 live-browser probe: the role-creation form keeps a paced,
// multi-character display name (with spaces) intact across input, Tab/blur
// and re-render, derives the matching editable ID, keeps an explicit ID edit
// user-owned, and still reports inline validation. Requires the authenticated
// dev cell (HCMNEXT_DEV_URL) with the Ironridge Walt Brennan admin persona.
// The probe never creates a role: the only submit carries an empty name and
// must be stopped by inline validation.
import { test, expect } from "@playwright/test";

async function waitForHydrated(page) {
  await page.waitForFunction(
    () =>
      window.performance
        .getEntriesByType("mark")
        .some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 30000, polling: 50 },
  );
  await page.waitForFunction(
    () =>
      window.performance
        .getEntriesByType("mark")
        .some((mark) => mark.name === "hcm:load-done"),
    null,
    { timeout: 30000, polling: 50 },
  );
}

test("[UXBLIND-069] role creation keeps paced input across Tab and blur", async ({
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
    if (message.type() === "error" || message.type() === "warning")
      diagnostics.push(`console ${message.type()}: ${message.text()}`);
  });
  test.setTimeout(120000);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await waitForHydrated(page);

  await page.goto("/workspace/app/admin/roles?locale=en-US");
  await waitForHydrated(page);

  const nameField = page.locator("#new-role-name");
  const idField = page.locator("#new-role-id");
  await page
    .locator(".role-create-disclosure > summary", { hasText: "Open role creation" })
    .click();
  await expect(nameField).toBeVisible({ timeout: 30000 });
  await expect(nameField).toBeEditable();
  await expect(idField).toBeEditable();

  // Paced entry, one character at a time, reading the DOM value after every
  // key the way the regression report did.
  const typed = "QA Audit";
  await nameField.click();
  const observed = [];
  for (const ch of typed) {
    await page.keyboard.press(ch === " " ? "Space" : ch);
    observed.push(await nameField.inputValue());
  }
  await test.info().attach("uxblind069-paced-values", {
    body: JSON.stringify({ observed }, null, 2),
    contentType: "application/json",
  });
  const beforeTab = await nameField.inputValue();
  expect(
    beforeTab,
    `paced entry should reach the full name, saw ${JSON.stringify(observed)}`,
  ).toBe(typed);

  // Tab out: the settled value must survive blur and any re-render.
  await page.keyboard.press("Tab");
  await expect
    .poll(() => nameField.inputValue(), { timeout: 5000 })
    .not.toBe("");
  const afterTab = await nameField.inputValue();
  expect(
    afterTab,
    `display name collapsed across Tab: ${JSON.stringify(beforeTab)} -> ${JSON.stringify(afterTab)}`,
  ).toBe(typed);

  // The derived ID tracks the full name.
  await expect
    .poll(() => idField.inputValue(), { timeout: 5000 })
    .toBe("qa_audit");

  // An explicit ID edit stays user-owned across blur.
  await idField.fill("qa_lead");
  await page.keyboard.press("Tab");
  await expect
    .poll(() => idField.inputValue(), { timeout: 5000 })
    .toBe("qa_lead");
  expect(await nameField.inputValue()).toBe(typed);

  // Validation still works: an empty name blocks the submit inline and the
  // form stays put.
  await nameField.fill("");
  await page.getByRole("button", { name: "Create role", exact: true }).click();
  await expect(page.locator("#new-role-name-error")).toBeVisible({
    timeout: 10000,
  });
  expect(page.url()).toContain("/workspace/app/admin/roles");

  await test.info().attach("uxblind069-diagnostics", {
    body: JSON.stringify({ url: page.url(), diagnostics }, null, 2),
    contentType: "application/json",
  });
  expect(
    diagnostics,
    "the role-creation journey should not emit runtime errors",
  ).toEqual([]);
});
