// stream-trace.ts — v1.9.4 (P0-A): the OPT-IN, TEST-ONLY STREAM BOUNDARY
// TRACE.
//
// PURPOSE: capture the last-good/first-bad boundary of a live run across
//   WebSocket receive → fast-path fold → accumulator flush → Zustand
//   commit → visible DOM → terminal settlement
// with MONOTONIC timestamps, sequence numbers and LENGTHS ONLY. It exists
// because the supplied Windows runtime evidence ends at the server's run
// summary (backend had a non-empty answer) with no event-by-event proof of
// where visibility broke — this module makes every boundary measurable on
// the real WebView2 runtime without changing product behavior.
//
// PRIVACY AND COST CONTRACT:
//   - DISABLED by default. It arms only when localStorage
//     `SHEYTAN_STREAM_TRACE` === "1" (or a test explicitly enables it via
//     enableForTest). Every hot-path call first checks one boolean and
//     returns — near-zero overhead when disabled.
//   - NEVER logs user content: captions are reduced to byte lengths,
//     event kinds to canonical names, identifiers to ephemeral per-run
//     sequence numbers minted here (never the backend runId/sessionId).
//   - BOUNDED: a fixed-size ring (recent events) plus per-run aggregate
//     counters; dumpStreamTrace() serializes one compact JSON snapshot
//     for the test artifact. It is NEVER written to ordinary application
//     logs (the store never calls logging with it) and never leaves the
//     process unless a test explicitly dumps it.
//   - On E2E failure the Playwright afterEach hook attaches the dump to
//     the test report (testInfo.attach) — the "compact sanitized test
//     trace saved on failure" the release contract requires.

const TRACE_FLAG = "SHEYTAN_STREAM_TRACE";

const RING_CAP = 512;

export interface StreamTraceEvent {
  // Monotonic milliseconds (performance.now() origin — durations between
  // entries are honest even across wall-clock adjustments).
  t: number;
  // Ephemeral sequence minted by THIS module in receive order.
  seq: number;
  // Boundary name: ws_frame | ws_state | fastpath_fold | accumulator_flush
  // | store_commit | dom_visible | lifecycle | reset.
  stage: string;
  // Canonical event kind (assistant_delta / thinking_delta / done / …).
  kind?: string;
  // Wire-side sequence (runLive monotonic seq) when the frame carried one.
  wireSeq?: number;
  // Content lengths only — never content.
  captionChars?: number;
  contentChars?: number;
  reasoningChars?: number;
  // Batch/queue observations.
  batchDepth?: number;
  queueDepth?: number;
  // Which scheduler boundary fired the flush (task | timer | frame | sync).
  source?: string;
  // Connection state transitions (connecting | connected | error | …).
  state?: string;
  // Error codes only (never messages with content).
  code?: string;
}

interface StreamTraceState {
  enabled: boolean;
  ring: StreamTraceEvent[];
  seq: number;
  // Per-run aggregates (reset by traceRunReset).
  counters: Record<string, number>;
  runStartedAt: number | null;
}

const state: StreamTraceState = {
  enabled: false,
  ring: [],
  seq: 0,
  counters: {},
  runStartedAt: null,
};

function detectEnabled(): boolean {
  try {
    return typeof localStorage !== "undefined" &&
      localStorage.getItem(TRACE_FLAG) === "1";
  } catch {
    return false;
  }
}

// Enabled at module load from the opt-in flag; a test may override.
state.enabled = detectEnabled();

// v1.9.4: the test-artifact hook. When the trace is enabled (and ONLY
// then) an explicit dumper is exposed for harnesses (the Windows
// acceptance script) to attach the sanitized snapshot to the failure
// artifact. Never populated in normal runs.
if (typeof window !== "undefined" && state.enabled) {
  (window as unknown as Record<string, unknown>).__SHEYTAN_STREAM_TRACE_DUMP__ =
    () => dumpStreamTrace();
}

export function streamTraceEnabled(): boolean {
  return state.enabled;
}

export function enableForTest(): void {
  state.enabled = true;
}

export function disableForTest(): void {
  state.enabled = false;
}

// record pushes one boundary observation (no-op when disabled — the
// hot-path callers must treat this as free).
export function recordStreamTrace(
  stage: string,
  detail?: Omit<StreamTraceEvent, "t" | "seq" | "stage">,
): void {
  if (!state.enabled) {
    return;
  }

  state.seq += 1;

  const entry: StreamTraceEvent = {
    t: Math.round(performance.now() * 1000) / 1000,
    seq: state.seq,
    stage,
    ...detail,
  };

  state.ring.push(entry);

  if (state.ring.length > RING_CAP) {
    state.ring.splice(0, state.ring.length - RING_CAP);
  }

  state.counters[stage] = (state.counters[stage] ?? 0) + 1;
}

// recordRunStart anchors the client-side run window (Send accepted).
export function recordRunStart(): void {
  if (!state.enabled) {
    return;
  }

  state.runStartedAt = performance.now();
  recordStreamTrace("lifecycle", { state: "run_start" });
}

// recordRunEnd closes the run window with the terminal phase name.
export function recordRunEnd(phase: string): void {
  if (!state.enabled) {
    return;
  }

  const totalMs =
    state.runStartedAt !== null
      ? Math.round(performance.now() - state.runStartedAt)
      : undefined;

  recordStreamTrace("lifecycle", {
    state: "run_end",
    code: phase,
    batchDepth: totalMs,
  });
}

// dumpStreamTrace serializes the compact sanitized snapshot: stage
// counters first (aggregate view), then the bounded recent ring (ordered
// boundary view). Content-free by construction.
export function dumpStreamTrace(): string {
  return JSON.stringify({
    enabled: state.enabled,
    counters: { ...state.counters },
    runStartedAt: state.runStartedAt,
    events: state.ring,
  });
}

// resetStreamTrace clears ring + counters (test isolation seam).
export function resetStreamTrace(): void {
  state.ring.length = 0;
  state.seq = 0;
  state.counters = {};
  state.runStartedAt = null;
}
