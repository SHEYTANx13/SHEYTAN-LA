// v1.2.2 — streaming snapshot accumulator, extracted as a PURE module so
// the backend wire contract is unit-testable (node --test) without the
// store or any browser API.
//
// THE CONTRACT (internal/agent/orchestrator.go, emitProgress): every
// `response` / `reasoning` activity event carries the FULL cumulative text
// emitted so far for the current iteration — NOT a delta. The previous
// store treated these captions as append-deltas, which duplicated the
// streamed text massively and re-duplicated it after reconnects.
//
// This accumulator implements the correct semantics:
//   - each event REPLACES the accumulated text for its stream;
//   - a shorter caption (a new agent iteration reset the server-side
//     builder) also replaces — the server is authoritative;
//   - duplicate/replayed events (the same cumulative caption arriving
//     again after a reconnect) are naturally idempotent;
//   - flush() returns the latest snapshot ONCE per animation frame, with
//     null marking "this stream did not change" so a reasoning-only
//     flush can never blank the visible content.
export interface StreamingSnapshot {
  content: string;
  reasoning: string;
}

export interface FlushResult {
  // null = no update arrived for this stream since the last flush.
  content: string | null;
  reasoning: string | null;
}

interface PendingState {
  content: string | null;
  reasoning: string | null;
}

export interface StreamingAccumulator extends PendingState {}

export function createStreamingAccumulator(): StreamingAccumulator {
  return { content: null, reasoning: null };
}

// applyResponseSnapshot folds one cumulative `response` caption.
export function applyResponseSnapshot(
  acc: StreamingAccumulator,
  caption: string,
): void {
  if (typeof caption !== "string" || caption.length === 0) {
    return;
  }

  acc.content = caption;
}

// applyReasoningSnapshot folds one cumulative `reasoning` caption.
export function applyReasoningSnapshot(
  acc: StreamingAccumulator,
  caption: string,
): void {
  if (typeof caption !== "string" || caption.length === 0) {
    return;
  }

  acc.reasoning = caption;
}

// isEmpty reports whether a flush would be a no-op.
export function isEmpty(acc: StreamingAccumulator): boolean {
  return acc.content === null && acc.reasoning === null;
}

// flush drains the pending cumulative snapshots (null = unchanged).
export function flush(acc: StreamingAccumulator): FlushResult {
  const result: FlushResult = {
    content: acc.content,
    reasoning: acc.reasoning,
  };

  acc.content = null;
  acc.reasoning = null;

  return result;
}

// mergeSnapshot applies one flush result onto the displayed snapshot:
// updated streams replace, unchanged streams keep their previous value.
export function mergeSnapshot(
  current: StreamingSnapshot | null,
  update: FlushResult,
): StreamingSnapshot {
  return {
    content:
      update.content !== null ? update.content : (current?.content ?? ""),
    reasoning:
      update.reasoning !== null ? update.reasoning : (current?.reasoning ?? ""),
  };
}
