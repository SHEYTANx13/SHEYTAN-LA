# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-16, v1.11.1 integrity-first milestone candidate)

The branch `work/v1.11.1-integrity-stability-uiux` (local, unpushed)
carries the run-43773885283 follow-up candidate. Baseline: `124b08a` on
main — the run-43773885283 commit AND current main HEAD (the published
run-43718750096 Windows-repair candidate). That hosted run left exactly
ONE failure: `TestDialContextBusyBudgetExhaustsTyped` on Windows. This
candidate fixes the production-dial root cause, delivers recoverable
session deletion, the Go-Gui v0.96.0 POC and the v1.11.1 documentation
set (`docs/architecture/`, `docs/diagnostics/`). Product version stays
1.11.1 everywhere. Full evidence inventory ships in `note.md` beside the
source ZIP.

## What changed in this candidate (the three load-bearing facts)

1. **Windows busy dial — production code fixed, not the fixture.** The
   run-43718750096 fixture repair made the busy condition deterministic,
   which exposed the real defect: `dial_windows.go` exited with the
   generic "no listening pipe instance after a 2000ms busy wait" error
   after ONE per-wait cap (pipeBusyWaitMS=2000) — 2s of the 10s
   pipeBusyDialBudget — so the typed `ErrBusyDialBudgetExhausted` was
   unreachable whenever every wait timed out. An `ERROR_SEM_TIMEOUT` wait
   timeout now continues the documented wait/retry loop (each wait
   clamped to the remaining budget) until the budget or the tighter
   caller deadline is spent; only an unresolved/unexpected wait failure
   surfaces the original busy error. New focused guard:
   `TestDialContextBusyDialCrossesWaitCap` (a caller deadline above one
   cap must cross the first wait timeout and still end at the deadline
   with the typed context error). `GOOS=windows go vet/build` clean;
   Linux localipc suite green.
2. **Session deletion is recoverable now** (`internal/sessions/trash.go`):
   delete moves file + sidecars into `trash/<id>/` with a metadata-only
   tombstone; restore refuses occupied destinations and verifies by
   re-reading; purge is the only permanent destroy and requires the
   trash state (two-step policy); crash windows reconcile additively.
   API: `GET /api/sessions/trash`, `POST .../restore`, `POST .../purge`,
   and DELETE now reports truthful `recoverable`. The pre-existing
   delete-contract tests pass UNCHANGED. Contract:
   [`architecture/CONTENT-INTEGRITY.md`](architecture/CONTENT-INTEGRITY.md).
3. **Go-Gui v0.96.0 POC exists and is isolated**:
   `experiments/ui-engine-poc/go-gui` (nested module, zero production
   dependency — `go list ./...` from root proves it). 5/5 headless
   app-level tests + software-rendered screenshot. POC_ONLY. Decision:
   KEEP_FYNE_FOR_1.11.1 ([`architecture/UI-ENGINE-DECISION.md`](architecture/UI-ENGINE-DECISION.md)).

## Live architectural constraints (unchanged, load-bearing)

- **One authority per concern.** The Go core owns sessions, runs, models,
  tools, approvals, policy, logging and persistence. The Fyne UI is a
  client. Never add a second backend, UI stack or policy implementation.
- **The ONE local transport.** Every in-process client dials through
  `internal/localipc.DialContext` (Windows named pipe / Unix domain
  socket). No TCP fallback, no second dial implementation, no weakened
  DACL/peer-credential/protocol checks.
- **Content mutations flow through the typed store APIs only** — never
  raw model text, arbitrary JSON or shell commands; destructive
  operations are recoverable-by-default with permanent purge as a
  separate explicit step (see CONTENT-INTEGRITY.md).
- **No sleep-based correctness; bounded everything.** Retries are
  deadline-bounded (the Windows busy-dial budget), event-driven waits
  only.
- **Evidence classes are `local-static`, `hosted`, `physical`.** Build
  success and a visible window prove nothing about IPC or runtime
  behavior.
- **Version identity from `internal/config.AppVersion` only.**

## Active blockers

- **Hosted verification of the Windows busy-dial fix, the content-
  integrity suites and the Fyne gate on THIS candidate is pending the
  operator's push.** No green-run claim exists for it. Native Windows
  execution is NOT RUN in this Linux sandbox — the hosted Windows lane
  is the runtime authority (`GOOS=windows go vet`/`go build` of
  `internal/localipc` are the local compile evidence).
- **Code Scanning inventory: BLOCKED.** The unauthenticated GitHub API
  is rate-limited from this sandbox and no token was provided; alert
  count/severities are UNKNOWN — recorded in the delivery's
  `code-scanning-alert-inventory.md` with the exact refusal. Never
  claim a clean alert state.
- **The two research reports were not attached** to this session; the
  traceability matrix follows the work order's §13 enumeration
  (provenance note in
  [`architecture/REQUIREMENTS-TRACEABILITY.md`](architecture/REQUIREMENTS-TRACEABILITY.md)).
- **UI-frame percentiles / typing latency / same-hardware renderer
  comparison: NOT RUN** (no comparable hardware budget); startup phase
  telemetry and core benches exist and are green.

## Local environment note (differs from earlier sessions)

This sandbox DID build the full CGO Fyne tree and run the Fyne UI gate
under Xvfb: GL/X11/wayland dev packages were dpkg-extracted into a local
prefix (`PKG_CONFIG_PATH` + a `wayland.pc` shim for glfw's pre-split
module name — sandbox-only, NOT a repository change). Commands and the
full gate matrix are recorded in `note.md`.

## Next actionable hand-off

1. Operator reviews the candidate diff + `note.md`, pushes, and watches
   the hosted run: `TestDialContextBusyBudgetExhaustsTyped` and
   `TestDialContextBusyDialCrossesWaitCap` must pass natively on the
   Windows lane; `internal/sessions` + `internal/api` green everywhere.
2. Any agent continuing here should re-verify with the smallest ladder
   prefix: `gofmt -l cmd internal ui experiments`, `go build ./...`,
   `go test -tags headless ./internal/...`, then the touched packages
   (`internal/sessions`, `internal/api`, `internal/localipc`), then the
   Fyne gate under Xvfb. Full ladder and evidence-class rules live in
   [`agent.md`](agent.md).
3. On a Windows host, run `go test ./internal/localipc -count=1 -v`
   natively — the two busy-budget tests are the first authority for the
   dial repair.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
