# changelog.md — SHEYTAN-Local-Agent Release History

**This file is the ONLY canonical release-history artifact.** The README
describes the current product only; per-release evidence lives in the
release tags, the tests that shipped with them, and `UPDATE.md` (current
maintenance behavior).

Every release was shipped with the same invariants: one authority per
concern, evidence over confidence, no sleep-based correctness, honest
hardware claims, the codename gate enabled.

---

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
   release notes only; `README.md` describes the current product;
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

## v1.8.8 — 2026-10-07 — audit-job repair, metadata synchronization, dependency security

Focus: repair the v1.8.7 `test:release` audit-job failure at its root
and leave every shipped surface backed by real implementation and
evidence. No new subsystem, no second authority, no architectural bet.

1. **Codename-gate fixture repair (CI run 37446279646 root cause).**
   The v1.8.7 `TestJSONColumnOrderIsDeterministic` fixtures used the
   retired product codename as a JSON key in four tracked lines, so the
   token-aware tracked-tree scan failed the audit job and both platform
   jobs were skipped. The fixture keys were renamed to a codename-free
   alternative that preserves the regression's ordering semantics
   (document order ≠ alphabetical order); the gate remains strict, the
   regression remains real, and no gate exemption was introduced.

2. **Release metadata synchronized to one version.** The
   `package-lock.json` root metadata (stale at 1.8.5 since the v1.8.6
   release) is repaired; every release surface — `package.json`,
   `internal/config/config.go`, `build/config.yml`, `SIGNATURE`,
   README current-release identity, ROADMAP marker, `UPDATE.md` — now
   reads 1.8.8 from the single canonical source through the existing
   `release-version.mjs` authority (`--check` mode green).

3. **Dependency security repair.** The high-severity advisory reported
   by `npm audit` is repaired at the exact vulnerable chain with a
   compatible semver upgrade; the frontend dependency contract is
   preserved and the full frontend verification stack (typecheck, lint,
   unit, release) re-run green.

4. **dataAnalysis hardening.** Regression coverage and correctness
   repairs on the one data authority: RFC-4180 parsing edge cases,
   deterministic ordering with explicit tie-breakers on every
   externally visible sequence, join cardinality semantics,
   aggregate/quality/export edge cases, cancellation honoring and
   honest size-bound messaging. Details in the v1.8.8 test suite.

---

## v1.8.7 — 2026-10-05 — deterministic Tier-2 discovery + the data-analysis authority upgrade

Focus: repair the v1.8.6 Windows CI failure in `internal/engdiscovery`
(Actions run 37273354268) at its root, then upgrade the EXISTING
`dataAnalysis` tool into the application's deterministic data-analysis
authority — computation moves out of the model and into local
execution, and the model receives compact results instead of raw
datasets. One data tool; no second manager; no speculative dependency.

1. **P0 — deterministic Tier-2 discovery (the Windows CI root cause).**
   The v1.8.6 frontier ordered QUEUED directories correctly, but
   multiple workers executed jobs concurrently and `emit()` sealed the
   whole scan on first ARRIVAL of the `MaxCandidates`-th candidate.
   Candidate discovery order was therefore race-dependent: an
   already-running deeper worker could report before a shallower worker
   and seal the scan
   (`TestFullScanPrefersShallowOverDeepLexicallyEarlierCandidate`:
   got `aaa-early\deep\llama-server.exe`, want
   `zzz-late-engine\llama-server.exe`). v1.8.7 keeps the bounded worker
   pool and the priority frontier and adds a DETERMINISTIC RETENTION
   BARRIER: workers are only ever handed jobs of the current (lowest
   outstanding) priority value — parallelism stays within one discovery
   level; candidates are buffered per level and retained in
   (level, path) order at the level drain; the scan seals at the drain
   of the level that satisfied `MaxCandidates`, never on first arrival.
   Retention is a pure function of the scanned dataset: parallel scan +
   same dataset + repeated executions → same winning candidate, for any
   worker count and any interleaving. `Workers` stays ≥ 1 configurable
   (not reduced to force determinism); no sleeps, no retries, no timing
   assumptions; timeout, cancellation, `MaxDepth`, noise skipping,
   deduplication and bounded memory preserved. Coverage:
   `internal/engdiscovery/discovery_barrier_test.go` (deterministic
   winner across worker counts 1–16, same-depth lexical determinism,
   multi-root class priority, sealed-scan leak/deadlock guard) plus the
   strengthened existing suite.

2. **P0 — repeated-scan priority inversion in the discovery cache
   removed.** The cache-parent seeding path enqueued every previously
   observed candidate's parent directory at class 1 BEFORE the scan
   started, pre-marking those directories as visited and DEMOTING any
   location the user tree would have discovered at its natural (higher)
   priority — a repeated scan could rank a previously-cached DEEP
   directory above a fresh SHALLOW one, and the retained winner could
   alternate between runs. v1.8.7 DEFERS cache-parent seeds until the
   barrier enters the class-1 band (every class-0 level fully drained),
   preserving the v1.4.0 intent (cached locations still rank ahead of
   broad volume recursion) without the inversion.
   `mergeCacheAll` now persists the cache in deterministic (tier, path)
   order so the next scan's same-level tiebreak is a pure function of
   the cache contents.

3. **P1 — avoidable Tier-2 scan work reduced (the 126 s Windows
   package runtime).** `noiseDirs` now also skips language-toolchain
   caches (`.cargo`, `.rustup`, `.go`, `.nuget`, `.dotnet`, `.gradle`,
   `.m2`, `.android`, `hostedtoolcache`, …), per-run temp churn
   (`temp`, `tmp`) and Windows update remnants — deep, high-latency
   trees that change every run and can never host a user engine
   install. Real engine hosts (`.local`, `bin`, scoop shims, `.ollama`)
   remain scanned; Tier 1 still covers PATH. Once the candidate budget
   is satisfied, child directories are no longer enqueued (they could
   never be visited — the scan seals at the current level's drain).

4. **P1 — the data-analysis authority upgrade (ONE tool: `dataAnalysis`).**
   Five new deterministic actions, all in-process pure Go on the
   existing dataset model and parse-once numeric cache:
   - `analyze` — ONE call for a complete compact dataset analysis:
     schema, missingness, numeric summary, categorical top values,
     distinct counts, strong correlations (|r| ≥ 0.6), IQR outlier
     summary, optional group-by and sample, and deterministic key
     findings. Configurable `sections` (default:
     `schema,missing,stats,categorical,outliers,findings`).
   - `aggregate` — multiple aggregations in ONE call
     (count/sum/mean/min/max/median/std/quantile with `q`) over one or
     more grouping columns (`by`/`byList`), with a `"<agg>:<column>"`
     spec grammar and a bare-agg × `columns` shorthand; groups sorted
     ascending by key.
   - `join` — deterministic local joins between two datasets
     (inner/left/right/full), explicit keys required (single or
     composite `leftKeys`/`rightKeys`), SQL-standard semantics with
     file-order determinism, a 2,000,000-row materialization cap, and
     bounded preview + optional artifact file.
   - `quality` — compact machine-readable diagnostics: missing values,
     duplicate rows, invalid numerics (±Inf in numeric columns),
     constant columns, high-cardinality columns, mixed numeric/text
     type inconsistencies and per-column outlier counts.
   - `export` — materialize rows (optionally filtered, projected,
     sorted, limited) as CSV/TSV/JSON artifacts under the workspace
     path authority; the model gets a path + counts, not pasted rows.
   All five stamp compact provenance metadata (backend, bytes, rows,
   cols) and honor three output modes: `compact` (default),
   `table` and `json`. Analysis results now answer most data questions
   from one deterministic tool call.

5. **P1 — JSON loading determinism repaired.** `loadJSON` derived its
   column order from Go map iteration, which is randomized — the same
   JSON file could produce a different column order (and therefore a
   different analysis result) on every load. Objects are now decoded
   with `json.Decoder` token streaming so column order follows the
   DOCUMENT's first-seen key order, deterministically, for both JSON
   arrays and JSONL. Pinned by `TestJSONColumnOrderIsDeterministic`.

6. **P1 — data-tool test surface established.** The package previously
   had no dedicated suite; `internal/tools/data_tool_test.go` adds
   focused coverage: CSV/TSV/JSON loading, type inference, missing
   values, the parse-once numeric cache, all five new actions,
   compact/table/json output modes, output limits, result
   materialization, cancellation, malformed datasets, path
   restrictions (traversal and absolute escapes rejected through the
   path authority), the honest 256 MB size bound and byte-for-byte
   deterministic repeated results. Orchestrator integration:
   `internal/agent/datatool_orchestrator_test.go` proves the real
   registration path; `internal/toolsets/datatool_selection_test.go`
   pins that `dataAnalysis` stays selected for data tasks.

Backend decision (documented, not hidden): the heavy-data backend
evaluation kept the pure-Go in-process engine. DuckDB's Go client is
cgo with a statically linked bundled engine — unacceptable packaging
and CI complexity (a C toolchain on the Windows/Linux packaging paths
plus tens of megabytes per target binary) for workload sizes the
256 MB fast path already covers. Parquet and SQL are therefore NOT
added; `query`/`aggregate`/`join` remain the relational authority, and
the 256 MB input bound is stated honestly (no streaming claim). The
remaining native C++ GPU work is unchanged and still not complete.

---

## v1.8.6 — 2026-10-05 — Phase 2: execution truth, GPU transaction authority, resource integration

Focus: PHASE 2's deep runtime/execution work — make the v1.8.5
execution/evidence foundation actually CONTROL backend selection and
resource behavior, prove GPU execution correctly, and integrate
CPU/GPU/RAM truth — through the EXISTING authorities (no second runtime,
no second selection authority, no second sampler, no second policy
engine). Five verticals landed, each pinned by deterministic suites:

1. **P0 — the execution-truth contradiction closed (enumeration is
   selection evidence, never execution proof).** The v1.8.5 ladder was
   correct conceptually but consumed an accelerator contract that could
   mark a GPU `ExecutionVerified=true` from `--list-devices` enumeration
   alone. v1.8.6 enforces the full invariant
   `GPU detected ≠ GPU available ≠ GPU selected ≠ GPU executed ≠ GPU
   verified` at the ONE accelerator authority
   (`internal/accelerator/resolveGPU`): enumeration now SELECTS
   GPU_VULKAN with the pending-execution verification plan and the CPU
   safety net; verification requires the measured runtime offload line
   or a still-valid **ExecutionReceipt** — a new small structured
   object (`accelerator.ExecutionReceipt`) carrying the identity the
   evidence was produced under (kind, line, engine tag, variant,
   device, model, status, time) with `ValidFor(engineTag, variant)`
   stale-evidence invalidation: a receipt from another engine build,
   another variant or a failed probe can NEVER verify the current
   selection. Coverage:
   `internal/accelerator/execution_truth_v186_test.go` (enumeration
   never verifies; offload line verifies; valid receipt verifies;
   stale/failed receipts never verify; CPU stays verified by
   definition; the receipt identity matrix) plus the corrected v1.2.6/
   v1.6.1 contracts.

2. **P0 — the GPU transaction is authoritative; premature activation
   removed.** The launcher (`LlamaServer.autoGPUOffload`) could turn on
   `--n-gpu-layers 99` from device enumeration (or even Vulkan DLL
   presence) BEFORE the bounded transaction proved real GPU execution.
   v1.8.6 splits the two modes: a NORMAL serving launch enables
   auto-offload ONLY on PROVEN execution (the current boot's measured
   offload line, or a persisted verified GPU-probe receipt whose engine
   tag+variant still match — `gpuExecutionProven`); enumeration/DLL
   presence keep AUTO CPU-safe. A CANDIDATE verification transaction
   (`gpuCandidateProving`, set strictly inside `updateEngineVariantTx`
   when the execution-evidence verify hook runs, transaction-scoped via
   deferred reset) may boot Vulkan from selection evidence to PROVE it
   — the transaction still requires a real generation AND the measured
   offload line before commit, so enumeration alone can never produce a
   verified serving posture. Manual `numGpu` config is applied verbatim
   and the CPU-forced profile is now enforced at the launcher too. The
   offload evidence became PER-BOOT (`launchArgs` resets it): a restart
   or model swap can never inherit the previous boot's GPU execution
   claim. Coverage: `internal/llm/gpu_activation_v186_test.go` (10
   tests: enumeration/DLL/failed-receipt never activate; offload line/
   valid receipt do; CPU-forced stays CPU; manual config respected;
   proving mode; per-boot reset) plus the existing v1.7.2 rollback
   suites unchanged.

3. **P0 — `/api/engine` and `/api/perf` agree (one authority, one
   evidence path).** The engine execution ladder previously depended on
   a memo written only by the `/api/perf` poll — the authoritative
   engine state accidentally depended on whether the performance page
   happened to poll first. v1.8.6: ONE accessor
   (`currentAcceleratorResolution`) serves BOTH surfaces; the memo now
   records its input SIGNATURE (engine tag + variant + requested
   profile + loaded model + offload evidence + probe state) and a stale
   memo is recomputed before any consumer can read it — no stale
   poll-only snapshot can claim a current backend; the engine surface
   refreshes the memo itself (a background warm-up at server start
   keeps the first poll off the enumeration cost); model/engine/device
   identity stays aligned, and the stage cannot move backwards because
   a UI poll happened later (only because serving reality changed).
   Coverage: `internal/api/engine_perf_consistency_v186_test.go`
   (engine-first polling, verdict agreement both orders, monotone
   stage across polls, stale-memo invalidation, fresh-memo reuse).

4. **P0 — real CPU/GPU/RAM resource integration through the ONE
   Governor.** The Governor now accounts for the CURRENT inference
   workload: a new injected footprint source (wired on the Stack from
   the EXISTING model-card/context authorities — model file bytes as a
   FILE fact, planned KV at the serving window) folds into the resource
   state and the envelope (a footprint that consumes the resident
   budget reduces background work through the existing honest
   adjustment class, reason stated; unknown stays unknown — RAM is
   memory, never an accelerator). The v1.8.6 RESOURCE-AWARE RUN GATE
   (`Stack.GovernorAdmitsModelLoad`, consulted by EnsureLLM and
   EnsureLLMContext BEFORE any engine start) admits the model load
   against the Governor's measured envelope using the SAME resident
   plan the preflight authority computes (weights + KV + runtime
   overhead); sustained pressure defers the load with the explainable
   reason; an unmeasured Governor falls through to the preflight gate
   exactly as before. The measured engine RSS now actually flows:
   LlamaBackend.Metrics reads the engine process resident set through
   the existing `resources.ProcRAM` authority (the same sampler family
   the live monitor uses), feeding the Governor's engine facts. A new
   `/api/perf` `engineMemory` block carries the serving engine's REAL
   memory evidence with provenance labels — measured process RSS (from
   the one monitor cadence), the model FILE size (explicitly never
   "RAM used"), the runtime offload line, and unknown KV allocation
   named as unknown (never a guessed figure). Coverage:
   `internal/governor/inference_footprint_v186_test.go` (6),
   `internal/runtime/governor_gate_v186_test.go` (6: wiring, unknown
   fallbacks, healthy admission, sustained-pressure deferral).

5. **P1 — Windows CPU telemetry through the EXISTING seam; native and
   GPU-backend truth preserved.** The Governor's Windows CPU seam
   (`governor.CPULoadPlatform`) now measures real load via kernel32
   `GetSystemTimes` through the ONE shared priming/delta state machine
   (`internal/governor/cpu_delta.go`): the first sample primes,
   subsequent samples compute the real busy-fraction delta, failures
   stay unknown (the cumulative-counter baseline is kept so the next
   delta stays true), and the Linux seam is untouched — no second
   sampler, no second cadence, the Governor's poll-path ownership
   unchanged. Coverage: `internal/governor/cpu_delta_test.go` (5,
   cross-platform, race-gated). The native C++ engine keeps Go as the
   control plane with its capabilities explicit (CPU-only execution,
   no GPU claim, no faked reasoning budgets — now stated in the
   engineMemory surface); the Intel/GPU direction stays the PROVEN
   llama.cpp Vulkan transaction (no SYCL/OpenVINO backend was added —
   the repository can not yet provision, launch, test and verify one,
   so none is claimed).

Phase 1 surfaces are preserved unchanged: live streaming conflation
queue, server conflation, the reasoning ladder, Show/Hide Thinking,
zero-session Send, generation guards, pause/edit/resume, abort
honesty, engine identity transaction, config atomicity, Governor
ownership, reconnect/replay and the one-authority architecture
(full llm/api/runtime/governor/accelerator suites + the race gates
green; 213 frontend unit tests and the release-metadata gate green).

---

## v1.8.5 — 2026-10-03 — Phase 1 of the staged engine program

Focus: PHASE 1 — the core runtime / user-surface foundation of the staged
engine program (the plan: Phase 1 runtime/user foundation → Phase 2 deep
execution-engine/resource integration → Phase 3+ new product capabilities).
Four user-facing/engineering verticals landed, each through the EXISTING
authorities (no new manager, scheduler, registry, downloader or runtime):

1. **P0 — live streaming: the server-side drop-newest bottleneck closed.**
   Trace (engine → SSE client → orchestrator emitProgress ~8ms → publish →
   hub → WS → frontend fast-path fold → triple-boundary flush → render)
   showed the v1.8.4 frontend scheduler robust; the remaining defect was
   the run hub's subscriber channel: on overflow it DROPPED THE NEWEST
   event ("a slow WebSocket must never block the agent run"). For
   cumulative response/reasoning captions that discards the frame with
   the FULL text while the buffer keeps stale prefixes — under transport
   backpressure the visible text freezes at a stale prefix until the run
   ends (Stop included), then the reconnect snapshot replay reveals
   everything at once: the server-side twin of the v1.8.4 MessageChannel
   wedge. v1.8.5: bounded conflation-aware subscriber queues (overflow
   evicts the OLDEST conflatable snapshot — newest-wins; order and the
   seq replay contract preserved; terminal events never preferentially
   evicted; publisher never blocks; memory bounded) plus a generous
   per-event write deadline (a wedged client tears down deterministically
   and recovers via reconnect replay). Coverage: 9 deterministic
   hub/queue suites including the drop-newest reproduction, terminal
   survival, publish-never-blocks (20k offers to a wedged reader),
   close-drains-first, concurrent publishers (race gate); the full API
   suite green.

2. **P0 — reasoning depth becomes a REAL four-level ladder: Low / Mid /
   High / Ultra.** Every level carries a numeric thinking-token budget
   applied to the generation request through the llama.cpp request-level
   `reasoning_budget_tokens` parameter — VERIFIED against the actual
   server sources of BOTH managed builds (b10642 DefaultEngineTag and
   b11205; the field exists at the OAI chat endpoint and maps into the
   reasoning budget sampler; the CLI fixtures independently document
   `--reasoning-budget`): low=0 (thinking off — "0 for immediate end"),
   mid=1024 (bounded default), high=4096, ultra=-1 → NOT SENT (engine
   default/unrestricted — the honest encoding of "no client-side cap").
   Pointer-typed field keeps the explicit 0 on the wire; local engines
   only (same gating as TopK/NumCtx/MinP); non-thinking models ignore the
   budget (no fabricated reasoning); the native C++ path is
   documented-inert. The level rides EVERY run request; legacy
   auto/fast/thinking values migrate at the wire and persistence
   boundaries. Tier posture: low keeps the latency-first floor, mid is
   neutral, high/ultra never inflate context (depth = budget, not
   context). Coverage: 9 Go suites (normalization, budgets, local/remote
   gating, ultra-nil, legacy migration, wire field name, defensive) +
   the tier-posture pin + 4 store-level scripted-transport suites
   (payload always carries the level; visibility split).

3. **P0 — Show Thinking / Hide Thinking (visibility only).** A persisted
   presentation preference gating the rendering of backend-reported
   reasoning on the live bubble, the safety-net bubble and history
   messages. It never touches the request payload (pinned), never changes
   the reasoning level or budget, keeps the store folding reasoning
   snapshots, and displays only what the backend actually reported —
   unavailable reasoning stays unavailable. Shared Chat/Agent semantics
   through the shared surfaces.

4. **P1 — the engine leaves the user's critical path.** The Start/Stop
   engine toggle is removed from the ordinary workflow; the engine boots
   on first use (run gate), restarts after engine-affecting changes
   (settings flow) and recovers through internal supervision; the UI
   represents engine state. Internal lifecycle operations (startup,
   update, restart, recovery, shutdown) unchanged — supervision
   ownership untouched.

5. **Phase 2 foundation — the ONE execution/evidence ladder.**
   `internal/llm/execution.go` + `/api/engine`'s `execution` block:
   detected → backend-available → device-selected → model-loaded →
   generation-executed → execution-evidence → verified, a PURE composer
   over the existing authorities (device presence, backend health, the
   accelerator selection memo, verified model, measured generation ring,
   runtime offload lines). Monotone by construction: device enumeration
   can never equal verified execution; unknowns stay unknown; gaps are
   named. Coverage: 5 deterministic ladder suites incl. the cardinal
   rule and the wire contract.

6. **Version identity** — all release surfaces at 1.8.5 through the ONE
   canonical gate (`node scripts/release-version.mjs --check`).

Not claimed by this release: physical GPU execution proof, the unified
C++ execution path, native reasoning budgets, Windows CPU telemetry —
all explicitly Phase 2 scope (`ROADMAP.md` §Phase 2). CI remains CI, not
physical-host runtime evidence.

---

## v1.8.4 — 2026-10-01

Focus: four P0 defect families — (A) live streaming visibility, (B) the
AUTO Vulkan posture circularity and the engine-identity verification
window, (C) the zero-session Send dead end, (D) stale context-usage
races — plus the reliability defect the verification funnel surfaced
(the fixed `config.json.tmp` name racing concurrent config writers). No
new authority was created anywhere: every fix routes through the
existing streaming scheduler, the existing variant transaction, the
existing session store and the existing context backend.

1. **P0-A — the streaming flush can no longer wedge permanently, and the
   activity timeline is no longer rAF-only.** Root cause of the real
   Windows runtime report ("live response/thinking invisible during
   generation, appears only after Stop"): the v1.8.2
   `MessageChannelTaskController` was ONE-SHOT — its port handler nulled
   the channel after the first delivery and a post arriving while a
   message was still in flight chained onto the armed callback WITHOUT
   posting a new message. A single lost or indefinitely delayed
   MessageChannel message therefore left the controller wedged forever
   (queued armed, channel non-null, microtask fallback unreachable, the
   scheduler latch stuck pending) — every later `schedule()` became a
   no-op and streamed text accumulated in the accumulator until the Stop
   path's synchronous flush revealed it, exactly matching the observed
   runtime (the phase label kept updating because the fast path's
   `transitionPhase` renders synchronously at socket-receive time).
   v1.8.4: the flush scheduler arms THREE independent boundaries behind
   the ONE coalescing latch — a reusable MessageChannel macrotask, a 0ms
   timeout task (the primitive class proven alive in the failing
   runtime), and the animation frame; the task controller is
   latest-callback-wins with an explicit in-flight handshake and a
   deterministic microtask fallback the moment a post arrives while a
   message is still undelivered (a lost channel degrades to microtask
   delivery instead of dying). The ACTIVITY flush — statuses, tool
   events and the done/error/aborted lifecycle events — moved off its
   rAF-only schedule onto the same triple-boundary scheduler. Evidence:
   `src/stream-flush-scheduler.test.ts` (13 tests, including the
   lost-message wedge reproduced deterministically — the v1.8.2 design
   fails it, the v1.8.4 design self-heals), the store-level suites, and
   a new browser E2E proving live text and honest run settlement with
   requestAnimationFrame fully suspended.

2. **P0-B — the AUTO Vulkan candidate is no longer blocked by a stale
   derived posture, and the verification window reports the binary that
   is actually on disk.** Two root causes:
   (i) The pre-1.8.4 recommendation pipeline wrote `gpuAutoOffload=false`
   + `numGpu=0` as a DERIVED posture ("CPU-only until a Vulkan engine
   build is provisioned") on every auto model selection; the AUTO probe
   eligibility gate then read that posture as an explicit OFF and refused
   the candidate forever — the exact circularity in the real runtime log
   ("GPU AUTO Vulkan candidate not considered this boot: GPU offload is
   disabled in settings (numGPU=0, auto-offload off)"). The
   recommendation never writes a derived OFF anymore (the launch-time
   evidence gate `autoGPUOffload` keeps the engine honestly on CPU while
   no usable device exists), a new `gpuAutoOffloadUserSet` config field
   marks EXPLICIT user actions (settings toggle, direct config patch),
   and `config.Load` repairs the legacy derived state ONCE — honestly
   noted and persisted — while never touching an explicit user OFF, an
   explicit manual layer count, or a CPU requested profile.
   (ii) The real log sequence "b11310 staged + SHA verified → engine
   boot probe: binary build b11273 → b11310 committed" was an identity
   violation in the deferred-commit window: the boot path keyed its
   identity on the RECORDED tag (installed.json), which still described
   the previous build until Commit ran, so the log misreported the
   serving binary AND the old build's persisted capability profile
   shadowed the new binary. The installer now writes a window-scoped
   staged-identity marker (`engine-stage-pending.json`) the moment the
   byte-verified candidate is swapped in; the boot probe probes, caches
   and reports the ACTUAL staged build; Commit and Rollback clear the
   marker. Evidence: `internal/updater/staged_identity_v184_test.go`
   (marker present during the window with the old recorded tag,
   cleared on commit and rollback, corrupt marker fails closed),
   `internal/config/gpu_posture_v184_test.go` (repair + explicit-OFF +
   manual-layers + CPU-profile boundaries, persisted once),
   `internal/recommendation` contract updates.

3. **P0-C — pressing Send with zero sessions creates and activates a
   session and continues the run.** Root cause: `run()` already created
   the session lazily, but the composer textarea and Send button were
   hard-disabled on `!activeSessionId` — with zero sessions the lazy
   creation was unreachable (the reported "Send/chat is blocked instead
   of creating a session"). The composer now enables on a zero-session
   space (the model gate and the live-run gate keep their semantics),
   the placeholder and footer state the truth ("sending starts a new
   session"), and `deleteSession` already left a valid zero-session
   state. Evidence: `src/zero-session-send.test.ts` (store-level:
   create + activate + continue the same send; valid zero state after
   deletion; the exact delete-then-send user path; honest rejection when
   creation fails; the created session survives a refresh),
   `e2e/zero-session.spec.ts` (real-stack: delete every session →
   zero-session state → Send → session created + active → user bubble →
   streamed text visible BEFORE completion → reload persistence; Chat
   and Agent modes).

4. **P0-D — stale context responses can never overwrite newer context
   state.** Root cause: `refreshSessionContext` guarded only on the
   active session id, so two concurrent requests for the SAME session
   could resolve out of order (older lands last), and deleting the
   active session never cleared or refreshed the visible context.
   v1.8.4 adds a monotonic request generation: every refresh claims a
   strictly newer generation; a response may land only when it is still
   the newest request AND its session is still active. Session switch,
   session deletion (with replacement refresh), session creation, mode
   switch and a fresh run's state transitions all invalidate the
   generation; the UX stays automatic/unlimited (no user-controlled
   context-size controls; physical limits remain backend-governed).
   Evidence: `src/context-refresh-race.test.ts` (7 scenarios over a
   scripted transport with causally forced response ordering: same-
   session out-of-order, triple-race, cross-session held response,
   deletion clears, deletion refreshes a replacement, creation clears,
   pre-run response invalidated).

5. **P1 — `config.Save` atomicity hardening (surfaced by the funnel).**
   The fixed `config.json.tmp` name let two concurrent config writers
   rename each other's temp file away — observed in this repo's own E2E
   run as a hard engine-start failure ("persist default engine path:
   rename …config.json.tmp …: no such file or directory"). Every
   config write now uses a unique same-directory temp file (CreateTemp +
   rename), preserving the atomic-rename semantics without the name
   collision. `config.Save`, the v1.8.4 posture repair and the sampling
   repair all share the helper.

Version surfaces are 1.8.4 (package.json → `release-version.mjs` →
config.go / build/config.yml / SIGNATURE); the codename gate and the
release contract tests pass unchanged.

---

## v1.8.3 — 2026-10-01

Focus: root-cause and repair the exact Linux CI session-delete failure
(Actions run 36713108772, job 109880449134: Browser E2E 30/31,
`e2e/sessions.spec.ts:88` "delete session removes it and activates a
remaining one" — `Expected: < 4, Received: 4` after 15 s), and close the
real defects the investigation exposed. No new authority was created; the
v1.8.2 streaming, memory-evidence and self-model behavior is untouched
(the full browser suite, including every live-stream test, passes).

1. **P0 — the run-107 failure root-caused with instrumented evidence.**
   An instrumented diagnostic (request/response journaling + server-side
   authoritative list snapshots + DOM identity capture, with the browser's
   create response delivery delayed to reproduce CI latency) proved the
   interleaving: the test sampled its count baseline BEFORE the
   asynchronous "New session" create landed in the sidebar (its
   `expect(first()).toBeVisible()` gate was satisfied by the PRE-EXISTING
   items, not the new one); the delete then correctly removed exactly one
   session — verified absent from the server's authoritative post-delete
   list — while the just-created session's late response kept the sidebar
   count flat. The product deleted the correct session and never
   resurrected it; the count arithmetic compared equal numbers for 15 s.
   A passing run of the same test was the same interleaving with the poll
   catching the transient count window — a coin flip on runner latency.

2. **P0 — the browser test repaired and strengthened (measurement
   synchronizes on state, never on timing).** The repaired test waits —
   state-based, no sleeps — for the created session to actually appear
   (the count grows by one) before sampling its baseline, then asserts
   the SPECIFIC deleted id (new `data-session-id` rows; the rendered
   8-char id slice is ambiguous within the same bucket), the survivor
   identity set, the count, replacement activation, composer usability,
   and — after a full reload — that the deleted id is still absent from
   both the sidebar and a fresh authoritative list fetch. No timeout was
   increased; nothing was weakened.

3. **P0 — the startup session-list write now goes through the ONE
   generation guard (the v1.7.5 authority).** `initializeAgentOnce`
   previously applied its `GET /api/sessions` response WITHOUT a ticket —
   the v1.7.4 stale-response defect class survived in the init consumer.
   The sidebar's "New session" button is actionable while the startup GET
   is on the wire, so a user create landing in that window was clobbered
   by the stale startup list: the created session VANISHED from the
   sidebar. The repair: the init takes a `sessionListGuard` ticket before
   the GET; a response may only land while its ticket is current and the
   mode unchanged; a superseded response applies only the non-session
   state and delegates the list + selection re-resolution to
   `refreshSessions()` (its own ticket); the eager first-install create
   invalidates the guard exactly like `store.createSession` does. Verified
   by mutation at two layers: the store-level suite and a real-browser
   E2E both FAIL against the unguarded v1.8.2 init and PASS with the
   repair.

4. **P0 — deterministic session-delete coverage at three layers.**
   `src/session-delete-regression.test.ts` (11 scenarios against the REAL
   store over a scripted HTTP transport where response ordering is forced
   by causality — pending/persisted delete, replacement selection,
   delete-vs-held-GET, stale-GET non-resurrection, mode separation,
   honest duplicate-delete, post-delete fresh fetch, init-race survival,
   clean-init apply, eager first-install session preserved — plus a
   Node resolve hook making the store testable outside the bundler);
   `internal/sessions/delete_pending_test.go` (pending delete removes
   every authority; persisted delete cleans index/file/sidecars with a
   fresh-store no-resurrection check; unknown-id honest failure); and the
   new browser E2E for the init race.

5. **P0 — a failed DELETE is never silently swallowed.** The sidebar
   invoked `void deleteSession(id)`; a server-side DELETE failure was an
   invisible unhandled rejection and the session stayed in the list with
   no explanation. `deleteSession` now surfaces the failure through the
   same error state `renameSession` uses (state untouched on failure,
   never a fake success).

6. **P0 — the created-session prepend is idempotent.** A list refresh
   whose response lands between the create POST's dispatch and its
   arrival can already contain the created session; the unconditional
   prepend then produced a duplicate row. The prepend now dedupes by id —
   the new session is always the first row and appears exactly once.

7. **P1 — roadmap repaired.** `ROADMAP.md` now states the artifact truth
   (changelog = sole release history; README current-only; UPDATE.md =
   current release/maintenance evidence) and carries the evidence-ranked
   **Performance, Reliability & Scale Backlog** (D/I/H labels). External
   report claims — percentages, TPS/TTFT/cache/VRAM figures, "3x/5x/80%"
   numbers, Tokio/asyncio rewrites, mandatory FAISS/USearch/SQLite/
   bbolt/tree-sitter, NPU embedding services, dual-model VRAM swapping —
   are explicitly treated as hypotheses or rejected; the one-authority
   architecture and the Go runtime are preserved.

Verification: frontend typecheck/lint/unit 191/191; `go test ./internal/...
-tags headless` 58 packages; race suite (api/agent/sessions/contextplan/
histref/runtime); native C++ build + ctest 12/12; full browser E2E
32/32 (the repaired delete test and the new init-race test included);
stress suite 47/47 with zero hangs/crashes; release metadata and codename
gates in check mode. Windows acceptance on a physical machine remains
outstanding until observed (CI is not physical-host evidence).

---

## v1.8.2 — 2026-09-30

Focus: make live generation genuinely visible and self-describing; remove
avoidable per-turn overhead; clean human-facing logs. No new memory or
capability authorities were created — every feature consumes the existing
ones.

1. **P0 — the "text visible only after Stop" live-rendering defect,
   root-caused and repaired.** v1.8.1's single-frame fast path folded
   stream-critical events into the accumulator at socket-receive time, but
   the FLUSH was armed exclusively on `requestAnimationFrame`. In the
   affected WebView2 runtimes the compositor's frame callbacks can be
   throttled or suspended while the JS event loop, the WebSocket and
   React's own scheduler keep running — phase labels updated, elapsed
   clock ticked, streamed text never appeared, and pressing Stop (whose
   path flushes synchronously) revealed everything at once. The flush is
   now scheduled on TWO deterministic boundaries — an event-loop task
   (MessageChannel, the primitive React's scheduler itself uses) AND an
   animation frame; whichever runs first flushes, the other no-ops.
   Exactly one coalescing latch remains (never one render per token), and
   visibility no longer requires frame callbacks at all.
   Evidence: `src/stream-flush-scheduler.test.ts` (8 deterministic tests:
   one-flush latch, first-boundary-wins, cancel, task-only sufficiency),
   plus the real-stack browser harness (`e2e/repro-stream.mjs`) tracing
   socket→fast-path→accumulator→flush→DOM.

2. **P0 — the live surface shows more of what is happening.** The live
   generation bubble now carries: (a) a concise FACTUAL state while the
   model emits no reasoning stream ("Thinking · this model is not
   exposing a reasoning stream") — never fabricated reasoning, never
   generated labels presented as model thoughts; (b) a backend-truth
   MEMORY indicator (see 3); (c) the existing phase, cursor, reasoning
   panel and runtime activity, unchanged in authority.

3. **P0 — memory boosting made real, visible and evidence-backed.** The
   orchestrator composes a `MemoryEvidence` record at the injection site
   from the survival-reconciled injection facts (rolling session summary
   injected + measured tokens; recalled exchanges actually carried; the
   attached cross-mode history references; whether recall was attempted
   at all) and publishes it on the existing `context` activity. The live
   UI renders exactly this record — e.g. `Memory: session summary · 2
   recalled exchanges` or `Memory: session summary · no recall matches` —
   and nothing when the report has not arrived. A block the history
   windower elided is never claimed. No second memory engine: the
   selection policy remains the existing tiers (FAST stays cheap; targeted
   recall fires on memory-relevant intent; the rolling summary stays
   automatic). Evidence: `TestContextActivityCarriesMemoryEvidence`,
   `TestMemoryEvidenceOnContextPlan` (Go),
   `src/memory-evidence.test.ts` (9 unit tests).

4. **P0 — the model can accurately describe its own tools and
   capabilities.** A deterministic capability-intent signal
   (`taskclassify.Signals.SelfDescribe`) fires on vocabulary like "what
   tools do you have?" / "what can you do?" / "what model are you
   running?"; on that intent the orchestrator injects ONE bounded runtime
   self-model block composed from the EXISTING authorities — the tool
   registry snapshot (`ShortDescription()` first, bounded first-sentence
   fallback), the already-resolved model card (architecture, quantization,
   parameters, context limit, tokenizer family, chat template, vision only
   when real, native-executability verdict), config-backed backend facts
   (provider, installed engine build, streaming/cancellation support,
   research availability, Lab and memory availability), the sysinfo fast
   snapshot (OS/CPU/RAM measured; GPU/NPU labeled detection-only) and the
   turn's memory plan. Truthful distinctions: registered vs enabled vs
   offered-this-request vs disabled-and-NOT-callable. Capability questions
   stay cheap: no research, no recall, no repo indexing, no extra engine
   turns. Evidence: `TestClassifySelfDescribeIntent`,
   `TestSelfDescribeAddsNoComplexity`, `TestCapabilityIntentInjectsSelfModel`,
   `TestOrdinaryChatDoesNotInjectSelfModel`, `TestBuildSelfModelCatalog`.

5. **P1 — human-facing logs lose the opaque identity tokens.** `runId=`,
   `runID=`, `session=`, `sessionId=`, `sessionID=` tokens are redacted at
   the ONE central sink (the log record formatter), so app.log, the UI
   LogViewer ring, stderr and crash-report text all inherit the rule.
   Timestamps, severity, subsystems, durations, outcomes, error causes and
   ordinary prose containing the word "session" survive; internal identity
   (API objects, run state, journals, storage keys) is untouched.
   Evidence: `internal/logging/redact_test.go` (18 cases + idempotence +
   diagnostic-field preservation).

6. **P1 — repeated per-turn model-card parsing eliminated.** The model
   capability cache replaced its arbitrary 10-second TTL with
   identity-based caching: the immutable parsed GGUF card is cached under
   (path, size, mtime) with no expiry, and the config-sensitive derived
   fields re-derive from the cached card whenever a configuration
   fingerprint changes — a turn separated by minutes no longer re-reads a
   32 MiB metadata block. The map is bounded; a replaced model file
   re-parses exactly once. No second capability authority.
   Evidence: `internal/llm/modelcaps_cache_v182_test.go`.

7. **P1 — README is current-only; release history moved to this file.**
   The README no longer carries release-history sections or duplicated
   chronology; `changelog.md` is the canonical history. `ARCHITECTURE.md`
   (architecture), `agent.md` (current handoff) and `UPDATE.md` (current
   operational notes) remain the other documentation authorities.

8. **P1 — engine identity log stages made explicit.** The boot-time
   capability probe now reads `engine boot probe: binary build <tag> …`
   and the deferred-commit line names the build that became active
   (`… build <tag> committed as the active engine (next boot probes it)`),
   so a log sequence `staged → boot probe → committed` is unambiguous
   about which build is serving and which is staged.

---

## v1.8.1 — 2026-09-30

* **P0 — real-Windows local-generation crash repaired at the root.** On a
  machine running a small local Gemma-class model, an ordinary `hi` turn
  panicked with a nil pointer dereference between `task classified` and
  `tier selected`, settling as an error in ~57 ms. Root cause (reproduced
  deterministically): Gemma-class GGUFs carry ~262K-entry tokenizer blocks
  whose metadata exceeded the model-card parser's 8 MiB read bound, so the
  capability card resolved to nil and the orchestrator's estimator block
  dereferenced it. Repairs: the documented nil-card fallback (configured
  context + conservative estimator) in `resolveEffectiveContext`, and a
  32 MiB read bound so real Gemma-class cards parse again.
  Evidence: `TestResolveEffectiveContextSurvivesUnreadableCard`,
  `TestGemmaClassCardParsesUnderRaisedBound`, `TestLocalGemmaHiChatCompletes`
  (the api package's first local-provider run-level E2E),
  `TestLocalChatSurvivesUnreadableModelCard`.
* **P0 — streamed answers became visible through ONE render frame instead
  of two.** `src/stream-fast-path.ts` folds stream-critical events into the
  streaming accumulator the moment the WebSocket delivers them; the
  timeline batch, sequence/stale-run protection, cumulative-snapshot
  semantics, replay idempotence and synchronous done/error/abort flushing
  are preserved (12 deterministic frame-controller tests). (v1.8.2 note:
  the flush-boundary dependency this left on `requestAnimationFrame` is
  repaired above.)
* **P1 — engine rollback restores the recorded identity** — a failed
  startup verification after a package swap could leave the transient
  bundled-default tag stamped in `installed.json`; `Rollback()` now
  re-records from the restored manifest.
  Evidence: `TestRollbackRestoresRecordedIdentityFromManifest`.
* Audited, no change: the AppData root migration contract; GPU/AUTO CPU
  with `numGPU=0` as the honest evidence-ladder outcome.

## v1.8.0 — 2026-09-30

* **P0 — pause→resume→pause synchronization repaired at the root** —
  after a resume, proof of live generation requires a strictly newer run
  sequence AND a changed cumulative snapshot (`resumedGenerationEvidence`).
* **P0 — honest abort marker end-to-end** — the orchestrator publishes
  `aborted` (never `done`) for canceled generations; the live state, the
  outcome registry and the frontend agree; the abort-after-resume flake
  is gone (15/15 stressed runs).
* **Runtime Governor vertical slice (Adaptive Runtime Intelligence)** —
  resource state with explicit unknowns, pressure model with rolling
  signals, execution envelope with honest adjustment classes, resident-
  budget admission, provenance-carrying self-model. Policy only.
* **`GET /api/governor` + System Centre Governor card** with real values
  and named unknowns.
* Documentation consolidated; version truth through the canonical gate.

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
