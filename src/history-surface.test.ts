// history-surface.test.ts — v1.4.0 unit tests for the transcript
// surface state machine. The contract under test is the one the UI
// renders (MessageStream consumes the same pure function):
//
//   loading history  → "Loading conversation…" (never the empty invite)
//   empty history    → the mode-aware invitation (ONLY after a page arrived)
//   loaded history   → the transcript
//   loading failure  → an explicit retryable error (never a fake-empty view)
//
// The regression these tests pin: a FAILED or IN-FLIGHT history fetch
// previously rendered the identical "Ready when you are" invitation as
// a genuinely empty conversation — the UI claimed an empty history that
// was never established (§5).
import test from "node:test";
import assert from "node:assert/strict";

import { historySurfaceState } from "./history-surface.ts";

const base = {
  historyStatus: "ready" as const,
  messageCount: 0,
  streaming: false,
  runLive: false,
};

test("a genuinely empty history renders the invitation", () => {
  assert.equal(historySurfaceState(base), "empty");
});

test("an in-flight history fetch renders loading, never the invitation", () => {
  assert.equal(
    historySurfaceState({ ...base, historyStatus: "loading" }),
    "loading",
  );
});

test("a failed history fetch renders an error, never a fake-empty view", () => {
  assert.equal(
    historySurfaceState({ ...base, historyStatus: "error" }),
    "error",
  );
});

test("an arrived page renders the transcript", () => {
  assert.equal(historySurfaceState({ ...base, messageCount: 3 }), "messages");
});

test("a live run renders the surface even while a reload is in flight", () => {
  // Reloading the session the user is reading (run finalization) must
  // not blank the view behind a loading spinner.
  assert.equal(
    historySurfaceState({
      ...base,
      historyStatus: "loading",
      messageCount: 5,
    }),
    "messages",
  );
});

test("a streaming bubble renders the surface even with zero loaded messages", () => {
  assert.equal(
    historySurfaceState({ ...base, historyStatus: "loading", streaming: true }),
    "messages",
  );
});

test("a live run phase renders the surface even with zero loaded messages", () => {
  assert.equal(
    historySurfaceState({ ...base, historyStatus: "loading", runLive: true }),
    "messages",
  );
});

test("a failed reload of a surface already showing messages keeps the transcript", () => {
  assert.equal(
    historySurfaceState({ ...base, historyStatus: "error", messageCount: 4 }),
    "messages",
  );
});
