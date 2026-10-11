// session-list-guard.ts — v1.7.5 stale session-list response protection.
//
// PROBLEM (the v1.7.4 Linux E2E failure, root-caused): refreshSessions()
// captured the MODE but not a mutation/request generation. Multiple
// asynchronous GET /api/sessions responses could be in flight at once,
// and a STALE response — one that started BEFORE a create/delete/rename/
// mode switch — could land AFTER the mutation applied and overwrite the
// newer state. Concretely: a deleted session could be RESURRECTED in the
// sidebar by a GET response that was already on the wire when the DELETE
// succeeded, and two overlapping refreshes could complete out of order so
// the OLDER list won.
//
// FIX (one monotonic generation, no second cache): every session-list
// consumer takes a ticket from ONE monotonic counter; any mutation (or a
// newer refresh) invalidates every ticket taken before it. A response may
// only land while its ticket is still the current generation. This is the
// smallest architecture-preserving guard: the store remains the ONE
// authoritative session list; the guard owns no session data at all.
//
// The guard is a PURE module so the out-of-order delivery semantics are
// deterministically unit-testable (node --test) without the Zustand store
// or any browser API.

export interface SessionListGuard {
  /** Begin a new list request: invalidates all earlier tickets, returns this request's ticket. */
  begin(): number;
  /** A mutation happened: every in-flight request started before this instant must never land. */
  invalidate(): void;
  /** Report whether this ticket is still the current generation. */
  isActive(ticket: number): boolean;
}

export function createSessionListGuard(): SessionListGuard {
  // Monotonic generation counter. Growth is unbounded but each step is one
  // request/mutation — numerically irrelevant for the lifetime of a page.
  let generation = 0;

  return {
    begin(): number {
      generation += 1;
      return generation;
    },

    invalidate(): void {
      generation += 1;
    },

    isActive(ticket: number): boolean {
      return ticket === generation;
    },
  };
}
