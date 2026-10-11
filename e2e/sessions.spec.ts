import { expect, test } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * sessions.spec.ts — SESSION LIFECYCLE + CHAT/AGENT MODE SEPARATION (§5/§8).
 *
 *   new session → select session → rename → delete → switch sessions
 *   Chat A → switch Agent (B) → switch back Chat → A intact
 *
 * Mode-separated conversation spaces are asserted through the UI: each
 * space keeps its own active session; switching modes never leaks one
 * space's transcript into the other.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan();
});

test.afterAll(async () => {
  // v1.5.0: a failed beforeAll leaves stack undefined — the afterAll must
  // never mask the PRIMARY startup failure with a secondary teardown error.
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack.baseURL + "/");
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 30_000 });
});

test("new session → empty and ready; the old transcript is not shown", async ({ page }) => {
  const composerBox = page.getByPlaceholder("Message SHEYTAN...");
  await composerBox.fill("belongs to the first session");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();

  // The new session's surface shows the (truthful) empty invitation —
  // the previous session's messages are gone from THIS surface.
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });
  await expect(page.getByText("Ready when you are")).toBeVisible();
});

test("switching sessions preserves each transcript", async ({ page }) => {
  const composerBox = page.getByPlaceholder("Message SHEYTAN...");

  // Session A with a recognizable marker.
  await composerBox.fill("marker alpha session");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  // Session B.
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });

  // Back to session A (the item now carries the derived title from its
  // first message — the title-clobber regression is pinned here).
  const itemA = page
    .locator(".session-list .session-item", { hasText: "marker alpha session" })
    .first();
  await expect(itemA).toBeVisible({ timeout: 20_000 });
  await itemA.click();
  await expect(page.locator(".message-row.from-user").last()).toContainText(
    "marker alpha session",
    { timeout: 30_000 },
  );
});

test("rename session updates the sidebar label", async ({ page }) => {
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".session-list .session-item").first()).toBeVisible();

  // Rename via double-click on the session title (the documented
  // gesture; the ✎ action button reveals on hover as the alternative).
  await page.locator(".session-list .session-item").first().dblclick();
  const renameBox = page.locator(".session-rename-input");
  await expect(renameBox).toBeVisible();
  await renameBox.fill("renamed-e2e-session");
  await renameBox.press("Enter");

  await expect(
    page.locator(".session-list .session-item", { hasText: "renamed-e2e-session" }).first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("delete session removes it and activates a remaining one", async ({ page }) => {
  // v1.8.3 REPAIR (Actions run 36713108772, Linux x64 job 109880449134:
  // "Expected: < 4, Received: 4" after 15 s). ROOT CAUSE, proven with an
  // instrumented reproduction: the count baseline was sampled BEFORE the
  // asynchronous "New session" create landed in the sidebar (the old
  // `expect(first()).toBeVisible()` gate was satisfied by the PRE-EXISTING
  // items, not by the new one). The delete then correctly removed exactly
  // one session — verified against the server's authoritative list — but
  // the just-created session (whose POST response was still in flight at
  // sampling time) kept the count flat, so `after < before` compared equal
  // numbers for 15 seconds. The repair is a STATE-BASED wait for the
  // created session (count increases — no sleeps, no timeout bumps), and
  // the assertions pin the SPECIFIC deleted id, not just the count.
  const before = await page.locator(".session-list .session-item").count();
  expect(before).toBeGreaterThanOrEqual(1);

  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();

  // The created session is newest-first: it IS the first row. Wait for it
  // to actually appear (state-based: the count grows by exactly one).
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(before + 1, {
    timeout: 20_000,
  });

  // Every surviving session keeps its row — the baseline identity set.
  const survivorIds = await page
    .locator(".session-list .session-item-wrap")
    .evaluateAll((rows) =>
      rows.slice(1).map((row) => (row as HTMLElement).dataset.sessionId ?? ""),
    );

  // The SPECIFIC session this test deletes (unambiguous id).
  const createdId = await page
    .locator(".session-list .session-item-wrap")
    .first()
    .evaluate((row) => (row as HTMLElement).dataset.sessionId ?? "");
  expect(createdId).toBeTruthy();

  // Delete via the per-session action button. The actions reveal on
  // hover (the real user gesture); Playwright's hover makes them
  // actionable, then the click runs.
  const first = page.locator(".session-list .session-item-wrap").first();
  await first.hover();
  await first.getByRole("button", { name: /^Delete / }).click();

  // The SPECIFIC deleted id disappears and stays gone.
  await expect(
    page.locator(`.session-list .session-item-wrap[data-session-id="${createdId}"]`),
  ).toHaveCount(0, { timeout: 15_000 });
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(before, {
    timeout: 15_000 });

  // Every pre-existing session survived untouched (identity set equal).
  const afterIds = await page
    .locator(".session-list .session-item-wrap")
    .evaluateAll((rows) => rows.map((row) => (row as HTMLElement).dataset.sessionId ?? ""));
  expect(afterIds.sort()).toEqual([...survivorIds].sort());

  // A replacement session is active and the composer is usable.
  if (before > 0) {
    await expect(page.locator(".session-list .session-item-wrap").first()).toHaveClass(
      /active/,
      { timeout: 15_000 },
    );
  }

  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({
    timeout: 15_000,
  });

  // A FRESH AUTHORITATIVE fetch must not resurrect the deleted id: a full
  // reload re-runs the startup list fetch from the backend's own state.
  await page.reload();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 30_000 });
  await expect(
    page.locator(`.session-list .session-item-wrap[data-session-id="${createdId}"]`),
  ).toHaveCount(0, { timeout: 15_000 });
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(before, {
    timeout: 15_000,
  });
});

test("a session created while the startup list response is still in flight is never dropped by it", async ({
  page,
}) => {
  // v1.8.3 REGRESSION (the init consumer of the v1.7.5 guard): the
  // sidebar's "New session" button is actionable WHILE the startup
  // GET /api/sessions is on the wire. The v1.8.2 initializeAgentOnce
  // wrote its response WITHOUT a generation ticket, so the stale
  // startup list (which cannot contain the just-created session)
  // clobbered the sidebar and the created session VANISHED. The repair
  // routes the startup write through the same one-authority guard; the
  // stale response must never land.
  //
  // The interleaving is forced DETERMINISTICALLY (no sleeps), matching
  // the exact CI-latency causality of the v1.8.2 defect:
  //
  //   1. the startup GET is dispatched and its response HELD;
  //   2. the user clicks "New session" — the POST is served and its
  //      response REACHES THE PAGE (the created session is added to the
  //      client state);
  //   3. only THEN is the stale startup list response (which cannot
  //      contain the created session) delivered to the page.
  //
  // The v1.8.2 init consumer wrote that stale response unguarded — the
  // created session VANISHED from the sidebar. The v1.8.3 repair routes
  // the startup write through the same one-authority generation guard
  // (the create invalidated the ticket), so the stale list is discarded
  // and the list re-resolved from the authoritative backend.
  let holdFirstListGet = true;
  let releaseHeldList: (() => void) | undefined;
  const postResponseReachedPage = new Promise<void>((resolve) => {
    releaseHeldList = resolve;
  });

  await page.route(/\/api\/sessions(\?.*)?$/, async (route) => {
    const method = route.request().method();

    if (method === "POST") {
      await route.continue();
      return;
    }

    if (method === "GET" && holdFirstListGet) {
      holdFirstListGet = false;

      // The server answers the startup list NOW; the RESPONSE delivery
      // to the page is held until the create's response has reached it.
      const response = await route.fetch();
      await postResponseReachedPage;
      await route.fulfill({ response });
      return;
    }

    await route.continue();
  });

  // The signal that releases the held startup response: the create's
  // POST response ARRIVING AT THE PAGE (never a timer).
  const postResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/sessions") &&
      response.request().method() === "POST",
  );
  const initListResponse = page.waitForResponse(
    (response) =>
      response.url().includes("/api/sessions?mode=chat") &&
      response.request().method() === "GET",
  );

  void postResponse.then(() => releaseHeldList?.());

  await page.goto(stack.baseURL + "/");

  // While the startup list response is still held in flight, create a
  // session through the real sidebar (the button is live during init).
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();

  // The AUTHORITATIVE identity of the created session comes from the
  // create's own POST response — never from a DOM row that the stale
  // response might already have reshuffled by sampling time.
  const createResult = await postResponse;
  const created = (await createResult.json()) as { id: string };
  expect(created.id).toBeTruthy();

  // The created session is in the sidebar exactly once.
  await expect(
    page.locator(
      `.session-list .session-item-wrap[data-session-id="${created.id}"]`,
    ),
  ).toHaveCount(1, { timeout: 15_000 });

  // The held startup response arrives NOW (state-based wait on the
  // response itself — never a sleep). It must NOT drop the created
  // session, and the created session must appear EXACTLY ONCE (the
  // idempotent prepend): the stale response must neither erase it nor
  // duplicate it.
  await initListResponse;

  await expect(
    page.locator(
      `.session-list .session-item-wrap[data-session-id="${created.id}"]`,
    ),
  ).toHaveCount(1, { timeout: 15_000 });

  // The composer is usable and the created session is genuinely active.
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 30_000 });
  await expect(
    page.locator(
      `.session-list .session-item-wrap[data-session-id="${created.id}"]`,
    ),
  ).toHaveClass(/active/, { timeout: 15_000 });
});

test("Chat ↔ Agent modes keep independent conversation spaces", async ({ page }) => {
  const chatComposer = page.getByPlaceholder("Message SHEYTAN...");
  await expect(chatComposer).toBeEnabled();

  // Chat space marker.
  await chatComposer.fill("chat space marker");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  // Switch to the Agent space. With no Agent sessions yet, the space
  // shows its honest empty state; creating one (the real user action)
  // arms the composer.
  await page.getByRole("tab", { name: "Agent" }).click();
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  const agentComposer = page.getByPlaceholder("Describe what SHEYTAN should forge...");
  await expect(agentComposer).toBeEnabled({ timeout: 30_000 });

  // The Agent space does NOT show the Chat transcript.
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });
  await agentComposer.fill("agent space own marker");
  await page.getByRole("button", { name: "Forge →" }).click();
  await expect(agentComposer).toBeEnabled({ timeout: 120_000 });

  // Switch back to Chat: the marker conversation is intact.
  await page.getByRole("tab", { name: "Chat" }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled();
  await expect(page.locator(".message-row.from-user").last()).toContainText("chat space marker", {
    timeout: 30_000,
  });
  await expect(
    page.locator(".message-row", { hasText: "agent space own marker" }),
  ).toHaveCount(0, { timeout: 10_000 });
});
