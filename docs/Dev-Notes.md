# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. Such one-time
> authorizations exist for the v1.11.1 maintenance cleanups (2026-11-14
> and the run-43630164424 repair, 2026-11-15): content refreshes under
> the active maintenance work order only. They set no precedent — from
> the next session onward the human-only rule applies again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Current work order

| Field | Value |
|---|---|
| **Owner** | Human developer |
| **Objective** | Publish the v1.11.1 run-43630164424 CI-resilience repair candidate: review the local branch `maintenance/v1.11.1-ci-resilience` (built on the exact baseline commit `2bc66e5`, the red run's commit and current main HEAD), decide on push/tag timing, then watch the first hosted run of the independent-gates pipeline. |
| **Priority** | P0 — the run-43630164424 red build: one `go vet` failure (unused `sh` in `ui/fyne/button_fit_test.go:234`) skipped ALL four independent validation gates (Core audit, Fyne UI, Windows x64, Linux x64), and the same defect killed CodeQL's Analyze (go) job (run 43630164359) before its database init. |
| **Status** | Candidate complete locally (vet/build/gofmt, full headless suite 68 pkgs, full desktop sweep 72 pkgs, race gate 7/7, stress 47/47, collector fixtures 25/25, diagnostics self-test, module verify + tidy idempotent); hosted verification NOT RUN (no push authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted build-desktop run where a red preflight still leaves audit/ui/windows/linux running and the final-verdict job decides the overall conclusion. (2) Windows lane green including the named-pipe CoreClient integration gate and the IPC-aware launch smoke. (3) CodeQL run where Analyze (go) passes vet and reaches init/analyze; a failed manual build must still yield analysis + SARIF upload. (4) No version change anywhere — internal/config.AppVersion stays 1.11.1. |
| **Dependencies / blockers** | None technical. Publishing requires the operator's push decision. The linked CodeQL run 43630164359 is confirmed FAILURE (Analyze (go) exit 1 on the same unused-`sh` defect; other language jobs and the alert export completed) — its exact alert counts were NOT retrievable from this environment (API rate-limited; run link recorded in note.md). |
| **Evidence needed** | Hosted run log for the candidate commit; the final-verdict step output on any red gate; makensis /VERSION output (3.13.x) on the Windows lane; the named-pipe integration log `ci-logs/win-ipc-integration.log`. |
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
