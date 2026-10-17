# changelog.md — SHEYTAN-Local-Agent Release History

**This file is the ONLY canonical release-history artifact.** The README
describes the current product only; per-release evidence lives in the
release tags, the tests that shipped with them, and `UPDATE.md` (current
maintenance behavior).

Every release was shipped with the same invariants: one authority per
concern, evidence over confidence, no sleep-based correctness, honest
hardware claims, the codename gate enabled.

---

## v1.10.10 — 2026-10-17 — the instance-invariant fix + the client pipe-busy contract: the named-pipe race repaired on BOTH sides of the contract, pinned under parallel load, and the operator's docs order executed (stale version lines removed, pre-1.9.x history condensed)

Focus: consume the run 38919925013 evidence boundary (v1.10.9, commit
0571b49) and fix exactly the one named Windows failure, while executing
the operator's docs order. That run was the LINUX MILESTONE: the Linux
x64 job went FULLY GREEN end-to-end for the first time in the
repository's history — the core booted hosted for the first time (the
CORE_BIN canonicalization worked), the real smoke gates G1–G5 passed,
and the entire chain behind them passed: Qt live-answer acceptance,
headless surface runtime smoke, packaging, ZIP verification, artifact
upload. Linux is DONE — a protected surface. Windows failed at exactly
one named place: step 14 "Go verification", the FULL go test suite, one
test, with the v1.10.9 emitter upgrade carrying the failing assertion
anonymously for the first time:

```text
win-go-test-full:     serve_test.go:287: health after garbage: Get "http://localipc/api/health": All pipe instances are busy.
win-go-test-full: --- FAIL: TestServeMalformedRequestIsRejectedNotCrashing (0.42s)
win-go-test-full: FAIL  github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc      12.487s
```

The asymmetry (both facts from the same run): the SAME package passed
the headless run and failed only under the full suite's parallel package
load — the failure is load-dependent. `All pipe instances are busy` is
ERROR_PIPE_BUSY: the pipe exists, but ZERO instances are listening at
the instant of the test's immediate redial. A production client issuing
rapid successive requests through the same window hits it identically —
real product behavior, not a test artifact.

1. **P0 — the root cause (W-RC): the "instance ahead of demand" guarantee
   was enforced in USERSPACE only.** `takeInstance` hands the spare
   instance to the accept loop; the refill (`preCreate`) ran only AFTER
   the server observed the overlapped connect completion
   (`GetOverlappedResult`). But the KERNEL marks an instance busy at the
   FIRST client's `CreateFile` — before the server goroutine has noticed
   anything. The window: [kernel connect → zero listening instances] →
   [event fires] → [goroutine wakeup] → [refill]. Under full-suite load
   the test goroutine's write+close+redial (microseconds) beats the
   server goroutine's refill; the redial's `CreateFile` finds zero
   listening instances. The v1.10.9 vanished-peer fix
   (`isVanishedPeer`: `ERROR_NO_DATA`/`ERROR_BROKEN_PIPE`/
   `ERROR_PIPE_NOT_CONNECTED` transient) repaired a different, real
   accept-path window — it stays, untouched; this release closes the
   remaining structural window.

2. **P0 — W-INV, the server side of the fix: the instance inventory
   invariant is now STRUCTURAL.** When `takeInstance` hands out the
   spare instance, the replacement instance is created in the SAME
   locked section that governs the inventory — at least one listening
   instance exists at EVERY instant from Serve's return until Close. The
   invariant holds because an instance created by `CreateNamedPipe`
   stays listening until a client connects to it, even while the accept
   loop holds the handed-out one (kernel FIFO semantics verified: the
   client-open scan takes the OLDEST listening instance, so the
   pending-connect instance always pairs first; a client landing on the
   spare is served by the very next Accept through the existing
   `ERROR_PIPE_CONNECTED` path). The post-completion refill is REMOVED —
   a refill that waits for the connect completion leaves exactly the
   kernel-level zero-instance window run 38919925013 exposed. Nothing is
   created after Close (the endpoint still vanishes with the last
   handle), and a refill failure defers to the next Accept exactly as
   before. The v1.10.3–v1.10.9 transport gains (W1–W4, W-C,
   isVanishedPeer, the Close-timer stop) are preserved untouched.

3. **P0 — W-CLI, the client side of the same contract: `dialEndpoint`
   now honors the documented ERROR_PIPE_BUSY sequence.** Verified
   against current Microsoft documentation (Named Pipe Client): "If the
   pipe exists but all of its instances are busy, CreateFile returns
   INVALID_HANDLE_VALUE and the GetLastError function returns
   ERROR_PIPE_BUSY. When this happens, the named pipe client uses the
   WaitNamedPipe function to wait for an instance of the named pipe to
   become available." On ERROR_PIPE_BUSY the dial now calls `WaitNamedPipe`
   (the OS's own readiness notification — event-driven, never a
   sleep-based poll; bound directly from winbase since x/sys does not
   expose it) with a bounded 2-second timeout, then retries `CreateFile`
   a small bounded count (3 retries); exhaustion returns the real error.
   This is the production dial path (the real UI's transport behind
   `http.Client`), not a test accommodation: a UI issuing rapid
   successive requests through the same endpoint benefits identically.

4. **P0 — W-PIN, the regression pins.** Two Windows-gated tests in the
   localipc package: (a) a STRUCTURAL pin — `takeInstance` must leave
   the replacement in the inventory under the same lock, inspected
   deterministically (not at the mercy of scheduling); (b) a LOAD pin —
   the exact W-A shape (dial, write garbage, close WITHOUT reading,
   IMMEDIATE redial for health) hammered by 8 parallel goroutines × 4
   cycles with keep-alives disabled (every request a TRUE fresh dial),
   asserting the endpoint never surfaces ERROR_PIPE_BUSY across the
   burst and every health redial answers 200. The existing
   malformed-garbage test is NOT modified — no assertion weakened, no
   timeout expanded, no skip added. Local proof for the Windows
   transport is compile-level only (GOOS=windows go build + go vet);
   the hosted Windows job is the runtime authority.

5. **D1 — the operator-ordered stale version lines removed.** README.md
   no longer carries a "Current release" line (the identity block stays
   version-free: Application / Executable / AppUserModelID / Branch);
   worklog.md's header no longer carries one either and points to this
   file as the canonical version authority. Pin-proof verified before
   removal (rule-8 sweep recorded in the v1.10.10 worklog entry): the
   release-metadata gate's surfaces are exactly package.json,
   internal/config/config.go, build/config.yml, SIGNATURE and
   .github/workflows/build-desktop.yml — no gate references README or
   worklog version content.

6. **D2 — the operator-ordered pre-1.9.x history condensation.** The
   nine FULL v1.8.0–v1.8.8 sections of this file collapse into one
   `## v1.8.0 → v1.8.8 (compact)` section (the v1.7.x precedent), each
   version keeping its one-line essence and key invariants; v1.9.0 and
   later stay detailed. worklog.md's full pre-1.9.x session writeups
   (v1.8.0, v1.8.1, v1.8.3) collapse to compact summaries; v1.9.2+
   sessions stay. UPDATE.md's superseded v1.10.0/v1.10.1 notes condense;
   "What v1.10.x changes" from v1.10.2 onward stays. Every run URL and
   every quoted evidence line is preserved.

Hosted status at ship time (stated honestly): audit and Linux x64 are
fully green (run 38919925013); the Windows fix is pinned by
compile-level local proof and the two new tests, with its hosted proof
pending the first run of this revision; Windows steps 16–32 and the
publish job (never executed in ANY run) remain first-time surfaces to be
consumed as evidence-driven fix-forward. The complete source ZIP is the
deliverable; the operator pushes, the workflow's own publish job is the
pipeline's end.

---

## v1.10.9 — 2026-10-16 — the smoke path canonicalization + the evidence-named Windows transport repair: the core boots past the work-directory cd for the first time in any hosted smoke run, the vanished-peer completion family stops killing the IPC endpoint, the stale deadline-timer leak is closed, and the Windows failure channel carries assertion statements anonymously

Focus: consume the run 38871731277 evidence boundary (v1.10.8, commit
a1041fe) and fix exactly what its annotations name. That run was the
breakthrough: the Windows gofmt gate passed for the first time (the LF pin
worked), the Windows go test suite ran hosted for the first time since
v1.10.1 with exactly ONE failure, the Linux Browser E2E passed, and the
stress suite passed on its first hosted run at the new shape. Two failures
remained, both with published anonymous evidence: Windows step 14 —
`--- FAIL: TestServeMalformedRequestIsRejectedNotCrashing (0.06s)` in
`internal/localipc` (artifact `windows-go-verification-logs-attempt1`) — and
Linux step 24, where the Qt smoke's core never started:
`SMOKE FAIL: core died at boot` with the decisive bash line
`bash: dist/linux/app/SHEYTAN-Local-Agent: No such file or directory`
(artifact `linux-verification-logs-attempt1`). The smoke's real gates G1–G5
had never executed hosted; the core had never booted in any hosted smoke
run.

**Evidence boundary (recorded before any fix was chosen).** The operator
paste of `windows-go-verification-logs-attempt1` (the exact failing
assertion line) was NOT available this cycle, and the anonymous annotations
API fetch for run 38871731277 was rate-limited from the working IP. Per the
cycle rule, the Windows fix selection proceeded from the register's ranked
candidates labeled **INFERENCE** (C1 primary); the hosted run is the only
runtime authority, and V-A.2 below guarantees the next red publishes its
assertion statement anonymously.

**L-A — the smoke path defect, fixed at the root.** The workflow passes
`CORE_BIN` as a REPO-ROOT-RELATIVE path (build-desktop.yml:
`CORE_BIN="dist/linux/app/${LINUX_PKG_ROOT}/SHEYTAN-Local-Agent"`); the
script's validation and the workflow's own `test -x` ran from the repo root
and passed, then the script `cd`s into its work directory and executed the
now-dangling relative path — bash ENOENT, instant child death, "core died
at boot". The repair canonicalizes `CORE_BIN` (and `UI_BIN` defensively) to
absolute paths inside `scripts/qt-desktop-smoke.sh` at resolution time,
BEFORE any cd, so the fallback-build check, the pkill patterns, and the
bottom exec all reference the same file regardless of working-directory
changes; the workflow may keep passing the relative path, the script is
immune. Proof: a two-line local reproduction (relative + cd + exec →
`No such file or directory`, exit 127; canonicalized → exec succeeds), a
tree-wide sweep (the smoke is the ONLY relative-path-after-cd core-exec
site; the workflow's other consumers already use `Resolve-Path`/`$(pwd)/`),
and a full workflow-shaped local smoke run (relative CORE_BIN exactly as
the workflow passes it) in which the core BOOTED past the cd —
`core endpoint: /tmp/…/core.sock` in core.log — for the first time this
script has run anywhere with the core up under a work-directory cd. The
local run was honestly bounded at G2 (the Qt 6.8.2 toolchain, openbox and
xdotool are absent in the Linux working environment; the core's own `none`
sentinel stood in for the Qt host, so G2 failed exactly as designed and
G1/G3/G4/G5 remain host-unproven until the run).

**W-A — the Windows transport, fixed at the evidence-named root cause
(INFERENCE: C1).** The malformed-garbage test is the only hosted test whose
client closes WITHOUT reading (dial → SetDeadline(5s) → write
`NOT-HTTP AT ALL\r\n\r\n` → Close → redial → GET /api/health). The accept
path classified ONLY `ERROR_NO_DATA` as the transient client-gone race; a
client that connects and dies before the overlapped `ConnectNamedPipe`
resolves can surface `ERROR_BROKEN_PIPE` or `ERROR_PIPE_NOT_CONNECTED`
from the `GetOverlappedResult` completion instead — that family propagated
to `return nil, err`, which ended `Accept`, ended `http.Serve`, and tore
down the whole endpoint, making the test's instant health redial fail
(the 0.06s fast-fail). `Accept` now classifies the whole vanished-peer
family as transient via `isVanishedPeer` — recreate the instance, keep
serving — while a genuine endpoint fault stays fatal
(`ERROR_OPERATION_ABORTED` outside the W3 close path, and everything else).
The malformed-garbage contract (garbage must not kill the endpoint) is the
product requirement; the transport serves it, not the reverse. No
assertion was weakened; the test is untouched.

**W-B — the stale deadline-timer leak, closed regardless of W-A.**
`pipeConn.Close` stopped no timer while `SetReadDeadline` armed
`time.AfterFunc(..., CancelIoEx(handle, nil))` for future deadlines — a
stale timer could fire on a RECYCLED handle value seconds later and abort
an unrelated live connection's in-flight IO. `Close` now stops the timer
UNDER the deadline mutex and clears the deadline, which also disarms an
already-running callback's armed re-check (Stop() is best-effort by Go
contract). Two regression pins were added to the Windows test file
(`TestAcceptVanishedPeerClassification`,
`TestPipeConnCloseStopsReadDeadlineTimer`); the Windows transport remains
compile-verified locally (Linux-only environment) and runtime-verified by
the hosted Windows job.

**V-A.2 — the Windows failure channel now carries statements.** The "Go
verification failure summary (Windows)" filters matched only
`^(--- FAIL|FAIL|panic:|fatal error:)`, so run 38871731277's annotations
named the failing TEST but not its failing LINE. Both filters now also
match the Go assertion class (`\.go:[0-9]+: ` — `serve_test.go:287: …`
statements and `file.go:NN:` compile errors), capped at the existing
10-annotation budget with the existing 250-char truncation and the same
no-secrets rule. The next red Windows run is fully self-describing,
anonymously.

**Preserved untouched (protected v1.10.3–v1.10.8 gains):** the transport
repair W1–W4 + W-C, the LF pin and `.gitattributes` policy, both gofmt
gates, the E2E build path and its failure summary, the stress gate, the
reshape, the Wails absence. Local battery this cycle: gofmt clean; go vet
clean; `go test ./...` 65 packages green; `go test ./internal/... -tags
headless` green with the malformed and oversized localipc tests UNCHANGED
on Unix; `-race` green on the six concurrency-heavy packages; stress 47
pass / 0 fail / 0 hangs / 0 crashes; GOOS=windows build + vet clean;
npm ci / typecheck / lint (0 findings) / 236 unit tests / 38 release
tests / build / verify:web green; `verify-all.mjs` 10 PASS (native CTest
+ Qt host NOT RUN, environment-limited); audit shape check mirrored
locally and green.

**Hosted boundary (do not overstate):** this cycle prepares the commit;
the OPERATOR pushes to main. The next hosted run is the FIRST-EVER hosted
execution of: the smoke's real gates G1–G5 (the core finally boots), the
Windows post-verification chain (steps 16–32), the Linux
acceptance/packaging chain (steps 26–33), and the publish job (never
executed in any run). Nothing about those surfaces is predicted or
claimed; fix forward ONLY on what that run's annotations and summaries
name.

## v1.10.8 — 2026-10-16 — the line-ending policy + E2E path repair + CI Visibility 3.0: the Windows gofmt gate observes the truth (the CRLF checkout class fixed by policy, the gate byte-identical), the Browser E2E builds again at the reshaped layout, the last unseen gate joins the visibility system, the smoke summary learns never-started vs died-at-gate, and the reshape's missed-reference class is closed

Focus: consume the run 38823624040 evidence boundary (v1.10.7, commit
22049b9) and make the hosted pipeline REACHABLE to its own end. That run
was the best-instrumented yet — the Visibility 2.0 annotations worked and
both failures are root-caused with primary evidence: Windows x64 failed
step 13 "Go verification" at the gofmt gate, and the gate was RIGHT (the
committed tree at 22049b9 is gofmt-clean byte-for-byte — Linux's identical
gate is green on the same commit); Linux x64 failed step 17 "Browser E2E"
for one deterministic reason: `package.json`'s `build:e2e` still built the
repository ROOT package, which the v1.10.7 reshape emptied of Go files.
Both fixes are small, exact, and proven by local reproduction.

**W-A — the line-ending policy: fix the environment, never the gate.**
The Windows checkout was wrong, not the gate: windows-latest runners
default to `core.autocrlf=true`, the repository had no `.gitattributes`,
so the checkout materialized every `.go` file as CRLF while `gofmt`
normalizes to LF — the gate listed the whole tree (the eight annotated
cmd/ files are exactly the first eight alphabetically; a listing
artifact, not eight violations). The repair is policy-only:
(1) a new scoped `.gitattributes` — `*.go text eol=lf` (every tracked
.go file is already `i/lf` in the index, verified via
`git ls-files --eol`, so the pin causes zero renormalization; it only
guarantees the working tree gofmt observes is the same LF tree on every
platform), plus one justified additional line `*.bat text eol=crlf`
(the launcher's `goto`-label control flow needs CRLF, and the new
Windows checkout pin would otherwise have silently changed its
historical CRLF materialization to LF); (2) the Windows job gains a
pre-Checkout step pinning `git config --global core.autocrlf false`
(git exists on the runner image pre-checkout), with its own assertion
that the pin took. The gofmt gate itself is byte-for-byte untouched.
Local proof of the mechanism: `gofmt -l .` is empty on the LF tree;
injecting CRLF into a working-tree file makes `gofmt -l .` name exactly
that file (the Windows failure mode reproduced); restoring the file
returns the tree to clean; `git check-attr text eol` reports the pin on
cmd/root.go, cmd/ask.go (lf) and packaging/SHEYTAN-LA.bat (crlf).

**E-A — the Browser E2E builds again: `build:e2e` targets the real core
entrypoint.** `package.json`'s `build:e2e` ran
`go build -tags headless -o e2e/.build/sheytan-e2e-server .` — the root
package no longer exists after the reshape; reproduction (BEFORE the
fix, exact command from the repository root): exit code 1, stderr
"no Go files in /home/z/sheytan-clone". The build target is now
`./cmd/sheytan-local-agent`. Reproduction AFTER the fix: the exact
command exits 0 and produces the 23 MB headless server; the FULL
`npm run build:e2e` exits 0; and — with the native C++ engine built
locally (CMake, 13/13 CTest PASS), the fixture model generated, and
Playwright chromium provisioned — the complete `npm run test:e2e` suite
passes 40/40 specs (4.4 min) against the real stack: real built
frontend, real headless server from the new entrypoint, real HTTP API +
WebSocket, real engine generation, real session lifecycle. No second
defect surfaced behind the build path.

**V-B — CI Visibility 3.0: the last unseen gate joins the system.** The
Browser E2E step failed in run 38823624040 as a bare exit code 1 — no
tee, no ci-logs capture, no failure summary, no annotations. The E2E
step now tees `build:e2e` to `ci-logs/e2e-build.log` and the Playwright
run to `ci-logs/e2e-playwright.log` (pipefail discipline on both — a
failing tee'd command still fails the step), and a new
"E2E failure summary (Linux)" step (if:failure()) appends the failing
spec names, the playwright verdict line and 200-line log tails to
`$GITHUB_STEP_SUMMARY` and emits capped `::error::` annotations (the
Windows Go verification summary pattern; the annotation counter lives
in the parent shell via process substitution so the 10-per-step cap
holds; no secrets — rule 7). The existing Upload verification logs step
carries both new logs. A missing Playwright log is reported as "failed
BEFORE Playwright ran — the build:e2e stage is the failing gate"
instead of implying the tests ran.

**V-A — the smoke summary learns never-started vs died-at-gate.** In
run 38823624040 the "Qt smoke failure summary (Linux)" step would have
emitted "failed before a gate marker was written" even though the smoke
NEVER STARTED (the job died at E2E; the smoke step was skipped) — a
false lead sending the next cycle to debug a smoke that never ran. When
the smoke's work directory does not exist, the summary now emits
`::error::The Qt smoke never started — the failure is upstream of the
smoke`, appends the same statement to the job summary, and skips the
gate-marker and log-tail logic entirely. When the directory DOES exist,
the previous behavior (named gate marker + ui.log/core.log tails) is
byte-for-byte unchanged.

**R-A — the reshape's missed-reference class is closed with a sweep
proof.** The known residue — `scripts/verify-all.mjs`'s Qt acceptance
block — expected the core at the repository root and taught the dead
command `go build -o sheytan .` in its skip hint. The block now builds
the core EXPLICITLY from its real entrypoint
(`go build -o sheytan ./cmd/sheytan-local-agent`, a new gated runner
step), points `SHEYTAN_CORE_BIN` at the produced binary, and the hint
teaches the working command; `/sheytan` joins .gitignore (anchored,
exact filename — cannot swallow source; the releasegate test still
passes). Tree sweep for other stale root-package build/run references
(`rg 'go build -o sheytan \.|go build \. |go run \.'`): the ONLY hit is
the repaired block — no other reference to the emptied root exists
(the workflow's build steps, the headless smoke, the NSIS staging, the
ZIP-entry checks, the audit shape contract and the stress release
surface already pointed at the new layout — v1.10.7's R-B confirmed
sound, not re-done). Local proof: the repaired command builds, the
binary reports `SHEYTAN-Local-Agent v1.10.8`, and the full
`node scripts/verify-all.mjs` passes end-to-end.

**Hosted truth boundary.** This environment cannot push (no
credentials). The commit is prepared for the OPERATOR to push to main.
The next hosted run is the FIRST-EVER hosted execution of: the repaired
Windows transport (W1–W4 + W-C, in since v1.10.6/v1.10.7 — the Windows
go test suite has still never run hosted since v1.10.1), the line-ending
policy at the Windows checkout, the E2E at the new layout, and the
reshaped stress gate. No hosted green is claimed for v1.10.8 anywhere
in this repository; v1.10.8's own hosted result is recorded only after
the operator's push executes the run. The gate table and per-item
register outcomes live in `docs/worklog.md`.

Preserved gains: the pipe transport repair (W1–W4, W-C), the visibility
install (2.0 → 3.0), the gofmt gates on both platforms, the repository
reshape, the Wails absence, the stress-gate contracts — all
byte-for-byte; no test weakened, no gate deleted, no timeout expanded,
no default flipped.

---

## v1.10.7 — 2026-10-16 — the evidence-driven gate repair: the Windows overlapped-IO client contract fixed at its root, the Linux smoke verified green first-hand on the CI Qt version, CI Visibility 2.0 (anonymous machine-readable failure annotations), the gofmt gate on both platforms, and the repository reshaped to the operator's specification

Focus: consume the run 38731387959 evidence boundary honestly and end the
six-run Windows failure. Run
https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38731387959
(v1.10.6, commit 48cee27) was the healthiest run since v1.10.1 — audit
SUCCEEDED (3m7s), the Linux Go verification gate GREEN for the first time,
the stress suite and the Qt host build green — with exactly two named
failures left: Windows x64 step 13 "Go verification" (output line 138, the
SIXTH consecutive run) and Linux x64 step 23 "Qt desktop acceptance smoke"
(output line 55, identical in v1.10.5 and v1.10.6); the publish job never
ran. The run's check-run annotations — read ANONYMOUSLY this cycle,
re-proving the V-A capability — carry bare exit codes only ("Process
completed with exit code 1." on both failing jobs), and the two failure
artifacts (`windows-go-verification-logs-attempt1` 1.53 KB,
`linux-verification-logs-attempt1` 3.11 KB) are auth-walled from a
credential-less environment. The register's boundary rule applied, and this
release converted it into STRONGER evidence than inference: it reproduced
the Linux smoke FIRST-HAND on the same Qt 6.8.2 minor CI pins, and
read-verified the Windows defect in source back to v1.10.1.

**Windows transport — the client handle now satisfies the overlapped-IO
contract (`internal/localipc/listener_windows.go`).** Since v1.10.1
(verified in `git show cb11a23:internal/localipc/listener_windows.go`),
`dialEndpoint` opened the CLIENT pipe handle with `dwFlagsAndAttributes =
0` — a synchronous handle — and handed it to the same `pipeConn` whose
`Read`/`Write` issue overlapped IO (Overlapped structs, events,
`GetOverlappedResult`) on every connection. Issuing overlapped IO on a
handle not opened with `FILE_FLAG_OVERLAPPED` is outside the Windows
contract (MSDN CreateFile: the results are unpredictable); the canonical
pipe client (x/sys winio `DialPipe`) opens with `FILE_FLAG_OVERLAPPED` for
exactly this reason. Every Windows pipe round-trip in the hosted Go
verification job traverses this path (handshake, tunnel, health,
malformed-garbage re-dial, 2 MiB oversized-header write, burst dials,
idle survival); the Linux transport (`net.Listen` UDS) never touches it —
matching the six-run Windows-only red split. The fix is one line at the
root: `windows.FILE_FLAG_OVERLAPPED` on the client `CreateFile`. The
register's W1–W4 repairs are untouched; no assertion weakened, no bound
expanded. Evidence class: read-verified source defect + channel boundary
(the run's failing test names are inside the auth-walled artifacts) —
labeled per the operating rules.

**Windows transport — read deadlines are enforced while a read is
pending.** `SetReadDeadline` only cancelled an in-flight overlapped read
when the deadline was ALREADY expired at set time; a future deadline never
fired during a pending `ReadFile` (the overlapped wait is unbounded), so
`http.Server`'s `ReadHeaderTimeout: 10s` (serve.go) was dead code on the
Windows transport — a silent peer hung a read forever. A future deadline
now arms a bounded `time.AfterFunc` cancel (`CancelIoEx` on the handle),
and an `ERROR_OPERATION_ABORTED` read answers `os.ErrDeadlineExceeded`
when the deadline passed (a genuine close-abort still surfaces as an
error). Evidence class: read-verified contract violation, same file.

**Linux smoke — the xcb platform-plugin dependency closure is pinned, and
the G1–G5 battery is verified green by first-hand execution.** The
first-hand reproduction (Debian trixie, aqt Qt 6.8.2 linux_gcc_64 — the
SAME minor CI pins — Xvfb :77, openbox, xdotool/x11-utils, software
rendering) reproduced the EXACT step-23 line-55 failure shape: a Qt
platform-plugin load failure ("Could not load the Qt platform plugin
xcb ... even though it was found") kills the UI at boot, the core exits
with its child, and the smoke dumps core.log + ui.log before
`SMOKE FAIL: core died at boot` — the v1.10.5/v1.10.6 signature. With the
xcb dependency set present, ALL GATES PASS first-hand: G1 typed keyed
handshake (sheytan-ipc/1), G2 core-spawned Qt host alive, G3 a real
1280x840 visible top-level window under openbox, G4 the session
round-trip over the channel plus channel sockets held, G5 the
WM_DELETE_WINDOW close with clean exit — and again after the repository
reshape, with the smoke's own core fallback building the new
`cmd/sheytan-local-agent/` entry. The v1.10.6 hardening (software
rendering, wrapper log capture, WM readiness poll) is verified sound by
EXECUTION, not only inspection. The workflow's apt step now installs the
complete canonical xcb set (libxcb-cursor0/icccm4/keysyms1/image0/
render-util0/render0/shape0/randr0/shm0/xfixes0/xinerama0/xinput0/xkb1/
sync1/dri3-0, libxkbcommon-x11-0, libxkbcommon0) so the plugin's
dependency closure is deterministic rather than runner-image luck. G1–G5
stay byte-for-byte as strict.

**CI Visibility 2.0 — the next red run is machine-readable without any
login (V-A).** Every failure-summary step now ALSO emits `::error::`
workflow-command annotations, anonymously readable via the check-run
annotations API (verified this cycle by reading run 38731387959's
annotations without credentials): Windows names the failing command and
the first failing test names (`--- FAIL` extraction from each gate's
teed log); Linux names the failing gate — the smoke now writes a
`smoke-fail.txt` marker on EVERY abort path (best-effort, never masking
the real failure) — plus bounded ui.log / core.log tails; the audit job
names the failing gates and first failing test names. Caps respect the
10-annotations-per-step limit, lines truncate at 250 chars, and no
secrets, keys, runIds or sessionIds are emitted (rule 7 — the same lines
already publish to the public step summary). The existing job-summary
and artifact channels are unchanged.

**The gofmt gate — hygiene joins the battery on BOTH platforms (W-B).**
The editing pipeline's tab-to-space mangling was worse than the register
knew: the register named three files (`internal/llm/backend.go`,
`internal/localipc/serve_test.go`,
`internal/localipc/listener_windows_test.go`), and `gofmt -l` proved the
damage tree-wide — 143 files. The whole tree is reformatted
(whitespace-only; both platform builds byte-equivalent), and BOTH Go
verification steps now require `gofmt -l .` to be EMPTY: a violation
fails the job, tees the full offending-file list to its own log
(`win-gofmt.log` / `linux-gofmt.log` — included in the failure summaries
and artifacts), and emits the first files as `::error::` annotations. No
gate weakened; the battery grew.

**Repository shape (R-A/R-B/R-D, operator specification).** All seven
documentation files moved into `docs/` (ARCHITECTURE.md, CONTRIBUTING.md,
ROADMAP.md, UPDATE.md, agent.md, changelog.md, worklog.md); the root
keeps exactly README.md, LICENSE.md and SECURITY.md as visible
documentation. The core entry set (main.go, main_headless.go,
main_other.go, main_stderr.go, main_windows.go) moved into
`cmd/sheytan-local-agent/` with the build-tag suffix contracts preserved
(verified: linux, linux+headless and windows builds all green); the
launcher script source moved to `packaging/SHEYTAN-LA.bat`. Every
reference swept in the same change set: the workflow's Windows build
(SHEYTAN-LA.exe + console smoke probe), Linux build, headless smoke
build, and smoke core-fallback build all target
`./cmd/sheytan-local-agent`; the Windows staging step sources
`docs/agent.md` (→ AI-CONTEXT.md) and `packaging/SHEYTAN-LA.bat`; the
audit job's repository-shape check is now TWO-SIDED (new paths must
exist, old root paths must be gone); the stress gate's release-surface
scenario pins the same two-sided inventory plus the new workflow build
strings; the license classification table (internal/brand/brand.go +
the regenerated LICENSE.md via scripts/gen-license.go) and the license
contract test's allowed-references map follow the new paths; README and
cross-document links updated; the native engine's documentation
references updated. Build-mandated root files stay, justified per-file
in `docs/worklog.md`: go.mod, go.sum, package.json, package-lock.json,
index.html, vite.config.ts, tsconfig.json, tsconfig.app.json,
tsconfig.node.json, SIGNATURE, .gitignore, .oxlintrc.json,
.prettierignore — the Go/npm/Vite/release contracts; moving any of them
breaks the toolchain or forces a config refactor beyond this release.

**Preserved gains (v1.10.3–v1.10.6).** Strict handshake, field pins,
stress-gate Qt contract, WS fragmentation reassembly, the W1–W4 pipe
transport repair, the failure-visibility install and the ws purge all
remain byte-for-byte. No test weakened, no check deleted, no timeout
expanded, no sleep-based correctness, the native-engine default
untouched, Wails still absent (the module-graph gate stays green).

**Local verification battery (this cycle, all green):** gofmt -l empty;
go build ./... + `-tags headless` + GOOS=windows; go vet ./... both
platforms; go test ./internal/... -tags headless; go test ./... -run
Test; go test -race on the audit's six concurrency-heavy packages; npm
ci / typecheck / lint / test:units (236 pass) / test:release / build /
verify-static-assets; scripts/verify-all.mjs full PASS; stress suite
47/47 including the reshaped release_surface; bash -n
qt-desktop-smoke.sh; the Qt desktop acceptance smoke G1–G5 first-hand
PASS (twice: pre-reshape and post-reshape); canonical identity 1.10.7
via release-version.mjs sync + --check. Hosted result: recorded only
after the operator's push executes the run (this environment cannot
push; no hosted green is claimed for v1.10.7 anywhere).

---

## v1.10.6 — 2026-10-15 — the hosted-truth release: the Windows named-pipe IPC defect class fixed in one sweep, the Linux Qt smoke hardened for headless runners, and CI failure-visibility installed so no future red run is invisible

Focus: break the three-run pattern of docs-only fixes. Run 38714950052
(v1.10.5) repeats the identical failure shape: Windows x64 fails at step
13 "Go verification" for the FIFTH consecutive run, and Linux x64 fails
at step 23 "Qt desktop acceptance smoke" — the first hosted execution of
`scripts/qt-desktop-smoke.sh`. v1.10.4 and v1.10.5 contained zero Windows
code; the hosted job logs are admin-gated and no GitHub credentials exist
in the repair environment, so each previous cycle retreated into
documentation. v1.10.6 removes that excuse: every VERIFIED defect class
is fixed in code without needing the log, and CI failure-visibility makes
any future red run publish its own diagnosis.

**Windows named-pipe IPC transport (W1–W4) — the full defect class fixed
in `internal/localipc/listener_windows.go`.** Verified at HEAD
(8d8e660): (W1) `listenEndpoint()` returned with NO pipe instance — the
first `CreateNamedPipe` happened lazily inside `Accept()`, so `Serve()`
returned before the endpoint existed and an immediately dialing client
hit `ERROR_FILE_NOT_FOUND`; (W2) `Accept()` waited on
`ConnectNamedPipe` in a 5000 ms window and returned the timeout as a
FATAL accept error — `http.Serve` exits on accept errors, so the Windows
IPC endpoint DIED after 5 idle seconds; (W3) `Close()` never cancelled a
pending overlapped accept (`CancelIoEx`), so a clean `Stop` could hang on
the accept; (W4) `TestCredAllowedDeniesForeignUid` was vacuous on
Windows (`os.Getuid() == -1` degenerated every assert), the SID identity
path (`verifyClientIsCurrentUser`) had zero test coverage, and the
malformed/oversized tests re-dialed racing the next instance creation.
The repair: `listenEndpoint` creates the FIRST pipe instance
synchronously (Serve returns only when the pipe exists — Unix bind
semantics); `Accept` waits on the overlapped connect INDEFINITELY (a
connect timeout is no longer an error class; `connectTimeoutMS` is gone)
and only `Close` ends the wait, via `CancelIoEx` on the published pending
overlapped state plus the closed-channel signal; a fresh instance is
created AHEAD of demand (immediately after a client connects, before the
SID check and before the connection is handed to `http.Serve`) so re-dials
never race instance creation; `ERROR_NO_DATA` (client vanished mid-race)
is transient, not fatal; the identity decision is extracted into
`sidAllowed` (a pure, fail-closed comparison against the real token SID —
empty/blank client SIDs are denied) with windows-only regression tests
(`internal/localipc/listener_windows_test.go`): own-SID allowed (real
token, no forged identity), foreign SID denied, empty denied, endpoint
survives 6.5 s idle (the exact former fatal window), a zero-delay
10-dial burst never races instance creation, and `Stop` completes while
an accept is pending. `TestCredAllowedDeniesForeignUid` now documents
its platform split honestly (the uid model is genuinely Unix-only; the
Windows equivalent is the SID contract above). The current-user DACL and
the post-connect SID check remain fail-closed and unchanged.

**Windows candidate sweep (W5) — classified by inspection at HEAD, with
the evidence quoted in `worklog.md`:** `net.Listen("unix")` exists only
behind the `PathSeparator` skip guard (serve_test.go); `/proc` readers
are platform-suffixed production files or Skipf/OS-guarded tests
(phase5_bench_test.go, cim_parse_test.go); test-side unix-tool exec is
git-only (present on windows-latest); `internal/sandbox` Job-Object
tests DO NOT EXIST (no test files in the package); the v1.10.0-era
`t.TempDir`/RemoveAll teardown race class in internal/api already
carries the cleanup-LIFO repair (verified ordering at HEAD). CGO_ENABLED=0
(the Windows verification environment) is NOT the cause: the localipc
path is pure `golang.org/x/sys/windows`, no cgo. No W5 fix was fabricated
where no defect reproduces or exists by inspection — the honest classes
are recorded in worklog.md.

**Linux Qt desktop smoke (L1–L3) — hardened for the headless hosted
runner, G1–G5 untouched.** (L1) Qt Quick cannot create a GL scene-graph
context under Xvfb — the classic first-run failure — so the software
renderer is forced for the UI process (`QT_QUICK_BACKEND=software`,
`QT_OPENGL=software`, `LIBGL_ALWAYS_SOFTWARE=1`), exported in both the
workflow smoke step and the script; the UI provably inherits them
(`desktop.Run` passes `os.Environ()` to the Qt child). (L2) the smoke's
own `ui.log` never existed — the product launcher streams the UI's
output into the core's log streams — so every failure path catted an
EMPTY file; the smoke now creates `ui.log` up front and routes the UI's
stdout/stderr into it via a smoke-owned wrapper that execs the REAL UI
binary UNCHANGED (same argv, same env, same PID after `exec` — G2/G3/G4/
G5 still observe the real child), and every failure path (core boot, G2,
G3, G5) now prints real UI evidence. (L3) the openbox readiness `sleep 1`
is replaced by a bounded poll of the root-window
`_NET_SUPPORTING_WM_CHECK` property (the ICCCM WM-ownership contract);
the fallback `sleep` path is explicitly non-authoritative — G3 still
gates real geometry either way. No gate was weakened, no threshold
moved, no timeout expanded: the L-class changes harden the ENVIRONMENT
and the DIAGNOSTICS only (L4 constraint held).

**CI failure-visibility — installed in the same release.** Every
Go-verification command now tees to its own log file: the Windows job
teed separately for `go test ./internal/... -tags headless`,
`go test ./...` and `go vet`; the Linux job tees the same three plus the
race gate in the audit job. On failure, follow-up `if: failure()` steps
append the failing test names (grep of `--- FAIL` / `FAIL` / `panic:`)
and a 200-line tail per failing command to `$GITHUB_STEP_SUMMARY` —
publicly visible without login, which is exactly how a credential-less
agent reads the next failure — and upload the full logs via
`actions/upload-artifact`. The smoke run's failure step appends the last
200 lines of every smoke log (core.log, ui.log, xvfb.log, wm.log,
engine.log) to the summary and uploads them. No IPC keys, capability
tokens, runIds, sessionIds or model content are written to these outputs
(the smoke reads the handshake key from the child environment and never
logs it). The gates are unchanged in strictness: tee is pass-through,
`pipefail` keeps the pipeline failure-true, and the exit-code assertion
discipline of v1.4.0 is preserved.

**Legacy purge — proof before delete.** (P1) the `ws@8.22.0`
devDependency is removed: zero `require("ws")`/`from "ws"` imports
existed anywhere at HEAD (the only `"ws"` hits are a URL-scheme literal
in `src/LogViewer.tsx`, a Go URL prefix in `runregistry_test.go` and a
directory name in `markdown_test.go` — the frontend uses the
browser-native WebSocket); `package-lock.json` regenerated, `npm ci`
clean, 0 vulnerabilities — the GHSA advisory class is permanently dead.
(P2) `e2e/repro-stream.mjs` deleted: the v1.8.2 pure-diagnostics harness
asserted nothing and was referenced by no workflow step, no package.json
script and no `verify-all.mjs` entry (only historical worklog/changelog
mentions remain, which are protected); `e2e/fixtures/sheytan-server.ts`
SURVIVES (shared with the spec files). (P3) the whole-tree Wails sweep
found 21 files: every mention is a gate (the Wails-absence check, the
stress gate's GTK-stack ban), origin-REJECTION security code
(`loopback-origin`, the retired-origin handling in `ws.go`), retired-era
comments, or protected historical doc mentions — nothing outside the
allowed classes, nothing deleted blindly; the one stale architecture
comment (`internal/llm/backend.go`) was corrected to the Qt-client chain.
PROTECTED files all survive: SHEYTAN-LA.bat, build/config.yml,
build/sheytan.ico, gen-syso, packaging/nsis/installer.nsi, src/web/vite,
scripts/sync-web.mjs, verify-all.mjs, gen-license.go, gen-signature.go,
the Wails-absence gate, and the historical mentions.

**Preserved gains:** every v1.10.3/v1.10.4/v1.10.5 surface is intact —
the strict typed handshake, the field pins, the stress-gate Qt contract,
the CoreClient WS fragmentation reassembly, the apt OpenGL dev-stack fix,
and the desktop override order (SHEYTAN_UI_EXECUTABLE wins over
next-to-core).

**Hosted truth (the honesty boundary of this release).** Windows x64
failed step 13 "Go verification" in five consecutive hosted runs
through run 38714950052 (v1.10.5, commit 8d8e660); Linux x64 failed step
23 "Qt desktop acceptance smoke" on the smoke's first hosted execution.
The v1.10.6 repairs are local-evidence: `GOOS=windows` build + vet
clean, the Linux localipc suite unchanged and green, and the defect
classes above fixed by construction — NOT hosted green. The repair
environment has no GitHub credentials, so this release could not push or
execute a hosted run; the failure-visibility install is part of this
release precisely so that the NEXT hosted run — red or green — publishes
its own diagnosis where everyone can read it. No hosted green is claimed
for v1.10.6 anywhere in this repository.


## v1.10.5 — 2026-10-14 — the live-stream + Qt-host-build release: the activity fragmentation defect fixed with byte-level evidence, the Linux Qt host build dependency gap closed

Focus: make the Linux `Build Qt host (Linux)` gate green (run 38584769607
step 22) and fix the one REAL product defect a locally-reproduced Qt
acceptance exposed. No new surface, no default flip, no test weakening.

**Linux `Build Qt host (Linux)` (step 22) — ROOT-CAUSED with local
fail-before/pass-after evidence.** Reproduced with the workflow's EXACT
command (`cmake -S ui/qt -B ui/qt/build -G Ninja -DCMAKE_BUILD_TYPE=Release
-DCMAKE_PREFIX_PATH=$Qt6_DIR` against the workflow's pinned Qt 6.8.2
aqtinstall layout): the configure step failed with
`Could NOT find OpenGL (missing: OPENGL_opengl_LIBRARY OPENGL_glx_LIBRARY
OPENGL_INCLUDE_DIR)` → `WrapOpenGL` NOT FOUND → `Qt6Gui` NOT FOUND. Root
cause: the workflow's `Install Linux build dependencies` step installed
only the GL RUNTIME packages (`libgl1`, `libegl1`) — no OpenGL development
headers and no link-time `.so` symlinks, which CMake's FindOpenGL
requires. Fix: the workflow now installs the glvnd dev stack
(`libgl-dev`, `libglx-dev`, `libopengl-dev`, `libegl-dev`),
`mesa-common-dev` (GL/gl.h, GL/glext.h) and `libxkbcommon-dev` (the
pkg-config module the Qt xcb plugin's build-time lookup needs). The
workflow matches the local reproduction exactly: after adding the same
dev packages, the exact CI configure + build produce `sheytan-ui` AND
`sheytan-ui-acceptance` (exit 0), and the full Qt live-answer acceptance
suite passes against the real core.

**Qt host `CoreClient` activity stream — REAL DEFECT fixed with a
raw-socket probe as evidence.** The acceptance gate
(`chatStreamPersistRepeatAndClose`) failed locally with `liveText()` stuck
empty while the core's run log showed the run settling `outcome=done`
with 20 delta events. A raw RFC6455 probe over the IPC channel proved the
core's activity hub emits `fragment(opcode 0x1, FIN=0, 1024 bytes) +
continuation(opcode 0x0, FIN=1)` for ANY activity event larger than 1024
bytes — gorilla's `WriteBufferSize: 1024` fragments every oversized
WriteMessage. The Qt client's v1.10.3 framing policy REFUSED all
fragmentation (written under the false assumption "the hub emits whole
text messages") and closed the stream mid-run — the live bubble froze on
every real long-context generation, and the browser stack never noticed
(JS clients reassemble per RFC6455 §5.4). Fix: `readWsFrame` now
reassembles fragmented text messages (bounded by the same kMaxWsFrame
contract, stale fragment state cleared on stream close) while protocol
violations — a continuation without a first fragment, an interleaved
message inside an open fragment — still fail LOUDLY with typed errors.
The acceptance contract test was revised from
`wsRefusesFragmentedFrames` to `wsReassemblesFragmentedMessages`: a real
activity event split mid-JSON must reach `liveText()` intact, and an
orphan continuation must still be refused with a typed error. No test
was deleted or weakened; the pinned contract moved to the RFC-correct
behavior the wire actually carries.

**Windows `Go verification` (step 13) — evidence class stated honestly.**
The step log is admin-gated for anonymous API callers (403 "Must have
admin rights"), so the exact failing assertion remains UNOBTAINABLE from
this environment. What IS proven at v1.10.5: `CGO_ENABLED=0
go test ./internal/... -tags headless -count=1` (exit 0, 64 packages),
`CGO_ENABLED=0 go test ./... -run Test -count=1` (exit 0, 65 packages),
`CGO_ENABLED=0 go vet ./...` (exit 0) — the exact step-13 commands —
plus `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (exit 0,
full type-check of every package including test files) and a clean
Windows build of the headless binary. The run history (v1.10.1 through
v1.10.4, ALL four Windows jobs red at step 13 while the SAME commands
pass on Linux CI and locally) and the clean cross-compile prove the
failure class is a Windows-host runtime assertion that cannot be
identified or fixed without the hosted log. Hosted Windows status stays
NOT VERIFIED until a push proves it; nothing speculative was changed on
the Windows path.

---

## v1.10.4 — 2026-10-14 — the reliability release: the stress gate tracks the Qt-era CI contract, the Windows evidence class stated honestly

Focus: turn the two red hosted-CI gates of run 38569081029 (v1.10.3 @
b0b3aba) into an honest state — fix what was provably broken, prove
everything provable, and label exactly what this environment cannot
prove. No new surface, no default flip, no test weakening.

**Linux `Stress suite` (step 18) — FIXED with fail-before/pass-after
evidence.** Root cause: the `release_surface` scenario
(`cmd/stress_release_surface.go` §3f) still REQUIRED the Wails-era
packages `libgtk-4-dev` + `libwebkitgtk-6.0-dev` — the exact stack
v1.10.1 removed from the workflow when the desktop became the native
Qt client. The scenario failed on every v1.10.1+ hosted run (the first
hosted run that executed the stress binary after the Qt migration was
38569081029) while the workflow itself was correct: the classic
"stress scenario assumes a retired transport" regression. The pin now
REQUIRES the Qt-era set the workflow installs (libx11-dev for the
smoke's `cc -lX11` build of closewin.c, the four libxcb runtime
pieces, libxkbcommon-x11-0, openbox as the REAL window manager for the
G3/G5 gates, xvfb, xdotool, x11-utils) and BANS the retired GTK/WebKit
stack in both Wails generations (libgtk-3-dev, libwebkit2gtk-4.1-dev,
libgtk-4-dev, libwebkitgtk-6.0-dev). Coverage strengthened: 11
must-exist pins (was 2), 4 banned packages (was 2). Evidence: the exact
CI command (`go build -trimpath -ldflags="-s -w" -o /tmp/sheytan-stress
./scripts/stress-main && /tmp/sheytan-stress stress`, CGO_ENABLED=1)
failed before the fix (`release_surface FAIL … libgtk-4-dev missing`,
exit 1) and passes after it (47/47, exit 0), with a clean `-race` pass
on the stress-exercised packages (cmd, sessions, memory, sandbox,
tools, agent).

**Windows `Go verification` (step 13) — evidence class recorded, NOT
claimed fixed.** The step's full log is not retrievable from this
environment: the Actions log-download API requires repository-admin
authentication and the unauthenticated API was rate-limited all
session. Everything provable without the log was proven at b0b3aba and
re-proven at v1.10.4: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`
compile AND full test-binary LINK of every package with and without
the headless tag (the exact step-13 build mode — `go vet` type-checks
but never links, so the link check closes the one hole vet leaves);
`GOOS=windows go vet ./...` clean in both tag modes; the IDENTICAL
three step-13 commands pass on Linux CI and locally; and a
package-by-package review of every test file changed since v1.10.1
(internal/api qt field contract, internal/desktop launcher resolution,
internal/perf host classification, internal/localipc endpoint,
internal/engcheck PE sniffing, internal/llm import lock, internal/updater
variant naming, internal/native/engine liveness probe) found no
Windows-runtime defect. The failing assertion therefore cannot be
identified without the hosted log, and v1.10.4 makes NO speculative
Windows change. WINDOWS GO VERIFICATION remains NOT VERIFIED until a
hosted rerun of this commit; the run's own step log is the arbiter.

**Also in v1.10.4:** the architecture doc marks the v1.9.5 loopback
activity transport RETIRED (the Qt desktop mounts no loopback WebSocket
listener — `internal/desktop/desktop.go`) and the Qt/IPC seam SHIPPED
(since v1.10.1); the roadmap names v1.10.4 as the shipped baseline;
the stress-main header comment no longer describes the removed Wails
shell; canonical identity is 1.10.4 on every surface (package.json →
release-version.mjs → config.go / build/config.yml / SIGNATURE).

---

## v1.10.3 — 2026-10-14 — the Qt IPC hardening release: real framing, strict handshake, runnable CI, honest evidence

Focus: close the real defects and untested risk surfaces the v1.10.2
commit audit verified. The Qt CoreClient's HTTP path now completes
responses under real framing (Content-Length honored, close-delimited
still supported, oversized heads/bodies refused deterministically) — the
v1.10.2 client completed a body only when the server closed the channel,
so any response that kept the socket open hung until timeout. The IPC
handshake now requires the protocol NAME and the protocol VERSION
(`sheytan-ipc` version 1) — a core speaking v2 is refused visibly. Every
silent non-200/malformed error
path in the client now surfaces a typed error. The Linux smoke aborts
cleanly on a missing core (the `fail` defined-after-use bug), cleans up
on INT/TERM, derives its stale-process patterns from the run's own
paths, resolves a bare `WINMGR_BIN` through PATH, and REFUSES G3 without
a window manager instead of claiming geometry proof under bare Xvfb. The
two platform CI jobs are runnable again: `libx11-dev` + `openbox` are
installed, the smoke is handed the REAL staged binary paths (the core
name and the never-staged UI_BIN both silently masked by fallbacks
before), and the Windows acceptance-binary staging resolves the MSVC
multi-config `Release/` layout instead of silently no-oppping. The
launcher's explicit `SHEYTAN_UI_EXECUTABLE` override now wins over the
next-to-core packaged UI, pinned by test. The perf flake on hosts
exporting extra CI markers is fixed (membership, not exact count); the
hosted baseline artifact was re-measured with the code SHA stamped and
UPDATE.md now quotes it character for character. Ten new QtTest
framing/contract slots and three Go field-contract pins lock the wire
contract (bare-array sessions, `msgCount`, systems shape) so the next
commit cannot reintroduce the class.

1. **P0 — CoreClient HTTP framing completed under Content-Length and
   Connection: close, with deterministic oversize refusals.** The
   response body now completes the moment Content-Length is satisfied
   (no hang when the server holds the socket open); a declared length
   beyond the 8 MiB body bound is refused before buffering; a completed
   head beyond the 64 KiB bound is refused (the v1.10.2 code only
   checked the incomplete case); close-delimited bodies still complete.
   Proven before/after: on the v1.10.2 client, 9 of the 10 new framing
   slots fail (including a 20 s hang per Content-Length response); on
   v1.10.3 all 14 acceptance slots pass.
2. **P0 — strict IPC handshake (R17).** `connectToCore` requires
   `protocol == "sheytan-ipc"` AND `protocolVersion == 1` before
   `connected` flips; the v1.10.2 happy path checked the name only and
   accepted a sheytan-ipc/2 core. Regression: the new
   `handshakeRejectsWrongProtocolVersion` slot FAILS on the v1.10.2
   client (it accepts v2) and PASSES on v1.10.3.
3. **P1 — every silent client error path now surfaces a typed error.**
   `refreshSystems` non-200 → `systems refresh failed (status N)` (R7);
   `refreshSessions` non-200/malformed → typed errors (the v1.10.2 code
   silently skipped BOTH — which had hidden a real contract drift, see
   item 4); `deleteSession` non-200 → typed error. The `ACTIVE` badge
   no longer keeps layout space when inactive (R9a). The activity
   WebSocket gained an explicit policy: only complete text frames carry
   events, fragmented frames are REFUSED with a typed error (the
   v1.10.2 code silently dropped continuation payloads), ping → masked
   pong, close → masked close reply + stream teardown, and the frame
   loop no longer starves frames queued behind a control frame.
4. **P1 — the sessions wire contract is now parsed as the core serves
   it.** `GET /api/sessions` returns a BARE JSON array whose stubs carry
   `id`, `title` (omitempty) and `msgCount` — the v1.10.2 client
   expected a wrapped `{"sessions":[…]}` envelope and a `messageCount`
   field that never exist on the wire; the silent malformed-skip hid
   both, so the sidebar list never loaded and every session showed
   "0 messages". Client parsing fixed, QML fixed, and three new Go
   field-contract pins (`internal/api/qt_field_contract_test.go`) lock
   the shape so a rename fails a test instead of blanking the UI.
   `activateSystem`/`openSession`/`deleteSession`/the activity WS URL
   now percent-encode id segments defensively (R8; the store's charset
   — `default` / `sys-` + lowercase hex — is verified URL-safe).
5. **P1 — the Linux smoke aborts and cleans correctly (R1/R2), and the
   hosted CI jobs are runnable (R3/R4/R16).** `fail()` is defined
   before its first call site (the v1.10.2 missing-core path printed
   "command not found" and CONTINUED, dying later with a confusing
   empty-log error — reproduced before the fix); INT/TERM now exit with
   signal codes instead of resuming; stale-process pkill patterns are
   derived from `$CORE_BIN`/`$UI_BIN`/`$WORK` instead of hardcoded
   /tmp paths; a bare `WINMGR_BIN` resolves through `command -v`; the
   IPC-key environ read retries bounded; Xvfb/engine/core readiness is
   event-based instead of fixed sleeps; and G3 REFUSES to run without a
   real window manager. The Linux job installs `libx11-dev` (closewin
   builds with `cc -lX11`) and `openbox`, stages `sheytan-ui` to the
   exact `UI_BIN` path it passes, and passes the REAL core name
   (`SHEYTAN-Local-Agent`) to both the smoke and the live-answer
   acceptance; the Windows job resolves the acceptance binary from
   `ui/qt/build/Release/` first and throws when neither path exists
   (never SilentlyContinue).
6. **P1 — launcher override order (R13) and measurement honesty
   (R5/R6/R10).** `uiExecutable` checks the explicit
   `SHEYTAN_UI_EXECUTABLE` override FIRST (the v1.10.2 next-to-core-first
   order silently ignored an explicit override whenever a UI sat beside
   the core); pinned by `TestUIExecutableOverrideWinsOverNextToCore`;
   the redundant `t.Setenv("")`+`os.Unsetenv` pairs are gone.
   `TestDetectHostEvidenceFindsMarkers` asserts membership, not an
   exact count (a host exporting TF_BUILD/JENKINS_URL/TRAVIS/CIRCLECI/
   BUILDKITE failed the v1.10.2 assert spuriously — reproduced with
   TF_BUILD=1 before the fix). The stale `readCodeSHA` ldflags comment
   is corrected. `baselines/perf-hosted-v1.10.2.json` is re-measured
   with `SHEYTAN_PERF_CODE_SHA` stamped to the audited commit and a
   trailing newline; UPDATE.md quotes the artifact character for
   character (the v1.10.2 release notes claimed 1.2 ms / 14.8 MB while
   the committed artifact recorded different numbers and an unknown
   codeSha).
7. **P2 — identity 1.10.3, docs reconciled (R11/R12/R14/R15).**
   Canonical version via `scripts/release-version.mjs` + `--check`
   (config.go, build/config.yml, SIGNATURE repaired; ui/qt/CMakeLists.txt
   project VERSION bumped); no stale "1.10.0/1.10.2 current" claims
   outside changelog history. The "smallest compatible fix" ws claim is
   corrected in place (upstream fixed in 8.20.1/8.21.0; 8.22.0 chosen;
   dependency unchanged). The Go space-indent convention is recorded in
   agent.md (no mass reformat; CI does not gate gofmt). The agent.md
   "CI calls verify-all.mjs" claim is corrected to match the workflow
   (the CI gates are inline; verify-all.mjs is the LOCAL canonical
   entrypoint). worklog.md carries the per-item fixed/skipped/not-run
   evidence.

---

## v1.10.2 — 2026-10-13 — the Windows-gate compile repair, the smoke that owns its environment, the AI Systems Qt surface, and fail-closed measurement

Focus: harden the v1.10.1 Qt + localipc architecture for real — repair
the Windows Go verification gate the full-suite compile exposes, make
the Linux desktop smoke own every prerequisite it needs (including its
window manager), migrate the highest-value remaining surface that can
meet the evidence bar (AI Systems list/activation) as a pure IPC
client, close the fail-open hole in measurement host-classification,
and reconcile every document to the 1.10.2 identity. Honest boundaries
unchanged: the native C++ engine default stays evidence-gated (the
physical-Windows Phase 1 baseline remains NOT MEASURED) and the
remaining Qt surfaces are not claimed done.

1. **P0 — `internal/localipc` contract tests now compile on Windows.**
   The test-only `credAllowed` helper lived behind the `!windows` build
   tag (`listener_unix.go`) while `serve_test.go` used it
   unconditionally, so `go vet` / `go test ./...` on Windows could not
   even build the package — the exact gate the Windows job runs
   (`go test ./... -run Test -count=1`) was red by construction. The
   helper moved to the platform-neutral `endpoint.go`; the Windows
   transport (named-pipe DACL + post-connect client-SID check) is
   unaffected and never calls it. Reproduced before
   (`GOOS=windows go vet ./internal/localipc/` → `undefined:
   credAllowed`), green after, with `GOOS=windows go build ./...` clean
   end to end. No assertion weakened; the pure-decision test now runs
   on both platforms.
2. **P1 — `scripts/qt-desktop-smoke.sh` owns its whole environment.**
   The v1.10.1 smoke assumed machine-specific state (hardcoded XTOOLS
   prefix, a pre-staged core, a pre-built close tool, no window
   manager). It now: derives X11 helpers from the system PATH (opt-in
   `XTOOLS`/`XLIBS` overrides), builds the core from the repo when not
   staged, falls back to the developer UI build tree, builds
   `closewin` from the tracked `scripts/closewin.c`, starts an Openbox
   (or `WINMGR_BIN`) window manager when present with private-prefix
   XDG dirs resolved, and tracks every spawned process behind a trap
   so no exit path leaks. G1–G5 PASS under Xvfb + Openbox: keyed
   handshake, core-spawned UI pid, visible 1280x840 top-level, real
   core interaction, clean WM_DELETE_WINDOW exit, no orphan, no leaked
   endpoint. Gate semantics unchanged — the environment contract is
   the fix.
3. **P1 — AI Systems list/activation migrated to Qt (surface 1 of the
   remaining set).** `CoreClient` gains `systems`/`activeSystemId` +
   `refreshSystems()`/`activateSystem(id)` over the EXISTING
   `/api/systems` handlers; `Main.qml` gains an AI Systems drawer
   (list, active badge, activate, refresh); the acceptance suite gains
   `aiSystemsListActivate` (seeded typed list, authoritative activation
   flip, typed 404 error path with unchanged authoritative state). No
   second store and no second authority: validation, persistence and
   the reserved default stay server-owned. Remaining surfaces (AI
   Systems editing/import, Goals, Coding Lab, Net Search, repository
   index, tasks/automations, downloads/diagnostics) stay on the
   documented headless/web mode — not claimed done.
4. **P1 — measurement host classification is fail-closed.**
   `cmd/shtn-perf`'s `--hosted-ci` bool (default true, never
   cross-checked) could silently swap physical ↔ hosted. Replaced by a
   mandatory `--host-class physical|hosted` (unclassified runs are
   refused), a physical-over-hosted-evidence refusal (well-known CI
   marker env vars are evidence; a hosted runner can never emit a
   physical-hardware artifact), a derived `hostedCI` schema-compat
   flag, and RAM/hostname/evidence/notes recorded in the envelope.
   Pins in `internal/perf/perf_test.go`; committed artifact
   `baselines/perf-hosted-v1.10.2.json` (hosted session host, plainly
   labeled).
5. **P1 — high-severity `ws` advisories closed.** GHSA-58qx-3vcg-4xpx + GHSA-96hv-2xvq-fx4p on the direct
   devDependency ws 8.18.3; bumped to 8.22.0 (same major, no
   `--force`); `npm audit --audit-level=high` clean; frontend gates
   green. (v1.10.3 correction: the v1.10.2 wording called this the
   "smallest compatible fix", but the advisories were already fixed
   upstream in ws 8.20.1 and 8.21.0 — 8.22.0 was chosen, not the
   smallest; the dependency itself is unchanged.)
6. **P2 — launcher resolution contract pinned by unit tests.** New
   `internal/desktop/desktop_test.go` locks the `uiExecutable`
   resolution: the `none` sentinel, the explicit override, the
   next-to-core packaged layout, the missing-override refusal, and the
   hard refusal when no UI can be located (a silent headless fallback
   would hide a broken install).
7. **P2 — documentation/version reconciliation.** Canonical identity
   1.10.2 (package.json → release-version.mjs → config.go /
   build/config.yml / SIGNATURE / ui/qt CMakeLists project VERSION);
   README current-release block updated (was stale at 1.10.0);
   ARCHITECTURE.md desktop-lifecycle paragraph extended (launcher
   resolution contract, smoke environment ownership, fail-closed
   measurement, AI Systems surface); ROADMAP Phase 0/2 status lines
   updated; UPDATE.md rewritten for v1.10.2 with the v1.10.1 notes
   retained as history; agent.md re-handed-off. The v1.9
   future-boundary items (goal-to-subtask fan-out, anti-hack guard,
   MCP registry) remain FUTURE unless code+tests exist.

Local verification on the session host (virtualized Linux container;
Go 1.26.0, Qt 6.8.2, CMake 4.4.4): full `go test ./... -count=1` PASS;
`go vet ./...` clean; GOOS=windows build+vet clean for the full module;
Qt 6.8.2 configure/build green; QtTest acceptance 4/4; qt-desktop-smoke
G1–G5 PASS under Openbox; frontend typecheck/lint/build/verify:web
green; `npm audit --audit-level=high` clean; release-version --check
PASS at 1.10.2. NOT RUN (no host): Windows MSVC/windeployqt/windowed
smoke/QtTest-on-Windows; native C++ CTest parity gates; hosted-CI
Actions verdict (no push; API status read rate-limited). The physical
Windows baseline stays NOT MEASURED.

---

## v1.10.1 — 2026-10-11 — the FULL Qt/QML migration + the v1.10.0 failure repair (P0): Wails removed from the product and dependency graph, the Go core + native Qt host desktop pair over a secured per-user IPC channel, deterministic run teardown, the canonical test entrypoint

Focus: repair the two demonstrated v1.10.0 run-`38152579695` failures at
the root (Windows test teardown, Linux GUI smoke), migrate the desktop
UI to native Qt Quick/QML per the R&D authority's Phase 2 (with the
Phase 1 engine gate honestly untouched — the physical-Windows baseline
remains NOT MEASURED and the native-engine default stays evidence-gated),
and remove Wails and its WebView transport from the product, the build,
the dependency graph and the current-product documentation.

1. **P0 — Windows test teardown (`TestWebSocketSeesTerminalErrorNotSilence`,
   run `38152579695`).** The test returned at the terminal error frame,
   but the run goroutine's deferred cleanup (memory-manager run-end
   bookkeeping, registry release, hub close) runs AFTER that frame; on
   Windows, `t.TempDir`'s `RemoveAll` raced that still-writing cleanup and
   failed with "The directory is not empty", failing the whole
   `internal/api` package. Root-cause repair, two legs: (a) the test now
   waits on the authoritative outcome AND on the run registry RELEASING
   the session entry (the last disk-writing step of the cleanup) before
   teardown — bounded polling over existing seams, no sleep, no deadline
   inflation, no changed assertion; (b) `Server.Close` now JOINS every
   run goroutine (bounded) before releasing the runtime stack, and new
   run/resume registrations are refused once shutdown has begun — the
   cleanup order is deterministic for every caller, not just the test.
2. **P0 — Linux GUI smoke (run `38152579695`).** The Wails window stayed
   1x1 under Xvfb+Openbox and no renderer/WebKit env workaround repaired
   it. Instead of a fourth Wails hypothesis, the UI is now the native Qt
   Quick/QML host: under the same Xvfb conditions the smoke proves a REAL
   visible 1280x840 top-level window owned by the UI pid, a typed keyed
   IPC handshake, real core interaction, a real WM_DELETE_WINDOW close
   request and a clean zero-status shutdown (scripts/qt-desktop-smoke.sh,
   gates G1-G5).
3. **Qt Phase 2 migration (R&D authority, §7-§8).** `ui/qt`: a
   QQmlApplicationEngine UI (sessions list, chat transcript, cumulative
   live streaming with Stop, settlement → persistence reload, repeat
   send, composer lock/unlock, error/empty/connecting states) over
   `CoreClient` — HTTP/1.1 + RFC6455 WebSocket framing over QLocalSocket,
   bounded headers/bodies/frames, request deadlines. `internal/localipc`:
   the ONE secured local channel — per-user endpoint (XDG runtime dir +
   0700 directory + 0600 socket; per-user named pipe on Windows with a
   current-user DACL + client-SID verification), SO_PEERCRED uid checks,
   stale-endpoint recovery, live-endpoint protection, an ephemeral
   128-bit handshake key (env-passed, never logged), structured bounded
   error responses. The Go core serves the EXISTING handlers through the
   channel — no session/run/tool/memory/approval policy is reimplemented
   in C++, and the Qt host is a client, never a second authority.
   `internal/desktop`: the launcher binds the channel, spawns the Qt
   host with endpoint+key, and joins lifecycle on child exit/signals.
4. **Wails removal.** Every Wails import, the module requirement and its
   whole transitive graph are gone from go.mod/go.sum (CI now asserts the
   ABSENCE); `internal/desktop` is the new launcher boundary; the Wails
   loopback activity transport and its discovery endpoint are retired
   (the endpoint now answers same-origin unconditionally for the
   documented headless/browser mode); build/config.yml no longer carries
   the wails3 task runner; scripts/windows-live-answer-acceptance.mjs is
   replaced by the QtTest acceptance suite. Retained historical mentions
   exist only where deleting them would falsify release history
   (changelog.md/worklog.md release records).
5. **The canonical test entrypoint.** scripts/verify-all.mjs runs every
   applicable suite (release contracts, frontend units, go vet/build/
   headless/full/race, CTest, Qt configure/build/acceptance) with
   fail-fast exit codes, preserved output, and honest NOT RUN reporting
   for genuinely absent toolchains (--release mode fails on them).
6. **Windows/Linux CI (build-desktop.yml).** Windows builds the Qt host
   with the official Qt 6.8.2 MSVC toolchain, deploys it QML-aware with
   windeployqt next to the core, and replaces the CDP harness with the
   Qt acceptance suite against the packaged core; the runtime smoke
   asserts the Qt child's real window. Linux installs the Qt runtime
   deps (the GTK4/WebKitGTK stack is gone), builds the host, and runs
   the G1-G5 smoke plus the acceptance suite under Xvfb.
7. **Versioning.** package.json 1.10.1 (canonical), SIGNATURE,
   build/config.yml, internal/config AppVersion and the workflow identity
   all say 1.10.1 (`release-version.mjs --check` green).

## v1.10.0 — 2026-10-11 — the FIRST R&D-cycle release: both v1.9.6 CI failure classes repaired at the root (P0), the UpdateEngineNow admission-seam gap closed (P0), the evidence-gated Phase 1 decision + the honest Phase 0 baseline + the Qt/IPC seam (P1)

Focus: deliver the first release of the R&D architecture cycle
(`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md`, unchanged in its source repository) by
removing the two demonstrated v1.9.6 CI failures so both platform
desktop gates can genuinely finish, completing the admission seam the
v1.9.6 release documented as standing, and taking the honest
measurement/evidence steps of R&D Phases 0 and 1. The production UI
remains the Wails shell; Qt is the documented Phase 2 target. No
v1.9.7 exists or is implied.

1. **P0 — Windows live-answer acceptance: the CDP test switch never
   reached WebView2 (run `38135751846`).** The step failed after a green
   desktop runtime smoke with the timing signature of the harness's full
   bounded 60 s CDP attach window (~75 s step total). Root cause derived
   from the pinned wails v3.0.0-beta.16 sources: the default pure-Go
   WebView2 loader pins `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` to the
   empty string at package init (`webviewloader.preventEnvAndRegistryOverrides`),
   erasing the harness's environment-variable switch before any WebView2
   environment existed. Repair: the desktop shell now forwards the
   TEST-ONLY `SHEYTAN_ACCEPTANCE_CDP_PORT` into the programmatic
   `application.Options.Windows.AdditionalBrowserArgs`
   (`internal/desktop/desktop.go`) — the one channel Wails composes
   itself — and the harness sets exactly that variable. Shape-checked
   (digits, bounded), inert in production, sanitized length-only failure
   diagnostics. Gate contract unchanged.

2. **P0 — Linux desktop runtime smoke: a real window manager, owned by
   the smoke step.** v1.9.6's own X11 evidence (created-but-unmapped
   windows owned by the app pid; both v1.9.5 and v1.9.6 failing
   identically despite the renderer switches) classifies the failure as
   the GTK4 top-level not mapping under bare Xvfb without a WM. The
   smoke step now installs and runs `openbox` on its display, proves WM
   ownership through the EWMH `_NET_SUPPORTING_WM_CHECK` root property
   (bounded poll), and cleans it up with the existing trap. Gate
   unchanged (visible pid-owned window, WM_DELETE_WINDOW, exit 0, full
   failure diagnostics). Product unchanged for CI; no gate weakened.

3. **P0 — UpdateEngineNow joins the destructive-boundary admission seam.**
   The same-tag/scheduled update transaction previously stopped the
   engine without the run-admission claim. v1.10.0 applies the same
   early advisory refusal, the same boundary claim (atomically with the
   live verdict, before the first destructive step), the same
   balance-on-every-exit release, and the same nil-seam boundary
   re-check; refusals are deferrals, never persisted bounded failures.
   One authority unchanged: `BeginEngineMutation`/`EndEngineMutation` +
   `EngineMutationActive`. Pinned by
   `internal/llm/admission_updatenow_v1100_test.go` (five deterministic
   pins); the v1.9.6 and v1.9.5 pins stay green unchanged.

4. **P1 — R&D Phase 1 (native promotion) decided by evidence: NOT
   flipped.** Native C++ CTest 13/13 locally including bit-for-bit
   prefill/KV parity; the native engine is exercised end-to-end by the
   browser E2E stack; `NativeBackendEnabled` keeps native as an explicit
   opt-in with managed llama.cpp automatic fallback. The blocking
   evidence for the default flip is specific: the warm-TTFT comparison
   against managed llama.cpp on physical Windows target hardware, per
   the R&D measurement contract. Checklist in ROADMAP.md.

5. **P1 — R&D Phase 0 baseline recorded honestly.**
   `baselines/perf-hosted-ci-v1.10.0.json`: cold start + idle/peak
   memory measured on the hosted host; engine-ready / warm-TTFT /
   stream-to-visible / tool / file metrics NOT MEASURED with per-metric
   reasons. Hosted-CI labeled; targets remain targets.

6. **P1 — the Qt/IPC seam is defined (documentation only).** ARCHITECTURE.md
   now specifies the typed, coarse-grained, versioned local boundary for
   the future Qt shell: secured local socket/pipe, the loopback
   capability-token auth discipline (reused, never duplicated as a
   second authority), no unauthenticated local API, shared memory only
   where profiling proves a win, and an explicit NOT-ready list. No Qt
   code ships in v1.10.0.

---

## v1.9.6 — 2026-10-11 — the FINAL 1.9.x maintenance release: the Windows admission-hostage regression (P0), the Linux native-window smoke repair (P0), the R&D alignment + the measurement baseline (P1)

Focus: deliver the last planned v1.9.x release as a trustworthy bridge
into v1.10.0. The authoritative failing run `38095637817` (commit
`070a05930f35ac09536ead74cc7cf2f376b8c2a4`) failed BOTH platform jobs;
this release root-causes and repairs both, adopts the R&D architecture
decision (`Parsaetak/Contents` → `Research-and-Development` →
`SHEYTAN-LA-2026-10-11.md` — Go orchestrator, C++ inference, Qt-primary
UI for the next major cycle) as the documented direction, and ships the
first honest vertical slice of that decision: the reproducible
measurement baseline framework. The next release is v1.10.0; no v1.9.7
is planned or implied.

1. **P0 — the v1.9.5 Windows 503 regression: a background optimization
   held run admission hostage for minutes.** Both authoritative Windows
   failures (`TestLocalGemmaHiChatCompletes`,
   `TestLocalChatSurvivesUnreadableModelCard`, POST `/api/run` → 503
   "engine maintenance in progress") root-caused to the v1.9.5 admission
   claim PLACEMENT, not to a test-only defect: the claim was taken at
   TRANSACTION START of `updateEngineVariantTx`, so on windows/amd64 the
   background AUTO Vulkan bootstrap (`go s.maybeAutoProvisionVulkan`,
   spawned by `EnsureSetup` after the maintenance gate) — fully eligible
   in the fixture posture (AUTO default, `GPUAutoOffload=true` default,
   GPU detected on the runner, CI online, a resolvable model) — took the
   claim and held it across the NETWORK resolve/download phase (minutes).
   Every run admitted in that window was refused 503. Linux never saw it
   because `VariantSupported(Vulkan)` is false there. This was a REAL
   product defect, not a fixture accident: any Windows user with
   AUTO + detected GPU + online got 503 on chats typed during a
   background probe's download window. THE FIX (one authority preserved):
   the admission claim now lives AT THE DESTRUCTIVE BOUNDARY of the
   transaction (stop → swap → start → verify → rollback/commit); the
   pre-boundary phases (validate, no-op, resolve, staged download) are
   claim-free, with an unconditional early advisory live-run refusal
   (read-only registry check, v1.9.4 semantics) preserved. The TOCTOU the
   claim exists to close lives entirely inside the destructive window, so
   the narrower claim closes it identically; the staged download was
   split from the swap via the new `updater.DownloadEngineArchiveStaged`
   (non-destructive half; the swap keeps the full install lease/lock
   discipline through `InstallStagedFromArchiveDeferredWithVariant`). A
   download failure no longer bounces a running engine. Deterministic
   regression coverage: `internal/llm/admission_boundary_v196_test.go`
   (claim held INSIDE the destructive window — observed from the
   verification hook; begin/end balance; a run registered during the
   claim-free download phase refuses AT the boundary without ever
   claiming; nil-seam boundary re-check), revised pins in
   `admission_v195_test.go` / `rungate_v194_test.go` (non-destructive
   exits never claim; the early check is unconditional). The two
   local-chat E2E fixtures now pin an explicit CPU profile — the honest
   test boundary that keeps the unrelated AUTO GPU bootstrap out of the
   pinned chat contract. Local evidence: both formerly-failing tests pass
   `-count=5`; the full `internal/... -tags headless` suite passes.

2. **P0 — the Linux desktop runtime smoke: the first REAL execution of
   the gate exposed the missing headless-renderer configuration.** The
   smoke step's shell was unchanged since v1.9.2, but earlier releases
   were skipped behind upstream gate failures — run `38095637817` was the
   gate's first actual execution, and it failed with "NO visible
   top-level window for pid 21762 on :77" while the process stayed alive
   and wrote every boot marker. That signature is the documented
   bare-Xvfb failure class: with no GPU/GLX/EGL device and no compositor,
   the WebKitGTK 6.0 DMABUF renderer and the GTK4 GL renderer stall
   widget realization, so the GTK top-level never maps. THE FIX (test
   environment, never the gate, never the product): the smoke step now
   owns the documented software-renderer switches for virtualized
   environments (`WEBKIT_DISABLE_DMABUF_RENDERER=1`,
   `WEBKIT_DISABLE_COMPOSITING_MODE=1`, `GSK_RENDERER=cairo`) — owned by
   the smoke step only; real desktop sessions keep hardware acceleration.
   The gate itself is UNCHANGED (real visible, pid-owned window +
   WM_DELETE_WINDOW + exit 0), and the failure path now dumps real X11
   evidence (root window tree via xwininfo — `x11-utils` added to the
   dependency step — plus every window xdotool can see and the smoke
   environment keys) so the next run is conclusive either way. LOCAL
   STATUS: UNVERIFIED — this authoring host has no GTK4/WebKitGTK
   headers and no root; the gate executes in CI.

3. **P1 — R&D alignment: the strategic direction is adopted and the
   first vertical slice is real.** The R&D decision is authoritative for
   direction: Go remains the ONE policy/orchestration authority;
   C++ owns numerical inference (the existing `native/engine` path is
   the promotion target — with managed llama.cpp as the honest fallback —
   gated on capability/parity evidence, NOT flipped by default in this
   patch); Qt (C++/QML) is the primary UI target for the next major
   cycle with a typed coarse-grained IPC boundary; WinUI 3 stays an
   optional Windows-only future shell. v1.9.6 ships the measured
   baseline slice (R&D §8 Phase 0): `internal/perf` (the metric set of
   R&D §10 with honest NOT-MEASURED semantics, nearest-rank P50/P95,
   host/method/limitation metadata, targets reported as GAPS) +
   `cmd/shtn-perf` (machine-readable JSON + concise report; cold start,
   memory captured; generation-path metrics stay NOT MEASURED without a
   real engine/model on the host). No performance TARGET is claimed as
   met: the baseline records what this host measured and labels the
   rest. The v1.10.0 migration plan (phases, gates, dependencies, risks)
   is documented in `ROADMAP.md`.

4. **P1 — known limitation recorded honestly:** `UpdateEngineNow` (the
   same-tag/scheduled update transaction) does not yet participate in
   the admission claim seam (pre-existing since v1.3.6; its startup
   path is ordered before any engine/prewarm, but the scheduled loop's
   later passes are not run-admission-coordinated). Wiring it to the
   same destructive-boundary claim is a v1.10.0 Phase 0 gate item.

Verification (v1.9.6 session, local Linux x86-64 authoring host, Go
1.26, Node 24): `go build -tags headless ./internal/... ./cmd/... .`,
the full `go test ./internal/... -tags headless -count=1`, the focused
admission/rungate/variant/local-chat suites (the two authoritative
failures pass `-count=5`), `go vet` on touched packages, the frontend
typecheck/lint/unit/release suites, and the C++ native configure/build/
ctest run recorded in `worklog.md`. CI verdicts (Windows live-answer
acceptance, Linux native window smoke, packaged E2E) remain UNVERIFIED
locally by construction — the authoritative verdict is the Actions run
carrying this revision, and no green-run claim is made until it
reports.

---

## v1.9.5 — 2026-10-11 — the false-proof acceptance harness repair (P0) + the least-privilege desktop transport (P0) + the atomic engine/run admission (P1) + the tree-wide identifier-free log contract (P1)

Focus: repair the failed v1.9.4 release attempt (Actions run
`38090590263`: `npm run lint` failed at
`scripts/windows-live-answer-acceptance.mjs:67` — `Identifier 'sleep'
has already been declared`, fail-fast shell skipped every downstream
job), then audit the v1.9.4 diff as untrusted new code. The audit
confirmed the reported defects against source, reproduced them, and this
release repairs all of them with focused regression tests. No claim
below is made without the matching evidence produced in this session;
evidence that this environment could not produce is labelled UNVERIFIED
and stays gated in CI.

1. **P0 — the Windows acceptance harness was a false-proof generator;
   it is now a real, strict gate.** Reproduced the exact lint failure
   (duplicate `const sleep`), then repaired the harness semantics:
   (a) single `sleep`; (b) the growth contract now requires ORDERED
   evidence — sample A with nonblank, non-placeholder live text AND the
   Stop button visible, then a strictly longer sample B with Stop STILL
   visible and content STILL non-placeholder — and removes BOTH
   false-positive paths (growth accepted after completion; settled
   Markdown accepted as a growth substitute); (c) settlement requires
   visible non-whitespace final Markdown text, no placeholder anywhere,
   exactly one persisted assistant reply AND the expected persisted user
   message; (d) the reload proof uses the REAL session-selection
   contract (`AgentSidebar` `data-session-id` active row) instead of
   inferring from row counts; (e) the real top-level window close is
   exercised via `taskkill` WITHOUT `/F` (delivered `WM_CLOSE`), the
   process must exit within a bounded deadline, a timeout is a FAILURE,
   and forced kill happens only after the failure is recorded;
   (f) the stream trace is armed BEFORE app modules load (CDP
   `Page.addScriptToEvaluateOnNewDocument` seeds localStorage, then
   reload) — the v1.9.4 path wrote localStorage after module load and
   passed a process env var the page module never read, so the hook was
   never installed; the harness now FAILS on `trace-hook-not-installed`
   or a trace missing the expected boundary stages, and validates the
   artifact is sanitized; (g) `ws` is a pinned declared dev dependency
   (8.18.3 + lockfile) — a clean `npm ci` provisions it, no manual step.
2. **P0 — the harness is now wired as a REQUIRED Windows CI gate.**
   The v1.9.4 script was never executed by any workflow. The Windows job
   now builds the exe, builds the native engine (CMake), generates the
   canonical wide-context fixture (`e2e/make-e2e-model.py`, the same
   writer the browser E2E uses), writes the desktop config and runs
   `scripts/windows-live-answer-acceptance.mjs` under `Invoke-Gate` —
   real packaged WebView2 app, real native-engine generation, real
   streaming. Status: WIRED, first execution on the next hosted Windows
   run — UNVERIFIED from this repair environment (no Windows host
   available here); a green Linux run must never be read as a Windows
   desktop proof.
3. **P0 — the loopback activity transport is least-privilege and
   authenticated.** The v1.9.4 listener served `s.Handler()` — the
   ENTIRE API — on `127.0.0.1:0`, exposing session/run/engine/updater
   REST endpoints to any local process that discovered or scanned the
   port, and authenticated nothing beyond an Origin header (which
   `ws.go` accepted as absent). v1.9.5: the listener mounts EXACTLY ONE
   route (`/ws/activity` — the SAME `handleActivityWS` authority, no
   second handler); every upgrade requires (i) a 256-bit cryptographically
   random per-server capability token (constant-time compare), delivered
   ONLY through the trusted intercepted discovery surface
   (`GET /api/ws-origin` now returns `{origin, token}`, token only when
   an origin exists and never logged), (ii) a non-empty Origin the
   existing authority accepts — an absent Origin can never bypass the
   loopback authentication, (iii) a Host header equal to the listener
   address, and (iv) no forwarding headers. Everything else is 404.
   The browser/headless same-origin contract is unchanged (no token
   exists in that mode). Regression: `internal/api/loopback_v195_test.go`
   (least-privilege surface walk, full guard matrix, real upgrade with
   token+origin receiving `attached`, idempotent + concurrent Close).
4. **P0 — discovery-side hardening (`src/config.ts` + new
   `src/loopback-origin.ts`).** The v1.9.4 `startsWith("http://127.0.0.1")`
   prefix match accepted lookalike origins. v1.9.5 parses with `URL` and
   accepts ONLY an exact normalized `http:` loopback origin (host exactly
   `127.0.0.1` or `localhost`, explicit valid port, no credentials, no
   path/query/fragment) with a canonical-form round trip (IP shorthand
   such as `127.1` is refused). Discovery on a desktop page origin is
   retried a bounded three times and, if the transport is still absent,
   the page enters an explicit, visible degraded state
   (`activityTransportDegraded` + a persistent banner in MessageStream)
   instead of silently caching the same-origin fallback forever; the
   browser/headless empty-origin verdict is still accepted immediately.
   Regression: `src/loopback-origin.test.ts` (every refusal class).
5. **P0 — lifecycle and startup ownership.** `Server.Close()` contained
   three consecutive `s.closeLoopback()` calls (a v1.9.4 merge artifact);
   exactly one ownership call remains and `closeLoopback` is
   idempotent/concurrency-safe (pinned by test). Desktop startup now
   runs `EnsureSetup()` BEFORE `StartLoopback()`, and a transport bind
   failure is a HARD, VISIBLE fault — the desktop shell exits non-zero
   instead of silently launching an app whose core interaction is
   broken. `server.go` also installed the run gate TWICE (duplicate
   v1.9.4 block) — one installation remains, now alongside the
   admission seam.
6. **P1 — the engine/run admission TOCTOU is closed atomically.** The
   v1.9.4 gate was checked once at the destructive boundary of
   `updateEngineVariantTx`; a run accepted during the minutes-long
   transaction (resolve → download → swap → verify → rollback) still
   lost its serving engine. v1.9.5 adds the `llm.EngineAdmission` seam:
   the transaction claims run-admission exclusivity FIRST — atomically
   with the live-run verdict, under the same `runsMu` critical section
   run registration uses — and holds the claim for the whole
   transaction. A run either registers before the claim (the claim's
   verdict sees it and the transaction refuses with
   `ErrEngineBusyWithRun`) or its admission observes the claim and is
   refused deterministically (HTTP 503,
   `errEngineMutationInProgress`) with the persisted transcript
   restored (no dangling user message). Run admission and resume both
   honour the claim; the nil seam (tests/headless) preserves the exact
   v1.9.4 behavior. No deadlock (runsMu is a leaf lock; the transaction
   holds switchMu + the engine lease without ever calling admission
   back). Regressions: `internal/llm/admission_v195_test.go`,
   `internal/api/admission_v195_test.go`; `go test -race` green on
   `internal/api` and `internal/llm`.
7. **P1 — ordinary logs are identifier-free.** `gpuEvent()` printed
   `runId=%s` on every AUTO-probe diagnostic, and the run settle
   summary, pause/resume/edit-transaction and history lines carried
   `runId=`/`session=` fields tree-wide (`internal/api/server.go`,
   `runcontrol.go`, `history.go`, `edittx.go`,
   `internal/llm/variant_auto.go`). All removed: one concise
   state transition or actionable failure per line; durations and
   counters (identifier-free) remain; the AUTO-probe ordinal is an
   ephemeral per-process counter (`probe=%d`). The prompt/reply text,
   per-token events and secrets were already absent and stay absent;
   the opt-in sanitized trace (`src/stream-trace.ts`) remains the ONLY
   detailed surface and is still disabled by default.
8. **P1 — metric semantics verified honest.** `acceptToDoneMs` is
   acceptedAt → terminal-published (never a WebSocket network latency);
   the summary names every boundary it measures and explicitly documents
   that client-side boundaries live only in the opt-in trace. GPU
   claims remain execution-evidence-gated (enumeration alone never
   claims execution); the AUTO probe still treats a refused transaction
   as a deferral, never a persisted bounded failure.

Verification (this session, local): `npm ci` clean; `npm run typecheck`,
`npm run lint` (0 warnings, 0 errors), `npm run test:units` (236/236),
`npm run test:release` (38/38), `npm run build` + `npm run
verify:web` (tracked `web/static` synchronized), `go build -tags
headless ./internal/... ./cmd/...`, `go vet -tags headless ./internal/
./cmd/...`, `go test -tags headless -count=1 ./internal/...` (all
packages), `go test -race -tags headless -count=1` on api/llm/agent/
sessions/runtime/englease. UNVERIFIED here (hosted CI runs them):
Windows desktop UI acceptance (no Windows host in this environment),
the native C++ CTest suite (no cmake in this environment; no native
code was modified), the full browser E2E with real generation (no
provisioned engine binary here; no e2e-relevant code path was changed
by this release beyond the harness itself).

---

## v1.9.4 — 2026-10-10 — the desktop live-answer transport root cause (P0) + the settled-reply proof repair (P0) + the engine/run coordination gate (P1)

Focus: root-cause and repair the persistent user-reported Windows defect
"the model's answer appears only after pressing Stop" — not with another
client-side scheduling fix, but by proving WHERE the chain
engine → orchestrator → runLive → hub → WebSocket → WebView2 → store →
flush → Zustand → DOM actually breaks in packaged desktop builds — and
repair the two zero-session E2E failures (Actions run `38069168867`,
`e2e/zero-session.spec.ts:86` and `:275` at `expectRealAssistantReply`)
that kept v1.9.3 from being a green release. Also closes a real
engine-transaction/run race confirmed from source, renames the
misleading `publishMs` metric, and ships the opt-in sanitized stream
trace plus the real-Windows acceptance harness the release contract
requires.

1. **P0 — the desktop live-answer defect: root-caused to an UNREACHABLE
   activity WebSocket, repaired with a loopback transport for the SAME
   authority.** Wails `v3.0.0-beta.16` serves packaged desktop builds
   through the webview's resource-request interception: on Windows,
   WebView2 forwards ONLY `http://wails.localhost` requests into the Go
   handler (`webview_window_windows.go` hands every other scheme to the
   webview's default handler); on Linux production the page rides the
   custom `wails://` URI scheme (`linux_cgo.go`). WebSocket handshakes
   are not web-resource requests, and no loopback listener existed — so
   the activity socket derived from `window.location`
   (`ws://wails.localhost/ws/activity`) never reached the backend at
   all: the default network stack resolves `*.localhost` to the loopback
   address on port 80, where nothing listens, and the client's 20
   reconnect attempts failed silently. Live run frames never reached the
   UI on any packaged desktop build: no streamed text, no `done` event,
   a locked composer — and Stop "worked" only because the abort path is
   HTTP (a synchronous flush plus a history reload over the intercepted
   surface). This is why four releases of client-side scheduling repairs
   (v1.8.1 single-frame fast path, v1.8.2–v1.8.5 flush schedulers and
   hub conflation) never changed the symptom: each is downstream of a
   socket that never connected on Windows. The fix keeps ONE authority:
   the desktop process additionally serves the EXISTING API handler
   (`internal/api/loopback.go`) on a loopback-only ephemeral listener;
   `GET /api/ws-origin` publishes the origin over the intercepted HTTP
   surface (empty = same-origin, the unchanged browser/headless
   contract); the client resolves it once and points ONLY the activity
   socket there (`src/config.ts`, `src/store.ts`); the upgrade is
   origin-checked (`ws.go`: the Wails page origin, http/wails +
   `wails.localhost` exactly, against loopback request hosts only — no
   wildcard, non-loopback hosts refused). Pinned by
   `internal/api/loopback_v194_test.go`, including a REAL WebSocket
   upgrade through the loopback with the Wails page origin receiving the
   deterministic `attached` acknowledgement. **Evidence class:
   root cause and server-side transport PROVEN; the end-to-end
   Windows WebView2 interaction is UNVERIFIED on the Linux authoring
   host** — `scripts/windows-live-answer-acceptance.mjs` ships to run
   the exact contract on a real Windows host (launch → send → real
   non-placeholder text while Stop is visible → strictly longer DOM text
   while Stop stays up → one persisted answer → reload → close).

2. **P0 — the zero-session E2E failures were a proof-selector defect;
   the product rendered real replies all along.** Local reproduction
   (3/4 zero-session tests failing the same helper, worse than CI's 2/4)
   plus the failure ARTIFACTS (the settled reply visibly rendered with
   real content while the locator found nothing) identified the v1.9.3
   helper: it waited on `.message-row.from-agent .message-content`, but
   `.message-content` exists only on the LIVE surfaces (generation
   bubble, streaming safety net). A SETTLED assistant message renders
   through `MessageBubble` → `<Markdown>` → `div.message-markdown` and
   never carries `.message-content`, so the helper could only ever match
   inside the finalising window — a timing race that Agent mode's CI run
   happened to win. The helper now asserts the settled Markdown surface
   (`.message-row.from-agent` carrying `.message-markdown`, placeholder
   rows excluded belt-and-braces, non-whitespace text asserted on the
   markdown surface itself) — the v1.9.3 strength contract preserved and
   tightened; the live/streaming bubbles can never satisfy it. No
   assertion was weakened, no placeholder accepted, no timeout inflated.

3. **P1 — engine variant transactions can no longer stop a RUNNING
   engine under an active run.** Confirmed from source: the AUTO Vulkan
   eligibility checks "no run is active" once, then the transaction
   (download → swap → verify → rollback — up to 20 minutes) stopped a
   running engine with no re-check; the supplied Windows runtime
   evidence showed candidate/rollback messages overlapping a user
   generation. `LlamaServer.SetRunGate` (wired to the API layer's
   `anyRunActive`) is consulted by `updateEngineVariantTx` BEFORE
   anything happens: a running engine plus an active run fails CLOSED
   (`ErrEngineBusyWithRun`), the engine keeps serving, the AUTO probe
   records a DEFERRAL (never a persisted bounded failure — a temporary
   condition must not disable the candidate for the hardware+engine
   identity), and the explicit user path surfaces the conflict.
   Pinned by `internal/llm/rungate_v194_test.go`; the existing strict
   variant-transaction suite passes unchanged.

4. **P1 — the run summary now names every boundary honestly.** The
   previous `publishMs` measured accepted→donePublished (run duration to
   terminal publication) and read like a per-event socket latency — the
   supplied log's `publishMs=82178` invited exactly that misdiagnosis.
   The summary now carries `gateMs` (accepted→engine-gate ready),
   `ttftMs` (the orchestrator's measured request_sent→first_token),
   `persistMs`, `acceptToDoneMs` (the renamed field), `totalMs`, and
   aggregate published-event counters (`events`, `deltas`,
   `firstDeltaMs`). Client-side boundaries (socket queue delay, write
   time, browser receipt, DOM visibility) are not derivable server-side;
   they live in the opt-in trace below.

5. **P0-A — the opt-in, test-only sanitized stream trace.**
   `src/stream-trace.ts` arms ONLY on `localStorage
   SHEYTAN_STREAM_TRACE=1`, records every client boundary (socket
   receive/attach/snapshot, fast-path fold, accumulator flush, run
   start/end, the GenerationBubble DOM commit) with monotonic
   timestamps, ephemeral sequence numbers and LENGTHS ONLY — unit tests
   pin that content never enters the trace and that the disabled path is
   a no-op. The zero-session spec attaches the dump to any failure
   (`stream-trace.json`), and the Windows harness saves it to the
   artifact directory. Ordinary application logs stay minimal: counts
   and stage durations only, no per-token lines, no run/session
   identifiers beyond the existing run-summary contract.

Verification (this release): npm ci, typecheck, lint, unit tests 226/226
(incl. the new stream-trace suite), release gate `--check` green; Go
`build`/`vet` clean, headless + full `go test` suites, race gate on the
stream-sensitive packages, native CMake/CTest, and the 40-test browser
E2E executed at release time — see `UPDATE.md` for the exact commands
and the explicit local/CI/unverified evidence separation.

---

## v1.9.3 — 2026-10-10 — the Governor race-gate root cause + the honest prefill-parity contract + the closed zero-session proof standard (P0/P1)

Focus: repair the authoritative v1.9.2 CI failure — Actions run
`38035650428` (job `114165397477`: the Go race gate failed
`TestGovernorEndpointServesComposedSelfModel` with
`ok-pressure envelope class = live, want none` while frontend/release
gates and the other five race packages passed) — by reproducing the
failure on a real host under real generated load, pinning the CPU policy
contract in code, and making the integration test independent of host
load. Also corrects the native prefill parity gate's claims to exactly
what is proven (and strengthens the proof), closes the remaining
zero-session placeholder loophole, and raises sub-10px UI typography to
the documented floors.

1. **P0 — Governor race gate: root cause reproduced, policy pinned,
   tests deterministic.** `runtime.StartGovernor` wires the production
   platform CPU seam (`governor.CPULoadPlatform`) into the governor the
   API test folds samples into; the test folded ONE OK-pressure RAM
   sample, the seam folded the runner's REAL load with it, and the first
   EWMA sample (equal to the raw value) crossed `CPUReduceAbove` — one
   high first sample flipped the envelope to `live`, contradicting the
   documented "one noisy sample never drives policy" promise.
   Reproduction: the original v1.9.2 code under real host load
   (loadavg 2.87 on 2 cores) fails with the exact CI signature; the fix
   passes under 96–218% measured load. The fix: (a) the CPU-reduction
   branch requires a SUSTAINED rolling signal — ≥ `cpuWarmupSamples` (3)
   folded samples AND average ≥ `CPUReduceAbove` — so a lone spike never
   flips the envelope while sustained high load still reduces
   background/concurrency; the sample count rides
   `ResourceState.CPUSamples` and the reduction reason; (b) a
   mutex-guarded `Governor.SetCPUSampler` seam lets the API test pin an
   honest unknown CPU (production wiring keeps `CPULoadPlatform`); (c)
   `TestSustainedCPUHigherThanThresholdReduces` folds a genuinely
   sustained signal and the new `TestLoneCPUSpikeDoesNotFlipEnvelope`
   pins both directions. No production sampling disabled, no race
   coverage removed, no expected value flipped.

2. **P0 — native prefill parity: claims corrected, proof strengthened.**
   The v1.9.2 gate claimed per-position logits parity but compared only
   the full prompt's final logits (the span path projects only
   span-final logits by design). The v1.9.3 gate proves per-position
   logits through the chunk=1 span cadence (bit-for-bit at every
   fixture position), final-token logits for chunks 3/7/16, full KV
   bytes identical across every chunking AND the serial run, chunk-
   boundary invariance, edge/error bounds, and mixed prefill→decode
   continuity; tied output is explicitly not claimed (no shipped fixture
   is tied). Clean native configure/build/ctest: 13/13.

3. **P1 — zero-session E2E: every test now proves the settled reply is
   real content** (non-empty `.message-content`, no
   `[data-stream-placeholder]` descendant); the count-based assertions
   could previously be satisfied by the placeholder-carrying generation
   bubble or an empty persisted reply. The rAF-suspended live-stream
   test additionally proves the persisted reply is non-empty. No
   assertion weakened.

4. **P1 — UI/UX: all 62 sub-10px `font-size` declarations raised to the
   documented floors** (metadata ≥ 10px, essential
   labels/states/errors ≥ 11px, interactive control text ≥ 12px); no
   healthy size touched.

---

## v1.9.2 — 2026-10-10 — the measured live-stream/TTFT engine repair (P0) + the desktop window-close lifecycle fix (P0) + the zero-session live-surface fix (P1)

Focus: repair the authoritative v1.9.1 CI failures — Actions run
`37869632400` (Linux job `113625251074`: all three
`e2e/live-stream.spec.ts` tests expired at the 60 s wait for real
non-placeholder streamed text; Windows job `113625251146`: the desktop
runtime smoke process survived `CloseMainWindow()` + 15 s) — by
reproducing both on real stacks, measuring the true boundaries, and
fixing the product where the evidence pointed. Also root-causes and
fixes a zero-session live-surface clobber exposed by the engine
speedup, and corrects the E2E observation contracts without weakening
them.

1. **P0 — live-stream/TTFT: the failure is the native prefill path, and
   the fix is measured, real, and parity-pinned.** The v1.9.1
   placeholder-baseline diagnosis was correct but INCOMPLETE: with the
   observation contract fixed, CI still expired — the failure moved
   earlier. The reproduced stack measured the complete chain: the E2E
   prompt encodes to ~1681 ENGINE tokens (1785 briefing bytes at ~1
   token/byte on the 5-merge fixture vocab; the Go estimate logs ~445),
   serial prefill cost a measured 19.0 ms/token — dominated by ~8064
   per-row tensor-name resolutions per token (std::map lookups +
   ostringstream name construction in the matvec path), NOT the
   arithmetic (a standalone -O0 vs -O2 benchmark times identically).
   First token ≈ 32 s standalone, >60 s under suite contention: all
   three tests expired before the first real snapshot existed. Repairs:
   (a) the **resolved weight table** — every layer's weight matrices and
   the output projection are resolved ONCE per model binding in
   `Forward::init` (name → base pointer + shape + layout, validated
   once through the same bounds machinery), and the forward pass walks
   rows by pointer arithmetic: measured **19.08 → 3.66 ms/token
   (5.2×)**, for prefill AND decode; (b) **`Forward::prefill_span`** —
   the generate loop prefills in bounded 16-token chunks, computes the
   vocabulary logits ONLY for the final prompt token (intermediate
   prompt logits were never consumed; on real-vocab models the output
   projection is the largest per-token cost), cancellation still
   observed every 16 tokens; (c) the native CMake build defaults to
   **Release** when no build type is given (a bare `cmake -S -B` —
   exactly what CI ran — previously shipped an unflagged -O0 engine).
   Numerical identity is pinned by the new `test_prefill_parity` native
   gate: prefill_span's last-token logits and the FULL KV cache bytes
   must match the serial `token()` path bit-for-bit, across chunk sizes
   1/3/7/16, plus edge cases (empty span, null buffers, out-of-vocab,
   context overflow) and mixed span→token() decode continuity. All 13
   native tests pass.

2. **P0 — desktop shutdown: closing the main window quit NOTHING; now
   it quits the application.** Verified in the `wails v3.0.0-beta.16`
   sources: a webview window's WM_CLOSE (Windows) / GTK close-request
   (Linux) only EMITS the platform WindowClosing event and destroys the
   window; the event loop keeps running with zero windows (Windows
   quits only on the hidden `__wails_hidden_mainthread` window's
   WM_CLOSE/WM_DESTROY; no quit-on-last-window logic exists for webview
   windows on either platform). SHEYTAN-LA is a single-window
   application with no tray: `internal/desktop` now binds
   `events.Common.WindowClosing` to `app.Quit()`, so the user-visible
   close path returns from `app.Run()` and the deferred `srv.Close()`
   cleanup runs. This is the product-side repair for the authoritative
   Windows smoke failure (process survived a DELIVERED close).

3. **P0 — the desktop smoke gates now prove exactly what they claim.**
   Windows: requires a real top-level window (`MainWindowHandle != 0`,
   Refresh-bounded wait), checks `CloseMainWindow()`'s return (false ⇒
   fail with "no window received the close request"), requires exit
   code 0, and reports the precise unmet evidence level when no window
   exists. Linux: owns a real Xvfb display (the `xvfb-run` wrapper's
   PID was never the app's — the v1.9.1 step SIGTERMed the wrapper),
   discovers the app's real window via xdotool with a `getwindowpid`
   ownership check, closes it with the WM's own `WM_DELETE_WINDOW`
   client message, requires normal-path exit (status 0), and keeps
   SIGTERM only as the failing-run backstop. `xdotool` joins the Linux
   dependency step.

4. **P1 — zero-session live-surface clobber: root-caused with a frame +
   transition trace and fixed.** With the faster engine, every
   zero-session Send lost its live generation surface ~90 ms in (5/5
   measured: bubble + Stop up, gone at +90 ms, "aborted" at +160 ms —
   the backend run completed normally 14 s later). The trace: the
   attach sequence `attached → idle → run_snapshot` lands just after
   `run()` sets its startup state; the attach-time idle sentinel (no
   session, no lastRun) hit `recoverRunFromIdle`'s final fallback —
   declaring the live run "lost" — and the grace re-check would have
   re-clobbered at +2.7 s (the real run's first gate evidence arrives
   at ~4.6 s). The repair: the **attach-handshake exclusion** — a
   lastRun-less idle sentinel within 250 ms of the attach
   acknowledgement is handshake, never recovery evidence. The v1.2.6
   fast-run recovery (idle + lastRun → Path A) and late-idle semantics
   are unchanged. Proven 3/3 with the state-transition harness: the
   live surface now holds 10–15 s and settles normally.

5. **P1 — E2E observation contracts corrected, never weakened.**
   `zero-session.spec.ts` excludes `data-stream-placeholder` arms from
   its visible-before-completion proof (presentation text is never
   model output) and observes within a 60 s bound (the engine gate +
   prefill phase legitimately precedes the first snapshot; the v1.9.1
   30 s bound expired inside it under load). The live-stream spec
   attaches compact structured failure diagnostics (live-surface
   snapshot + server log tail) on a poll timeout.

6. **Verification (local evidence, Linux x86-64 sandbox, Go 1.26, Node
   24, 2 cores — the CI verdict is the Actions run carrying this
   revision):** native cmake configure + build + ctest 13/13 (incl. the
   new prefill_parity gate); `go test ./internal/... -tags headless
   -count=1` 62/62 packages ok; `go test ./... -run Test -count=1` green
   except the documented no-GTK-headers sandbox boundary (the desktop
   packages cannot BUILD without gtk4/webkitgtk pkg-config — CI
   installs them); go vet clean on every compilable package; race gate
   (api/agent/multiagent) clean; typecheck, oxlint, test:units 220/220,
   test:release 38/38 green; stress 47/47 (hangs=0, crashes=0); the
   FULL browser E2E suite 40/40 green (4.4 min) including all three
   live-stream proofs and the corrected zero-session proofs. The
   desktop smoke stages are wired for CI and were NOT executed locally
   (no GTK headers, no Windows — the boundary is recorded, not claimed
   away).

---

## v1.9.1 — 2026-10-08 — the live-stream observation-contract repair (P0) + desktop runtime smoke gates

Focus: root-cause and repair the v1.9.0 Linux CI live-stream failure
(Actions run `37704905404`, Linux Browser-E2E job `113077751580`,
`e2e/live-stream.spec.ts` "streamed text is visible WHILE the run is live
and grows without Stop" — 39/40), restore evidence discipline around the
v1.9.0 suite claims, and add real desktop runtime smoke gates (Linux +
Windows) to the CI workflow so packaging success can never again be
confused with GUI runtime success. No product streaming path changed:
the engine → hub → WebSocket → fast-path → accumulator → scheduler → DOM
chain was verified healthy end to end; the defect was in the TEST'S
observation contract.

1. **P0 — the placeholder-baseline defect, root-caused with measured
   evidence and repaired.** The failing growth assertion baselined its
   "first visible streamed text" on ANY non-empty text in the live
   bubble. The bubble legitimately renders PRESENTATION PLACEHOLDERS
   before the first content snapshot exists ("Connecting to the engine
   and preparing the turn…" while preparing, "…" afterwards), so
   `waitForLiveText` passed within ~100 ms of Send — on placeholder
   text, never model output. The growth poll's 30 s budget then ran
   during the engine gate + prefill phase, which was measured on the
   real stack at ~40 ms per prompt token (standalone host probe: a
   446-token system briefing costs ~8 s of prefill unloaded, >30 s under
   CPU contention; the run telemetry recorded TTFT 7.6 s and
   classifyMs 4091 — the classify stage includes the engine-gate wait).
   On the CI host the poll expired before the real streaming window
   opened: exactly the authoritative signature "first bubble visible,
   Stop visible, no strictly longer text snapshot within 30 s"
   (poll predicate stuck at 0 — live the whole time, text never past the
   48-char placeholder baseline). Reproduced locally under measured
   2-core CPU contention (failed identically), and the full chain was
   verified healthy with a WS-frame + DOM timeline diagnostic (22
   cumulative response frames over a ~1.2 s window, DOM following every
   frame at 50–70 ms cadence, exactly one persisted reply).

   The repair (observation, never weaker):
   - the live surfaces mark their placeholder arms with
     `data-stream-placeholder` (`src/MessageStream.tsx`) — presentation
     text is now distinguishable from streamed content in the DOM;
   - `waitForLiveText` and both growth observations require a
     NON-placeholder snapshot — the growth baseline is the FIRST REAL
     streamed snapshot, so the strictly-longer observation is measured
     inside the genuine live window on every host speed;
   - the fixture budget rises to 320 tokens (the engine emits one event
     chunk per 8 tokens, so the budget IS the deterministic snapshot
     count of the live window — 40 chunks; window duration still scales
     with host speed, snapshot count does not);
   - the rAF-suspended proof uses the same real-content contract.
   Every original assertion is preserved and strengthened: real streamed
   text visible while live, a strictly longer cumulative snapshot while
   Stop is up, honest settlement, exactly one persisted reply.

2. **P0 — evidence discipline.** The v1.9.0 worklog recorded "full
   Playwright suite 40/40" from a single LOCAL run while the
   authoritative GitHub Actions run `37704905404` shows 39/40. The
   v1.9.1 session re-classified that claim as local-only evidence, and
   the worklog now separates local, CI and physical-platform evidence
   explicitly. GitHub Actions remains the only authority for CI claims;
   local runs never upgrade to suite-green claims.

3. **P1 — desktop runtime smoke gates in CI (Linux + Windows).**
   `.github/workflows/build-desktop.yml` gains a Linux desktop runtime
   smoke job (GTK4/WebKitGTK 6.0 stack on ubuntu-24.04, real Wails
   desktop binary built with CGO, launched under Xvfb, embedded UI +
   `/api/health` probed over the loopback HTTP surface, clean SIGTERM
   shutdown) and a Windows runtime smoke step (real desktop executable
   built, launched, process liveness verified, embedded UI health
   probed, clean shutdown) — both clearly labelled RUNTIME SMOKE and
   kept strictly apart from build/package gates: packaging success never
   implies GUI runtime success. These steps are wired for the CI
   environment and are NOT executed by the v1.9.1 local verification
   (this sandbox has no root, no GTK4/WebKitGTK headers, no Windows);
   the worklog records exactly that evidence boundary.

Verification (v1.9.1, local Linux x86-64 sandbox — Go 1.27, Node 24,
2 cores): recorded in `worklog.md` with the local/CI separation; the
authoritative CI verdict for v1.9.1 is the GitHub Actions run that
carries this revision, never a local claim.

---

## v1.9.0 — 2026-10-08 — the AI System Builder + long-horizon agentic engineering + the Linux zero-session reload repair

Focus: turn the existing one-authority runtime into the first real **AI
System Builder + long-horizon agentic engineering layer** — a durable,
user-owned AI System object whose frozen snapshot binds every run; a
durable Goal engine with checkpoints, bounded replanning, approvals and
honest boot recovery; a bounded delegation engine; one deterministic
approval classification; deterministic repository navigation — while
fixing the v1.8.8 Linux Browser-E2E release blocker at its root and
preserving every verified v1.8 behavior. Every new capability extends an
existing authority; no competing manager, registry, store or runtime was
created. Evidence in the sections below; the strategic boundary lives in
`ROADMAP.md` (v1.9 current/future boundary).

1. **P0 — the Linux zero-session reload-persistence defect, root-caused
   and repaired (Actions run 37654274420).** The failing test was
   `e2e/zero-session.spec.ts` — "reload after a zero-session Send". The
   defect signature: the created session persisted but the expected
   transcript marker was missing after reload. Root cause: in the
   zero-session path specifically, `run()`'s lazy `createSession()` POST
   executed while the store still read `running:false` — the v1.8.8
   startup-race repair had moved `running:true` before the WebSocket
   attach wait, but this ONE async window remained. The E2E "wait for
   the run to settle" probe (`expect(composer).toBeEnabled()`) is a pure
   state assertion, not a transition assertion: it was legitimately
   satisfiable INSIDE the create window, and a reload landing there
   killed the pipeline before `POST /api/run` was ever dispatched. The
   session then existed server-side (pending sessions are listed), the
   history fetch returned `total:0`, and the empty-transcript surface is
   exactly what the marker assertion could not find. The repair:
   (a) the run-startup state (`running:true`, `runPhase:"preparing"`,
   bookkeeping resets, context invalidation) is set BEFORE any transport
   work; (b) the lazy create carries `keepRunState` so it can no longer
   clobber the startup state mid-flight; (c) every failure path cleans
   the startup state deterministically (a failed create is a FAILED run
   start — the composer never stays locked); (d) pending attachments are
   snapshotted before transport so the zero-session path can no longer
   drop them; (e) the E2E now waits for the deterministic "the run left
   the client" marker (the optimistic user bubble, pushed synchronously
   immediately before the run POST), waits for real settlement
   (composer unlocked AND the assistant row present), and asserts the
   DURABLE post-reload transcript (user AND assistant rows). The
   deterministic regression pair lives at the store level:
   `zero-session-send.test.ts` #6 proves `running:true` is visible
   SYNCHRONOUSLY with the create POST provably in flight; #7 proves the
   created session carries the run state forward (keepRunState) while
   the plain create path keeps its historical idle semantics; #4 now
   also asserts the failure cleanup. Chat/Agent semantics are unchanged.

2. **AI System — the first-class, user-owned run configuration object.**
   `internal/aisystem` implements the ONE AI System store:
   `systemId`, name, monotonic `revision`, instructions, model override,
   reasoning/effort preference, allowed tool surface, approval policy,
   enabled skills, knowledge refs, memory/compaction policy, runtime
   profile, verification policy, created/updated timestamps. Persistence
   follows the house pattern: one JSON document per system under
   `<DataDir>/ai-systems/`, atomic unique-temp+rename writes, bounded
   counts and sizes, deterministic (CreatedAt, systemId) ordering,
   corruption-tolerant listing, and a persisted active pointer with
   dangling-pointer repair. A fresh install receives exactly ONE valid
   default system whose behavior is byte-identical to the pre-v1.9
   runtime; existing configs migrate non-destructively (nothing outside
   the ai-systems directory is touched). Full CRUD, activation with
   correct active-system fallback (deleting the active system falls back
   to the default; the default can never be deleted), clone, export and
   import (import mints a fresh identity — never an overwrite).
   `internal/aisystem/aisystem_test.go` proves: fresh-install default,
   reload persistence of the active selection, revision increments with
   frozen-snapshot isolation, clone/export/import round trip, garbage
   rejection, reservation, validation bounds and unknown-vocabulary
   normalization, corruption tolerance, deterministic ordering and the
   snapshot JSON identity contract.

3. **Execution binding — the snapshot is frozen per run.** `handleRun`
   resolves the binding ONCE, before the run goroutine starts and before
   the response: an explicit request `systemId` wins (404 when missing —
   an honest, deterministic contract), otherwise the ACTIVE system. The
   frozen snapshot (`systemId` + `systemRevision`) rides the run as a
   value copy, so a mid-run edit can never mutate a running
   configuration. The binding is enforced server-side: instructions ride
   the system prefix as one bounded block after the briefing; the model
   override replaces the generation model at every request build; the
   reasoning preference resolves at the wire boundary (an explicit
   request level always wins; unset/auto adopts the system's preference)
   through the EXISTING v1.8.5 numeric ladder — low=0, mid=1024,
   high=4096, ultra=engine default — never a fabricated budget; the tool
   surface constrains the OFFERED and EXECUTABLE tools in both policy
   modes (`ToolPolicy.AISystemConstrain` — it can only remove, never
   widen; the Net Search intent is equally subject to it); the skills
   surface filters skill activation (irrelevant and excluded skills never
   inflate context). The frozen identity is published as an `ai_system`
   activity event and travels on the run response.

4. **Durable Goal engine.** `internal/goal` implements the long-horizon
   loop (UNDERSTAND → PLAN → DELEGATE/ACT → OBSERVE → VERIFY → REPLAN →
   COMPLETE) over durable goal state: goalId, original goal, system
   binding identity, phase (`understanding → planning → acting →
   verifying → completed`), status (active, waiting_for_approval,
   waiting_for_resource, paused, blocked, failed, completed, cancelled),
   bounded plan (≤12 steps) with per-step status/result/evidence, a
   bounded goal-level evidence journal, changed files, artifacts,
   verification state, effort, turn budget, replans, checkpoints and
   timestamps. Progress derives from actual plan state — no decorative
   percentages. Checkpoints persist after planning, after EACH completed
   step, at approval boundaries and at terminal settlement; resume
   continues from the checkpoint WITHOUT replaying committed mutations.
   Bounded replanning (default 2) revises only remaining work and keeps
   the failure evidence at goal level — a plan revision can never erase
   the failure record. Recoverable tool failures block the STEP, never
   destroy the goal. The turn budget parks the goal instead of running
   away. A model claim never completes a goal: the terminal settlement
   maps the orchestrator's objective verification report
   (`VerificationVerified`), never the model's prose. Boot recovery
   (`RecoverOnBoot`) marks any goal found live with no run behind it as
   paused — never falsely running — and is idempotent; terminal goals
   stay terminal. HTTP: `/api/goals` (list/create+start), item
   get/delete(cancel), start/pause/resume/cancel/approve/reject. The UI
   surface is the Goals card in the System Centre.

5. **Approval authority — one deterministic risk classification.**
   `internal/approval` classifies every tool call into exactly one of
   five risk classes (`read-only`, `workspace-write`, `external-network`,
   `destructive`, `privileged/host-level`) from the tool identity and
   normalized arguments — deterministic shell-fragment classification for
   destructive/privileged commands, conservative defaults for unknown
   tools. The policy vocabulary (`auto`, `ask-risky` — the default,
   `ask-all`) decides which classes ask. The EXACT normalized call
   identity (`CallKey`: tool + risk + canonical arguments) is the only
   thing an approval decision can bind to; the bounded decision ledger
   proves approvals never leak across different calls and argument order
   never changes identity. The per-run `agent.WithApprovalGate` seam is
   installed ONLY by goal runs (ordinary chat/agent runs keep the
   pre-v1.9 behavior exactly): a call whose risk class requires ask under
   the goal's AI System policy is DENIED before execution with an
   instructive evidence message — nothing risky ever executes silently.
   Goal-level approvals are DURABLE: `waiting_for_approval` + the pending
   exact call persist (reload-safe), approve resumes the exact call,
   rejection is recorded as evidence, and stale/mismatched approval ids
   are rejected explicitly. Tests: `internal/approval/approval_test.go`
   (risk matrix, policy defaults, identity binding, ledger exactness) and
   the goal approval tests.

6. **Bounded sub-agents — executable delegation alongside the advisory
   specialists.** `internal/multiagent/subtasks` adds real bounded
   delegation while the existing researcher/architect/coder/debugger/
   tester/security consultations remain advisory. Every subtask carries
   explicit budgets: role, objective, allowed tool surface, read/write
   capability, workspace scope, context bound, deadline and evidence
   requirement. Resource policy is enforced structurally: read-only
   subtasks run at most 2 simultaneously (the concurrency seam can only
   tighten the bound, never widen it), mutating subtasks run strictly
   serialized, fan-out is bounded (≤8), the executor is a leaf (no nested
   spawning is possible), and a deadline hit is an honest `blocked`
   result. Results merge DETERMINISTICALLY in `subtaskId` order — never
   in worker completion order (proven by a test whose completion order is
   deliberately reversed). Failed subtasks stay failed with unresolved
   issues; the parent receives concise structured evidence, never raw
   transcripts. The goal runner does not yet decompose steps into
   subtasks automatically — the engine is implemented, tested and
   surface-ready (honest v1.9 boundary, see ROADMAP).

7. **Repository navigation — the agentic retrieval workflow.**
   `repo_nav` (`internal/repoindex/navigation.go`, registered in the
   runtime beside `repo_search`) adds deterministic navigation actions
   over the ONE persistent index: `open` (bounded head), `navigate`
   (line/symbol context window), `read` (exact bounded range) and `grep`
   (exact substring pattern in ONE identified file, honest TOTAL match
   count with bounded content). Every result carries the source identity,
   exact line range, workspace provenance and an explicit truncation
   state; entire files are never returned (120-line and 8 KiB bounds;
   1 MiB file ceiling). All paths go through the same path-safety
   authority as `repo_search` — traversal, absolute paths, Windows volume
   and UNC forms are refused identically on Linux and Windows (proven by
   the hostile-path tests).

8. **Version, docs and release-metadata synchronization.** Every release
   surface reads 1.9.0 from the single canonical source: `package.json`,
   `package-lock.json`, `internal/config/config.go`, `build/config.yml`,
   `SIGNATURE`; `release-version.mjs --check` is green (canonical
   surfaces + workflow contract). `ROADMAP.md` is restructured to its
   mandated shape (purpose/evidence rules; the compact shipped baseline;
   the v1.9 current/future boundary; v1.10–v2.0 future; the evidence-
   driven backlog) — detailed v1.8.x release history now lives ONLY in
   this changelog (the accidental duplicated v1.8.5 section in the
   roadmap is gone with the restructure). `UPDATE.md` carries the v1.9.0
   release notes only; `../README.md` describes the current product;
   `ARCHITECTURE.md` documents the implemented v1.9 behavior;
   `agent.md` is the repaired v1.9.0 handoff.

9. **Frontend — real state, minimal UI.** The System Centre hosts the two
   new compact cards: the AI System selector/editor (create, edit with
   revision bump, activate, clone, export, import, delete — all real
   backend actions) and the Goals card (create+start, honest status chips
   derived from durable state, plan progress from actual plan state,
   pause/resume/cancel/approve/reject, live-goal polling only while a
   goal is live). View-model logic is pure and unit-tested
   (`ai-systems-view.test.ts`: deterministic selector rows, defensive
   payload tolerance, evidence-derived progress, honest state
   descriptions). The browser E2E flow (`e2e/ai-systems.spec.ts`) proves
   the default-system-first contract, create+activate through the real
   backend, activation SURVIVING A RELOAD, and the goal
   create→terminal-cancel lifecycle surviving reload — against the same
   real headless server + engine the release gate uses.

10. **Preserved v1.8 behavior.** The full frontend unit suite (220
    tests), the Go agent/api/runtime suites, `go vet`, typecheck and lint
    run green on this release; Chat, Agent, sessions, zero-session Send,
    streaming, pause/resume/abort, refresh/recovery, reasoning,
    dataAnalysis, research, repoindex, the coding Lab, the scheduler,
    custom tools, the updater/downloader, the Governor, execution
    evidence and the native engine are untouched in their authority
    structure.

---

## v1.8.0 → v1.8.8 (compact)

The staged-engine-program and hardening era, condensed per the operator's
v1.10.10 docs order (each version's essence and key invariants preserved;
v1.9.0 and later stay detailed above).

* **v1.8.8** — audit-job repair (the codename-gate fixture keys, CI run
  37446279646), release metadata synchronized to one version through the
  canonical gate, the high-severity npm advisory repaired at the exact
  chain, dataAnalysis hardening (RFC-4180 edges, deterministic ordering
  with explicit tie-breakers, join cardinality semantics, honest
  size-bound messaging).
* **v1.8.7** — deterministic Tier-2 engine discovery: a retention barrier
  makes a parallel scan a pure function of the dataset (worker-count
  independent winner; repeated-scan cache priority inversion removed;
  avoidable scan work skipped — the 126 s Windows package runtime), plus
  the dataAnalysis authority upgrade: analyze / aggregate / join /
  quality / export as five deterministic in-process pure-Go actions,
  JSON column order follows document order, the honest 256 MB bound,
  DuckDB evaluated and rejected (cgo + packaging complexity).
* **v1.8.6** — Phase 2 execution truth: the full invariant GPU detected
  ≠ available ≠ selected ≠ executed ≠ verified enforced at the ONE
  accelerator authority (ExecutionReceipt with stale-evidence
  invalidation); the GPU transaction is authoritative (auto-offload only
  on proven execution, per-boot offload evidence); /api/engine and
  /api/perf agree through one accessor with a signature-checked memo;
  the resource-aware run gate + measured engine RSS through the one
  sampler; Windows CPU telemetry via the one kernel32 GetSystemTimes
  delta seam.
* **v1.8.5** — Phase 1 runtime/user foundation: the server-side
  drop-newest bottleneck closed (conflation-aware newest-wins subscriber
  queues — a slow client never freezes visible text at a stale prefix);
  the REAL four-level reasoning ladder (low=0 / mid=1024 / high=4096 /
  ultra=unrestricted, applied through the request-level
  reasoning_budget_tokens parameter); Show/Hide Thinking (visibility
  only, never the payload); the engine off the user's critical path; the
  ONE monotone execution/evidence ladder.
* **v1.8.4** — the flush scheduler becomes self-healing on three
  independent boundaries (MessageChannel + timeout task + rAF; the
  one-shot controller wedge root-caused); the AUTO Vulkan
  stale-derived-posture circularity removed and the staged-identity
  marker makes the verification window report the binary actually on
  disk; zero-session Send creates and continues a session; monotonic
  context-refresh generation kills stale-response overwrites;
  config.Save atomicity (unique same-directory temp file per writer).
* **v1.8.3** — the Linux CI session-delete failure (Actions run
  36713108772) root-caused with instrumented evidence: the MEASUREMENT
  raced the app (count baseline sampled before the async create landed;
  product behavior was correct); the browser test repaired to state-based
  synchronization + specific deleted-id assertion; the startup
  session-list write routed through the ONE sessionListGuard;
  deterministic session-delete coverage at three layers (mutation
  verified); failed DELETEs surfaced; the created-session prepend made
  idempotent.
* **v1.8.2** — live visibility root-caused ("text visible only after
  Stop" in throttled WebView2 runtimes): the flush rides MessageChannel
  AND the animation frame behind one coalescing latch; the factual
  no-reasoning-stream label; the MemoryEvidence record published on the
  context activity; the self-model capability block (registered ≠
  enabled ≠ offered ≠ callable); identifier redaction at the ONE central
  log sink; identity-based model-card caching; README current-only.
* **v1.8.1** — the real-Windows Gemma-class local-generation crash
  repaired at the root (nil capability card → documented nil-card
  fallback + the 32 MiB GGUF metadata read bound), streamed answers
  visible in ONE render frame (the stream fast path), engine rollback
  restores the recorded identity from the restored manifest.
* **v1.8.0** — pause→resume→pause synchronization repaired
  (resumedGenerationEvidence: strictly newer run sequence AND changed
  cumulative snapshot); the honest abort marker end-to-end; the Runtime
  Governor vertical slice (resource state with explicit unknowns,
  pressure model, execution envelope, resident-budget admission — policy
  only, no second sampler); GET /api/governor + the System Centre
  Governor card.

## v1.7.0 → v1.7.6 (compact)

* **v1.7.6** — edit-transaction journals with ten-window crash/fault
  recovery matrix; restart-recovered runs claim the checkpoint revision;
  paused surface excludes journals; token-aware codename gate;
  generation-aware spec cache.
* **v1.7.5** — transactional pause/edit/resume (CAS validation, transcript
  replace, checkpoint commit, publication last); stale session-list
  generation guard; numeric-variant gates.
* **v1.7.4** — pause/edit/resume state machine, durable checkpoints, WS
  replay continuity, double-pause idempotency, stop-after-pause semantics.
* **v1.7.3** — recovery/handoff durability and lifecycle hardening.
* **v1.7.2** — scheduler settlement contract, live-protection hardening.
* **v1.7.1** — preflight gate (PREFLIGHT → REFUSE → NO ENGINE START), live
  monitor with hysteresis + cooperative critical protection, native engine
  first-class backend, context-exhaustion recovery.
* **v1.7.0** — reliability pass: engine lifecycle, hub replay, stress
  surfaces.

## v1.5 → v1.6 (compact)

Multi-agent context, memory/recall tiers, engine-variant provisioning
through the authoritative resolver, Vulkan asset validation, clone +
repository indexing, Lab workspaces, stable embedded-frontend asset
contract.

## v1.3 → v1.4 (compact)

Durable summaries, cross-mode history, task state, handoff, continuum
rollover, repository index, custom tools, update rollback safety.

## ≤ v1.2 (compact)

Foundations: sessions, streaming with sequence-stamped replay, tool
registry, downloader, installer, health checks, environment centre,
bounded authoritative run state, settlement edge.
