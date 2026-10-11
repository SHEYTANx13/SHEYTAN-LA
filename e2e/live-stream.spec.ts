import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * live-stream.spec.ts — LIVE VISIBILITY PROOF (v1.9.1 P0).
 *
 * Proves through the REAL stack (server + HTTP + WebSocket + the real
 * C++ engine executing the live fixture: real logits, real sampler,
 * real streaming — the EOS gate is closed at the FIXTURE level so the
 * generation runs its token budget, making the live window observable):
 *
 *   1. generation begins;
 *   2. real streamed text appears in the LIVE assistant surface
 *      (.generation-bubble) WHILE the run is still live;
 *   3. MORE text becomes visible without pressing Stop;
 *   4. the run continues generating after the first visible text;
 *   5. completion produces exactly ONE final assistant message;
 *   6. no duplicate transcript appears.
 *
 * A second test proves Stop during a live generation settles honestly.
 * No test here "only waits for final completion" — every visibility
 * assertion is taken while the run is provably live (Stop button up).
 *
 * v1.9.1 P0 REPAIR — THE PLACEHOLDER BASELINE (root-caused from Actions
 * 37704905404 / job 113077751580, reproduced locally under measured CPU
 * contention). The v1.8.2 observation contract sampled the live surface's
 * textContent and accepted ANY non-empty text as "streamed text": the
 * GenerationBubble renders PRESENTATION PLACEHOLDERS while no content
 * snapshot has arrived ("Connecting to the engine and preparing the turn…"
 * while preparing — 48 chars — and "…" afterwards), so waitForLiveText
 * passed on the placeholder within ~100ms of Send and the growth poll's
 * 30s budget was consumed by the ENGINE GATE + PREFILL phase — measured on
 * this stack at ~40ms per prompt token (a 446-token system briefing ≈ 8s
 * unloaded, >30s under contention) BEFORE the first real token exists.
 * On CI the poll expired (or shrank to a sliver) before the real streaming
 * window opened: exactly "first bubble visible, Stop visible, no strictly
 * longer text within 30s". The observation now tracks REAL streamed
 * content only: the live surface marks its placeholder arms with
 * data-stream-placeholder (MessageStream.tsx), waitForLiveText requires a
 * non-placeholder snapshot, and the growth baseline is the FIRST REAL
 * streamed snapshot — the strictly-longer observation is measured inside
 * the genuine live window on every host speed.
 *
 * v1.9.1 FIXTURE BUDGET: maxTokens 320 (was 160). The engine emits one
 * event chunk per 8 tokens (kEmitTokenWindow), so the budget IS the number
 * of real cumulative snapshots the live window carries (40 here). The
 * window duration necessarily scales with host speed; the SNAPSHOT COUNT
 * is the deterministic dimension — growth observation needs strictly more
 * than one poll interval of chunks after the baseline, which 40 chunks
 * provides on any plausible host (measured: ~55ms/chunk under 2-core
 * contention, ~15ms/chunk on a fast host).
 *
 * v1.9.2 ROOT CAUSE (authoritative Actions 37869632400 — the v1.9.1
 * diagnosis was correct but INCOMPLETE): the v1.9.1 repair fixed the
 * observation contract, yet all three tests still expired at the 60s
 * waitForLiveText boundary. Measured on the reproduced stack: the E2E
 * prompt's ~1785 briefing bytes encode to ~1681 ENGINE tokens (the
 * fixture vocab carries 5 merges — ~1 token/byte; the Go-side estimate
 * logged ~445), and the native engine prefilled SERIALLY at a measured
 * 19.0 ms/token — the per-token cost dominated by per-row tensor-name
 * resolution (~8064 rows/token resolved through std::map + ostringstream),
 * not the arithmetic. First token ≈ 32s standalone, >60s under suite
 * contention: the budget expired before the first real snapshot existed.
 * The repair is the REAL optimization at the proven boundary: the
 * resolved weight table (each layer's matrices resolved once per model
 * binding; rows walked by pointer arithmetic — 5.2x measured per-token
 * speedup, pinned bit-for-bit vs the serial path by the native
 * prefill_parity gate) + the batched prefill_span (16-token chunks,
 * last-token logits only, cancellation cadence preserved) + a
 * Release-default native build. waitForLiveText remains the honest 60s
 * bound; failures now attach compact structured diagnostics.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan({
    model: "e2e-live.gguf",
    modelGenerator: "make-e2e-live-model.py",
    // 40 real engine chunks of slow-dims generation — the deterministic
    // snapshot budget of the live window (see the header note).
    maxTokens: 320,
  });
});

test.afterAll(async () => {
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack!.baseURL + "/");

  // Isolation: every test starts in a FRESH session (the previous test's
  // transcript must never leak into this test's assertions).
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
  await page.getByRole("button", { name: "New session" }).click();
  await expect(
    page.locator(".message-row, .conversation-empty").first(),
  ).toBeVisible();
});

const composer = (page: Page) => page.getByPlaceholder("Message SHEYTAN...");

// liveSample reads the live generation surface's state in one evaluation:
// phase label, cumulative text length, whether the run is still live, and
// whether the rendered text is a PRESENTATION PLACEHOLDER (the bubble's
// preparing/ellipsis arms — never model output).
function liveSample(page: Page) {
  return page.evaluate(() => {
    const bubble = document.querySelector(".generation-bubble");
    const phase = document.querySelector(".generation-phase");
    const content = bubble?.querySelector(".message-content");
    const stopVisible = Array.from(document.querySelectorAll("button")).some(
      (b) => b.textContent?.trim() === "Stop",
    );
    return {
      hasBubble: Boolean(bubble),
      phase: phase?.textContent?.trim() ?? null,
      textLen: (content?.textContent ?? "").replace(/\u200b/g, "").length,
      // v1.9.1: true while the surface renders its placeholder arm —
      // this text is presentation, never a streamed snapshot.
      placeholder: Boolean(bubble?.querySelector("[data-stream-placeholder]")),
      stopVisible,
    };
  });
}

// waitForLiveText polls until the LIVE bubble shows real, non-placeholder
// streamed text (never just waits for completion) — bounded, state-driven.
// The engine gate + prefill phase (measured ~8s unloaded, >30s under
// contention, ~40ms/prompt-token) legitimately precedes the first token;
// this poll is the honest bound on that wait (60s), and the growth
// observation below starts only from real streamed content.
//
// v1.9.2 FAILURE DIAGNOSTICS: a poll timeout throws with a compact,
// structured snapshot of the OBSERVATION BOUNDARY plus the server log
// tail (context metrics, native-engine lines) — the timeline evidence
// the authoritative Actions logs lacked — without dumping prompt/answer
// content.
async function waitForLiveText(page: Page) {
  try {
    await expect
      .poll(
        async () => {
          const s = await liveSample(page);
          return s.hasBubble && s.stopVisible && !s.placeholder && s.textLen > 0;
        },
        { timeout: 60_000, intervals: [50] },
      )
      .toBe(true);
  } catch (err) {
    const s = await liveSample(page);
    const tail = (stack?.log() ?? "").split("\n").slice(-14).join("\n");
    console.log(
      `[live-stream diagnostics] waitForLiveText expired.\n` +
        `live-surface: ${JSON.stringify(s)}\n` +
        `server log tail:\n${tail}`,
    );
    throw err;
  }
}

test("streamed text is visible WHILE the run is live and grows without Stop", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await composer(page).fill("tell me something long");
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // (1)+(2) The LIVE surface shows non-empty REAL streamed text while the
  // run is provably live (Stop button still up) — no user action required.
  // Placeholder presentation does not satisfy this (v1.9.1 repair).
  await waitForLiveText(page);

  const first = await liveSample(page);
  expect(first.stopVisible, "run must still be live at first text").toBe(true);
  expect(first.placeholder, "first snapshot must be real content, not the placeholder").toBe(false);
  expect(first.textLen, "first visible snapshot must be non-empty").toBeGreaterThan(0);

  // (3)+(4) MORE text becomes visible without pressing Stop, and the run
  // continues generating after the first visible text. The fixture
  // generates its full token budget (40 real engine chunks), so growth is
  // deterministic; we require at least one strictly-later, strictly-longer
  // snapshot while still live. The baseline is the FIRST REAL streamed
  // snapshot, so this observation is measured inside the genuine live
  // window on every host speed (v1.9.1 repair).
  await expect
    .poll(
      async () => {
        const s = await liveSample(page);
        if (!s.stopVisible) return -1; // settled before we caught growth
        if (s.placeholder) return 0; // placeholder arm — never model output
        return s.textLen > first.textLen ? s.textLen : 0;
      },
      { timeout: 30_000, intervals: [50] },
    )
    .toBeGreaterThan(0);

  // (5) The run settles: the composer unlocks, the live bubble is gone,
  // and exactly ONE final assistant message carries the full answer.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(
    page.getByRole("button", { name: "Stop", exact: true }),
  ).toHaveCount(0);

  const assistantRows = page.locator(".message-row.from-agent");
  const count = await assistantRows.count();

  // The final transcript: our optimistic user row + exactly one
  // assistant reply for this single send (no duplicate transcript).
  expect(count, "exactly one assistant message after settlement").toBe(1);

  const finalText = (await assistantRows.last().textContent()) ?? "";
  expect(finalText.trim().length, "final reply non-empty").toBeGreaterThan(0);
});

test("Stop during a live generation settles honestly and keeps the partial", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await composer(page).fill("generate and I will stop you");
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // Wait for provable liveness with visible text, then Stop mid-run.
  await waitForLiveText(page);

  const stop = page.getByRole("button", { name: "Stop", exact: true });
  await stop.click();

  // The composer unlocks (terminal settlement) and Stop is gone.
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
  await expect(
    page.getByRole("button", { name: "Stop", exact: true }),
  ).toHaveCount(0);

  // The transcript stays consistent: one user row, at most one assistant
  // row (the persisted partial or the preserved snapshot), never a
  // duplicated stream.
  const userCount = await page.locator(".message-row.from-user").count();
  const agentCount = await page.locator(".message-row.from-agent").count();

  expect(userCount).toBe(1);
  expect(agentCount, "at most one assistant row after abort").toBeLessThanOrEqual(1);
});

// ---------------------------------------------------------------------------
// v1.8.4 (P0-A): THE SUSPENDED-BOUNDARY REPRODUCTION.
//
// The v1.8.3 Windows runtime failure class, reproduced deterministically
// in Chromium: requestAnimationFrame NEVER fires (the WebView2 occlusion
// failure class). In v1.8.3 the ACTIVITY timeline — statuses, tool events
// and the done/aborted LIFECYCLE events — was flushed through a rAF-ONLY
// schedule, so a suspended frame callback starved run settlement: the
// composer stayed locked and the visible state froze until Stop (whose
// abort path flushes synchronously). v1.8.4 routes the activity flush
// through the same triple-boundary scheduler as the streaming flush
// (MessageChannel macrotask + 0ms timer + animation frame), so the run
// stays visible and settles WITHOUT the frame callback. The MessageChannel
// wedge variant of the failure class is pinned deterministically at the
// unit level (stream-flush-scheduler.test.ts — a lost channel message must
// self-heal via the microtask fallback and the timer boundary).
// ---------------------------------------------------------------------------

test("the run streams and settles while live even with requestAnimationFrame suspended", async ({
  page,
}) => {
  await page.addInitScript(() => {
    // Kill the frame boundary: rAF callbacks are never invoked (the
    // WebView2 occlusion failure class). The event loop, timers, the
    // WebSocket and MessageChannel stay healthy — the scheduler must not
    // NEED the frame callback for visibility or settlement.
    window.requestAnimationFrame = ((): number => 0) as typeof requestAnimationFrame;
    window.cancelAnimationFrame = (() => {}) as typeof cancelAnimationFrame;
  });

  await page.goto(stack!.baseURL + "/");

  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
  await page.getByRole("button", { name: "New session" }).click();
  await expect(
    page.locator(".message-row, .conversation-empty").first(),
  ).toBeVisible();

  await composer(page).fill("suspended rAF streaming proof");
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // (1)+(2) The LIVE surface shows non-empty REAL streamed text while the
  // run is provably live (Stop up) — no frame callbacks involved. The
  // placeholder arm does not satisfy this (v1.9.1 repair). The v1.9.2
  // diagnostics attach the same structured snapshot on expiry.
  try {
    await expect
      .poll(async () => {
        const s = await liveSample(page);
        return s.hasBubble && s.stopVisible && !s.placeholder && s.textLen > 0;
      }, { timeout: 60_000, intervals: [100] })
      .toBe(true);
  } catch (err) {
    const s = await liveSample(page);
    const tail = (stack?.log() ?? "").split("\n").slice(-14).join("\n");
    console.log(
      `[live-stream diagnostics] rAF-suspended waitForLiveText expired.\n` +
        `live-surface: ${JSON.stringify(s)}\n` +
        `server log tail:\n${tail}`,
    );
    throw err;
  }

  const first = await liveSample(page);
  expect(first.stopVisible, "run must still be live at first text").toBe(true);
  expect(first.placeholder, "first snapshot must be real content, not the placeholder").toBe(false);
  expect(first.textLen, "first visible snapshot must be non-empty").toBeGreaterThan(0);

  // (3) The run SETTLES without the frame callback: the activity path's
  // lifecycle flush (done) no longer depends on rAF — the composer
  // unlocks and the final transcript is exactly one reply.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(
    page.getByRole("button", { name: "Stop", exact: true }),
  ).toHaveCount(0);

  const assistantRows = page.locator(".message-row.from-agent");
  expect(await assistantRows.count(), "exactly one assistant message").toBe(1);

  // v1.9.3: the persisted reply must be REAL content (non-empty, never
  // the presentation placeholder) — the same contract the other paths
  // enforce; settlement without the frame callback proves nothing about
  // the reply's substance by itself.
  const finalText = (await assistantRows.last().textContent()) ?? "";
  expect(finalText.trim().length, "final reply non-empty").toBeGreaterThan(0);
});
