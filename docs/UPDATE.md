# UPDATE.md — v1.10.9 Release Notes & Maintenance Behavior

**Release:** `v1.10.9` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ a1041fe` (`v1.10.8`) · **Date:** 2026-10-16
**Package:** `SHEYTAN-LA-v1.10.9-FINAL.zip` (complete repository tree)
**R&D authority:** `Parsaetak/Contents`, branch `Research-and-Development`,
`SHEYTAN-LA-2026-10-11.md` (linked, not copied — the R&D document stays in
its source repository).

The authoritative per-release history lives in `docs/changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. v1.10.9 is the SMOKE PATH CANONICALIZATION + EVIDENCE-NAMED
WINDOWS TRANSPORT REPAIR release: run
https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38871731277
(v1.10.8, commit a1041fe) was the breakthrough run — the Windows gofmt gate
passed for the first time (the LF pin worked), the Windows go test suite ran
hosted for the FIRST time since v1.10.1 with exactly ONE failure (Windows
step 14, `TestServeMalformedRequestIsRejectedNotCrashing`, package
internal/localipc, artifact windows-go-verification-logs-attempt1), the
Linux Browser E2E passed, and the stress suite passed on its first hosted
run at the new shape. Linux step 24: the Qt smoke's core NEVER STARTED —
`SMOKE FAIL: core died at boot` with the decisive bash ENOENT line naming
the relative path executed after the cd (artifact
linux-verification-logs-attempt1). v1.10.9 fixes exactly what that run's
evidence names: the smoke path defect (canonicalize CORE_BIN/UI_BIN before
any cd), the Windows accept-path vanished-peer classification (INFERENCE:
C1 — the exact assertion-line paste was unavailable), the latent
stale-deadline-timer leak in pipeConn.Close, and the Windows failure
summary's missing assertion-line class. The v1.10.8 parent release was the
line-ending policy + E2E path repair release: run
https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38823624040 (v1.10.7,
commit 22049b9) was the best-instrumented run yet — the Visibility 2.0
annotations worked and both failures are root-caused with primary evidence.
Windows x64 failed step 13 "Go verification" at the gofmt gate, and the gate
was RIGHT: the committed tree at 22049b9 is gofmt-clean byte-for-byte (the
Linux gate is green on the same commit) — the windows-latest checkout
materialized every .go file as CRLF (runner default `core.autocrlf=true`, no
`.gitattributes`) and gofmt normalizes to LF. Linux x64 failed step 17
"Browser E2E" for one deterministic reason: `build:e2e` still built the
repository ROOT package, which the v1.10.7 reshape emptied of Go files
(reproduced locally: `go build -tags headless -o e2e/.build/sheytan-e2e-server .`
exits 1 with "no Go files"). v1.10.8 fixes the ENVIRONMENT, never the gate
(a scoped `.gitattributes` pinning `*.go text eol=lf` plus a Windows
pre-Checkout `core.autocrlf=false` pin — the gofmt gate byte-identical),
repairs the E2E build target to `./cmd/sheytan-local-agent` (proven by
reproduction: the full stack passes 40/40 Playwright specs locally), extends
the visibility system to the last unseen gate (E2E tee + failure summary),
teaches the smoke summary to distinguish never-started from died-at-gate,
and closes the reshape's missed-reference class (verify-all.mjs + sweep
proof).

## What v1.10.9 changes

1. **L-A — the smoke path defect, fixed at the root.** The workflow passes
   `CORE_BIN` as a repo-root-relative path; the smoke validated it from the
   repo root, then `cd`s into its work directory and executed the
   now-dangling relative path (bash ENOENT → "core died at boot"; the
   smoke's real gates G1–G5 never executed hosted). The smoke script now
   canonicalizes `CORE_BIN` and `UI_BIN` to absolute paths at resolution
   time, BEFORE any cd — validation, the pkill patterns, and the exec all
   reference the same file regardless of working-directory changes; a
   missing canonical path fails loudly at resolution instead of ENOENTing
   at the exec. Proof: a two-line mechanism reproduction (relative + cd +
   exec → exit 127; canonicalized → exec succeeds), a tree-wide sweep (the
   smoke is the ONLY such site), and a workflow-shaped local smoke run in
   which the core BOOTED past the cd (the endpoint line in core.log) —
   honestly bounded at G2 because the Qt toolchain/openbox/xdotool are
   absent in the Linux working environment (the core's `none` sentinel
   stood in for the Qt host; no gate result was claimed that did not
   execute).
2. **W-A — the Windows transport, evidence-named root cause (INFERENCE:
   C1).** `Accept` treated only `ERROR_NO_DATA` as the transient
   client-gone race; the rest of the vanished-peer completion family
   (`ERROR_BROKEN_PIPE`, `ERROR_PIPE_NOT_CONNECTED` from the overlapped
   connect completion) was fatal — it killed `Accept`, `http.Serve`, and
   the whole endpoint, and the malformed-garbage test (the only test whose
   client closes WITHOUT reading) failed at its instant health redial.
   `Accept` now classifies the whole family as transient via
   `isVanishedPeer`; genuine endpoint faults (`ERROR_OPERATION_ABORTED`
   outside the W3 close path, everything else) stay fatal. The
   malformed-garbage test is UNTOUCHED — no assertion weakened.
3. **W-B — the stale deadline-timer leak (latent, fixed regardless).**
   `pipeConn.Close` never stopped the timer `SetReadDeadline` arms for
   future deadlines (`time.AfterFunc(..., CancelIoEx(handle, nil))`); a
   stale timer could fire on a RECYCLED handle value and abort an
   unrelated live connection's IO. `Close` now stops the timer under the
   deadline mutex and clears the deadline (disarming an already-running
   callback's re-check). Regression pins:
   `TestAcceptVanishedPeerClassification`,
   `TestPipeConnCloseStopsReadDeadlineTimer` (executed by the hosted
   Windows job; compile-verified locally — Linux-only environment).
4. **V-A.2 — the Windows failure channel carries statements.** The "Go
   verification failure summary (Windows)" filters now ALSO match the Go
   assertion class (`\.go:[0-9]+: ` — `serve_test.go:287: …` statements and
   `file.go:NN:` compile errors), capped/truncated/no-secrets as before.
   The next red Windows run publishes its failing STATEMENT anonymously.

## What v1.10.8 changes

1. **W-A — the repository line-ending policy + the Windows checkout pin.**
   New scoped `.gitattributes`: `*.go text eol=lf` (all 548 tracked .go
   files are `i/lf` in the index — zero renormalization, the pin only
   makes the checkout gofmt observes the SAME tree on Windows as Linux
   already sees) plus one justified additional line, `*.bat text
   eol=crlf` (the launcher's goto-label control flow needs CRLF, and the
   new checkout pin would otherwise have changed its historical CRLF
   materialization to LF). The Windows job pins
   `git config --global core.autocrlf false` BEFORE Checkout, asserting
   the pin took. The gofmt gate itself is byte-for-byte untouched — fix
   the environment, never the gate. Local mechanism proof: `gofmt -l .`
   empty on the LF tree; CRLF injected into a working-tree file makes
   `gofmt -l .` name exactly that file; restored → clean; check-attr
   reports the pins.
2. **E-A — `build:e2e` builds the real core entrypoint.** The go build
   target becomes `./cmd/sheytan-local-agent` (the reshape left the root
   package empty; the old command exits 1 with "no Go files" —
   reproduced before the fix, exit 0 after). The full E2E stack is
   proven locally: engine built (13/13 CTest), fixture generated,
   Playwright chromium provisioned — `npm run test:e2e` 40/40 specs
   against the real frontend + real headless server + real engine
   generation. No second defect surfaced behind the build path.
3. **V-B — CI Visibility 3.0 on the Browser E2E gate.** The E2E step
   tees `build:e2e` → `ci-logs/e2e-build.log` and the Playwright run →
   `ci-logs/e2e-playwright.log` (pipefail discipline); a new "E2E
   failure summary (Linux)" step (if:failure()) emits capped ::error::
   annotations with failing spec names + error lines (the Windows Go
   verification summary pattern), appends log tails to the step
   summary, and reports a missing Playwright log as "failed BEFORE
   Playwright ran — the build:e2e stage is the failing gate". The
   existing Upload verification logs step carries both logs.
4. **V-A — the smoke summary distinguishes never-started from
   died-at-gate.** When the smoke's work directory does not exist, the
   summary emits "The Qt smoke never started — the failure is upstream
   of the smoke" and skips the gate-marker logic (run 38823624040's
   false-lead class); with the directory present the v1.10.7 behavior
   is byte-for-byte unchanged.
5. **R-A — the missed-reference class closed.** verify-all.mjs's Qt
   acceptance block builds the core EXPLICITLY from
   `./cmd/sheytan-local-agent` and points `SHEYTAN_CORE_BIN` at the
   produced binary; the skip hint teaches the working command instead
   of the dead `go build -o sheytan .`; `/sheytan` joins .gitignore
   (anchored; the releasegate no-swallow test still passes). Sweep
   proof: the only stale root-package build/run reference in the tree
   WAS that block — no other exists.
6. **Preserved gains.** The W1–W4 + W-C pipe transport repair, the
   visibility install, the gofmt gates on both platforms, the repository
   reshape, the Wails absence and the stress-gate contracts all remain
   byte-for-byte; no test was weakened, no gate deleted, no timeout
   expanded, no default flipped.

## What v1.10.7 changes

1. **W-TRANSPORT — the Windows client pipe handle now satisfies the
   overlapped-IO contract.** `dialEndpoint`
   (internal/localipc/listener_windows.go) opened the CLIENT side of the
   named pipe with `dwFlagsAndAttributes = 0` — a SYNCHRONOUS handle —
   while `pipeConn.Read/Write` issue overlapped IO (Overlapped structs,
   events, `GetOverlappedResult`) on EVERY connection, client-side
   included. Issuing overlapped IO on a handle not opened with
   `FILE_FLAG_OVERLAPPED` is outside the Windows contract (MSDN: the
   results are unpredictable); the canonical pipe client (x/sys winio
   `DialPipe`) opens with `FILE_FLAG_OVERLAPPED` for exactly this reason.
   The defect is present since v1.10.1 (verified in `git show cb11a23`) —
   matching the six-run Windows-only failure whose Linux counterpart
   (`net.Listen` UDS transport) never touches this path. Evidence class:
   read-verified source defect + boundary (the run-38731387959 failing
   test names are inside the auth-walled failure artifacts; the
   anonymous annotation channel carries bare exit codes only — see Run
   truth below).
2. **W-DEADLINE — read deadlines are now enforced while a read is
   pending.** `SetReadDeadline` only cancelled in-flight overlapped reads
   when the deadline was ALREADY expired at set time; a future deadline
   never fired during a pending `ReadFile` (the wait is unbounded), which
   made `http.Server`'s `ReadHeaderTimeout: 10s` (serve.go) dead code on
   the Windows transport — a silent peer hung a read forever. A future
   deadline now arms a bounded `time.AfterFunc` cancel (`CancelIoEx`),
   and an aborted read answers `os.ErrDeadlineExceeded` when the deadline
   has passed. Evidence class: read-verified contract violation in the
   same file.
3. **L-SMOKE — the dependency set the xcb platform plugin loads is now
   pinned explicitly.** The first-hand reproduction of the smoke on
   Qt 6.8.2 + Xvfb + openbox + software rendering showed the EXACT
   line-55 failure shape: a UI platform-plugin load failure kills the UI
   at boot, the core exits with it, and the smoke dumps
   core.log + ui.log before `SMOKE FAIL` — the v1.10.5/v1.10.6 signature.
   With the full xcb dependency set present, ALL GATES PASS first-hand on
   the same Qt minor as CI (G1 handshake, G2 core-spawned host, G3 real
   1280x840 visible window, G4 session round-trip + channel sockets, G5
   WM_DELETE close) — the v1.10.6 hardening is verified sound by
   EXECUTION, not only inspection. The workflow's apt step now installs
   the complete canonical xcb set (cursor, icccm, keysyms, image,
   render-util, render, shape, randr, shm, xfixes, xinerama, xinput,
   xkb, sync, dri3, xkbcommon) so the plugin's dependency closure is
   deterministic, not image-luck. G1–G5 stay byte-for-byte as strict.
4. **VISIBILITY 2.0 — the next red run is machine-readable without any
   login.** Every failure-summary step (audit, Windows Go verification,
   Linux smoke) now ALSO emits `::error::` workflow-command annotations:
   Windows names the failing command and the first failing test names;
   Linux names the failing gate (the smoke now writes a
   `smoke-fail.txt` marker on EVERY abort path) plus bounded ui.log /
   core.log tails; audit names the failing gates and test names. Caps
   respect the 10-annotations-per-step limit; long lines truncate; no
   secrets (the same lines already publish to the public step summary).
   The check-run annotations API is anonymously readable — verified
   against run 38731387959 this cycle.
5. **GOFMT GATE — hygiene joins the battery on BOTH platforms.** The
   editing pipeline had mangled tabs to spaces across 143 Go files (the
   register named three; `gofmt -l` proved the damage tree-wide). The
   tree is reformatted (whitespace-only, builds byte-identical), and
   `gofmt -l .` output must now be EMPTY inside both Go verification
   steps — a violation fails the job, names every offending file in its
   own log (`win-gofmt.log` / `linux-gofmt.log`), and emits the first
   files as `::error::` annotations. No gate was weakened: the battery
   grew.
6. **REPOSITORY SHAPE (operator specification).** All documentation
   moved into `docs/` (ARCHITECTURE.md, CONTRIBUTING.md, ROADMAP.md,
   UPDATE.md, agent.md, changelog.md, worklog.md); the root shows only
   README.md, LICENSE.md and SECURITY.md as visible documentation files;
   the core entry moved into `cmd/sheytan-local-agent/`
   (main.go, main_headless.go, main_other.go, main_stderr.go,
   main_windows.go — the build-tag suffix contracts preserved); the
   launcher script source moved to `packaging/SHEYTAN-LA.bat`. Every
   reference was swept in the same change set: the workflow's Windows and
   Linux build commands, the headless smoke build, the smoke's core
   fallback build, the Windows staging step (agent.md → AI-CONTEXT.md,
   SHEYTAN-LA.bat), the audit job's repository-shape check (now
   two-sided: new paths must exist, old root paths must be GONE), the
   stress gate's release-surface scenario (same two-sided philosophy,
   plus the workflow build-path strings), the license classification
   table (internal/brand + the regenerated LICENSE.md), the license
   contract test's allowed-references map, README and cross-document
   links, and the native engine's documentation references. The
   build-mandated root files stay (justified per-file in
   `docs/worklog.md`): go.mod, go.sum, package.json, package-lock.json,
   index.html, vite.config.ts, tsconfig.json, tsconfig.app.json,
   tsconfig.node.json, SIGNATURE, .gitignore, .oxlintrc.json,
   .prettierignore — the Go/npm/Vite/release contracts.
7. **Preserved gains.** Strict handshake, field pins, stress-gate Qt
   contract, WS fragmentation reassembly, the W1–W4 pipe transport
   repair, the failure-visibility install and the ws purge all remain
   byte-for-byte; no test was weakened, no gate deleted, no timeout
   expanded, no default flipped.

The v1.10.6 notes below remain the parent-release record: the W1–W4
transport contract, the L1–L3 smoke hardening, the failure-visibility
install and the legacy purge they describe are unchanged in v1.10.7.

## What v1.10.6 changes

1. **W1 — the endpoint now exists before `Serve` returns.**
   `listenEndpoint` creates the first pipe instance synchronously;
   an immediately dialing client can no longer hit
   `ERROR_FILE_NOT_FOUND`. Unix bind semantics, mirrored.
2. **W2 — the endpoint no longer dies after 5 idle seconds.** `Accept`
   waits on the overlapped connect indefinitely; the former
   5000 ms fatal timeout (which killed `http.Serve` — and the whole IPC
   endpoint — after 5 idle seconds) is gone. `connectTimeoutMS` no
   longer exists.
3. **W3 — a clean `Stop` cannot hang.** `Close` cancels a pending
   overlapped accept via `CancelIoEx` (the pending state is published by
   the listener) and signals through the closed channel.
4. **W4 — the identity path is tested; the re-dial races are closed.**
   The SID decision is the pure, fail-closed `sidAllowed` (own token SID
   allowed, foreign denied, empty denied), covered by windows-only
   regression tests; a fresh instance is created ahead of demand so the
   malformed/oversized re-dials (and the real UI's rapid requests) never
   race instance creation; `TestCredAllowedDeniesForeignUid` documents
   its honest platform split instead of passing vacuously.
5. **L1–L3 — the smoke proves the UI on a headless runner.** Software
   rendering is forced for the UI process; the smoke captures the REAL
   UI's output into its own `ui.log` (via a wrapper that execs the real
   binary unchanged — same PID, same env) and prints it on every failure
   path; openbox readiness is the bounded `_NET_SUPPORTING_WM_CHECK`
   event poll, not a fixed sleep. G1–G5 are byte-for-byte as strict as
   before.
6. **CI failure-visibility — the next red run is readable.** Every
   Go-verification command (Windows ×3, Linux ×3, audit race gate) tees
   to its own log; `if: failure()` steps append failing test names +
   200-line tails to `$GITHUB_STEP_SUMMARY` and upload the full logs.
   The smoke's logs (core, ui, xvfb, wm, engine) are summarized and
   uploaded the same way. No secrets, keys, runIds or sessionIds are
   ever written to these outputs.
7. **Legacy purge with proof.** `ws@8.22.0` removed (zero imports
   tree-wide; lockfile regenerated; `npm ci` clean; 0 vulnerabilities —
   the GHSA class is dead); `e2e/repro-stream.mjs` deleted (asserted
   nothing, referenced by no entry point; the shared fixture survives);
   the Wails sweep found only gates, origin-rejection security code,
   retired-era comments and protected historical mentions.

## Run truth and evidence classes

**v1.10.8 hosted truth (the operator pushes; this environment cannot):**
run https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38823624040
(v1.10.7, commit 22049b9) — the Visibility 2.0 annotations WORKED: the
Windows gofmt gate named eight cmd/ files (the first eight alphabetically —
a listing artifact of a CRLF checkout, not eight violations; the committed
tree is gofmt-clean byte-for-byte and Linux's identical gate is green on the
same commit), and the Linux job died at step 17 "Browser E2E" with a bare
exit code 1 while the smoke summary step was SKIPPED (the smoke never
started — the false-lead class V-A now names). The Windows go test suite
has STILL never run hosted at the repaired transport (W1–W4 + W-C): no
hosted run since v1.10.1 has executed the Windows go tests, and v1.10.8
does not claim otherwise. The next hosted run is the FIRST-EVER hosted
execution of the repaired Windows transport, the line-ending policy at the
Windows checkout, the E2E at the new layout, and the reshaped stress gate.
Local evidence for v1.10.8's own fixes: both root causes reproduced BEFORE
the fix (CRLF mechanism demonstrated on a working-tree file; the dead
e2e build command's exact exit 1 + stderr), the repairs proven AFTER
(build:e2e exit 0; test:e2e 40/40; gofmt clean; check-attr pins verified),
and the full local battery green — see `docs/worklog.md` for the gate
table. No hosted green is claimed for v1.10.8 anywhere in this repository;
v1.10.8's OWN hosted result is recorded here only after the operator's
push executes the run.

**v1.10.7 hosted truth (parent record; the operator pushes; this environment
cannot):**
run 38731387959 (v1.10.6, commit 48cee27) — audit SUCCEEDED (3m7s),
Windows x64 FAILED at step 13 "Go verification" (8m18s; step-13 output
line 138), Linux x64 FAILED at step 23 "Qt desktop acceptance smoke (real
core + Qt UI under Xvfb)" (9m22s; step-23 output line 55), publish job
skipped. Both failing jobs' check-run annotations read anonymously this
cycle carry exactly one error each: "Process completed with exit code 1."
— bare exit codes, no test names, no gate names; the two failure
artifacts (windows-go-verification-logs-attempt1, 1.53 KB;
linux-verification-logs-attempt1, 3.11 KB) are auth-walled from a
credential-less environment. v1.10.7's fixes therefore carry an honest
evidence split: the Windows transport and deadline defects are
read-verified in source (present since v1.10.1, matching the six-run
Windows-only failure split), the Linux smoke gates G1–G5 are verified
GREEN first-hand on the same Qt 6.8.2 minor with the full battery
locally (build, vet, suites, race, stress 47/47, npm, smoke), and the
consumed failure-summary contents are NOT quotable from this
environment — the boundary is recorded in `docs/worklog.md`. v1.10.7's
OWN hosted result is recorded here only after the operator's push
executes the run; until that run is green, no hosted green is claimed
for v1.10.7 anywhere in this repository.

* Windows x64 failed step 13 "Go verification" in FIVE consecutive
  hosted runs through run
  https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38714950052
  (v1.10.5, commit 8d8e660), and again in run 38731387959 (v1.10.6,
  commit 48cee27) — six in total. The hosted log is admin-gated; the
  defect register verified at HEAD names the cause set above. The
  v1.10.6/v1.10.7 transport repair is local evidence:
  `GOOS=windows` build + vet clean and the defect classes fixed by
  construction — it is NOT hosted green, and no hosted green is claimed
  for v1.10.6 anywhere in this repository. The repair environment has no
  GitHub credentials, so this release could not push or execute a hosted
  run; the failure-visibility install exists precisely so the NEXT
  hosted run publishes its own diagnosis.
* Linux x64 failed step 23 "Qt desktop acceptance smoke" on the smoke's
  first hosted execution (same run). The hardening above is the
  software-rendering + real-UI-logs + event-based-readiness answer; the
  G1–G5 gates themselves are unchanged.
* Physical-hardware claims are unchanged from v1.10.4/v1.10.5: GPU
  runtime behavior remains PHYSICAL HARDWARE UNVERIFIED in CI; the
  Windows Qt GUI window proof remains a hosted-runner-only evidence
  class (WINDOWS QT NOT VERIFIED outside the hosted job that runs it).

The v1.10.5 notes below remain the parent-release record: the live-stream
fragmentation fix, the OpenGL dev-stack dependency fix and the Qt host
build reality they describe are unchanged in v1.10.6.

## What v1.10.5 changes

1. **P0 — the Linux `Build Qt host (Linux)` dependency gap is closed
   (run 38584769607, step 22).** Reproduced locally with the workflow's
   EXACT cmake/ninja command against the pinned Qt 6.8.2 aqtinstall
   layout: `Could NOT find OpenGL (missing: OPENGL_opengl_LIBRARY
   OPENGL_glx_LIBRARY OPENGL_INCLUDE_DIR)` → `WrapOpenGL` → `Qt6Gui` all
   NOT FOUND. The workflow installed only the GL runtime (`libgl1`,
   `libegl1`); CMake's FindOpenGL needs the development headers and
   link-time `.so` symlinks. The workflow now installs `libgl-dev`,
   `libglx-dev`, `libopengl-dev`, `libegl-dev`, `mesa-common-dev` and
   `libxkbcommon-dev`. Fail-before/pass-after: configure fails before,
   configure + build exit 0 after, `sheytan-ui` and
   `sheytan-ui-acceptance` produced at the paths the later steps copy,
   and the full Qt live-answer acceptance suite passes against the real
   core (14/14).
2. **P0 — the Qt host activity stream survives fragmentation
   (RFC6455 §5.4).** A raw-socket probe over the IPC channel proved the
   core's activity hub fragments every activity event larger than its
   1024-byte gorilla write buffer (fragment 1024 bytes FIN=0 +
   continuation FIN=1). The v1.10.3 client policy refused all
   fragmentation and closed the stream mid-run, so `liveText()` never
   grew on real generations — the acceptance
   `chatStreamPersistRepeatAndClose` failed while the same run settled
   `done` with 20 delta events server-side. The client now reassembles
   fragmented text messages (bounded by kMaxWsFrame, fragment state
   cleared on close); protocol violations still fail loudly with typed
   errors. The contract test moved from `wsRefusesFragmentedFrames` to
   `wsReassemblesFragmentedMessages` — revised, not weakened: the
   orphan-continuation refusal it also pins is intact.
3. **Windows `Go verification` evidence class — unchanged and honest.**
   The exact step-13 commands pass locally with the step's exact
   environment (`CGO_ENABLED=0`: internal headless 64 packages ok, full
   `./... -run Test` 65 packages ok, `go vet ./...` clean) and
   `GOOS=windows` type-checks every package including test files. The
   hosted step log is admin-gated (403 for anonymous API callers), the
   failing Windows-host assertion therefore remains UNIDENTIFIED, and
   the hosted Windows job stays NOT VERIFIED until a push proves it.

## What v1.10.4 changes

1. **P0 — the Linux `release_surface` stress gate is retargeted to the
   current CI contract (run 38569081029, step 18).** The scenario still
   REQUIRED the retired Wails-era packages (`libgtk-4-dev`,
   `libwebkitgtk-6.0-dev`) that v1.10.1 removed from the workflow when
   the desktop became the native Qt client — the gate failed on every
   v1.10.1+ hosted run while the workflow itself was correct (the
   "stress scenario assumes a retired transport" class). The pin now
   REQUIRES the Qt-era dependency set the workflow actually installs
   (libx11-dev, the four libxcb runtime pieces, libxkbcommon-x11-0,
   openbox, xvfb, xdotool, x11-utils) and BANS the whole retired
   GTK/WebKit stack in both Wails generations (libgtk-3-dev,
   libwebkit2gtk-4.1-dev, libgtk-4-dev, libwebkitgtk-6.0-dev).
   Coverage is STRENGTHENED (11 must-exist pins instead of 2, 4 banned
   packages instead of 2). Fail-before/pass-after: the exact CI command
   (`go build ./scripts/stress-main` + `stress`) exited 1 with
   `release_surface FAIL … libgtk-4-dev missing` before the fix and
   passes 47/47 with exit 0 after it, plus a clean `-race` pass on the
   stress-exercised packages.
2. **P1 — the Windows `Go verification` evidence class is stated
   honestly (run 38569081029, step 13).** The step's full log is NOT
   retrievable without repository-admin authentication (the Actions
   log-download API refuses anonymous access; unauthenticated API calls
   were rate-limited all session). Everything provable without the log
   was proven at b0b3aba and re-proven at v1.10.4: `GOOS=windows`
   compile AND full test-binary LINK of every package (±headless tags,
   CGO_ENABLED=0 — the exact step-13 environment) is clean; `go vet`
   under the same cross-compilation is clean; the IDENTICAL three
   commands pass on Linux CI; and every test file changed since v1.10.1
   was reviewed for Windows-runtime portability with no defect found.
   The failing assertion therefore cannot be identified from this
   environment, and v1.10.4 makes NO speculative Windows change and
   NO test weakening. WINDOWS GO VERIFICATION remains NOT VERIFIED
   until a hosted rerun; the run's own log is the arbiter.
3. **P2 — documentation truth alignment.** The architecture doc now
   marks the v1.9.5 loopback activity transport RETIRED (the Qt desktop
   mounts no loopback WebSocket listener) and marks the Qt/IPC seam
   SHIPPED (since v1.10.1) instead of "defined, not shipped"; the
   roadmap names v1.10.4 as the shipped baseline; the stress-main
   header comment no longer describes the removed Wails shell;
   identity surfaces are consistent at 1.10.4.

## What v1.10.3 changed (parent release — unchanged behavior)

1. **P0 — CoreClient HTTP + WebSocket framing repaired and pinned.**
   Content-Length completion (no hang when the server holds the socket
   open), close-delimited completion kept, oversized heads/bodies
   refused deterministically, strict handshake (name AND version=1),
   percent-encoded id segments, typed errors on every silent path, WS
   control-frame policy (ping→pong, close→close-reply + teardown,
   fragmentation refused), and the frame loop no longer starves frames
   behind a control frame. Ten new QtTest slots
   (`ui/qt/tests/tst_acceptance.cpp`) prove the framing contract
   byte-level; the two REAL-core slots prove the live-answer cycle.
   Before/after evidence: on the v1.10.2 client 9 of the 10 framing
   slots FAIL (including the R17 accept-a-v2-core behavior); on
   v1.10.3 all 14 slots PASS against the real core.
2. **P1 — the sessions wire contract is parsed as the core serves it.**
   GET /api/sessions is a BARE array of stubs (`id`, `title` omitempty,
   `msgCount`) — the v1.10.2 client expected a wrapped envelope and a
   `messageCount` field, both hidden by the silent malformed-skip, so
   the sidebar never loaded and every session showed "0 messages".
   Fixed in the client and QML; pinned by three new Go contract tests
   (`internal/api/qt_field_contract_test.go`).
3. **P1 — smoke + CI runnability (R1/R2/R3/R4/R16) and launcher order
   (R13).** fail-before-define aborted correctly reproduced and fixed;
   traps/pkill patterns/WINMGR resolution hardened; G3 refuses without
   a WM; CI installs libx11-dev + openbox, stages the UI to the exact
   UI_BIN path, passes the REAL core name; the Windows acceptance
   staging resolves Release/ first and throws if absent; the explicit
   SHEYTAN_UI_EXECUTABLE override wins over next-to-core (pinned).
4. **P1 — measurement honesty (R5/R6/R10).** Membership-only host
   evidence assert (the TF_BUILD=1 repro failed before the fix); the
   stale readCodeSHA comment corrected; the artifact re-measured with
   the code SHA stamped and a trailing newline — quoted below
   character for character.
5. **P2 — identity + docs (R11/R12/R14/R15).** Canonical 1.10.3 via
   `node scripts/release-version.mjs` + `--check`; ws "smallest
   compatible fix" reworded in place (upstream fixed in 8.20.1 and
   8.21.0; 8.22.0 chosen; dependency unchanged); Go space-indent
   convention recorded in agent.md; the "CI calls verify-all.mjs"
   claim corrected (CI gates are inline); per-item evidence appended
   to worklog.md.

## Current measured baseline (honest, v1.10.3 re-measurement)

Recorded in this session (virtualized Linux container session host —
NOT physical consumer hardware and NOT GitHub Actions hosted CI;
artifact: `baselines/perf-hosted-v1.10.2.json`, declared host class
`hosted`, `SHEYTAN_PERF_CODE_SHA=1d3b1f2e0ece271f9eac0656788368638062366a`
stamped, method + limitations + honesty notes embedded):

* `cold_start_to_interactive_ms`: MEASURED — 1.339 ms (api.New +
  EnsureSetup boundary on the hosted host; the GUI shell adds its own
  window cost, measured by the desktop acceptance surface). Target
  < 800 ms is met on THIS host only — not a product-wide claim.
* `idle_memory_mb` / `peak_memory_mb`: MEASURED — 14.3203125 MB (process
  RSS/VmHWM, server-only posture; excludes the desktop shell process).
  The < 80 MB preference is met on THIS host only.

---

# v1.10.2 release notes (historical — superseded by v1.10.3 above)

The v1.10.2 notes below are kept as history; where they describe the
CURRENT release behavior they are superseded by the v1.10.3 sections
above. The v1.10.2-era measured-baseline section further down carries
the R10 correction narrative.

## What v1.10.2 changes

1. **P0 — the Windows Go verification gate now COMPILES (`internal/
   localipc`).** The contract-test helper `credAllowed` lived in
   `listener_unix.go` behind the `!windows` build tag while
   `serve_test.go` used it unconditionally, so on Windows `go vet` and
   `go test ./...` (the exact gate the Windows job runs) failed to BUILD
   the package — the suite could not even reach its assertions. The
   helper moved to the platform-neutral `endpoint.go` (the Windows
   transport uses the DACL + client-SID check instead and simply never
   calls it), so the full suite builds and runs on both platforms.
   Reproduced before (`GOOS=windows go vet ./internal/localipc/` →
   `undefined: credAllowed`) and green after; `GOOS=windows go build
   ./...` clean end to end. No test was weakened — the pure-decision
   test now runs on Windows too.
2. **P1 — the Linux desktop smoke owns its whole environment.**
   `scripts/qt-desktop-smoke.sh` no longer assumes machine-specific
   state: X11 helpers come from the system PATH (opt-in `XTOOLS`/
   `XLIBS` overrides remain), the `closewin` WM-request tool is built
   from the tracked `scripts/closewin.c` by the smoke itself, the core
   binary is built from the repo when not staged, the UI binary falls
   back to the developer build tree, an Openbox (or equivalent,
   `WINMGR_BIN`) window manager is started when present — with its
   private-prefix XDG dirs resolved so themes load — and every spawned
   process is tracked and killed on ANY exit path (trap-based).
   G1–G5 passed under a real WM: keyed handshake, core-spawned UI pid,
   visible 1280x840 top-level, real core interaction, clean
   WM_DELETE_WINDOW exit, no orphan, no leaked endpoint. The gates
   themselves are unchanged.
3. **P1 — the AI Systems list/activation surface moved to Qt** — the
   highest-value remaining surface that could meet the evidence bar
   inside this release. `CoreClient` gains `systems` /
   `activeSystemId` properties and `refreshSystems()` /
   `activateSystem(id)` over the EXISTING `/api/systems` handlers
   (GET list, POST activate) — no second store, no second authority;
   validation, persistence and the reserved default stay server-owned.
   `Main.qml` gains an AI Systems drawer (list, active badge, activate,
   refresh). The acceptance suite gains `aiSystemsListActivate`:
   seeded list arrives typed, activation flips the authoritative
   activeSystemId, an unknown id surfaces the typed 404, and the
   authoritative state does not change. The remaining surfaces (AI
   Systems editing/import, Goals, Coding Lab, Net Search, repository
   index, tasks/automations, downloads/diagnostics) stay on the
   documented headless/web mode — migrating them did NOT fit the
   evidence bar in this budget and is not claimed done.
4. **P1 — measurement host-classification is fail-closed.** The old
   `--hosted-ci` bool defaulted to true and could be silently swapped
   in either direction. `cmd/shtn-perf` now REQUIRES `--host-class
   physical|hosted` (no default: an unclassified run is refused),
   refuses a physical claim on any host carrying well-known hosted-CI
   markers (GITHUB_ACTIONS, GITLAB_CI, TRAVIS, CIRCLECI, BUILDKITE,
   JENKINS_URL, TF_BUILD, CI), and derives the schema-compat
   `hostedCI` flag from the single declared class so the artifact can
   never disagree with itself. Artifacts additionally record RAM,
   hostname, the detected-evidence list and caller-supplied honesty
   notes. New pins in `internal/perf/perf_test.go` cover refusal on
   empty/unknown classes, the dangerous direction, and the envelope
   round-trip. Committed artifact:
   `baselines/perf-hosted-v1.10.2.json` (hosted session host, plainly
   labeled — NOT GitHub Actions, NOT physical hardware).
5. **P1 — high-severity `ws` advisories closed.** GHSA-58qx-3vcg-4xpx (uninitialized memory
   disclosure) and GHSA-96hv-2xvq-fx4p (memory-exhaustion DoS) affected
   the direct devDependency ws 8.18.3. Bumped to 8.22.0 (same major,
   no `--force`); `npm audit --audit-level=high` is clean and the full
   frontend gate stack stays green. (v1.10.3 correction: the
   "smallest compatible fix" claim was wrong — the advisories were
   already fixed upstream in ws 8.20.1 and 8.21.0; 8.22.0 was chosen,
   not the smallest. The dependency is unchanged.)
6. **P2 — documentation/version reconciliation.** Canonical identity
   1.10.2 via `scripts/release-version.mjs` (package.json source;
   config.go, build/config.yml, SIGNATURE repaired; `--check` green);
   `ui/qt/CMakeLists.txt` project VERSION bumped in the same pass;
   README current-release block (stale 1.10.0 wording), ARCHITECTURE.md
   desktop-lifecycle paragraph, ROADMAP Phase 0/2 status lines,
   changelog.md, agent.md and this file updated. No claim anywhere that
   the remaining surfaces are already on Qt; the v1.9 future-boundary
   items (goal-to-subtask fan-out, anti-hack guard, MCP registry) stay
   FUTURE.

## v1.10.1 notes (superseded where they describe the current release)

1. **P0 — Windows test teardown repaired (run `38152579695`,
   `TestWebSocketSeesTerminalErrorNotSilence`).** The test returned at
   the terminal error frame while the run goroutine's deferred cleanup
   was still writing inside the data dir; on Windows `t.TempDir`'s
   RemoveAll then failed with "The directory is not empty" and failed
   the whole `internal/api` package (~91 s in, blocking every later
   Windows gate). The deterministic repair: the test waits on the
   authoritative outcome and on the run registry RELEASING the session
   entry before teardown, and `Server.Close` JOINS every run goroutine
   (bounded) before releasing the runtime stack; new run/resume
   registrations are refused once shutdown has begun. No assertion was
   weakened and no deadline was inflated.
2. **P0 — Linux GUI smoke repaired by the Qt migration itself (run
   `38152579695`).** The Wails 1x1-window failure is not reproducible in
   the new stack: the Qt desktop smoke under Xvfb proves a real visible
   top-level window with plausible geometry owned by the UI pid (the
   exact assertion class the Wails shell failed), keyed IPC handshake,
   real core interaction and a clean WM_DELETE_WINDOW shutdown.
3. **The desktop UI is now native Qt Quick/QML** (`ui/qt`) consuming the
   UNCHANGED Go core over `internal/localipc` (per-user Unix socket /
   named pipe, SO_PEERCRED or current-user DACL + client-SID checks,
   versioned keyed handshake, bounded framing, structured errors). The
   shipped Qt surfaces: sessions, chat, live streaming with Stop,
   settlement/persistence/reload, repeat send, composer lock/unlock,
   error/empty/connecting states. The remaining UI surfaces continue to
   be served by the documented headless/browser mode until their Qt
   migration lands (tracked as future work; not claimed done).
4. **Wails is removed** from the product, build, dependency graph and
   current-product docs; the module graph is asserted Wails-free in CI.
   Historical release records that mention Wails remain (deleting them
   would falsify release history); no Wails code or dependency remains.
5. **One canonical test entrypoint:** scripts/verify-all.mjs (CI-called,
   fail-fast, honest NOT RUN reporting). The Windows CDP live-answer
   harness is replaced by the QtTest acceptance suite running the real
   QML UI against the real core.

## v1.10.0 notes (superseded where they describe the Wails shell)

The v1.10.0 sections below describe the v1.10.0 release as it shipped;
statements about the Wails desktop shell, its CDP acceptance switch and
the Openbox smoke are now historical (that release is still v1.10.0 in
the canonical history), and the current maintenance behavior for those
areas is the one described above.

1. **P0 — the Windows live-answer acceptance failure is repaired at its
   root: the CDP test switch could never reach WebView2.** The
   authoritative run `38135751846` ("v1.9.6" #120) failed the
   `Windows live-answer acceptance` step (~75 s: ~15–25 s preparation +
   the harness's full bounded 60 s CDP attach window, exit 1) AFTER a
   green desktop runtime smoke — the first time this gate ever executed
   (every earlier release failed upstream). Root cause, derived from the
   pinned wails v3.0.0-beta.16 sources (the CI log payload itself is not
   retrievable unauthenticated): the harness passed the WebView2 remote
   debugging switch through the `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`
   environment variable, but Wails' default pure-Go WebView2 loader
   (`internal/webview2/webviewloader`, build tag `windows &&
   !native_webview2loader`) runs `preventEnvAndRegistryOverrides()` at
   package init and executes
   `os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "")` — the switch
   was erased inside the app process BEFORE any WebView2 environment was
   created. The CDP endpoint never opened; the harness failed honestly.
   The repair routes the switch through the one channel Wails composes
   itself and hands to `CreateCoreWebView2EnvironmentWithOptions`:
   `application.Options.Windows.AdditionalBrowserArgs`. The desktop shell
   reads `SHEYTAN_ACCEPTANCE_CDP_PORT` (digits-only, bounded shape check)
   and forwards `--remote-debugging-port=<port>` programmatically
   (`internal/desktop/desktop.go`, `acceptanceCDPPort`); the harness sets
   exactly that variable. The switch stays TEST-ONLY: production
   launchers never set it, production builds never expose a debugging
   endpoint. The harness's CDP-timeout failure now carries a sanitized
   diagnostic (process liveness + output BYTE LENGTHS only, never
   content). The gate's ordered-evidence contract is unchanged.

2. **P0 — the Linux desktop runtime smoke gains the missing session
   piece: a real window manager, owned by the smoke step.** Both v1.9.5
   (`38095637817`) and v1.9.6 (`38135751846`) failed the same gate:
   `NO visible top-level window` while the process stayed alive with all
   boot markers — and v1.9.6's own X11 evidence dump shows the app pid
   owning windows that are created but never MAPPED. The v1.9.6
   software-renderer switches (`WEBKIT_DISABLE_DMABUF_RENDERER=1`,
   `WEBKIT_DISABLE_COMPOSITING_MODE=1`, `GSK_RENDERER=cairo`) did not
   change the outcome: under bare Xvfb (no window manager) the GTK4
   top-level on this runner stack stalls before the map. That is an
   environment prerequisite of the normal user-visible path — every real
   Linux desktop session runs a WM — so the smoke step now installs
   (`openbox` added to the dependency step) and starts a real lightweight
   WM on its own display, waits for the EWMH `_NET_SUPPORTING_WM_CHECK`
   root property (the WM's own ownership signal — bounded poll, no
   sleeps), and cleans it up with the same trap discipline as Xvfb. The
   gate is UNCHANGED: a real visible, pid-owned top-level window, delivered
   `WM_DELETE_WINDOW`, normal-path exit 0, plus the full failure-path
   X11 diagnostics (tree, map states, pids, WM ownership evidence, app
   log tail). No product change was made for CI; no gate was weakened;
   discovery still requires `--onlyvisible` and pid ownership.

3. **P0 — the standing admission-seam gap is closed: UpdateEngineNow now
   participates in the SAME destructive-boundary claim seam.** The
   same-tag/scheduled engine-update transaction (`LlamaServer.UpdateEngineNow`)
   stopped the engine without the run-admission claim the v1.9.6 variant
   transaction takes — a run could be admitted onto an engine about to
   be stopped. v1.10.0 wires the SAME primitives: the same early advisory
   refusal (a run active at entry refuses before anything happens; no
   claim is taken), the same boundary claim taken atomically with the
   live-run verdict immediately before the first destructive step (the
   stop), the same balance-on-every-exit discipline (released exactly
   once on every path), and the same nil-seam boundary re-check for
   hosts without an admission authority. Refusals are deferrals
   (temporary), never persisted bounded failures. No second claim
   mechanism, no second authority: `BeginEngineMutation` /
   `EndEngineMutation` and `EngineMutationActive` remain the only seam.
   Five deterministic pins in `internal/llm/admission_updatenow_v1100_test.go`
   mirror the v1.9.6 boundary pins (claim held across the destructive
   window, balance, live refusal keeps the engine serving, early refusal,
   nil-seam re-check); the v1.9.6 pins and the v1.9.5 pins stay green
   unchanged.

4. **P1 — R&D Phase 1 (native C++ engine promotion): the evidence-gated
   first step; the production default is NOT flipped.** The promotion
   mechanism is exercised through its real contract: the native engine's
   CTest suites — including bit-for-bit prefill/KV parity — pass
   (13/13, local Linux session), the browser E2E stack runs the native
   engine end-to-end with the canonical fixture model, and
   `NativeBackendEnabled` selects native-first with managed llama.cpp
   automatic fallback for unsupported models/capabilities or verified
   failure. The R&D Phase 0 measurement contract additionally requires
   the warm-TTFT comparison against managed llama.cpp on physical target
   hardware; that sample does not exist yet (no physical Windows host in
   this session — the honest posture is recorded in the baseline
   artifact). Per the R&D decision rule, promotion by default therefore
   does NOT ship in v1.10.0: native remains an explicit opt-in
   (`engineBackend: "native"`), managed llama.cpp remains the default,
   and the promotion gate checklist with its specific blocking evidence
   is documented in ROADMAP.md. Promotion never happens by filename or
   device detection; the fallback stays automatic and honest.

5. **P1 — R&D Phase 0 (measurement baseline) recorded, honestly.**
   `baselines/perf-hosted-ci-v1.10.0.json` + the report in `worklog.md`
   capture the shipped `internal/perf` + `cmd/shtn-perf` envelope on the
   hosted Linux CI-class host: `cold_start_to_interactive_ms` and
   idle/peak process memory MEASURED; `engine_ready_ms`, `warm_ttft_ms`,
   `stream_chunk_to_visible_ms`, `stream_duration_ms`,
   `tool_round_trip_ms`, `file_output_overhead_ms` NOT MEASURED (no
   serving llama-server binary, no physical hardware, no desktop shell
   in this posture — each marker carries its specific reason). Hosted-CI
   numbers are labeled hosted-CI; no R&D target is claimed as met.

6. **P1 — the Qt/IPC seam for the next cycle is DEFINED (documentation
   only; nothing ships).** The typed, coarse-grained boundary between a
   future Qt (C++/QML) shell and the unchanged Go core is specified in
   ARCHITECTURE.md: one secured local transport for control + streaming
   (the loopback capability-token discipline, reused, not copied as a
   second authority), versioned message shapes, least-privilege auth,
   no unauthenticated local API, no arbitrary host binding; shared
   memory / zero-copy only where the Phase 0 harness proves a
   payload-size win. Exactly what is and is NOT ready is listed there.
   No Qt code ships in v1.10.0; the Wails shell remains the production
   UI.

7. **Carried forward (unchanged):** the v1.9.5 loopback activity
   transport and capability token (single route, ephemeral, loopback
   only, token on the same-origin discovery path only, strict
   origin/host validation, visible degraded-streaming reporting); the
   opt-in sanitized stream trace (lengths only); the Windows live-answer
   acceptance as a REQUIRED gate (ordered live growth with Stop visible,
   deterministic multi-chunk fixture, settlement with exactly one
   persisted reply, same-session reload, real window close); the v1.9.6
   admission claim placement at the variant transaction's destructive
   boundary; identifier-free ordinary logs (no `runId=` / `session=`
   reintroduction).

## v1.10.2-era measured baseline (historical record)

Recorded in the v1.10.2 session (virtualized Linux container session host —
NOT physical consumer hardware and NOT GitHub Actions hosted CI;
artifact: `baselines/perf-hosted-v1.10.2.json`, declared host class
`hosted`, method + limitations + honesty notes embedded):

> v1.10.3 correction (R10): the v1.10.2 session quoted 1.2 ms / 14.8 MB
> here while the committed artifact recorded different numbers and an
> unstamped code SHA — an evidence narrative drift. The artifact was
> re-measured in the v1.10.3 session (same evidence class, same
> fail-closed classification) with `SHEYTAN_PERF_CODE_SHA` stamped to the
> audited commit `1d3b1f2e0ece271f9eac0656788368638062366a` and a
> trailing newline; the CURRENT baseline section at the top of this file
> quotes THAT artifact character for character. Never keep two
> narratives at once.

* `cold_start_to_interactive_ms`: MEASURED — 1.339 ms (api.New +
  EnsureSetup boundary on the hosted host; the GUI shell adds its own
  window cost, measured by the desktop acceptance surface). Target
  < 800 ms is met on THIS host only — not a product-wide claim.
* `idle_memory_mb` / `peak_memory_mb`: MEASURED — 14.3203125 MB (process
  RSS/VmHWM, server-only posture; excludes the desktop shell process).
  The < 80 MB preference is met on THIS host only.
* `engine_ready_ms`, `warm_ttft_ms`, `stream_duration_ms`: NOT MEASURED
  — no serving llama-server binary and no real model on this host.
* `stream_chunk_to_visible_ms`: NOT MEASURED — requires the real desktop
  shell (the Windows live-answer acceptance surface).
* `tool_round_trip_ms`, `file_output_overhead_ms`: NOT MEASURED — no
  agent tool round executed by the perf runner.

R&D §4 targets remain TARGETS. The native-engine warm-TTFT comparison
(native vs managed llama.cpp on physical Windows hardware) is the
specific blocking evidence for the Phase 1 default promotion.

## Verification evidence (classes kept separate)

* **Local, v1.10.2 release session (Linux virtualized container,
  Go 1.26.0 / Qt 6.8.2 / CMake 4.4.4):** canonical version gate green
  (`node scripts/release-version.mjs --check` at 1.10.2); `go build
  ./...` + `go vet ./...` clean; FULL `go test ./... -count=1` PASS
  (incl. the new `internal/perf` fail-closed pins and the new
  `internal/desktop` resolution pins); `GOOS=windows go build ./...`
  clean and `GOOS=windows go vet` on the touched packages clean (the
  Windows full-suite compile gate now builds); Qt 6.8.2 configure +
  build of `ui/qt` green; QtTest acceptance 4/4 (chat live-answer cycle
  + the new AI Systems list/activate/typed-404 slot) with the real core
  + fixture engine; `scripts/qt-desktop-smoke.sh` G1–G5 PASS under
  Xvfb + Openbox with no orphan and no leaked endpoint; frontend
  typecheck / lint (0 findings) / clean release build / `verify:web`
  green; `npm audit --audit-level=high` clean at ws 8.22.0;
  `cmd/shtn-perf` baseline captured with the fail-closed classification
  (unclassified run refused; physical-over-hosted-evidence refused).
  Exact commands and results are recorded in `worklog.md`.
* **NOT RUN / NOT VERIFIED in this session (honestly):** the Windows
  Qt 6.8.2 MSVC build, windeployqt packaging, windowed runtime smoke
  and QtTest live-answer acceptance on real Windows (no Windows host
  here; the hostable subset — full-suite Windows compile/vet and
  windows/amd64 core cross-build — is green and documented above);
  the hosted-CI Actions verdict (no push performed from this session;
  the GitHub Actions API query was rate-limited from this IP, so even
  the STATUS of existing runs could not be read); the native C++
  engine CTest parity gates (no llama.cpp sources fetched — the native
  default stays gated exactly as before); and the physical-Windows
  Phase 1 baseline (NOT MEASURED — no physical hardware). Local green
  is not CI green; Linux green is not Windows green; hosted numbers
  are not physical benchmarks.
* **CI:** this session does NOT push and therefore has NO Actions run
  for v1.10.2. The authoritative verdict is the first Actions run that
  carries this revision; until it exists, no green-run claim is made
  for v1.10.2.
