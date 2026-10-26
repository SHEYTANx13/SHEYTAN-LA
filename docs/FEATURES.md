# FEATURES.md — SHEYTAN-LA single feature inventory

This document is the ONE feature inventory: a feature is listed under
"Implemented" only when its source lives in the tracked tree at HEAD AND a
deterministic test or CI gate in this repository evidences it. Documentation
alone never implements a feature; `docs/changelog.md` stays the only release
history and `docs/ROADMAP.md` stays the only direction document. Hardware
claims use five distinct classes everywhere below: hardware **detected**
(a probe found it) ≠ **available** (usable) ≠ **selected** (chosen for a
run) ≠ **executed** (a run actually drove it) ≠ **verified** (a hosted
green gate proves the outcome).

## Implemented features

* Long-horizon goal engine — durable checkpointed UNDERSTAND → PLAN → ACT →
  VERIFY loop with turn budget and bounded replans. Source:
  `internal/goal/runner.go`, `internal/goal/goal.go`. Evidence:
  `internal/goal/goal_test.go`, hosted Go race gate green.
* Goal store durability — atomic file store, reload contract, boot recovery
  (never falsely running). Source: `internal/goal/goal.go`. Evidence:
  `TestCancelIsTerminalAndSurvivesReload`, `TestRecoverOnBootNeverFalselyRunning`.
* Terminal settlement guard — a terminal goal can never be re-classified by
  a late drive settlement (the run-#28 cancel race: cancelled → blocked →
  paused). Source: `internal/goal/goal.go` (MarkStep/SetVerification
  guards), `internal/goal/runner.go` (settledElsewhere + replan guard).
  Evidence: `TestTerminalGoalRefusesLateSettlements`,
  `TestGoalCancelDuringVerifyKeepsCancelledStatus`, and Probe A's red
  (unfixed) → green (fixed) pair in
  `TestGoalRoundTripCancelReplayPackagedProfile`.
* Drive registry — atomic single-flight admission, generation-owned
  cleanup, reservation retention across cancel (I1/I2/I6). Source:
  `internal/api/goal_drive.go`. Evidence:
  `internal/api/goal_drive_lifecycle_test.go`.
* Approval parking — ask-all systems park at the acting boundary with a
  durable pending call; decisions are one-use and bound to the exact
  canonical action identity (I4). Source: `internal/api/goals.go`,
  `internal/goal/goal.go`. Evidence: `TestGoalApproveConsumesGrantExactlyOnce`,
  `TestApprovalIdentityBindsExactAction`, `TestApprovalParkingAndStaleRejection`.
* Frozen AI-System revision binding — goals run against their frozen system
  revision; revised systems fail typed instead of silently substituting.
  Source: `internal/api/goals.go`. Evidence:
  `TestGoalSystemRevisionFrozenContract`.
* Packaged-profile reproduction probes — the exact `fake-remote-engine.py`
  timing (9 SSE chunks × 150ms) replayed through the real server stack for
  the round-trip cancel leg and the two-goal ask-all park. Source:
  `internal/api/goal_drive_lifecycle_test.go`. Evidence: Probe A reproduced
  2/2 then green 2/2 post-fix; Probe B green 2/2 (the Go park machinery
  exonerated at this timing).
* One-Go-UI acceptance contracts — the UI behavioral contracts (zero-session
  send, session-list generation guard, cumulative stream folding, terminal
  settlement, mode separation, history-ref cap, thinking ladder, settings
  IA quarantine rules) pinned as Go tests over a deterministic fake core.
  Source: `ui/fyne/logic_test.go`, `ui/fyne/shell_test.go`. Evidence:
  `go test ./ui/fyne/` green locally; hosted verdict pending push.
* Fyne view-tree composition — every primary view and sub-panel builds a
  non-empty widget tree against a live fake core; navigation switches
  views; the responsive layout flips rail/compact at the breakpoint; empty
  API data never panics. Source: `ui/fyne/shell_test.go`. Evidence:
  `go test ./ui/fyne/` green locally (Fyne test harness); hosted verdict
  pending push.
* CI visibility system — errors-only failure summaries with the §9 severity
  contract (assertions/gates → ::error; QWARN/toolchain → ::warning;
  goal/chat-persist/captures-empty context → ::notice), dedup, caps,
  redaction, escaping, truncation and one per-step accounting line; the
  empty-captures line is classified per C1 (Qt Test docs: `-o file,txt`
  routes ALL QtTest output to the report file). Source:
  `.github/workflows/build-desktop.yml` (both failure-summary steps).
  Evidence: local classifier/dedup fixtures against the extracted step body
  (30/30); hosted verdict NOT RUN (no push authority).
* Windows/Linux packaging and installer chain — portable ZIPs, NSIS
  installer, release-publish gate on tags only. Source:
  `.github/workflows/build-desktop.yml`. Evidence: hosted runs through
  v1.10.12 (run 39024607296 proved the Windows chain through step 21).

## Coming features

Only items `docs/ROADMAP.md` tracks at HEAD, in its wording and order, with
its evidence labels.

D-planned (v1.9 boundary): automatic sub-agent fan-out FROM the goal runner
(the delegation engine is implemented; the goal loop does not yet decompose
steps into subtasks automatically); protected-evaluation anti-hack guard
(spec §12) — designed, opt-in and context-aware when it lands; MCP runtime
wiring (client implemented and tested, not yet registered into the tool
registry); oversized-output spool handles; native-engine forward-pass
arithmetic beyond v1.9.2 (SIMD batched matvec/GEMM — evidence-gated).

FUTURE — NOT IMPLEMENTED (v1.10 Universal Agent): controlled computer-use
layer (browser/desktop interaction, screen, mouse/keyboard, application
discovery, document workflows, multimodal execution, connectors, voice
foundation) through explicit permission/trust boundaries; bounded parallel
tool orchestration; tool-call state hashing for cycle detection; bounded
reflection/recovery and pause/cancel propagation; artifact/handle workflows.

FUTURE — NOT IMPLEMENTED (v1.11 AI Learning & Evaluation): evaluation
suites, baselines, experiments, candidate variants, feedback learning, skill
evolution, memory optimization, routing/context optimization,
promotion/rollback decided ONLY by measured evidence; the protected-
evaluation guard is a prerequisite.

FUTURE — NOT IMPLEMENTED (v1.12 Advanced Model Intelligence): multi-model
routing, capability discovery, specialist models, vision/reasoning routing,
draft/speculative models, dynamic loading/unloading, model pools,
cross-model fallback; native-engine optimization only with parity tests.

FUTURE (v2.0 platform end state): durable AI systems users create, run,
understand, modify and continuously improve; scalable local workers, mature
multi-run orchestration, portable import/export/versioning.

v1.11.0 one-Go-UI boundary (honest): the retired Qt/QML host and the
React web frontend are removed; every shipped UI surface renders in the
Fyne application over the same IPC seam. The former Phase-2 gap list
(AI Systems editing/import, Goals,
Coding Lab, Net Search page, repository index, tasks/automations,
downloads/diagnostics panels remain on the documented headless/browser mode.

## Verification map and maintenance rule

* Go evidence: `go test -race ./internal/api ./internal/goal`, the broad
  `go test ./internal/...` battery, `go vet ./...`, `gofmt -l` — all green
  at this HEAD. CI evidence: `.github/workflows/build-desktop.yml` gates;
  the annotations and accounting line are the anonymous run-page contract.
* Hardware claims: the engine pipeline reports detected/available, the
  config selects, a run executes, and only a hosted green gate verifies.
* Maintenance rule: a line moves from "Coming" to "Implemented" only when
  the source AND the test/CI evidence exist at HEAD; this file is never a
  second roadmap and never carries release history.
