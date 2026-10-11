// memory-evidence.ts — v1.8.2 BACKEND-TRUTH MEMORY INDICATOR (pure module).
//
// The live generation bubble shows a compact line describing what the
// memory authorities ACTUALLY injected this turn. The backend owns the
// truth: the orchestrator composes a MemoryEvidence record from the
// injection facts (internal/contextplan.MemoryEvidence) and publishes it
// on the `context` activity; this module parses that wire block and
// renders the exact wording. Nothing here invents a count: a missing
// record means the UI shows NOTHING rather than a guess.

// MemoryEvidenceState mirrors internal/contextplan.MemoryEvidence.
export type MemoryEvidenceState = {
  summaryInjected: boolean;
  summaryTokens?: number;
  recalledExchanges?: number;
  historyRefs?: number;
  recallAttempted?: boolean;
};

// extractMemoryEvidence reads the memory-evidence block from one
// `context` activity's data. Returns null when the frame carries none.
export function extractMemoryEvidence(
  data: unknown,
): MemoryEvidenceState | null {
  if (data === null || typeof data !== "object") {
    return null;
  }

  const detail = (data as Record<string, unknown>)["detail"];
  if (detail === null || typeof detail !== "object") {
    return null;
  }

  const memory = (detail as Record<string, unknown>)["memory"];
  if (memory === null || typeof memory !== "object") {
    return null;
  }

  const m = memory as Record<string, unknown>;

  const num = (v: unknown): number | undefined =>
    typeof v === "number" && Number.isFinite(v) ? v : undefined;

  const summaryTokens = num(m.summaryTokens);
  const recalledExchanges = num(m.recalledExchanges);
  const historyRefs = num(m.historyRefs);

  const evidence: MemoryEvidenceState = {
    summaryInjected: m.summaryInjected === true,
    ...(summaryTokens !== undefined ? { summaryTokens } : {}),
    ...(recalledExchanges !== undefined ? { recalledExchanges } : {}),
    ...(historyRefs !== undefined ? { historyRefs } : {}),
    recallAttempted: m.recallAttempted === true,
  };

  return evidence;
}

// memoryEvidenceLine renders the compact, backend-truth memory line.
// The wording states exactly what the backend reported — never a
// fabricated count, never a fake "memory used".
export function memoryEvidenceLine(
  evidence: MemoryEvidenceState | null,
): string | null {
  if (!evidence) {
    return null;
  }

  const parts: string[] = [];

  if (evidence.summaryInjected) {
    parts.push("session summary");
  }

  if (typeof evidence.recalledExchanges === "number") {
    parts.push(
      evidence.recalledExchanges > 0
        ? `${evidence.recalledExchanges} recalled exchange${
            evidence.recalledExchanges === 1 ? "" : "s"
          }`
        : evidence.recallAttempted
          ? "no recall matches"
          : "recall not used",
    );
  }

  if (typeof evidence.historyRefs === "number" && evidence.historyRefs > 0) {
    parts.push(
      `${evidence.historyRefs} history reference${
        evidence.historyRefs === 1 ? "" : "s"
      }`,
    );
  }

  if (parts.length === 0) {
    return "Memory: session context only";
  }

  return `Memory: ${parts.join(" · ")}`;
}
