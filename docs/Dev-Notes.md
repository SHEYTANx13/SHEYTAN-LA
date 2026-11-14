# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. One such
> authorization exists for the v1.11.1 maintenance cleanup (2026-11-14):
> a one-time content reorganization under the maintenance work order. It
> set no precedent — from the next session onward the human-only rule
> applies again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Current work order

| Field | Value |
|---|---|
| **Owner** | Human developer |
| **Objective** | Publish the v1.11.1 Windows IPC maintenance candidate: review the local branch `maintenance/v1.11.1-windows-ipc` (5 commits on top of `c8fc15e`), decide on push/tag timing, then watch the first hosted run of the upgraded gates. |
| **Priority** | P0 — the Windows production client could not reach the core through the named pipe; every HTTP-backed tab (including Logs) failed together. |
| **Status** | Candidate complete locally; hosted verification NOT RUN (no push was authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted Windows lane green including the new named-pipe CoreClient integration gate and the IPC-aware launch smoke (handshake record + first core read + logging persistence + graceful close). (2) Hosted Linux + audit lanes green, button-fit matrix green. (3) No CodeQL regressions (baseline reported clear). (4) No version change anywhere — internal/config.AppVersion stays 1.11.1. |
| **Dependencies / blockers** | None technical. Publishing requires the operator's push decision. chromedp/cdproto stay on the v0.16.0 + pseudo pairing (breaking v0.23 API rewrite; see ai-notes.md) — plan a dedicated migration session with a Chrome-equipped lane before the next dependency pass. |
| **Evidence needed** | Hosted run log for the candidate commit; makensis /VERSION output (3.13.x) on the Windows lane; the named-pipe integration log `ci-logs/win-ipc-integration.log`. |
| **Next action** | Operator: review the candidate diff and `note.md` in the delivery, then push the branch or request changes. |

---

## Open backlog (prioritized)

1. **chromedp → v0.23.x migration** (P1, technical debt) — one-file-plus-tools
   migration off the pre-generics API; requires a Chrome-equipped hosted
   lane for runtime proof. Until then the pair stays at
   chromedp v0.16.0 + cdproto pseudo (documented exception).
2. **Hosted Windows verification of the v1.11.1 IPC gates** (P0, follows
   the push above) — the new smoke semantics (window + handshake + core
   read + persistence + graceful close) must be observed green once.
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
   [`ROADMAP.md`](ROADMAP.md); Phase 0 (design tokens +
   [`ui-contract.md`](ui-contract.md)) and Phase 1 (composer contract,
   primary action slot, focus return, empty states) are delivered.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
