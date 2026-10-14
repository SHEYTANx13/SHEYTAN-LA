# UPDATE.md — v1.10.4 Release Notes & Maintenance Behavior

**Release:** `v1.10.4` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ b0b3aba` (`v1.10.3`) · **Date:** 2026-10-14
**Package:** `SHEYTAN-LA-v1.10.4-FINAL.zip` (complete repository tree)
**R&D authority:** `Parsaetak/Contents`, branch `Research-and-Development`,
`SHEYTAN-LA-2026-10-11.md` (linked, not copied — the R&D document stays in
its source repository).

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. v1.10.4 is the reliability release on top of the v1.10.3 Qt
IPC hardening: it turns the Linux stress gate green by retargeting the
one stale pin the v1.10.1 Qt migration left behind, states the Windows
verification evidence class honestly, and aligns every product document
with the Qt desktop reality. It adds NO new product surface and flips NO
default. The v1.10.3 notes below remain the parent-release record: the
framing, handshake, sessions contract, smoke and packaging behavior they
describe is unchanged in v1.10.4.

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
