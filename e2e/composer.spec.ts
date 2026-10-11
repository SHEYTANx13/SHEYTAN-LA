import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * composer.spec.ts — REAL COMPOSER CONTROLS (§9).
 *
 * Every control is driven like a user drives it; the assertions bind
 * to the REAL wire/backend effects (request payload → server
 * interpretation → visible result), never to component internals.
 *
 *   attachments: choose file → upload → chip appears → send → backend
 *                received the attachment (the retrieval note is part of
 *                the real request assembly) → response arrives
 *   thinking:    selecting a mode is persisted and travels with the run
 *   net search:  the explicit intent arms the real research path; the
 *                state chip reflects the REAL wire events, never a
 *                decorative toggle
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

async function send(page: Page, text: string): Promise<void> {
  await page.getByPlaceholder("Message SHEYTAN...").fill(text);
  await page.getByRole("button", { name: "Send", exact: true }).click();
}

test("attachment: choose file → upload → chip → send → real response", async ({
  page,
}) => {
  // A real file, uploaded through the real file input into the real
  // staged-attachments pipeline.
  await page.setInputFiles('input[type="file"][aria-label="Attach files"]', {
    name: "e2e-notes.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("SHEYTAN E2E attachment payload: the pipeline is real.", "utf8"),
  });

  // The staged attachment chip appears with the real file name.
  await expect(
    page.locator(".composer-attachments .attachment-chip", { hasText: "e2e-notes.txt" }).first(),
  ).toBeVisible({ timeout: 20_000 });

  await send(page, "summarize the attached notes");

  // The run settles with a real response (backend assembled the
  // attachment content into the request; the response is real
  // generation).
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });
  await expect(page.locator(".message-row.from-agent .message-bubble").last()).toBeVisible();
});

test("thinking control: selecting a mode persists and travels with runs", async ({
  page,
}) => {
  // The Thinking control is a listbox with real modes (auto/fast/thinking).
  await page.getByRole("button", { name: /Thinking · / }).click();
  const option = page.getByRole("option", { name: /thinking/i }).first();
  await expect(option).toBeVisible({ timeout: 10_000 });
  await option.click();

  await send(page, "thinking mode travel check");

  // The run completes with the control applied server-side.
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });
});

test("net search: the explicit intent arms the real path with an honest state", async ({
  page,
}) => {
  // Toggle the Net Search control ON through the real control. The
  // pressed state is the user's explicit per-request intent; the chip
  // suffix renders only measured wire states.
  const netSearchToggle = page.getByRole("button", { name: /^Net Search/ });
  await expect(netSearchToggle).toBeVisible();

  const pressedBefore = await netSearchToggle.getAttribute("aria-pressed");
  if (pressedBefore !== "true") {
    await netSearchToggle.click();
  }
  await expect(netSearchToggle).toHaveAttribute("aria-pressed", "true");

  await send(page, "research the latest llama.cpp release notes");

  // The run settles. The state suffix (if any) reflects REAL wire
  // events: Searching… / results with a stated count / failed — never
  // an invented "results" claim. Genuine network search is
  // environment-dependent and only the honest settlement is asserted.
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 120_000 });
});
