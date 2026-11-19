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

Status (2026-11-18, session 3 — run-44306343338 repair):

- DONE on `work/alpha-run443063-repair` (base `7c6678f` = the exact
  commit run 44306343338 tested): the four failed lanes' root causes
  repaired (Android x/tools graph via the x/mobile proxy-verified pin;
  Windows PowerShell $?-vs-LASTEXITCODE contract with a strict-mode
  fixture; Linux/macOS absolute build-info probe paths; the darwin
  local-IPC/Mach-O/RAM/proc/renderer cluster), the duplicate platform
  topology CONSOLIDATED (legacy in-orchestrator Windows/Linux lanes
  retired with byte-exact transfer into the reusable lanes — this
  authorizes the §4.1 no-deletion transition OUT of session 2's state;
  the current work order's §6.2 requires exactly one build path per
  platform), the errors-warnings-only consolidated artifact delivered
  with a 23-case offline fixture suite, and the identity homoglyph
  (U+0422) fixed with a tracked-tree scan tripwire. Full local evidence
  and the honest NOT-DONE list (Go-Gui migration NOT started; hosted CI
  NOT RUN) live in [`ai-notes.md`](ai-notes.md); changelog carries the
  per-item detail. Candidate tip: 9c5d9f6 and later (see worklog).
- EVIDENCE CLASSES recorded per claim; nothing is claimed green from
  code review alone. The five platform lanes remain NOT-RUN hosted
  until the operator authorizes push + run (Gate H).

Status (2026-11-18, session 2 — run-44205355718 CI repair):

- DONE: the RED run's three root causes fixed at main HEAD
  `79b21a4` (ancestry verified: direct child of the older green base
  `f29d759`; the local `work/alpha-transition` f0a03e6 candidate was NOT
  available to this session and nothing was reused from it). A: catalog
  generator made cwd-/staging-independent (`git rev-parse --show-toplevel`
  + `ls-files --cached --others --exclude-standard -z`), coverage test
  universe aligned, new audit-lane freshness gate (regeneration must be a
  byte-level no-op). B: installer-retirement scanner enumerates the
  TRACKED tree via `git ls-files -z` (forward slashes on every OS — the
  Windows self-match died with `filepath.Rel` backslashes) with
  unconditional slash normalization + regression pins; guard intent and
  negative tests preserved. C: new read-only machine-readable
  `build-info` JSON identity probe (product_stage=ALPHA asserted
  separately from build_id=1.11.1), Linux gate rewritten to it, the
  retired semver scrape removed; human `version` output untouched.
  §10: the five platform workflows (build-windows/linux/macos/android/
  ios.yml) delivered as real reusable workflow_call lanes with the
  orchestrator's final verdict requiring and inspecting all of them
  (platform-* jobs; native lanes kept per the §4.1 no-deletion rule);
  machine-readable failure ledger:
  [`diagnostics/run-44205355718-ledger.json`](diagnostics/run-44205355718-ledger.json).
- LOCAL EVIDENCE (linux/amd64, go1.27.4): the exact failed hosted lanes
  re-run green — `go test ./internal/... -tags headless` (all packages),
  release-contract suite, cmd purity suite, stress 47/0 (incl. the
  workflow-contract validators); gofmt clean; catalog regeneration
  deterministic. BLOCKED IN THIS ENVIRONMENT: full Fyne CGO build (no
  GL/wayland dev headers, no sudo), PowerShell/Windows-hosted re-runs,
  mobile toolchains, hosted CI (no push/dispatch authorized).
- NOT DONE (unchanged from session 1, per the work order's priority
  order): the Go-Gui primary-UI migration (all 49 ui/fyne files remain
  the active shell), GoGPU/Wails integration phases, downloads/Git
  clone/continuation/queue/security feature phases. Do not read the five
  new workflow files as Go-Gui gates — they truthfully build the current
  Fyne product and are swapped only when the migration delivers the new
  shell.

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

---

## Active work order status — run 44378562071 (2026-11-19)

The one active work order remains **SHEYTAN-LA ALPHA — Run 44378562071 repair and Go-Gui five-platform delivery**. Status after the 2026-11-19 session on `work/alpha-run443785-repair`: the §4 build/test failure set (module tidy drift, Windows gofmt/toolchain pin, collector false positives, darwin socket path, lease/adoption fail-closed semantics, temp-root residue truthfulness) is repaired and locally verified; the §5 Go-Gui primary-UI migration is NOT STARTED and remains the gating item for TEST-READY ALPHA — Fyne stays the production shell. Full evidence: `docs/worklog.md` 2026-11-19 row, `docs/ai-notes.md` CURRENT HANDOFF, and the delivery `note.md`.
