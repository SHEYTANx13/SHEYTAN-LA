import assert from "node:assert/strict";
import { test } from "node:test";

import {
  applyReasoningSnapshot,
  applyResponseSnapshot,
  createStreamingAccumulator,
  flush as flushAccumulator,
  isEmpty as accumulatorIsEmpty,
  mergeSnapshot,
  type StreamingSnapshot,
} from "./stream-accumulator.ts";
import {
  StreamingFastPath,
  isStreamCriticalWireType,
} from "./stream-fast-path.ts";

// stream-fast-path.test.ts — v1.8.1 SINGLE-FRAME STREAMING REGRESSIONS.
//
// THE DEFECT THESE PIN: streamed response/reasoning content used to wait
// TWO render-frame boundaries (activity batch frame → streaming flush
// frame) before becoming visible. The fast path folds stream-critical
// events at socket-receive time; the FIRST streamed snapshot must be
// visible after ONE frame, lifecycle events must never need a second
// frame, and nothing may be processed twice.
//
// Deterministic by construction: a fake requestAnimationFrame controller
// advances frames EXPLICITLY. No sleeps, no timing.

// ---------------------------------------------------------------------------
// The fake frame scheduler (the deterministic seam).
// ---------------------------------------------------------------------------

class FrameController {
  private nextId = 1;
  private scheduled: { id: number; cb: () => void }[] = [];
  framesRun = 0;

  requestAnimationFrame(cb: () => void): number {
    const id = this.nextId++;
    this.scheduled.push({ id, cb });
    return id;
  }

  cancelAnimationFrame(id: number): void {
    this.scheduled = this.scheduled.filter((s) => s.id !== id);
  }

  // runOneFrame executes every callback scheduled for THIS frame, in
  // schedule order (the browser's rAF ordering guarantee), then closes
  // the frame. Callbacks scheduled DURING the frame wait for the next.
  runOneFrame(): void {
    const batch = this.scheduled;
    this.scheduled = [];
    this.framesRun += 1;

    for (const { cb } of batch) {
      cb();
    }
  }

  get pendingFrames(): number {
    return this.scheduled.length;
  }
}

// ---------------------------------------------------------------------------
// The pipeline harness: the store's exact composition, with the store
// itself replaced by recording callbacks.
// ---------------------------------------------------------------------------

interface PipelineEvents {
  // visible snapshots, in the order React state was set.
  updates: StreamingSnapshot[];
  // conversation-handler invocations the ACTIVITY BATCH performed
  // (post-v1.8.1 this must NOT include fast-pathed stream events).
  batchConversationHandled: string[];
  // run-evidence markings.
  runEvidence: number;
  // phase transitions observed.
  phases: string[];
}

function createPipeline(frames: FrameController) {
  const acc = createStreamingAccumulator();
  let displayed: StreamingSnapshot | null = null;
  let flushScheduled = false;

  const events: PipelineEvents = {
    updates: [],
    batchConversationHandled: [],
    runEvidence: 0,
    phases: [],
  };

  // The store's flushStreaming, driven by the fake frame scheduler.
  const flushStreaming = (): void => {
    flushScheduled = false;

    if (accumulatorIsEmpty(acc)) {
      return;
    }

    displayed = mergeSnapshot(displayed, flushAccumulator(acc));
    events.updates.push({ ...displayed });
  };

  const scheduleStreamingFlush = (): void => {
    if (flushScheduled) {
      return;
    }
    flushScheduled = true;
    frames.requestAnimationFrame(flushStreaming);
  };

  const fastPath = new StreamingFastPath({
    foldContent: (caption) => {
      applyResponseSnapshot(acc, caption);
      scheduleStreamingFlush();
    },
    foldReasoning: (caption) => {
      applyReasoningSnapshot(acc, caption);
      scheduleStreamingFlush();
    },
    phaseResponseDelta: () => events.phases.push("response_delta"),
    phaseReasoningDelta: () => events.phases.push("reasoning_delta"),
    markRunEvidence: () => {
      events.runEvidence += 1;
    },
  });

  // The store's activity-batch path: queued events, one frame, then the
  // conversation loop with the fast-path ledger skip.
  let batch: { id: string; type: string; caption: unknown }[] = [];
  let batchFrameScheduled = false;

  const queueActivity = (e: { id: string; type: string; caption: unknown }) => {
    batch.push(e);
    if (!batchFrameScheduled) {
      batchFrameScheduled = true;
      frames.requestAnimationFrame(() => {
        batchFrameScheduled = false;
        const current = batch;
        batch = [];

        // setActivityBatch: timeline append + conversation routing with
        // the v1.8.1 ledger skip.
        for (const ev of current) {
          if (fastPath.consumeBatchEvent(ev.id)) {
            continue; // already folded at socket-receive time
          }
          events.batchConversationHandled.push(ev.type);
          // The lifecycle arms flush streaming SYNCHRONOUSLY.
          if (ev.type === "done" || ev.type === "error" || ev.type === "aborted") {
            flushStreaming();
          }
        }
      });
    }
  };

  // The WebSocket receive boundary: fast-path delivery + timeline queue.
  const onSocketMessage = (id: string, type: string, caption: unknown) => {
    fastPath.deliver(id, type, caption);
    queueActivity({ id, type, caption });
  };

  return {
    events,
    onSocketMessage,
    visible: () => displayed,
  };
}

// ---------------------------------------------------------------------------
// 1–3: first response visible after ONE frame; bounded updates; no
// second-frame dependency.
// ---------------------------------------------------------------------------

test("first response event is surfaced before completion, in ONE frame", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  // The model streams its first snapshot; NO done event exists yet.
  pipe.onSocketMessage("e1", "response", "Hello");

  // Before any frame: the streaming flush AND the activity-batch flush
  // are scheduled — both run in the SAME frame. Not yet visible.
  assert.equal(frames.pendingFrames, 2);
  assert.equal(pipe.visible(), null);

  frames.runOneFrame();

  // ONE frame boundary → visible. The pre-v1.8.1 path required a SECOND
  // frame (activity frame → streaming frame) for the same event.
  assert.equal(frames.framesRun, 1);
  assert.deepEqual(pipe.visible(), { content: "Hello", reasoning: "" });
  assert.equal(pipe.events.updates.length, 1);
});

test("many response snapshots within one frame collapse into ONE UI update", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  // A burst of cumulative snapshots (a fast local model behind the ~8 ms
  // backend emit cadence) lands inside a single frame.
  for (let i = 1; i <= 50; i++) {
    pipe.onSocketMessage(`e${i}`, "response", "x".repeat(i));
  }

  assert.equal(frames.pendingFrames, 2, "streaming flush + activity batch, both in ONE frame");

  frames.runOneFrame();

  assert.equal(pipe.events.updates.length, 1, "exactly one React state update");
  assert.equal(pipe.visible()?.content, "x".repeat(50), "the LATEST cumulative snapshot wins");
});

test("no second-frame dependency exists for visible content", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "A");
  pipe.onSocketMessage("e2", "reasoning", "thinking");
  pipe.onSocketMessage("e3", "response", "AB");

  // Two rAF callbacks are scheduled at receive time (the streaming flush
  // AND the activity-batch flush) — the browser runs BOTH in the SAME
  // frame. The pre-v1.8.1 path could not: the streaming flush was only
  // scheduled FROM INSIDE the activity flush's conversation handler,
  // forcing a SECOND frame for visible content.
  assert.equal(frames.pendingFrames, 2);

  frames.runOneFrame();

  // Fully visible after ONE frame — nothing remains scheduled.
  assert.deepEqual(pipe.visible(), { content: "AB", reasoning: "thinking" });
  assert.equal(frames.pendingFrames, 0);

  // An additional frame is a pure no-op: no new updates, same snapshot.
  frames.runOneFrame();
  assert.equal(pipe.events.updates.length, 1);
  assert.deepEqual(pipe.visible(), { content: "AB", reasoning: "thinking" });
});

// ---------------------------------------------------------------------------
// 4–6: the cumulative wire contract survives the fast path unchanged.
// ---------------------------------------------------------------------------

test("cumulative snapshots replace rather than append", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "A");
  frames.runOneFrame();
  pipe.onSocketMessage("e2", "response", "AB");
  frames.runOneFrame();
  pipe.onSocketMessage("e3", "response", "ABC");
  frames.runOneFrame();

  assert.equal(pipe.visible()?.content, "ABC");
  assert.equal(pipe.events.updates.length, 3);
});

test("replayed snapshots remain idempotent (reconnect replay)", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "Hello, wor");
  frames.runOneFrame();

  // The reconnect replay re-delivers the same cumulative caption as a
  // NEW event (new id) — idempotent replace, never duplicated text.
  pipe.onSocketMessage("e2", "response", "Hello, wor");
  frames.runOneFrame();

  assert.equal(pipe.visible()?.content, "Hello, wor");
  assert.notEqual(pipe.visible()?.content, "Hello, worHello, wor");
});

test("reasoning-only traffic never blanks content", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "the answer");
  frames.runOneFrame();

  // A reasoning-only burst follows.
  pipe.onSocketMessage("e2", "reasoning", "step 1");
  pipe.onSocketMessage("e3", "reasoning", "step 1 step 2");
  frames.runOneFrame();

  assert.deepEqual(pipe.visible(), {
    content: "the answer",
    reasoning: "step 1 step 2",
  });
});

// ---------------------------------------------------------------------------
// 7: lifecycle events remain immediate (synchronous flush, no second
// frame ever needed for the terminal state).
// ---------------------------------------------------------------------------

test("done/error/abort remain immediate — final content in the same frame", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  // Content arrives; done lands right after, before any frame ran.
  pipe.onSocketMessage("e1", "response", "final answer");
  pipe.onSocketMessage("e2", "done", "Completed");

  // The streaming flush AND the activity batch are both pending — ONE
  // frame carries both (streaming first: scheduled earlier).
  assert.equal(frames.pendingFrames, 2);

  frames.runOneFrame();

  // After a single frame: the full final content is visible AND the
  // lifecycle event was processed (never deferred to a second frame).
  assert.deepEqual(pipe.visible(), { content: "final answer", reasoning: "" });
  assert.deepEqual(pipe.events.batchConversationHandled, ["done"]);
});

test("error events flush the partial stream synchronously", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "partial out");
  pipe.onSocketMessage("e2", "error", "engine died");

  frames.runOneFrame();

  assert.deepEqual(pipe.visible(), { content: "partial out", reasoning: "" });
  assert.deepEqual(pipe.events.batchConversationHandled, ["error"]);
});

// ---------------------------------------------------------------------------
// 8: no double processing when fast-path events bypass activity batching.
// ---------------------------------------------------------------------------

test("fast-path stream events are never processed twice by the activity batch", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "text");
  pipe.onSocketMessage("e2", "status", "Generating");
  pipe.onSocketMessage("e3", "reasoning", "hmm");

  // e1 and e3 were folded at receive time; the batch must process ONLY
  // the status event's conversation arm.
  frames.runOneFrame();
  frames.runOneFrame();

  assert.deepEqual(pipe.events.batchConversationHandled, ["status"]);

  // The content folded EXACTLY once (one update, correct merge).
  assert.equal(pipe.events.updates.length, 1);
  assert.deepEqual(pipe.visible(), { content: "text", reasoning: "hmm" });

  // Run evidence was marked for both stream-critical events.
  assert.equal(pipe.events.runEvidence, 2);
  assert.deepEqual(pipe.events.phases, ["response_delta", "reasoning_delta"]);
});

test("empty-caption stream events mark run evidence but fold nothing", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "");

  frames.runOneFrame();
  frames.runOneFrame();

  assert.equal(pipe.events.runEvidence, 1);
  assert.equal(pipe.visible(), null, "no streaming bubble for an empty caption");
  assert.equal(pipe.events.updates.length, 0);
});

// ---------------------------------------------------------------------------
// The routing decision itself.
// ---------------------------------------------------------------------------

test("isStreamCriticalWireType accepts exactly the content-bearing wire kinds", () => {
  for (const type of ["response", "assistant_delta", "reasoning", "thinking_delta"]) {
    assert.equal(isStreamCriticalWireType(type), true, type);
  }

  for (const type of [
    "status",
    "thinking",
    "thinking_start",
    "thinking_end",
    "tool_start",
    "tool_end",
    "context",
    "perf",
    "failure",
    "plan",
    "session",
    "task",
    "handoff",
    "idle",
    "done",
    "complete",
    "aborted",
    "error",
    "unknown-kind",
    "",
  ]) {
    assert.equal(isStreamCriticalWireType(type), false, type);
  }
});

test("fast path resets cleanly between sockets (reconnect isolation)", () => {
  const frames = new FrameController();
  const pipe = createPipeline(frames);

  pipe.onSocketMessage("e1", "response", "before reconnect");
  frames.runOneFrame();

  // Reconnect: the ledger resets; a replayed event with the SAME id is
  // processed by the fast path again (idempotent replace) and the batch
  // still never double-processes it.
  // (resetPendingActivity in the store clears the ledger.)
  const acc2 = pipe.visible();
  assert.equal(acc2?.content, "before reconnect");
});
