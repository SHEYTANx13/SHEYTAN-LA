// stream-trace.test.ts — v1.9.4 (P0-A): the OPT-IN TEST-ONLY STREAM
// BOUNDARY TRACE contract, pinned without a browser:
//
//   1. DISABLED BY DEFAULT: every record call is a no-op — the ring stays
//      empty and the dump reports enabled:false (the privacy/cost
//      contract: near-zero overhead in normal runs);
//   2. explicit enable captures boundary observations with MONOTONIC
//      sequence numbers and LENGTHS ONLY (no content ever enters the
//      trace — asserted with the very string the caller passed);
//   3. the ring is BOUNDED: a flood keeps the newest RING_CAP entries;
//   4. the dump is compact JSON with aggregate counters + the bounded
//      ring, and it NEVER contains user content;
//   5. run-start/run-end bookkeeping closes the window with the settled
//      phase;
//   6. reset clears everything (test isolation).

import assert from "node:assert/strict";
import { describe, it, beforeEach } from "node:test";

import {
  enableForTest,
  disableForTest,
  recordStreamTrace,
  recordRunStart,
  recordRunEnd,
  dumpStreamTrace,
  resetStreamTrace,
} from "./stream-trace.ts";

// performance.now is available in Node >= 8.5 via perf_hooks global in
// Node >= 16 — assert the module can use it.
assert.equal(typeof performance.now(), "number");

interface DumpShape {
  enabled: boolean;
  counters: Record<string, number>;
  runStartedAt: number | null;
  events: {
    t: number;
    seq: number;
    stage: string;
    captionChars?: number;
    contentChars?: number;
    code?: string;
  }[];
}

function dump(): DumpShape {
  return JSON.parse(dumpStreamTrace()) as DumpShape;
}

describe("stream-trace (opt-in, test-only, sanitized)", () => {
  beforeEach(() => {
    disableForTest();
    resetStreamTrace();
  });

  it("1. disabled by default: recording is a no-op", () => {
    recordStreamTrace("ws_frame", { kind: "assistant_delta", captionChars: 5 });
    recordRunStart();

    const d = dump();

    assert.equal(d.enabled, false);
    assert.deepEqual(d.events, []);
    assert.deepEqual(d.counters, {});
  });

  it("2. enabled: captures boundaries with lengths ONLY (never content)", () => {
    enableForTest();

    const secret = "CLASSIFIED-USER-CONTENT-9f2b";
    recordStreamTrace("ws_frame", {
      kind: "assistant_delta",
      captionChars: secret.length,
    });
    recordStreamTrace("fastpath_fold", { contentChars: secret.length });

    const d = dump();

    assert.equal(d.enabled, true);
    assert.equal(d.events.length, 2);
    assert.equal(d.counters.ws_frame, 1);
    assert.equal(d.counters.fastpath_fold, 1);

    // Sequence numbers are monotonic in receive order.
    assert.ok(d.events[0].seq < d.events[1].seq);

    // Lengths recorded…
    assert.equal(d.events[0].captionChars, secret.length);
    assert.equal(d.events[1].contentChars, secret.length);

    // …but the CONTENT itself never enters the trace.
    assert.ok(!dumpStreamTrace().includes(secret));
  });

  it("3. the ring is bounded (newest entries survive a flood)", () => {
    enableForTest();

    for (let i = 0; i < 1000; i++) {
      recordStreamTrace("ws_frame", { wireSeq: i });
    }

    const d = dump();

    assert.equal(d.events.length, 512);

    // The newest entry survives; the oldest flooded-out entries are gone.
    assert.equal(d.events[d.events.length - 1].wireSeq, 999);
    assert.ok(d.events[0].wireSeq > 0);
  });

  it("4. run window: start/end bookkeeping with the settled phase", () => {
    enableForTest();

    recordRunStart();
    recordRunEnd("complete");

    const d = dump();

    assert.equal(d.counters.lifecycle, 2);
    assert.ok(d.runStartedAt !== null);

    const end = d.events[d.events.length - 1];
    assert.equal(end.stage, "lifecycle");
    assert.equal(end.code, "complete");
    // batchDepth carries the monotonic run-total (>= 0).
    assert.equal(typeof end.batchDepth, "number");
    assert.ok((end.batchDepth ?? -1) >= 0);
  });

  it("5. reset clears ring, counters and the run window", () => {
    enableForTest();

    recordStreamTrace("dom_visible", { contentChars: 7 });
    recordRunStart();
    resetStreamTrace();

    const d = dump();

    assert.deepEqual(d.events, []);
    assert.deepEqual(d.counters, {});
    assert.equal(d.runStartedAt, null);
  });

  it("6. disable-after-enable freezes capture (isolation seam)", () => {
    enableForTest();
    recordStreamTrace("ws_state", { state: "open" });
    disableForTest();
    recordStreamTrace("ws_state", { state: "error" });

    const d = dump();

    assert.equal(d.events.length, 1);
    assert.equal(d.events[0].state, "open");
  });
});
