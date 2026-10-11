// run-events.ts — v1.2.5 wire-event vocabulary mapping (PURE module).
//
// The backend activity stream carries BOTH the legacy event names
// (response / reasoning / done / tool_start / tool_end / verification /
// error / context / perf / failure / session) and the v1.2.5 additions:
//
//      thinking_start / thinking_delta / thinking_end
//      assistant_delta
//      complete
//      status            (live status chip telemetry: phase · tier · tokens)
//      escalation        (tier move with reason, e.g.
//                         "FAST 3.8K → STANDARD 7.2K reason=MissingFileContext")
//
// normalizeEventKind maps every wire name onto the canonical kinds the
// store consumes. Legacy names keep their meaning; the new names are
// ALIASES of the same semantics (never a second copy of streamed content
// — the backend emits thinking_start/end as contentless markers and the
// reasoning/response events carry the cumulative text as before).
//
// Pure functions only — unit-testable without a store or browser API.

export type CanonicalEventKind =
  | "assistant_delta" // streamed answer text (cumulative snapshot)
  | "thinking_delta" // streamed reasoning text (cumulative snapshot)
  | "thinking_open" // reasoning began — open the panel
  | "thinking_close" // reasoning finished — fold the panel
  | "tool_start"
  | "tool_end"
  | "verification"
  | "status" // live status telemetry (no content)
  | "escalation" // tier move notice (no content)
  | "progress" // legacy "thinking" progress captions (iterations, offline notes)
  | "context" // context provenance report
  | "perf"
  | "failure"
  | "plan"
  | "session"
  | "task" // v1.2.8: bounded agent task-state snapshot (goal/step/files/tests/…)
  | "handoff" // v1.2.8: agent.md handoff written at settlement
  | "idle" // socket has no run attached (recovery signal)
  | "done" // run finished (legacy name)
  | "complete" // run finished (v1.2.5 name)
  | "aborted" // v1.8.0: run stopped by user/budget — the honest abort marker
  | "error"
  | "unknown";

const KIND_MAP: Record<string, CanonicalEventKind> = {
  // streamed content (cumulative snapshots — never deltas to append)
  response: "assistant_delta",
  assistant_delta: "assistant_delta",

  reasoning: "thinking_delta",
  thinking_delta: "thinking_delta",

  // contentless thinking markers
  thinking_start: "thinking_open",
  thinking_end: "thinking_close",

  // tool lifecycle
  tool_start: "tool_start",
  tool_end: "tool_end",

  // verification + run-end
  verification: "verification",
  done: "done",
  complete: "complete",
  aborted: "aborted", // v1.8.0: the orchestrator's honest ctx-cancelation marker

  // statuses
  thinking: "progress", // legacy caption events keep their meaning
  status: "status",
  escalation: "escalation",
  context: "context",
  perf: "perf",
  failure: "failure",
  plan: "plan",
  session: "session",
  task: "task",
  handoff: "handoff",
  idle: "idle",

  error: "error",
};

// normalizeEventKind maps a wire event type to its canonical kind.
export function normalizeEventKind(type: string): CanonicalEventKind {
  return KIND_MAP[type] ?? "unknown";
}

// isRunEvidence reports whether an event kind proves a run is live (the
// watchdog that recovers stuck composers trusts only these).
export function isRunEvidence(kind: CanonicalEventKind): boolean {
  return (
    kind === "assistant_delta" ||
    kind === "thinking_delta" ||
    kind === "thinking_open" ||
    kind === "thinking_close" ||
    kind === "tool_start" ||
    kind === "tool_end" ||
    kind === "done" ||
    kind === "complete" ||
    kind === "aborted" ||
    kind === "status" ||
    kind === "escalation" ||
    kind === "task"
  );
}

// ThinkingControl is the composer's per-request REASONING DEPTH control
// (v1.8.5 four-level ladder). It is sent with every run request and each
// level carries a REAL numeric thinking-token budget that reaches the
// generation request (llama.cpp `reasoning_budget_tokens` — verified in
// both managed engine builds):
//
//   low  — budget 0: thinking disabled, latency first (legacy "fast")
//   mid  — budget 1024: bounded thinking, the balanced default
//   high — budget 4096: deeper reasoning (legacy "thinking")
//   ultra — engine default: unrestricted thinking
//
// Legacy persisted values (auto/fast/thinking) are migrated by
// normalizeThinkingControl — never re-emitted.
export type ThinkingControl = "low" | "mid" | "high" | "ultra";

export const THINKING_OPTIONS: {
  value: ThinkingControl;
  label: string;
  hint: string;
}[] = [
  {
    value: "low",
    label: "Low",
    hint: "Thinking off (budget 0) — latency first, smallest context",
  },
  {
    value: "mid",
    label: "Mid",
    hint: "Bounded thinking (1024-token budget) — the balanced default",
  },
  {
    value: "high",
    label: "High",
    hint: "Deep reasoning (4096-token budget) for complex work",
  },
  {
    value: "ultra",
    label: "Ultra",
    hint: "Unrestricted thinking — the engine's own maximum budget",
  },
];

// normalizeThinkingControl validates a persisted control value onto the
// v1.8.5 ladder, migrating the legacy v1.2.5 vocabulary (auto → mid,
// fast → low, thinking → high).
export function normalizeThinkingControl(v: unknown): ThinkingControl {
  if (v === "low" || v === "mid" || v === "high" || v === "ultra") {
    return v;
  }

  if (v === "fast") {
    return "low";
  }

  if (v === "thinking" || v === "deep") {
    return "high";
  }

  return "mid";
}

// ToolPolicyMode is the composer's per-request tool control.
export type ToolPolicyMode = "auto" | "manual";

export function normalizeToolPolicyMode(v: unknown): ToolPolicyMode {
  return v === "manual" ? "manual" : "auto";
}

// normalizeToolAllowlist keeps the manual allow-list honest: unique,
// trimmed, non-empty names.
export function normalizeToolAllowlist(list: unknown): string[] {
  if (!Array.isArray(list)) {
    return [];
  }

  const seen = new Set<string>();
  const out: string[] = [];

  for (const item of list) {
    if (typeof item !== "string") {
      continue;
    }

    const name = item.trim();
    if (name === "") {
      continue;
    }

    const key = name.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    out.push(name);
  }

  return out;
}

// ---------------------------------------------------------------------------
// v1.3.6 (spec §25): Net Search UI state — pure helpers, unit-testable.
// ---------------------------------------------------------------------------

// NetSearchState is the compact in-composer Net Search control state:
//
//      off      the control is disabled
//      enabled  armed — the next request may use the research tool
//      searching a run is executing the research tool right now
//      results   the last run produced external evidence
//      failed   the last search attempt failed (visible + actionable)
export type NetSearchState =
  | "off"
  | "enabled"
  | "searching"
  | "results"
  | "failed";

// isResearchToolStart reports whether one wire event marks the START of
// the backend research tool. The tool_start activity carries the tool
// call in `detail` ({function:{name,...}}) and a caption — both are
// consulted, neither is trusted beyond the name check.
export function isResearchToolStart(data: unknown): boolean {
  return eventToolName(data) === "research";
}

// isResearchToolEnd reports whether one wire event marks the END of the
// research tool call. tool_end captions read "Tool research → ...".
export function isResearchToolEnd(caption: unknown): boolean {
  if (typeof caption !== "string") {
    return false;
  }

  const c = caption.toLowerCase();

  return c.startsWith("tool research") || c.includes("tool \"research\"");
}

// researchEndFailed reports whether a tool_end result text marks the
// research call as FAILED (the backend prefixes refused/failed results
// with "Error:").
export function researchEndFailed(resultText: unknown): boolean {
  return typeof resultText === "string" && resultText.startsWith("Error:");
}

// extractNetSearchResultCount pulls the result count from a research
// result text when it is stated ("8 results", "8 个结果"). Returns null
// when the count is not stated — the UI must not invent one.
export function extractNetSearchResultCount(text: unknown): number | null {
  if (typeof text !== "string") {
    return null;
  }

  const m = text.match(/(\d+)\s+results?/i);

  return m ? Number(m[1]) : null;
}

function eventToolName(data: unknown): string | null {
  if (data === null || typeof data !== "object") {
    return null;
  }

  const detail = (data as Record<string, unknown>)["detail"];
  if (detail === null || typeof detail !== "object") {
    return null;
  }

  const fn = (detail as Record<string, unknown>)["function"];
  if (fn !== null && typeof fn === "object") {
    const name = (fn as Record<string, unknown>)["name"];
    if (typeof name === "string") {
      return name.toLowerCase();
    }
  }

  // Fallback: some paths flatten the tool name directly.
  const flat = (detail as Record<string, unknown>)["name"];
  if (typeof flat === "string") {
    return flat.toLowerCase();
  }

  return null;
}
