# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-16, hybrid-native-UI milestone candidate)

The branch `hybrid-native-ui-v1111` (local, unpushed) carries the
run-43862893851 follow-up candidate. Baseline: `0d61aeb` on main — the
run-43862893851 commit AND current main HEAD. That hosted run left
exactly TWO Windows failures (`TestDialContextCallerDeadlineBoundsBusyDial`,
`TestDialContextBusyDialCrossesWaitCap`); this candidate fixes their
production root cause, fixes the core-audit AWK warning at source,
extends the Go-Gui POC with the mandated responsive matrix, adds the
isolated GoGPU POC and the `internal/webapphost` contract, and ships the
hybrid-stack documentation set. Version stays 1.11.1 everywhere.

## What changed in this candidate (the load-bearing facts)

1. **Windows busy dial — terminal identity is now a state decision, not
   scheduler luck.** Root cause of both run-43862893851 failures: the
   exhausted-deadline branch of `dial_windows.go` returned
   `ErrBusyDialBudgetExhausted` without re-examining the caller context.
   With the wait clamp truncating to whole milliseconds, the final
   sub-millisecond of a caller-deadline dial raced the runtime's context
   timer, so both tests (300ms / 2.75s caller deadlines, far under the 10s
   budget) received the budget error where they demand
   `context.DeadlineExceeded`. The branch now attributes deterministically:
   observed context error first (cancellation precedence), then an
   arithmetically-passed caller deadline, and only a still-live context
   returns the typed budget error. No constants changed; both tests'
   assertions untouched; `gofmt`/`GOOS=windows` build/vet/test-compile
   clean; Linux localipc suite green.
2. **Go-Gui POC: 10/10 with the mandated §4.4.5 matrix — and a real
   containment defect fixed.** At 640x480 the `Send` button lost its
   visible area inside the starved splitter pane; fixed in POC code
   (never by weakening the test): below a 900-unit breakpoint the
   workspace renders one content column (mirroring the Fyne shell's
   compact contract). The Fyne-side launch action is STAGED_WITH_GATE —
   the authoring host cannot compile `ui/fyne` (missing CGO headers), and
   no blind edit was made.
3. **GoGPU + WebView layers exist as honest POCs/contracts only.**
   GoGPU: isolated module (gogpu v0.96.0→v0.54.1 pin matrix in the README),
   deterministic LCG spectrum, real software render + bounded Xvfb
   windowed soak; GPU HARDWARE NOT RUN; separate surfaces, no bridge, no
   zero-copy claim. WebView: `internal/webapphost` typed contract with the
   §4.6.6 policy as enforced code (8/8 fail-closed tests); zero adapters
   (all STAGED_WITH_GATE with exact reasons).

## Live architectural constraints (unchanged, load-bearing)

- **One authority per concern.** The Go core owns sessions, runs, models,
  tools, approvals, policy, logging and persistence. Every renderer is a
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

1. Hosted verification of the whole candidate (both dial tests on a real
   Windows lane) — requires the operator's push; nothing is claimed green
   before that run completes.
2. Code Scanning live inventory: BLOCKED from the agent sandbox
   (unauthenticated API 401/403). The operator must retrieve it
   (`security_events` scope, or the `code-scanning-alerts.txt` artifact
   the CodeQL workflow already exports per run) and file the
   two-warning fix as a follow-up work order. This candidate does NOT
   claim those warnings fixed and does not guess their identities.
3. Fyne-side launch action for the Go-Gui workspace: needs a host that
   can compile `ui/fyne` (CGO headers); recorded STAGED_WITH_GATE.
4. Native WebView adapters (all platforms) and the GoGPU hardware path:
   STAGED_WITH_GATE with exact prerequisites (WEBVIEW-CAPABILITY-MATRIX.md,
   GO-GUI-GOGPU-INTEROP.md).

## Next actionable step

Operator: review the delivery (`note.md` beside the source ZIP), push
`hybrid-native-ui-v1111`, watch the hosted run, then retrieve the Code
Scanning inventory and decide the follow-up work order.

---

*Proprietary material under the Proprietary License (see
[`../LICENSE.md`](../LICENSE.md)).*
