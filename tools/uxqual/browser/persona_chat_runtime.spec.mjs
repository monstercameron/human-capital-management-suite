import { test, expect } from "@playwright/test";

test.setTimeout(180000);

async function hydrate(page) {
  await page.waitForFunction(
    () =>
      performance.getEntriesByType("mark").some((mark) => mark.name === "hcm:hydrated"),
    null,
    { timeout: 45000 },
  );
  await expect(page.locator("#chat-composer")).toBeAttached({ timeout: 45000 });
}

async function signIn(page) {
  await page.goto("/workspace/login?company=ironridge-demo");
  await page.getByRole("button", { name: "Continue as Walt Brennan" }).click();
  await page.waitForURL(/\/workspace\/app\//);
  await page.goto("/workspace/app/chat");
  await hydrate(page);
}

// Reads the real authenticated boundary. No route interception or substituted
// provider answers are permitted in this acceptance journey.
async function personaAPI(page, path) {
  return page.evaluate(async (endpoint) => {
    const config = JSON.parse(document.getElementById("journey-config").textContent);
    const response = await fetch(endpoint, {
      headers: { Authorization: `Bearer ${config.bearer}` },
    });
    if (!response.ok) throw new Error(`Persona API returned ${response.status}`);
    return response.json();
  }, path);
}

async function findInvocablePersona(page) {
  const ids = await page
    .locator('[data-action="select"][data-id]')
    .evaluateAll((rows) => [...new Set(rows.map((row) => row.dataset.id))]);
  expect(ids.length, "served conversations must be available").toBeGreaterThan(0);
  for (const id of ids) {
    const directory = await personaAPI(
      page,
      `/api/chat/personas?conversation_id=${encodeURIComponent(id)}`,
    );
    const persona =
      directory.personas.find(
        (candidate) => candidate.reference.display === "Policy Helper",
      ) ?? directory.personas[0];
    if (persona) {
      await page.goto(`/workspace/app/chat#channel=${encodeURIComponent(id)}`);
      await hydrate(page);
      const composer = page.locator("#chat-composer");
      await expect(composer).toBeEnabled();
      await composer.fill(`@${persona.reference.display.split(" ")[0]}`);
      await expect(
        page
          .locator(".mention-option.persona")
          .filter({ hasText: persona.reference.display }),
      ).toBeVisible({ timeout: 20000 });
      return { id, persona, directory };
    }
  }
  throw new Error(
    "No authorized, installed persona exists in the served conversations",
  );
}

test("TestTodo_AGENTP_019_Browser authorized profiles and keyboard selection use canonical identity", async ({
  page,
}) => {
  await signIn(page);
  const { id, persona, directory } = await findInvocablePersona(page);
  const menu = page.locator("#chat-composer-mentions");
  await expect(menu).toHaveAttribute("role", "listbox");
  const rendered = await menu
    .locator(".mention-option.persona .mention-name")
    .allTextContents();
  for (const name of rendered)
    expect(
      directory.personas.some((candidate) => candidate.reference.display === name),
    ).toBeTruthy();
  const row = menu
    .locator(".mention-agent-row")
    .filter({ hasText: persona.reference.display });
  await row.locator("summary").click();
  const profile = row.locator(".mention-profile-card");
  await expect(profile).toContainText(persona.purpose);
  await expect(profile).toContainText(persona.owner);
  await expect(profile).toContainText(persona.version);
  await expect(profile).toContainText("Acts with your current access");
  for (const skill of persona.skills) await expect(profile).toContainText(skill.name);
  for (const reach of persona.data_classes) await expect(profile).toContainText(reach);
  await row.locator("summary").click();
  const composer = page.locator("#chat-composer");
  await composer.focus();
  await expect(composer).toHaveAttribute(
    "aria-activedescendant",
    /chat-composer-mention-/,
  );
  const selectedName = await menu
    .locator('[role="option"][aria-selected="true"] .mention-name')
    .innerText();
  await composer.press("Enter");
  await expect(composer).toHaveValue(`@${selectedName} `);
  await expect(menu).not.toBeAttached();
  await composer.fill("");
  const anonymous = await page.request.get(
    `/api/chat/personas?conversation_id=${encodeURIComponent(id)}`,
    { headers: { Authorization: "Bearer invalid" } },
  );
  expect(anonymous.status()).toBe(401);
});

test("TestTodo_AGENTP_020_Browser real mention creates a tracked invocation and a real thread result", async ({
  page,
}) => {
  const diagnostics = [];
  page.on("pageerror", (error) => diagnostics.push(error.message));
  await signIn(page);
  const { id, persona } = await findInvocablePersona(page);
  const before = await personaAPI(
    page,
    `/api/chat/personas/invocations?conversation_id=${encodeURIComponent(id)}`,
  );
  const known = new Set(
    before.invocations.map((invocation) => invocation.invocation_id),
  );
  await page
    .locator(".mention-option.persona")
    .filter({ hasText: persona.reference.display })
    .click();
  const composer = page.locator("#chat-composer");
  await composer.press("End");
  await composer.pressSequentially(
    "What company policies explain onboarding? Cite the sources you actually read.",
  );
  await composer.press("Enter");
  await expect(page.locator(".thread-pane")).toBeVisible({ timeout: 30000 });
  let invocation;
  await expect
    .poll(
      async () => {
        const current = await personaAPI(
          page,
          `/api/chat/personas/invocations?conversation_id=${encodeURIComponent(id)}`,
        );
        invocation = current.invocations.find((row) => !known.has(row.invocation_id));
        return invocation?.status?.toLowerCase();
      },
      { timeout: 120000, intervals: [500, 1000, 2000] },
    )
    .toBe("completed");
  expect(invocation.post_id).toBeTruthy();
  await expect(page.locator(".persona-progress-status")).not.toBeAttached();
  let reply;
  if (invocation.private_conversation_id && invocation.private_post_id) {
    await expect(page.locator(".persona-private-result").last()).toBeVisible();
    await page.locator(".persona-private-result").last().click();
    await hydrate(page);
    reply = page.locator(
      `[data-message-id=${JSON.stringify(invocation.private_post_id)}]`,
    );
  } else {
    reply = page.locator(".thread-replies .thread-message").last();
  }
  await expect(reply.locator(".message-body")).not.toBeEmpty({ timeout: 30000 });
  await expect(reply.locator(".agent-badge")).toHaveAttribute(
    "data-agent-id",
    persona.reference.id,
  );
  await expect(reply.locator(".agent-badge")).toHaveAttribute("data-persona-id", /.+/);
  expect(diagnostics).toEqual([]);
  await test.info().attach("persona-runtime-result", {
    body: JSON.stringify({
      invocation_id: invocation.invocation_id,
      post_id: invocation.post_id,
      status: invocation.status,
    }),
    contentType: "application/json",
  });
});

test("TestTodo_AGENTP_019_I18n persona profile fits desktop and phones in light, dark and RTL", async ({
  page,
}) => {
  await signIn(page);
  const { id, persona } = await findInvocablePersona(page);
  for (const locale of ["en-US", "de-DE", "ar"]) {
    for (const colorScheme of ["light", "dark"]) {
      await page.emulateMedia({ colorScheme, reducedMotion: "reduce" });
      for (const width of [1280, 390, 320]) {
        await page.setViewportSize({ width, height: 900 });
        await page.goto(
          `/workspace/app/chat?locale=${locale}#channel=${encodeURIComponent(id)}`,
        );
        await hydrate(page);
        await page
          .locator("#chat-composer")
          .fill(`@${persona.reference.display.split(" ")[0]}`);
        const row = page
          .locator(".mention-agent-row")
          .filter({ hasText: persona.reference.display });
        await expect(row).toBeVisible({ timeout: 20000 });
        await row.locator("summary").click();
        const profile = row.locator(".mention-profile-card");
        await expect(profile).toHaveAttribute("dir", locale === "ar" ? "rtl" : "ltr");
        const bounds = await profile.boundingBox();
        expect(bounds.x).toBeGreaterThanOrEqual(0);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(width + 1);
        expect(
          await profile.evaluate(
            (element) => element.scrollWidth <= element.clientWidth + 1,
          ),
        ).toBeTruthy();
        await test.info().attach(`persona-${locale}-${colorScheme}-${width}`, {
          body: await page.screenshot(),
          contentType: "image/png",
        });
      }
    }
  }
});
