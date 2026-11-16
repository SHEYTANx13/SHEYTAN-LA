# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. Such one-time
> authorizations exist for the v1.11.1 maintenance cleanups (2026-11-14
> and the run-43630164424 repair, 2026-11-15) and for the single
> run-43718750096 work-order refresh (2026-11-16), plus one further
> refresh for the run-43773885283 integrity-first milestone contract
> (2026-11-16, later the same day) and one for the run-43862893851
> hybrid-native-UI milestone contract (2026-11-16, latest of the same
> day): content refreshes under the active maintenance work order only.
> They set no precedent — from the next session onward the human-only
> rule applies again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Current work order

| Field | Value |
|---|---|
| **Owner** | Human developer |
| **Objective** | Publish the v1.11.1 hybrid-native-UI milestone candidate (run-43862893851 follow-up): review the local branch `hybrid-native-ui-v1111` (baseline commit `0d61aeb`, current main HEAD — the run-43862893851 commit), decide on push timing, then watch the hosted run. |
| **Priority** | P0 — run 43862893851 left exactly TWO Windows headless failures (`TestDialContextCallerDeadlineBoundsBusyDial`, `TestDialContextBusyDialCrossesWaitCap`): both landed in the exhausted-deadline branch of `dial_windows.go`, which attributed every exhaustion to `ErrBusyDialBudgetExhausted` without re-examining the caller context — a scheduler race after the ms-truncated wait clamp. The candidate fixes that attribution deterministically (ctx-first, then the arithmetic caller-deadline check; the typed budget error only for a still-live context), fixes the core-audit AWK `\"` regexp warning at source (+ fixture case 25), extends the Go-Gui POC to 10/10 with the mandated responsive matrix (a real 640x480 containment defect found and fixed), adds the isolated GoGPU spectrum POC (software render verified; GPU HARDWARE NOT RUN), the `internal/webapphost` contract (8/8 policy tests), and the hybrid-stack documentation set. |
| **Status** | Candidate complete locally on Linux (dial fix gofmt-clean, `GOOS=windows` build/vet/test-compile green with both tests' assertions unchanged; headless suite 69 pkgs green; race localipc+sessions green; releasecontract green; collector fixtures 26/0; Go-Gui POC 10/10; gogpu POC 2/2 plus a real software render and a bounded Xvfb windowed soak; webapphost 8/8); hosted and native-Windows verification NOT RUN (no push authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted Windows headless suite green — both run-43862893851 tests pass natively with unchanged assertions. (2) Full `internal/localipc` green on the Windows and POSIX lanes. (3) No version change anywhere — `internal/config.AppVersion` stays 1.11.1 (version purity green). (4) The independent-gates DAG unchanged; the Windows package unblocks through the upstream test gate, never by bypassing it. (5) The operator retrieves the live Code Scanning inventory (`security_events` scope, or the `code-scanning-alerts.txt` artifact the CodeQL workflow already exports per run) and files the two-warning fix as a follow-up work order — this candidate does NOT claim those warnings fixed. |
| **Dependencies / blockers** | None technical for the push. The two research reports and the raw run log were again not attached (provenance recorded in `docs/architecture/REQUIREMENTS-TRACEABILITY.md`); Code Scanning retrieval is BLOCKED from the agent sandbox (unauthenticated API 401/403) — operator-side retrieval required. |
| **Evidence needed** | Hosted run log for the candidate commit; both dial tests green on the hosted Windows lane; the Fyne UI gate green on the candidate; refreshed Code Scanning alert state after the new SARIF upload (until then the dial fix reports `FIXED_LOCALLY_AWAITING_HOSTED_RUN` — never green before the run). |
| **Next action** | Operator: review the candidate diff and `note.md` in the delivery, push the branch, then retrieve the Code Scanning inventory and decide the follow-up. |

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
