// history-surface.ts — v1.4.0: the transcript surface state machine.
//
// The conversation area must render exactly one of four states, and the
// choice is a PURE function of authoritative inputs:
//
//   error     — the history fetch FAILED (retryable; never rendered as
//               an empty conversation — a failed fetch says nothing
//               about whether the history is empty)
//   loading   — the history fetch is in flight (the empty invite would
//               be a claim that was never established)
//   messages  — a page arrived; render the transcript (streaming/live
//               phases render even while a reload is in flight)
//   empty     — a page arrived with zero messages (the ONLY state that
//               may render the "conversation ready" invitation)
//
// Extracted as a pure function so the UI and the unit tests share ONE
// definition of the contract (the same discipline as run-phase.ts).

export type HistorySurfaceState = "loading" | "error" | "messages" | "empty";

export interface HistorySurfaceInput {
  /** The store's authoritative history fetch status. */
  historyStatus: "loading" | "ready" | "error";
  /** Number of currently held transcript messages. */
  messageCount: number;
  /** A live streaming assistant bubble is attached. */
  streaming: boolean;
  /** The run lifecycle phase is live (preparing/streaming/finalizing). */
  runLive: boolean;
}

export function historySurfaceState(input: HistorySurfaceInput): HistorySurfaceState {
  const { historyStatus, messageCount, streaming, runLive } = input;

  // A live surface (messages on screen, a streaming bubble, or an
  // active run) always wins over a transient reload state: reloading a
  // transcript the user is already reading must not blank the view.
  const liveSurface = messageCount > 0 || streaming || runLive;

  if (historyStatus === "error" && !liveSurface) {
    return "error";
  }

  if (historyStatus === "loading" && !liveSurface) {
    return "loading";
  }

  if (messageCount === 0 && !streaming && !runLive) {
    return "empty";
  }

  return "messages";
}
