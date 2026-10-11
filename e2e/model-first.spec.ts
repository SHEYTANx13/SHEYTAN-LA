import { expect, test, type Page } from "@playwright/test";
import { copyFile } from "node:fs/promises";
import { join } from "node:path";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * model-first.spec.ts — THE v1.5.0 MODEL-FIRST STARTUP CONTRACT (§2).
 *
 * Proven through the browser against the REAL stack, starting from the
 * FRESH-INSTALL state (config.model = "" with a real GGUF present in
 * the models dir — the exact shape that used to prewarm an arbitrary
 * "first .gguf"):
 *
 *   launch → local models DISCOVERED without loading one → Model
 *   Selector shown → composer gated → user selects the model →
 *   Analyzing/Configuring/Loading (backend-authoritative) → engine
 *   ready → real streaming generation → persistence.
 *
 * v1.5.1 extends the real-stack coverage to the rest of the §9 list:
 *
 *   - automatic profile application (AUTO applies a measured-evidence
 *     profile; the honest "not benchmarked" note is asserted — a green
 *     run never claims a benchmark that did not run);
 *   - Chat/Agent TASK selection (the surface's task travels with the
 *     selection — switching surfaces and re-selecting re-tunes for THAT
 *     surface, never the backend's silent default);
 *   - explicit model change (a second model file, explicitly picked in
 *     the Agent surface) + the previous-selection marker;
 *   - persistence after reload (model, task and serving state survive).
 *
 * Winner/rollback behavior of the bounded calibration is proven by the
 * deterministic Go control-contract tests (internal/calibration) — this
 * stack runs the native C++ engine, which has no llama launch-arg
 * profile surface to calibrate, so an honest e2e run asserts exactly
 * the "not benchmarked" state instead of fabricating a pass.
 *
 * The fixture engine is the real C++ host executing the wide-context
 * synthetic fixture: generation is real (real logits/sampler/streaming);
 * the token CONTENT is synthetic-weights noise — the suite proves the
 * PIPELINE, never model quality.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  // noModel: the fresh-install shape — no selected model, engine
  // auto-start ARMED but gated on the selection.
  stack = await startSheytan({ noModel: true });
});

test.afterAll(async () => {
  // v1.5.0: a failed beforeAll leaves stack undefined — the afterAll must
  // never mask the PRIMARY startup failure with a secondary teardown error.
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack.baseURL + "/");
});

// The composer TEXTAREA (stable locator — the placeholder honestly
// changes with the model-selection state, so placeholder matching would
// only work in one phase).
const composer = (page: Page) => page.locator(".composer textarea");

async function selectTheModel(page: Page): Promise<void> {
  // The Model Selector is the content area on a fresh install. When the
  // model is ALREADY selected (a previous test in this file completed
  // the selection), the picker is closed and the composer is armed —
  // the selection step is legitimately a no-op.
  const card = page.getByTestId("model-card-e2e-wide.gguf");

  if (await card.isVisible().catch(() => false)) {
    await card.getByRole("button", { name: "Use model" }).click();
    return;
  }

  // Not visible: either the picker is still loading or the selection
  // already happened. Distinguish by the backend's own state.
  const engine = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as { selectionRequired?: boolean };
  });

  if (engine.selectionRequired) {
    // Still unselected — wait for discovery and select for real.
    await expect(card).toBeVisible({ timeout: 30_000 });
    await card.getByRole("button", { name: "Use model" }).click();
  }
}

test("launch → models discovered WITHOUT loading → Model Selector shown", async ({
  page,
}) => {
  // The application shell renders with the product identity.
  await expect(page.getByRole("banner")).toContainText("SHEYTAN", { timeout: 30_000 });

  // The selector is the primary surface (fresh install, no selection).
  await expect(page.getByRole("region", { name: "Model selection" })).toBeVisible({
    timeout: 30_000,
  });

  // Discovery found the fixture model and lists its measured facts —
  // WITHOUT loading it (the engine state is pre-selection).
  const card = page.getByTestId("model-card-e2e-wide.gguf");
  await expect(card).toBeVisible();
  await expect(card).toContainText("e2e-wide");

  // The engine snapshot is the honest pre-selection state: selection
  // required, engine idle — never an arbitrary model boot.
  const state = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as {
      state: string;
      selectionRequired?: boolean;
      phase?: string;
    };
  });

  expect(state.selectionRequired).toBe(true);
  expect(state.phase).toBe("model-selection");
  expect(["idle", "stopped"]).toContain(state.state);
});

test("fresh install gates the composer until a model is selected", async ({ page }) => {
  await expect(composer(page)).toBeDisabled({ timeout: 30_000 });

  // The honest gate points at the Model Selector.
  await expect(page.locator(".composer-selection-gate")).toContainText(
    "Choose a model to start",
  );
});

test("select → Analyzing/Configuring/Loading → Ready → the selected model serves", async ({
  page,
}) => {
  await selectTheModel(page);

  // The selection flow settles — the composer unlocks once the
  // selection applied and the engine serves the SELECTED model. (The
  // in-picker phase banner is TRANSIENT by design: the picker closes
  // itself the moment the switch applies, so the durable proof is the
  // terminal state asserted below — state-based waits, no sleeps.)
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  // The backend verifies the selected model is what actually serves.
  const state = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as {
      state: string;
      selectionRequired?: boolean;
      backend?: string;
      loadedPath?: string;
      selection?: { phase: string; model?: string };
    };
  });

  expect(state.selectionRequired).toBeFalsy();
  expect(state.selection?.phase).toBe("ready");
  expect(state.selection?.model).toBe("e2e-wide.gguf");
  expect(state.loadedPath ?? "").toContain("e2e-wide.gguf");
});

test("after selection: send → real streaming → settles → persists (the §7 chat chain)", async ({
  page,
}) => {
  await selectTheModel(page);

  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const marker = `model-first persist ${Date.now()}`;
  await composer(page).fill(marker);
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // The run settles and a non-empty assistant bubble persisted.
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker);

  const assistant = page.locator(".message-row.from-agent .message-bubble").last();
  await expect(assistant).toBeVisible();
  const text = (await assistant.textContent()) ?? "";
  expect(text.trim().length).toBeGreaterThan(0);

  // Full reload: the conversation remains (real persistence).
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker, {
    timeout: 30_000,
  });
});

test("AUTO applies an evidence profile and reports it honestly (no fake benchmark)", async ({
  page,
}) => {
  await selectTheModel(page);
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  // AUTO applied a real profile from the recommendation engine — the
  // context is set, and the note states the honest truth for this stack
  // (the native engine has no launch-arg surface to calibrate: the note
  // must say "not benchmarked", never claim a winner that never ran).
  const snap = await page.evaluate(async () => {
    const [engine, config] = await Promise.all([
      fetch("/api/engine").then((r) => r.json()),
      fetch("/api/config").then((r) => r.json()),
    ]);
    return {
      selection: engine.selection as {
        phase: string;
        performanceMode: string;
        calibrated?: boolean;
        calibrationNote?: string;
        applied?: { context?: number };
      },
      numCtx: (config.llm as { numCtx?: number }).numCtx,
      runtimeProfile: config.runtimeProfile as string,
    };
  });

  expect(snap.selection.phase).toBe("ready");
  expect(snap.selection.performanceMode).toBe("auto");
  expect(snap.selection.applied?.context ?? 0).toBeGreaterThan(0);
  expect(snap.numCtx ?? 0).toBeGreaterThan(0);

  // The chat surface's selection tuned the CHAT task — the task the UI
  // was on travels with the selection (spec §5).
  expect(snap.runtimeProfile).toBe("chat");

  expect(snap.selection.calibrated).toBeFalsy();
  expect(snap.selection.calibrationNote).toContain("not benchmarked");
});

test("explicit model change on the Agent surface: task retunes + previous marker + reload persistence", async ({
  page,
}) => {
  // A second REAL model file: the fixture generator's output copied
  // under a new name (identical bytes, distinct model identity — the
  // selection contract is about the USER's explicit choice, not about
  // model diversity).
  const modelsDir = join(stack.dataDir, "models");
  await copyFile(
    join(modelsDir, "e2e-wide.gguf"),
    join(modelsDir, "e2e-second.gguf"),
  );

  // Reload first: proves the FIRST selection persisted (model + task),
  // and the discovery now lists BOTH models.
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const before = await page.evaluate(async () => {
    const config = await fetch("/api/config").then((r) => r.json());
    return {
      model: config.model as string,
      runtimeProfile: config.runtimeProfile as string,
    };
  });
  expect(before.model).toBe("e2e-wide.gguf");
  expect(before.runtimeProfile).toBe("chat");

  // Switch to the AGENT surface. The agent surface starts with NO
  // session (an honest empty state) — create one the way a user does,
  // so the composer arms for the assertions below.
  await page.getByRole("tab", { name: "Agent" }).click();
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });

  // The agent runtime panel exposes the model select; the chat rail is
  // hidden in this mode, so exactly one select is visible.
  const modelSelect = page.locator("select[aria-label='Model']:visible");
  await expect(modelSelect).toBeVisible({ timeout: 30_000 });

  // The freshly copied model must be discovered before it can be
  // explicitly chosen.
  await expect
    .poll(
      async () =>
        page.evaluate(async () => {
          const res = await fetch("/api/models");
          const models = (await res.json()) as { local?: Array<{ id?: string }> };
          return (models.local ?? []).some((m) => m.id === "e2e-second.gguf");
        }),
      { timeout: 30_000 },
    )
    .toBe(true);

  // EXPLICIT user choice of the second model on the AGENT surface.
  await modelSelect.selectOption("e2e-second.gguf");

  // The selection flow settles on the new model, tuned for the AGENT
  // task (spec §5: the surface's task travels with the selection).
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  await expect
    .poll(async () =>
      page.evaluate(async () => {
        const config = await fetch("/api/config").then((r) => r.json());
        return config.model as string;
      }),
      { timeout: 30_000 },
    )
    .toBe("e2e-second.gguf");

  const after = await page.evaluate(async () => {
    const config = await fetch("/api/config").then((r) => r.json());
    const engine = await fetch("/api/engine").then((r) => r.json());
    const models = await fetch("/api/models").then((r) => r.json());
    return {
      model: config.model as string,
      runtimeProfile: config.runtimeProfile as string,
      loadedPath: (engine.loadedPath as string) ?? "",
      phase: (engine.selection as { phase?: string })?.phase,
      previous: (models.local as Array<{ id: string; previous?: boolean }>)
        ?.find((m) => m.id === "e2e-wide.gguf")?.previous,
    };
  });

  expect(after.model).toBe("e2e-second.gguf");
  expect(after.runtimeProfile).toBe("agent");
  expect(after.phase).toBe("ready");
  expect(after.loadedPath).toContain("e2e-second.gguf");
  // The previous-selection marker points at the outgoing model.
  expect(after.previous).toBe(true);

  // Full reload: the model change persisted (model, task, serving state).
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const reloaded = await page.evaluate(async () => {
    const config = await fetch("/api/config").then((r) => r.json());
    const engine = await fetch("/api/engine").then((r) => r.json());
    return {
      model: config.model as string,
      runtimeProfile: config.runtimeProfile as string,
      loadedPath: (engine.loadedPath as string) ?? "",
      selectionRequired: engine.selectionRequired as boolean,
    };
  });
  expect(reloaded.model).toBe("e2e-second.gguf");
  expect(reloaded.runtimeProfile).toBe("agent");
  expect(reloaded.loadedPath).toContain("e2e-second.gguf");
  expect(reloaded.selectionRequired).toBeFalsy();
});
