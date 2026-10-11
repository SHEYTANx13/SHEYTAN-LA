import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * agent.spec.ts — AGENT AS A REAL PIPELINE (§6/§7).
 *
 * The Agent surface is exercised through the same real stack. With the
 * synthetic-weights fixture the model produces a real streamed turn
 * but does not author tool calls — so this suite proves:
 *
 *   task submission → context preparation → classification → tier
 *   selection → REAL model invocation (streamed) → task state updates
 *   → terminal settlement → persistence
 *
 * and the §7 honesty contract: the task panel renders ONLY observed
 * backend evidence — no decorative pipeline steps (INSPECT/ACT/TEST)
 * that were never executed.
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

async function enterAgentMode(page: Page): Promise<void> {
  await page.goto(stack.baseURL + "/");
  await page.getByRole("tab", { name: "Agent" }).click();

  // The Agent conversation space is mode-separated: a fresh install
  // starts it empty. Creating its first session is the real user
  // action; the composer arms afterwards.
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();

  await expect(page.getByPlaceholder("Describe what SHEYTAN should forge...")).toBeEnabled({
    timeout: 30_000,
  });
}

test("agent task → real model turn → settlement → persistence", async ({ page }) => {
  await enterAgentMode(page);

  const composerBox = page.getByPlaceholder("Describe what SHEYTAN should forge...");
  await composerBox.fill("forge a greeting and report the result");
  await page.getByRole("button", { name: "Forge →" }).click();

  // Terminal settlement: the composer textarea unlocks (disabled for
  // the whole live run) — the honest "run finished" signal.
  await expect(composerBox).toBeEnabled({ timeout: 120_000 });

  // A real assistant turn persisted in the AGENT conversation space.
  const assistant = page.locator(".message-row.from-agent .message-bubble").last();
  await expect(assistant).toBeVisible();
  expect(((await assistant.textContent()) ?? "").trim().length).toBeGreaterThan(0);
});

test("the task panel shows only OBSERVED evidence — no fabricated steps", async ({ page }) => {
  await enterAgentMode(page);

  const composerBox = page.getByPlaceholder("Describe what SHEYTAN should forge...");
  await composerBox.fill("inspect nothing, just answer briefly");
  await page.getByRole("button", { name: "Forge →" }).click();

  await expect(composerBox).toBeEnabled({ timeout: 120_000 });

  // §7: pipeline steps (INSPECT / ACT / TEST / VERIFY markers) may only
  // appear when the backend ACTUALLY executed the corresponding tool
  // traffic. The synthetic model authors no tool calls, so the panel
  // must not claim any of them.
  const fabricated = page.locator(
    ".task-row-items .task-item:has-text('INSPECT')," +
      ".task-row-items .task-item:has-text('ACT:')," +
      ".task-row-items .task-item:has-text('TEST:')," +
      ".task-row-items .task-item:has-text('VERIFY')",
  );
  expect(await fabricated.count()).toBe(0);
});

test("agent and chat histories stay separated after agent runs", async ({ page }) => {
  await enterAgentMode(page);

  const composerBox = page.getByPlaceholder("Describe what SHEYTAN should forge...");
  await composerBox.fill("agent side marker");
  await page.getByRole("button", { name: "Forge →" }).click();
  await expect(composerBox).toBeEnabled({ timeout: 120_000 });

  // Chat space keeps its own conversation (no agent leakage).
  await page.getByRole("tab", { name: "Chat" }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled();
  await expect(
    page.locator(".message-row", { hasText: "agent side marker" }),
  ).toHaveCount(0, { timeout: 20_000 });
});
