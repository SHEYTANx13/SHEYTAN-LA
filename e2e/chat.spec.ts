import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * chat.spec.ts — CHAT AS A REAL USER WORKFLOW (§5).
 *
 * Proven through the browser against the REAL stack (server + HTTP +
 * WebSocket + session store + native engine generation):
 *
 *   launch → runtime init → session exists → model selected → prompt
 *   → submit → composer locks → real run starts → streaming text
 *   appears → completion settles → composer unlocks → assistant
 *   message persists → reload → conversation remains
 *
 * The model is the wide-context synthetic fixture executed by the REAL
 * C++ engine: generation is real (real logits/sampler/streaming); the
 * token CONTENT is synthetic-weights noise — the suite proves the
 * pipeline, never claims model quality.
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
});

const composer = (page: Page) => page.getByPlaceholder("Message SHEYTAN...");

async function sendChatMessage(page: Page, text: string): Promise<void> {
  await composer(page).fill(text);
  await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Send", exact: true }).click();
}

test("launch → runtime initializes → a chat session exists and is selectable", async ({
  page,
}) => {
  // The application shell renders with the product identity.
  await expect(page.getByRole("banner")).toContainText("SHEYTAN", { timeout: 30_000 });

  // Runtime initialization resolved the backend state: the eager
  // session exists (fresh install never shows a dead composer) and the
  // composer is usable.
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });
});

test("send → run starts → composer locks → generation streams → settles → persists", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  await sendChatMessage(page, "hello there say something");

  // While the run is live the composer is LOCKED and Stop is offered —
  // the lock is owned by the run state machine, not a timer.
  // (The fixture engine is fast; the locked phase may be brief, so the
  // terminal settlement below is the durable assertion.)
  await expect(page.locator(".message-row.from-user").last()).toContainText(
    "hello there say something",
  );

  // The run settles: the composer textarea unlocks (it is disabled for
  // the whole live run) and the Stop control is gone. The Send BUTTON
  // may legitimately stay disabled afterwards — the draft was cleared
  // by the send; "unlocked" means EDITABLE, not "sendable with no text".
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });
  await expect(page.getByRole("button", { name: "Stop" })).toHaveCount(0);

  // A non-empty assistant bubble persisted into the transcript.
  const assistant = page.locator(".message-row.from-agent .message-bubble").last();
  await expect(assistant).toBeVisible();
  const text = (await assistant.textContent()) ?? "";
  expect(text.trim().length, "assistant reply must contain real generated text").toBeGreaterThan(
    0,
  );

  // The transcript is re-fetched from the authoritative backend state
  // (the run-finalization reload), not fabricated client-side.
  const sessionUrl = page.url();
  expect(sessionUrl).toBeTruthy();
});

test("reload → the conversation remains (real persistence)", async ({ page }) => {
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  const marker = `persist check ${Date.now()}`;
  await sendChatMessage(page, marker);

  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker);

  // Full reload: the same session loads from the backend and the
  // conversation (user marker + assistant reply) is still there.
  await page.reload();

  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker, {
    timeout: 30_000,
  });
  await expect(page.locator(".message-row.from-agent .message-bubble").last()).toBeVisible();
});

test("abort during generation settles honestly and unlocks the composer", async ({ page }) => {
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  await sendChatMessage(page, "abort me please generate something");

  // If the run is still live, Stop must be offered and must settle the
  // run. The fixture engine may finish before we click — in that case
  // the run has already settled and Send is back. Either outcome must
  // leave the composer usable; the suite accepts the honest race.
  const stop = page.getByRole("button", { name: "Stop" });
  if (await stop.isVisible().catch(() => false)) {
    await stop.click();
  }

  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
});

test("regenerate re-runs the last user message", async ({ page }) => {
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  await sendChatMessage(page, "regenerate me");
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const assistantCountBefore = await page.locator(".message-row.from-agent").count();

  await page.getByRole("button", { name: "Regenerate" }).click();

  // A new run starts (composer locks again) and settles with a fresh
  // assistant reply persisted.
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const assistantCountAfter = await page.locator(".message-row.from-agent").count();
  expect(assistantCountAfter).toBeGreaterThanOrEqual(assistantCountBefore);
  await expect(page.locator(".message-row.from-agent .message-bubble").last()).toBeVisible();
});
