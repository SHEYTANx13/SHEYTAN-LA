# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. Such one-time
> authorizations exist for the v1.11.1 maintenance cleanups (2026-11-14
> and the run-43630164424 repair, 2026-11-15) and for the single
> run-43718750096 work-order refresh (2026-11-16), plus one further
> refresh for the run-43773885283 integrity-first milestone contract
> (2026-11-16, later the same day): content refreshes under the active
> maintenance work order only. They set no precedent — from the next
> session onward the human-only rule applies again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Current work order

| Field | Value |
|---|---|
| **Owner** | Human developer |
| **Objective** | Publish the v1.11.1 integrity-first milestone candidate (run-43773885283 follow-up): review the local branch `work/v1.11.1-integrity-stability-uiux` (baseline commit `124b08a`, the run-43773885283 commit and current main HEAD), decide on push timing, then watch the hosted run. |
| **Priority** | P0 — run 43773885283 left exactly ONE Windows headless failure: `TestDialContextBusyBudgetExhaustsTyped` received the generic "no listening pipe instance after a 2000ms busy wait" error because the production busy-dial loop exited after ONE per-wait cap (2000ms) instead of exhausting the 10s budget, leaving the typed `ErrBusyDialBudgetExhausted` unreachable. The candidate fixes that root cause and delivers the milestone's defining content-integrity work — recoverable session deletion (trash + tombstone + restore + two-step purge) with API surface and regression tests — plus the isolated Go-Gui v0.96.0 POC and the new architecture/diagnostics documentation set. |
| **Status** | Candidate complete locally on Linux (Windows busy-dial fix compile-verified with `GOOS=windows go vet/build` and pinned by a new focused regression test; headless suite 68 pkgs green, race gate 7/7 incl. the new trash tests, stress 47/47, collector fixtures 25/25, diagnostics self-test, gofmt/vet, full desktop sweep 72 pkgs green incl. the Fyne UI gate under Xvfb, Go-Gui POC 5/5); hosted and native-Windows verification NOT RUN (no push authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted Windows headless suite green — `TestDialContextBusyBudgetExhaustsTyped` passes natively with the new `TestDialContextBusyDialCrossesWaitCap`. (2) The content-integrity suites (`internal/sessions`, `internal/api`) green on both hosted lanes. (3) No version change anywhere — `internal/config.AppVersion` stays 1.11.1 (version purity tests green). (4) The independent-gates DAG stays green exactly as in run 43773885283; the Windows package unblocks through the upstream test gate, never by bypassing it. |
| **Dependencies / blockers** | None technical. Publishing requires the operator's push decision. The two research reports and the raw run log were not attached to the milestone session — requirements were mapped from the work order's §13 enumeration (see `docs/architecture/REQUIREMENTS-TRACEABILITY.md` provenance note); the Code Scanning inventory is BLOCKED (unauthenticated API rate-limited; recorded in the delivery's `code-scanning-alert-inventory.md`). |
| **Evidence needed** | Hosted run log for the candidate commit; `TestDialContextBusyBudgetExhaustsTyped` + `TestDialContextBusyDialCrossesWaitCap` green on the hosted Windows lane; the Fyne UI gate green on the candidate; `note.md` beside the delivery ZIP. |
| **Next action** | Operator: review the candidate diff and `note.md` in the delivery, then push the branch or request changes. |

---

## Open backlog (prioritized)

1. **Hosted Windows verification of the v1.11.1 IPC gates** (P0, follows
   the push above) — the new smoke semantics (window + handshake + core
   read + core-close record + JSONL validity + responsiveness soak +
   graceful close) must be observed green once.
2. **chromedp browser-tool runtime smoke** (P1) — the v0.23.1/v0.160.3
   migration is compile- and suite-verified; a Chrome-equipped hosted
   lane must exercise the real browser tools once.
3. **Physical verification of 3D Studio WebGL** (P1) — r187 assets are
   compile/embed-verified; a real browser/hardware run is still physical
   class. See [`3dstudio.md`](3dstudio.md).
4. **Physical high-DPI / multi-monitor Fyne behaviour** (P2).
5. **Governor remains policy-only** (P2) — admission envelope exists; no
   automatic enforcement beyond it (deliberate).
6. **MCP client into the live tool registry** (P2).
7. **UI/UX phased upgrade** (P1, direction) — the phased plan
   (design-system tokens → conversation surface → artifact/workspace →
   goal/command surface → polish → measurement) is owned by
   [`ROADMAP.md`](ROADMAP.md); Phases 0–2 (design tokens +
   [`ui-contract.md`](ui-contract.md), conversation surface,
   artifact/workspace integration) are delivered.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
