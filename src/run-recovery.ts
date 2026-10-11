// run-recovery.ts — v1.2.8 wire-contract helpers for the idle-sentinel
// recovery paths (PURE module).

/**
 * parseEndedAt accepts the two shapes the backend has used on the wire
 * for the authoritative `lastRun.endedAt` value:
 *   1. an RFC3339 timestamp STRING — the actual shape Go marshals
 *      time.Time into (pinned by internal/api/runregistry_test.go), and
 *   2. an epoch-millisecond number (older wire shape).
 *
 * ROOT-CAUSE FIX (v1.2.8): the parser previously accepted ONLY numbers,
 * so the authoritative fast path in recoverRunFromIdle could never fire
 * and every recovery silently fell through to the grace re-check.
 */
export function parseEndedAt(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }

  if (typeof value === "string" && value !== "") {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) {
      return parsed;
    }
  }

  return null;
}
