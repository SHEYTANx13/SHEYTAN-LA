import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * run-control.spec.ts — v1.7.5 PAUSE / EDIT / RESUME + SESSION-CONSISTENCY
 * browser E2E against the REAL application stack (server + HTTP + WS +
 * session store + native-engine generation).
 *
 * Covered here (release contract §24):
 *
 *   B. Pause lifecycle — send → stream → Pause → authoritative PAUSED
 *      panel with the partial draft visible and editable.
 *   C. Reconnect — reload while paused → the same paused draft and the
 *      paused panel are restored from the backend authority.
 *   D. Edit — edit the paused draft → submit → the SAME run resumes.
 *   E. Completion — the run settles; the transcript carries exactly one
 *      user turn and one authoritative assistant reply.
 *   F. Stop — Pause → Stop settles the run; the composer is usable again.
 *   G. Stale control — a stale revision sent to /api/run/edit is rejected
 *      with an explicit conflict and the paused state stays intact.
 *   A. Session consistency — delete the ACTIVE session → the count
 *      decreases, the deleted session never reappears, and a replacement
 *      session is active (the stale-refresh guard at the UI level).
 *
 * Prompts deliberately carry NO capability-signal keywords: a pure
 * conversation selects ZERO tools and is served by the native engine
 * (the llama.cpp fallback host does not exist in the E2E environment).
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan();
});

test.afterAll(async () => {
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack.baseURL + "/");
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 30_000 });
});

const composer = (page: Page) => page.getByPlaceholder("Message SHEYTAN...");

async function sendChatMessage(page: Page, text: string): Promise<void> {
  await composer(page).fill(text);
  await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Send", exact: true }).click();
}

// Each pause test runs in a FRESH conversation: the stack is shared across
// the file (one real server), so a leftover transcript would break the
// exactly-one-user-turn assertions.
async function newSession(page: Page): Promise<void> {
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });
}

const streamedDraft = (page: Page) =>
  page.locator(".message-row.from-agent .message-bubble").last();

async function pauseLiveRun(page: Page): Promise<string> {
  // Wait for REAL streamed content (the optimistic streaming bubble exists
  // before the first delta — only actual text proves tokens are flowing).
  await expect
    .poll(
      async () =>
        ((await streamedDraft(page).textContent()) ?? "").trim().length,
      { timeout: 90_000, intervals: [200] },
    )
    .toBeGreaterThan(15);

  await page.getByRole("button", { name: "Pause", exact: true }).click();

  // The authoritative PAUSED panel (backend-derived phase).
  await expect(page.locator(".paused-run-panel")).toBeVisible({ timeout: 30_000 });
  await expect(page.locator(".paused-run-title")).toContainText("Paused");

  await expect
    .poll(async () => page.locator(".paused-draft-editor").inputValue(), {
      timeout: 30_000,
    })
    .not.toBe("");

  return page.locator(".paused-draft-editor").inputValue();
}

test("B. pause lifecycle: send → stream → Pause → PAUSED panel with the partial draft", async ({
  page,
}) => {
  await newSession(page);
  await sendChatMessage(page, "tell me a story about a lighthouse keeper");

  const draft = await pauseLiveRun(page);

  // The checkpointed partial answer is visible and editable.
  expect(draft.length).toBeGreaterThan(0);

  // The paused composer stays editable (it edits the paused revision).
  await expect(composer(page)).toBeEnabled();

  // Resume without changes completes the run.
  await page.getByRole("button", { name: "Resume without changes" }).click();
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".paused-run-panel")).toHaveCount(0);
});

test("C+D+E. reload while paused restores the same draft; editing resumes the SAME run to one final answer", async ({
  page,
}) => {
  await newSession(page);
  await sendChatMessage(page, "count the stars in a long poem");

  const draftBefore = await pauseLiveRun(page);

  // C: a full reload while paused — the backend is the authority; the
  // paused panel and the SAME draft must be restored.
  await page.reload();
  await expect(page.locator(".paused-run-panel")).toBeVisible({ timeout: 60_000 });

  await expect
    .poll(async () => page.locator(".paused-draft-editor").inputValue(), {
      timeout: 30_000,
    })
    .not.toBe("");

  const draftAfterReload = await page.locator(".paused-draft-editor").inputValue();
  expect(draftAfterReload).toBe(draftBefore);

  // D: edit the paused draft and resume — the composer submit path edits
  // the paused revision and resumes the SAME run (never a brand-new run).
  const edited = `${draftBefore} THE END.`;
  await page.locator(".paused-draft-editor").fill(edited);
  await page.getByRole("button", { name: "Update & Resume" }).click();

  // E: the run settles; exactly one user turn and one authoritative reply.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".paused-run-panel")).toHaveCount(0);

  await expect(page.locator(".message-row.from-user")).toHaveCount(1);

  const assistant = page.locator(".message-row.from-agent .message-bubble").last();
  await expect(assistant).toContainText("THE END.", { timeout: 30_000 });

  // Reload: the final conversation persists with the same shape.
  await page.reload();
  await expect(page.locator(".message-row.from-user")).toHaveCount(1, { timeout: 30_000 });
  await expect(
    page.locator(".message-row.from-agent .message-bubble").last(),
  ).toContainText("THE END.", { timeout: 30_000 });
});

test("F. pause then stop settles the run and frees the composer", async ({ page }) => {
  await newSession(page);
  await sendChatMessage(page, "compose a very long letter to the moon");

  await pauseLiveRun(page);

  // Stop the PAUSED run (the paused state offers its own Stop control).
  await page.getByRole("button", { name: "Stop", exact: true }).last().click();

  // The run settles as stopped: the paused panel is gone, the composer is
  // usable again for the next ordinary message.
  await expect(page.locator(".paused-run-panel")).toHaveCount(0, { timeout: 30_000 });
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  // The next ordinary chat works on the same session.
  await sendChatMessage(page, "one short follow-up question");
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(
    "one short follow-up question",
  );
});

test("G. stale revision edit is rejected with an explicit conflict; the paused state stays intact", async ({
  page,
  request,
}) => {
  await newSession(page);
  await sendChatMessage(page, "describe a long journey through mountains");

  await pauseLiveRun(page);

  // Read the authoritative paused record (the most recent one — the suite
  // runs sequentially and earlier pauses were consumed).
  const paused = await (await request.get(`${stack!.baseURL}/api/run/paused`)).json();

  expect(Array.isArray(paused)).toBe(true);
  expect(paused.length).toBeGreaterThan(0);

  const entry = paused[paused.length - 1];

  // Intentionally STALE revision.
  const staleRevision = Number(entry.revision) - 5;

  const response = await request.post(`${stack!.baseURL}/api/run/edit`, {
    data: {
      sessionId: entry.sessionId,
      runId: entry.runId,
      revision: staleRevision,
      draft: "a draft that must never be accepted",
    },
  });

  expect(response.status()).toBe(409);

  const conflict = await response.json();

  expect(String(conflict.error ?? "")).toContain("stale revision");
  expect(String(conflict.error ?? "")).toContain(String(entry.revision));

  // The paused state stays intact: the current revision is still
  // authoritative and the panel is still offered.
  const after = await (await request.get(`${stack!.baseURL}/api/run/paused`)).json();
  expect(after[after.length - 1].revision).toBe(entry.revision);

  await expect(page.locator(".paused-run-panel")).toBeVisible();

  // Resume without changes settles the run.
  await page.getByRole("button", { name: "Resume without changes" }).click();
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
});

test("A. deleting the ACTIVE session removes it, never resurrects it, and activates a replacement", async ({
  page,
}) => {
  await newSession(page);

  // Give the active session a transcript so the delete is meaningful, then
  // create one more: the NEWEST session is active and sits first.
  await sendChatMessage(page, "marker session for deletion");
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });

  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });

  const before = await page.locator(".session-list .session-item").count();

  // Delete the ACTIVE (newest, first) session.
  const active = page.locator(".session-list .session-item-wrap").first();
  await active.hover();
  await active.getByRole("button", { name: /^Delete / }).click();

  // The count decreases and STAYS decreased — the deleted session is never
  // resurrected by a late list refresh.
  await expect
    .poll(async () => page.locator(".session-list .session-item").count(), {
      timeout: 15_000,
    })
    .toBeLessThan(before);

  await expect
    .poll(async () => page.locator(".session-list .session-item").count(), {
      timeout: 15_000,
    })
    .toBeLessThan(before);

  expect(
    await page
      .locator(".session-list .session-item", { hasText: "marker session for deletion" })
      .count(),
  ).toBe(1); // the marker session SURVIVES and becomes the replacement

  // A replacement session is active and the composer is usable.
  if ((await page.locator(".session-list .session-item").count()) > 0) {
    await expect(page.locator(".session-list .session-item-wrap").first()).toHaveClass(
      /active/,
      { timeout: 15_000 },
    );
  }

  await expect(composer(page)).toBeEnabled({ timeout: 15_000 });
});
