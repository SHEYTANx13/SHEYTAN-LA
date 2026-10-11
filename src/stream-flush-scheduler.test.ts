// stream-flush-scheduler.test.ts — deterministic proofs for the v1.8.4
// triple-boundary, self-healing streaming flush (pure module, no browser,
// no sleeps).
//
// Pinned contracts:
//   - schedule() arms exactly ONE flush across the task, timer and frame
//     boundaries (one latch);
//   - the first boundary to run flushes; the others are no-ops;
//   - folds arriving while a flush is pending coalesce (schedule() is
//     idempotent while pending);
//   - cancel() drops the pending flush and disarms the frame callback;
//   - the task boundary ALONE is sufficient — the frame callback is
//     never required (the WebView2 rAF-loss class this module repairs);
//   - v1.8.4: the TIMER boundary alone is sufficient — a lost MessageChannel
//     delivery can never wedge the latch (the v1.8.3 runtime failure class);
//   - v1.8.4: MessageChannelTaskController is RECOVERABLE — a post that
//     arrives while a message is still undelivered delivers the latest
//     callback via microtask (latest-wins, never an unbounded chain);
//   - v1.8.4: a wedged channel degrades to microtask delivery for every
//     subsequent window instead of killing the flush path forever.

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  StreamFlushScheduler,
  MessageChannelTaskController,
  TimeoutTaskController,
  type FlushFrameController,
  type FlushTaskController,
} from "./stream-flush-scheduler.ts";

// FakeTaskController collects posted callbacks; the test drains them
// explicitly — deterministic, no real event loop involved.
class FakeTaskController implements FlushTaskController {
  posted: (() => void)[] = [];

  post(callback: () => void): void {
    this.posted.push(callback);
  }

  runFirst(): void {
    const cb = this.posted.shift();
    if (cb) cb();
  }
}

// LostTaskController simulates a task source whose messages are never
// delivered (the lost-MessageChannel failure class): posts are recorded,
// but nothing ever fires them.
class LostTaskController implements FlushTaskController {
  posted: (() => void)[] = [];

  post(callback: () => void): void {
    this.posted.push(callback);
  }
}

// FakeFrameController records the requested callback + cancel handle.
class FakeFrameController implements FlushFrameController {
  callback: (() => void) | null = null;
  cancelled = false;

  request(callback: () => void): () => void {
    this.callback = callback;
    return () => {
      this.cancelled = true;
      this.callback = null;
    };
  }

  run(): void {
    const cb = this.callback;
    this.callback = null;
    if (cb) cb();
  }
}

function makeScheduler(flushes: number[] = []) {
  const task = new FakeTaskController();
  const timer = new FakeTaskController();
  const frame = new FakeFrameController();
  let count = 0;
  const scheduler = new StreamFlushScheduler(task, timer, frame, () => {
    count += 1;
    flushes.push(count);
  });
  return { task, timer, frame, scheduler, flushes };
}

test("schedule arms the task, timer AND frame boundaries once", () => {
  const { task, timer, frame, scheduler } = makeScheduler();

  scheduler.schedule();

  assert.equal(task.posted.length, 1, "one task posted");
  assert.equal(timer.posted.length, 1, "one timer task posted");
  assert.ok(frame.callback, "frame callback armed");
  assert.ok(scheduler.isPending());
});

test("schedule is idempotent while a flush is pending (coalescing latch)", () => {
  const { task, timer, frame, scheduler } = makeScheduler();

  scheduler.schedule();
  scheduler.schedule();
  scheduler.schedule();

  assert.equal(task.posted.length, 1, "no extra tasks while pending");
  assert.equal(timer.posted.length, 1, "no extra timer tasks while pending");
  assert.ok(frame.callback, "single frame callback");
});

test("the task boundary flushes and disarms the frame callback", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst();

  assert.deepEqual(flushes, [1], "flushed exactly once");
  assert.ok(frame.cancelled, "frame callback cancelled");
  assert.ok(!scheduler.isPending());
});

test("the frame boundary alone flushes (task not run yet)", () => {
  const { task, timer, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  frame.run();

  assert.deepEqual(flushes, [1], "flushed exactly once");
  assert.ok(!scheduler.isPending());

  // Draining the task afterwards must NOT flush again.
  task.runFirst();
  timer.runFirst();
  assert.deepEqual(flushes, [1], "second and third boundaries are no-ops");
});

test("the timer boundary alone flushes when the task AND frame never fire", () => {
  // THE v1.8.3 RUNTIME FAILURE CLASS: the MessageChannel message is lost
  // (task callback never delivered) and the compositor suspended rAF.
  // The 0ms timer — the primitive class proven alive in that runtime —
  // must be able to flush on its own. The v1.8.2 dual-boundary design
  // wedged forever here; the v1.8.4 latch must close the window.
  const task = new LostTaskController();
  const timer = new FakeTaskController();
  const frame = new FakeFrameController();
  const flushes: number[] = [];

  const scheduler = new StreamFlushScheduler(task, timer, frame, () => {
    flushes.push(1);
  });

  scheduler.schedule();
  assert.deepEqual(flushes, [], "nothing delivered yet");

  timer.runFirst();

  assert.deepEqual(flushes, [1], "the timer boundary flushed");
  assert.ok(!scheduler.isPending(), "the latch closed");

  // A late delivery of the lost message must be a harmless no-op.
  frame.run();
  assert.deepEqual(flushes, [1]);
});

test("both boundaries running flush exactly once", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst();
  frame.run();

  assert.deepEqual(flushes, [1]);
});

test("cancel drops the pending flush and cancels the frame callback", () => {
  const { task, timer, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  scheduler.cancel();

  assert.ok(!scheduler.isPending());
  assert.ok(frame.cancelled, "frame disarmed");

  task.runFirst();
  timer.runFirst();
  frame.run();

  assert.deepEqual(flushes, [], "no flush after cancel");
});

test("folds across consecutive flush windows each produce one flush", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst(); // window 1 flushed

  scheduler.schedule();
  frame.run(); // window 2 flushed via the frame boundary

  scheduler.schedule();
  task.runFirst(); // window 3 flushed via the task boundary

  assert.deepEqual(flushes, [1, 2, 3]);
});

test("run() without a pending schedule is a no-op", () => {
  const { scheduler, flushes } = makeScheduler();

  scheduler.run();
  scheduler.run();

  assert.deepEqual(flushes, []);
});

// ---------------------------------------------------------------------------
// v1.8.4: the MessageChannel task controller's recovery contract.
//
// These tests inject DETERMINISTIC fake channels instead of Node's real
// MessageChannel: a real port implicitly re-refs the host event loop when
// port1.onmessage is assigned (the same host hazard the store-level test
// harness documents) — a fake keeps the proofs exact and the runner fast.
// ---------------------------------------------------------------------------

type FakePort1 = { onmessage: (() => void) | null };
type FakePort2 = { postMessage: () => void };

// installFakeChannel swaps a deterministic MessageChannel global in and
// returns the restore function. mode "healthy": port2.postMessage schedules
// port1.onmessage on a microtask (real channel semantics, no host loop).
// mode "wedged": messages are LOST — onmessage never fires (the exact
// v1.8.3 wedge).
function installFakeChannel(mode: "healthy" | "wedged"): () => void {
  const host = globalThis as unknown as {
    MessageChannel?: unknown;
  };

  const original = host.MessageChannel;

  host.MessageChannel = function () {
    const port1: FakePort1 = { onmessage: null };
    const port2: FakePort2 = {
      postMessage: () => {
        if (mode === "wedged") {
          return; // the message is lost — delivery never happens
        }
        queueMicrotask(() => port1.onmessage?.());
      },
    };

    return { port1, port2 };
  } as unknown as typeof MessageChannel;

  return () => {
    host.MessageChannel = original;
  };
}

test("a healthy channel is REUSED for every flush window (no one-shot)", async () => {
  const restore = installFakeChannel("healthy");

  try {
    const controller = new MessageChannelTaskController();
    const delivered: string[] = [];

    controller.post(() => delivered.push("window-1"));
    await Promise.resolve(); // the fake channel delivers on a microtask

    controller.post(() => delivered.push("window-2"));
    await Promise.resolve();

    controller.post(() => delivered.push("window-3"));
    await Promise.resolve();

    assert.deepEqual(delivered, ["window-1", "window-2", "window-3"]);
  } finally {
    restore();
  }
});

test("a LOST channel message cannot wedge the controller (self-heal)", async () => {
  // THE v1.8.3 RUNTIME FAILURE CLASS, reproduced deterministically: the
  // first message is posted and never delivers. The v1.8.2 controller
  // stayed wedged forever (queued armed, channel non-null, no reposts,
  // fallback unreachable) — the v1.8.4 controller must deliver every
  // subsequent window through the microtask fallback.
  const restore = installFakeChannel("wedged");

  try {
    const controller = new MessageChannelTaskController();
    const delivered: string[] = [];

    // Window 1: posted into the void (lost message — no delivery).
    controller.post(() => delivered.push("window-1"));
    await Promise.resolve();
    assert.deepEqual(delivered, [], "the wedged channel delivered nothing");

    // Window 2: the post-while-in-flight fallback must deliver NOW.
    controller.post(() => delivered.push("window-2"));
    await Promise.resolve();
    assert.deepEqual(
      delivered,
      ["window-2"],
      "the fallback delivered the latest callback",
    );

    // Window 3: the controller keeps self-healing — never wedges.
    controller.post(() => delivered.push("window-3"));
    await Promise.resolve();
    assert.deepEqual(delivered, ["window-2", "window-3"]);
  } finally {
    restore();
  }
});

test("latest-callback-wins: a superseded callback never runs twice", async () => {
  const restore = installFakeChannel("healthy");

  try {
    const controller = new MessageChannelTaskController();
    let calls = 0;

    // Two posts inside one window coalesce: only the latest callback is
    // kept, and the delivered one runs exactly once.
    controller.post(() => {
      calls += 1;
    });
    controller.post(() => {
      calls += 10;
    });

    await Promise.resolve();
    await Promise.resolve();

    assert.equal(calls, 10, "exactly one delivery, the latest callback");
  } finally {
    restore();
  }
});

test("TimeoutTaskController delivers on a macrotask", async () => {
  const controller = new TimeoutTaskController();
  let delivered = false;

  controller.post(() => {
    delivered = true;
  });

  assert.ok(!delivered, "not delivered synchronously");

  await new Promise<void>((resolve) => setTimeout(resolve, 0));

  assert.ok(delivered, "delivered on the timer macrotask");
});
