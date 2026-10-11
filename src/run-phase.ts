// v1.2.2 — the generation lifecycle state machine, extracted as a PURE
// module so the run-phase transitions can be unit-tested (node --test)
// without booting the Zustand store or any browser API.
//
// The phase is derived ONLY from real backend events the frontend actually
// receives (run POST accepted, reasoning/response deltas, thinking/tool
// activity, done/error/abort) — the UI never invents a state the backend
// did not signal.
export type RunPhase =
  | "idle"
  | "preparing"
  | "thinking"
  | "generating"
  | "pausing"
  | "paused"
  | "resuming"
  | "finalising"
  | "complete"
  | "error"
  | "aborted";

// The event kinds that drive transitions. Every kind maps to a real wire
// event or a real store-side action (optimistic send, abort button).
export type RunEventKind =
  | "run_started" // POST /api/run accepted (optimistic, before first event)
  | "reasoning_delta" // activity type "reasoning" (cumulative caption)
  | "response_delta" // activity type "response" (cumulative caption)
  | "thinking_activity" // activity types "thinking"/"context"/"tool_*"
  | "pause_requested" // POST /api/run/pause accepted (phase "pausing")
  | "paused" // backend confirmed: durable checkpoint persisted (phase "paused")
  | "resume_requested" // POST /api/run/resume accepted (phase "resuming")
  | "resumed" // backend confirmed: run generating again
  | "done" // activity type "done"
  | "history_confirmed" // authoritative session history contains the reply
  | "error" // activity type "error"
  | "aborted"; // user pressed Stop / backend abort acknowledged

// nextPhase is the whole machine: (phase, event) -> phase.
//
// Rules:
//   - idle + run_started                -> preparing      (Send accepted)
//   - preparing/thinking + reasoning    -> thinking       (model reasoning)
//   - preparing/thinking + response     -> generating     (first answer token)
//   - any live phase + thinking_activity stays/keeps phase
//     (tool/context events accompany reasoning or generation; they never
//      demote generating back to thinking)
//   - live + done                       -> finalising     (awaiting history)
//   - finalising + history_confirmed    -> complete
//   - live + error                      -> error
//   - live + aborted                    -> aborted
//   - terminal phases only leave via run_started (a fresh run)
export function nextPhase(phase: RunPhase, event: RunEventKind): RunPhase {
  switch (event) {
    case "run_started":
      return "preparing";

    case "pause_requested":
      // Only a live run pauses. A duplicate pause (double-click, retry,
      // stale client) is a no-op — the backend contract is idempotent.
      if (isLivePhase(phase)) {
        return "pausing";
      }
      return phase;

    case "paused":
      // The backend confirms PAUSED only after its durable checkpoint is
      // persisted. From any live or pausing phase.
      if (isLivePhase(phase) || phase === "pausing") {
        return "paused";
      }
      return phase;

    case "resume_requested":
      if (phase === "paused") {
        return "resuming";
      }
      return phase;

    case "resumed":
      if (phase === "resuming" || phase === "paused") {
        return "generating";
      }
      return phase;

    case "reasoning_delta":
      if (phase === "preparing" || phase === "thinking" || phase === "resuming") {
        return "thinking";
      }
      return phase;

    case "response_delta":
      if (phase === "preparing" || phase === "thinking" || phase === "resuming") {
        return "generating";
      }
      return phase;

    case "thinking_activity":
      if (phase === "preparing") {
        return "thinking";
      }
      return phase;

    case "done":
      if (isLivePhase(phase)) {
        return "finalising";
      }
      return phase;

    case "history_confirmed":
      if (phase === "finalising") {
        return "complete";
      }
      return phase;

    case "error":
      // v1.7.4: a paused run can also receive an authoritative terminal
      // error (engine death while parked) — the backend is the authority.
      if (isLivePhase(phase) || phase === "paused") {
        return "error";
      }
      return phase;

    case "aborted":
      // v1.7.4: Stop after pause settles the parked run as aborted — the
      // backend really does abort a paused run, so the machine follows.
      if (isLivePhase(phase) || phase === "paused") {
        return "aborted";
      }
      return phase;

    default:
      return phase;
  }
}

// isLivePhase reports whether the phase belongs to an in-flight run.
// v1.7.4: pausing/resuming are live CONTROL phases (the run is still
// registered and can still be stopped); "paused" is deliberately NOT live —
// the run is safely parked and the composer is editable.
export function isLivePhase(phase: RunPhase): boolean {
  return (
    phase === "preparing" ||
    phase === "thinking" ||
    phase === "generating" ||
    phase === "pausing" ||
    phase === "resuming" ||
    phase === "finalising"
  );
}

// isPausedPhase reports the confirmed paused state: the checkpoint is
// durable, the composer is editable, resume/edit/discard are offered.
export function isPausedPhase(phase: RunPhase): boolean {
  return phase === "paused";
}

// isTerminalPhase reports whether the phase is a settled end state.
export function isTerminalPhase(phase: RunPhase): boolean {
  return phase === "complete" || phase === "error" || phase === "aborted";
}

// PHASE_LABELS is the user-facing timeline wording. Unknown/legacy phases
// render verbatim — never invented.
export const PHASE_LABELS: Record<RunPhase, string> = {
  idle: "Idle",
  preparing: "Preparing",
  thinking: "Thinking",
  generating: "Generating",
  pausing: "Pausing…",
  paused: "Paused",
  resuming: "Resuming…",
  finalising: "Finalising",
  complete: "Complete",
  error: "Failed",
  aborted: "Stopped",
};
