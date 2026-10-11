// stream-flush-scheduler.ts — v1.8.4 TRIPLE-BOUNDARY, SELF-HEALING STREAMING
// FLUSH (evolution of the v1.8.2 dual-boundary design).
//
// THE DEFECT CLASS THIS MODULE REPAIRS (P0: streamed text visible only
// after Stop): the store scheduled the streaming flush EXCLUSIVELY via
// requestAnimationFrame (pre-v1.8.2). In the user's WebView2 runtime the
// compositor's frame callbacks can be throttled or suspended (occlusion
// heuristics, DWM/GPU states) while the JS event loop, the WebSocket and
// React's own scheduler keep running. The v1.8.2 repair armed a SECOND
// boundary — a MessageChannel macrotask. v1.8.4 closes the remaining hole
// the live v1.8.3 runtime evidence exposed:
//
//   - the v1.8.2 MessageChannelTaskController was ONE-SHOT: its port
//     handler nulled the channel after the first delivery, and a post()
//     arriving while a message was still in flight CHAINED onto the armed
//     callback WITHOUT posting a new message. If that one in-flight
//     message is ever lost or delayed indefinitely (the exact renderer
//     state the failing runtime exhibits), the armed callback never
//     delivers, the channel never resets, the microtask fallback never
//     engages, and the scheduler latch stays pending FOREVER — every
//     later schedule() is a no-op, streamed text accumulates in the
//     accumulator, and only the Stop path's synchronous flush reveals it.
//   - the ACTIVITY timeline (status/tool/done/error events) is flushed
//     through the same class of boundary and had the same exposure.
//
// THE v1.8.4 REPAIR — three INDEPENDENT scheduling boundaries behind ONE
// coalescing latch, and a task controller that can never wedge:
//
//   1. a MessageChannel macrotask (the same primitive React's scheduler
//      uses; delivered the moment the current turn ends);
//   2. a 0ms timeout task (an INDEPENDENT task source — the one class of
//      primitive proven alive in the failing runtime, whose elapsed clock
//      kept ticking while rAF and MessageChannel delivery failed);
//   3. an animation frame (the frame-aligned cadence healthy renderers
//      already have).
//
//   Whichever runs first performs the flush; the others are harmless
//   no-ops. The MessageChannel controller is RECOVERABLE by construction:
//   latest-callback-wins, an explicit in-flight handshake, and a microtask
//   delivery whenever a post arrives while a message is still undelivered
//   (a chain-free, bounded replacement for the old chain) — so a lost
//   message degrades the controller to microtask delivery instead of
//   killing it. Properties:
//
//   - exactly ONE coalescing latch: folds that arrive before any boundary
//     runs coalesce into one flush (no per-token renders);
//   - visibility no longer depends on ANY single scheduling primitive;
//   - no sleeps, no polling, no retry storms — every boundary is a
//     platform scheduling primitive armed at most once per flush window;
//   - the flush itself is idempotent and drains-then-clears.
//
// The scheduling DECISION is a pure class with injectable controllers so
// it is unit-testable without a browser (stream-flush-scheduler.test.ts),
// mirroring the stream-fast-path.ts approach.

export interface FlushTaskController {
  // post schedules callback as an event-loop task (macrotask).
  post(callback: () => void): void;
}

export interface FlushFrameController {
  // request schedules callback on the next animation frame; returns a
  // cancel handle.
  request(callback: () => void): () => void;
}

export class StreamFlushScheduler {
  private pending = false;

  private readonly task: FlushTaskController;
  private readonly timer: FlushTaskController;
  private readonly frame: FlushFrameController;
  private readonly flush: () => void;

  private cancelFrame: (() => void) | null = null;

  constructor(
    task: FlushTaskController,
    timer: FlushTaskController,
    frame: FlushFrameController,
    flush: () => void,
  ) {
    this.task = task;
    this.timer = timer;
    this.frame = frame;
    this.flush = flush;
  }

  // schedule arms all three boundaries exactly once while a flush is
  // pending. Idempotent: folds that arrive while a flush is pending only
  // coalesce into the pending flush.
  schedule(): void {
    if (this.pending) {
      return;
    }

    this.pending = true;

    // The task boundary fires as soon as the current JS turn ends — in
    // every renderer state where JavaScript runs at all.
    this.task.post(() => this.run());

    // The timer boundary is the INDEPENDENT task source: if the
    // MessageChannel message is lost or delayed (the v1.8.3 runtime
    // failure class), the timeout still delivers the flush. It is armed
    // at most once per flush window — never a polling loop.
    this.timer.post(() => this.run());

    // The frame boundary keeps the frame-aligned cadence in healthy
    // renderers. The task usually wins; the frame is the natural cadence
    // for folds that arrive inside the same frame window.
    this.cancelFrame = this.frame.request(() => this.run());
  }

  // run performs the pending flush exactly once. All boundaries invoke
  // it; the latch guarantees one execution per schedule().
  run(): void {
    if (!this.pending) {
      return;
    }

    this.pending = false;
    this.disposeFrame();
    this.flush();
  }

  // cancel drops a pending flush (socket reset / session switch /
  // accumulator reset — mirrors resetPendingStreaming). Already-armed
  // task/timer callbacks become latch no-ops.
  cancel(): void {
    this.pending = false;
    this.disposeFrame();
  }

  // isPending reports whether a flush is armed (test/diagnostic seam).
  isPending(): boolean {
    return this.pending;
  }

  private disposeFrame(): void {
    if (this.cancelFrame !== null) {
      this.cancelFrame();
      this.cancelFrame = null;
    }
  }
}

// Browser controllers — the real bindings the store installs.

// MessageChannelTaskController is a RECOVERABLE MessageChannel macrotask
// source (v1.8.4):
//
//   - the channel is REUSED for the controller's lifetime (the v1.8.2
//     one-shot reset is gone);
//   - latest-callback-wins: the scheduler's latch makes every armed
//     callback from an earlier window a no-op, so replacing (never
//     chaining) the queued callback is always safe;
//   - a post arriving while a message is still undelivered falls back to
//     a microtask delivery of the latest callback. In a healthy renderer
//     the message delivers first and the microtask is a no-op; when a
//     message is lost or delayed past a whole flush window, this is the
//     deterministic self-heal — the controller degrades to microtask
//     delivery (which always runs) instead of wedging forever;
//   - when MessageChannel is unavailable entirely, every post is a
//     microtask boundary.
export class MessageChannelTaskController implements FlushTaskController {
  private channel: MessageChannel | null = null;
  private queued: (() => void) | null = null;
  private inFlight = false;

  constructor() {
    if (typeof MessageChannel === "function") {
      try {
        const ch = new MessageChannel();

        ch.port1.onmessage = () => {
          this.deliver();
        };

        this.channel = ch;
      } catch {
        this.channel = null;
      }
    }
  }

  // deliver runs the latest queued callback exactly once and clears the
  // in-flight handshake, regardless of WHICH delivery path won.
  private deliver(): void {
    this.inFlight = false;

    const cb = this.queued;
    this.queued = null;

    if (cb) {
      cb();
    }
  }

  post(callback: () => void): void {
    // Latest wins. The scheduler's one latch means any previously armed
    // callback can only ever be a same-flush no-op.
    this.queued = callback;

    if (this.channel) {
      if (!this.inFlight) {
        this.inFlight = true;

        try {
          this.channel.port2.postMessage(null);
          return;
        } catch {
          // A dead/failed port must not wedge the controller — fall
          // through to the microtask delivery below (and stop trusting
          // the channel).
          this.channel = null;
        }
      } else {
        // A message from a PREVIOUS flush window is still undelivered:
        // this channel has missed at least one whole window, which is
        // exactly the v1.8.3 wedge signature. Degrade permanently to
        // microtask delivery (the path that always runs) instead of
        // re-arming a channel that demonstrably loses messages — and
        // never leave a post undelivered.
        this.channel = null;
        this.inFlight = false;
      }
    }

    // No channel (unavailable, failed, or degraded), or a first post in a
    // channel-less environment: deliver the latest callback on a
    // microtask, which runs the moment the current task ends in every
    // renderer state where JavaScript runs at all.
    if (typeof queueMicrotask === "function") {
      queueMicrotask(() => this.deliver());
      return;
    }

    this.deliver();
  }
}

// TimeoutTaskController is the independent timer boundary: a 0ms timeout
// is a macrotask from a DIFFERENT task source than MessageChannel — the
// one primitive class proven alive in the failing runtime (the elapsed
// clock kept ticking). Armed at most once per flush window by the
// scheduler's latch; never a polling loop.
export class TimeoutTaskController implements FlushTaskController {
  post(callback: () => void): void {
    if (typeof setTimeout === "function") {
      setTimeout(callback, 0);
      return;
    }

    callback();
  }
}

export class AnimationFrameController implements FlushFrameController {
  request(callback: () => void): () => void {
    if (typeof requestAnimationFrame === "function") {
      const handle = requestAnimationFrame(callback);
      return () => cancelAnimationFrame(handle);
    }

    // No frame callbacks in this environment: run nothing (the task
    // boundaries alone own the flush).
    return () => {};
  }
}
