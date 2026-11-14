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
| **Objective** | Publish the v1.11.1 run-43601915826 repair candidate: review the local branch `maintenance-v1.11.1` (built on the exact baseline commit `cbb07af`), decide on push/tag timing, then watch the first hosted run of the upgraded gates. |
| **Priority** | P0 — the Windows production client could not reach the core through the named pipe; every HTTP-backed tab (including Logs) failed together. |
| **Status** | Candidate complete locally; hosted verification NOT RUN (no push was authorized). Awaiting operator review. |
| **Acceptance criteria** | (1) Hosted Windows lane green including the new named-pipe CoreClient integration gate and the IPC-aware launch smoke (handshake record + first core read + logging persistence + graceful close). (2) Hosted Linux + audit lanes green, button-fit matrix green. (3) No CodeQL regressions (baseline reported clear). (4) No version change anywhere — internal/config.AppVersion stays 1.11.1. |
| **Dependencies / blockers** | None technical. Publishing requires the operator's push decision. chromedp/cdproto are migrated to v0.23.1/v0.160.2 (compile-verified; the browser-tool runtime smoke stays physical class until a Chrome-equipped lane runs it — see ai-notes.md). |
| **Evidence needed** | Hosted run log for the candidate commit; makensis /VERSION output (3.13.x) on the Windows lane; the named-pipe integration log `ci-logs/win-ipc-integration.log`. |
| **Next action** | Operator: review the candidate diff and `note.md` in the delivery, then push the branch or request changes. |

---

## Open backlog (prioritized)

1. **Hosted Windows verification of the v1.11.1 IPC gates** (P0, follows
   the push above) — the new smoke semantics (window + handshake + core
   read + core-close record + JSONL validity + responsiveness soak +
   graceful close) must be observed green once.
2. **chromedp browser-tool runtime smoke** (P1) — the v0.23.1/v0.160.2
   migration is compile-verified; a Chrome-equipped hosted lane must
   exercise the real browser tools once.
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
