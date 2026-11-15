# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. Such one-time
> authorizations exist for the v1.11.1 maintenance cleanups (2026-11-14
> and the run-43630164424 repair, 2026-11-15) and for the single
> run-43718750096 work-order refresh (2026-11-16): content refreshes
> under the active maintenance work order only. They set no precedent —
> from the next session onward the human-only rule applies again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Current work order

| Field | Value |
|---|---|
| **Owner** | Human developer |
| **Objective** | Publish the v1.11.1 run-43718750096 Windows-repair candidate: review the local branch `maintenance/v1.11.1-run43718750096` (built on the exact baseline commit `94a97de`, the failing run's commit and current main HEAD), decide on push timing, then watch the first hosted run. |
| **Priority** | P0 — the run-43718750096 build: exactly one terminal job failed (Windows x64, two failing steps) on four Windows headless test failures — the two named-pipe busy-dial fixture deadlocks (`dial_windows_test.go` readiness wait) and two CRLF-portability release-contract failures (`workflow_dag_contract_test.go` mutation fixtures). The Windows package was blocked by `BLOCKED_BY_UPSTREAM_TEST_FAILURE`, not by a packaging defect. Linux x64, Fyne UI, Core audit and preflight were green; CodeQL run 43718750102 was all-success with one C++ overlay-cache warning. |
| **Status** | Candidate complete locally on Linux (both Windows fixture defects fixed; headless suite 68 pkgs green, race gate 7/7, stress 47/47, collector fixtures 25/25, diagnostics self-test, gofmt/vet/build, module verify + tidy idempotent, Windows headless cross-vet clean, new §7.3 100 MB package-size gate added to both lanes with contract tests); hosted and native-Windows verification NOT RUN (no push authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted Windows headless suite where all four formerly failing tests pass natively (`TestDialContextCallerDeadlineBoundsBusyDial`, `TestDialContextBusyBudgetExhaustsTyped`, `TestOldPreflightDAGShapeIsRejected`, `TestContinueOnErrorIsRejected`). (2) Windows lane produces the portable ZIP + NSIS installer through the new size gates. (3) No version change anywhere — internal/config.AppVersion stays 1.11.1. (4) The independent-gates DAG stays green exactly as in run 43718750096. |
| **Dependencies / blockers** | None technical. Publishing requires the operator's push decision. The two raw run logs (log43718750096.txt, codeql43718750102.txt) were not attached to the repair session — run facts come from the work order's authoritative summary; the GitHub API was rate-limited from that environment. |
| **Evidence needed** | Hosted run log for the candidate commit; the four test names green on the hosted Windows lane; `ci-logs/linux-size-audit.log` + `ci-logs/win-size-audit.log`; the final-verdict step output on any red gate. |
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
