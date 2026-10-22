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

export type RunOutcome = "done" | "aborted" | "lost";

export interface FinalisationFacts {
  // The authoritative history reload gained a NEW assistant reply (vs the
  // run's baseline count).
  historyGainedReply: boolean;
  // The run streamed content (the streaming snapshot carries text) OR an
  // earlier attempt already preserved a partial row on screen.
  hasPartial: boolean;
  // The bounded retry already ran once — this is the final attempt.
  finalAttempt: boolean;
}

export interface FinalisationDecision {
  // "finalising" keeps the run LIVE (composer locked) while the bounded
  // retry re-confirms history; any terminal phase settles the timeline.
  phase: RunPhase;
  // Append/keep the local partial row so the transcript never shows a
  // reply-less settlement for a run that streamed content.
  preservePartial: boolean;
  clearStreaming: boolean;
  // Schedule the bounded second confirmation attempt.
  armRetry: boolean;
}

// terminalPhaseFor maps the backend's run outcome onto the timeline's
// terminal phase (the v1.2.8 "lost" sentinel settles as Stopped).
export function terminalPhaseFor(outcome: RunOutcome): RunPhase {
  return outcome === "done" ? "complete" : "aborted";
}

// planFinalisation is the WHOLE finalisation decision, extracted as a
// PURE function so the settlement contract is unit-testable without the
// store or the WebSocket (the same discipline as nextPhase above).
//
// THE RUN-#39 DEFECT CLASS (e2e/live-stream.spec.ts:266, "Error: exactly
// one assistant message"): finaliseRun always settled the phase to a
// TERMINAL state even when the authoritative history did not yet contain
// the reply (a first reload that raced the persist on a slow host), and
// its designed bounded retry could never fire (it required the phase to
// still be "finalising" — which the terminal settle made impossible).
// Under hosted contention with a suspended rAF the transcript then showed
// the WRONG number of assistant rows at settlement: zero (the reload
// wiped the visible partial), or the degraded "partial preserved locally"
// state standing in for a reply that HAD persisted milliseconds later.
//
// The contract keeps the transcript at EXACTLY ONE assistant row on every
// path:
//   - reply confirmed by history  → settle terminal, streaming dropped;
//   - reply missing + partial seen, FIRST attempt → stay "finalising",
//     preserve the partial as the visible row, arm the bounded retry;
//   - reply missing + partial seen, FINAL attempt → settle terminal on
//     the preserved partial (the reply genuinely never persisted);
//   - reply missing + nothing streamed → settle terminal (nothing ever
//     existed to show; the error path carries captions).
export function planFinalisation(
  facts: FinalisationFacts,
  outcome: RunOutcome,
): FinalisationDecision {
  const terminal = terminalPhaseFor(outcome);

  if (facts.historyGainedReply) {
    return {
      phase: terminal,
      preservePartial: false,
      clearStreaming: true,
      armRetry: false,
    };
  }

  if (facts.hasPartial) {
    if (facts.finalAttempt) {
      return {
        phase: terminal,
        preservePartial: true,
        clearStreaming: true,
        armRetry: false,
      };
    }
    return {
      phase: "finalising",
      preservePartial: true,
      clearStreaming: true,
      armRetry: true,
    };
  }

  return {
    phase: terminal,
    preservePartial: false,
    clearStreaming: true,
    armRetry: false,
  };
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
