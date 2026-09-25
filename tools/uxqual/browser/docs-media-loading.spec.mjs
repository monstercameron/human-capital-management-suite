/* global HTMLImageElement */

import { test, expect } from "@playwright/test";

const charter = "doc-9e3baeb9-4baf-4a8b-b8f3-e2d4d632061e";
test.setTimeout(90000);

test("a document image loads on entry and after returning through history", async ({
  page,
}) => {
  await page.goto("/workspace/login");
  await page.getByRole("button", { name: "Continue as Rafael Torres" }).click();
  await page.waitForURL(/\/workspace\/app\//);

  const mediaRequests = [];
  page.on("response", (response) => {
    if (response.url().includes(`/v1/documents/media/${charter}/`)) {
      mediaRequests.push(response.status());
    }
  });

  await page.goto(`/workspace/app/docs?document=${charter}`);
  const image = page.getByRole("img", { name: "People Operations org chart" });
  await expect(image).toBeVisible({ timeout: 15000 });
  await expect
    .poll(
      () =>
        image.evaluate(
          (element) =>
            element instanceof HTMLImageElement &&
            element.complete &&
            element.naturalWidth > 0,
        ),
      { timeout: 20000 },
    )
    .toBe(true);
  expect(mediaRequests).toContain(200);

  await page.getByRole("link", { name: "Back to documents" }).click();
  await page.goBack({ waitUntil: "domcontentloaded" });
  await expect(image).toBeVisible({ timeout: 15000 });
  await expect
    .poll(
      () =>
        image.evaluate(
          (element) =>
            element instanceof HTMLImageElement &&
            element.complete &&
            element.naturalWidth > 0,
        ),
      { timeout: 20000 },
    )
    .toBe(true);
  await expect(page.locator(".docs-media-placeholder")).toHaveCount(0);
});
