// AGENT-044/045 browser evidence. This journey exercises the served Studio
// surface with a real authenticated tenant; IDs are read from the rendered
// controls so the request cannot accidentally use fixture identifiers.
import { test, expect } from "@playwright/test";

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
}

function rolloutResponse(page, action) {
  return page.waitForResponse(response => response.url().endsWith("/api/agents/rollouts") && response.request().method() === "POST" && response.request().postDataJSON()?.action === action);
}

test.describe("agent rollout and portable definitions", () => {
  test.beforeEach(async ({ page }) => { await signIn(page); });
  test("preview exposes exact installation and canary selections", async ({ page }) => {
    const diagnostics = [];
    page.on("pageerror", (error) => diagnostics.push(`pageerror: ${error.message}`));
    page.on("console", (message) => {
      if (message.type() === "error") diagnostics.push(`console: ${message.text()}`);
    });
    await page.goto("/workspace/app/chat/agents");
    await expect(page.locator("#agent-rollout-preview")).toBeVisible({ timeout: 30000 });

    const selected = await page.locator("input[data-rollout-installation]:checked").evaluateAll((nodes) => nodes.map((n) => n.value));
    expect(selected, "the preview must start with no implicit installation scope").toEqual([]);
    const installations = await page.locator("input[data-rollout-installation]").evaluateAll((nodes) => nodes.map((n) => n.value));
    expect(installations.length, "served installations must be real current IDs").toBeGreaterThan(0);
    await page.locator("input[data-rollout-installation]").first().check();
    const canary = page.locator("input[data-rollout-canary]").first();
    if (await canary.count()) await canary.check();
    await page.locator("button[data-rollout-submit='PREVIEW']").click();
    await expect(page.locator("#agent-rollout-plan")).toBeVisible({ timeout: 30000 });
    await test.info().attach("agent-rollout-preview", { body: JSON.stringify({ url: page.url(), installations, diagnostics }, null, 2), contentType: "application/json" });
  });

  test("portable import requires explicit destination mapping", async ({ page }) => {
    await page.goto("/workspace/app/chat/agents");
    await expect(page.locator("#agent-portable-import")).toBeVisible({ timeout: 30000 });
    await expect(page.locator("#agent-portable-body")).toHaveAttribute("aria-describedby", "agent-portable-import-help");
    const mapping = page.locator("#agent-portable-import input[data-destination-id]");
    expect(await mapping.count(), "initial import has no definition mappings yet").toBe(0);
    await expect(page.getByRole("button", { name: /Import as draft|Als Entwurf importieren|استيراد كمسودة/ })).toBeVisible();
  });

  test("live preview approval canary promotion and portable draft receipt", async ({ page }) => {
    const diagnostics = [];
    page.on("pageerror", e => diagnostics.push(`pageerror: ${e.message}`));
    page.on("console", m => { if (m.type() === "error") diagnostics.push(`console: ${m.text()}`); });
    await page.goto("/workspace/app/chat/agents");
    await expect(page.locator("#agent-rollout-preview")).toBeVisible({ timeout: 30000 });
    const persona = page.locator("#agent-rollout-persona");
    const version = page.locator("#agent-rollout-version");
    const installations = page.locator("input[data-rollout-installation]:visible");
    const count = await installations.count();
    expect(count, "server must publish current installations").toBeGreaterThanOrEqual(2);
    await installations.nth(0).check(); await installations.nth(1).check();
    const canary = page.locator("input[data-rollout-canary]:visible").first(); await canary.check();
    const versions = await version.locator("option:not([disabled])").evaluateAll(options => options.map(option => Number(option.value)));
    expect(new Set(versions).size, "upgrade/rollback needs two genuinely published and evaluated versions").toBeGreaterThanOrEqual(2);
    await version.selectOption(String(Math.max(...versions)));
    const previewResponse = rolloutResponse(page,"PREVIEW");
    await page.locator("button[data-rollout-submit='PREVIEW']").click();
    const preview = await previewResponse;
    expect(preview.ok()).toBe(true);
    const receipt = await preview.json();
    expect(receipt.plan?.id).toBeTruthy(); expect(receipt.plan?.digest).toBeTruthy();
    expect(receipt.plan?.candidates?.length).toBeGreaterThanOrEqual(2);
    await expect(page.locator("#agent-rollout-plan")).toBeVisible();
    const approve = page.locator("button[data-rollout-action='APPROVE']"); await expect(approve).toHaveCount(1);
    { const response = rolloutResponse(page,"APPROVE"); await approve.click(); expect((await response).ok()).toBe(true); }
    const advance = page.locator("button[data-rollout-action='ADVANCE']");
    await expect(advance).toHaveCount(1); { const response = rolloutResponse(page,"ADVANCE"); await advance.click(); expect((await response).ok()).toBe(true); }
    await expect(page.locator("#agent-rollout-progress")).toContainText("CANARY_COMPLETE");
    const promote = page.locator("button[data-rollout-action='PROMOTE']");
    await expect(promote).toHaveCount(1); { const response = rolloutResponse(page,"PROMOTE"); await promote.click(); expect((await response).ok()).toBe(true); }
    const broadAdvance = page.locator("button[data-rollout-action='ADVANCE']"); await expect(broadAdvance).toHaveCount(1); { const response = rolloutResponse(page,"ADVANCE"); await broadAdvance.click(); expect((await response).ok()).toBe(true); }
    await expect(page.locator("#agent-rollout-progress")).toContainText("COMPLETE");
    await page.locator("#agent-portable-export").scrollIntoViewIfNeeded();
    const exportedManifest = await page.locator("#agent-portable-manifest").inputValue();
    const exportResponse = page.waitForResponse(r => r.url().includes("/api/agents/portable/export") && r.request().method() === "POST");
    await page.locator("#agent-portable-export button[type='submit']").click();
    const exported = await exportResponse; expect(exported.ok()).toBe(true);
    const definition = await exported.json();
    const body = page.locator("#agent-portable-body"); await expect(body).not.toHaveValue("");
    const target = page.locator("#agent-portable-target"); await target.selectOption(exportedManifest);
    const mappings = page.locator("#agent-portable-import [data-destination-id]");
    expect(await mappings.count(), "exported definition must expose explicit references").toBeGreaterThan(0);
    for (let i = 0; i < await mappings.count(); i++) { const source = await mappings.nth(i).getAttribute("data-source-id"); expect(source).toBeTruthy(); await mappings.nth(i).fill(source); }
    const importResponse = page.waitForResponse(r => r.url().includes("/api/agents/portable/import") && r.request().method() === "POST");
    await page.locator("#agent-portable-import button[type='submit']").click();
    const imported = await importResponse; expect(imported.ok()).toBe(true);
    const draft = await imported.json(); expect(draft.state).toBe("DRAFT"); expect(draft.instructions).toBe(definition.instructions); expect(draft.manifest.context_grants).toEqual([]);
    await expect(page.locator("#agent-portable-draft")).toContainText(draft.manifest.id);
    await page.locator("#agent-portable-draft-id").fill(draft.manifest.id);
    const readResponse = page.waitForResponse(response => response.url().endsWith("/api/agents/portable/draft"));
    await page.locator("#agent-portable-review button").click();
    const recovered = await readResponse; expect(recovered.ok()).toBe(true); expect((await recovered.json()).manifest.id).toBe(draft.manifest.id);
    await test.info().attach("agent-rollout-portable-live", { body: JSON.stringify({ url: page.url(), rolloutID: receipt.plan.id, diagnostics }, null, 2), contentType: "application/json" });
    expect(diagnostics).toEqual([]);
  });

  for (const locale of ["en-US","de-DE","ar"]) for (const width of [1280,390,320]) for (const colorScheme of ["light","dark"]) {
    test(`portable controls ${locale} ${width}px ${colorScheme}`, async ({page}) => {
      await page.setViewportSize({width,height:900}); await page.emulateMedia({colorScheme});
      await page.goto(`/workspace/app/chat/agents?lang=${locale}`);
      const surface = page.locator("#agent-rollout-portable"); await expect(surface).toBeVisible({timeout:30000});
      expect(await surface.evaluate(node => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
      await test.info().attach(`portable-${locale}-${width}-${colorScheme}`,{body:await surface.screenshot(),contentType:"image/png"});
    });
  }

  test("rollout controls remain usable at narrow RTL width", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/workspace/app/chat/agents?lang=ar");
    const surface = page.locator("#agent-rollout-portable");
    await expect(surface).toBeVisible({ timeout: 30000 });
    await expect(surface).toHaveAttribute("dir", "rtl");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    expect(overflow, "rollout and portable controls overflow at 390px").toBe(false);
  });
});
