import { test, expect } from "@playwright/test";

test.setTimeout(120000);

async function signIn(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto("/workspace/app/agents");
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 45000 },
  );
  await expect(page.locator("#agent-controls-status")).toBeVisible({ timeout: 45000 });
}

// This boundary reaches the actual serving stack with its issued bearer.
// Browser routes and provider outputs are never intercepted.
async function ownerAPI(page, path, command) {
  return page.evaluate(
    async ({ path, command }) => {
      const cfg = JSON.parse(document.getElementById("journey-config").textContent);
      const response = await fetch(path, {
        method: command ? "POST" : "GET",
        headers: {
          Authorization: `Bearer ${cfg.bearer}`,
          ...(command ? { "Content-Type": "application/json" } : {}),
        },
        ...(command ? { body: JSON.stringify(command) } : {}),
      });
      return { status: response.status, body: await response.json() };
    },
    { path, command },
  );
}

test("TestTodo_AGENT_041_Browser owner controls reflect authorized durable projections and refuse forged scope", async ({
  page,
}) => {
  const diagnostics = [];
  page.on("pageerror", (error) => diagnostics.push(error.message));
  await signIn(page);
  const response = await ownerAPI(page, "/api/agent-controls");
  expect(response.status).toBe(200);
  const projection = response.body.snapshot;
  const mount = page.locator("#agent-controls");
  for (const [kind, rows] of [
    ["schedule", projection.schedules ?? []],
    ["run", projection.runs ?? []],
    ["memory", projection.memory ?? []],
  ]) {
    const rendered = await mount
      .locator(`[data-control-${kind}]`)
      .evaluateAll(
        (nodes, kind) => nodes.map((node) => node.getAttribute(`data-control-${kind}`)),
        kind,
      );
    expect(rendered.sort()).toEqual(rows.map((row) => row.id).sort());
    for (const row of rows) {
      const element = mount
        .locator(`[data-control-${kind}]`)
        .filter({ has: page.getByRole("heading", { name: row.id, exact: true }) });
      for (const action of await element
        .locator("[data-owner-action]")
        .evaluateAll((nodes) => nodes.map((node) => node.dataset.ownerAction)))
        expect(row.actions).toContain(action);
    }
  }
  const forged = await ownerAPI(page, "/api/agent-controls/control", {
    kind: "run",
    id: "other-tenant-task",
    expected_revision: 1,
    action: "pause",
    idempotency_key: "forged-scope-browser",
    reason: "scope proof",
    tenant_id: "other-tenant",
  });
  expect(forged.status).toBe(400);
  const anonymous = await page.request.get("/api/agent-controls", {
    headers: { Authorization: "Bearer invalid" },
  });
  expect(anonymous.status()).toBe(401);
  await expect(mount.getByRole("status")).toHaveAttribute("aria-live", "polite");
  const refresh = mount.locator("[data-owner-refresh]");
  await refresh.focus();
  await expect(refresh).toBeFocused();
  await refresh.press("Enter");
  await expect(refresh).toBeEnabled({ timeout: 30000 });
  for (const width of [1280, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(mount).toBeVisible();
    expect(
      await mount.evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
    ).toBeTruthy();
    await test.info().attach(`agent-controls-${width}`, {
      body: await mount.screenshot(),
      contentType: "image/png",
    });
  }
  expect(diagnostics).toEqual([]);
});

test("TestTodo_AGENT_030_Browser authorized schedule pause and resume carry durable revisions", async ({
  page,
}) => {
  await signIn(page);
  const response = await ownerAPI(page, "/api/agent-controls");
  expect(response.status).toBe(200);
  const schedule = response.body.snapshot.schedules?.find((row) =>
    row.actions.includes("pause"),
  );
  expect(
    schedule,
    "a real published schedule with current owner authority is required",
  ).toBeTruthy();
  expect(
    response.body.snapshot.can_draft,
    "the reviewed owner needs a current draft grant",
  ).toBeTruthy();
  const budget = schedule.budget.match(/^(\d+).*?(\d+).*?(\d+)/);
  expect(
    budget,
    "server budget must include cost, input and output bounds",
  ).toBeTruthy();
  for (const [field, value] of Object.entries({
    id: schedule.id,
    revision: String(schedule.revision),
    version: schedule.version,
    installation: schedule.installation,
    recurrence: schedule.recurrence,
    zone: schedule.zone,
    calendar: schedule.calendar ?? "",
    destination: schedule.destination,
    max_cost: budget[1],
    max_input: budget[2],
    max_output: budget[3],
  }))
    await page.locator(`#agent-schedule-${field}`).fill(value);
  await page.locator("#agent-schedule-misfire").selectOption(schedule.misfire);
  await page.locator("#agent-schedule-overlap").selectOption(schedule.overlap);
  await page.locator("#agent-schedule-dst").selectOption(schedule.dst);
  await page.locator('[data-owner-draft="preview"]').click();
  await expect(page.locator("[data-owner-occurrence]").first()).toBeVisible({
    timeout: 30000,
  });
  await expect(page.locator("#agent-schedule-id")).toHaveValue(schedule.id);
  await page
    .locator("#agent-controls-reason")
    .fill("Owner schedule health browser check");
  const row = page
    .locator("[data-control-schedule]")
    .filter({ has: page.getByRole("heading", { name: schedule.id, exact: true }) });
  const pause = row.locator('[data-owner-action="pause"]');
  await expect(pause).toHaveAttribute("data-owner-revision", String(schedule.revision));
  await pause.focus();
  await pause.press("Enter");
  await expect(row.locator('[data-owner-action="resume"]')).toBeVisible({
    timeout: 30000,
  });
  const paused = await ownerAPI(page, "/api/agent-controls");
  const current = paused.body.snapshot.schedules.find(
    (item) => item.id === schedule.id,
  );
  expect(current.state).toBe("PAUSED");
  expect(current.revision).toBeGreaterThan(schedule.revision);
  const stale = await ownerAPI(page, "/api/agent-controls/control", {
    kind: "schedule",
    id: schedule.id,
    expected_revision: schedule.revision,
    action: "resume",
    idempotency_key: "stale-revision-browser",
    reason: "stale control proof",
  });
  expect(stale.status).toBe(409);
  await row.locator('[data-owner-action="resume"]').click();
  await expect(row.locator('[data-owner-action="pause"]')).toBeVisible({
    timeout: 30000,
  });
  const resumed = await ownerAPI(page, "/api/agent-controls");
  expect(
    resumed.body.snapshot.schedules.find((item) => item.id === schedule.id).state,
  ).toBe("ACTIVE");
});

test("TestTodo_AGENT_043_Browser owner controls preserve actions across locale, direction and theme", async ({
  page,
}) => {
  await signIn(page);
  for (const [locale, direction] of [
    ["en-US", "ltr"],
    ["de-DE", "ltr"],
    ["ar", "rtl"],
  ]) {
    for (const theme of ["light", "dark"]) {
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
      await page.goto(`/workspace/app/agents?locale=${locale}`);
      const mount = page.locator("#agent-controls");
      await expect(mount.locator("#agent-controls-status")).toBeVisible({
        timeout: 45000,
      });
      await expect(mount).toHaveAttribute("data-locale", locale);
      await expect(mount).toHaveAttribute("dir", direction);
      for (const width of [1280, 390, 320]) {
        await page.setViewportSize({ width, height: 900 });
        expect(
          await mount.evaluate((node) => node.scrollWidth <= node.clientWidth + 1),
        ).toBeTruthy();
        await test.info().attach(`controls-${locale}-${theme}-${width}`, {
          body: await mount.screenshot(),
          contentType: "image/png",
        });
      }
      const canonicalActions = await mount
        .locator("[data-owner-action]")
        .evaluateAll((nodes) => nodes.map((node) => node.dataset.ownerAction));
      for (const action of canonicalActions)
        expect([
          "publish",
          "pause",
          "skip",
          "resume",
          "dry_run",
          "retire",
          "quarantine",
          "export",
          "delete",
          "revoke",
          "hold",
        ]).toContain(action);
    }
  }
});
