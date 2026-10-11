import assert from "node:assert/strict";
import { test } from "node:test";

import {
  applyReasoningSnapshot,
  applyResponseSnapshot,
  createStreamingAccumulator,
  flush,
  isEmpty,
  mergeSnapshot,
} from "./stream-accumulator.ts";

// v1.2.2 regression tests — the streaming wire contract.
//
// THE BUG THESE PIN: internal/agent/orchestrator.go's emitProgress sends
// CUMULATIVE captions (each `response`/`reasoning` activity event carries
// the FULL text so far). The pre-v1.2.2 store appended every caption,
// so a stream of snapshots ["A", "AB", "ABC"] rendered as "AABABABC…".
// The accumulator must REPLACE per stream, making replays idempotent.

test("cumulative snapshots replace, never append", () => {
  const acc = createStreamingAccumulator();

  applyResponseSnapshot(acc, "A");
  applyResponseSnapshot(acc, "AB");
  applyResponseSnapshot(acc, "ABC");

  const update = flush(acc);

  assert.deepEqual(update, { content: "ABC", reasoning: null });
});

test("a shorter caption (new agent iteration) also replaces", () => {
  const acc = createStreamingAccumulator();

  applyResponseSnapshot(acc, "long partial answer from iteration one");
  applyResponseSnapshot(acc, "New");

  const update = flush(acc);

  // The server is authoritative: iteration two's builder was reset.
  assert.equal(update.content, "New");
});

test("replayed duplicate snapshots after reconnect are idempotent", () => {
  // Simulate a reconnect that re-delivers the last cumulative snapshot.
  let displayed = { content: "", reasoning: "" };

  const first = createStreamingAccumulator();
  applyResponseSnapshot(first, "Hello, wor");
  displayed = mergeSnapshot(displayed, flush(first));

  const replay = createStreamingAccumulator();
  applyResponseSnapshot(replay, "Hello, wor"); // same cumulative text again
  displayed = mergeSnapshot(displayed, flush(replay));

  assert.equal(displayed.content, "Hello, wor");
});

test("reasoning and content update independently — a reasoning-only flush keeps content", () => {
  const acc = createStreamingAccumulator();

  applyResponseSnapshot(acc, "answer text");
  let displayed = mergeSnapshot(null, flush(acc));

  applyReasoningSnapshot(acc, "thinking about the answer");
  displayed = mergeSnapshot(displayed, flush(acc));

  assert.equal(displayed.content, "answer text");
  assert.equal(displayed.reasoning, "thinking about the answer");
});

test("reasoning snapshots replace cumulatively too", () => {
  const acc = createStreamingAccumulator();

  applyReasoningSnapshot(acc, "step 1");
  applyReasoningSnapshot(acc, "step 1 step 2");
  applyReasoningSnapshot(acc, "step 1 step 2 step 3");

  const update = flush(acc);

  assert.equal(update.reasoning, "step 1 step 2 step 3");
  assert.equal(update.content, null);
});

test("empty and invalid captions are ignored", () => {
  const acc = createStreamingAccumulator();

  applyResponseSnapshot(acc, "");
  applyReasoningSnapshot(acc, "");
  // @ts-expect-error — runtime safety against malformed backend frames
  applyResponseSnapshot(acc, 42);

  assert.equal(isEmpty(acc), true);

  const update = flush(acc);
  assert.deepEqual(update, { content: null, reasoning: null });
});

test("flush drains the accumulator (second flush is a no-op)", () => {
  const acc = createStreamingAccumulator();

  applyResponseSnapshot(acc, "text");
  assert.equal(isEmpty(acc), false);

  flush(acc);
  assert.equal(isEmpty(acc), true);

  const second = flush(acc);
  assert.deepEqual(second, { content: null, reasoning: null });
});

test("merge keeps previous values for streams that did not change", () => {
  const current = { content: "kept answer", reasoning: "kept reasoning" };

  const merged = mergeSnapshot(current, {
    content: null,
    reasoning: "new reasoning",
  });

  assert.equal(merged.content, "kept answer");
  assert.equal(merged.reasoning, "new reasoning");
});

test("merge onto null starts from empty strings", () => {
  const merged = mergeSnapshot(null, { content: "first", reasoning: null });

  assert.deepEqual(merged, { content: "first", reasoning: "" });
});
