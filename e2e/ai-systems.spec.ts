import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * ai-systems.spec.ts — v1.9.0: the AI SYSTEM + GOAL UI FLOW, end to end
 * against the REAL stack (real server, real engine, real persistence).
 *
 * Proves through the browser:
 *
 *   1. the System Centre exposes the AI System selector with the
 *      always-present DEFAULT system active on a fresh install (the
 *      pre-v1.9 behavior contract);
 *   2. creating a system and activating it works through the REAL
 *      backend (revision 1, activation confirmed, active flag moves);
 *   3. an activation SURVIVES A RELOAD (the active pointer is durable,
 *      not client state);
 *   4. the Goal surface creates a durable goal through the backend and
 *      its lifecycle actions (cancel → terminal) work through the UI;
 *      a cancelled goal stays cancelled (never falsely running).
 *
 * No mock substitute: every assertion runs against the same headless
 * server the Linux release gate uses.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan({
    model: "e2e.gguf",
    modelGenerator: "make-e2e-model.py",
    maxTokens: 96,
  });
});

test.afterAll(async () => {
  await stack?.stop();
});

const openSystemCentre = async (page: Page) => {
  await page.goto(stack!.baseURL + "/");
  await page.getByRole("tab", { name: "System" }).click();
  const heading = page.getByText(/AI SYSTEMS/).first();
  await expect(heading).toBeVisible({ timeout: 60_000 });
};

test("the default AI System exists and is active on a fresh install", async ({
  page,
}) => {
  await openSystemCentre(page);

  const selector = page.getByLabel("Active AI System");
  await expect(selector).toBeVisible({ timeout: 30_000 });

  // Exactly one system on a fresh install: the DEFAULT, already active.
  const options = selector.locator("option");
  await expect(options).toHaveCount(1);
  await expect(options.first()).toHaveText(/Default · rev 1 — ACTIVE/);
});

test("create + activate an AI System; the activation survives a reload", async ({
  page,
}) => {
  await openSystemCentre(page);

  const selector = page.getByLabel("Active AI System");
  await expect(selector).toBeVisible({ timeout: 30_000 });

  // Create: the compact editor rides the same card.
  await page.getByRole("button", { name: "New system" }).click();
  await page.getByLabel("Name").fill("E2E Caretaker");
  await page
    .getByLabel("Instructions")
    .fill("Always verify file changes before claiming success.");
  await page.getByRole("button", { name: "Create", exact: true }).click();

  // The new system appears selected as the draft target (rev 1).
  await expect(page.getByText(/created E2E Caretaker/)).toBeVisible({
    timeout: 30_000,
  });

  // Activate it (the default was active; the new one is not yet).
  await selector.selectOption({ label: "E2E Caretaker · rev 1" });
  await page
    .getByRole("button", { name: /^Activate$/ })
    .first()
    .click();
  await expect(page.getByText(/activated rev 1/)).toBeVisible({
    timeout: 30_000,
  });

  // RELOAD: the active pointer is DURABLE backend state.
  await page.reload();
  await page.getByRole("tab", { name: "System" }).click();
  const selector2 = page.getByLabel("Active AI System");
  await expect(selector2).toBeVisible({ timeout: 60_000 });
  await expect(
    selector2.locator('option[value^="sys-"]', { hasText: "E2E Caretaker" }),
  ).toHaveText(/— ACTIVE/);
});

test("a goal created through the UI is durable and cancel keeps it terminal", async ({
  page,
}) => {
  await openSystemCentre(page);

  const input = page.getByPlaceholder(/Describe a long-horizon objective/);
  await expect(input).toBeVisible({ timeout: 30_000 });

  await input.fill("inventory the e2e fixture workspace and report");
  await page.getByRole("button", { name: "Start goal" }).click();

  // The goal appears with an honest lifecycle chip (created + started:
  // the drive may already be past understanding when we look — the chip
  // must be one of the live or settled states, never a fabricated one).
  const chip = page
    .locator(".settings-chip", {
      hasText: /awaiting|executing|understanding|planning|acting|verifying|paused|blocked|failed|completed|cancelled/,
    })
    .first();
  await expect(chip).toBeVisible({ timeout: 30_000 });

  // Cancel: terminal, and it stays terminal after a reload.
  await page.getByRole("button", { name: "Cancel", exact: true }).first().click();
  await expect(
    page.locator(".settings-chip", { hasText: "CANCELLED" }).first(),
  ).toBeVisible({ timeout: 30_000 });

  await page.reload();
  await page.getByRole("tab", { name: "System" }).click();
  await expect(
    page.locator(".settings-chip", { hasText: "CANCELLED" }).first(),
  ).toBeVisible({ timeout: 60_000 });
});
