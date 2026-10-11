# UPDATE.md — v1.10.1 Release Notes & Maintenance Behavior

**Release:** `v1.10.1` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ c9494dc` (`v1.10.0`) · **Date:** 2026-10-11
**Package:** `SHEYTAN-Local-Agent-v1.10.1-FINAL.zip` (complete repository tree)
**R&D authority:** `Parsaetak/Contents`, branch `Research-and-Development`,
`SHEYTAN-LA-2026-10-11.md` (linked, not copied — the R&D document stays in
its source repository).

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. v1.10.1 completes the R&D Phase 2 UI decision: the desktop UI
is now the native Qt Quick/QML host over a secured per-user IPC channel,
Wails is removed from the product and the dependency graph, and the two
run-`38152579695` failures are repaired at their roots. The R&D Phase 1
engine gate is honestly untouched: the physical-Windows warm-TTFT /
native-vs-managed comparison remains NOT MEASURED in this release (no
physical hardware was available), and the native C++ engine default
stays evidence-gated.

## What v1.10.1 changes

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

## Current measured baseline (honest)

Recorded in this session (hosted Linux CI-class container — NOT physical
consumer hardware; artifact:
`baselines/perf-hosted-ci-v1.10.0.json`, method + limitations embedded):

* `cold_start_to_interactive_ms`: MEASURED — 1.4 ms (api.New +
  EnsureSetup boundary on the hosted host; the GUI shell adds its own
  window cost, measured by the desktop acceptance surface). Target
  < 800 ms is met on THIS host only — not a product-wide claim.
* `idle_memory_mb` / `peak_memory_mb`: MEASURED — 16.5 MB (process
  RSS/VmHWM, server-only posture; excludes the WebView process). The
  < 80 MB preference is met on THIS host only.
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

* **Local, v1.10.0 release session (Linux hosted container):** canonical
  version gate green (`node scripts/release-version.mjs --check` at
  1.10.0); frontend `typecheck` / `lint` (0 findings) / `test:units`
  (236/236) / `test:release` (38/38) / clean `build` /
  `verify:web` green; native C++ clean cmake configure + build with
  `ctest --output-on-failure` 13/13 (including bit-for-bit prefill
  parity); `go test ./internal/llm/ -run 'TestUpdateNow|TestAdmission|TestNilGate'
  -count=1` 9/9 (the four new v1.10.0 pins + the v1.9.6/v1.9.5 pins
  unchanged); `GOOS=windows go build/vet ./internal/desktop/` clean (the
  repaired shell compiles for Windows); `go vet ./internal/desktop/`
  clean (headless); `cmd/shtn-perf` baseline captured. Exact commands
  and results are recorded in `worklog.md`.
* **UNVERIFIED in this session (honestly):** the Windows desktop smoke +
  live-answer acceptance on real Windows (no Windows host here — the fix
  is derived from the pinned wails sources and the step timing), the
  Linux Xvfb window smoke (no GTK4/WebKitGTK headers and no root in this
  container — the WM repair follows this run's own X11 evidence class),
  the full packaged browser-E2E matrix, and the physical-hardware
  baseline. Local green is not CI green; Linux green is not Windows
  green; hosted-CI numbers are not physical benchmarks.
* **CI:** this session does NOT push and therefore has NO Actions run
  for v1.10.0. The authoritative verdict is the first Actions run that
  carries this revision; until it exists, no green-run claim is made for
  v1.10.0. The v1.9.6 run (`38135751846`) is a FAILED baseline for both
  platform jobs and is cited only as the failure evidence this release
  repairs.
