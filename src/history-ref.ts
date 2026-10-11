// history-ref.ts — v1.2.8 cross-mode history reference helpers (PURE
// module).
//
// An attached history session is a REFERENCE, not a copy: the picker
// attaches stable ids, each run retrieves only the relevant portions, the
// source conversation is never merged into the active transcript, and the
// total number of refs is bounded. These helpers are the deterministic
// core of that behavior, extracted for unit tests.

export type HistoryRefRange = {
  from: number;
  to: number;
};

export type HistoryRef = {
  sessionId: string;
  mode?: string;
  summaryVersion?: number;
  ranges?: HistoryRefRange[];
};

/** MAX_HISTORY_REFS — mirrors the backend sessions.MaxHistoryRefs cap. */
export const MAX_HISTORY_REFS = 4;

/**
 * normalizeHistoryRefs trims, dedupes by id, drops self-references and
 * empties, and caps the list at MAX_HISTORY_REFS (keeping the most
 * recently attached — the array order is the attachment order).
 */
export function normalizeHistoryRefs(
  refs: HistoryRef[] | null | undefined,
  activeSessionId: string | null,
): HistoryRef[] {
  if (!Array.isArray(refs)) {
    return [];
  }

  const seen = new Set<string>();
  const out: HistoryRef[] = [];

  for (const ref of refs) {
    const id = typeof ref?.sessionId === "string" ? ref.sessionId.trim() : "";
    if (id === "") {
      continue;
    }

    if (activeSessionId && id === activeSessionId) {
      continue; // the active transcript is already here — never self-attach
    }

    if (seen.has(id)) {
      continue;
    }

    seen.add(id);
    out.push({
      sessionId: id,
      mode: ref.mode,
      summaryVersion: ref.summaryVersion,
      ranges: Array.isArray(ref.ranges) ? ref.ranges : undefined,
    });

    if (out.length >= MAX_HISTORY_REFS) {
      break;
    }
  }

  return out;
}

/**
 * mergeHistoryRefs attaches new refs to the existing list (dedup by id,
 * cap enforced, existing order preserved, new ones appended).
 */
export function mergeHistoryRefs(
  existing: HistoryRef[] | null | undefined,
  added: HistoryRef[] | null | undefined,
  activeSessionId: string | null,
): HistoryRef[] {
  return normalizeHistoryRefs([...(existing ?? []), ...(added ?? [])], activeSessionId);
}

/**
 * removeHistoryRef detaches one ref by session id.
 */
export function removeHistoryRef(
  refs: HistoryRef[] | null | undefined,
  sessionId: string,
): HistoryRef[] {
  return (refs ?? []).filter((r) => r.sessionId !== sessionId);
}

/**
 * refLabel renders the chip label — the mode badge makes the provenance
 * visible in the composer ("source: agent conversation").
 */
export function refLabel(ref: HistoryRef): string {
  const mode = ref.mode === "chat" ? "Chat" : "Agent";
  return `${mode} · ${ref.sessionId.slice(0, 8)}`;
}

/**
 * describeRange renders the retrieval scope for the chip tooltip
 * ("whole session" or "turns 2–5").
 */
export function describeRange(ref: HistoryRef): string {
  if (!ref.ranges || ref.ranges.length === 0) {
    return "Relevant turns retrieved per request (whole session)";
  }
  const parts = ref.ranges.map((r) => `${r.from + 1}–${r.to}`);
  return `Focused on message(s) ${parts.join(", ")}`;
}
