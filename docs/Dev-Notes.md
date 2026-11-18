# Dev Notes — Developer Work-Order Handler

> **Human-owned file.** This is the single central handler for current
> developer intent and active work. AI agents must not edit it unless the
> human explicitly authorizes it in the current prompt. The master
> engineering work order supplied with the 2026-11-17 session ("SHEYTAN
> Local Agent — Master Engineering Work Order", ALPHA, single
> authoritative prompt) is such a one-time authorization: it instructs
> (§2.2) that this file stays the single work-order handler with ONE
> active work order and is updated in place. That authorization sets no
> precedent — from the next session onward the human-only rule applies
> again in full.

*Long-term product direction lives in [`ROADMAP.md`](ROADMAP.md). Release
history lives in [`changelog.md`](changelog.md). The agent operating
procedure lives in [`agent.md`](agent.md). The current AI hand-off lives
in [`ai-notes.md`](ai-notes.md). This file never duplicates them.*

---

## Active work order (ONE, current)

**SHEYTAN Local Agent — Master Engineering Work Order** (ALPHA,
113-minute implementation workflow, single authoritative prompt; the
full text is the authoritative prompt supplied by the human on
2026-11-17 and is preserved verbatim by the human — it is not
reproduced here).

Mandate, in priority order: replace the Fyne primary UI with Go-Gui;
adopt GoGPU where actually supported; keep Wails v3 as an isolated Web
Workspace only; rebuild every Fyne surface at parity; add a safe Git
clone/project workflow; remove context budgeting for ChatGPT-style
continuation; rebuild downloads as a user-controlled resumable queue;
bound all resource-intensive work behind queues and admission control;
harden selected-file security with least privilege; remove installer
production (portable artifacts only); update all documentation and make
ALPHA the only user-facing product label; keep every quality gate
evidence-based — never obtain green by skipping or masking a failure.

Status (2026-11-17, session 1):

- DONE: preflight/evidence baseline; §12.1 ALPHA identity split
  (numeric build identifier stays internal); §10 installer retirement
  with the portable-only package gate; §3.2.1/§3.2.5 structured
  preflight refusal codes (empty model, missing model file, backend,
  resources, capability); docs/catalog updates for the touched
  surfaces.
- BLOCKED IN THIS ENVIRONMENT (Linux sandbox, no GL headers, no sudo,
  no PowerShell, no Windows/macOS/mobile runtimes, no push
  authorization): renderer integration proof execution, the full UI
  migration, hosted CI verification, native runtime evidence, the
  PowerShell fixture suites. These continue in a capable environment on
  the same branch, in the work order's priority order.
- Evidence and the exact blocker list: [`ai-notes.md`](ai-notes.md)
  (current hand-off) and the delivery `note.md`.

Rules that bind every session on this work order: work only on a new
local branch derived from an inspected candidate; no remote writes
(push, PR, release, workflow dispatch) without explicit human
authorization; never mark TEST-READY while a required gate is red or
untested; a build is not a runtime test.
