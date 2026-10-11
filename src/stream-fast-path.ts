// stream-fast-path.ts — v1.8.1 SINGLE-FRAME STREAMING ROUTING (pure module).
//
// THE DEFECT THIS REPAIRS: src/store.ts previously routed EVERY activity
// frame through TWO render-frame boundaries before streamed text became
// visible:
//
//      ws message → queueActivity → rAF#1 (activityFlushFrame)
//      → setActivityBatch → handleConversationEvent → queueStreamingContent
//      → rAF#2 (streamingFlushFrame) → flushStreaming → React render
//
// The first streamed token of a fast local model could therefore wait
// TWO frames (~33 ms at 60 Hz) before appearing. The v1.8.1 contract:
// STREAM-CRITICAL events (response / reasoning content — the canonical
// assistant_delta / thinking_delta kinds) fold into the streaming
// accumulator THE MOMENT the WebSocket delivers them, with at most ONE
// render-frame coalescing boundary (the streaming flush frame) between
// the socket and visible text.
//
// Timeline/bulk activity (status, tool, context, perf, done/error/…)
// keeps the existing activity-batch path unchanged — including the
// lifecycle events, which flush streaming SYNCHRONOUSLY (never a second
// frame) when the batch reaches them.
//
// This module owns the routing decision and the no-double-processing
// ledger so the decision is unit-testable without a store, a browser or
// timing: a fake requestAnimationFrame controller drives it in
// stream-fast-path.test.ts.
//
// Invariants preserved (see the test file for the pinned proofs):
//   - a stream-critical event joins the ACTIVITY TIMELINE (the batch is
//     not starved) but is never PROCESSED twice;
//   - cumulative snapshots REPLACE, never append (the accumulator
//     contract is unchanged — stream-accumulator.ts);
//   - the fast path engages only for content-bearing wire kinds;
//     everything else — done/error/abort included — is untouched.

import {
  normalizeEventKind,
  type CanonicalEventKind,
} from "./run-events.ts";

// StreamingFastPathCallbacks is the store's injection seam: the exact
// operations the store's activity path performs for streamed content,
// supplied by the caller so the routing logic stays pure.
export interface StreamingFastPathCallbacks {
  // foldContent folds one cumulative response snapshot.
  foldContent(caption: string): void;
  // foldReasoning folds one cumulative reasoning snapshot.
  foldReasoning(caption: string): void;
  // phaseResponseDelta / phaseReasoningDelta advance the run-phase
  // machine exactly as the batched path does.
  phaseResponseDelta(): void;
  phaseReasoningDelta(): void;
  // markRunEvidence records that THIS run delivered hub evidence.
  markRunEvidence(): void;
}

// isStreamCriticalKind reports whether a canonical event kind carries
// streamed CONTENT (the fast-path eligible set).
export function isStreamCriticalKind(kind: CanonicalEventKind): boolean {
  return kind === "assistant_delta" || kind === "thinking_delta";
}

// isStreamCriticalWireType maps a RAW wire type onto the fast-path
// decision (response/assistant_delta + reasoning/thinking_delta and
// nothing else).
export function isStreamCriticalWireType(type: string): boolean {
  return isStreamCriticalKind(normalizeEventKind(type));
}

export class StreamingFastPath {
  // processed is the no-double-processing ledger: ids of events the fast
  // path already folded. The activity-batch flush consumes (deletes)
  // entries as it skips them, so the ledger is self-draining and never
  // outlives the events it describes.
  private processed = new Set<string>();

  private readonly cb: StreamingFastPathCallbacks;

  constructor(callbacks: StreamingFastPathCallbacks) {
    this.cb = callbacks;
  }

  // deliver routes ONE activity event the instant the WebSocket
  // receives it. Returns true when the event was fast-path processed
  // (the caller still queues it for the timeline; the batch flush will
  // skip its conversation processing via consumeBatchEvent).
  //
  // Mirrors handleConversationEvent's assistant_delta/thinking_delta
  // arms EXACTLY: run evidence is marked for the event type (the
  // RUN_EVENT_TYPES contract), and the fold + phase transition happen
  // only for non-empty captions.
  deliver(id: string, type: string, caption: unknown): boolean {
    if (!isStreamCriticalWireType(type)) {
      return false;
    }

    this.processed.add(id);
    this.cb.markRunEvidence();

    const text = typeof caption === "string" ? caption : "";
    if (text === "") {
      return true;
    }

    if (normalizeEventKind(type) === "thinking_delta") {
      this.cb.foldReasoning(text);
      this.cb.phaseReasoningDelta();
      return true;
    }

    this.cb.foldContent(text);
    this.cb.phaseResponseDelta();
    return true;
  }

  // consumeBatchEvent reports whether the activity-batch flush must
  // SKIP this event's conversation processing (it was already folded by
  // the fast path). Deleting on read keeps the ledger bounded.
  consumeBatchEvent(id: string): boolean {
    return this.processed.delete(id);
  }

  // reset clears the ledger (socket reconnect / session switch / test
  // isolation — mirrors resetPendingActivity).
  reset(): void {
    this.processed.clear();
  }
}
