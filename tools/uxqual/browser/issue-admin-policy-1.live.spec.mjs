import { test, expect } from "@playwright/test";

// Admin > Chat settings: retention storage limit, and the By country form.
// Only refused country values are saved here, so no row is added to the
// tenant-wide list.

test.setTimeout(120000);

async function openSettings(page) {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto("/workspace/app/admin/chat-settings?locale=en-US");
  await expect(page.locator("#chatmapadmin-country")).toBeVisible({
    timeout: 60000,
  });
}

// The message the Country input points at through aria-describedby, which
// must be an alert inside the country form (not the section status line).
async function countryFieldError(page) {
  const country = page.locator("#chatmapadmin-country");
  await expect(country).toHaveAttribute("aria-invalid", "true");
  const ids = (await country.getAttribute("aria-describedby")).split(" ");
  const found = [];
  for (const id of ids) {
    const node = page.locator(`[data-chatmapadmin=country] #${id}`);
    if ((await node.count()) === 0) continue;
    found.push({
      role: await node.getAttribute("role"),
      text: (await node.innerText()).trim(),
    });
  }
  const alert = found.find((f) => f.role === "alert");
  expect(alert, `an alert inside the country form among ${ids}`).toBeTruthy();
  return alert.text;
}

async function saveCountry(page, country, basis) {
  await page.locator("#chatmapadmin-country").fill(country);
  await page.locator("#chatmapadmin-basis").fill(basis);
  const response = page.waitForResponse((r) => r.url().includes("/setjurisdiction"), {
    timeout: 30000,
  });
  await page.getByRole("button", { name: "Save country" }).click();
  return response;
}

test("#3703 invalid storage limit shows an inline range message and Basis is marked required", async ({
  page,
}) => {
  await openSettings(page);
  await page.locator("#chat-retention-mode").selectOption("SIZE_BUDGET");
  const limit = page.locator("#chat-retention-budget");
  await expect(limit.first()).toBeVisible();
  const save = page.locator("#chat-retention-save");
  for (const value of ["-5", "0", "1.5", "99999999999999999999"]) {
    await limit.first().fill(value);
    await limit.first().dispatchEvent("input");
    const message = page.locator(".chat-retention-form [role=alert]");
    await expect(message, `message for ${value}`).toBeVisible();
    const text = (await message.innerText()).trim();
    expect(text).toMatch(/\d/);
    expect(text.length).toBeGreaterThan(10);
    await expect(save).toBeDisabled();
  }
  await limit.first().fill("10");
  await limit.first().dispatchEvent("input");
  await expect(page.locator(".chat-retention-form [role=alert]")).toHaveCount(0);

  // Basis: required attribute plus a visible marker on its label.
  const basis = page.locator("#chatmapadmin-basis");
  await expect(basis).toHaveAttribute("required", "");
  const marker = await page.locator("label[for=chatmapadmin-basis]").evaluate((el) => {
    const s = window.getComputedStyle(el, "::after");
    return { content: s.content, color: s.color };
  });
  expect(marker.content).toContain("*");
});

test("#3700 a refused country shows at the Country field, not as the page-level failure", async ({
  page,
}) => {
  await openSettings(page);
  // The input caps its length, so 300 characters reach the server as 40; it
  // refuses them and the refusal is shown at the field.
  const country = page.locator("#chatmapadmin-country");
  const long = await saveCountry(page, "x".repeat(300), "qa");
  expect(long.status()).toBe(400);
  await countryFieldError(page);
  const refused = await saveCountry(page, "Narnia", "qa");
  expect(refused.status()).toBe(400);
  await expect(country).toHaveAttribute("aria-invalid", "true");
  const text = await countryFieldError(page);
  expect(text).toMatch(/not|unknown|valid/i);
  await expect(page.getByText("Location settings could not load or save")).toHaveCount(
    0,
  );
});

test("#3699 unknown and markup countries are refused with an inline message and add no row", async ({
  page,
}) => {
  await openSettings(page);
  const before = await page
    .locator(
      "#chatmapadmin-countries-title ~ ul li, #chatmapadmin-countries-title ~ ol li",
    )
    .count();
  for (const value of ["ZZ", "Narnia", "<b>x</b>"]) {
    const response = await saveCountry(page, value, "qa test");
    expect(response.status(), value).toBe(400);
    const text = await countryFieldError(page);
    expect(text, value).toMatch(/not (a )?known|unknown|isn.t a known/i);
  }
  const after = await page
    .locator(
      "#chatmapadmin-countries-title ~ ul li, #chatmapadmin-countries-title ~ ol li",
    )
    .count();
  expect(after).toBe(before);
  await expect(page.locator("body")).not.toContainText("NARNIA");
  await expect(page.locator("body")).not.toContainText("<B>X</B>");
});
