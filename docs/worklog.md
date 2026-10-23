# SHEYTAN-Local-Agent — Engineering Worklog

This worklog is a session log, not a second architecture document. The
architecture truth lives in `ARCHITECTURE.md`, the release evidence in
`UPDATE.md`, the future in `ROADMAP.md`, and the canonical version
authority in `docs/changelog.md` (the release history — no version line
is carried in this header; a worklog-header version is a stale-claim
hazard by construction).

---
---
Task ID: 40 (v1.10.15-fixed, run #44 cycle) — tree pinned at f5a31cf (run #43: Windows x64 15m16s + Linux x64 11m32s RED, acceptance 25/26 on BOTH OSes — navChromeSevenViewsLoad "nav button missing for view: chat" deterministic; 16 errors incl. the false "win-go-vet.log exists but is EMPTY" capture-defect class; ONE all-logs artifact, zero success notices)
  R1: the nav Repeater delegate's `required property int index` is never initialized by QQuickRepeater on JS-array models (Qt 6.8.2) — every delegate aborted creation — replaced by SEVEN static-objectName nav buttons (findChild contract, visuals, order byte-equal; WorkspaceView's recents delegate carried the same silent class and is restored).
  R2: per-engine Core provider (qmlRegisterSingletonType) in main.cpp + tst_acceptance retires the one-engine warning and the SettingsView:308/316/342 undefined-config TypeErrors; SystemView's envelope text binding guards its own nested read.
  R4/R5: the Windows Go-verification summary implements the three-class clean-silent guard (gate-order marker-based; Linux twin already correct); `function action()` → `submit()` (final-member QWARN); the harness pins QQuickStyle "Basic" (the Windows-only background-customization QWARN class).
  Battery (verbatim, I14): RED-first 7 assertions RED at f5a31cf → release suite 101/101; units 242/242; lint 0 warnings 0 errors; typecheck exit 0; web build exit 0; YAML parse OK; §9 balance scans ALL PASS (6 changed C++/QML files). NOT RUN locally: gofmt/go build/go vet/go test/E2E — no Go toolchain in the session.
  Boundary: hosted verification NOT RUN at doc time — the operator pushes; the next run must prove acceptance 26/26 on both OSes, ZERO "Cannot read property" lines, zero capture-defect errors, and the dist+installer artifacts returning at 1.10.15 names. The physical install / every-view-tab-section / uninstall retest is the operator's.
  Future work: keep the Repeater-required-index scan in the suite so the delegate class cannot re-enter; Windows physical install smoke stays hosted-only.
---
Task ID: 39 (v1.10.15-fixed, run #42 cycle) — tree pinned at bf6cf2d (run #42: all five jobs green, 20 success notices, four artifacts, ONE log artifact; the operator's physical log: clean boot/close with the Qt surface judged broken — the Wails/Qt parity gap is the defect of the cycle)
  R1 (D28/I21): full Qt parity landed — Main.qml gains the seven-tab nav rail over a StackLayout; NEW SettingsView (all 8 sections, the full 72-field FIELD_PLACEMENT census wired through the now-implemented config read), AgentView (goals sidebar + shared transcript + plan task panel), WorkspaceView (+ repository index panel), LabView (+ verbatim /api/lab request console), AutomationView (tasks + pause/resume/cancel/run + artifacts), SystemView (preflight/governor/maintenance), HistoryDrawer, ModelPickerDrawer, MyToolsDrawer, LogViewerDrawer, PerfStrip; coreclient +15 glue methods and +13 properties, each a 1:1 client of an existing handler; tst_acceptance +8 parity gates; CMakeLists registers all 13 QML files.
  R2 (D29/I17): success-path ::notice emissions became plain step output and the five NEVER-STARTED guards became ::warnings — the suite now pins a zero-notice green run, the warning class, and the manifest rows.
  Battery (verbatim, I14): release suite 93/93 (12 RED-first at bf6cf2d); gofmt clean; go build + vet exit 0 on linux and windows; go test 65 ok exit 0; units 242/242; typecheck exit 0; lint 0 warnings 0 errors; build:e2e exit 0; Playwright E2E 40/40; YAML parse OK; §9 balance scans ALL BALANCED on 15 changed C++/QML files; identity frozen at 1.10.15.
  Boundary: hosted verification NOT RUN at doc time — the operator pushes; the next hosted run must prove the Qt compile, the extended acceptance suite over the new surfaces, the zero-annotation green run, and every #42 gate held; the physical install / exercise-every-tab-and-section / uninstall retest is the operator's. Future work: the models/recommendations card on SystemView (the one System row scope cut, recorded in the manifest).
---
Task ID: 38 (v1.10.15, run #41 cycle) — tree pinned at 246eaba (run #41: audit 3m54s + preflight 19s green, Windows GREEN a second time incl. the silent-install smoke, Linux RED at step 15 "Go verification" exit 1 (anchor step:15:312) after its headless gate-OK (step:15:149); E2E/stress/Qt never ran; four if:failure() summaries mis-fired never-started errors, and the "build:e2e is the failing gate" annotation was summary boilerplate for a stage that never began)
  R1: the run-#41 red gate is Linux Go verification — the full CI-order local battery is GREEN on the exact tree (headless 64 ok, full 65 ok with the real shtn-engine-host, vet both GOOS, build:e2e, E2E 40/40, ctest 13/13), so the exit-1 root cause is UNVERIFIED from anonymous evidence and no guess shipped; NEW "Go verification failure summary (Linux)" publishes the gate's captured FAIL/compile/vet/panic lines as capped ::error annotations (D5: failures visible, nameable, faster to fix).
  R2 (D24/I17): guard classes split everywhere — absent first-byte log = ONE never-started ::notice + exit 0 (E2E, stress, Qt smoke reclassed; Qt acceptance already correct); existing-but-empty log = the named capture-defect ::error; the E2E failing-gate claim is evidence-gated on build-log content; RUN/OK markers + green notices on build:e2e/playwright; RED-first 5 assertions, suite 85/85.
  R3/R4 (D25/D26): main.cpp applicationVersion 1.10.1→1.10.15 (I6 drift, now pinned by the suite to package.json); live bubble bounded + scrollable (composer's own ScrollView pattern); identity 1.10.15 on all surfaces via the one machinery.
  Boundary: hosted verification NOT RUN at doc time — the operator pushes; the next run proves the Go-verification summary wiring, the guard classes, the stress suite, the Qt smoke + acceptance path, and the 1.10.15 artifact names.
  Future work: Node 26 LTS pin (LTS 2026-10-28); if the Go verification red repeats hosted, its annotations now carry the failing sub-gate and test names verbatim.
---
Task ID: 37 (v1.10.14-fixed re-release, run #41 cycle) — tree pinned at e1db00c (run #40: audit 3m43s + preflight 42s green, Windows 18m21s GREEN incl. the silent-install smoke's first hosted PASS, Linux red at step 22 Stress suite 20s exit 1 with ZERO annotations — the content-free red D17 bans; eight log artifacts incl. the byte-identical preflight duplicate)
  R1 (the red gate, pinned → fixed): local repro verbatim (go1.27.3 linux): release_surface FAIL "CI toolchain not pinned to 1.26" — the Go check lagged the workflow's own 1.27/v7 pins; fix follows the workflow forward (1.27, upload-artifact@v7, download-artifact@v8) + the stress gate gets the full capture contract (first-byte log, RUN/OK markers, green notice, if:failure() FAIL/panic/verdict summary); 47 pass / 0 fail locally after.
  R2 (D21): per-job <job>-logs-part-attempt parts (if:always(), the job's LAST step after Run summary, retention 1) + scripts/consolidate-logs.mjs (deterministic merge, fixed job order, MISSING PART = named instrumentation failure, exit 1) + consolidate-logs job (download-artifact@v8 pattern+merge-multiple → ONE SHEYTAN-LA-v1.10.14-all-logs/all-logs.txt → parts deleted in-run via gh api -X DELETE, actions: write) — the run's final artifact state is ONE log artifact.
  R3: every pin re-derived at P1 from primary sources; only download-artifact v7→v8 moved (v8.0.2, 2026-10-07); Node 26 LTS (2026-10-28) is future work.
  Battery (verbatim, I14): gofmt clean; go build + go vet exit 0 on linux and windows; go test headless+full exit 0; audit race row exit 0; units 242/242; release suite 78/78 (RED-first: 8 failures at e1db00c); lint 0 warnings; typecheck exit 0; stress 47/0; YAML parse OK; identity 1.10.14 frozen; NO C/C++ files in the diff.
  Hosted boundary (NOT RUN at doc time): the stress gate's first hosted verdict, the Qt smoke + acceptance path unblocked for the first time since #38, the ONE-artifact final state, part deletion, and the carried Windows/E2E wins are the next hosted run's to prove; the physical install/boot/close/uninstall retest is the operator's. Future work: Node 26 LTS pin after 2026-10-28.

---
---
Task ID: 36 (v1.10.14-fixed re-release, run #39 cycle) — tree pinned at ace68fd (run #39: preflight 37s green, audit 3m38s green, Windows 15m06s RED, Linux 10m48s RED, 18m52s total; the installer artifact 25.8 MB proved makensis itself compiled)
  Pinned at P1: the two Windows "gate produced no log" catchers were FALSE ALARMS — Tee-Object never runs on empty pipeline input, so vet (silent on success) and gofmt (clean under Go 1.26) passed but left no logs; the ONE true Windows red was the include-marker assertion; makensis verbosity compile-proven locally (nsis 3.10 extracted from Ubuntu debs: 0 marker hits at -V1/-V2/-V3/default, 1 hit at -V4).
  Shipped: R1 makensis -V4 + gofmt -w internal/research/service.go (1.27 drift) + GO_VERSION "1.27"; R2 first-byte logs on every Windows/Linux/audit gate + per-gate OK notices + command-naming no-log guard + <job>-ci-logs uploads if:always(); R3 planFinalisation pure contract + finaliseRun bounded retry wired + promotedPartial preservation (exactly-one-row on every settled path); R4 cache v6 + upload-artifact v7 (others verified current) + the L500 startsWith lint fix.
  Battery (verbatim, I14): gofmt clean; go build + go vet PASS on linux and windows; go test headless + full exit 0; release suite 72/72; units 242/242; lint 0 warnings; typecheck exit 0; E2E live-stream 3/3 + chat/run-control 10/10 LOCAL (chromium 1243, real C++ engine host built locally).
  Hosted boundary (NOT RUN at doc time): the Windows gates, the marker in the captured log, the silent-uninstall smoke's FIRST verdict, the hosted E2E and the Node-20-warning-gone are the next hosted run's to prove; the physical install/boot/close/uninstall retest is the operator's. Future work: Node 26 LTS pin after 2026-10-28; Qt/aqtinstall/NSIS versions frozen by charter.
---
Task ID: 35 (v1.10.14-fixed re-release, run #38 cycle) — tree pinned at 4755df8 (run #38: audit green 3m39s; BOTH platform jobs red at go test — desktop.go did not compile; 21m29s; the run-#38 catcher shipped as dead code)
  R1 (the red gate, pinned → fixed): the compiler's own words — "ipc.Stop() (no value) used as value" (L165) and "srv.Close() (no value) used as value" (L136); both methods return nothing; fix removes both use-as-value captures, each deferred site logs the named Info line ("ipc stopped", "server closed"); NO signature changes; exit codes/control flow untouched; the catcher's 9 regression tests GREEN.
  R2 (D16 capacity): concurrency group ${{ github.workflow }}-${{ github.ref }} + cancel-in-progress:true; preflight go-build job wired needs:[audit,preflight] into both platform jobs; Playwright browsers cached on the lockfile key; timeout-minutes verified on every job (preflight 10). Additive only (I16).
  R3 (D15/I15): first-byte log capture (E2E, preflight) + named "gate produced no log — capture defect" guards in the E2E / Windows go-log / Qt-smoke summaries; RED-first: installer-contract.test.mjs +7 tests (6 RED on the pre-fix workflow), release suite 64/64 exit 0 post-fix.
  Battery: go build/vet/test PASS + gofmt clean on changed files (Go 1.27.3; pre-existing internal/research/service.go flagged only by 1.27 — clean under the pinned 1.26, untouched); YAML parse OK; §10 NO C/C++ files. Hosted boundary: preflight latency, cache hit rates and the carried fixes' FIRST hosted verification (uninstall symmetry, include marker, both smokes, naming map) are HOSTED-provable only; the physical retest (install → launch → close → uninstall) is the operator's. Hosted verification: NOT RUN at doc time (operator push pending). Future work: Qt C++/QML changes and the Linux executable's name stay out of scope this cycle.
---
Task ID: 34 (v1.10.14-fixed re-release, run #37 cycle) — tree pinned at c94d172 (run #37: audit+Linux green, Windows red at the silent-uninstall smoke, 18m51s)
  R1 (the red gate, pinned → fixed): the generated payload-uninstall.nsh opened with "!macro" only — the .nsi's "!ifdef SHEYTAN_PAYLOAD_UNINSTALL" tests the DEFINE namespace, was compile-time FALSE, the uninstall list never compiled in; fix = generator emits the "!define" before the macro + in-guard "!echo" marker + makensis asserts the marker in win-nsis.log + the smoke's throw names the surviving targets.
  R2 (D8): run-phase catcher — reportUIExit/uiExitReason/showUIExitBox in desktop.go: abnormal UI death logs the named ERROR "ui exit" line + MessageBox (reason+log path); clean close ONE Info line; deferred ipc.Stop/srv.Close errors log; banner/endpoint/signal prints gain log pairs; dead uiSpawnTimeout removed; exit codes/control flow untouched.
  R3 (D7/I11): ONE naming map — LINUX_PKG_ROOT "SHEYTAN-LA" (Linux exe name kept), unified asset names ROOT-v{ver}-{windows-x64-installer.exe|windows-x64.zip|linux-x64.zip}, release title "SHEYTAN-LA v{ver}" + structured body, uploads if: always(), ONE run-summary table per build job; internal/releasecontract + release-version.mjs/release-version.test.mjs flipped in lockstep (drift would have failed the stress gate / release suite).
  Battery: suite 57/57 exit 0; YAML parse OK; go build/vet/test NOT RUN (no Go toolchain this cycle — hosted authoritative); §10 NO C/C++ files. Hosted boundary: uninstall symmetry, the marker assertion and the smokes are HOSTED-provable only; the physical retest (install → launch → close → uninstall; app.log names any failing step) is the operator's. Hosted verification: NOT RUN at doc time (operator push pending). Future work: none this cycle.
---
Task ID: 33 (v1.10.14 run #36 cycle) — tree pinned at 67797c1 (run #36: first fully green hosted matrix, 20m43s; the physical defect: the installed product closed instantly twice, app.log showed full boot then "[boot] exit code 1", zero error lines)
  Shipped R1 (payload, pinned → fixed): installer payload staging (portable-only excluded) + makensis from the payload dir + `File /r "${BUILDDIR}\*.*"` + compile-time FileExists guards (sheytan-ui.exe, Qt6Core.dll) + CI-generated payload-uninstall.nsh included via !include /nonfatal + !ifdef-guarded insertmacro — uninstall removes EXACTLY the payload, user data survives, "RMDir /r" negative verbatim.
  Shipped R2 (visibility): named-step ERROR lines on all 8 desktop fatal paths (failBoot) + platform.MessageBox (user32 MessageBoxW syscall-only; ErrUnsupportedPlatform elsewhere) + the UI stdio→log adapter ("ui" tag, new internal/desktop/ui_stdio.go) + config.Load failure logging (main_windows.go) + root's ONE default-path ERROR summary; exit codes and control flow untouched.
  Shipped R3 (gate): silent-install smoke — installs the built installer (/S /D), asserts the pair in the INSTALLED tree, boots the installed core (SHEYTAN_UI_EXECUTABLE=none; banner + boot markers + liveness + no crash + bounded stop), then silent uninstall proves payload-gone + data-survives; gate-order RUN/OK pairs + one ci-summary line.
  Shipped R4: package.json → 1.10.14; release-version.mjs re-derived config.go/build.yml/SIGNATURE; no stray identity surface; installer-contract.test.mjs added to test:release (RED-first proven, 46/46 green).
  Battery: suite 46/46 exit 0; YAML parse OK; gofmt clean; go build/vet/test ./... PASS (Go 1.23.4 local); §10 NO C/C++ files. Hosted boundary: the payload guards, the installed boot and the uninstall symmetry are HOSTED-provable only; the physical retest (installer → launch → desktop opens; on failure app.log now names the step) belongs to the operator. Hosted verification: NOT RUN at doc time (operator push pending). Future work: none this cycle.
---
Task ID: 32 (v1.10.13 run #35 cycle) — tree pinned at 009df42 (Linux x64 FULLY GREEN end-to-end first time ever; audit green incl. race gate; 18/18 slots BOTH platforms; parked + chat-persist held; Windows red in the never-before-executed post-acceptance packaging chain, outside every tracked gate)
  Shipped R1 (pinned → fixed, gate intent untouched): installer.nsi VIAddVersionKey "ProductVersion" "${VERSION}" → "${VERSION}.0" — ONE content line (numstat 1/1, one inline run-#35 comment); the string-table key shadows the four-part VIProductVersion in .NET FileVersionInfo.ProductVersion, so the W-INST-VR gate ("<APP_VERSION>.0") threw deterministically after "OK NSIS makensis" — exactly the fallback signature run #35 published.
  Shipped R1 regression: static identity-contract assertion in scripts/release-version.test.mjs (the existing release-metadata suite behind "npm run test:release") — RED-first proven on the pre-fix .nsi (exit 1, actual '${VERSION}' vs expected '${VERSION}.0'), GREEN post-fix (39/39, exit 0); hosted enforcement rides the audit job, no Windows runtime needed.
  Shipped R2 (visibility class): six additive gate-order RUN/OK pairs across the whole post-acceptance chain (package prepare, ZIP create/verify/integrity, NSIS post-build contract, installer verify) — 12 hunks, 27 insertions, 0 deletions; Failure-summary logic untouched (trailing-RUN-without-OK already names the gate via lastGate inference); uploads stay untracked by design.
  Battery: suite 39/39 exit 0; YAML parse OK; diff --check clean; diffstat ⊆ allowlist (3 files); gofmt/go build/go vet NOT RUN (no Go toolchain in the sandbox; zero Go files in the diff); §10: NO C/C++ files in the diff (recorded, balance scan not applicable).
  Boundary: run #36 owns every hosted verdict — the ProductVersion gate passing, the new markers naming (not masking) any future packaging failure, parked slot + chat-persist holding, the race-gate verdict unmoved on the suite change. Hosted verification: NOT RUN at doc time (operator push pending). Docs caps honored (one ≤10-line entry each here and in changelog.md).
  Future work: NSIS installer logic beyond the one version-resource line and the four residual direct JSON writers (lab.go writeJSONStatus, aisystems export, automation artifact bytes, clone structured error) stay future-work; the chat-persist recurrence watch stays silent.
---
Task ID: 31 (v1.10.13 run #34 cycle) — tree pinned at 6141c6c (audit green incl. race gate + native C++; 17/18 slots green on BOTH platforms; parked red on both, every error annotation deriving from that one slot)
  Shipped R1 (framing class, pinned → fixed): raw-TCP probe TestGoalParkedStateListRawTCPFraming REPRODUCED at the parked state — list head "Transfer-Encoding: chunked", Content-Length ABSENT, single-goal control Content-Length: 1577 — the client's documented contract (coreclient.cpp) claims no chunked decoding, and net/http chunks any undeclared body over its ~2048-byte buffer; the parked list was the project's first response ever to cross it. FIX: framedJSON at the one boundary (server.go writeJSON/writeErr) — marshal fully, declare the exact Content-Length, write once; class over instance.
  Shipped: TestGoalListOverBufferFramingDeterministic (engine-free over-buffer list, >2048 premise asserted) — both new tests FAIL on pristine 6141c6c, PASS post-fix (list head Content-Length: 3079 == delivered, no Transfer-Encoding, both parked goals waiting_for_approval in the list view).
  Shipped: tst_acceptance.cpp ONE framing-header dump line (goal LIST → framing headers: …, defaulted ipcRequest out-param, both channels, emitted before any status/parse branch); no assertion/condition/bound/existing line touched; §10 balance scan + symbol inventory PASS.
  Battery: gofmt (one pre-existing internal/research/service.go violation at 6141c6c left untouched), go build ./..., go vet ./..., go test -race ./internal/api ./internal/goal — green; workflow byte-identical; diff --check clean; diffstat ⊆ allowlist (3 files); Qt host build + acceptance NOT RUN locally (no Qt toolchain).
  Boundary: run #35 (fourth hosted acceptance ever) owns the verdicts — the audit job must absorb the two new tests unchanged; the parked slot is expected GREEN on both platforms with the named framing mechanism. Hosted verification: NOT RUN at doc time (run #35 pending).
  Future work: route the four residual direct writers (lab.go writeJSONStatus, aisystems export, automation artifact bytes, clone structured error) through the framed boundary in a later cycle; the chat-persist recurrence watch stays silent.
---
Task ID: 30 (v1.10.13 run #33 cycle) — tree pinned at 30c4f27 (audit green incl. race gate + native C++; acceptance executed 2nd time ever; parked red BOTH platforms, Windows chat-persist 'firstPersisted' FALSE)
  Shipped R2 (root-caused + fixed): the orchestrator's done/complete broadcast rides the activity callback INSIDE RunDetailed while the reply's AppendMessage runs in finishSuccessfulRun after it returns — the Qt settle-path reload (coreclient.cpp:843) races the append and nothing re-fetches after (evidence: orchestrator.go:2136/2144, the callback's publish-first ordering, runcontrol.go:123). server.go now holds terminal frames at the ONE broadcast boundary; runcontrol.go flushes at the transcript commit (pause/error/append-failure paths flush defensively, I3; seq + frame order unchanged); TestDoneFrameFollowsTranscriptCommit pins it 3/3 green.
  Shipped R1: TestGoalParkedStateListMatchesSingleGoalEnvelope — byte-compare of BOTH HTTP views at the parked state through the real stack: BYTE-IDENTICAL (server projection exonerated, V3a dead); client parsing inspected clean, coreclient.cpp untouched; the raw-list dump line ships as the run-#34 discriminator.
  tst_acceptance.cpp: two dump extensions only (goal %1-list → … / chat-persist[raw:%1] → …, both channels, §9-pattern-compatible); no assertion/condition/bound touched; §10 balance scan + symbol inventory PASS. Battery: gofmt/build/vet/-race (api 131s + goal) green; workflow byte-identical; Qt host build + acceptance NOT RUN locally (no Qt toolchain).
  Boundary: run #34 (third hosted acceptance ever) owns the verdicts — the Windows chat-persist green now has a named mechanism; a parked green WITHOUT the raw-list line confirming list-state delivery stays an open observation gap; docs caps honored (one ≤10-line entry each here and in changelog.md).
  Future work: the resume path's same-class race (runcontrol.go:1090 callback keeps today's broadcast timing, flushTerminal nil there); run-#34 raw-list line analysis if the parked slot stays red; QWARN cosmetics unchanged.
---
Task ID: 29 (v1.10.13 run #32 cycle) — tree pinned at 1960bb5 (audit green incl. race gate; both Qt host builds red on the single C2059)
  Shipped: tst_acceptance.cpp line 535 — exactly one ')' deleted in dumpChatPersistDiagnostics's chat-persist dump (the emitDumpLine rewrite dropped one closer at four of five sites and missed this one). I7 added to the never-regress set: no C++ edit commits without a recorded whole-file balance scan + symbol inventory — both ran clean pre-commit (1257/1257 parens, 213/213 braces, duplicate bodies none).
  Boundary: run #32's acceptance never started (compile gate); run #33 will be only the second hosted acceptance ever. Hosted verification NOT RUN at doc time (operator push pending); the audit job must not move on run #33.
  Future work: if the parked-goal slot stays red on run #33, its now-visible goal-dump diagnostics (§9 DUMP-DIAGNOSTIC notices) pin the discriminating layer — the silent ask-risky downgrade on a failed systems-store read stays prime suspect — for the next cycle.
---
Task ID: 28 (v1.10.13 cycle) — tree pinned at 57d2592 (baseline 0cdd705 + operator doc uploads, all three follow-up hosted runs canceled)
  Actual results: Probe A reproduced 2/2 (cancel race: cancelled → blocked → paused, unguarded MarkStep/SetVerification); fixed at the pinned layer, green 2/2; Probe B NOT-REPRODUCED 2/2 (park machinery exonerated at the packaged timing). §10 dual-channel dumps + cancel-leg dump shipped; §9 severity contract + accounting line in both failure summaries; C1 confirmed per Qt docs. Battery: gofmt/build/-race/vet/internal-65/65/YAML/C++ inventory/workflow fixtures 30/30 — green. Docs caps honored (FEATURES.md 119 lines; one ≤10-line entry each here and in changelog.md).
  Unresolved risks: hosted verdict for the whole cycle NOT RUN (no push authority) — run #29 on the pushed commit owns G1-G5's hosted proof; Windows capture fixture NOT RUN (no pwsh); the parked-goal residual delta (packaged core/IPC/Qt-client, V8 fallback) and Main.qml:569:41/OpenThemeData/CI-font QWARNs remain classified, never chased.
  Future work: step-activation discarded mutate (~runner.go:390) needs a deterministic failing-without-it test; V8's silent ask-risky downgrade on systems-store lookup failure; warnings digest on green runs; Qt suite splitting.
---
Task ID: 27 (v1.10.13 run #27 cycle) — agent: compile-repair + silent-return session; tree pinned at 7fdee73
Task: repair the run #27 Qt compile break (duplicate dumpChatPersistDiagnostics) so the acceptance suite finally executes hosted, land the one structural item (CoreClient openSession silent-return → typed error), read the first real hosted verdicts for the parked-goal and chat-persist slots
Work Log: tst_acceptance.cpp — exactly two hunks: the byte-identical second dumpChatPersistDiagnostics block (comment + definition, 512–546 at 7fdee73) deleted, one forward declaration added after the includes; call sites (284/314 post-edit), waits, bounds and the surviving body untouched; Csymbol inventory PASS — every file-scope definition unique, declaration line 49 above both call sites.
coreclient.cpp — openSession malformed-payload branch: the silent `if (!ok) return;` (354–356 at 7fdee73) now sets the typed error "session %1: transcript reload: malformed payload" via the existing setError channel before returning; well-formed path byte-identical, no new signals, no API change; inventory PASS.
Local — go build ./... green; go test -race ./internal/goal (1.3s) and ./internal/api (119.4s) green; gofmt clean; diff vs 7fdee73 = exactly the two files above; workflow, Go tree and protected docs byte-identical. NOT RUN locally — Qt host build and Qt acceptance (no Qt toolchain in the sandbox); hosted proof belongs to run #28 on the pushed commit.
Future work (scope unchanged): CI restructuring, Qt suite splitting, revision-addressed snapshot history, installer/NSIS work, any frontend or publish-job change, any workflow edit (frozen this cycle), raising any wait bound without hosted evidence. Workspace-path restructuring (flattening D:\a\SHEYTAN-LA\SHEYTAN-LA) is CLOSED-WONT-FIX — GITHUB_WORKSPACE is fixed by the GitHub-hosted runner contract (https://docs.github.com/en/actions/reference/workflows-and-actions/variables — GITHUB_WORKSPACE defaults cannot be overwritten; https://github.com/actions/checkout#inputs — the checkout path is constrained beneath github.workspace) and nothing in the repo references the absolute path.

---
Task ID: 26 (v1.10.13 run #26 cycle) — agent: park diagnosis + structural session; tree pinned at ffcf2c8
Task: root-cause the deterministic ask-all park failure (goalsApproveRejectParkedGoal, red on three consecutive hosted runs), deliver anonymous diagnosability for it and the Windows-only chat-persist failure, land the two structural items (phantom gate wiring, drive-registry extraction)
Work Log: probe TestGoalAskAllTwoGoalsParkConcurrently — the exact Qt shape (two ask-all goals started at creation through the real HTTP surface, chunked SSE fixture engine) parks BOTH goals in 0.17s locally, full race battery green — NOT REPRODUCED; the probe stays as the regression pin and the hosted QWARN facts discriminate the client/packaged path.
goals.go — goalApprovalGate{} wired via agent.WithApprovalGate when a snapshot is bound (the documented denial gate: denial-as-evidence, never a run error; unbound goals unchanged); the drive registry moved out byte-identically (4086 bytes, mechanically verified against ffcf2c8) into goal_drive.go.
tst_acceptance.cpp — dumpChatPersistDiagnostics on both assistant-count waits (messages/roles/working-fine/error/busy/liveActive/session); conditions and bounds unchanged. coreclient.cpp untouched — the settle→reload analysis pins nothing: the core appends the assistant reply BEFORE the done frame, and every client failure mode (non-200, timeout, malformed payload) is exposed by the new QWARNs.
build-desktop.yml — exactly two hunks: one QWARN arm in the Windows errClasses scriptblock, QWARN inside the Linux failClasses alternation; every protected surface byte-identical.
Local — go build ./... green; go test -race ./internal/goal (1.2s) and ./internal/api (116–120s) green; gofmt clean; workflow YAML parses (two hunks proven).
NOT RUN locally — the Qt host build and the Qt acceptance suite (no Qt toolchain in the sandbox); hosted proof belongs to the run on the pushed commit.
Future work (scope unchanged): CI restructuring, Qt suite splitting, revision-addressed snapshot history in the systems store, installer/NSIS work, any frontend or publish-job change, raising any wait bound without hosted evidence.

---
Task ID: 25 (v1.10.13 run #25 cycle) — agent: compile unblock + residual Go repairs session; tree pinned at 2d07da6
Task: fix the confirmed QJsonValue compile blocker in tst_acceptance.cpp, close the residual Go lifecycle gaps (approval identity binding, rollback error propagation, frozen-revision probe), prove locally what the sandbox can prove
Work Log: tst_acceptance.cpp — dumpGoalDiagnostics: QJsonDocument parse → isObject → value("goal") → isObject → toObject().toVariantMap(); named QWARN on malformed JSON, missing goal, non-object goal; sweep confirmed the other 12 .toMap() calls are QVariant-based.
goals.go — the ApprovalCheck caller builds goal.ActionIdentity{step, tool, objective} from the live plan and logs a typed grant rejection; resumeAndDrive + approve-restart compensations structured-log their failures (I3); goalRunFunc resolves via resolveGoalSystemSnapshot.
goal.go — ApprovedAction carries ToolName+Objective; ActionIdentity type; objectiveOf; ErrApprovalIdentity; TakeApprovedAction(id, want) typed contract; RunRequest.SystemRevision; runner passes the frozen revision on all five phase requests.
Tests — TestApprovalIdentityBindsExactAction (objective/tool mismatch → typed, no side effects, resolve replay typed), TestRunRequestsCarryFrozenRevision (every phase carries the frozen revision), TestGoalSystemRevisionFrozenContract (revised system → typed ErrInvalid, goal keeps rev 1); TestApprovedActionMismatchedStepNotConsumed strengthened to the typed contract; all deterministic, channel/poll-based.
Local — go build ./... green; go test -race ./internal/goal (1.2s) and ./internal/api (119.5s) green; gofmt clean; CMake project() 1.10.6→1.10.13.
NOT RUN locally — the Qt host build and the Qt acceptance suite (the sandbox has no Qt/CMake and no root to install them); the hosted run must carry that proof or the cycle stays honest-red.
Future work (scope unchanged): CI restructuring, Qt suite splitting, revision-addressed snapshot history in the systems store.

---
Task ID: 3 (v1.10.13 final cycle) — agent: Go source repair session; tree pinned at fa6afd1
Task: repair the four verified Go defects named by run 39243477114 (Linux 16 passed / 2 failed: "goal resume failed (status 500)",
park timeout with zero diagnostics; Windows exit 2, empty captures), capture the Windows report, encode the never-again invariants
Work Log: goals.go — goalDriveRecord{gen,cancel,done}; goalDriveAdmit under ONE lock (I1); goalDriveRelease generation-safe (I2);
goalDriveCancel retains until unwind; resumeAndDrive bounded serialization (typed 409, pause-rollback on admit conflict);
settleDriveError observed completion (bounded evidence + blocked, I3/I6); goalRunFunc binds SnapshotOf (typed failure, no active
substitution); ApprovalCheck consumes the ONE-USE grant (I4); writeGoalErr errors.Is. goal.go — Resume excludes
waiting_for_approval; ResolveApproval arms/clears ApprovedAction; TakeApprovedAction; SettleDriveFailure. Workflow — the -o report
both platforms, non-empty assertion on nonzero exits, if:always() uploads, summaries read the report. tst_acceptance.cpp —
dumpGoalDiagnostics (status/phase/driving/last evidence) on the park and resume waits.
Battery: go build ./... OK; go vet ./... OK; go test ./internal/... exit 0 (65 packages) and -race clean; gofmt clean;
Qt acceptance build NOT RUN (no Qt toolchain). ZIP: git-archive of the tracked tree, single top-level dir, web/static/ present,
no .git/build trees, no backslash entries, SHA-256 recorded.

Task ID: 2 (v1.10.13 fix cycle) — agent: hosted-defect repair session; tree pinned at 161ea36b
Task: restore the Linux platform env, unmask the empty CTest, make the red gates self-naming, keep the acceptance claims
honest, close the two failing Windows slots — the run-#23 cycle (39129668510: Win acceptance exit 2, Linux exit 134, zero FAIL lines)
Work Log: L-QPA — export QT_QPA_PLATFORM=offscreen restored (the W-UIBIN hunk deleted the live export beside the dead one);
L-CTEST — the empty masked ctest invocation removed; W-ANN-ORDER — signal-first gate tail (FAIL lines, then evidence, prelude
last) + failing slot names mirrored into the uncapped job summary + R-UNBUF (stdbuf -o0 -e0, separate stderr on Linux);
T-INTEGRITY — the false "REAL Main.qml" claim re-labeled in code + ONE bounded genuine-QML slot (qmlLoadsCleanly) added;
GOALS-OBS — waitForGoalState added: both slots' waits polled a frozen list (the client owns no goal state beyond its last
read — the async transition was provably unobservable); same conditions, same bounds, the re-read added, nothing weakened.
Battery: Qt 6.8.2 syntax compile OK (one real API bug caught: qmlRegisterSingletonInstance takes const char*), YAML parse OK,
bash -n OK on all three edited steps; Go/link/runtime NOT RUN — no Go toolchain, no cmake, no root. ZIP: git-archive, tracked tree only.

---
Task ID: 1 (v1.10.13 fix cycle) — agent: hosted-defect repair session; tree pinned at 070bc61
Task: repair the four defects named by run 39115765789 ("Build Desktop" #22: audit green, Win steps 1-21 green,
Linux 1-16 green; W step 22 exit 1, L step 27 exit 127, E2E waitForResponse timeout at sessions.spec.ts:170:1)
Work Log: W-UIBIN (dead SHEYTAN_UI_BIN pin deleted), W-SUM127 (backslash-n corruption rewritten + upstream branch),
W-PIPE (|| true before PIPESTATUS repaired) — class sweeps, YAML parse, bash -n and a 3-scenario summary simulation green;
L-E2E decision (local inspection class): no change — the sidebar boot path is intact, local repro unavailable (no Go);
L-ACC pre-read: both slots' wait contracts satisfiable under the deterministic fixture (the ask-all park is policy-driven,
not model-driven) — hypotheses stay runtime-class, run #1's FAIL! lines are the authority; local battery green (tsc, lint,
units 236:0, release incl. codename gate, vite build); Go/Qt gates local-unavailable. ZIP: git-archive of tracked tree only.
---
Task ID: 1 (v1.9.5 session)
Agent: v1.9.5 engineering session
Task: v1.9.5 — repair the failed v1.9.4 release attempt (Actions run
38090590263, lint blocker at scripts/windows-live-answer-acceptance.mjs),
audit the v1.9.4 diff as untrusted new code, repair every reproduced
P0/P1 (harness false-proof paths, loopback transport exposure, engine/run
admission TOCTOU, identifier-bearing ordinary logs), and ship the
complete verified source ZIP

Work Log:
- Baseline: HEAD `171f6b7` (v1.9.4) == live origin/main (re-fetched and
  compared during the session — the checkout was the tip, not a stale
  clone). GitHub REST API rate-limited (unauthenticated): the Actions
  run logs were not fetchable, so the documented lint failure was
  REPRODUCED LOCALLY instead (exact oxlint error at :67/:136).
- Reproduced and confirmed against source: duplicate `const sleep`;
  growth contract accepted completion-time samples and settled rows as
  substitutes; OBSERVE_SETTLED counted rows; proc.kill() masquerading as
  window close; trace armed after module load (hook never installed) +
  a process env var the page never read; `ws` undeclared; the harness
  referenced by NO workflow; loopback.go serving the ENTIRE API on
  127.0.0.1:0 with no authentication; ws.go accepting an absent Origin;
  config.ts startsWith prefix origin matching; triple closeLoopback in
  Close(); StartLoopback before EnsureSetup with a non-fatal bind
  failure; duplicate SetRunGate installation; updateEngineVariantTx
  single-check TOCTOU; gpuEvent printing `runId=`; runId/session fields
  in ordinary logs across server.go, runcontrol.go, history.go,
  edittx.go.
- IMPLEMENT (P0): rewrote the acceptance harness to the ordered-evidence
  contract (sample A nonblank+non-placeholder+Stop up -> strictly larger
  sample B with Stop still up -> terminal settlement; any absence
  FAILS; settlement asserts non-whitespace final Markdown, no
  placeholder, exactly one assistant reply AND the expected user
  message; reload proven via the sidebar `data-session-id` active-row
  contract; real WM_CLOSE via taskkill without /F with bounded exit and
  failure-only forced cleanup; trace armed via CDP
  addScriptToEvaluateOnNewDocument BEFORE modules load, hook presence +
  expected stages + sanitization asserted; `ws` pinned 8.18.3 +
  lockfile). Wired the harness as a REQUIRED step of the Windows CI job
  (real packaged exe + native engine built by CMake + canonical
  e2e/make-e2e-model.py fixture + desktop config), after the existing
  runtime smoke.
- IMPLEMENT (P0, transport): loopback listener narrowed to EXACTLY ONE
  route (/ws/activity -> the SAME handleActivityWS); 256-bit
  crypto/rand per-server capability token, constant-time compared,
  required on every upgrade, delivered only via GET /api/ws-origin
  ({origin, token}; token only with an origin; never logged); absent
  Origin refused on the loopback path (no bypass); Host must equal the
  listener address; forwarding headers refused; everything else 404.
  src/loopback-origin.ts: strict URL-parse validation (exact normalized
  loopback http origin, canonical-form round trip refusing 127.1-style
  shorthand), bounded 3-attempt discovery retry on desktop pages with
  an explicit visible degraded state (activityTransportDegraded +
  persistent MessageStream banner); desktop.go sequences EnsureSetup
  BEFORE StartLoopback and treats a bind failure as a hard non-zero
  exit; lifecycle.go keeps exactly ONE idempotent closeLoopback (test
  pins concurrent Close); duplicate SetRunGate removed.
- IMPLEMENT (P1, admission): llm.EngineAdmission seam — the transaction
  claims admission exclusivity first, atomically with the live-run
  verdict under the API server's runsMu (the same section registration
  uses), and holds it across the whole transaction; runs admitted under
  a claim get the deterministic 503 (errEngineMutationInProgress) with
  the persisted transcript restored (preMutationMessages snapshot); a
  run live at claim time still refuses the transaction
  (ErrEngineBusyWithRun); resume honours the claim; nil seam preserves
  the historical behavior; runsMu stays a leaf lock (no deadlock).
- IMPLEMENT (P1, logs): gpuEvent now logs an ephemeral probe ordinal
  (probe=%d) — no runId; runId=/session= removed from every ordinary
  production log line tree-wide (settle summary, pause/resume/edit-tx,
  history); durations/counters kept; format-arg consistency verified by
  go vet.
- TESTS: internal/api/loopback_v195_test.go (least-privilege surface
  walk, guard matrix incl. no-token/bad-token/absent-Origin/foreign-
  Origin, real upgrade with token+origin receiving `attached`,
  idempotent+concurrent Close, no token without loopback);
  internal/llm/admission_v195_test.go +
  internal/api/admission_v195_test.go (claim lifecycle, live-run
  refusal, refusal 503 with unchanged transcript, claim-not-taken-when-
  live); src/loopback-origin.test.ts (accept/refuse matrix incl.
  lookalikes, schemes, ports, credentials, paths, IP shorthand).

Stage Summary:
- v1.9.5 verified locally: npm ci clean; typecheck green; lint 0/0
  (the CI blocker fixed, no rule disables); test:units 236/236 (incl.
  the new loopback-origin suite); test:release 38/38; release-version
  --check green at 1.9.5 (package.json -> config.go / build/config.yml /
  SIGNATURE); npm run build + verify:web green (tracked web/static
  synchronized AFTER the version bump); go build/vet/test (headless,
  -count=1) green on ./internal/... + ./cmd/...; go test -race green on
  api/llm/agent/sessions/runtime/englease.
- UNVERIFIED here, executed by hosted CI: Windows desktop UI acceptance
  (no Windows host in the authoring environment — the gate is wired,
  runnable and unexecuted), native C++ CTest (no cmake here; no native
  source modified), the full browser E2E with real generation (no
  provisioned engine binary here; no E2E product path changed by
  v1.9.5 beyond the harness).
- Honest boundaries: no push/PR/tag/release; no remote main change; no
  permissions widened; GPU claims still execution-evidence-gated; the
  AUTO probe still records refusals as deferrals, never persisted
  failures.

---
Task ID: 1 (v1.9.4 session)
Agent: v1.9.4 engineering session
Task: v1.9.4 — P0 root cause and repair of the Windows live-answer
defect ("the answer appears only after Stop"), the two zero-session E2E
failures (Actions run 38069168867, e2e/zero-session.spec.ts:86 and :275
at expectRealAssistantReply), the engine-transaction/run TOCTOU (P1),
the publishMs metric clarification (P1), the opt-in sanitized stream
trace (P0-A), and the real-Windows acceptance harness

Work Log:
- Baseline: HEAD `5bcd17b` (v1.9.3). The authoritative Actions run
  38069168867 FAILED (Linux E2E 38/40: zero-session.spec.ts:86 and :275
  at `expectRealAssistantReply`); v1.9.3 is NOT a green release. Local
  reproduction on this session's host was WORSE (3/4 zero-session tests
  failed the same helper) — Agent mode's CI pass was timing luck.
- P0-C REPRODUCTION AND ROOT CAUSE (test defect, not a product defect):
  local Playwright runs failed at `expectRealAssistantReply` with
  "element(s) not found", while the failure ARTIFACTS show the settled
  assistant reply rendered with real content. Proven: v1.9.3's helper
  waits on `.message-row.from-agent .message-content`, but
  `.message-content` exists ONLY on the live surfaces (GenerationBubble,
  StreamingBubble). A SETTLED assistant message renders through
  MessageBubble → `<Markdown>` → `div.message-markdown` — it never
  carries `.message-content` at all. The helper could only ever match
  inside the finalising window (the bubble's last live content), a pure
  timing race; every other suite asserts settled replies via
  `.message-bubble` for exactly this reason (chat.spec.ts:80,
  agent.spec.ts:58, run-control.spec.ts:62, model-first.spec.ts:179,
  composer.spec.ts:64). The helper was retargeted to the settled
  Markdown surface — strength PRESERVED and tightened (non-whitespace
  text asserted on the markdown surface itself; placeholder rows
  excluded belt-and-braces; the generation/streaming bubbles can never
  satisfy it).
- P0-A/B REPRODUCTION AND ROOT CAUSE (the Windows live-answer defect),
  proven from source at the exact pinned versions: Wails
  v3.0.0-beta.16 serves packaged desktop builds through the webview's
  resource-request interception. On Windows, WebView2 forwards ONLY
  `http://wails.localhost` requests into the Go handler
  (webview_window_windows.go: any request whose scheme is not "http" is
  handed to the webview's default handler); on Linux production the
  page rides the custom `wails://` URI scheme
  (linux_cgo.go:1271). WebSocket handshakes are NOT web-resource
  requests (documented WebView2 limitation), and no loopback listener
  exists in the desktop build — so the activity socket derived from
  window.location (`ws://wails.localhost/ws/activity`,
  src/config.ts WS_BASE) NEVER reached the backend: WebView2 resolves
  *.localhost to the loopback address on port 80, where nothing
  listens. The client retried 20 times and gave up silently. Live run
  frames (response/reasoning snapshots, status, done/error, run_snapshot
  replays, idle sentinels) therefore never reached the UI on ANY
  packaged desktop build: no streamed text, no done event, composer
  locked. Stop worked because abort() is HTTP: it flushes the
  accumulator synchronously (store.ts abort path) and schedules
  finalisation, whose history reload recovers the persisted reply over
  plain HTTP — exactly the user's symptom. This also explains why four
  releases of client-side scheduling repairs (v1.8.1 fast path,
  v1.8.2–v1.8.5 flush schedulers, hub conflation) never changed the
  symptom: every one of them is downstream of a socket that never
  connected on Windows. Linux E2E never saw it (real loopback
  listener); the Windows runtime smoke never exercised a chat.
- P0-B FIX (same endpoint, same authority, reachable transport):
  `internal/api/loopback.go` binds the EXISTING API handler on a
  loopback-only ephemeral listener (127.0.0.1:0) for desktop builds;
  `GET /api/ws-origin` publishes the origin over the intercepted HTTP
  surface ("" = stay same-origin — browser/headless behavior
  unchanged); the client resolves it once (config.ts
  ensureActivityWsOrigin, memoized, best-effort, loopback-only values
  honoured) and opens the activity socket against it (store.ts
  connectActivity → openActivitySocket, extracted verbatim). ws.go
  allowedOrigin accepts the Wails page origin (http/wails +
  wails.localhost exactly) against LOOPBACK request hosts only. Local-
  first posture preserved: loopback bind, ephemeral port, origin-
  checked upgrade, one handler, one /ws/activity, no second channel.
  Regression tests: internal/api/loopback_v194_test.go (origin
  discovery, REAL WebSocket upgrade through the loopback with the
  Wails origin receiving the `attached` ack, close resets, origin
  matrix incl. refusals).
- P1 ENGINE/RUN TOCTOU (confirmed from source; the user's Windows log
  showed candidate/rollback messages overlapping a run):
  autoVulkanEligibility() checks anyRunActive() ONCE, then
  AutoProvisionVulkanIfWorthy's transaction (updateEngineVariantTx) can
  run for many minutes (resolve → download → swap → verify → rollback)
  and stops a RUNNING engine with no re-check. Fix: SetRunGate seam
  (internal/llm/llama.go) installed by the API layer (anyRunActive);
  updateEngineVariantTx consults it at the top — a running engine plus
  an active run fails CLOSED with ErrEngineBusyWithRun before anything
  happens. The AUTO probe records the refusal as a DEFERRAL (never a
  persisted bounded failure — a temporary condition must not disable
  the candidate for the hardware identity). Tests:
  internal/llm/rungate_v194_test.go (refusal, state untouched, gate
  consulted once; clear-gate no-op path; nil-gate compatibility), plus
  the existing strict transaction tests still pass.
- P1 METRIC CLARIFICATION: the run summary's `publishMs` measured
  acceptedAt→donePublished (run duration to terminal publication) and
  read like a per-event socket latency. Renamed to
  `acceptToDoneMs`; added `gateMs` (acceptedAt→gateReady), `ttftMs`
  (the orchestrator's measured request_sent→first_token from
  agent.Timing), and aggregate published-event counters
  (`events`, `deltas`, `firstDeltaMs`) on runTimeline. Client-side
  boundaries (queue delay, write time, browser receipt, DOM visibility)
  are NOT derivable server-side; they live in the opt-in trace.
- P0-A DIAGNOSTICS: `src/stream-trace.ts` — opt-in (localStorage
  `SHEYTAN_STREAM_TRACE=1`), test-only, sanitized: monotonic
  performance.now timestamps, ephemeral sequence numbers, canonical
  kinds and LENGTHS ONLY (content asserted never to enter the trace by
  its unit test), bounded 512-entry ring + aggregates, exposed to test
  harnesses via window.__SHEYTAN_STREAM_TRACE_DUMP__ ONLY when enabled.
  Wired into every client boundary (ws receive/attach/snapshot, fold,
  flush, run start/end) and the GenerationBubble DOM commit
  (MessageStream). The zero-session spec attaches the dump on failure
  (testInfo.attach). Unit tests: src/stream-trace.test.ts (6).
  Server-side aggregates (events/deltas/firstDeltaMs) ride the ordinary
  run summary — counts only, no identifiers beyond the existing
  run-summary contract.
- WINDOWS ACCEPTANCE HARNESS: scripts/windows-live-answer-acceptance.mjs
  launches the real desktop exe with WebView2 CDP enabled (TEST-ONLY
  switch), asserts the exact release contract: ready surface → send →
  real non-placeholder text WHILE Stop is visible → strictly longer DOM
  text while Stop stays up → exactly one persisted answer → reload
  persists one user + one assistant reply → clean close; dumps the
  sanitized trace to the artifact dir on failure.
- VERSION: 1.9.4 through the canonical flow (package.json →
  release-version.mjs repaired config.go / build/config.yml / SIGNATURE;
  --check green).

Stage Summary:
- The Windows "answer only after Stop" defect is root-caused to an
  unreachable activity WebSocket in packaged desktop builds (Wails v3
  beta.16 interception is HTTP-only) and repaired with a loopback
  transport for the SAME API/WebSocket authority; proven server-side by
  a real WebSocket upgrade + attach ack through the loopback with the
  Wails page origin (Go test). The end-to-end Windows WebView2 proof
  REMAINS UNVERIFIED on this Linux session (no Windows host, no
  webview); the harness ships for a real Windows run and the native
  evidence is labelled unverified everywhere.
- Both zero-session E2E failures are root-caused to the v1.9.3 helper
  selector (settled rows have no .message-content); the product was
  rendering real replies all along. Fixed with strength preserved.
- Local verification this session: unit tests 226/226 (incl. the new
  stream-trace suite) and release tests 38/38; typecheck/lint clean;
  go build + vet -tags headless clean; go test ./internal/... -tags
  headless 62 pkgs ok, 0 FAIL; race gate green on api/agent/sessions/
  contextplan/histref/runtime; native cmake+ctest 13/13; browser E2E
  40/40 (both previously-failing zero-session tests green with the
  strengthened assertions). Env-bound limitation: the non-headless
  Wails desktop packages do not compile in this sandbox (GTK4/WebKitGTK
  headers absent — the documented v1.9.3 limitation, unchanged).


---
Task ID: 1 (v1.8.3 session — condensed to compact form per the operator's
v1.10.10 docs order; the full session narrative lived in the v1.8.3 cycle)
Agent: v1.8.3 engineering session
Task: v1.8.3 — P0 root-cause and repair of the Linux CI session-delete
failure (Actions run 36713108772 / job 109880449134,
e2e/sessions.spec.ts:88 "Expected: < 4, Received: 4")

Summary (compact):
- Baseline HEAD `c8867886` (v1.8.2); CI run 36713108772 Browser E2E 30/31,
  only the session-delete test failing; the v1.8.2 diff touched no session
  path — a latent timing race, not a regression.
- Root cause (instrumented reproduction: request/response journal +
  server-side authoritative list snapshots + DOM identity capture): the
  test sampled its count baseline BEFORE the asynchronous create landed
  in the sidebar; the delete removed the correct session (verified absent
  server-side); the late create kept the count flat; final DOM == final
  server state. The product deleted the correct session; the MEASUREMENT
  raced the app.
- Real defects found and repaired behind the investigation: the startup
  GET /api/sessions write bypassed the ONE sessionListGuard (a create
  landing during init was clobbered by the stale startup list — the
  created session vanished from the sidebar); `void deleteSession(id)`
  turned server DELETE failures into invisible unhandled rejections; the
  unconditional create prepend could duplicate a row.
- Test repaired state-based, never weakened (no timeout increased): wait
  for the created session to appear (count grows by one), then assert the
  SPECIFIC deleted id via data-session-id, survivor identity-set equality,
  count, replacement activation, composer usability, and post-reload
  absence from the sidebar AND a fresh authoritative fetch.
- Deterministic three-layer coverage: src/session-delete-regression.test.ts
  (11 scenarios over the REAL store, causality-forced ordering),
  internal/sessions/delete_pending_test.go, browser E2E (the init-race
  test holds the startup GET response until the create POST's response has
  reached the page). Mutation-verified: with the init guard neutralized,
  both the store test and the browser E2E fail.
- ROADMAP.md repaired (artifact truth + the evidence-ranked Performance,
  Reliability & Scale Backlog); version truth 1.8.3 through the canonical
  gate.

---

Task ID: 1 (v1.8.1 session — condensed to compact form per the operator's
v1.10.10 docs order; the full session narrative lived in the v1.8.1 cycle)
Agent: v1.8.1 engineering session
Task: v1.8.1 — P0 real-Windows local-generation crash repair (small Gemma
model + `hi`), P0 single-frame streamed-answer visibility, P1 engine
rollback identity repair, audits, version truth

Summary (compact):
- Baseline HEAD `e5a12c0` (v1.8.0), CI run 36570586149 green. The primary
  evidence: the user's Windows runtime log — local CPU backend,
  gemma-4-E2B-it-Q4_K_M.gguf, llama.cpp b11261, preflight compatible,
  then a recovered nil-pointer panic between `task classified` and
  `tier selected`, run settled error in ~57 ms.
- Root cause reproduced deterministically BEFORE any fix: Gemma-class
  GGUFs carry ~262K-entry tokenizer blocks exceeding the model-card
  parser's 8 MiB read bound → ReadModelCard fails → the capability card
  resolves to nil → resolveEffectiveContext dereferences it.
- Repairs: the documented nil-card fallback in resolveEffectiveContext
  (configured context + conservative estimator); the GGUF metadata read
  bound raised 8 → 32 MiB so real Gemma-class cards parse again;
  ResolveModelCapabilities made uniformly nil-config-safe (the
  recommendation block had the same deref class); gemma3n/gemma4 lineage
  entries. Regressions proven to FAIL pre-fix, including the api
  package's FIRST local-provider run-level E2E (the test binary
  re-executes as a fake llama-server subprocess serving the real engine
  contract).
- P0 streaming: src/stream-fast-path.ts folds stream-critical events into
  the streaming accumulator at socket-receive time — ONE frame boundary;
  12 deterministic frame-controller tests. (The flush-boundary dependency
  on requestAnimationFrame this left behind was root-caused and closed in
  v1.8.2/v1.8.4.)
- P1 engine identity: Rollback() re-records from the restored manifest (a
  FAILED verification left the transient bundled-default tag stamped in
  installed.json while the restored manifest described the serving
  engine); TestRollbackRestoresRecordedIdentityFromManifest fails pre-fix
  at exactly the transient tag.
- Audited with no change: the AppData runtime-root migration contract
  (detected=1 merged=1 recovered=0 collisions=4 removed=1 is the intended
  deterministic newer-wins contract); GPU/AUTO CPU with numGPU=0 as the
  honest evidence-ladder outcome. Version truth 1.8.1 through the
  canonical gate.

---

Task ID: 1 (v1.8.0 session — condensed to compact form per the operator's
v1.10.10 docs order; the full session narrative lived in the v1.8.0 cycle)
Agent: v1.8.0 engineering session
Task: v1.8.0 — P0 pause/resume synchronization repair, honest abort
marker, Runtime Governor vertical slice (Adaptive Runtime Intelligence),
documentation consolidation, version truth

Summary (compact):
- Baseline HEAD `eab6e1b` (v1.7.6); failing Actions run 36553559366
  (Windows Go verification: TestPauseResumePauseAgainThenResumeCompletes
  — "run settled done while waiting for phase paused"). Root cause: the
  test reused waitForRunResponse after a resume — its condition was
  satisfied by the STALE pre-resume cumulative snapshot, so the second
  pause was issued without proof the resumed generation was live
  (scheduling-dependent on Linux, deterministic on the Windows runner).
- P0 repair: resumedGenerationEvidence (strictly newer run sequence AND
  changed cumulative response/reasoning snapshot) + waitForResumedGeneration
  (fail-fast on settlement); deterministic unit + integration regression
  coverage.
- P0 latent defect found during the audit (~1-in-6 flaky at BASELINE):
  the orchestrator's four context-cancelation exits published
  Activity{Type: "done"} while the caller settled "aborted" — a
  registry/state divergence the one-settlement rule can never repair.
  Honest "aborted" activity type end-to-end (orchestrator, observe
  folding, captureTerminal, frontend typed kind); 15/15 stressed
  abort-after-resume runs.
- Runtime Governor vertical slice (internal/governor): ONE runtime policy
  authority fed by the EXISTING live monitor (same sampler, same cadence,
  same protection path). ResourceState (measured facts + explicit
  unknowns), sustained-duration + bounded rolling pressure signals,
  Envelope with adjustment classes (live/next-run/reload),
  AdmitHeavyweight/AdmitMemory resident-budget admission (mapped size ≠
  resident; unknown → conservative refusal), provenance-carrying
  SelfModel. Policy only — no second sampler/scheduler/authority;
  executes nothing. Wired via Stack.StartGovernor on the existing poll
  path; GET /api/governor + the System Centre GovernorCard.
- Docs consolidated (README current + compact history; ROADMAP
  capability-driven with v1.9+ explicitly FUTURE); version identity 1.8.0
  via the canonical gate. Physical-PC Windows runtime verification
  honestly NOT executed in that session.

---
Task ID: 1 (pre-v1.8.0 record — summarized once)
Agent: v1.3.0 → v1.7.6 engineering sessions
Task: everything shipped before v1.8.0

Summary (compressed; the authoritative per-release detail lives in the
release tags, their tests, and the historical sections of UPDATE.md):
- Foundations (≤v1.2): sessions, streaming with sequence-stamped replay,
  tool registry, downloader, installer, health checks, environment
  centre, bounded authoritative run state, settlement edge.
- v1.3–v1.4: durable completion ordering (persist → summary → handoff →
  recall → rollover → terminal), idle-sentinel barrier, task state,
  cross-mode history, continuum rollover, repository index, custom
  tools, update rollback safety.
- v1.5–v1.6: multi-agent context, memory/recall tiers, Lab workspaces,
  engine-variant provisioning through the authoritative resolver,
  Vulkan asset validation on the Windows runner, clone + workspace
  surfaces, stable embedded-frontend asset contract.
- v1.7.0–v1.7.2: reliability passes (engine lifecycle, hub replay,
  stress surfaces), scheduler settlement contract, tool-registry
  snapshot isolation (the one-word-chat crash repair).
- v1.7.1: preflight gate (refuse before engine start), LiveMonitor with
  hysteresis + cooperative critical protection, context-exhaustion
  recovery (typed conditions, one bounded restart), native engine
  first-class backend, LICENSE.md consolidation.
- v1.7.4–v1.7.6: pause/edit/resume with durable checkpoints; transactional
  edits + journals + ten-window fault-injection recovery; restart
  recovery claiming the checkpoint revision; token-aware codename gate;
  generation-aware spec cache; the llama.cpp b10642/b11205 CLI contract
  verified against real `--help` fixtures.

---
Task ID: 1 (v1.8.2 session)
Agent: v1.8.2 engineering session
Task: v1.8.2 — P0 live-visibility repair (Stop-to-see bug), live
reasoning/memory/capability surfaces, P1 log redaction, identity-based
model-caps cache, README→changelog split, engine-identity log clarity,
version truth

Work Log:
- Baseline: upstream `main @ 9d481be3` (`v1.8.1`). Provisioned the
  sandbox toolchain (Go 1.27.1, Node 24, native build via plain make);
  native engine + frontend + headless server all built from THIS tree.
- REPRODUCTION (real stack): `e2e/repro-stream.mjs` drives Chromium
  against the real server + embedded frontend + native-engine fixture,
  tracing every WebSocket frame with timestamps and sampling the live
  generation bubble's DOM at 100 ms. Measured: `response` frames reach
  the browser; the fast path folds them; the rAF flush paints live text.
  The v1.8.1 pipeline is intact in a healthy renderer — every client
  failure class except the flush boundary was eliminated with evidence
  (frame trace, store-site audit, component/CSS audit, Go local-chat
  E2E for the llama path).
- ROOT CAUSE (P0): the streaming flush was armed EXCLUSIVELY on
  `requestAnimationFrame`. In the user's WebView2 runtime compositor
  frame callbacks can be throttled/suspended while the JS event loop,
  the WebSocket and React keep running — phase label + elapsed clock
  update, streamed text never appears, and Stop (synchronous flush)
  reveals everything at once. Repair: `src/stream-flush-scheduler.ts` —
  the flush is armed on an event-loop task (MessageChannel, React's own
  scheduling primitive) AND an animation frame; first to run flushes,
  the other no-ops; ONE coalescing latch; visibility never requires
  frame callbacks. Evidence: 8 deterministic scheduler tests + the
  browser harness.
- P0 live surface: factual no-reasoning state ("Thinking · this model is
  not exposing a reasoning stream" — only while thinking/preparing and
  no reasoning arrived; never fabricated); backend-truth memory line.
- P0 memory evidence: `contextplan.MemoryEvidence` composed at the
  injection site from survival-reconciled facts (summary injected +
  measured tokens; recalled exchanges actually carried; history refs;
  recall attempted) riding the EXISTING `context` activity; rendered
  verbatim (`Memory: session summary · 2 recalled exchanges` / `… · no
  recall matches`; NOTHING before the report arrives). Selection policy
  unchanged (FAST stays cheap; recall on intent; rolling summary
  automatic). Evidence: 2 Go tests + 9 frontend unit tests.
- P0 capability self-model: `taskclassify.SelfDescribe` deterministic
  intent signal (28 phrases, negative-space tested) +
  `internal/agent/selfmodel.go` — ONE bounded formatter over the
  EXISTING authorities: registry snapshot (ShortDescription-first,
  first-sentence fallback), the already-resolved model card (one read
  per run — `resolveEffectiveContextCaps`), config-backed backend
  facts, sysinfo fast snapshot, memory plan. Registered/enabled/
  offered/disabled distinctions; capability questions stay cheap (no
  research/recall/repo; one engine turn). Evidence: intent tests,
  inject/no-inject orchestrator tests, builder unit test.
- P1 log redaction: `internal/logging/redact.go` removes `runId=` /
  `runID=` / `session=` / `sessionId=` / `sessionID=` tokens at the ONE
  central sink (log formatter + crash-report panic text); token-start
  boundaries, empty-paren cleanup, idempotence; prose/URLs/durations/
  causes survive; internal identity untouched. Evidence: 18-case unit
  family + diagnostic-preservation test.
- P1 caps cache: `internal/llm/modelcaps.go` dropped the 10 s TTL for
  identity-based caching — immutable card under (path, size, mtime)
  with no expiry; config-sensitive fields re-derive from the cached
  card on a config-fingerprint change; bounded map, oldest-evicted.
  Evidence: 4-test family (no-reparse beyond TTL window, re-parse on
  identity change, re-derive on config drift, fingerprint coverage).
- P1 README/changelog: README rewritten current-only (no release
  history); `changelog.md` created as the ONE canonical history
  artifact (v1.8.2 detail + v1.8.1/v1.8.0 + compressed pre-history);
  UPDATE.md top section = v1.8.2 notes with v1.8.1 compressed to
  history; ARCHITECTURE.md + agent.md updated to v1.8.2 truth.
- P1 engine identity logs: boot probe reads `engine boot probe: binary
  build <tag> …`; deferred commit names the active build + next-boot
  probe (`staged → boot probe → committed` unambiguous).
- P1 turn latency: measured the class during the caps-cache work — the
  identity-based cache removes the per-turn GGUF metadata re-parse
  (32 MiB reads on Gemma-class cards) that the 10 s TTL always
  expired into. No benchmark numbers published (not measured on
  physical Windows here).
- E2E: `e2e/live-stream.spec.ts` (2 tests) + fixture support
  (`e2e/make-e2e-live-model.py` — a fixture-level unreachable-EOS
  model so the live window is deterministic; `startSheytan` gained
  model/generator/maxTokens options). Both tests PROVE live visibility
  while the run is live (Stop button up), text growth without user
  action, one final message, no duplicates, and an honest mid-run
  Stop.
- Version truth 1.8.2 via the canonical gate (`release-version.mjs
  --check` green: package.json, package-lock, config.go,
  build/config.yml, SIGNATURE); codename gate green.

Verification (evidence classes; Linux x86-64, Go 1.27.1, Node 24):
- go vet -tags headless ./internal/... green; `go test ./internal/...
  -tags headless -count=1` green (zero FAIL).
- Race: api/agent/llm/governor `-race` green (api re-run in isolation
  green; one earlier FAIL was CPU contention with the concurrent
  browser suite, not a race).
- Frontend: typecheck, oxlint 0 warnings, `npm run test:units` 180
  pass, production build + embedded sync green.
- Native: `make -C native/engine test` — all engine test binaries
  pass.
- Browser E2E: FULL suite 31/31 green (29 existing + 2 new
  live-visibility), real stack, no skipped-failure.
- NOT executed here (stated honestly): physical-Windows runtime
  acceptance; the WebView2 frame-throttling behavior was eliminated
  by construction (task-boundary flush) and pinned by deterministic
  tests, not by a physical-host observation.

Stage Summary:
- v1.8.2 complete on this revision: the Stop-to-see bug is repaired at
  the scheduling boundary, the live surface shows truthful reasoning
  and memory state, the model can describe its real tools and
  capabilities, human-facing logs are free of opaque identity tokens,
  repeated model-card work is gone, README is current-only with the
  history in `changelog.md`, and version identity is exactly 1.8.2.

---
Task ID: v1.8.4 session
Agent: v1.8.4 engineering session
Task: v1.8.4 — P0 root-cause and repair of four defect families from the
real Windows runtime evidence: (A) live response/thinking invisible
during generation until Stop; (B) the AUTO Vulkan candidate blocked by a
stale derived GPU posture + the b11273/b11310 boot-probe identity
contradiction; (C) zero-session Send dead end; (D) stale context-usage
races. Documentation to current state; full verification funnel; the
v1.8.4 final source ZIP.

Work Log:
- Baseline verified first: HEAD `ca44b08008f26df467d4bd9c7b70cc49729e1ffb`
  (v1.8.3) on `main` — matches the stated baseline exactly.
- P0-A root cause (from the code, deterministic): the v1.8.2
  `MessageChannelTaskController` was ONE-SHOT (port handler nulled the
  channel after the first delivery; a post while a message was in flight
  chained onto the armed callback WITHOUT reposting). One lost/delayed
  MessageChannel message wedged the controller and the flush latch
  forever — streamed text accumulated until Stop's synchronous flush,
  while the fast path's synchronous `transitionPhase` renders kept the
  phase label moving. Exactly the reported split. Also found the
  ACTIVITY flush was rAF-only (statuses/tools/done/error) — the same
  boundary-loss class for run settlement.
  Repair: `src/stream-flush-scheduler.ts` — THREE independent boundaries
  (reusable MessageChannel macrotask + 0ms timer task + animation frame)
  behind ONE coalescing latch; recoverable latest-wins task controller
  with an in-flight handshake and a deterministic microtask fallback; the
  activity flush moved onto the same scheduler class.
- P0-B root causes: (i) `internal/recommendation` wrote
  `gpuAutoOffload=false + numGpu=0` as a derived posture ("CPU-only
  until a Vulkan engine build is provisioned"); the AUTO probe gate then
  read it as an explicit OFF — the exact circularity in the runtime log.
  Repair: the recommendation never writes a derived OFF; a new
  `gpuAutoOffloadUserSet` field marks explicit user actions (toggle +
  config patch); `config.Load` repairs the legacy derived state ONCE,
  honestly noted and persisted; explicit OFF / manual layers / CPU
  profile are never touched. (ii) the deferred-commit verification
  window keyed boot identity on the RECORDED tag (still the previous
  build until Commit) — the "b11310 staged → probe reports b11273"
  contradiction. Repair: the installer writes a window-scoped
  `engine-stage-pending.json` marker at swap time; `detectCapsForBoot`
  probes/caches/reports the ACTUAL staged binary while it exists;
  Commit/Rollback clear it.
- P0-C root cause: `run()`'s lazy session creation was unreachable — the
  composer textarea and Send button were hard-disabled on
  `!activeSessionId`. Repair: composer enabled on zero-session spaces
  (model + live-run gates unchanged), truthful placeholder/footer.
- P0-D root cause: `refreshSessionContext` guarded only the session id —
  same-session out-of-order responses overwrote newer state; deletion
  never cleared/refreshed the context. Repair: monotonic request
  generation; invalidation on session switch/deletion/creation/mode
  switch/fresh runs; deletion now clears + refreshes a replacement.
- Funnel-surfaced defect (P1): `config.Save` used the FIXED
  `config.json.tmp` name — two concurrent writers rename each other's
  temp away (observed in this repo's own E2E run as a hard engine-start
  failure: "persist default engine path: rename …: no such file"). Every
  config write now uses a unique same-directory temp file.
- Tests added/updated: `src/stream-flush-scheduler.test.ts` (13 —
  including the deterministic lost-message wedge the v1.8.2 design
  fails), `src/context-refresh-race.test.ts` (7), 
  `src/zero-session-send.test.ts` (5), `e2e/zero-session.spec.ts` (4),
  the suspended-rAF live-stream E2E (1),
  `internal/updater/staged_identity_v184_test.go` (3),
  `internal/config/gpu_posture_v184_test.go` (5), recommendation
  contract updates. New store-level suites run the REAL store via the
  extensionless resolver (same harness discipline as v1.8.3).
- Docs: README current-only to v1.8.4 (triple-boundary flush, new
  verified capabilities); UPDATE.md rewritten as v1.8.4 current notes
  with compressed history; changelog.md v1.8.4 entry (factual,
  evidence-cited); ARCHITECTURE.md current for v1.8.4 (flush, zero-
  session, context generation, staged identity, GPU posture contract);
  agent.md handoff updated; ROADMAP.md only records what actually
  shipped with its evidence classes.

Verification (what actually ran here, Linux x64):
- `go build ./...` and the headless build pass; `go vet`-equivalent via
  test compilation clean.
- Go: full `go test ./internal/... -tags headless -count=1` PASS;
  race gate PASS (api, agent, sessions, contextplan, histref, runtime,
  config, updater, recommendation; one load-sensitive governor flake
  observed once under heavy parallel load, not reproducible in 3
  targeted race runs — unrelated to this change set).
- Native C++: `make -C native/engine test` all checks passed.
- Frontend: `tsc --noEmit` clean; `oxlint` clean; `npm run test:units`
  209/209; `npm run test:release` 38/38; release-version gate consistent
  at 1.8.4.
- Browser E2E (real stack: headless server + embedded frontend + native
  engine + Chromium): FULL suite 37/37 green (32 existing + 5 new).
- NOT executed here (stated honestly): GitHub Actions Windows x64
  packaging, physical-Windows/WebView2 runtime acceptance, real-GPU
  Vulkan execution evidence — the repairs are proven deterministically
  and on healthy Chromium; the physical claims remain the user's own
  acceptance step.

Stage Summary:
- v1.8.4 complete on this revision: the streaming flush cannot wedge
  (three boundaries, recoverable controller), the activity lifecycle is
  not rAF-hostage, zero-session Send works end-to-end, stale context
  responses can never land, the AUTO GPU posture is honest and
  provenance-marked, the engine-update window reports the binary that is
  actually on disk, and config writes are race-free. Version identity is
  exactly 1.8.4; no new authority was introduced anywhere.

---
Task ID: 1 (v1.8.5 PHASE 1 session)
Agent: v1.8.5 Phase 1 engineering session
Task: SHEYTAN-LA v1.8.5 Phase 1 — P0 live-streaming server-side repair, P0 reasoning-depth ladder (Low/Mid/High/Ultra) with real backend budgets, P0 Show/Hide Thinking, P1 engine Start/Stop removal, Phase 2 execution/evidence foundation, docs reconciliation, repository ZIP

Work Log:
- Baseline: HEAD `ef2e15f` (v1.8.4), clean tree. Full inspection of the streaming path end-to-end: orchestrator emitProgress (~8ms SmoothStream cadence) → publish wrapper (seq stamp + runLive.observe) → activityHub → WS writer → frontend fast-path fold → triple-boundary flush → render. Frontend v1.8.4 scheduler verified robust (self-healing MessageChannel controller, three boundaries).
- ROOT CAUSE FOUND (the remaining P0 bottleneck): the activityHub's subscriber delivery was a plain 128-deep channel whose overflow policy DROPPED THE NEWEST event ("a slow WebSocket must never block the agent run"). For cumulative response/reasoning snapshots that discards the frame carrying the FULL text while the buffer keeps stale prefixes — under transport backpressure (the same runtime class that lost MessageChannel deliveries) visible text freezes at a stale prefix until the run ends (Stop included), then the reconnect run_snapshot replay reveals everything at once. The server-side twin of the v1.8.4 wedge.
- P0 FIX: bounded, conflation-aware subscriber queues (activitySub). Overflow evicts the OLDEST conflatable event (response/assistant_delta, reasoning/thinking_delta, status — newest subsumes older); order + seq replay contract preserved; terminal events never preferentially evicted; publisher never blocks; memory bounded; per-event write deadline (30s) tears a wedged client down deterministically (reconnect + snapshot replay recover). 9 deterministic regression suites (drop-newest reproduction, terminal survival, publish-never-blocks with 20k offers to a wedged reader, close-drains-first, concurrent publishers, eviction policy); race gate green; full API suite green.
- P0 REASONING LADDER: replaced Auto/Fast/Thinking with Low/Mid/High/Ultra. Verified the llama.cpp request-level `reasoning_budget_tokens` parameter exists in BOTH managed builds by fetching and reading the ACTUAL b10642 + b11205 server sources (server-common.cpp: the OAI chat endpoint maps it into the reasoning budget sampler, applied when the chat template exposes a thinking section; the CLI fixtures independently document --reasoning-budget). Budgets: low=0 (thinking off), mid=1024 (default), high=4096, ultra=not sent (engine default — honest encoding). Implementation: ThinkingControl ladder + legacy migration at BOTH boundaries (Go NormalizeThinkingControl, TS normalizeThinkingControl); ChatRequest.ReasoningBudget (*int — explicit 0 stays on the wire; found AND fixed that the custom MarshalJSON needed the field added — caught by the wire-field-name test); orchestrator applyReasoningBudget gated !IsRemote; tier posture (low=latency floor, mid=neutral, high/ultra=nudge without context inflation); the level rides EVERY run request. 9 Go suites + tier-posture pin + 4 store-level scripted-transport suites.
- P0 SHOW/HIDE THINKING: persisted visibility-only preference (showThinking) gating the rendering of backend-reported reasoning on the live bubble, safety-net bubble and history messages; never in the payload (pinned), never touches level/budget; the store keeps folding reasoning snapshots while hidden; Chat/Agent share the semantics.
- P1 ENGINE CONTROLS: removed the user-facing Start/Stop engine toggle from AgentBody (the supervised-machinery decision); internal lifecycle (run gate EnsureLLMContext, settings restart flow, recovery, shutdown) untouched; download cancellation kept.
- PHASE 2 FOUNDATION: internal/llm/execution.go — the ONE shared execution/evidence ladder (detected → backend-available → device-selected → model-loaded → generation-executed → execution-evidence → verified) as a PURE composer over existing authorities (device presence, backend health, accelerator selection memo, verified model, measured perf ring, runtime offload line); surfaced as /api/engine's execution block; a read-through memo of the accelerator resolution (written by the ONE selection authority on /api/perf) — no second sampler/policy engine. 5 ladder suites pin detected ≠ verified, monotonicity, unknowns, wire contract.
- Docs: README (current product, v1.8.5 surfaces + honest limitations), ROADMAP (explicit Phase 1 COMPLETE / Phase 2 NEXT / Phase 3+ sections), ARCHITECTURE (subscriber delivery, execution ladder, reasoning budgets), UPDATE (v1.8.5 release notes), changelog (v1.8.5 entry), agent.md (v1.8.5 handoff + Phase 2 entry notes).
- Verification: go build headless OK; go vet changed packages OK; full go test for api/agent/llm/taskclassify OK; -race for the same four OK; frontend typecheck OK; 213/213 unit tests (incl. the 4 new reasoning suites); npm run build + sync:web + verify:web OK; release-version gate OK at 1.8.5; browser E2E on the REAL stack: live-stream 3/3 (visible-before-completion, Stop-settles-honestly, suspended-rAF), chat 5/5, composer 4/4 (incl. the thinking-control persistence/travel test). KNOWN FLAKE observed in THIS environment: zero-session.spec Chat-mode visible-before-completion fails ~50% of runs at the ROUTER level (POST /api/run answered 404 before any run/hub involvement — orthogonal to this diff; the Agent-mode zero-session test and all other suites pass deterministically; the same flake appeared across runs in different test positions). No test was weakened or skipped.
- Packaging: download/SHEYTAN-Local-Agent-v1.8.5-PHASE1-FINAL.zip (complete repository state, excluding .git/node_modules/caches/build artifacts).

Stage Summary:
- v1.8.5 Phase 1 COMPLETE: the live-streaming failure class is now closed on BOTH sides of the wire (v1.8.4 render side + v1.8.5 server side); the reasoning surface has real, verified backend meaning; thinking visibility is a first-class control; the engine left the user's critical path; Phase 2 starts from an explicit execution/evidence contract and a precise roadmap.
- Phase 2 entry: ROADMAP.md §Phase 2 + agent.md "Phase 2 entry notes".

---
Task ID: 1 (v1.8.8 session)
Agent: v1.8.8 engineering session
Task: v1.8.8 — audit-job repair, release-metadata synchronization, dependency
security, dataAnalysis hardening, E2E zero-session reload-persistence repair;
clean-room build and package of SHEYTAN-Local-Agent-v1.8.8-FINAL.zip

Work Log:
- Baseline: HEAD `e9a8448` (v1.8.7). Reproduced the CI run 37446279646
  audit-job failure exactly: `codename-gate.mjs` flagged 4 tracked lines in
  `internal/tools/data_tool_test.go` (650/658/666/672) — the v1.8.7
  TestJSONColumnOrderIsDeterministic fixtures used the retired codename as a
  JSON key. Renamed the fixture key to a codename-free alternative that
  preserves the ordering semantics (document order ≠ alphabetical order);
  the gate stays strict and the regression stays real (no exemption added).
- Release metadata: package.json 1.8.8; release-version.mjs repaired
  config.go / build/config.yml / SIGNATURE; package-lock.json root metadata
  synchronized (stale at 1.8.5 since v1.8.6); README current-release marker,
  ROADMAP CURRENT RELEASE section, UPDATE.md release notes, changelog §v1.8.8.
- npm audit: the high-severity advisory (GHSA-68fv-2mgg-jv7q,
  source-map-js <1.2.2 via vite→postcss) repaired with the semver-compatible
  lockfile-only upgrade to 1.2.2; direct dependency contract untouched;
  `npm audit` now reports 0 vulnerabilities.
- dataAnalysis hardening (the ONE data authority — no new subsystem):
  (a) export with `limit` mutated the LRU-cached dataset (cache poisoning)
  — the limited view is now a separate dataset; (b) delimiter sniff counted
  delimiters inside quoted fields — now quote-aware (countUnquoted);
  (c) analyze correlations ordering relied on sort internals for equal |r| —
  explicit (a,b) tie-breaker with SliceStable; (d) a requested correlations
  section with <2 numeric columns silently vanished — now answers honestly;
  (e) the 256 MB error text implied chunked processing — message corrected
  to the honest in-process bound; (f) join's right-index build honors
  cancellation; (g) the sample-mode PRNG global state is mutex-guarded.
  New `data_tool_hardening_test.go` pins RFC-4180 edges, splitLinesAny
  parity, delimiter sniff, numeric semantics, stats/outlier edges, aggregate
  degenerate groups, join many-to-many/empty, quality ±Inf, export cache
  containment/determinism, JSON later-keys/duplicate-keys, correlation
  tie-break.
- E2E (real stack, real engine): the zero-session reload-persistence spec
  failed deterministically. Trace + network HAR + WS frame logs root-caused
  it: run() kept `running:false` across session creation and the bounded
  attach wait, so the composer read idle while a Send was mid-startup; a
  reload in that window killed the pipeline before POST /api/run and the
  message never persisted (HAR: POST /api/sessions ok, no POST /api/run,
  messages total:0). Repair: the run-startup state (running/preparing) is
  set BEFORE the transport work in run(). Spec green in isolation (2.2s) and
  in the full suite.
- Verification evidence: frontend typecheck/lint/213 units/38 release green;
  codename gate green; release-version --check green; go test
  ./internal/... -tags headless -count=1 green; -race audit green on
  api/agent/sessions/contextplan/histref/runtime/tools/engdiscovery; native
  engine cmake clean configure + 12/12 ctest; 15 TestRealCppHost* Go
  integration tests RUN (not skipped) and pass; Playwright full suite
  36-37/38 per run, remaining failure class = live-window sampling
  sensitivity (proven pre-existing via stash experiment: live-stream:87
  fails identically on unmodified v1.8.7 code in this sandbox; the
  deterministic streaming siblings — Stop + rAF-suspended growth — pass).
- Sandbox gaps recorded honestly (no root): `go test ./...` and `go vet`
  fail only on the Wails cgo desktop packages (gtk4/webkitgtk-6.0 headers
  absent; CI installs them); Playwright --with-deps unusable (sudo) but
  chromium runs; all other gates executed for real.

Stage Summary:
- Every v1.8.8 surface reads 1.8.8 through the ONE release authority.
- The audit job's blocker is repaired at the root; platform jobs unblocked.
- The one dataAnalysis authority is hardened with regression coverage.
- The zero-session reload-persistence defect is fixed in the product, not
  the test.
- Deliverable: SHEYTAN-Local-Agent-v1.8.8-FINAL.zip (728 files, ZIP audit:
  opens, root present, required sources present, no junk, version 1.8.8,
  no stale identity, no codename leakage).

---

Task ID: v1.9.0-release
Agent: release engineering session (2026-10-08)
Task: v1.8.8 -> v1.9.0 — the AI System Builder + long-horizon agentic
engineering layer; root-cause repair of the v1.8.8 Linux zero-session
reload-persistence E2E blocker; version/doc restructure; packaging.

Work Log:
- P0 root cause (Actions 37654274420, e2e/zero-session.spec.ts reload
  test): the zero-session lazy `createSession()` POST ran while the store
  still read `running:false` — the v1.8.8 repair moved the startup state
  before the attach wait but left this ONE async window; the E2E
  "composer enabled" probe legitimately sampled INSIDE it and reloaded
  before POST /api/run (session exists server-side pending; transcript
  total:0 forever). Product fix in `src/store.ts`: startup state set
  BEFORE all transport work; lazy create carries `keepRunState` (it
  previously reset running/runPhase mid-startup); failure paths clean up
  deterministically; attachment snapshot before transport. E2E waits for
  the deterministic run-dispatch marker (optimistic user bubble) + the
  assistant row, then reloads and asserts the DURABLE transcript.
  Deterministic store regressions: zero-session-send.test.ts #6/#7 (+#4
  cleanup assertions).
- AI System authority: internal/aisystem (durable per-document store,
  atomic writes, bounded, deterministic, corruption-tolerant, reserved
  default system, non-destructive migration, revision monotonicity,
  clone/export/import, active pointer with repair). HTTP /api/systems.
  Execution binding frozen per run (systemId + systemRevision):
  instructions block, model override, reasoning preference (existing
  v1.8.5 ladder; explicit request wins), tool-surface constrain
  (ToolPolicy.AISystemConstrain — offer AND execution, remove-only),
  skills filtering, approval policy. ai_system activity event + run
  response identity.
- Goal engine: internal/goal (phases, bounded plan, per-step +
  goal-level evidence journal, checkpoints after planning/each step/
  approval/terminal, bounded replanning that keeps failure evidence,
  turn-budget parking, boot recovery marking found-live goals paused —
  idempotent, terminal stays terminal). Drive executor = ONE orchestrator
  with the frozen AI System snapshot. HTTP /api/goals (+start/pause/
  resume/cancel/approve/reject). Goals UI card.
- Approval authority: internal/approval (5 deterministic risk classes,
  policy vocabulary, exact normalized call identity + bounded ledger),
  per-run agent.WithApprovalGate seam installed ONLY by goal runs
  (deny-by-default; chat/agent behavior byte-identical), durable
  goal-level approval parking with exact-call resume + evidence on
  rejection + stale-id rejection.
- Bounded delegation: internal/multiagent/subtasks (read-only
  parallelism <=2, mutating serialized, no nested spawning, fan-out <=8,
  honest deadline blocks, deterministic subtaskId-order merge, failures
  stay failed). Race-tested.
- Repository navigation: internal/repoindex/navigation.go (`repo_nav`:
  open/navigate/read/grep; bounded ranges, provenance, honest truncation;
  same path-safety authority as repo_search). Registered in the runtime.
- Frontend: System Centre hosts the AI System selector/editor (create/
  edit/activate/clone/export/import/delete) and the Goals card (create+
  start, honest state chips, plan progress from actual plan state,
  pause/resume/cancel/approve/reject). Pure view-model unit-tested
  (ai-systems-view.test.ts). New E2E: e2e/ai-systems.spec.ts.
- Live-visibility fixture: e2e/make-e2e-live-model.py dims re-tuned
  (emb=144, layers=6, heads=6, kv=3, head_dim=24, ffn=384) after
  measured evidence: the previous dims generated the full 160-token
  reply in ~6 ms on a fast host — first response frame and done 6 ms
  apart — so the live surface legitimately never painted between two
  renderer frames (pre-existing sensitivity recorded for v1.8.7/v1.8.8
  sandboxes above); the first re-tune (emb=256/layers=10) overshot into
  a >30 s generation (observation bound expired before the first token).
  The shipped dims stream 22 response frames over a ~2 s window on this
  host: real generation, deterministic visibility on any host speed.
- Verification: go test ./internal/... -tags headless = ALL PASS; go vet
  (headless) clean; race detector green on goal/multiagent/aisystem/
  approval; npm typecheck/lint/220 unit/38 release tests green; full
  Playwright suite 40/40 (including zero-session reload and the new
  ai-systems flows) against the real headless server + real native
  engine + fixture GGUF. Sandbox gaps (unchanged, honest): no root —
  the Wails cgo desktop packages cannot compile here (gtk4/webkitgtk
  headers absent; CI installs them); no codename literals in this log.

Stage Summary:
- v1.8.8 -> v1.9.0: every surface reads 1.9.0 through the ONE release
  authority; changelog.md gains the v1.9.0 entry; ROADMAP restructured
  (compact baseline, v1.9 current/future boundary, v1.10-2.0 future);
  UPDATE.md = v1.9.0 notes; README current-only; ARCHITECTURE documents
  the v1.9 authorities; agent.md is the repaired v1.9.0 handoff.
- The Linux E2E blocker is fixed in the product; the full browser suite
  is green on the real stack; Windows-side sources carry no regression
  (platform-independent Go/TS only).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.0-FINAL.zip
  (repository tree; ZIP audit: opens, required sources present, version
  1.9.0 everywhere, no stale current identity, no codename leakage,
  exactly one LICENSE.md).

---
Task ID: v1.9.1-release
Agent: v1.9.1 engineering session (2026-10-08)
Task: v1.9.0 -> v1.9.1 — P0 root-cause + repair of the authoritative
Linux CI live-stream failure (Actions run 37704905404, Linux Browser-E2E
job 113077751580, e2e/live-stream.spec.ts "streamed text is visible
WHILE the run is live and grows without Stop"; CI 39/40), evidence-
discipline repair for the v1.9.0 "40/40" claim, Windows/Linux desktop
RUNTIME SMOKE gates in CI, packaging.

Work Log:
- EVIDENCE CLASSIFICATION FIRST: the v1.9.0 entry below records "full
  Playwright suite 40/40" — that was a LOCAL single-run claim. The
  authoritative GitHub Actions run 37704905404 shows 39/40 with the
  live-stream growth assertion failing. Correction recorded here: local
  runs are local evidence only; GitHub Actions is the only CI authority;
  the v1.9.1 verdict below is likewise local until the CI run carrying
  this revision reports.
- INSPECT: HEAD 4cdd392 (v1.9.0); the whole streaming chain was read
  (orchestrator emitProgress -> runLive.observe -> activityHub (bounded,
  conflation-aware) -> WS writer -> store fast path -> accumulator ->
  triple-boundary scheduler -> GenerationBubble DOM).
- MEASURED FIXTURE FACTS (this 2-core sandbox, real native engine):
  standalone engine-host probe — decode ~130+ tok/s (160 tokens over 21
  event frames in ~0.26 s warm), and PREFILL ~25-40 ms per prompt token
  (2-token prompt TTFT 0.02 s; ~300-token prompt TTFT 8.15 s; ~450-token
  prompt TTFT 15.9 s). Full-stack run telemetry: system briefing 440
  tokens, context 446/3.6k, TTFT 7.6 s, classifyMs 4091 (the classify
  stage includes the engine-gate wait), run total ~12.8 s, response
  frames 22 over a ~1.2 s window (p50 inter-frame 62 ms), final persisted
  reply 203 chars, exactly one assistant message.
- REPRODUCE: the failing test passed in isolation UNLOADED (20.3 s), so
  the CI failure was reproduced under MEASURED CPU CONTENTION (2 busy
  workers saturating both cores): the test FAILED with the exact
  authoritative signature — growth poll "Expected: > 0, Received: 0"
  (predicate stuck at 0: Stop visible the whole time, text never past
  the baseline) and the failure screenshot shows the run STILL LIVE at
  30 s with the bubble rendering the "…" PLACEHOLDER arm.
- ROOT CAUSE (test observation contract, NOT the product stream): the
  v1.8.2 contract sampled the live bubble's textContent and accepted ANY
  non-empty text as "streamed text". GenerationBubble renders
  PRESENTATION PLACEHOLDERS before the first content snapshot
  ("Connecting to the engine and preparing the turn…" — 48 chars — while
  preparing, "…" afterwards), so waitForLiveText passed within ~100 ms
  of Send on placeholder text; the growth poll's 30 s budget then ran
  during the engine gate + prefill phase (measured above: >30 s under
  contention), expiring before the real streaming window ever opened.
  The product chain was verified healthy end to end (WS frame + DOM
  timeline: browser received every cumulative snapshot; DOM followed at
  50-70 ms cadence; one persisted reply).
- REPAIR (observation only, never weaker):
  1) src/MessageStream.tsx: both placeholder arms now carry
     data-stream-placeholder in the DOM — presentation text is
     distinguishable from streamed content.
  2) e2e/live-stream.spec.ts: waitForLiveText + both growth observations
     require NON-placeholder snapshots; the growth baseline is the FIRST
     REAL streamed snapshot; the rAF-suspended proof uses the same
     real-content contract. All original assertions preserved.
  3) Fixture budget 160 -> 320 tokens: the engine emits one event chunk
     per 8 tokens (kEmitTokenWindow), so the budget IS the deterministic
     snapshot count of the live window (40 chunks); window duration
     still scales with host speed, snapshot count does not.
- MUTATION VERIFICATION: with the repaired test under the same 2-worker
  contention — (a) repaired contract: PASS; (b) placeholder detection
  disabled (the v1.9.0 contract restored by mutation): FAIL with the CI
  signature; (c) restored: PASS unloaded and under load. The coverage
  detects the defect class it was written for.
- CI RUNTIME SMOKE GATES (wired for the CI environment; NOT executed in
  this sandbox): build-linux gains "Desktop runtime smoke (native Wails
  binary under Xvfb)" — the REAL CGO desktop executable launched under
  Xvfb, session banner + boot markers asserted STATE-BASED in the
  isolated data dir's logs/app.log, liveness + no-crash-report, bounded
  SIGTERM shutdown — and "Headless surface runtime smoke" — the headless
  build serving the REAL embedded UI + API: event-driven /api/health
  readiness, GET / proves the embedded shell, /api/models discovery,
  clean shutdown. build-windows gains "Desktop runtime smoke (real
  executable, process + backend init)" — SHEYTAN-LA.exe launched with an
  isolated SHEYTAN_DATA_DIR, banner/liveness/no-crash state-based,
  CloseMainWindow + bounded exit (Kill only as a failing-run backstop).
  Packaging remains a DISTINCT gate: packaging success never implies GUI
  runtime success. Evidence levels are labelled explicitly in the steps.
- LOCAL VERIFICATION OF THE SMOKE DESIGN: the headless-surface smoke was
  executed HERE against the real binary and passed (embedded UI +
  health + model discovery + clean shutdown). The native Wails desktop
  binary CANNOT be built in this sandbox (no gtk4/webkitgtk pkg-config
  headers, no root) — same documented boundary as v1.9.0; its smoke step
  is CI-wired and its execution is recorded nowhere until CI runs it.
- Version identity 1.9.1 through the canonical gate (release-version
  --check green); changelog.md gains the factual v1.9.1 entry; UPDATE.md
  rewritten as v1.9.1 current notes; agent.md is the v1.9.1 handoff;
  ROADMAP gains the measured native-prefill batching work item; README
  current-only.

Verification (evidence classes; LOCAL Linux x86-64 sandbox, Go 1.27,
Node 24, 2 cores — see the classification note above):
- Frontend: typecheck green; oxlint 0 warnings/0 errors (108 files);
  npm run test:units 220/220; npm run test:release 38/38 (includes the
  codename gate); production build + embedded sync green;
  verify-static-assets green.
- Go: go test ./internal/... -tags headless -count=1 = 62 packages ok
  (2 load-sensitive flake observations in internal/api on this 2-core
  box while OTHER Go suites ran concurrently: TestGovernorEndpoint-
  ServesComposedSelfModel and TestLocalGemmaHiChatCompletes (30 s
  chat deadline) — both pass in isolation, both also flaked at the
  v1.9.0 baseline the same way, and the api package passes cleanly
  when run sequentially; not v1.9.1 regressions — CI runs suites
  sequentially). go test ./... -run Test = 61 packages ok + the two
  sandbox-blocked desktop packages (root + internal/desktop) failing
  to BUILD for the documented no-GTK-headers boundary. go vet ./cmd/...
  ./internal/... clean (the same two packages are the sandbox build
  boundary; CI vets the full module with the deps installed). Race
  gate (-race, headless): api/agent/sessions/contextplan/histref/
  runtime ALL ok.
- Native: clean cmake configure + build + ctest 12/12 green.
- Browser E2E: FULL suite 40/40 green (5.6 min, real headless server +
  real native engine + fixture GGUF, no skipped failures) — the
  repaired live-stream spec (3 tests) included; additionally the
  live-stream growth test was verified under 2-worker CPU contention
  (PASS) and with the placeholder detection mutated away (FAIL with
  the CI signature — the mutation evidence above).
- Stress: 47 pass / 0 fail, hangs=0, crashes=0.
- Headless-surface runtime smoke: executed HERE against the real
  binary — PASS (embedded UI + /api/health overall=ok + model
  discovery + clean SIGTERM shutdown).
- NOT executed here (stated honestly): GitHub Actions (no runner
  access from this sandbox), the native Wails desktop smoke (GTK4/
  WebKitGTK headers absent — the binary cannot even build here),
  Windows (no Windows). The authoritative v1.9.1 CI verdict is the
  Actions run that carries this revision; until it reports, no CI
  claim is made for v1.9.1.

Stage Summary:
- v1.9.0 -> v1.9.1: the authoritative live-stream CI failure is
  root-caused (placeholder-baseline observation contract racing the
  measured prefill phase — NOT a product streaming defect; the chain was
  verified healthy end to end), repaired with strengthened (never
  weakened) assertions, mutation-verified coverage, and a deterministic
  fixture snapshot budget; the v1.9.0 "40/40" claim is re-classified as
  local-only evidence with the CI 39/40 recorded as authoritative;
  desktop runtime smoke gates (Linux Xvfb + Windows process/backend-init)
  are wired into CI with explicit evidence-level labels; version
  identity is exactly 1.9.1 through the canonical gate.
- Full local regression green on this revision (see Verification above);
  the CI verdict for this revision is pending by construction (no local
  run may claim it).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.1-FINAL.zip (complete
  reproducible source tree; exclusions per the release contract; ZIP
  audit recorded in this entry).

---

## v1.9.3 session (2026-10-10) — the Governor race-gate root cause (reproduced on real load), the honest prefill-parity contract, the closed zero-session proof standard, the UI microtext floors

Authoritative starting point: Actions run 38035650428 (v1.9.2, HEAD
e5e1b79) — job 114165397477: the Go race gate
(`go test -race -tags headless -count=1 ./internal/api ./internal/agent
./internal/sessions ./internal/contextplan ./internal/histref
./internal/runtime`) failed `TestGovernorEndpointServesComposedSelfModel`
(internal/api/governor_test.go:93) with
`ok-pressure envelope class = live, want none`; agent, sessions,
contextplan, histref and runtime passed; Windows/Linux/publish jobs were
skipped behind the audit gate. Local HEAD verified identical: e5e1b79.

Work Log:
- Environment: cloned the repo at the authoritative HEAD; Go 1.26.1
  (matches the workflow's GO_VERSION "1.26"); cmake 4.4.4 (native
  engine); npm ci for the frontend toolchain.
- REPRODUCTION (true, not simulated): `governor.CPULoadPlatform` on
  Linux reads /proc/loadavg load1 normalized by cores. Spinning 4 busy
  loops on the 2-core verification host drove the real 1-minute loadavg
  to 2.87 (≈143% of core capacity) and the ORIGINAL v1.9.2 code failed
  the exact test with the exact CI signature
  (`governor_test.go:93: ok-pressure envelope class = live, want none`).
  Mechanism confirmed: runtime.StartGovernor wires the platform CPU seam
  into the governor the API test folds samples into; the test folded ONE
  OK-pressure RAM sample; rollingSeries.observe sets its FIRST EWMA
  sample equal to the raw value; one high first sample crossed
  CPUReduceAbove (85) and envelopeLocked flipped the class to `live`.
- FIX (internal/governor/governor.go): pinned the CPU policy contract in
  code — the envelope's CPU-reduction branch requires a SUSTAINED
  rolling signal (≥ cpuWarmupSamples = 3 folded samples AND average ≥
  CPUReduceAbove); the folded sample count rides the new
  ResourceState.CPUSamples and the reduction reason names the sample
  count. A lone spike (including the first sample ever folded) can no
  longer flip the envelope; sustained high load (~45 s at the 15 s
  cadence) still reduces background/concurrency. Added the
  mutex-guarded `Governor.SetCPUSampler` seam (production keeps
  CPULoadPlatform as the ONE platform sampler).
- TEST DETERMINISM (internal/api/governor_test.go): the OK-pressure
  wire-contract test now pins the CPU seam to an honest unknown before
  folding its sample — host load cannot leak into the pressure contract.
- POLICY TESTS (internal/governor/governor_test.go):
  TestSustainedCPUHigherThanThresholdReduces now folds a genuinely
  sustained signal (3 samples — its name means it); NEW
  TestLoneCPUSpikeDoesNotFlipEnvelope pins both directions (one high
  sample → class none; the same load sustained past the warm-up floor →
  class live).
- FIX VERIFICATION under real load: the fixed code passed the exact
  failing test with the host at 96–218% measured load (loadavg 1.92–4.35
  on 2 cores). The race gate command then ran GREEN on all six packages
  (api 102s under -race, agent, sessions, contextplan, histref,
  runtime). go vet ./... green; the full ./internal/... -tags headless
  suite green.
- NATIVE PARITY HONESTY (native/engine/tests/test_prefill_parity.cpp):
  the v1.9.2 header claimed per-position logits parity but the test
  compared only the full prompt's FINAL logits (the span path projects
  only span-final logits by design). The v1.9.3 gate proves and claims:
  per-position logits through the chunk=1 span cadence (bit-for-bit at
  all 20 fixture positions), final-token logits for chunks 3/7/16, full
  KV bytes identical across every chunking AND the serial run,
  chunk-boundary invariance of the final logits, edge/error bounds,
  mixed prefill→decode continuity; tied output explicitly NOT claimed
  (every shipped fixture carries output.weight); cancellation documented
  as the between-chunks generate-loop contract. CMakeLists.txt and
  forward.h comments aligned. Clean native configure/build/ctest:
  13/13 passed locally.
- ZERO-SESSION PROOF STANDARD (e2e/zero-session.spec.ts): the three
  tests v1.9.2 did not cover used count-based assertions that the
  placeholder-carrying generation bubble (it also carries
  .message-row.from-agent) or an empty persisted reply could satisfy;
  all now prove the settled reply is REAL content via
  expectRealAssistantReply (non-empty .message-content, no
  [data-stream-placeholder] descendant). e2e/live-stream.spec.ts: the
  rAF-suspended test additionally proves the persisted reply non-empty.
  No assertion weakened anywhere.
- UI MICROTEXT FLOORS (src/styles.css): all 62 font-size declarations at
  6–9px raised to the documented floors — dense secondary metadata ≥
  10px; essential labels/states/errors ≥ 11px (topbar status, engine
  status, error banner, Lab inline error/failure, automation form
  error); interactive control text ≥ 12px (Stop button, text/secondary
  buttons, composer + Lab/Automation textareas). No healthy size
  touched. The audited min-width conflict was re-examined: the ≤1050px
  breakpoint already releases body min-width to 0 and the native window
  clamps at MinWidth 1024 (desktop.go), so the layout contract holds;
  no structural layout change was made.
- VERSION/DOCS: package.json bumped to 1.9.3 and
  scripts/release-version.mjs repaired config.go, build/config.yml and
  SIGNATURE (--check green); README.md, UPDATE.md (v1.9.3 notes),
  changelog.md (v1.9.3 entry), ARCHITECTURE.md (the CPU policy
  contract), agent.md (v1.9.3 handoff), ROADMAP.md unchanged (future
  work only), this worklog entry.
- LOCAL EVIDENCE BOUNDARY (honest): verified locally — race gate green
  (all six packages), full headless Go suite green, go vet green,
  frontend typecheck/lint (0/0)/units (220/220)/build/release gates
  green, clean native CMake configure/build/ctest 13/13, the
  reproduction pair (original fails / fixed passes under real load).
  NOT verified locally — a green Actions run (local pass does not
  establish CI), the Windows/Linux desktop runtime smokes (no Windows;
  no GTK4/WebKitGTK in the sandbox), the full Playwright browser E2E
  suite (CI-bound). The authoritative v1.9.3 verdict is the Actions run
  that carries this revision.
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.3-FINAL.zip (complete
  reproducible source tree; exclusions per the release contract; ZIP
  audit recorded in this entry).

---

## v1.9.2 session (2026-10-10) — the measured live-stream/TTFT engine repair, the desktop window-close lifecycle fix, the zero-session live-surface fix

Authoritative starting point: Actions run 37869632400 (v1.9.1, HEAD
f687489) — Linux job 113625251074: Browser E2E 37/40, ALL THREE
e2e/live-stream.spec.ts tests expired at the 60 s wait for REAL
non-placeholder streamed text; Windows job 113625251146: the desktop
runtime smoke process survived CloseMainWindow() + 15 s. The v1.9.1
release-level success claim is thereby corrected: local 40/40 was LOCAL
evidence only, and the placeholder-baseline diagnosis, while correct,
was not the complete root cause.

REPRODUCE (Linux, 2-core sandbox, the full real stack built from this
tree):
- The three live-stream tests PASS in isolation against a Release-built
  engine (matching the v1.9.1 local claim) and FAIL with the EXACT CI
  signature (60 s expiry, run still live, zero real snapshots) with the
  engine built the way CI builds it. Authoritative reproduction, not a
  guess.
- Server log evidence from the failing runs: promptBytes=1785
  estTokens=445-446 (the GO estimate), and the native generation still
  "before the first token" at 59-66 s (cancelled by the test's expiry).
  The chain engine -> hub -> WS -> fast path -> accumulator -> scheduler
  -> DOM was separately verified healthy (probe: every cumulative
  snapshot rendered at 50-70 ms cadence).

MEASURE (standalone C++ benchmarks against the live fixture GGUF,
Release and default builds):
- The E2E prompt encodes to 1681 ENGINE tokens (the fixture vocab has 5
  merges: ~1 token per byte; the Go-side estimate is 3.8x lower). The
  prefill budget question is decided by the ENGINE count, not the Go
  estimate.
- Serial per-token forward cost: 19.08 ms (-O2) and 19.02 ms (-O0,
  CI's exact configure) — the optimization level is NOT the dominant
  factor. Per-token arithmetic is ~1 ms; the rest is PER-ROW TENSOR-NAME
  RESOLUTION: ~8064 weight rows per token, each resolved through an
  ostringstream-built name plus std::map lookups (shape2d + find x2 +
  layout) inside row_span/row_f32.
- First token therefore ~= 1681 x 19 ms ~= 32 s standalone; under
  browser-suite contention on 2-4 cores it exceeds the 60 s budget —
  all three tests expire inside the gate+prefill phase. The v1.9.1
  fixture budget increase (160 -> 320) cannot affect TTFT (confirmed).

ROOT CAUSE (stream): serial prefill over a per-row-name-resolution
forward path. FIX (in the engine, at the proven boundary):
- tensor.h/.cpp: `Weights::resolve` validates a 2-D tensor ONCE (name,
  shape, type, WHOLE-tensor byte range through the same checked
  arithmetic) and returns a stable ResolvedTensor record; `row_ptr` +
  `dequant_row` own the per-row pointer math and the ONE dequantization
  switch (row_f32 keeps its behavior).
- forward.h/.cpp: `resolve_model` builds the per-layer resolved table
  (7 matrices/layer + the output projection) once per binding;
  `run_layers` is the single numerics authority for token() AND
  prefill_span; `matvec_resolved` replaces per-row name resolution.
  Measured: 19.08 -> 3.66 ms/token (5.2x), prefill AND decode.
- generate.cpp: the prefill loop calls `Forward::prefill_span` in
  bounded 16-token chunks (cancellation cadence unchanged, mu_ held per
  chunk exactly like the serial path), with the vocabulary logits
  projection ONLY for the final prompt token (intermediate prompt
  logits were never consumed; on real-vocab models the output
  projection is the largest per-token cost — the fixture's 50-token
  vocab makes it neutral there).
- CMakeLists.txt: CMAKE_BUILD_TYPE defaults to Release when unset (a
  bare `cmake -S -B` — CI's exact invocation — previously shipped an
  unflagged -O0 engine; the recorded measurement shows -O0 == -O2 for
  the name-resolution-dominated F32 fixture, and unoptimized inference
  arithmetic was never a supported configuration).
- PARITY GATE: tests/test_prefill_parity.cpp — prefill_span's
  last-token logits and the FULL KV cache bytes must match the serial
  token() path BIT-FOR-BIT across chunk sizes 1/3/7/16, plus edge cases
  (empty span OK, null span INVALID_ARG, out-of-vocab INVALID_ARG,
  position overflow CONTEXT_OVERFLOW, last-token logits with a null
  buffer INVALID_ARG) and mixed span -> token() decode continuity.
  Native suite: 13/13 (12 previous + this gate).

ROOT CAUSE (Windows smoke -> PRODUCT defect): the wails v3.0.0-beta.16
sources show a webview window's WM_CLOSE (Windows) / GTK close-request
(Linux) only EMITS the platform WindowClosing event and destroys the
window; the application event loop keeps running with zero windows
(Windows quits only from the hidden "__wails_hidden_mainthread"
window's WM_CLOSE/WM_DESTROY; no quit-on-last-window logic exists for
webview windows on either platform). SHEYTAN-LA is single-window with
no tray: closing the main window stranded a zombie process with the
backend running and the deferred srv.Close() un-executed — the
authoritative smoke failure was a REAL product defect, not a smoke
measurement artifact. FIX: internal/desktop/desktop.go binds
events.Common.WindowClosing -> app.Quit() (listener-order safe on both
dispatch orders — the default close listener and this binding are
idempotent in either order). Desktop lifecycle is thus: visible close
-> Quit -> app.Run returns -> deferred cleanup.

SMOKE GATE REPAIRS (evidence level made precise, never weakened):
- Windows: requires MainWindowHandle != 0 (Refresh-bounded wait),
  checks CloseMainWindow()'s BOOL (false => fail "no window received
  the close request"), requires exit code 0 through the normal path,
  and reports the exact unmet evidence level when no window exists.
  Kill remains the failing-run backstop only.
- Linux: the step owns a real Xvfb display on a dedicated display
  number (the xvfb-run wrapper's PID was never the app's PID; SIGTERM
  on the wrapper killed the X server, not the app lifecycle), locates
  the app's REAL top-level window via `xdotool search --onlyvisible
  --pid` + a `getwindowpid` OWNERSHIP CHECK, closes it with the WM's
  own WM_DELETE_WINDOW client message, requires normal-path exit
  (status 0), and keeps SIGTERM only as the failing-run backstop.
  xdotool added to the Linux dependency step. YAML validated.

P1 DISCOVERY + FIX — the zero-session live-surface clobber (exposed by
the 5.2x engine speedup; a REAL product defect, 5/5 measured):
- Symptom: after deleting the last session and sending, the live
  generation surface died ~90 ms after Send (bubble + Stop up at t0,
  stop gone at +90 ms, "aborted" at +160 ms) while the backend run
  completed normally 14 s later; the zero-session visible-before-
  completion E2E failed accordingly.
- Evidence chain (temporary in-page tracers, removed after the fix):
  the server's attach sequence attached -> idle -> run_snapshot lands
  just after run() sets its startup state; the attach-time idle
  sentinel (no session, no lastRun — composed BEFORE the run existed)
  hit recoverRunFromIdle's final fallback ("lost") because the grace
  branch saw run evidence that predates/belongs to no run; the armed
  grace re-check would have re-clobbered at +2.7 s (the real run's
  first gate evidence arrives at ~4.6 s). Bisection: without the
  prior-session deletion the flow is textbook (10.7 s live surface,
  clean settle).
- FIX: the attach-handshake exclusion — a lastRun-less idle sentinel
  within 250 ms of the attach acknowledgement
  (activityAttachAckAt, set when the `attached` frame settles) is
  handshake, never recovery evidence. Path A (idle + lastRun -> the
  v1.2.6 fast-run recovery) and late-idle semantics are unchanged.
  Post-fix harness runs 3/3: live surface holds 10-15.5 s, settles
  normally.

E2E CONTRACT CORRECTIONS (never weaker):
- zero-session.spec.ts: the visible-before-completion MutationObserver
  now excludes data-stream-placeholder arms (presentation text is never
  model output) and observes within a 60 s bound (the gate+prefill
  phase legitimately precedes the first snapshot; the v1.9.1 30 s bound
  expired INSIDE that phase under load — measured ~13 s unloaded).
- live-stream.spec.ts: a poll timeout now attaches compact structured
  diagnostics (the live-surface snapshot + the server log tail — the
  timeline evidence the authoritative Actions logs lacked) without
  dumping prompt/answer content; the header documents the v1.9.2 root
  cause with the measured numbers.

VERIFY (local evidence classes; Linux x86-64 sandbox, Go 1.26, Node 24,
2 cores — LOCAL ONLY, no Actions claim):
- Frontend: typecheck green; oxlint 0/0 (106 files); test:units 220/220;
  test:release 38/38; production build + embedded sync + verify:web
  green.
- Go: internal/... -tags headless 62/62 packages ok (first run, no
  flakes); ./... green except the documented sandbox build boundary
  (internal/desktop + the root package cannot BUILD without
  gtk4/webkitgtk pkg-config headers — no root in this sandbox; CI
  installs them); go vet clean on every compilable package; -race
  (api/agent/multiagent) clean.
- Native: clean cmake configure (default now lands Release) + build +
  ctest 13/13.
- Browser E2E: the FULL suite 40/40 green, 4.4 min (real headless
  server + real native engine + fixture GGUF; the three live-stream
  proofs included; the corrected zero-session proofs included). An
  interim full-suite run surfaced the zero-session clobber (the P1
  above); the final run is post-fix.
- Stress: 47/47, hangs=0, crashes=0 (after the functional gates).
- NOT executed here (stated honestly): GitHub Actions (no runner
  access from this sandbox); the desktop runtime smokes (the Wails
  binary cannot even build without the GTK4/WebKitGTK headers; no
  Windows). The authoritative v1.9.2 CI verdict is the Actions run
  that carries this revision; until it reports, no CI claim is made.

Version identity 1.9.2 through the canonical gate (release-version
--check green; package.json -> config.go / build/config.yml / SIGNATURE
repaired by the canonical script). README current-only; UPDATE.md
rewritten as v1.9.2 current notes; changelog.md carries the factual
v1.9.2 entry; ROADMAP's prefill item replaced by the POST-v1.9.2
arithmetic-optimization item (the name-resolution bottleneck is fixed
and measured); ARCHITECTURE current for v1.9.2 (the resolved forward
path + the desktop lifecycle); agent.md is the v1.9.2 handoff.

ZIP AUDIT (download/SHEYTAN-Local-Agent-v1.9.2-FINAL.zip, 747 files,
extracted to a clean room):
- opens/extracts; root SHEYTAN-Local-Agent/ present;
- required sources present: go.mod, main.go, main_windows.go,
  internal/{desktop,agent,api}/..., src/{store,MessageStream}.tsx,
  native/engine/{src/forward.cpp,src/generate.cpp,CMakeLists.txt,
  tests/test_prefill_parity.cpp}, e2e/{live-stream,zero-session}.spec.ts
  + fixtures + the live-model generator,
  .github/workflows/build-desktop.yml, scripts/release-version.mjs,
  web/static/index.html (the embedded frontend carries the store fix);
- exact version 1.9.2: package.json / internal/config/config.go
  AppVersion / build/config.yml productVersion / SIGNATURE first line;
- no stale current-release identity (README/agent.md/worklog all v1.9.2;
  UPDATE.md = the v1.9.2 notes);
- exactly one LICENSE.md (root);
- junk scan clean: no .git, no node_modules, no native build dir, no
  e2e/.build, no e2e/.artifacts, no dist, no *.log, no ZIPs inside;
  GGUF files only the COMMITTED test fixtures (tests/fixtures/);
- codename gate: zero codename references (run on the extracted copy);
- release-version --check: consistent (run on the extracted copy);
- CLEAN-ROOM BUILD from the extracted copy: native cmake configure
  (CMAKE_BUILD_TYPE=Release by default — the CI-class bare configure) +
  build + ctest 13/13; go build -tags headless OK;
  go test ./internal/api -tags headless ok; npm ci + test:units
  220/220. The archive is a complete, independently buildable source
  tree.

---

## v1.9.6 session (2026-10-11) — the FINAL 1.9.x release: the Windows admission-boundary repair, the Linux smoke environment repair, the R&D alignment + the measurement baseline

Task ID: v1.9.6-final (single-agent session)
Agent: release engineering agent (authoring host: Linux x86-64 VM, no
root, no GTK4/WebKitGTK headers, no Windows, no GUI)

Work Log:
- INSPECT: cloned live `main` — HEAD is exactly the baseline commit
  `070a05930f35ac09536ead74cc7cf2f376b8c2a4` (v1.9.5), clean tree; the
  R&D decision doc was read in full from `Parsaetak/Contents` branch
  `Research-and-Development` (`SHEYTAN-LA-2026-10-11.md`). GitHub API
  access to the failing run was rate-limited (unauthenticated), so the
  authoritative failure facts were taken from the mission record
  (exact test names, job IDs, messages) and re-derived from source.
- REPRODUCE (Windows 503): both authoritative failures re-derived from
  the code path; the causal chain was proven deterministically —
  `updateEngineVariantTx` (v1.9.5) takes the admission claim at
  TRANSACTION start; the background AUTO Vulkan probe
  (`go s.maybeAutoProvisionVulkan(gate.Done())` in `EnsureSetup`) is
  fully eligible on windows/amd64 (AUTO default, GPUAutoOffload=true
  default, GPU detected on CI runners, CI online, resolvable fixture
  model; `VariantSupported(Vulkan)` is windows-only) and held the claim
  across the network resolve/download → POST /api/run → 503. Linux is
  structurally immune (platform matrix gate).
- ROOT CAUSE + FIX (Windows): moved the admission claim to the
  DESTRUCTIVE boundary of the transaction; split the staged download
  from the swap via the new `updater.DownloadEngineArchiveStaged`
  (non-destructive half, no install lease; the swap half keeps the full
  lease/lock discipline through
  `InstallStagedFromArchiveDeferredWithVariant`); added the
  unconditional early advisory live-run refusal (read-only; v1.9.4
  semantics preserved at the boundary via the nil-seam re-check); a
  download failure no longer restarts the engine (nothing was stopped);
  fresh `wasRunning` re-read at the boundary. Both local-chat E2E
  fixtures pin an explicit CPU profile (the unrelated background probe
  is out of the pinned contract).
- VERIFY (Windows): `TestLocalGemmaHiChatCompletes` +
  `TestLocalChatSurvivesUnreadableModelCard` PASS, `-count=5`
  (determinism); new `internal/llm/admission_boundary_v196_test.go`
  pins (claim observed INSIDE the destructive window via the
  verification hook; begin/end balance; mid-download registration
  refused at the boundary without claiming; nil-seam boundary re-check);
  revised `admission_v195_test.go` (non-destructive exits never claim)
  + `rungate_v194_test.go` (single early consult); the FULL archive-seam
  runtime choreography suite (commit/rollback/honest-manifest) passes
  UNCHANGED against the new order.
- ROOT CAUSE + FIX (Linux smoke): the smoke shell is unchanged since
  v1.9.2 and earlier releases were skipped behind upstream gate
  failures — run 38095637817 was the gate's FIRST real execution; the
  "NO visible top-level window" + alive-process signature is the
  documented bare-Xvfb failure class (WebKitGTK 6.0 DMABUF renderer /
  GTK4 GL renderer stalling without GPU/GLX/EGL/compositor). Fix owned
  by the smoke step: `WEBKIT_DISABLE_DMABUF_RENDERER=1`,
  `WEBKIT_DISABLE_COMPOSITING_MODE=1`, `GSK_RENDERER=cairo` + xwininfo
  root-tree failure diagnostics (`x11-utils` added to deps). The gate
  (--onlyvisible + pid ownership + WM_DELETE_WINDOW + exit 0) is
  UNCHANGED and remains required.
- R&D slice: `internal/perf` (metric set of R&D §10; NOT-MEASURED
  semantics; nearest-rank P50/P95 with exact-value unit tests; targets
  recorded as gaps) + `cmd/shtn-perf` (JSON artifact + concise report;
  cold start + memory measured on the authoring host; generation-path
  metrics honestly NOT MEASURED without a real engine/model). NO
  performance target claimed as met.
- DOCS: canonical version gate 1.9.6 (package.json →
  release-version.mjs → config.go / build/config.yml / SIGNATURE,
  --check green); changelog v1.9.6 entry (the ONLY release chronology);
  UPDATE.md rewritten as current release notes; README current-release
  line; ARCHITECTURE engine/run coordination revised + strategic
  direction section; ROADMAP v1.9.6 section + the full v1.10.0 plan
  (phases/gates/dependencies/risks + the UpdateEngineNow standing gate
  item); agent.md handoff; native/engine/README promotion-status section.
- REGRESSION (local): `go build -tags headless ./internal/... ./cmd/... .`
  clean; `go test ./internal/... -tags headless -count=1` ALL packages
  ok; `go vet` clean on touched packages; `internal/perf` unit suite
  green. Frontend and C++ suites: executed in Phase 9 of this session
  and recorded below (see the stage summary).

Stage Summary:
- v1.9.6 is code-complete on the authoring host with a minimal, clean
  source diff. The two authoritative CI failures are root-caused and
  repaired with deterministic regression pins; the R&D direction is
  documented as authoritative with the first slice shipped as code.
- Evidence boundaries kept honest: Windows live-answer acceptance,
  Linux native window smoke, packaged E2E and physical-hardware
  performance numbers are UNVERIFIED locally by construction; the
  authoritative verdict is the Actions run carrying this revision (no
  push/tag was performed from this session).

## v1.10.0 session (2026-10-11) — the FIRST R&D-cycle release: the Windows CDP transport repair, the Linux WM repair, the UpdateEngineNow admission seam, the evidence-gated Phase 1 decision, the honest Phase 0 baseline, the Qt/IPC seam definition

Task: open the R&D engineering cycle. Deliver v1.10.0 against the
authoritative failing run `38135751846` (v1.9.6, commit `33884e1`,
FAILURE — Source & frontend audit SUCCEEDED 2m58s; Windows x64 FAILED
13m57s at `Windows live-answer acceptance` AFTER a green desktop runtime
smoke; Linux x64 FAILED 10m47s at `Desktop runtime smoke (native Wails
binary under Xvfb)`; Publish skipped). Per-job/per-step conclusions were
pulled live from the GitHub API; the log payloads are not retrievable
unauthenticated (HTTP 403), so the failure mechanisms are DERIVED from
source + step-level timing, labeled as derivations throughout.

ROOT CAUSE 1 (Windows, derived and source-proven): the acceptance
harness's CDP switch traveled through `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`,
but the pinned wails v3.0.0-beta.16 pure-Go WebView2 loader
(`internal/webview2/webviewloader`, `windows && !native_webview2loader`)
runs `preventEnvAndRegistryOverrides()` at package init and pins that
variable to "" (`os.Setenv`) — the switch was erased before any WebView2
environment existed, the CDP endpoint never opened, and the harness
consumed its full bounded 60 s attach window (matching the ~75 s step
total: ~15–25 s numpy/fixture prep + 60 s + cleanup). Fix: the desktop
shell forwards the TEST-ONLY `SHEYTAN_ACCEPTANCE_CDP_PORT` through
`application.Options.Windows.AdditionalBrowserArgs` (the channel Wails
composes itself into `CreateCoreWebView2EnvironmentWithOptions`); the
harness sets exactly that variable; digits-only shape check; sanitized
length-only CDP-timeout diagnostics. Windows build + vet verified by
cross-compilation (`GOOS=windows go build/vet ./internal/desktop/`).

ROOT CAUSE 2 (Linux, evidence-classified): both v1.9.5 and v1.9.6 failed
the same gate identically DESPITE the v1.9.6 renderer switches, and the
v1.9.6 dump shows the app pid owning created-but-unmapped windows — the
GTK4 top-level does not map under bare Xvfb without a window manager on
this runner stack. Fix: the smoke step installs and starts `openbox`,
proves WM ownership via the EWMH `_NET_SUPPORTING_WM_CHECK` root
property (bounded poll), cleans it up in the existing trap, and the
failure dump gains WM-ownership evidence. Gate unchanged; product
unchanged for CI. NOT reproduced locally (no GTK4/WebKitGTK dev headers
and no root in this container) — labeled a derivation from this run's
own evidence.

ADMISSION SEAM (P0): `UpdateEngineNow` now takes the same
destructive-boundary claim as the variant transaction: early advisory
refusal (no claim) → claim atomically with the live verdict before the
first destructive step → balanced release on every exit → nil-seam
boundary re-check. Pins: `internal/llm/admission_updatenow_v1100_test.go`
(five deterministic pins: claim held across the destructive window
observed from the claim seam itself — running at Begin, StateReady at
End; begin/end balance; live-at-boundary refusal keeps the engine
serving; early refusal consults the gate exactly once; nil-seam
re-check consults exactly twice). All v1.9.6/v1.9.5 pins green
unchanged (9/9 in the focused run).

R&D PHASE 1 (promotion decision): native C++ CTest 13/13 locally
(clean cmake 4.4.4 configure + build; `test_prefill_parity` green =
bit-for-bit prefill/KV parity; forward/generate/tokenizer/tensor
suites green). `NativeBackendEnabled` keeps native as an explicit
opt-in; managed llama.cpp remains the default. The default flip is
BLOCKED on specific evidence: the warm-TTFT comparison native vs
managed llama.cpp on physical Windows target hardware (R&D Phase 0
measurement contract). Decision recorded in UPDATE.md / ROADMAP.md /
native/engine/README.md.

R&D PHASE 0 (baseline): `baselines/perf-hosted-ci-v1.10.0.json`
captured via the shipped `cmd/shtn-perf` (hostedCI=true, code SHA
33884e1 + v1.10.0 working tree): cold_start_to_interactive_ms P50/P95
1.4 (api.New + EnsureSetup boundary), idle/peak memory 16.5 MB
(server-only RSS posture); engine_ready / warm_ttft /
stream_chunk_to_visible / stream_duration / tool_round_trip /
file_output_overhead NOT MEASURED with per-metric reasons. Targets
remain targets.

QT/IPC SEAM: defined in ARCHITECTURE.md (typed coarse-grained local
boundary, versioned message shapes, capability-token auth reusing the
loopback discipline, no second authority, shared memory only on
profiled wins, explicit NOT-ready list). No Qt code ships.

REGRESSION (local, exact commands in this session's transcript):
frontend `npm run typecheck` clean; `npm run lint` 0 findings;
`npm run test:units` 236/236; `npm run test:release` 38/38;
`npm run build` + `npm run verify:web` green (bundle rebuilt at the
canonical 1.10.0 identity); native cmake+ctest 13/13; Go focused
admission suites 9/9; `go vet` clean on touched packages; Windows
cross-build of the repaired shell clean. UNVERIFIED locally: the two
desktop gates (no Windows host; no GTK headers), the full browser-E2E
matrix, the physical-hardware baseline, and any Actions verdict for
this revision (no push performed from this session).

Stage Summary:
- v1.10.0 is code-complete on the session host with a minimal diff:
  `internal/desktop/desktop.go` (CDP seam), `scripts/windows-live-answer-acceptance.mjs`
  (transport + diagnostics), `.github/workflows/build-desktop.yml`
  (openbox install + WM-owned smoke), `internal/llm/llama.go`
  (UpdateEngineNow seam), `internal/llm/admission_updatenow_v1100_test.go`
  (pins), `baselines/perf-hosted-ci-v1.10.0.json` (baseline), docs
  (UPDATE/changelog/README/ARCHITECTURE/ROADMAP/agent/worklog/native
  README), canonical version 1.10.0 everywhere.
- The stale worklog header ("Current release: v1.9.5") — a v1.9.6
  reconciliation miss — is fixed; this file now states the truth for
  v1.10.0.


---

## Session — v1.10.1: full Qt/QML migration + v1.10.0 failure repair (2026-10-11)

Scope: the run `38152579695` P0 repairs, the R&D Phase 2 Qt migration,
the complete Wails removal, the canonical test entrypoint, version 1.10.1.

Work Log:
- Live inspection: HEAD `c9494dc` (`v1.10.0`) confirmed on `main`; run
  `38152579695` (failure) job evidence read: Windows `internal/api`
  teardown failure in `TestWebSocketSeesTerminalErrorNotSilence`,
  Linux desktop-runtime-smoke 1x1-window failure.
- Root cause A (Windows): the WS test returned at the terminal error
  frame while the run goroutine's deferred cleanup (TrackRunEnd →
  registry release → hub close) was still executing; on Windows,
  t.TempDir's RemoveAll raced it ("directory is not empty"). Repair:
  (a) the test now waits for the authoritative outcome AND the run
  registry release (bounded poll over existing seams); (b)
  `Server.Close` JOINS all run goroutines (bounded) and refuses new
  run/resume registrations after shutdown began (`closing` flag +
  runWG, ordering made total under runsMu).
- Root cause B (Linux): the Wails/GTK window never mapped under
  Xvfb+Openbox (1x1). Resolved by migration, not another workaround.
- Qt Phase 2 implemented: `internal/localipc` (per-user UDS + SO_PEERCRED
  / named pipe + current-user DACL + client-SID check, keyed versioned
  handshake, stale-endpoint recovery, bounded framing, structured
  errors, clean idempotent stop) + `ui/qt` (CMake, Qt 6.8.2, CoreClient
  HTTP+RFC6455 over QLocalSocket, Main.qml chat UI) + `internal/desktop`
  launcher rewrite. The existing api.Server handlers serve the channel —
  no policy duplicated in C++.
- Wails removal: imports/module requirement/transitive graph gone
  (CI asserts absence), loopback activity transport retired,
  `/api/ws-origin` answers same-origin unconditionally,
  build/config.yml rewritten, CDP harness replaced by the QtTest
  acceptance suite.
- Tests: `internal/localipc` suite (9 tests: handshake happy/403/401,
  real API tunneling, malformed request resilience, oversized-header
  bound, stale-endpoint recovery, foreign-uid denial, idempotent stop);
  `ui/qt/tests/tst_acceptance.cpp` (QtTest: real QML + real core + real
  fixture engine: handshake → session → send → ordered live growth with
  Stop → exactly one settle → exactly one persisted assistant reply →
  reload persistence → repeat send); `scripts/qt-desktop-smoke.sh`
  (G1-G5 Linux gates); teardown regression tests preserved unchanged.
- CI: build-desktop.yml — Windows Qt build + windeployqt + Qt
  acceptance; Linux Qt build + G1-G5 smoke + Qt acceptance; negative
  Wails dependency gate; GTK/WebKitGTK deps removed; YAML validated.
- Docs: changelog (canonical v1.10.1 entry), UPDATE, README,
  ARCHITECTURE (Current for v1.10.1 + the Qt lifecycle truth),
  ROADMAP baseline, agent handoff, native/engine README stack diagram.
- Version identity: package.json 1.10.1 → release-version.mjs repaired
  config.go/build/config.yml/SIGNATURE; --check green; codename gate
  green.

Verified on the session host (Linux x64, Debian trixie, Go 1.26.0,
Qt 6.8.2, cmake 3.31.10, Xvfb): go build ./... (Linux + windows/amd64
cross) clean; go vet ./... clean; go test ./... -count=1 PASS (exit 0,
full suite); go test -race (six concurrency-heavy packages) PASS
(exit 0); localipc suite PASS; the two previously-failing api tests
PASS; Qt acceptance suite PASS; qt-desktop-smoke G1-G5 PASS (real
1280x840 window, keyed handshake, core interaction, clean WM-close
exit, no orphan, no leaked endpoint); release-version --check PASS;
codename gate PASS.

NOT RUN / NOT VERIFIED on the session host (honest boundary):
Windows tests and packaging (no Windows host — Go windows/amd64
cross-build is clean, Qt host is prepared for MSVC but unverified
locally); native engine CTest locally (CI runs it; local disk/time
budget went to the migration); the physical-Windows R&D Phase 1
baseline (NOT MEASURED — no physical hardware; the engine default
stays evidence-gated); a hosted-CI Actions verdict for this revision
(no push performed from this session); remaining Qt surfaces beyond
the chat/sessions core (documented, not claimed).

Stage Summary:
- v1.10.1 is code-complete on the session host. Primary deliverable:
  `download/SHEYTAN-Local-Agent-v1.10.1-FINAL.zip` (source archive,
  audited). Version identity 1.10.1 everywhere.

---
Task ID: 1-2-3 (audit + reproduce + plan)
Agent: Super Z (v1.10.2 hardening session)
Task: Phase 1 LIVE AUDIT, Phase 2 GATE REPRODUCTION, Phase 3 ROOT CAUSE + ORDERED PLAN

Work Log:
- HEAD = cb11a238 (v1.10.1) = the audited baseline; worktree clean; no local tags.
- Toolchain provisioned user-space (no root): Go 1.26.0, CMake 4.4.4/Ninja, Qt 6.8.2 (aqt, linux_gcc_64), Xvfb present, xdotool/xwininfo/openbox extracted via dpkg -x into /home/z/xtools.
- go vet ./... clean; go build ./... clean; go test ./... -count=1 PASS (full suite).
- Focused localipc contract suite PASS (9 tests: uid scoping, stale endpoints, keyed handshake happy/mismatch, real API tunneling, malformed/oversized rejection, clean stop, no-TCP-listener).
- QtTest live-answer acceptance PASS (3/3) on Qt 6.8.2 + real core + fixture engine: keyed handshake, session create, live text growth with Stop visible, exactly-one terminal settle, one persisted assistant reply, reload, repeat send.
- GitHub Actions API query: 403 rate-limited from this IP → hosted-CI verdict NOT QUERYABLE this session (no push performed; per rules).
- npm audit: HIGH — ws 8.18.3 (direct devDependency): GHSA-58qx-3vcg-4xpx (uninitialized memory disclosure), GHSA-96hv-2xvq-fx4p (memory-exhaustion DoS). Smallest compatible fix: direct bump to 8.22.0 (same major), no --force.

ROOT CAUSE TABLE (Phase 3):
| # | Area | Cause / Evidence | Fix priority |
|---|------|------------------|--------------|
| R1 | docs drift | README.md "Current release: v1.10.0" vs canonical 1.10.1 | P3 (Phase 8) |
| R2 | smoke portability | scripts/qt-desktop-smoke.sh hardcodes XTOOLS=/home/z/xcb-libs (machine-specific; broken default anywhere else) and references $WORK/closewin without building it; no WM ownership (spec: smoke owns its WM) | P2 (Phase 5) |
| R3 | perf fail-open | cmd/shtn-perf --hosted-ci defaults true and is never cross-checked against host evidence; physical<->hosted can be silently swapped; RAM/hostname not recorded | P1 (Phase 6) |
| R4 | desktop lifecycle tests | internal/desktop has zero test files (uiExecutable resolution untested) | P2 (Phase 6) |
| R5 | remaining Qt surfaces | AI Systems, Goals, Coding Lab, Net Search, repoindex, tasks, downloads/diagnostics still headless/web (ROADMAP L141-143). Highest-value safe slice: AI Systems list+activation (GET /api/systems, POST /api/systems/{id}/activate — typed, bounded, existing authority) | P2 (Phase 7) |
| R6 | version identity | all canonical surfaces at 1.10.1; ui/qt/CMakeLists project VERSION not covered by release-version.mjs (manual sync needed) | P3 (Phase 8) |
| R7 | Windows Qt gates | MSVC/windeployqt/windowed smoke/QtTest-on-Windows cannot run here (no Windows host). Hostable proof: GOOS=windows cross-build of the core + static review of listener_windows.go + workflow. Honest status: NOT RUN / PHYSICAL HARDWARE UNVERIFIED | P1 honest-report (Phase 4) |

ORDERED PLAN: R7(honest hostable subset) → R2 smoke hardening+WM → R3 perf fail-closed → R4 desktop tests → R5 AI Systems Qt surface + acceptance slot → R6/R1 version+docs → ws bump → full regression → baselines → ZIP.

Stage Summary:
- Baseline reproduced green locally (Go + localipc + Qt acceptance). Real defects found: R2, R3, R4 (+R1 doc drift, ws advisory). Windows gate remains honestly NOT RUN (no physical/hosted Windows).

---
Task ID: 4-10 (implement + regression + package)
Agent: Super Z (v1.10.2 hardening session)
Task: Phases 4-10 — Windows hostable subset, smoke hardening, perf fail-closed, desktop tests, AI Systems Qt surface, docs/version/security, full regression, clean ZIP

Work Log:
- R8/P0 Windows gate: `GOOS=windows go vet ./internal/localipc/` reproduced `undefined: credAllowed` (test-only helper behind !windows tag used unconditionally by serve_test.go → the Windows job's `go test ./...` could not even build). Moved credAllowed to platform-neutral endpoint.go. After: Windows vet + full cross-build clean; Linux suite unchanged. No test weakened.
- R2 smoke: scripts/qt-desktop-smoke.sh — XTOOLS/XLIBS default to system PATH (opt-in overrides only), self-builds closewin from scripts/closewin.c (-lX11), self-builds the core when unstaged, UI fallback to the developer build tree, Openbox/equivalent WM ownership (WINMGR_BIN, private-prefix XDG dirs resolved so themes load), trap-based cleanup of every spawned process. G1-G5 PASS under Xvfb+Openbox: keyed handshake, core-spawned UI pid, visible 1280x840 top-level, real core interaction, clean WM_DELETE_WINDOW exit, no orphan, no leaked endpoint (ran twice, deterministic).
- R3 perf fail-closed: --hosted-ci bool (default true) replaced by MANDATORY --host-class physical|hosted; DetectHostEvidence (8 well-known CI markers); CheckHostClassification refuses physical-over-hosted-evidence; hostedCI derived from the declared class (normalize at JSON/Report); RAM/hostname/evidence/notes recorded. Pins in internal/perf/perf_test.go. Verified refusals: no class → refused; physical+GITHUB_ACTIONS → refused. New artifact baselines/perf-hosted-v1.10.2.json (hosted session host, honestly noted).
- R4: new internal/desktop/desktop_test.go (5 tests: none sentinel, override, override-missing refusal, next-to-core resolution, hard refusal when nothing resolvable). Portability gate initially flagged my #!/bin/sh placeholder fixtures — fixed to plain bytes (gate discipline respected).
- R5 AI Systems on Qt: CoreClient systems/activeSystemId properties + refreshSystems/activateSystem over existing /api/systems handlers; Main.qml AI Systems drawer (list, active badge, activate, refresh); acceptance slot aiSystemsListActivate (seeded typed list, authoritative activation flip, typed 404 with unchanged authoritative state). Acceptance 4/4.
- R6/R1 version+docs: package.json 1.10.2 → release-version.mjs repaired config.go/build/config.yml/SIGNATURE; ui/qt/CMakeLists.txt project VERSION 1.10.2; README (stale 1.10.0 block), ARCHITECTURE (current-for + desktop-lifecycle paragraph), ROADMAP (shipped-baseline line, Phase 0 + Phase 2 v1.10.2 status), UPDATE.md (v1.10.2 notes; v1.10.1 retained as history; baseline/verification sections rewritten), changelog.md (v1.10.2 entry), agent.md (v1.10.2 handoff). Stale-wording sweep clean.
- Security: ws 8.18.3 → 8.22.0 (direct devDependency; GHSA-58qx-3vcg-4xpx, GHSA-96hv-2xvq-fx4p; no --force). npm audit --audit-level=high: 0 findings.
- Regression: node test:units 236/236 + test:release PASS; go vet/build clean; go test ./... -count=1 65 pkgs PASS; go test -race on 5 heavy packages PASS; headless build PASS; GOOS=windows build+vet PASS; native/engine cmake build clean + CTest 13/13 (incl. prefill parity); Qt clean reconfigure/build at 1.10.2 + acceptance 4/4; qt-desktop-smoke G1-G5 PASS under Openbox; E2E (playwright, real native engine host) 40/40 PASS; canonical `node scripts/verify-all.mjs` 16/16 PASS.
- Actions status: NOT RUN this session (no push per rules); the unauthenticated Actions API query was 403 rate-limited from this IP — even run STATUS was not readable. Windows MSVC/windeployqt/QtTest-on-Windows and the physical-Windows Phase 1 baseline remain NOT RUN / NOT MEASURED (no Windows host; no physical hardware).

Stage Summary:
- v1.10.2 is code-complete on the session host. Deliverable: download/SHEYTAN-LA-v1.10.2-FINAL.zip (clean tracked tree via git archive, audited clean-room). Version identity 1.10.2 everywhere. Native-engine default NOT flipped; remaining Qt surfaces honestly listed.

---
Task ID: 1 (v1.10.3 session)
Agent: v1.10.3 engineering session
Task: v1.10.3 — the Qt IPC hardening release: make the Qt CoreClient's
HTTP/WebSocket paths correct under real framing, make the smoke and the
two hosted CI jobs runnable, prove the Windows staging path, pin the IPC
handshake and field contracts, and reconcile the evidence narrative
(register R1–R17 audited against 1d3b1f2 = live main HEAD at session
start)

Work Log:
- Baseline: cloned origin/main at `1d3b1f2` (v1.10.2) — HEAD matched the
  audited baseline commit exactly; every register item re-confirmed by
  reading the committed tree before fixing.
- Environment (hosted container, NOT GitHub Actions, NOT physical
  hardware): Go 1.27.2 (module builds go 1.26 language), Qt 6.8.2 via
  aqtinstall (the CI pin), CMake 4.4.4 + Ninja, Node 24; GL/X11 stacks
  staged from distro debs into a local prefix because the host has no
  root. GitHub Actions API rate-limited from this IP — live Actions
  status could NOT be retrieved; the runnability claims rest on the
  local reproduction + workflow wiring fixes, stated honestly below.

Per-item results (register → outcome → evidence):

- R1 (P0, smoke fail-after-use) — FIXED+VERIFIED. Reproduced first:
  missing-core/no-go printed "fail: command not found" and CONTINUED,
  dying later with "core died at boot". After the fix the same probe
  aborts immediately: "SMOKE FAIL: core binary … missing and no 'go'
  on PATH to build it", exit 1.
- R2 (P2, traps/pkill/WINMGR) — FIXED. EXIT-only cleanup + INT→130 /
  TERM→143 traps; pkill patterns derived from $CORE_BIN/$UI_BIN/$WORK
  plus the repo-tree fallback; bare WINMGR_BIN resolved via
  `command -v`; environ key read retried (bounded); Xvfb/engine/core
  readiness is event-based (socket/conn/log-line polls), fixed sleeps
  removed where replacement was exact.
- R3 (P0, Linux CI deps) — FIXED. libx11-dev + openbox added to the
  apt-get list (closewin builds with `cc -lX11` locally against staged
  headers; G3 refuses WM-less, so openbox is REQUIRED, not optional).
- R4 (P1, wrong staged paths) — FIXED. CORE_BIN and SHEYTAN_CORE_BIN
  now pass the REAL staged core name (SHEYTAN-Local-Agent); sheytan-ui
  is copied to $RUNNER_TEMP/sheytan-ui with an explicit `test -x`
  BEFORE the smoke runs — the silent repo-tree fallback can no longer
  mask the wiring.
- R5 (P1, exact-count flake) — FIXED+VERIFIED. Reproduced with
  TF_BUILD=1 (test failed: "evidence must contain exactly the set
  markers, got [GITHUB_ACTIONS TF_BUILD CI]"); after the fix the same
  env passes (membership asserted; foreign markers exercised in-test).
- R6 (P2, stale comment) — FIXED. readCodeSHA comment now describes the
  SHEYTAN_PERF_CODE_SHA contract. The three fail-closed CLI refusal
  probes were executed (no flag → 1; --host-class bogus → 1; physical
  with CI=1 → 1).
- R7 (P1, silent refreshSystems) — FIXED+VERIFIED. Non-200 now surfaces
  "systems refresh failed (status N)"; regression slot
  systemsRefreshSurfacesTypedError FAILS on the v1.10.2 client, PASSES
  on v1.10.3. refreshSessions/deleteSession got the same treatment.
- R8 (P2, raw id in request line) — FIXED+VERIFIED. Store charset
  verified URL-safe (`default`, `sys-`+lowercase hex); ids are
  percent-encoded defensively (path segments + the activity WS query);
  activateSystemEncodesSegment asserts the encoded wire path.
- R9 (P3, badge/EmptyState/double-refresh) — R9a FIXED (badge visible
  bound to text); R9b (EmptyState centered in ListView contentItem) and
  R9c (onOpened + connectToCore double refresh) are DOCUMENTED ONLY per
  register (harmless; noted in the changelog, no code change).
- R10 (P0, evidence drift) — FIXED+RE-MEASURED. The artifact was
  re-measured on this host (same declared evidence class as the v1.10.2
  record, notes carry the honest host description) with
  SHEYTAN_PERF_CODE_SHA=1d3b1f2… stamped and a trailing newline:
  cold_start 1.339 ms, idle/peak 14.3203125 MB. UPDATE.md quotes the
  artifact character for character; the stale 1.2 ms / 14.8 MB claims
  are corrected in place with a dated correction note.
- R11 (P2, "smallest compatible fix") — REWORDED in changelog.md,
  UPDATE.md, agent.md (upstream fixed in ws 8.20.1 / 8.21.0; 8.22.0
  chosen; dependency unchanged — `npm audit --audit-level=high` clean,
  0 vulnerabilities).
- R12 (RECORD, indent convention) — RECORDED in agent.md
  ("Code formatting convention": space-indented Go by convention, no
  gofmt gate, no mass reformat performed).
- R13 (P2, launcher override order) — FIXED+VERIFIED. Override now wins
  over next-to-core; TestUIExecutableOverrideWinsOverNextToCore added;
  the redundant t.Setenv("")+os.Unsetenv pairs removed.
- R14 (P2, verify-all claim) — FIXED. agent.md now states the CI gates
  are inline in build-desktop.yml and verify-all.mjs is the LOCAL
  canonical entrypoint.
- R15 (P3, this entry) — DONE.
- R16 (P2, Windows acceptance staging) — FIXED. Release/ resolved
  before the flat path; hard throw when neither exists; a post-copy
  existence check; never SilentlyContinue.
- R17 (P0, handshake version) — FIXED+VERIFIED. Name AND version==1
  required before connected; the regression slot
  handshakeRejectsWrongProtocolVersion FAILS on the v1.10.2 client
  ("!core.connected() … a sheytan-ipc/2 core must NOT be accepted" —
  the old client ACCEPTED v2) and PASSES on v1.10.3.

Beyond the register (discovered by executing the new gates):

- CoreClient HTTP framing: the v1.10.2 client had NO Content-Length
  handling — bodies completed only on server close (any response that
  held the socket open hung until the 20 s timeout). Reproduced in the
  before/after QtTest run: 9 of 10 framing slots FAIL on the v1.10.2
  client (including 20-second hangs), all PASS on v1.10.3. Oversize
  heads/bodies refused deterministically; a completed head beyond the
  64 KiB bound is now also refused (only the incomplete case was
  checked before).
- WS policy: fragmentation refused with a typed error (v1.10.2
  silently DROPPED continuation payloads), ping→masked pong,
  close→masked close reply + teardown, and the frame loop no longer
  starves frames queued behind a control frame (the old loop broke on
  ANY empty-payload frame).
- Sessions wire contract: GET /api/sessions serves a BARE array with
  id / title(omitempty) / msgCount — the v1.10.2 client parsed a
  wrapped {"sessions":[…]} envelope and a messageCount field, both
  hidden by the silent skip; the sidebar never loaded its list and
  every session rendered "0 messages". Client + QML fixed; three Go
  field-contract pins added (internal/api/qt_field_contract_test.go).
- Smoke G3/G5 window selection: the Qt host maps a 1x1 helper window
  alongside the real 1280x840 main window; the v1.10.2 `head -1` picked
  the helper and would have failed a green build's gates. Every visible
  window of the UI pid is now checked and G5 closes the window G3
  verified (found by actually running the smoke end-to-end).

Verification evidence (this session, hosted container — classes kept
separate):

- Go: `go build ./...`; FULL `go test ./... -run Test -count=1` PASS;
  `go test ./internal/... -tags headless -count=1` PASS; `go vet ./...`
  clean; `go test -race -tags headless ./internal/api ./internal/agent
  ./internal/sessions ./internal/contextplan ./internal/histref
  ./internal/runtime` PASS; `GOOS=windows go build ./...` +
  `GOOS=windows go vet ./...` clean (final state re-run).
- Qt: cmake configure + build of ui/qt green; QtTest acceptance 14/14
  PASS with the REAL core + fixture engine (offscreen platform): the
  two live-answer slots (chat stream with cumulative live growth while
  Stop is visible → exactly one settle → persistence reload → repeat
  send; AI Systems list/activate/typed-404) plus the ten framing/
  contract slots. Before/after proof recorded above.
- Native: `native/engine` cmake Release build + ctest 13/13 PASS.
- Frontend: npm ci; typecheck clean; oxlint 0 findings; test:units
  236/236; test:release pass; release build + verify:web green;
  npm audit 0 high/critical.
- Linux smoke: scripts/qt-desktop-smoke.sh G1–G5 ALL PASS locally under
  Xvfb + Openbox (staged debs; SMOKE_WORK/display overridden): keyed
  handshake, core-spawned UI pid, REAL visible 1280x840 window, UI
  holding 2 channel sockets + real session create/read through the
  core, real WM_DELETE_WINDOW close, UI exit, core stop, no orphan
  process, no leaked endpoint file. The no-go/missing-core abort path
  re-proven after the fix. The G3 WM-less refusal is in place (no
  GUI claims without a WM).
- Hosted CI: NOT RUN from this session (Actions API rate-limited from
  this IP; no push/PR per the operating rules). The two platform jobs
  are wired to the REAL staged paths and required deps; the first
  hosted run must still prove them.
- Windows Qt hosted gate: NOT VERIFIED (requires the hosted runner);
  the R16 staging path is repaired and compile-gated (GOOS=windows
  build/vet green), which is the maximum provable without the runner.
- Physical hardware: UNVERIFIED (no physical host; the native-engine
  default stays evidence-gated; generation paths remain NOT MEASURED
  without a real model).

Stage Summary:
- v1.10.3 closes R1–R17 (R9b/c documented-only per register), plus the
  framing, WS, and sessions-contract defects the new gates exposed.
- All local gates green at final state; hosted CI and physical-Windows
  remain honestly NOT RUN / NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.3-FINAL.zip (complete tracked
  tree, no .git, no build artifacts).

---

Task ID: 1 (single-agent session)
Agent: Super Z (GLM engineering session, hosted Linux container)
Task: v1.10.4 — turn the two red hosted-CI gates of run 38569081029 into an
honest state (fix what is provably broken, prove everything provable, label
what cannot be proven here), align the docs with the Qt reality, deliver the
clean-room source ZIP under download/.

Work Log:
- INSPECT: cloned live main HEAD = b0b3aba (v1.10.3, exactly the failing
  run's head_sha; clean worktree). Confirmed run 38569081029 jobs via the
  Actions API BEFORE the rate limit hit: audit success; Windows job
  115870797382 failed step 13 (Go verification); Linux job 115870797589
  failed step 18 (Stress suite) while its Go verification (step 15) PASSED;
  publish skipped. Version surfaces read at 1.10.3.
- LOG EXTRACTION BLOCKED: the Actions job-log download API returns 403
  "Must have admin rights to Repository" for anonymous callers; the
  unauthenticated API rate pool was exhausted for the session's egress IPs;
  the legacy checks log endpoint 404s. The exact Windows error line is
  therefore UNOBTAINABLE from this environment (documented, not guessed).
- REPRODUCE (Linux stress, exact CI command): CGO_ENABLED=1
  `go build -trimpath -ldflags="-s -w" -o /tmp/sheytan-stress
  ./scripts/stress-main && /tmp/sheytan-stress stress` → FAIL-BEFORE
  captured: `release_surface FAIL: Linux CI dependencies missing
  "libgtk-4-dev" — Wails v3 needs GTK4 + WebKitGTK 6.0`, 46 pass / 1 fail,
  exit 1. (An earlier run from a foreign CWD also failed on
  `internal/sessions/sessions.go missing` — a CWD artifact of the sandbox,
  not a product defect; the scenario correctly gates repo checks on
  finding .gitignore in the CWD.)
- ROOT CAUSE (Linux): cmd/stress_release_surface.go §3f still REQUIRED the
  Wails-era GTK stack that v1.10.1 removed from the workflow when the
  desktop became the native Qt client. The workflow was correct; the stress
  contract was stale — the "stress scenario assumes a retired transport"
  class the mission predicted.
- FIX (Linux): §3f retargeted to the CURRENT Qt-era contract — REQUIRE
  libx11-dev, libxkbcommon-x11-0, libxcb-{cursor0,icccm4,keysyms1,image0,
  render-util0}, openbox, xvfb, xdotool, x11-utils (11 pins, was 2); BAN
  libgtk-4-dev, libwebkitgtk-6.0-dev, libgtk-3-dev, libwebkit2gtk-4.1-dev
  (4 bans, was 2). No coverage weakened; coverage strengthened.
  PASS-AFTER: exact CI command → 47 pass / 0 fail, exit 0; `-race` clean on
  cmd, sessions, memory, sandbox, tools, agent.
- REPRODUCE (Windows): compile/link PROVEN clean at b0b3aba AND at v1.10.4:
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`, `go vet ./...`
  with AND without `-tags headless` (the exact step-13 environment), plus a
  FULL test-binary LINK check for every package (`go test -run XXXNONE`)
  — zero build failures. The identical three step-13 commands pass on
  Linux CI (step 15 of the failing run) and locally. A user-space Wine
  10.0 attempt (no sudo; deb extraction into a relocated loader layout) was
  made to EXECUTE the Windows test binaries and abandoned within budget —
  wine's wineserver/ntdll lookup cannot be satisfied relocated.
- WINDOWS DISPOSITION: every test file changed since v1.10.1 reviewed
  package-by-package for Windows-runtime portability (api qt field
  contract, desktop launcher resolution, perf host classification, localipc
  endpoint, engcheck PE sniffing, llm import lock, updater variant naming,
  native/engine liveness probe) — no defect found; the failure class is a
  Windows-host runtime assertion that cannot be identified without the
  admin-gated log. v1.10.4 makes NO speculative Windows change and NO test
  weakening; WINDOWS GO VERIFICATION stays NOT VERIFIED until a hosted
  rerun.
- IDENTITY: package.json → 1.10.4; release-version.mjs synced config.go,
  build/config.yml, SIGNATURE; `--check` green.
- DOC TRUTH: README current-release block → v1.10.4; ARCHITECTURE currency
  header → v1.10.4, v1.9.5 loopback activity transport marked RETIRED (the
  Qt desktop mounts NO loopback WS listener — desktop.go), Qt/IPC seam
  marked SHIPPED since v1.10.1; ROADMAP shipped baseline → v1.10.4 with the
  accurate Windows verification label; UPDATE.md retitled with the
  v1.10.4 notes (v1.10.3 kept as parent record); changelog.md v1.10.4 entry
  added (canonical history); agent.md handoff → v1.10.4; stress-main header
  comment no longer describes the removed Wails shell.
- REGRESSION (v1.10.3 wins re-verified): qt_field_contract tests green (in
  the full suite); Main.qml binds modelData.msgCount (the messageCount hit
  is the defect-history comment); smoke script G1–G5 gates + WM requirement
  unchanged; workflow staging pins now ENFORCED by the retargeted
  release_surface scenario; sessions/systems wire contracts green.
- FULL GATES (local, this session): Go — internal headless CGO=0 (64 pkgs
  ok), full `./... -run Test` CGO=0 (0 FAIL), vet CGO=0, internal headless
  CGO=1 (0 FAIL), -race on stress packages, stress exact command 47/47;
  GOOS=windows build + vet(±headless) + full test-binary link (0 build
  failures). Frontend — npm ci, typecheck, oxlint 0/0, test:units 236/236,
  test:release, build, verify:web, embedded index.html, npm audit 0
  vulnerabilities. Native — cmake 3.31.6 (user-space) fresh configure +
  Release build + ctest 13/13 PASS.
- NOT RUN HERE (evidence classes kept separate): Qt host cmake build +
  QtTest acceptance + Linux qt-desktop-smoke.sh G1–G5 (no Qt 6.8/Xvfb/WM in
  this container; ui/qt and scripts/qt-desktop-smoke.sh are UNCHANGED since
  the v1.10.3 session where they passed locally); hosted CI (no push/PR/tag
  per the operating rules); browser E2E (optional, cut for budget).

Stage Summary:
- v1.10.4 = the honest reliability bridge: Linux stress gate FIXED
  (proven), Windows gate evidence recorded (NOT claimed fixed), zero
  surface migration, zero default flips, zero test weakening.
- All local gates green at final state; hosted CI NOT RUN from this
  session; Windows Qt + physical hardware remain NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.4-FINAL.zip (complete tracked
  tree, single top-level SHEYTAN-LA/, no .git, no build artifacts).

---
Task ID: 1 (single-agent session)
Agent: Super Z (GLM engineering session, hosted Linux container)
Task: v1.10.5 — BOTH-PLATFORM GREEN OR NO ZIP. Make the Linux "Build Qt
host (Linux)" gate (run 38584769607 step 22) green with the workflow's
EXACT command, keep every known-good gate green, state the Windows "Go
verification" evidence class honestly, and deliver the clean-room source
ZIP under download/ — or stop with NO ZIP.

Work Log:
- INSPECT: cloned live main HEAD = 354122d (v1.10.4, exactly the failing
  run's head; clean worktree). Full workflow YAML read; the exact failing
  commands extracted: Windows step 13 = CGO_ENABLED=0 `go test
  ./internal/... -tags headless -count=1` + `go test ./... -run Test
  -count=1` + `go vet ./...` (pwsh Invoke-Gate discipline); Linux step 22
  = cmake -S ui/qt -B ui/qt/build -G Ninja -DCMAKE_BUILD_TYPE=Release
  -DCMAKE_PREFIX_PATH=$Qt6_DIR (Qt 6.8.2 via aqtinstall) + cmake --build.
- LOG EXTRACTION BLOCKED (documented, not guessed): the Actions job-log
  API requires admin rights for anonymous callers (403 "Must have admin
  rights"); the web UI gates logs behind sign-in; annotations carry only
  "Process completed with exit code 1". Run HISTORY recovered from the
  public runs/steps pages: v1.10.1, v1.10.2, v1.10.3, v1.10.4 — ALL FOUR
  Windows jobs failed at "Go verification"; the Linux job's Go
  verification PASSED every time (failure moved: v1.10.3 stress → v1.10.4
  Qt host build).
- REPRODUCE (Windows, maximum provable off-Windows): CGO_ENABLED=0
  internal headless 64 packages ok; full ./... -run Test 65 packages ok
  (re-run at the final tree state after every change); CGO_ENABLED=0
  go vet ./... clean; GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
  clean (full type-check incl. test files); GOOS=windows headless build
  clean. NO Windows-host runtime assertion can be identified without the
  admin-gated log; NO speculative Windows change was made.
- REPRODUCE (Linux Qt host, exact CI command): FAIL-BEFORE captured —
  `Could NOT find OpenGL (missing: OPENGL_opengl_LIBRARY
  OPENGL_glx_LIBRARY OPENGL_INCLUDE_DIR)` → WrapOpenGL NOT FOUND →
  Qt6Gui NOT FOUND. Root cause: the workflow installed only the GL
  runtime (libgl1, libegl1); CMake FindOpenGL needs the development
  headers + link-time .so symlinks. FIX: workflow now installs libgl-dev,
  libglx-dev, libopengl-dev, libegl-dev, mesa-common-dev,
  libxkbcommon-dev. PASS-AFTER: exact configure + build exit 0;
  sheytan-ui + sheytan-ui-acceptance produced; ctest found no registered
  tests (the workflow's ctest call is `|| true` non-gating, unchanged).
- REAL PRODUCT DEFECT found & fixed via the acceptance gate: the Qt
  live-answer acceptance failed with liveText() empty while the core log
  showed the run settling done with 20 delta events. A raw RFC6455 probe
  over the IPC unix socket proved the hub fragments every activity event
  > 1024 bytes (gorilla WriteBufferSize: 1024 → fragment(0x1,FIN=0,1024)
  + continuation(0x0,FIN=1)); the v1.10.3 client policy refused all
  fragmentation and closed the stream mid-run. FIX: readWsFrame now
  reassembles fragmented text messages (bounded by kMaxWsFrame; fragment
  state cleared on close); orphan continuations and interleaved messages
  still fail loudly with typed errors. Contract test revised
  wsRefusesFragmentedFrames → wsReassemblesFragmentedMessages (a real
  event split mid-JSON must reach liveText; an orphan continuation must
  still be refused). PASS-AFTER: full acceptance suite 14/14 exit 0
  against the REAL core (chatStreamPersistRepeatAndClose green;
  suite wall-time 30s → 4.7s).
- REGRESSION (all at the final tree state): go test ./internal/...
  -tags headless (CGO=0) exit 0; go test ./... -run Test (CGO=0) exit 0;
  go vet ./... (CGO=0) exit 0; stress suite EXACT CI command exit 0
  (47/47 — the one earlier FAIL was the documented foreign-CWD artifact);
  releasecontract + releasegate green after the version bump; npm ci,
  typecheck, oxlint 0/0, test:units 236/236, test:release, build,
  verify:web all exit 0; embedded frontend regenerated at web/static.
- NOT RUN HERE (evidence classes kept separate): qt-desktop-smoke.sh
  G1–G5 (no Xvfb/WM/xdotool in this container, no root); browser E2E
  (optional, cut for budget); hosted CI (no push/PR/tag per the
  operating rules); physical hardware (no physical host — UNVERIFIED,
  unchanged).
- IDENTITY: package.json → 1.10.5; release-version.mjs synced config.go,
  build/config.yml, SIGNATURE; `--check` green; core binary reports
  SHEYTAN-Local-Agent v1.10.5. Docs aligned: changelog (new v1.10.5
  entry), UPDATE.md (v1.10.5 notes, v1.10.4 kept as parent), README
  current-release, ARCHITECTURE currency header, ROADMAP shipped
  baseline + v1.10.5 STATUS, agent.md handoff.
- Windows `Go verification` disposition: exact step-13 commands GREEN
  locally with the step's exact CGO_ENABLED=0 environment; the hosted
  Windows job remains NOT VERIFIED until a push proves it. Per the ship
  rule this session records: local exact-command green (required for the
  ZIP) + honest hosted-CI status + zero test weakening (one contract
  test REVISED to the RFC-correct behavior, its violation refusals
  intact and re-pinned).

Stage Summary:
- v1.10.5 = the live-stream + Qt-host-build release: the step-22 Linux
  blocker root-caused and fixed at the workflow dependency level, the
  Qt activity-stream fragmentation defect fixed with byte-level probe
  evidence, all local gates green, hosted-Windows honestly NOT VERIFIED.
- Deliverable: download/SHEYTAN-LA-v1.10.5-FINAL.zip (complete tracked
  tree, single top-level SHEYTAN-LA/, no .git, no build artifacts).

---

## v1.10.6 REGISTER RE-VERIFICATION (live HEAD == 8d8e660, main)

Verified against live main HEAD (identical to baseline 8d8e660; no drift):

- W1 VERIFIED — internal/localipc/listener_windows.go:47-75: listenEndpoint() returns with NO pipe instance; first CreateNamedPipe is lazy inside Accept() (line 141). Immediate dial → ERROR_FILE_NOT_FOUND. Unix binds synchronously (listener_unix.go); Windows must too.
- W2 VERIFIED — listener_windows.go:251-257 (connectTimeoutMS=5000 at line 27): Accept returns the 5s connect timeout as a FATAL error → http.Serve exits → Windows IPC endpoint dies after 5 idle seconds.
- W3 VERIFIED — listener_windows.go:189-205: Close() never CancelIoExes a pending overlapped ConnectNamedPipe; a clean Stop can hang on the accept.
- W4 VERIFIED — serve_test.go:95-105 TestCredAllowedDeniesForeignUid is vacuous on Windows (os.Getuid()==-1 degenerates both asserts); verifyClientIsCurrentUser (listener_windows.go:264-295) has zero test coverage; malformed/oversized tests (serve_test.go:262,288) re-dial racing the next pipe instance creation.
- W5 CLASSIFIED BY INSPECTION (HEAD):
  - net.Listen("unix") — serve_test.go:117 only; PathSeparator skip guard present. SAFE.
  - /proc reads — production readers are platform-suffixed (perf_linux.go, governor/cpu_linux.go, proc/alive_unix.go, proc/identity_unix.go, resources_other.go) with honest fallbacks; test reachability guarded (phase5_bench_test.go:177 Skipf; cim_parse_test.go:126 OS=="linux" guard). SAFE.
  - exec of unix tools in tests — git only (releasegate_test.go, repoindex_test.go); git ships on windows-latest. SAFE.
  - internal/sandbox Job-Object tests — NO test files exist in internal/sandbox. NOT-PRESENT.
  - t.TempDir/RemoveAll teardown races — v1.10.0-era internal/api WS class already repaired (cleanup LIFO: srv.Close before TempDir RemoveAll; WS conns closed per-test); spot-checks confirm correct ordering at HEAD. SAFE as-found.
  - CGO_ENABLED=0 (Windows Go-verification env) — localipc is pure golang.org/x/sys/windows; no cgo on this path. NOT the cause.
- L1 CLASSIFIED — Qt Quick scene-graph GL context under Xvfb: software-rendering exports (QT_QUICK_BACKEND=software, QT_OPENGL=software, LIBGL_ALWAYS_SOFTWARE=1) must be set for the UI process. Env reaches the UI by construction: desktop.go:112 passes os.Environ() to the Qt child.
- L2 VERIFIED — scripts/qt-desktop-smoke.sh: UI stdout/stderr inherit the core's streams (desktop.go:118-119 → core.log line 190); $WORK/ui.log is never created, so failure paths at lines 210/273/325 cat an empty file.
- L3 VERIFIED — qt-desktop-smoke.sh:137: openbox readiness is a fixed `sleep 1`, not an event.
- L4 CONFIRMED — workflow smoke step wiring (LD_LIBRARY_PATH, QT_QPA_PLATFORM_PLUGIN_PATH, staged CORE_BIN/UI_BIN, xvfb/openbox/xdotool/x11-utils install) is correct; harden environment + diagnostics only; G1-G5 unchanged.
- P1 PROVEN AT HEAD — package.json:44 "ws": "8.22.0"; zero require("ws")/from "ws" imports tree-wide (hits are a URL-scheme literal src/LogViewer.tsx:90, a Go URL prefix runregistry_test.go:135, a dir name markdown_test.go:119). Frontend uses the browser-native WebSocket.
- P2 PROVEN AT HEAD — e2e/repro-stream.mjs referenced only by worklog.md:589 + changelog.md:2008 (both historical mentions, allowed residue), no workflow step, no package.json script, no verify-all.mjs entry. e2e/fixtures/sheytan-server.ts is shared with spec files and SURVIVES.
- P3 SWEEP AT HEAD — 21 files mention wails: all are (a) gates (verify-all.mjs absence gate, cmd/stress_release_surface.go GTK-stack ban, releasegate_test.go), (b) origin-REJECTION security code (src/loopback-origin.ts, internal/api/ws.go retired-origin handling + their tests/bundle), (c) retired-era comments (desktop.go, headless.go, gen-syso, backend.go, server.go), or (d) historical changelog/worklog/docs mentions. Nothing outside the protected classes; no blind deletion required. internal/llm/backend.go:13 carries a stale architecture comment — corrected to the Qt-client chain in this release.
- D1 VERIFIED — UPDATE.md/agent.md/changelog.md describe gates as fixed/runnable while Windows failed five consecutive hosted runs (through run 38714950052) and the Linux smoke had never run hosted until its first failure. v1.10.6 docs state the run truth with URLs and the failure-visibility mechanism.

OUTCOME REGISTER (v1.10.6 code actions):
- W1-W4: full transport repair in listener_windows.go (synchronous first instance; indefinite accept wait aborted by CancelIoEx on Close; next-instance pre-creation ahead of demand; sidAllowed seam + windows-only tests; TestCredAllowedDeniesForeignUid gets a documented platform split).
- W5: classifications above (no code change justified beyond the transport tests; all candidates SAFE/NOT-PRESENT with the evidence quoted).
- L1/L2/L3: smoke hardening (software-rendering exports, ui.log capture via a smoke-owned exec wrapper around the REAL UI binary, event-based xprop WM-readiness poll).
- CI: failure-visibility install (tee to logs; if:failure steps append failing-test names + log tails to $GITHUB_STEP_SUMMARY and upload artifacts; no secrets/keys in outputs).
- P1/P2: purge executed after the proofs above.
- D1: docs rewritten with run URLs + visibility mechanism; hosted result for v1.10.6 itself is recorded honestly (this environment has no GitHub credentials — push not possible; hosted green for v1.10.6 is NOT claimed).

---

## v1.10.6 IMPLEMENTATION OUTCOMES (evidence per item)

LOCAL EVIDENCE (this environment, Go 1.26.9 / Node 24 / linux amd64):
- W1–W4 transport repair: `CGO_ENABLED=0 GOOS=windows go build ./...` PASS; `CGO_ENABLED=0 GOOS=windows go vet ./...` PASS (compile proof ONLY — never runtime proof). Linux `go build ./...` + `-tags headless` PASS; `go vet ./...` PASS.
- Linux localipc suite unchanged and green: default tags 9/9 PASS (incl. TestCredAllowedDeniesForeignUid platform split, TestStaleEndpointRemovedLiveEndpointKept, TestStopIsCleanAndIdempotent), headless tags PASS.
- Full local regression: `go test ./internal/... -tags headless -count=1` — 71 packages, ALL PASS (api 89.9s, llm 65.9s, agent 20.9s, native/engine 16.4s); `go test ./... -run Test -count=1` — 77 packages, ALL PASS; race gate `-race -tags headless` on api/agent/sessions/contextplan/histref/runtime ALL PASS (CGO_ENABLED=1); stress suite 47 pass / 0 fail / 0 hangs / 0 crashes.
- Frontend: npm ci clean (0 vulnerabilities), typecheck PASS, lint 0 warnings/errors, test:units PASS (fail 0), test:release PASS (fail 0), npm run build PASS, verify-static-assets --dist PASS, verify:web PASS.
- verify-all.mjs: PASS with 2 environment-limited NOT RUNs (native CTest — no cmake binary in this environment; Qt host suites — no Qt6 CMake package).
- Browser E2E: NOT RUN (environment-limited) — the harness honestly refuses without the REAL native engine host; cmake is unavailable here so `native/engine/build/shtn-engine-host` cannot be built locally ("The browser suite proves the REAL stack; it never runs green without it."). Hosted Linux builds the engine before E2E; the suite is unchanged.
- Qt desktop smoke G1–G5: NOT RUN locally (no Xvfb WM/xdotool/openbox/Qt 6.8.2 in this environment); the script is bash -n validated and its environment hardening (L1–L3) is the hosted-run fix. qt-desktop-smoke.sh syntax PASS.
- Windows-only tests (TestSIDAllowed*, TestServeSurvivesIdleBeyondFormerConnectTimeout, TestBurstDialsNeverRaceInstanceCreation, TestStopDoesNotHangOnPendingAccept): compile-verified via GOOS=windows vet/build; they EXECUTE in the hosted Windows job — runtime proof belongs to that run, per the evidence-class rule.

PURGE OUTCOMES:
- P1 ws removed from package.json + package-lock.json regenerated (npm install --package-lock-only; zero "ws" entries; npm ci clean; 0 vulnerabilities).
- P2 e2e/repro-stream.mjs deleted (git rm) after the zero-reference proof at HEAD; e2e/fixtures/sheytan-server.ts untouched (shared fixture).
- P3 no deletions: all remaining wails mentions classified gates/security-rejection/retired-era-comments/protected-historical (see register above). internal/llm/backend.go stale architecture comment corrected. ui/qt/CMakeLists.txt project version aligned 1.10.3 → 1.10.6 (no gate reads it; identity consistency).

IDENTITY: package.json 1.10.6 → release-version.mjs repaired config.go AppVersion, build/config.yml productVersion, SIGNATURE first line; `--check` PASS ("release metadata consistent").

HOSTED (not executable from this environment — no GitHub credentials):
- Push to main: NOT POSSIBLE from the repair environment (credential-less). The failure-visibility install ships in this tree so the next hosted run publishes its own diagnosis to $GITHUB_STEP_SUMMARY + artifacts.
- No hosted green is claimed for v1.10.6. The gate table in the final report marks hosted rows accordingly.

---

## v1.10.7 — 2026-10-16 — the evidence-driven gate repair + CI Visibility 2.0 + repository shape

Base: main @ 48cee27 (v1.10.6) — verified LIVE HEAD at cycle start (the
baseline commit IS the hosted HEAD; the register applied verbatim).

### Evidence boundary (Phase 1, recorded per the operating rules)

Run 38731387959 (v1.10.6, commit 48cee27):
https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38731387959

Captured VERBATIM from the public run page (HTTP 200, server-rendered
HTML, fetched without credentials):
- Jobs: "Source & frontend audit" SUCCEEDED in 3m7s; "Windows x64"
  FAILED in 8m18s — failing step "Go verification" (step 13, output
  line 138, per the page's own anchor `job/116411076184#step:13:138`);
  "Linux x64" FAILED in 9m22s — failing step "Qt desktop acceptance
  smoke (real core + Qt UI under Xvfb)" (step 23, output line 55, anchor
  `job/116411076192#step:23:55`); "Publish and verify
  ${{ github.ref_name }}" present and skipped (never ran).
- Check-run annotations, read anonymously (the V-A capability, re-proven
  this cycle): the Windows job carries exactly one error annotation —
  step "Go verification", text "Process completed with exit code 1.";
  the Linux job exactly one — step "Qt desktop acceptance smoke (real
  core + Qt UI under Xvfb)", text "Process completed with exit code 1.";
  the audit job carries none. BARE EXIT CODES ONLY: no failing test
  names, no gate names, anywhere in the anonymously readable channel.
- Artifacts: `windows-go-verification-logs-attempt1` (1.53 KB,
  sha256:61e66aa60966fded2b8ef0f58ada2b57b9c135c7408cfd67de666eb074d3384a)
  and `linux-verification-logs-attempt1` (3.11 KB) EXIST but the
  artifact download endpoints are auth-walled (HTTP 404 anonymous) —
  their contents are NOT quotable from this environment.
- CHANNELS EXHAUSTED: the GitHub REST API is anonymously rate-limited
  from this shared egress IP (first call over quota), artifact download
  requires authentication, the HTML run/job pages carry job/step
  metadata and annotations but not log text (logs render via
  authenticated XHR), and the operator did not paste the failure-summary
  text with the prompt. Per the mission's boundary rule, every fix
  below is labeled EVIDENCE (locally reproduced or read-verified) or
  INFERENCE (register-ranked candidate), and nothing outside a named or
  verified cause was changed.

### Per-item register outcomes

- **W-A (boundary, resolved in Phase 1)**: RESOLVED AS BOUNDARY. The
  failing Windows command/test names exist only inside the auth-walled
  artifacts; the anonymously readable channel names only the failing
  STEP. Evidence classes recorded per fix below; no failing test name
  was guessed or claimed.
- **W-B (VERIFIED hygiene)**: EXCEEDED THE REGISTER. `gofmt -l .` at
  HEAD listed **143 files** (the register named three:
  internal/llm/backend.go, internal/localipc/serve_test.go,
  internal/localipc/listener_windows_test.go — all confirmed members).
  The mangling is the tab-to-space editing-pipeline damage mode,
  tree-wide. FIX: `gofmt -w .` (whitespace-only diff, both platform
  builds verified byte-equivalent), and a REQUIRED gofmt gate added to
  the Go verification step on BOTH platforms (empty `gofmt -l .` or
  fail; full offending list teed to win-gofmt.log / linux-gofmt.log,
  first files emitted as ::error:: annotations). Local proof: gofmt -l
  empty; builds+vet green on linux/headless/windows before and after.
- **W-C (candidates → root cause)**: THE WINDOWS ROOT CAUSE IS
  READ-VERIFIED IN SOURCE, PRESENT SINCE v1.10.1. `dialEndpoint`
  (internal/localipc/listener_windows.go) opened the CLIENT pipe handle
  with dwFlagsAndAttributes = 0 — a SYNCHRONOUS handle — and handed it
  to the same pipeConn whose Read/Write issue OVERLAPPED IO (Overlapped
  structs + events + GetOverlappedResult) on every connection. Issuing
  overlapped IO on a handle not opened with FILE_FLAG_OVERLAPPED is
  outside the Windows contract (MSDN CreateFile: "the results are
  unpredictable"); the canonical pipe client (x/sys winio DialPipe)
  opens with FILE_FLAG_OVERLAPPED. Verified present at cb11a23 via
  `git show cb11a23:internal/localipc/listener_windows.go` — matching
  "the failure predates v1.10.6 by five runs". EVERY Windows pipe
  round-trip in the hosted gate traverses this path (handshake, tunnel,
  health, malformed re-dial, 2 MiB oversized-header write, burst dials,
  idle survival); the Linux transport (net.Listen UDS) never touches it —
  matching the six-run Windows-only split. FIX:
  `windows.FILE_FLAG_OVERLAPPED` on the client CreateFile. Evidence
  class: EVIDENCE (read-verified defect, git-history-verified age,
  first-hand-informed) + INFERENCE label re: which exact test names the
  run names (channel boundary). A SECOND read-verified defect in the
  same file was fixed at the same time: SetReadDeadline only cancelled
  in-flight reads when the deadline was ALREADY expired (a future
  deadline never fired during a pending ReadFile — http.Server's
  ReadHeaderTimeout: 10s was dead code on Windows); a future deadline
  now arms a bounded time.AfterFunc CancelIoEx, and ERROR_OPERATION_
  ABORTED reads answer os.ErrDeadlineExceeded when the deadline passed.
  Windows-only regression proof remains the hosted job's to produce
  (compile proof here: GOOS=windows build + vet green).
- **L-A (boundary, resolved in Phase 1)**: RESOLVED AS BOUNDARY +
  FIRST-HAND REPRODUCTION. The failing gate identity (which of G1–G5)
  lives in the auth-walled artifacts; the anonymously readable channel
  names only the step. The reproduction below converts the L-C ranking
  into execution evidence.
- **L-B (VERIFIED sound, do not re-do)**: CONFIRMED BY EXECUTION. The
  v1.10.6 hardening (software-rendering exports in step + script,
  wrapper execing the real UI binary with captured output, event-based
  WM readiness poll) passed the full G1–G5 battery first-hand on
  Qt 6.8.2 — the exact minor CI pins. Not re-done; not touched.
- **L-C (candidates → first-hand evidence)**: THE LINE-55 FAILURE SHAPE
  REPRODUCED FIRST-HAND. Environment: Debian trixie, aqt Qt 6.8.2
  linux_gcc_64 (SAME minor as CI), Xvfb :77, user-space openbox +
  xdotool + x11-utils (extracted debs), software rendering, the REAL
  v1.10.6 smoke script and REAL core. Run 1 (without the xcb plugin
  dependency set): `qt.qpa.plugin: From 6.5.0, xcb-cursor0 or
  libxcb-cursor0 is needed to load the Qt xcb platform plugin.` /
  `qt.qpa.plugin: Could not load the Qt platform plugin "xcb" in
  ".../plugins/platforms" even though it was found.` / `This application
  failed to start because no Qt platform plugin could be initialized.` →
  UI dead at boot → core exits with its child → `SMOKE FAIL: core died
  at boot` after the core.log + ui.log dump — the step-23 line-55
  signature (a UI boot death dumps ~19+ lines before the FAIL line).
  This is register L-C candidate (a), the QML/plugin load class, with
  the ui.log showing it exactly as L-C predicted. Run 2 (with the full
  xcb dependency set present): **ALL GATES PASS** — G1 PASS (typed
  keyed handshake, sheytan-ipc/1), G2 PASS (core-spawned Qt host alive),
  G3 PASS (visible top-level window, 1280x840, openbox-owned display),
  G4 PASS (real core interaction, session round-trip; UI holds 2 channel
  sockets), G5 PASS (UI exited after the WM_DELETE_WINDOW close; no
  orphan, no leak). Run 3 (post-reshape, core built by the smoke's own
  fallback from cmd/sheytan-local-agent/): **ALL GATES PASS** again.
  FIX: the workflow's apt step now pins the COMPLETE canonical xcb set
  (libxcb-cursor0/icccm4/keysyms1/image0/render-util0/render0/shape0/
  randr0/shm0/xfixes0/xinerama0/xinput0/xkb1/sync1/dri3-0 +
  libxkbcommon-x11-0 + libxkbcommon0) so the plugin dependency closure
  is deterministic rather than runner-image luck. G1–G5 untouched.
  Evidence class: EVIDENCE (first-hand reproduction on the CI Qt
  minor); the exact CI-side ui.log remains inside the auth-walled
  artifact, so if the hosted cause differs, the new ::error::
  annotations will name it anonymously.
- **V-A (VERIFIED capability)**: RE-PROVEN AND USED. The check-run
  annotations API is anonymously readable (this cycle: run
  38731387959's annotations, no login). VISIBILITY 2.0 INSTALLED: the
  audit, Windows and Linux failure-summary steps now emit ::error::
  workflow-command annotations (Windows: failing command + first
  failing test names from each teed log; Linux: the failing gate from
  the smoke's new smoke-fail.txt marker — written on EVERY abort path,
  best-effort — plus bounded ui.log/core.log tails; audit: failing
  gates + first failing test names). Caps: ≤10 per step (GitHub's
  per-step annotation budget), 250-char truncation, no secrets (rule 7:
  the same lines already publish to the public step summary; the key
  never enters any log). Job-summary and artifact channels unchanged.
- **R-A (docs consolidation)**: DONE. git mv of all seven documents
  (ARCHITECTURE.md, CONTRIBUTING.md, ROADMAP.md, UPDATE.md, agent.md,
  changelog.md, worklog.md) into docs/. Root visible documentation is
  now exactly README.md, LICENSE.md, SECURITY.md.
- **R-B (core entry + launcher)**: DONE. git mv of the five main*.go
  files into cmd/sheytan-local-agent/ (build-tag suffix contracts
  preserved and re-verified: linux, linux+headless, windows builds all
  green; the GOOS-suffixed files keep their constraints inside the new
  directory); SHEYTAN-LA.bat → packaging/. Same-change-set reference
  sweep: workflow Windows build (SHEYTAN-LA.exe + console smoke probe →
  ./cmd/sheytan-local-agent), Linux build, headless smoke build, smoke
  core fallback build; Windows staging step (docs/agent.md →
  AI-CONTEXT.md, packaging/SHEYTAN-LA.bat → staged launcher); audit
  job's repository-shape check now TWO-SIDED (new paths exist + old
  root paths gone); stress release_surface pins the same two-sided
  inventory + the new workflow build strings ("./cmd/sheytan-local-agent",
  "packaging/SHEYTAN-LA.bat"); license classification table updated in
  internal/brand/brand.go and LICENSE.md regenerated via
  scripts/gen-license.go (sync contract test green); license contract
  test's allowed-references map repointed to docs/UPDATE.md and
  docs/worklog.md; README.md document references → docs/*; docs
  back-links to root-staying files → ../README.md etc.; native/engine
  README + shtn/types.h references → docs/*. The runtime agent.md
  handoff (internal/agent/handoff.go HandoffFileName) writes the USER's
  workspace file and is NOT the repository document — untouched.
- **R-C (protected keep-list, per-file justification)**:
  * go.mod / go.sum — the Go module contract; the toolchain resolves
    every package from these two root files. Moving them breaks every
    go command (GO111MODULE root discovery).
  * package.json / package-lock.json — the npm contract (scripts,
    dependency graph identity); npm ci/install and the audit's identity
    resolution read them from the root by contract.
  * index.html — Vite's canonical entry; vite.config.ts build input
    resolution starts here.
  * vite.config.ts — the frontend build contract (root/index.html
    discovery, outDir web/static, plugins); relocating forces a config
    refactor beyond this release.
  * tsconfig.json / tsconfig.app.json / tsconfig.node.json — the
    TypeScript project-references root (typecheck walks app+node from
    the root solution file); relocation breaks the typecheck contract.
  * SIGNATURE — the release-identity artifact read by
    release-version.mjs --check and the release contract at the root
    path; relocating would change the identity contract.
  * .gitignore — must be at the repository root to function (git only
    reads it there).
  * .oxlintrc.json / .prettierignore — linter/formatter configs
    discovered at the root by their tools (oxlint/prettier walk-up
    contract); relocating forces config rewrites beyond this release.
- **R-D (reference sweep)**: DONE — enumerated under R-B; plus
  scripts/release-version.mjs surfaceTargets (config.go, build/config.yml,
  SIGNATURE — all root-staying, unchanged semantics), codename-gate.mjs
  (walks the whole tree; docs/ is scanned, no exclusion needed),
  verify-all.mjs (suite runner, no path pins), e2e/ (no path pins to
  moved files — verified by grep). New stress-gate assertions added
  following the gate's two-sided philosophy (see R-B).
- **D1 (docs truth)**: DONE. changelog.md carries the v1.10.7 entry
  quoting the consumed evidence boundary (annotation texts, artifact
  names + digests, job/step/line positions, run URLs) and the full
  local battery; UPDATE.md is the v1.10.7 release notes with the
  v1.10.7 hosted-truth block (run status quoted; v1.10.7's OWN hosted
  result explicitly deferred to the post-push run — no green claimed);
  agent.md is the v1.10.7 handoff with the same run truth; this
  worklog carries the per-item outcomes with evidence.

### Phase 7 local regression battery (all green)

- gofmt -l . → 0 files.
- go build ./... ; go build -tags headless ./... ; GOOS=windows
  CGO_ENABLED=0 go build ./... — ALL PASS (darwin is broken at BASELINE
  48cee27 — unix.SO_PASSCRED in listener_unix.go — pre-existing, not a
  CI target, untouched per the no-fix-without-cause rule).
- go vet ./... and GOOS=windows go vet ./... — PASS.
- go test ./internal/... -tags headless -count=1 — ALL PASS.
- go test ./... -run Test -count=1 — ALL PASS.
- go test -race -tags headless on ./internal/api ./internal/agent
  ./internal/sessions ./internal/contextplan ./internal/histref
  ./internal/runtime (the audit job's exact set, CGO_ENABLED=1) — PASS.
- npm ci (0 vulnerabilities); typecheck; lint (0 warnings, 0 errors);
  test:units (236 pass / 0 fail); test:release (pass, 0 fail);
  npm run build; verify-static-assets --dist PASS.
- scripts/verify-all.mjs --go — full PASS (all suites, zero NOT RUN).
- Stress suite: 47 pass / 0 fail / 0 hangs / 0 crashes — including the
  reshaped release_surface (new two-sided inventory + workflow strings).
- bash -n scripts/qt-desktop-smoke.sh PASS; workflow YAML parse PASS.
- Qt desktop acceptance smoke: G1–G5 first-hand PASS twice (pre-reshape
  and post-reshape, the latter with the smoke's own fallback build from
  cmd/sheytan-local-agent/), on aqt Qt 6.8.2 — CI's exact minor.
- Canonical identity: package.json 1.10.7 → release-version.mjs sync
  (config.go, build/config.yml, SIGNATURE repaired) → --check PASS.

### Push boundary

This environment cannot push (no credentials). The commit is prepared
for the OPERATOR to push to main. The hosted run (~13 min) is the
authority for hosted truth: with VISIBILITY 2.0 in place, any red step
in that run publishes anonymously machine-readable ::error:: annotations
naming the failing command/tests/gate — the fix-forward loop no longer
needs credentials to read a failure. No hosted green is claimed for
v1.10.7 anywhere in this repository.

## v1.10.8 — 2026-10-16 — the line-ending policy + E2E path repair + CI Visibility 3.0

Baseline: live main HEAD == 22049b9 (v1.10.7) — verified at session start
(`git rev-parse HEAD` == 22049b99f9dc0f5dbeadc47738c003a780180577; no drift
past the baseline commit, so the register was audited directly against HEAD).
Consumed failure: run
https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38823624040
(v1.10.7) — Windows x64 step 13 "Go verification" failed at the gofmt gate
(the gate lists files alphabetically; the eight annotated cmd/ files are the
first eight alphabetically in the tree — a listing artifact, not eight
violations); Linux x64 step 17 "Browser E2E" failed with a bare exit code 1
before Playwright started; the smoke summary step never executed (skipped —
the smoke never started, yet its failure-summary logic was written to blame
a missing gate marker if it ever ran with no work directory: the false-lead
class). Both root causes were reproduced LOCALLY in this cycle before any
fix (Phase-2 transcripts below); the hosted annotations/summaries named the
steps, the local reproduction named the mechanisms.

### Phase 2 — reproduction transcripts (BEFORE any fix)

* E-A reproduced exactly: from the repository root,
  `go build -tags headless -o e2e/.build/sheytan-e2e-server .`
  → EXIT CODE 1, stderr: `no Go files in /home/z/sheytan-clone`
  (the v1.10.7 reshape emptied the root package; build:e2e still
  targeted it). Toolchain: go1.26.3 linux/amd64 (go.mod requires go 1.26).
* W-A mechanism verified on the committed tree: `gofmt -l .` → EMPTY
  (exit 0) — the tree gofmt observes locally is clean, matching the
  Linux hosted gate on the same commit; zero CR bytes in cmd/*.go
  (`rg -c $'\r'` over every cmd/*.go → no matches); `.gitattributes`
  ABSENT (`ls .gitattributes` → No such file); `git check-attr text eol
  -- cmd/root.go cmd/ask.go` → `text: unspecified, eol: unspecified` for
  both. Index census: `git ls-files --eol -- '*.go'` → all 548 files
  `i/lf w/lf` (the policy pin therefore causes ZERO renormalization).
  The external mechanism is primary-documented: windows-latest images
  default `core.autocrlf=true` (the checkout materializes every text
  file CRLF), and gofmt normalizes CRLF→LF (a CRLF tree is never clean).
* W-A failure mode REPRODUCED mechanically: CRLF injected into a
  working-tree cmd/root.go copy → `gofmt -l .` names exactly
  `cmd/root.go`; file restored → `gofmt -l .` empty again. This is the
  exact Windows-hosted failure shape, demonstrated locally.
* R-A sweep (`rg -n "go build -o sheytan \\.|go build \\. |go run \\.|go build -o sheytan "` +
  a broader anchored pattern over the tree, .git excluded): the ONLY
  stale root-package build/run reference outside package.json's
  build:e2e is scripts/verify-all.mjs:169 — the skip hint
  `core executable not built — run \`go build -o sheytan .\` first` —
  with SHEYTAN_CORE_BIN pointing at ROOT/sheytan (nothing produces it:
  `go build ./...` compiles without writing binaries). No other hit.
  package.json:16 named in the same sweep (E-A's exact defect).

### Register outcomes (evidence per item)

* **W-A — CLOSED (code diff: new .gitattributes + workflow Windows-job
  checkout pin).**
  - NEW `.gitattributes` (scoped, justified per line in the file
    header): `*.go text eol=lf` (all 548 .go files already i/lf — zero
    renormalization; gofmt on every platform now observes the same LF
    tree) + `*.bat text eol=crlf` (the ONE justified additional line:
    packaging/SHEYTAN-LA.bat uses goto-label control flow; the new
    checkout pin would otherwise have silently changed its historical
    CRLF materialization to LF; the index copy stays LF exactly as
    committed). No blanket forcing — installer sources (packaging/nsis)
    and everything else keep repository-native endings.
  - Workflow build-windows job: NEW first step "Pin checkout to LF
    (line-ending policy, W-A)" — `git config --global core.autocrlf
    false` + assertion the pin took — placed BEFORE Checkout (git
    exists on the runner image pre-checkout). The gofmt gate block is
    byte-for-byte UNTOUCHED (fix the environment, never the gate).
  - Local verification: `git check-attr text eol -- cmd/root.go
    cmd/ask.go packaging/SHEYTAN-LA.bat` → text: set/eol: lf (both .go)
    and eol: crlf (.bat); `git status` shows NO renormalization drift
    (only the intended edits).
* **E-A — CLOSED (code diff: package.json build:e2e target).**
  `build:e2e` now runs `go build -tags headless -o
  e2e/.build/sheytan-e2e-server ./cmd/sheytan-local-agent`. Proof
  AFTER: the exact command exits 0 (23,149,084-byte server); the FULL
  `npm run build:e2e` (tsc -b && vite build && sync:web && go build)
  exits 0; and the complete `npm run test:e2e` passes 40/40 Playwright
  specs in 4.4m against the REAL stack (built frontend, headless server
  from the new entrypoint, real C++ engine via the fixture model, real
  HTTP+WebSocket, real session lifecycle). No second defect surfaced
  behind the build path.
* **V-B — CLOSED (code diff: workflow E2E tee + new failure-summary
  step).** The Browser E2E step tees `npm run build:e2e` →
  `ci-logs/e2e-build.log` and `npm run test:e2e` →
  `ci-logs/e2e-playwright.log` under `set -euo pipefail` (a failing
  tee'd command still fails the step); NEW step "E2E failure summary
  (Linux)" (if:failure()) appends failing-spec grep, the playwright
  verdict line and 200-line tails to $GITHUB_STEP_SUMMARY, emits capped
  ::error:: annotations (spec names first, then error lines; the cap
  counter lives in the parent shell via process substitution — the
  pipe-fed-while subshell class is explicitly avoided), reports a
  missing Playwright log as "failed BEFORE Playwright ran — the
  build:e2e stage is the failing gate", and lets the existing
  Upload verification logs step carry both logs (path ci-logs/*.log).
  Validated: workflow YAML parses; `bash -n` on the E2E step, the
  summary step, the smoke summary step and the stress step — all OK;
  `node --check scripts/verify-all.mjs` OK.
* **V-A — CLOSED (code diff: workflow smoke-summary discrimination).**
  "Qt smoke failure summary (Linux)" now checks the smoke work
  directory FIRST: `[ ! -d "$work" ]` → emits
  `::error::The Qt smoke never started — the failure is upstream of the
  smoke` + the same statement in the job summary, and exits 0 (skips
  the gate-marker logic and log tails entirely). With the directory
  present, the v1.10.7 behavior (gate marker + ui.log/core.log tails +
  the "failed before a gate marker was written" fallback) is preserved
  byte-for-byte — that fallback is now only reachable when the smoke
  actually STARTED, ending the false-lead class.
* **R-A — CLOSED (code diff: verify-all.mjs Qt block + .gitignore).**
  The Qt acceptance block now builds the core EXPLICITLY as a gated
  runner step (`go build -o <coreBin> ./cmd/sheytan-local-agent`,
  skipped only when the go toolchain is absent), points
  `SHEYTAN_CORE_BIN` at the produced binary, and the skip hint teaches
  `go build -o sheytan ./cmd/sheytan-local-agent`. `.gitignore` gains
  anchored `/sheytan` (exact filename, cannot swallow source; the
  releasegate no-swallow test still passes — verified in the full go
  test run). Direct proof: the repaired command builds and the binary
  reports `SHEYTAN-Local-Agent v1.10.8` (after the identity bump).
  Sweep proof re-recorded: no other stale reference exists tree-wide.
* **R-B — SOUND, NOT RE-DONE.** The workflow's build steps, the
  headless surface smoke, the NSIS staging, the ZIP-entry checks, the
  audit repository-shape contract and the stress release-surface gate
  already pointed at the new layout; the Phase-2 sweep found no
  additional stale references; the shape contract was re-verified
  locally (both-sided assertions pass).
* **W-B — BOUNDARY (resolves on the next hosted run, per rule).**
  The Windows go test suite (including W1–W4 + W-C) has NEVER run
  hosted since v1.10.1. internal/localipc/listener_windows.go and the
  W1–W4/W-C repairs are UNTOUCHED this cycle (zero diffs — churning
  them without hosted evidence is an automatic failure). The next
  hosted run is their first hosted proof; if its annotations name
  failing Windows tests, each is fixed at its named root cause with
  quoted assertion output.
* **D1 — CLOSED (docs + identity).** changelog.md v1.10.8 entry quotes
  the run-38823624040 evidence and both reproduction transcripts;
  UPDATE.md retitled to v1.10.8 with the new "What v1.10.8 changes"
  section and the v1.10.8 hosted-truth paragraph (v1.10.7 demoted to
  parent record); agent.md handoff updated (v1.10.8 current-release
  paragraph; the STALE pre-v1.10.7 "CI does NOT gate gofmt" formatting
  claim corrected — gofmt IS gated on both platforms; two stale
  identity claims fixed: "exactly 1.10.3" and "exactly 1.10.2" →
  1.10.8); README current-release line → v1.10.8; canonical identity
  1.10.8 via `node scripts/release-version.mjs` (package.json →
  config.go AppVersion, build/config.yml productVersion, SIGNATURE
  first line repaired) and `--check` PASS.

### Full local regression gate table (this environment: linux/amd64, go1.26.3, node 24.21.0, cmake 4.4.4, Qt6 ABSENT)

| Gate | Command | Result |
|---|---|---|
| npm ci | `npm ci` | PASS (0 vulnerabilities) |
| typecheck | `npm run typecheck` | PASS (exit 0) |
| lint | `npm run lint` | PASS (0 warnings, 0 errors, 111 files) |
| frontend units | `npm run test:units` | PASS (236 pass / 0 fail) |
| release units | `npm run test:release` | PASS (38 pass / 0 fail) |
| frontend build | `npm run build` | PASS (embedded at web/static) |
| web verify | `npm run verify:web` | PASS (stable-asset contract) |
| gofmt | `gofmt -l .` | PASS (empty) |
| go vet | `go vet ./...` | PASS (exit 0) |
| go test internal headless | `go test ./internal/... -tags headless -count=1` | PASS (exit 0) |
| go test full | `go test ./... -run Test -count=1` | PASS (exit 0) |
| go race | `go test -race -tags headless -count=1 ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime` | PASS (api 101.1s, agent 25.1s, others green) |
| windows cross | `GOOS=windows CGO_ENABLED=0 go build ./...` + `go vet` | PASS (both exit 0) |
| stress gate | `go build ./scripts/stress-main` + `stress` | PASS (47 pass / 0 fail / 0 hangs / 0 crashes) |
| native engine CTest | cmake configure+build + `ctest --test-dir native/engine/build` | PASS (13/13) |
| E2E build (E-A proof) | `npm run build:e2e` | PASS (exit 0; exact command exit 0) |
| E2E suite (real stack) | `npm run test:e2e` | PASS (40/40 specs, 4.4m) |
| verify-all runner | `node scripts/verify-all.mjs` | PASS (10 PASS; native+Qt NOT RUN — cmake/Qt6 absent at that run) |
| smoke script syntax | `bash -n scripts/qt-desktop-smoke.sh` | PASS |
| workflow validation | YAML parse + bash -n touched steps + pin-contract checks | PASS (4/4) |
| repository shape | audit-job assertions mirrored locally | PASS (both-sided) |
| version identity | `node scripts/release-version.mjs --check` | PASS (1.10.8 consistent) |
| Qt host suites | cmake + Qt6 (aqt) + Xvfb smoke | NOT RUN (Qt6 toolchain absent in this environment — environment-limited, honest skip; the workflow provisions Qt 6.8.2 itself) |
| Hosted run | the operator's push | NOT RUN (this environment cannot push; no hosted green is claimed for v1.10.8) |

### Push boundary

This environment cannot push (no credentials). The commit is prepared for
the OPERATOR to push to main. The next hosted run is the FIRST-EVER hosted
execution of: the repaired Windows transport (W1–W4 + W-C), the
line-ending policy at the Windows checkout, the E2E at the new layout, and
the reshaped stress gate. With Visibility 3.0 in place, any red step —
including the Browser E2E — now publishes anonymously machine-readable
::error:: annotations and ci-logs artifacts, and the smoke summary
distinguishes never-started from died-at-gate. Fix forward ONLY on what
that run's evidence names. No hosted green is claimed for v1.10.8 anywhere
in this repository.

---

# v1.10.9 — SMOKE PATH CANONICALIZATION + EVIDENCE-NAMED WINDOWS TRANSPORT FIX

## Phase 1 — LIVE STATE + EVIDENCE

### Live HEAD verification

- Live main HEAD at inspection: `a1041fe2ef1866eab0177b42866fbf78428753de`
  (`git log`: a1041fe v1.10.8 ← 22049b9 v1.10.7 ← 48cee27 v1.10.6 ← …).
- HEAD **equals** the audit baseline `a1041fe`: zero drift; every register
  item below re-verified against the exact audited tree. 783 tracked files.
- Baseline failing run: `https://github.com/SHEYTANx13/SHEYTAN-LA/actions/runs/38871731277`
  (v1.10.8; Windows go test red on exactly one test, Linux step 24 smoke red,
  E2E + stress + both gofmt gates green).

### Evidence-class boundary (recorded BEFORE any fix was chosen)

The operator-pasted `windows-go-verification-logs-attempt1` content (1.3 KB,
the exact failing assertion line of TestServeMalformedRequestIsRejectedNotCrashing)
was **NOT available** with this cycle's prompt: the mission text references
the paste but no artifact content accompanied it. The anonymous annotations
API fetch for run 38871731277 was also attempted and is NOT readable from
this environment:

```
$ curl -s https://api.github.com/repos/SHEYTANx13/SHEYTAN-LA/actions/runs/38871731277
{"message":"API rate limit exceeded for <egress-ip>. ..."}
```

Per the mission's own rule, the fix selection therefore proceeds from the
register's ranked candidates with every choice labeled **INFERENCE**, and
the hosted run remains the only runtime authority for the Windows transport.

### Quoted evidence (from the register, which quotes the run's annotations)

1. Windows step 14 (`win-go-test-headless`, package `internal/localipc`):

   ```
   --- FAIL: TestServeMalformedRequestIsRejectedNotCrashing (0.06s)
   ```

   Every other test in the package PASSED hosted, including
   TestServeOversizedRequestHeadersRejected — the differential isolates the
   garbage request line and the close-without-read (see Phase 2).

2. Linux step 24 (Qt smoke), the decisive annotation lines:

   ```
   SMOKE FAIL: core died at boot
   bash: dist/linux/app/SHEYTAN-Local-Agent: No such file or directory
   ```

   (the bash ENOENT line names the RELATIVE path executed after the cd —
   the line-258 exec of `$CORE_BIN` from the changed working directory).

3. Artifact name confirmed in the workflow source:
   `windows-go-verification-logs-attempt${{ github.run_attempt }}`
   (build-desktop.yml:778) — attempt 1 is the artifact the mission names.

### Register re-verification against live HEAD (file:line at a1041fe)

| Item | Status | Live proof |
|------|--------|------------|
| L-A | VERIFIED | build-desktop.yml:2107 passes `CORE_BIN="dist/linux/app/${LINUX_PKG_ROOT}/SHEYTAN-Local-Agent"` (RELATIVE; `LINUX_PKG_ROOT: "SHEYTAN-Local-Agent"` at yml:83); workflow pre-check `test -x` (yml:2103) and the script's `[ ! -x "$CORE_BIN" ]` fallback block (qt-desktop-smoke.sh:82–90) both run from the repo root and PASS; `cd "$WORK"` at sh:94; `"$CORE_BIN"` exec'd at sh:258–261 from $WORK → bash ENOENT → `fail "core died at boot"` (sh:267). UI_BIN (yml:2108) and SMOKE_WORK (yml:2105) are absolute ($RUNNER_TEMP) in the hosted run. |
| W-A | VERIFIED (test + candidates) | serve_test.go:269–293: dial → SetDeadline(5s) → write `NOT-HTTP AT ALL\r\n\r\n` → **Close WITHOUT reading** → redial → GET /api/health. The failing assertion line itself is host-published only in the unavailable paste — candidate selection is INFERENCE (C1 primary, see Phase 2). listener_windows.go:288 classifies ONLY `windows.ERROR_NO_DATA` as transient on the accept path; ERROR_BROKEN_PIPE / ERROR_PIPE_NOT_CONNECTED from the overlapped connect completion (GetOverlappedResult at :385) propagate to `return nil, err` → Accept dies → http.Serve returns → endpoint dead → the test's health redial fails instantly (0.06s class). |
| W-B | VERIFIED (latent) | listener_windows.go:661–690 SetReadDeadline arms `time.AfterFunc(t.Sub(now), CancelIoEx(handle, nil))` for future deadlines; pipeConn.Close (:638–647) stops NO timer and clears NO deadline — a stale timer fires on a recycled handle value seconds later and aborts an unrelated live connection's IO. Fix regardless of W-A's outcome. |
| V-A | VERIFIED | build-desktop.yml:719 "Go verification failure summary (Windows)"; the failing-lines filters at yml:737 and yml:754 match only `^(--- FAIL|FAIL|panic:|fatal error:)` — Go assertion lines (`serve_test.go:287: ...`) never match, so run 38871731277's annotations named the test but not its statement. |
| U-A | OPEN (by design) | First-time surfaces consumed as evidence-only after L-A/W-A land: smoke gates G1–G5 (core never booted hosted), Windows steps 16–32, Linux steps 26–33, the publish job. No prediction; fix forward on named evidence only. |

## Phase 2 — ROOT-CAUSE CLASSIFICATION

### L-A mechanism reproduction (two-line transcript, run in this environment)

```
$ mkdir -p dist/app && printf '#!/bin/sh\necho CORE-BOOTED\n' > dist/app/core && chmod +x dist/app/core
$ CORE_BIN=dist/app/core; [ -x "$CORE_BIN" ] && echo "validation PASSES (workflow test -x equivalent)"
validation PASSES (workflow test -x equivalent)
$ mkdir -p work && cd work && "$CORE_BIN"
/bin/bash: line 1: dist/app/core: No such file or directory
exit=127
```

Matches the hosted step-24 evidence class verbatim (`bash:
dist/linux/app/SHEYTAN-Local-Agent: No such file or directory` → the core
child dies instantly → `SMOKE FAIL: core died at boot`). After
canonicalization the identical sequence execs successfully:

```
$ CORE_BIN="$(cd "$(dirname "$CORE_BIN")" && pwd)/$(basename "$CORE_BIN")"
/home/z/my-project/work/repro-lA/dist/app/core → CORE-BOOTED, exit=0
```

### Tree sweep proof (relative-path-after-cd core exec sites)

`rg` over scripts/, e2e/, packaging/, .github/workflows/build-desktop.yml for
CORE_BIN-class consumers (`CORE_BIN|SHEYTAN_CORE_BIN|UI_BIN|SHEYTAN_UI`):

- `scripts/qt-desktop-smoke.sh` — the ONLY site that execs a CORE_BIN-class
  variable after a `cd` (sh:94 `cd "$WORK"` → sh:261 exec). THIS FIX.
- `.github/workflows/build-desktop.yml:1189` — `(Resolve-Path $core).Path`
  → already absolute (Windows desktop runtime smoke).
- `.github/workflows/build-desktop.yml:2213` — `"$(pwd)/dist/..."` → already
  absolute (Linux acceptance chain).
- `scripts/verify-all.mjs:163-183` — `path.join(ROOT, …)` → absolute.
- No other script `cd`s and then execs a core-class relative path. Sweep
  clean; the workflow may keep passing the relative CORE_BIN — the script
  becomes immune.

### W-A classification (INFERENCE — paste unavailable, per Phase 1 boundary)

The failing statement is host-published only in the unavailable
`windows-go-verification-logs-attempt1` paste. Candidate selection is
therefore **INFERENCE, C1 primary**, per the mission's own fallback rule.
The Windows-semantics violation, stated in one paragraph before editing:

> On the accept path, `connectUntilClientOrClose` waits on the overlapped
> `ConnectNamedPipe` and returns whatever `GetOverlappedResult` reports
> (listener_windows.go:385). `Accept` (:277–291) classifies ONLY
> `windows.ERROR_NO_DATA` as the transient client-gone race; the rest of the
> vanished-peer completion family — `ERROR_BROKEN_PIPE` (109) and
> `ERROR_PIPE_NOT_CONNECTED` (233), which a client that connects and dies
> before the connect completion resolves can surface instead — propagates to
> `return nil, err`, which ends `Accept`, ends `http.Serve`, and tears down
> the whole endpoint. The malformed-garbage test is the ONLY hosted test
> whose client closes WITHOUT reading: garbage "NOT-HTTP AT ALL\r\n\r\n",
> instant Close, then a health redial — if the accept loop died on the
> vanished garbage peer's completion, the redial's CreateFile finds no
> listening instance and `client.Get` fails instantly, matching the 0.06s
> fast-fail. The passing oversized test reads the 400/431 response before
> closing, so its peer is still connected through the whole transport
> window and never exercises this classification. INFERENCE: fix C1 —
> classify the vanished-peer completion family as transient on the accept
> path, preserving the endpoint-fault distinction (ERROR_OPERATION_ABORTED
> outside the W3 close path and every other fault stays fatal). The hosted
> run is the only runtime authority; if it proves otherwise, the next fix
> consumes its assertion text (V-A's upgrade makes that text machine-
> readable).

## Phase 3 — WINDOWS TRANSPORT FIX (compile-level local proof; hosted run = runtime authority)

- `internal/localipc/listener_windows.go`:
  - **W-A / C1**: `Accept` now classifies the WHOLE vanished-peer completion
    family via `isVanishedPeer` (`ERROR_NO_DATA`, `ERROR_BROKEN_PIPE`,
    `ERROR_PIPE_NOT_CONNECTED`) as transient client-gone — recreate the
    instance, keep serving. `ERROR_OPERATION_ABORTED` and every other fault
    stay fatal (endpoint-fault distinction preserved; the W3 close path is
    already filtered by the `l.closed` select before classification).
  - **W-B**: `pipeConn.Close` now stops `readTimer` UNDER `muDeadline` and
    clears `readDeadline` — a stale `time.AfterFunc(..., CancelIoEx(handle,
    nil))` can no longer fire on a recycled handle value. Clearing the
    deadline under the same lock also disarms an already-running callback's
    armed re-check (Stop() is best-effort by Go contract).
- `internal/localipc/listener_windows_test.go`: two regression pins added —
  `TestAcceptVanishedPeerClassification` (family membership + fatal
  boundaries) and `TestPipeConnCloseStopsReadDeadlineTimer` (timer stopped +
  deadline cleared). Executed by the hosted Windows job; compile-verified
  locally (this environment is Linux-only).
- Local gate results:
  - `gofmt -l .` → empty (clean).
  - `go build ./...` + `go vet ./internal/localipc/` → exit 0 (Unix).
  - `GOOS=windows CGO_ENABLED=0 go build ./...` + `GOOS=windows go vet` → exit 0.
  - `go test ./internal/localipc/ -tags headless -count=1` → ALL PASS,
    including `TestServeMalformedRequestIsRejectedNotCrashing` (0.00s,
    unchanged contract) and `TestServeOversizedRequestHeadersRejected`.
- Evidence-class boundary, stated plainly: the Windows transport CANNOT run
  in this Linux-only environment — local proof for W-A/W-B is COMPILE-LEVEL
  plus the unchanged Unix localipc suite (the Unix transport serves the same
  malformed-garbage contract). The hosted Windows run is the only runtime
  authority for the overlapped-pipe behavior.

## Phase 4 — SMOKE PATH FIX + LOCAL PROOF

- `scripts/qt-desktop-smoke.sh`: CORE_BIN and UI_BIN are canonicalized to
  absolute paths at resolution time — immediately after the fallback-build
  block, BEFORE `cd "$WORK"` — so the fallback-build check, the pkill
  patterns, and the bottom exec all reference the same file regardless of
  working-directory changes. A relative CORE_BIN that is missing after
  resolution now fails LOUDLY at the canonical name instead of ENOENTing
  silently at the exec. `bash -n` clean.
- Local end-to-end run, workflow-shaped (relative CORE_BIN exactly as
  build-desktop.yml:2107 passes it), from the repo root:
  - transcript: `core binary not staged — building it from the repo with go`
    → fallback build from the RELATIVE path → canonicalization → Xvfb up →
    fixture engine up → **core BOOTED under the work-directory cd**:
    `SHEYTAN-Local-Agent v1.10.8 (Qt desktop)` /
    `core endpoint: /tmp/sheytan-la-1001/core.sock` — the endpoint line in
    core.log, the FIRST time this script has run anywhere with the core up
    past a work-directory cd. NO bash ENOENT line anywhere in the run.
  - Result: `SMOKE FAIL: G2: core did not spawn a live UI child` (exit 1) —
    the EXPECTED honest boundary: the run was invoked with
    `SHEYTAN_UI_EXECUTABLE` = the core's own `none` sentinel because the Qt
    6.8.2 toolchain is ABSENT in this environment. Missing tools recorded
    exactly: openbox, xdotool, xwininfo, xprop, qmake6/aqt (Qt host), pwsh.
    G1 was reached by the core (endpoint served), but the script's gate
    order executes G2 first, so G1/G3/G4/G5 remain host-unproven locally —
    never claimed otherwise.
  - Cleanup: trap swept all children (no orphan core/Xvfb/engine). One
    observation recorded, NOT fixed (pre-existing, outside the register,
    no evidence named it): a G2 abort leaves `$WORK/../core.sock` behind —
    the next boot's stale-endpoint sweep removes it (`localipc: removed
    stale endpoint …` line in this same transcript).

## Phase 5 — WINDOWS ANNOTATION UPGRADE (V-A.2)

- `build-desktop.yml` "Go verification failure summary (Windows)": BOTH
  failing-lines filters (step summary + ::error:: emission loop) extended
  from `'^(--- FAIL|FAIL|panic:|fatal error:)'` to
  `'^(--- FAIL|FAIL|panic:|fatal error:)' -or $_ -match '\.go:[0-9]+: '`.
  Go assertion statements ("serve_test.go:287: ...") and compile errors
  ("file.go:NN: ...") now publish anonymously, capped at the existing
  10-annotation budget with the existing 250-char truncation. No gate
  weakened; no new secrets class (rule 7 re-asserted in the step comment).
- Pattern verification against a simulated log excerpt (mission-mandated),
  recorded verbatim: OLD filter caught 3 lines (--- FAIL / FAIL / module
  FAIL); NEW filter caught 5 — additionally
  `    serve_test.go:287: health after garbage: …` (the failing STATEMENT)
  and `listener_windows.go:123: undefined: notARealSymbol` (compile-error
  class, desirable).
  Environment boundary: pwsh is unavailable in this Linux environment — the
  pattern was verified with `grep -E` and Python `re` (identical semantics
  to .NET regex for this pattern class). The pwsh `-match` form is the same
  expression; the hosted run is the final authority.
- YAML validity re-confirmed after the edit (`yaml.safe_load` → OK).

## Phase 6 — FULL LOCAL REGRESSION (gate table)

| Gate | Command | Result |
|------|---------|--------|
| npm ci | `npm ci --no-audit --no-fund` | PASS (139 packages, exit 0) |
| typecheck | `npm run typecheck` (tsc --noEmit) | PASS |
| lint | `npm run lint` (oxlint) | PASS (0 warnings, 0 errors, 111 files) |
| unit tests | `npm run test:units` | PASS (236/236) |
| release tests | `npm run test:release` | PASS (38/38) |
| frontend build | `npm run build` | PASS (exit 0) |
| web verify | `npm run verify:web` | PASS (stable-asset contract satisfied) |
| gofmt | `gofmt -l .` | PASS (empty) |
| go test internal | `go test ./internal/... -tags headless -count=1` | PASS (exit 0) |
| go test full | `go test ./... -run Test -count=1` | PASS (65 packages ok, 0 fail) |
| go race | `go test -race -tags headless -count=1` on api/agent/sessions/contextplan/histref/runtime | PASS (api 102.6s, agent 25.2s, rest green) |
| go vet | `go vet ./...` | PASS |
| windows cross | `GOOS=windows CGO_ENABLED=0 go build ./...` + `go vet` | PASS (both) |
| stress gate | `go build -o /tmp/sheytan-stress ./scripts/stress-main` + `stress` | PASS (47 pass / 0 fail / 0 hangs / 0 crashes) |
| verify-all | `node scripts/verify-all.mjs` (go on PATH) | PASS (10 PASS; native CTest + Qt host NOT RUN — cmake/Qt6 absent, environment-limited as in v1.10.8's local run) |
| smoke syntax | `bash -n scripts/qt-desktop-smoke.sh` | PASS |
| repository shape | audit-job assertions mirrored locally | PASS (all present/absent as contracted) |
| version contract | `node scripts/release-version.mjs --check` | PASS at 1.10.8 (pre-bump state) |
| Browser E2E | `npm run test:e2e` | NOT RUN locally (PROTECTED verified-green surface — untouched this cycle; browsers not provisioned here and the mission's Phase 6 battery does not include it; the hosted run exercises it unchanged) |

No Unix regression anywhere in the transport fix: the localipc malformed and
oversized tests pass unchanged (the automatic revert-and-redo tripwire never
fired).

## Phase 7 — COMMIT + DOCS/VERSION (per-item outcomes)

Version identity: `node scripts/release-version.mjs` bumped from package.json
1.10.9 → config.go AppVersion, build/config.yml productVersion, SIGNATURE
first line, and the tracked web bundle (`web/static/assets/index.js`, the
version string embedded by the tracked build contract); `--check` green.

Per-item outcome table (this cycle's register):

| Item | Outcome | Closure class |
|------|---------|---------------|
| L-A | CLOSED (code) | `scripts/qt-desktop-smoke.sh` canonicalization + local workflow-shaped run with the core booted past the cd (honestly bounded at G2) + two-line reproduction transcript + tree sweep proof |
| W-A | CLOSED (code, INFERENCE C1) | `listener_windows.go` accept-path vanished-peer family via `isVanishedPeer`; compile-level local proof (Linux-only env); hosted run = runtime authority |
| W-B | CLOSED (code) | `pipeConn.Close` stops readTimer under muDeadline + clears deadline; regression pin test |
| V-A | CLOSED (code) | both Windows summary filters extended with the `\.go:[0-9]+: ` class; simulated-log verification (grep -E + python re; pwsh absent locally) |
| U-A | OPEN by design | first-time surfaces (smoke gates G1–G5, Windows steps 16–32, Linux steps 26–33, publish job) consumed as evidence-only fix-forward after the operator's push |
| D1 | CLOSED (docs) | changelog v1.10.9 entry (evidence quoted, INFERENCE labeled), UPDATE.md/agent.md hosted-truth statements, this worklog |

Commit: prepared in the working tree — this environment cannot push
(no credentials). The OPERATOR pushes main; the hosted run of this revision
is the first exercise of the first-time surfaces; fix forward ONLY on what
its annotations and summaries name.

---

# v1.10.10 — THE INSTANCE-INVARIANT FIX + CLIENT PIPE-BUSY CONTRACT + DOCS CONDENSATION

Task ID: 1 (single-agent session, 113-minute budget)
Agent: v1.10.10 engineering session
Task: fix the Windows named-pipe instance race at its root on BOTH sides of
the contract (server instance inventory + client ERROR_PIPE_BUSY handling),
pin it with a Windows-gated burst regression test, execute the operator's
docs order (stale version lines removed, pre-1.9.x history condensed,
docs polished to the hosted truth), and ship the complete source ZIP.

## Phase 1 — LIVE STATE + EVIDENCE (quoted BEFORE any fix was chosen)

Baseline claim re-verified at live HEAD: clone of main == `0571b49b2b8c3a54aa4c97edf439433c98c0076c` (v1.10.9) — the register's observed baseline, NO drift.

Hosted run 38919925013 (v1.10.9), jobs re-read via the anonymous REST API
this cycle:

- `Source & frontend audit` — success.
- `Linux x64` — success. FULL GREEN end-to-end for the first time in the
  repository's history: the core booted hosted for the first time
  (CORE_BIN canonicalization), the real smoke gates G1–G5 passed, and the
  entire chain behind them passed (Qt live-answer acceptance, headless
  surface runtime smoke, packaging, ZIP verification, artifact upload).
  Linux is DONE — a protected surface.
- `Windows x64` — failure at exactly step 14 "Go verification", the FULL
  go test suite, one test.
- `Publish and verify main` — skipped (never executed in any run).

The Windows evidence, quoted verbatim from the run's check-run annotations
(anonymously readable, re-fetched this cycle; the v1.10.9 emitter upgrade
carried the failing assertion anonymously as designed):

```text
win-go-test-full:     serve_test.go:287: health after garbage: Get "http://localipc/api/health": All pipe instances are busy.
win-go-test-full: --- FAIL: TestServeMalformedRequestIsRejectedNotCrashing (0.42s)
win-go-test-full: FAIL  github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc      12.487s
```

The asymmetry (both facts from THIS run): the SAME package PASSED the
headless run (`go test ./internal/... -tags headless`) and FAILED only the
full suite (`go test ./... -run Test -count=1`, parallel package load) —
the failure is load-dependent. `All pipe instances are busy` is
ERROR_PIPE_BUSY: the pipe exists, but ZERO instances are listening at the
instant of the test's immediate health redial (write garbage, close,
redial in microseconds).

Docs boundary confirmed at HEAD: README.md line 16 `Current release:
v1.10.8` inside the ```text identity block; worklog.md line 3 `Current
release:  v1.10.3`; changelog.md carries nine FULL per-version sections
v1.8.0–v1.8.8 with the `## v1.7.0 → v1.7.6 (compact)` precedent directly
below them; worklog.md carries the full pre-1.9.x session writeups for
v1.8.0/v1.8.1/v1.8.3; UPDATE.md carries superseded v1.10.0/v1.10.1 notes
sections.

## Phase 2 — ROOT-CAUSE CONFIRMATION (W-RC, line-by-line at HEAD)

`internal/localipc/listener_windows.go` at `0571b49`:

- `Accept` (line 248) → `takeInstance` (line 256) hands out the spare
  instance and EMPTIES the inventory; the refill `preCreate` sits at line
  316 — AFTER `connectUntilClientOrClose` (line 277) returns, i.e. after
  the server goroutine observes the overlapped connect completion via
  `GetOverlappedResult`.
- The KERNEL marks instance #1 busy (connected) at the FIRST client's
  `CreateFile` — before the server goroutine has noticed anything. The
  window: [kernel connect → zero listening instances] → [event fires] →
  [goroutine wakeup] → [GetOverlappedResult] → [preCreate refills]. Under
  the full suite's parallel package load, the test goroutine's
  write+close+redial (microseconds) beats the server goroutine's refill
  bookkeeping; the redial's `CreateFile` finds zero listening instances →
  ERROR_PIPE_BUSY. The headless run's lighter load leaves the refill ahead
  in practice — exactly the observed asymmetry.
- `dialEndpoint` (lines 150–164) treats EVERY `CreateFile` error —
  including ERROR_PIPE_BUSY — as a plain immediate failure: no
  WaitNamedPipe, no retry. The documented Windows named-pipe client
  contract for this exact error is unimplemented.
- The v1.10.9 vanished-peer fix (`isVanishedPeer`) repaired a DIFFERENT,
  real accept-path window (a client that connects and dies before the
  overlapped connect resolves) — it stays, untouched.

The Windows named-pipe semantics violation, stated plainly: the listener
guaranteed "an instance ahead of demand" only in USERSPACE bookkeeping;
the KERNEL's connect-time instance-busy marking is what empties the
listening set, and the userspace refill ran outside the lock that governs
the instance inventory, after a goroutine wakeup. A listening instance
must exist at EVERY instant from Serve's return until Close — enforced
structurally (refill inside the same locked section as the hand-out), not
by userspace bookkeeping order.

The client contract, verified against current Microsoft documentation
this cycle (Named Pipe Client, learn.microsoft.com, fetched 2026-10-17):
"If the pipe exists but all of its instances are busy, CreateFile returns
INVALID_HANDLE_VALUE and the GetLastError function returns ERROR_PIPE_BUSY.
When this happens, the named pipe client uses the WaitNamedPipe function
to wait for an instance of the named pipe to become available." The kernel
instance-selection semantics were verified against the NPFS implementation
(client open scans the instance list head-to-tail; instances are inserted
at the TAIL — FIFO by creation order): the handed-out instance is always
older than the spare, so a client always pairs with the pending-connect
instance first, and a client landing on the spare is served by the very
next Accept via the existing ERROR_PIPE_CONNECTED path.

Rule-8 sweeps (proof-before-delete):

- `Current release` across the tree: README.md:16 (v1.10.8, ordered
  removed), worklog.md:3 (v1.10.3, ordered removed), docs/agent.md (the
  current-release narrative line — updated to v1.10.10 hosted truth, not
  in the D1 removal scope), plus two historical mentions INSIDE old
  session writeups (session records, kept as history).
- Version literals in non-docs code: only the five canonical release
  surfaces (package.json, internal/config/config.go, build/config.yml,
  SIGNATURE, .github/workflows/build-desktop.yml). Pin-proof re-verified:
  `scripts/release-version.mjs` maintains exactly those five surfaces and
  contains no README/worklog reference; `scripts/release-version.test.mjs`,
  `cmd/stress_release_surface.go` (presence check only — content
  untouched), and `internal/releasecontract` (packaged-app README.txt
  paths, not the repo README) contain no "Current release" reference.
  Removal breaks no gate.
- Cross-references into the sections being condensed: the v1.8.x mentions
  in README/agent.md/ARCHITECTURE.md are version-fact markers
  ("Deterministic data analysis (v1.8.7)", "the v1.8.5 reasoning ladder"),
  not section-anchor links; the compact changelog section preserves each
  version's one-line essence so every marker still resolves. The pre-1.9.x
  worklog sessions carry NO run URLs (verified by sweep) — condensation
  loses no evidence lines. No doc references the UPDATE v1.10.0/v1.10.1
  superseded sections by anchor.

Condensation plan (scope decision, stated before editing): changelog.md
collapses the nine FULL v1.8.0–v1.8.8 sections into ONE `## v1.8.0 →
v1.8.8 (compact)` section following the v1.7.x precedent (each version
keeps its one-line essence and key invariants; v1.9.0+ stays detailed);
worklog.md collapses the named pre-1.9.x session writeups (v1.8.0, v1.8.1,
v1.8.3) to compact summaries (v1.9.2+ sessions and the already-compact
records stay); UPDATE.md condenses the superseded v1.10.0/v1.10.1 notes
("What v1.10.x changes" from v1.10.2 onward stays). Keep-list regardless
of age: every v1.9.x/v1.10.x entry, every run URL, every quoted evidence
line.

## Phase 3 — WINDOWS TRANSPORT FIX (compile-level local proof; hosted run = runtime authority)

W-INV (server, `internal/localipc/listener_windows.go`): `takeInstance`
now creates the replacement pipe instance inside the SAME locked section
that hands out the spare — the inventory can never be observed empty at
a hand-out, so at least one listening instance exists at every instant
from Serve's return until Close. The post-completion refill (the old
`preCreate` call in Accept, and with it the now-unused `setHandleErr`
publisher) is REMOVED; `takeInstance` also refuses to create anything
after Close (the endpoint still vanishes with the last handle — this
also closes a latent post-Close spare-creation race the old shape had).
A refill failure defers to the next Accept via `handleErr` exactly as
before (the current connection is still handed to http.Serve). The
kernel FIFO pairing was verified against the NPFS implementation
(client open scans the instance list head-to-tail; instances insert at
the tail — so the pending-connect instance always pairs first and a
client landing on the spare is served by the very next Accept through
the existing ERROR_PIPE_CONNECTED path).

W-CLI (client, same file): `dialEndpoint` — the production dial path
behind the UI's http.Client — now implements the documented
ERROR_PIPE_BUSY sequence (verified against current Microsoft
documentation, quoted in Phase 2): on ERROR_PIPE_BUSY, call
WaitNamedPipe (bounded 2000 ms — the OS's own readiness notification,
event-driven, never a sleep-based poll; bound directly from winbase as
WaitNamedPipeW since x/sys does not expose it), retry CreateFile up to
3 times, return the real error (wrapping the last ERROR_PIPE_BUSY) on
exhaustion. A WaitNamedPipe ERROR_FILE_NOT_FOUND (endpoint vanished)
stops the retry immediately — no retry can create a pipe into existence.

W-PIN (two Windows-gated tests in `listener_windows_test.go`):
`TestTakeInstanceRefillsSpareUnderSameLock` — the STRUCTURAL pin,
deterministic: after takeInstance hands out an instance, the
replacement must already exist, inspected under the inventory lock
(this fails deterministically against the old post-completion-refill
shape); `TestMalformedBurstUnderParallelismNeverSurfacesPipeBusy` — the
LOAD pin: the exact W-A shape (dial, write garbage, close WITHOUT
reading, IMMEDIATE redial for health) × 8 parallel goroutines × 4
cycles, keep-alives disabled so every request is a TRUE fresh dial,
asserting no dial surfaces an ERROR_PIPE_BUSY error and every health
redial answers 200. The existing malformed-garbage test in serve_test.go
is byte-identical (git diff proves it); serve_test.go is untouched.

Evidence class, stated plainly: local proof for the Windows transport is
COMPILE-LEVEL ONLY (GOOS=windows go build + go vet — both green); the
Linux-only environment cannot run Windows tests. The Unix localipc
suite (which exercises the SAME serve/accept contract over the Unix
transport, including the untouched malformed and oversized tests) is
green and unchanged — no contract drift on the shared path. The hosted
Windows job is the only runtime authority for the named-pipe fix.

## Phase 4 — D1/D2 EXECUTION (docs)

D1: README.md's identity block is version-free (the Current release line
removed; Application / Executable / AppUserModelID / Branch kept);
worklog.md's header carries no version and names docs/changelog.md as
the canonical version authority. The Phase 2 sweep proof covers both
removals.

D2: changelog.md's nine FULL v1.8.0–v1.8.8 sections (765 lines) → one
`## v1.8.0 → v1.8.8 (compact)` section (75 lines, the v1.7.x precedent),
each version keeping its one-line essence and key invariants; v1.9.0+
detailed sections untouched. worklog.md's named pre-1.9.x full sessions
(v1.8.3, v1.8.1, v1.8.0 — 284 lines) → compact summaries (125 lines)
with every baseline fact, root cause and evidence identifier preserved;
the already-compact records (pre-v1.8.0, v1.8.5 PHASE 1) and every other
session untouched. UPDATE.md's superseded v1.10.1/v1.10.0 notes (172
lines) → condensed (102 lines) preserving every run URL and decision;
the "What v1.10.x changes" sections from v1.10.2 onward untouched. All
seams verified; the seven docs files remain present (repository-shape
contract intact — the stress gate checks presence, not content).

## Phase 5 — D3 + POLISH + VERSION IDENTITY

changelog.md carries the v1.10.10 entry (run 38919925013 evidence quoted
verbatim: the Linux full-green milestone, the assertion line, the FAIL
line, the headless/full-suite asymmetry, the root cause, both fix parts,
the pins, the docs order). UPDATE.md carries "What v1.10.10 changes" and
the v1.10.10 header. agent.md states the hosted truth (Linux protected
and fully green; Windows compile-level proof, hosted proof pending;
steps 16–32 and publish first-time surfaces). ARCHITECTURE.md's
"Current for" marker and ROADMAP.md's shipped-baseline marker updated to
v1.10.10. Version identity: package.json 1.10.9 → 1.10.10, canonical
sync green (internal/config/config.go AppVersion, build/config.yml
productVersion, SIGNATURE first line), `release-version.mjs --check`
GREEN at 1.10.10; the tracked web bundle rebuilt (web/static/assets/
index.js — the embedded frontend, verify:web green).

## Phase 6 — FULL LOCAL REGRESSION (gate table; every command executed this session)

| Gate | Command | Result |
|------|---------|--------|
| Frontend deps | npm ci | PASS (0 vulnerabilities) |
| Typecheck | npm run typecheck | PASS |
| Lint | npm run lint | PASS (0 warnings, 0 errors) |
| Frontend units | npm run test:units | PASS 236/236 |
| Release units | npm run test:release | PASS 38/38 |
| Frontend build | npm run build (+sync:web) | PASS (bundle embedded at 1.10.10) |
| Stable asset contract | npm run verify:web | PASS |
| gofmt | gofmt -l . | CLEAN (empty) |
| Go vet | go vet ./... | PASS |
| Headless suite | go test ./internal/... -tags headless -count=1 | PASS (exit 0) |
| FULL suite | go test ./... -count=1 | PASS (exit 0) |
| Race gate | go test -race (api/agent/sessions/contextplan/histref/runtime + localipc) | PASS (exit 0) |
| Windows compile | GOOS=windows go build ./... | PASS |
| Windows vet | GOOS=windows go vet ./... | PASS (compiles the new tests) |
| Smoke syntax | bash -n scripts/qt-desktop-smoke.sh | PASS |
| Canonical funnel | node scripts/verify-all.mjs | PASS (native CTest + Qt host NOT RUN — environment-limited, no cmake/Qt6 locally; hosted run covers both) |
| Stress suite | go build ./scripts/stress-main && /tmp/sheytan-stress stress | PASS 47/47, hangs=0, crashes=0 |
| Release metadata | node scripts/release-version.mjs --check | GREEN at 1.10.10 |
| Unix localipc | go test ./internal/localipc/ -count=1 -v | PASS (malformed + oversized untouched, green) |

## Phase 7 — COMMIT + OPERATOR PUSH

Commit prepared in the working tree (transport fix + pins + docs + version
identity + rebuilt web bundle). This environment cannot push (no
credentials) — the OPERATOR pushes main. The hosted run of this revision
is the first exercise of the Windows fix (W-INV/W-CLI/W-PIN) and of the
never-executed Windows steps 16–32 and the publish job; fix forward ONLY
on what its annotations and summaries name. Run URL: to be filled by the
operator's push (run of the v1.10.10 commit this entry ships in).

Per-item outcome table (this cycle's register):

| Item | Outcome | Closure class |
|------|---------|---------------|
| W-A | CLOSED (code, both sides of the contract) | W-INV server invariant (refill inside the inventory lock) + W-CLI client WaitNamedPipe contract; compile-level local proof; hosted run = runtime authority |
| W-RC | CLOSED (root cause confirmed line-by-line) | kernel connect-time busy marking vs post-completion userspace refill — the gap is structurally eliminated |
| W-INV | CLOSED (code) | takeInstance same-lock refill; post-Close creation refused; deterministic structural pin added |
| W-CLI | CLOSED (code) | dialEndpoint ERROR_PIPE_BUSY → WaitNamedPipe(2s) × bounded 3 retries → real error; MSDN contract quoted; no sleep-based correctness |
| W-PIN | CLOSED (code) | structural pin + 8×4 parallel burst pin (W-A shape, keep-alives disabled); malformed-garbage test byte-identical |
| W-BND | OPEN by design | first-time surfaces (Windows steps 16–32, publish job) consumed as evidence-only fix-forward after the operator's push |
| D1 | CLOSED (docs) | README.md + worklog.md version lines removed; rule-8 sweep proof recorded (Phase 2) |
| D2 | CLOSED (docs) | changelog v1.8.x compact, worklog pre-1.9.x sessions compact, UPDATE superseded notes condensed; keep-list honored |
| D3 | CLOSED (docs) | changelog v1.10.10 entry (evidence verbatim), this worklog entry, UPDATE.md v1.10.10 section, agent.md hosted truth |

---

# v1.10.11 — THE KERNEL32 BINDING REPAIR + NON-PANICKING DIAL PATH

Task ID: 1 (single-agent session, 113-minute budget)
Agent: v1.10.11 engineering session
Task: fix the win-go-test-headless process panic at its root — the
WaitNamedPipeW lazy binding targeting a non-existent module (winbase.dll)
— by rebinding from kernel32.dll (the documented exporting module), making
the dial path structurally non-panicking (Find()-based resolution, typed
error), pinning the defect class with a Windows-gated proc-resolution
regression test, writing the ordered docs entries, and shipping the
complete source ZIP.

## Phase 1 — LIVE STATE + EVIDENCE (quoted BEFORE any fix was chosen)

Baseline claim re-verified at live HEAD: clone of main ==
`5ccbe357e3616e256f446f9e9ae92c798007f549` (v1.10.10) — the register's
observed baseline, NO drift (HEAD == baseline, empty diff).

Hosted run 38984879544 (v1.10.10, commit 5ccbe35), jobs re-read via the
run page this cycle (the anonymous REST API was rate-limited from the
working IP; the run page carries the same annotation content):

- `Source & frontend audit` — success.
- `Linux x64` — success, FULL GREEN end-to-end (2nd consecutive complete
  pass — Linux is DONE, a protected surface; not churned this cycle).
- `Windows x64` — failure at step 14 "Go verification", the HEADLESS run,
  on a process panic.
- `Publish and verify main` — 0 s, never executed (in any run, ever).

The Windows evidence, quoted verbatim from the run's annotations (four
emitted; re-read live this cycle and matching the register exactly):

```text
win-go-test-headless: panic: The specified module could not be found.
win-go-test-headless: FAIL  github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc  7.795s
win-go-test-headless: FAIL
Process completed with exit code 1.
```

W-FULL, labeled INFERENCE (high confidence): the emitter captures every
FAIL/panic line from BOTH Windows test runs with per-run prefixes, cap 10
annotations; only 4 were emitted and ZERO carry the win-go-test-full
prefix — the Windows FULL suite (which failed four consecutive releases
with ERROR_PIPE_BUSY) appears GREEN at 5ccbe35. This is inference, not
verified fact: the hosted run of THIS revision is the confirmation
authority; no claim of "full suite green" is made until its step 14
report is green under both runs.

Evidence-class boundary, stated up front: local proof for the Windows
transport is COMPILE-LEVEL ONLY (GOOS=windows go build + go vet), and
THIS cycle proved that class CANNOT see lazy-binding defects — LazyDLL
loads on first Call, not at compile or init. The hosted Windows job is
the only runtime authority for this fix.

## Phase 2 — ROOT-CAUSE CONFIRMATION + LAZY-BINDING SWEEP (rule 8)

`internal/localipc/listener_windows.go` at `5ccbe35`, line-by-line (the
register's account confirmed exactly):

- Line 188: `modwinbase = windows.NewLazySystemDLL("winbase.dll")` — a
  module that does NOT exist as a loadable file on modern Windows.
- Line 189: `procWaitNamedPipeW = modwinbase.NewProc("WaitNamedPipeW")`.
- Line 202: `procWaitNamedPipeW.Call(...)` — an UNGUARDED LazyProc.Call:
  on load failure LazyProc.Call PANICS, killing the process.

Documented contract re-verified against current Microsoft documentation
(WaitNamedPipeA function, winbase.h): Requirements — "Library:
Kernel32.lib", "DLL: Kernel32.dll" (winbase is the HEADER family;
kernel32 is the exporting DLL), and against the canonical go-winio
client: `modkernel32 = windows.NewLazySystemDLL("kernel32.dll")` binding
`WaitNamedPipeW` in zsyscall_windows.go. MSDN Named Pipe Client documents
the ERROR_PIPE_BUSY → WaitNamedPipe contract the v1.10.10 client code
implements; only the binding's module name and the panic behavior were
wrong.

The failure mechanism, end to end: the v1.10.10 burst pin
(TestMalformedBurstUnderParallelismNeverSurfacesPipeBusy, 8 workers × 4
cycles) legitimately surfaces transient ERROR_PIPE_BUSY (concurrent
CreateFile calls race for the ONE listening instance W-INV guarantees —
the invariant guarantees one LISTENING instance, not one free slot per
racing client; losers receive busy until takeInstance's refill lands).
The first busy handler called waitNamedPipe → procWaitNamedPipeW.Call →
LoadLibrary("winbase.dll") → ERROR_MOD_NOT_FOUND (message exactly "The
specified module could not be found.") → LazyProc.Call panic → the
localipc test binary died 7.795 s into the headless package run → FAIL +
exit code 1. W-TRIG (the burst pin) is the TRIGGER, not a defect — it
exposed a real production defect exactly as designed (in the real UI the
same panic kills the application process on its first contended dial);
it stays byte-identical.

Rule-8 sweep (proof-before-edit) — every lazy DLL load in the tree,
`rg 'NewLazyDLL|NewLazySystemDLL'`, module × proc × reachable production
path, recorded BEFORE the edit:

| File | Module | Procs | Production path | Module loadable on modern Windows? |
|------|--------|-------|-----------------|-------------------------------------|
| internal/localipc/listener_windows.go | winbase.dll | WaitNamedPipeW | dialEndpoint → waitNamedPipe (every ERROR_PIPE_BUSY dial) | NO — THE DEFECT (header, not DLL) |
| internal/sysinfo/sysinfo_fast_windows.go | kernel32.dll | GlobalMemoryStatusEx | sysinfo fast path | YES (documented) |
| internal/sysinfo/sysinfo_fast_windows.go | ntdll.dll | RtlGetVersion | sysinfo fast path | YES |
| internal/resources/resources_windows.go | kernel32.dll | OpenProcess, CloseHandle | process resource sampler | YES |
| internal/resources/resources_windows.go | psapi.dll | GetProcessMemoryInfo | process resource sampler | YES |
| internal/platform/identity_windows.go | shell32.dll | SetCurrentProcessExplicitAppUserModelID | process identity (UI host) | YES |
| internal/native/filedialog_windows.go | comdlg32.dll | GetOpenFileNameW | native file dialog | YES |
| internal/native/filedialog_windows.go | user32.dll | GetForegroundWindow | native file dialog | YES |
| internal/api/perf_windows.go | kernel32.dll | GetSystemTimes, GlobalMemoryStatusEx | perf counters | YES |
| internal/screen/screen_windows.go | user32.dll, gdi32.dll | GetSystemMetrics, GetDC, CreateCompatDC, CreateCompatibleBitmap, SelectObject, BitBlt, GetDIBits, DeleteDC, DeleteObject, ReleaseDC | screen capture | YES (both) |
| internal/governor/cpu_windows.go | kernel32.dll | GetSystemTimes | governor CPU sampler | YES |
| internal/llm/importlock_windows.go | kernel32.dll | LockFileEx, UnlockFileEx | import lock | YES |
| internal/proc/identity_windows.go | iphlpapi.dll | GetExtendedTcpTable | proc identity | YES |

Sweep conclusion: exactly ONE non-existent module name in the tree —
`winbase.dll` in listener_windows.go, the named defect. Every other
binding targets a documented, loadable module (the same module set Go's
own stdlib loads via internal/syscall/windows/sysdll), and every proc
name is a documented export of its module. The other unguarded
LazyProc.Call sites cannot panic-from-missing-module because their
modules exist; per "do not edit anything the evidence does not name",
only W-BIND was fixed. The transport's dial path — the one place where a
lazy-load failure would kill the HOST APPLICATION's process rather than
a test binary — is now Find()-guarded regardless.

Defect and fix, one paragraph: the v1.10.10 client-side busy handler
bound WaitNamedPipeW from "winbase.dll", a module name that can never
LoadLibrary on modern Windows (the exporting DLL is kernel32.dll; winbase
is a header), and invoked it through an unguarded LazyProc.Call whose
load failure is a process-killing panic — invisible to compile-level
checks because LazyDLL defers loading to the first Call. The fix rebinds
the proc from kernel32.dll (the documented exporting module, the canonical
winio binding) and makes waitNamedPipe resolve via Find() with a typed
error return (ErrWaitNamedPipeUnresolved) so the dial's bounded retries
exhaust and surface the original busy error — no panic path remains in
transport dial code; the retry constants and dialEndpoint loop are
unchanged.

## Phase 3 — THE FIX + PIN (code diff; docs never substitute)

internal/localipc/listener_windows.go:

- `modkernel32 = windows.NewLazySystemDLL("kernel32.dll")`;
  `procWaitNamedPipeW = modkernel32.NewProc("WaitNamedPipeW")` (W-FIX a).
- `var ErrWaitNamedPipeUnresolved = errors.New("localipc: WaitNamedPipeW
  proc unresolved")` — the typed resolution-failure error.
- `waitNamedPipe` now calls `procWaitNamedPipeW.Find()` BEFORE any Call
  and returns `fmt.Errorf("%w: %v", ErrWaitNamedPipeUnresolved, err)` on
  resolution failure (W-FIX b) — a real error, never a panic; the dial
  loop's bounded retries then exhaust and surface the ORIGINAL busy
  error honestly.
- dialEndpoint: UNTOUCHED (git diff proves the loop, the constants
  pipeBusyWaitMS = 2000 / pipeBusyMaxRetries = 3, and the whole accept
  path are byte-identical).
- Burst pin (TestMalformedBurstUnderParallelismNeverSurfacesPipeBusy),
  structural pin (TestTakeInstanceRefillsSpareUnderSameLock), the
  malformed-garbage test: BYTE-IDENTICAL (git diff of
  listener_windows_test.go is purely additive — 0 deletions).

internal/localipc/listener_windows_test.go — the new W-PIN (additive
only):

- `TestWaitNamedPipeProcResolves` — asserts `procWaitNamedPipeW.Find()`
  returns nil; any future wrong module/proc name fails as a NAMED test.
- `TestWaitNamedPipeLiveEndpointSemantics` — calls the real
  waitNamedPipe against a live served endpoint (own listener, spare
  instance listening per W-INV) and asserts the documented success
  semantics (nil); exercises Find() + the actual kernel32 call
  end-to-end; deterministic, no sleep.

State plainly: local proof CANNOT execute the pins or the dial path
(Linux-only environment); compile-level regression is GOOS=windows
build + vet (both green below); the hosted Windows job is the only
runtime authority for this fix.

## Phase 4 — DOCS + VERSION IDENTITY

changelog.md: the v1.10.11 entry (the four annotation lines quoted
verbatim, the root cause, the two-part fix, W-FULL labeled INFERENCE,
the rule-8 sweep, the protected-surface list). worklog.md: this entry.
UPDATE.md: the "What v1.10.11 changes" section + the v1.10.11 header.
agent.md: the hosted truth (Linux green twice — protected; the Windows
panic fixed, compile-level proof, hosted proof pending; Windows steps
17–32 and the publish job never executed in any run). Version identity:
package.json 1.10.10 → 1.10.11, canonical sync green (config.go
AppVersion, build/config.yml productVersion, SIGNATURE first line),
`release-version.mjs --check` GREEN at 1.10.11; the tracked web bundle
rebuilt so the embedded frontend version matches (verify:web green).

## Phase 5 — FULL LOCAL REGRESSION (gate table; every command executed this session)

| Gate | Command | Result |
|------|---------|--------|
| gofmt | gofmt -l . | CLEAN (empty) |
| Go vet (host) | go vet ./... | PASS |
| Windows compile | GOOS=windows go build ./... | PASS |
| Windows vet (compiles the new pins) | GOOS=windows go vet ./... | PASS |
| Unix localipc | go test ./internal/localipc/ -count=1 | PASS (ok 0.529s) |
| Headless suite | go test ./internal/... -tags headless -count=1 | PASS (exit 0) |
| FULL suite | go test ./... -count=1 | PASS (exit 0) |
| Race gate | go test -race (concurrency-heavy pkgs incl. localipc) | PASS (exit 0) |
| Release metadata | node scripts/release-version.mjs --check | GREEN at 1.10.11 |
| (remaining gates) | npm ci / typecheck / lint / test:units / test:release / build / verify:web / verify-all / stress / bash -n | see the final gate table in the session deliverable — all executed this session |

## Phase 6 — COMMIT + OPERATOR PUSH

Commit prepared in the working tree (binding fix + Find() guard + pins +
docs + version identity + rebuilt web bundle). This environment cannot
push (no credentials) — the OPERATOR pushes main. The hosted run of this
revision is the potential FIRST complete pipeline pass: Windows step 14
green under both runs would unblock steps 17–32 (first-time surfaces) and
then the publish job (never executed in ANY run); fix forward ONLY on
what that run's annotations and summaries name.

## Phase 7 — HOSTED RESULT LOOP

Reserved for the operator's push: read the run page + annotations for
every red step, quote its named evidence, root-cause, fix as a code diff,
hand the commit to the operator, read the next run (~13 min per run;
last fix-forward commit before minute 92). Never mark a gate VERIFIED
without its green job; never fabricate green; never fix what the
evidence does not name.

Per-item outcome table (this cycle's register):

| Item | Outcome | Closure class |
|------|---------|---------------|
| W-PANIC | CLOSED (code) | the headless process panic eliminated at its root by W-BIND + W-FIX; compile-level local proof; hosted run = runtime authority |
| W-BIND | CLOSED (code diff) | kernel32.dll binding (the documented exporting module); winbase was a header, not a DLL; confirmed line-by-line at 5ccbe35 |
| W-TRIG | NOT A DEFECT (protected) | the burst pin legitimately surfaces transient busy; it stays byte-identical — it exposed a real production panic |
| W-FIX | CLOSED (code) | kernel32 binding + Find()-guarded waitNamedPipe + ErrWaitNamedPipeUnresolved; dialEndpoint loop and retry constants unchanged; no LazyProc.Call panic path remains in transport code |
| W-PIN | CLOSED (code) | TestWaitNamedPipeProcResolves + TestWaitNamedPipeLiveEndpointSemantics; compile-blind defect class becomes a named first-run test failure |
| W-FULL | INFERENCE (high confidence) | no win-go-test-full-prefixed FAIL/panic line in run 38984879544's 4 annotations; NOT claimed as verified until the next hosted run's step 14 is green |
| W-BND | OPEN by design | first-time surfaces (Windows steps 17–32, publish job) consumed as evidence-only fix-forward after the operator's push |
| D1 | CLOSED (docs) | changelog v1.10.11 entry (evidence verbatim, inference labeled), this worklog entry, UPDATE.md v1.10.11 section, agent.md hosted truth; canonical identity 1.10.11 --check green |

---
Task ID: 1 (single-agent session, 113-minute budget)
Agent: v1.10.12 engineering session
Task: v1.10.12 — the documented busy-retry contract + ROADMAP shipped-baseline
completion: replace dialEndpoint's fixed 3-attempt busy budget with the
documented Microsoft Named Pipe Client pattern (retry CreateFile while
WaitNamedPipe reports an instance becoming available, bounded by a 10 s
busy-dial deadline), complete ROADMAP.md's shipped baseline (v1.9 + v1.10
delivered paragraphs, stale Phase 2 status sentence, header), write the
v1.10.12 docs entries, set canonical identity 1.10.12, and consume run
39019535982 (v1.10.11, commit e89680b) as the evidence authority.

Work Log:

- Phase 1 — LIVE STATE + EVIDENCE. Live main HEAD is `73761f9` ("Update
  README.md"), PAST the register baseline e89680b; `git diff --stat
  e89680b..HEAD` = README.md only (1 file, 1 insertion, 1 deletion) —
  every register surface is at baseline state. Every register item was
  re-verified against the live tree BEFORE any edit: the busy-loop
  constants (listener_windows.go L197 `pipeBusyWaitMS = 2000`, L201
  `pipeBusyMaxRetries = 3`), the attempt-cap check (L267), the failure
  string (L278), the burst pin (listener_windows_test.go L275
  TestMalformedBurstUnderParallelismNeverSurfacesPipeBusy, 8 workers ×
  4 cycles, DisableKeepAlives, dial → garbage → close → health redial),
  the structural pin (L238 TestTakeInstanceRefillsSpareUnderSameLock),
  the resolution pin (L351 TestWaitNamedPipeProcResolves), the kernel32
  binding (L209-210 modkernel32/procWaitNamedPipeW), the non-panicking
  waitNamedPipe (L229-245, Find() before Call), isVanishedPeer
  (L608-612), and the ROADMAP sentences (L22 header names v1.10.11;
  L43-50 the v1.8 paragraph; L193-199 the stale UNIDENTIFIED sentence).
  Hosted re-read: GitHub REST API returned 403 for every Actions
  endpoint from this environment (unauthenticated, rate-limited) — the
  run page and annotations API were NOT fetchable, so the register's
  verbatim quotes below stand as the hosted evidence, per the same
  contingency the v1.9.5 session recorded.

- EVIDENCE FIRST — hosted annotation lines from run 39019535982
  (v1.10.11, commit e89680b), Windows step 14 "Go verification", the
  headless run, quoted verbatim from the defect register (all 7
  annotations carry the win-go-test-headless prefix; none carry
  win-go-test-full; GitHub truncates annotation text at 250 chars):

```text
--- FAIL: TestMalformedBurstUnderParallelismNeverSurfacesPipeBusy (0.60s)
listener_windows_test.go:329: burst dial: localipc: dial \\.\pipe\sheytan-la-runneradmin-core: no listening pipe instance after 3 busy waits: All pipe instances are busy. (ERROR_PIPE_BUSY surfaced — the instance invariant ...
```
  (emitted twice — two of the eight workers hit it)
```text
FAIL  github.com/Parsaetak/SHEYTAN-local-agent/internal/localipc  9.157s
FAIL
Process completed with exit code 1.
```
  Steps 15–16 ran (failure summary + upload); steps 17–32 skipped; the
  publish job skipped.

- Evidence-class boundary (stated up front, governs every claim below):
  local Windows proof is COMPILE-LEVEL ONLY (GOOS=windows go build +
  go vet; this is a Linux-only environment — it cannot execute the
  Windows dial path, the pins, or any Windows test). The hosted Windows
  job is the RUNTIME AUTHORITY. Local green is not hosted green;
  compile proof is not runtime proof. Linux suite results are RUNTIME
  evidence for the Unix transport only.

- Phase 2 — ROOT-CAUSE CONFIRMED by inspection + arithmetic, exactly as
  the register states. dialEndpoint (listener_windows.go L247-279)
  loops on an attempt counter: on ERROR_PIPE_BUSY it calls
  waitNamedPipe(name, 2000) and retries CreateFile at most
  pipeBusyMaxRetries = 3 times, then fails with "no listening pipe
  instance after 3 busy waits". The 0.60 s failure duration is
  decisive: three full 2000 ms waits cannot fit inside 0.60 s, so EVERY
  WaitNamedPipe returned PROMPTLY (an instance became available each
  time — the server's W-INV consume-and-refill published a listening
  instance on every cycle; the SERVER side is healthy). What failed
  was the CreateFile immediately after each wait: 8 workers race for
  the single listening instance the invariant guarantees; each race
  loser receives transient ERROR_PIPE_BUSY and burns one of only 3
  attempts; losing three consecutive microsecond-scale CreateFile
  races under 8-way contention is normal scheduling, not a server
  defect. The fixed attempt count is the defect.

- DOCUMENTED PATTERN VERIFIED against the current Microsoft Named Pipe
  Client documentation (learn.microsoft.com/windows/win32/ipc/
  named-pipe-client): on ERROR_PIPE_BUSY "the named pipe client uses
  the WaitNamedPipe function to wait for an instance of the named pipe
  to become available"; the documented example is a loop that retries
  CreateFile whenever WaitNamedPipe reports an instance became
  available (20 s wait timeout in the example), with NO attempt count —
  the bound is the caller's own. Stated plainly: replacing the attempt
  cap with a bounded deadline is the DOCUMENTED Microsoft Named Pipe
  Client pattern, the OPPOSITE of test-weakening; the burst pin that
  surfaced the defect stays byte-identical and must still pass — it
  will pass because the dial finally stops surrendering after 3
  lost lottery tickets.

- Rule-8 sweep 1 — every dialEndpoint caller and every reader of the
  busy constants (grep over the whole tree, *.go, at HEAD 73761f9):

| Path | Role | Depends on 3-attempt semantics? |
|------|------|---------------------------------|
| internal/localipc/listener_windows.go:247 | the Windows dial under repair | the surface itself |
| internal/localipc/listener_windows.go:127 | listener `dial` closure assigned `dialEndpoint(endpoint)`; field declared :291; NO readers anywhere in the tree | none found |
| internal/localipc/endpoint.go:150 | cleanStaleEndpoint probe dial — the WINDOWS branch returns EARLY (:148); the call executes on Unix only, resolving to listener_unix.go:16's net.Dial variant | no (different function; build-tagged) |
| internal/localipc/listener_unix.go:16 | Unix dialEndpoint (net.Dial) | no — untouched |
| internal/localipc/listener_windows_test.go:306 | burst pin's direct dialEndpoint calls | no — it asserts DIAL SUCCESS under contention |
| internal/localipc/listener_windows_test.go:373 | TestWaitNamedPipeLiveEndpointSemantics reads pipeBusyWaitMS as its wait timeout | no — pipeBusyWaitMS stays 2000 |
| "no listening pipe instance after %d busy waits" | emitted ONLY at listener_windows.go:278; NO test or code greps the string | no |

  pipeBusyMaxRetries readers: listener_windows.go L201 (def), L267
  (cap check), L278 (message) — nothing else in the tree. No caller
  depends on the 3-attempt semantics; no caller owns a shorter
  deadline today (the 10 s budget is the transport's own default).

- Rule-8 sweep 2 — ROADMAP inventory (grep + full read at HEAD): the
  shipped-baseline section (L20-50) contains the v1.8 delivered
  paragraph and NO v1.9.x or v1.10.x equivalents; the header (L22)
  names v1.10.11; the Phase 2 section carries the v1.10.5-era sentence
  "The Windows `Go verification` failure remains UNIDENTIFIED (the
  step log is admin-gated; ...)" (L193-199) — false since v1.10.7: the
  failure chain has been root-caused and repaired across
  v1.10.7–v1.10.11 (emitter visibility, vanished-peer classification,
  Close-timer stop, W-INV structural invariant, WaitNamedPipe client
  contract, kernel32 binding, non-panicking dial). Both are repaired in
  this release.

- DEFECT + FIX STATEMENT (one paragraph, before editing): the defect is
  the CLIENT's fixed attempt-count budget in dialEndpoint — not the
  burst pin, not the busy condition it surfaces, not the server. The
  fix restructures ONLY that loop: on ERROR_PIPE_BUSY, call
  waitNamedPipe(name, pipeBusyWaitMS); while the wait reports an
  instance becoming available, retry CreateFile immediately with NO
  attempt counter, bounded by a total busy-dial budget of 10 seconds
  of wall clock (a single deadline constant; each wait is clamped to
  the remaining budget so the total stays honest); exit early and
  honestly on ERROR_FILE_NOT_FOUND from the wait (the pipe vanished —
  the server is gone; that error is returned, it is not busy); on a
  wait that times out with no instance (or an unresolved proc —
  waiting is impossible, no spin), surface the ORIGINAL busy error
  honestly; on deadline exhaustion return a typed error that names
  the elapsed budget. pipeBusyWaitMS = 2000 stays. The server side
  (W-INV takeInstance refill under the same lock, preCreate,
  isVanishedPeer, Close-timer) and every pin stay byte-identical.

- Phase 3 — THE FIX (W-BUDGET) implemented in
  internal/localipc/listener_windows.go, ONE function restructured:
  dialEndpoint no longer carries an attempt counter. On ERROR_PIPE_BUSY
  it calls waitNamedPipe(name, pipeBusyWaitMS) and retries CreateFile
  immediately while the wait reports an instance becoming available;
  the only bound is the new pipeBusyDialBudget = 10 * time.Second total
  busy-dial wall clock (each wait clamped to the remaining budget);
  honest early exits: non-busy CreateFile errors return as-is, a wait
  reporting ERROR_FILE_NOT_FOUND returns that error ("pipe vanished
  while busy-waiting" — the old loop broke and mislabeled it as a busy
  exhaustion), a wait that times out with no instance or an unresolved
  proc surfaces the ORIGINAL busy error (no spin on a broken wait
  facility), and deadline exhaustion returns the new typed
  ErrBusyDialBudgetExhausted naming the budget and the elapsed time.
  pipeBusyWaitMS = 2000 stays; pipeBusyMaxRetries = 3 is GONE. The
  ErrWaitNamedPipeUnresolved doc line was updated to the new (still
  honest) semantics. One compile error was caught by the
  compile-level gate itself (uint32/int mismatch on the wait clamp) and
  fixed before any test ran — the gate doing its job. Server side
  (W-INV takeInstance, preCreate, isVanishedPeer, Close-timer), the
  kernel32 binding, the non-panicking waitNamedPipe, and EVERY pin are
  BYTE-IDENTICAL — `git diff` over the test file and over the server
  functions is empty.

- Phase 4 — ROADMAP shipped-baseline completion (R1/R2/R3): the header
  names v1.10.12 with a compact v1.10.12 description prepended to the
  parent chain; "v1.9 delivered" and "v1.10 delivered" paragraphs added
  in the v1.8 paragraph's exact form, using ONLY the register's
  evidence-backed feature lists; the stale v1.10.5-era "remains
  UNIDENTIFIED (the step log is admin-gated...)" Phase 2 sentence
  replaced with the verified repair chain (v1.10.7 visibility →
  v1.10.8–v1.10.9 vanished-peer/Close-timer → v1.10.10 W-INV + client
  contract → v1.10.11 kernel32 binding → v1.10.12 busy-dial budget,
  compile-level local proof only) while KEEPING the honest boundary —
  Windows CI remains NOT VERIFIED until a green hosted run. No
  invented features, no future-phase rewrites, no status flips.

- Phase 5 — D1 docs: changelog v1.10.12 entry (annotation lines
  verbatim, 0.60 s arithmetic, root cause, fix, W-FULL labeled
  INFERENCE, honest hosted boundary); UPDATE.md retitled v1.10.12
  with the new narrative + "What v1.10.12 changes" (the v1.10.11
  changes section retained below it); agent.md retitled to the
  v1.10.12 handoff with the hosted truth (Linux three consecutive
  green runs, Windows FULL clean two consecutive runs as INFERENCE,
  busy-budget fix pending hosted proof, steps 17–32 + publish
  unproven, ROADMAP baseline completed through v1.10.x); two stale
  v1.10.8-era identity claims inside agent.md corrected to 1.10.12.
  Canonical identity 1.10.12: package.json + package-lock.json (both
  version fields) edited, scripts/release-version.mjs repaired
  internal/config/config.go (AppVersion), build/config.yml
  (productVersion), SIGNATURE (first line); --check GREEN. The web
  bundle was rebuilt (npm run build → dist → scripts/sync-web.mjs →
  web/static) and carries exactly one 1.10.12 stamp (index.js);
  verify:web green. The v1.10.10 docs condensation is untouched.

- Phase 6 — FULL LOCAL REGRESSION, every gate with its transcript
  result (evidence class: RUNTIME for Unix suites, COMPILE-LEVEL for
  GOOS=windows):

| Gate | Command | Result |
|------|---------|--------|
| gofmt | gofmt -l . | empty (clean) |
| Windows build | CGO_ENABLED=0 GOOS=windows go build ./... | clean |
| Windows vet | CGO_ENABLED=0 GOOS=windows go vet ./... | clean |
| Unix vet | go vet ./... | clean |
| localipc suite (Unix) | go test ./internal/localipc/... -count=1 | ok 0.536s |
| headless suite | go test ./internal/... -tags headless -count=1 | exit 0, all ok |
| FULL suite | go test ./... -count=1 | exit 0, all ok |
| race | go test -race ./internal/localipc/... ./internal/llm/... -count=1 | ok 1.657s / 528.590s |
| npm ci | npm ci | 139 packages, ok |
| typecheck | tsc --noEmit | exit 0 |
| lint | oxlint . | 0 warnings 0 errors (111 files) |
| unit tests | npm run test:units | 236 tests, exit 0 |
| release tests | npm run test:release | exit 0 |
| web build + bundle | npm run build (tsc -b && vite build && sync:web) | exit 0; index.js carries 1.10.12 |
| verify:web | node scripts/verify-static-assets.mjs | contract satisfied |
| canonical entrypoint | node scripts/verify-all.mjs | PASS — 10 PASS + 2 NOT RUN (native CTest: cmake unavailable; Qt host: Qt6 CMake package not found — environment-limited Linux container, honestly recorded) |
| smoke script syntax | bash -n scripts/qt-desktop-smoke.sh | OK |
| LF policy | grep -c \\r over every changed file | 0 CRLF everywhere |
| release metadata | node scripts/release-version.mjs --check | GREEN at 1.10.12 |

  A Unix localipc regression would have been an automatic
  revert-and-redo: none occurred — the Unix suite is green and
  unchanged. The two NOT RUN suites are environment limits of this
  Linux container (no cmake, no Qt6), recorded honestly as NOT RUN,
  not as PASS; the hosted Windows job remains the runtime authority
  for the Windows dial path, and the hosted Linux job owns the Qt
  host build.

- Phase 7 — COMMIT (this environment cannot push; the OPERATOR pushes
  main). The commit carries: the dial-budget fix
  (internal/localipc/listener_windows.go — the ONLY code change), the
  ROADMAP completion, the D1 docs entries, the version identity
  1.10.12 (package.json, package-lock.json, internal/config/config.go,
  build/config.yml, SIGNATURE), and the rebuilt web bundle
  (web/static). While the hosted run executes, doc edits that depend
  on its result are HELD for the final wording.

## Phase 8 — HOSTED RESULT LOOP

Reserved for the operator's push (this environment has no push
credentials): read the run page + jobs + annotations for every red
step, quote its named evidence, root-cause it, fix it as a code diff,
hand the commit to the operator, read the next run. Expected sequence:
Windows step 14 green under BOTH suites for the first time, then the
never-executed Windows steps 17–32, then — if both platforms are green
— the publish job executing for the FIRST time in the repository's
history. Never mark a gate VERIFIED without its green job; never
fabricate green; never fix what the evidence does not name. NOTHING in
this cycle's register was closed by a hosted run: the hosted loop is
OPEN by design, handed to the operator.

Per-item outcome table (this cycle's register):

| Item | Outcome | Closure class |
|------|---------|---------------|
| W-NAME | CONFIRMED at 73761f9 | hosted annotations quoted verbatim (register); GitHub API 403 from this environment — the quotes stand as evidence, as in the v1.9.5 session |
| W-ROOT | CLOSED (code diff) | the fixed 3-attempt budget replaced by the documented deadline-bounded retry; 0.60 s arithmetic confirmed against the live code; compile-level local proof |
| W-TRIG | NOT A DEFECT (protected) | the burst pin stays byte-identical (`git diff` on the test file is empty) and the Unix suite including all pins is green |
| W-FIX | CLOSED (code) | dialEndpoint restructured to the documented Microsoft Named Pipe Client pattern; pipeBusyDialBudget = 10s; honest early exits; typed ErrBusyDialBudgetExhausted; pipeBusyWaitMS = 2000 unchanged |
| W-FULL | INFERENCE (unchanged) | second consecutive clean full-suite run at e89680b; NOT claimed verified until the next hosted run's step 14 is green |
| W-BND | OPEN by design | Windows steps 17–32 + the publish job remain never-executed first-time surfaces; consumed as evidence-only fix-forward after the operator's push |
| R1 | CLOSED (docs) | v1.9 + v1.10 delivered paragraphs added from the register's lists only |
| R2 | CLOSED (docs) | stale UNIDENTIFIED sentence replaced with the verified chain + NOT-VERIFIED boundary |
| R3 | CLOSED (docs) | shipped-baseline header names v1.10.12 |
| D1 | CLOSED (docs + identity) | changelog/UPDATE/agent/worklog entries; identity 1.10.12 --check GREEN; web bundle rebuilt |

---
Task ID: 1 (v1.10.13 session)
Agent: v1.10.13 engineering session
Task: v1.10.13 — POST-VERIFICATION VISIBILITY + PREDICTED-DEFECT CLEARANCE +
the two-run tag protocol. Repair the post-verification visibility gap proven
live by run 39024607296 (Windows if:failure() steps positioned before the
post-verification chain), fix the five inspection-proven predicted defects
(P-DEPLOY, P-CMAKE, P-PY, P-SUMS, P-ZIP), record the W-QT non-defects, write
the D1 docs, bump the canonical identity to 1.10.13, run the full local
regression, and deliver the complete source ZIP. The environment cannot push:
the commits are prepared; the OPERATOR pushes main (run #1) and then the
v1.10.13 tag (run #2 — the first possible complete pipeline pass, because the
release job carries if: startsWith(github.ref, 'refs/tags/v') and can never
execute on a main push).

## Phase 1 — LIVE STATE + EVIDENCE (hosted record quoted verbatim BEFORE any fix)

Live HEAD vs baseline: `git ls-remote` HEAD == b8817d953096a61c4faa647a799a212fabf5725b ==
refs/heads/main — the local clone IS the baseline commit; zero drift, zero
local modifications (`git status` clean). No tags exist in the repository.

EVIDENCE-CLASS BOUNDARY (stated up front, per the operating rules): local
proof for the Windows C++/Qt surface in this session is STATIC ONLY (YAML
parse, actionlint, script review, shell syntax checks). The hosted Windows
job is the runtime authority; local green is not hosted green. The GitHub
REST API was re-read anonymously from this environment and returned 403
(rate-limited for this IP — the same boundary recorded in the v1.9.5 and
v1.10.12 sessions), so the hosted evidence below stands as the register's
verbatim record of run 39024607296 (Build Desktop #13, push of v1.10.12,
conclusion failure): audit job 117373016836 green; Linux x64 job
117373537371 fully green (FOURTH consecutive); Windows x64 job
117373537389 steps 1–14 green with step 14 "Go verification" green for the
FIRST time in the repository's history, steps 17–21 green on first-ever
execution, and step 22 "Build Qt host (Windows)" failed in 41 s, exit 1.

Hosted annotation, job 117373537389 (W-BND-403), quoted verbatim:

    "Process completed with exit code 1."

— one single annotation, path ".github", line 137 (a nominal position: the
live workflow's line 137 falls inside the audit job's "Verify repository
shape and release identity" shape-check loop, nowhere near the failing
Windows step — the annotation names nothing actionable). The job log
download returns 403 "Must have admin rights to Repository." The exact
failing command inside step 22 — cmake configure, cmake build, acceptance
staging, or windeployqt — is NOT anonymously determinable. HONEST
ROOT-CAUSE BOUNDARY: asserting that P-DEPLOY or any predicted defect CAUSED
run 39024607296's failure is forbidden and no such claim is made anywhere
in this cycle. The predicted-defect fixes are justified by their own
inspection evidence; the W-VIS visibility repair is what will name the real
culprit in run #1.

W-NAME verification against the live tree: the step-22 script (workflow
lines 955–993 at b8817d9) matches the register's verbatim quote
line-for-line (comments elided): $ErrorActionPreference = "Stop"; the
$appRoot derivation from ${env:WIN_PKG_ROOT}; cmake -S ui/qt -B
ui/qt/build -DCMAKE_BUILD_TYPE=Release with a $LASTEXITCODE throw; cmake
--build ... --config Release with a throw; the two-path acceptance-binary
probe (build/Release/ then flat) with the not-produced throw; the staging
Copy-Item with its assert; the windeployqt line aimed at
"$appRoot/SHEYTAN-LA.exe" piped to Out-Null with NO exit-code assertion;
the two-path sheytan-ui.exe staging with SilentlyContinue; the deployed
assert; the closing Write-Host. ZERO drift from the register.

Step-numbering reconciliation (for the record): the register uses GitHub
UI positions (which count the "Set up job" pseudo-step); a PyYAML parse of
the live file numbers build-windows at 31 steps: YAML 13 = UI 14 "Go
verification" (first green), YAML 14–15 = UI 15–16 the two if:failure()
steps, YAML 16–20 = UI 17–21 the first-execution chain, YAML 21 = UI 22
"Build Qt host (Windows)" (the failure). The register is internally
consistent with the live file under that mapping.

W-VIS verification (workflow structure + this run's live proof): the two
if:failure() steps ("Go verification failure summary (Windows)",
"Upload Go verification logs (Windows)") sit at YAML positions 14–15,
BEFORE the post-verification chain 16–31. GitHub evaluates a step's
condition when the step is reached in sequence: at 14–15 the job was still
green, so both reported "skipped" while the run page carried them as
skipped, and the later failure at step 22 triggered nothing — publishing
only the generic exit-code annotation. Proven live by run 39024607296. The
audit job's own failure pair is correctly positioned at its END (steps
13–14 of 14) — the pattern replicated for the Windows job this cycle. The
Linux job carries structurally similar mid-job pairs ("E2E failure summary"
at step 17, "Upload verification logs (Linux)" at step 25 of 32) — RECORDED
as an observed sibling, NOT changed this cycle: the evidence does not name
them, the Linux chain is four-times green, and churning it is forbidden.

P-TAG verification: the release job ("Publish and verify
${{ github.ref_name }}") carries needs: [build-windows, build-linux, audit]
and if: startsWith(github.ref, 'refs/tags/v') (workflow line 2523). Every
run in the repository's history was a push to main, so publish has NEVER
executed — a property of the trigger discipline, not of build health. The
two-run protocol is therefore structural: run #1 = operator pushes main;
run #2 = operator pushes the v1.10.13 tag; the tag run re-executes the full
matrix and then publishes for the first time.

## Phase 2 — CONTRACT VERIFICATION + RULE-8 SWEEPS

Contract verification: (1) Qt's documented deployment model
(doc.qt.io/qt-6/windows-deployment.html) — windeployqt copies dependencies
based on the TARGET binary's imports; run against the non-Qt Go launcher it
deploys nothing useful for the Qt host, and the v1.4.0 gate-integrity rule
(workflow header comments L21–44; the run 35996462352 repair notes at
L650–657) forbids the silent `| Out-Null` with no $LASTEXITCODE assertion
that the windeployqt line carries. (2) The repo's own CMake cache rule
(L312–335 in the audit job, restated at L595–597 for the Windows native
engine step): "a relocatable CMake build tree is NOT a safe cache artifact
— CMakeCache.txt / CMakeFiles embed absolute paths ... wiped and
reconfigured with --fresh on every run" — the Qt host step violates it
(no wipe, no --fresh, no prefix). (3) The green Linux Qt twin (L2053–2060)
passes -DCMAKE_PREFIX_PATH="$Qt6_DIR" explicitly. (4) The e2e fixture
established the platform-robust interpreter precedent
(e2e/fixtures/sheytan-server.ts:193: win32 → "python", else "python3").

Sweep table 1 — every reader of WIN_PKG_ROOT (34 workflow references + the
Go contract mirror): workflow-level definition L82; Windows job readers at
L847/883/960/1000/1191/1214 (staging + smoke + acceptance), L1288/1295/
1318/1329–1352/1375 (ZIP name + entries), L1434/1474/1511 (NSIS staging +
installer), L1543/1544/1551/1552 (artifact upload names/paths); release-job
readers at L2543/2549/2564/2566/2594/2782/2784 (artifact downloads + asset
names); Go mirror internal/releasecontract/releasecontract.go
(WorkflowWinRootEnvLine, WinRootPwsh/WinRootBash/WinRootGHA slot forms).
NONE of these readers depends on the ORDER of steps inside the Windows job;
moving the failure pair does not touch any reader.

Sweep table 2 — every windeployqt call: exactly ONE
(L983, the P-DEPLOY target); docs/ROADMAP.md and docs/changelog.md carry
prose mentions only; ui/qt/CMakeLists.txt carries a deploy-script comment.

Sweep table 3 — every if:failure() step and its position (7 total):
audit L355 (step 13/14) + L397 (step 14/14) — correctly at job END;
build-windows L728 (step 14/31) + L783 (step 15/31) — THE W-VIS DEFECT,
moved to job END this cycle; build-linux L1877 (step 17/32, E2E-scoped) +
L2138 (step 24/32) + L2202 (step 25/32) — mid-job, NOT named by evidence,
RECORDED ONLY.

Sweep table 4 — dependencies on current Windows step ordering: (a) the
ci-logs/ workspace directory is created by "Go verification" (L679) and
read by the failure summary + artifact upload — workspace state persists
across steps, so moving the pair to the job's end preserves both; (b) the
artifact names consumed by the release job
(${WIN_PKG_ROOT}-Windows-x64-v…, …-installer-v…) are produced by the last
two steps and untouched; (c) no step reads another step's console output;
(d) GITHUB_ENV chains (APP_VERSION, WINDOWS_ZIP, LINUX_ZIP, INSTALLER) are
produced/consumed within their own steps or the release job.

Sweep table 5 — the publish job's checksum readers: "Generate SHA256
checksums" (L2695–2706) is the only producer; consumers are the
"Publish GitHub Release" files list (L2763) and the "Verify published
release" asset-name assert (L2801). internal/ Go code reads
release-manifest.json (computed independently from the artifact FILES, not
from SHA256SUMS.txt) — no Go reader of SHA256SUMS.txt exists. REFINED
FAILURE TRACE (recorded honestly, superseding the register's description
of the published-file shape): the step `cd`s to dist/windows (the Windows
ZIP's directory), then runs `sha256sum <win.zip> <installer.exe>` — but the
installer artifact downloads to dist/installer/, so the second operand
resolves to a NONEXISTENT path inside dist/windows; GNU sha256sum exits 1
and `set -euo pipefail` fails the step BEFORE any mv. The step therefore
either fails the release job outright (the trace above) or — if the
operands happened to resolve — would publish a Linux-only sums file (the
register's trace). Both classes are repaired by the P-SUMS rewrite: one
deterministic file, three correct operand paths, line-count + per-asset
assertion.

Sweep table 6 — NSIS installer.nsi contract fragments at HEAD (local
pre-flight; the workflow gate asserts them at build time): MUI_PAGE_DIRECTORY=2,
"Create a &desktop shortcut"=2, BST_CHECKED=3, WriteUninstaller=1,
UninstallString=1, SHEYTAN_DATA_DIR=10, "preserve user data"=2, and the
negative contract "RMDir /r"=0. ALL PRESENT — the pre-flight passes locally;
no hosted run is spent discovering a missing fragment.

Sweep table 7 — every python3 reference under ui/qt: exactly ONE
(ui/qt/tests/tst_acceptance.cpp:80 — the P-PY target, hardcoded
setProgram("python3") with the QVERIFY(waitForStarted(5000)) gate at L84).
The repo's own e2e fixture already establishes the win32 "python" fallback
precedent.

Sweep table 8 — W-QT non-defects recorded (do NOT "fix"): (1) CMake's
documented config-mode search honors <PackageName>_DIR and PATH-derived
prefixes with /bin stripped — install-qt-action's environment does NOT
preclude finding Qt; no discovery gap is claimed from here. (2)
QLocalSocket::connectToServer on Windows accepts a full \\.\pipe\... server
name verbatim (qtbase 6.8 qlocalsocket_win.cpp uses names starting with the
pipe prefix as-is) — no normalization work. (3) Compress-Archive on pwsh
7.4+ is within the fixed-module range for the backslash entry bug
(Microsoft.PowerShell.Archive #48); covered defensively by the new P-ZIP
gate, no packaging rewrite.

Boundary sentence: which command failed inside run 39024607296's step 22 is
UNKNOWN and will be named by the visibility installed this cycle.

## Phase 3 — W-VIS VISIBILITY REPAIR (closed as a CODE/WORKFLOW DIFF)

Implemented in .github/workflows/build-desktop.yml, build-windows job:

1. THE MOVE: the if:failure() pair left its mid-job position (YAML steps
   14–15) and now sits at the END of the job (YAML steps 30–31 of 31,
   after "Upload Windows package"), so if:failure() covers the ENTIRE
   job including the post-verification chain. The summary step is
   renamed "Failure summary (Windows)" (mirroring the audit job's
   end-of-job pair naming); the upload step name and the artifact name
   windows-go-verification-logs-attempt${{ github.run_attempt }} are
   KEPT unchanged (proven surfaces, minimal churn; the artifact name
   remains referenced verbatim by the summary's fallback message).
2. THE TEE + ASSERT DISCIPLINE: cmake configure → ci-logs/win-qt-configure.log;
   cmake build → ci-logs/win-qt-build.log; windeployqt →
   ci-logs/win-windeployqt.log; the acceptance run →
   ci-logs/win-qt-acceptance.log; NSIS makensis → ci-logs/win-nsis.log —
   every one with 2>&1 | Tee-Object (the proven Invoke-Gate pattern from
   the Go verification step, where $LASTEXITCODE still belongs to the
   native command) and an explicit $LASTEXITCODE assertion whose throw
   names the log. choco install nsis is NOT teed (not named by the
   evidence; its failure class is caught by the existing makensis
   existence check).
3. THE GATE-ORDER TRACKER: each new gated step appends RUN/OK/FAIL
   markers to ci-logs/gate-order.txt (plus STEP boundary markers). The
   summary parses the markers: explicit FAIL wins; a RUN without
   OK/FAIL names the gate that started and never finished; all-OK means
   the failure happened outside the tracked gates and the summary says
   exactly that, pointing at the run page's red step. The state machine
   was SIMULATED locally across 7 scenarios (Go-verification failure
   with no markers; each tracked gate failing; a tracked gate starting
   without finishing; an untracked step failing after all gates OK; a
   staging throw between markers) — ALL PASS.
4. THE EXTENDED SUMMARY: header "## Windows job FAILED"; the failing
   gate's name plus redacted, truncated tail lines (≤12 candidates,
   ≤10 annotations total, 250-char cap) of that gate's own log emitted
   as ::error:: annotations FIRST so a cap drop can never hide them.
   Rule-7 redaction (Redact-Anonymous) applies to the NEW logs only:
   named-pipe endpoints (\\.\pipe\... → \\.\pipe\<redacted>) and
   key-shaped assignments (key|token|secret|password[=:]… → <redacted>)
   are masked; the proven Go logs (win-gofmt.log, win-go-test-*.log,
   win-go-vet.log) keep their audited byte-identical summary and
   annotation path, and the gofmt fallback now runs ONLY when no
   post-verification markers exist — the Go-verification failure path
   is unchanged.
5. HONEST SCOPE NOTE: Windows steps before the tracked gates (UI steps
   17–21 — variant assets, resources, exe build, exe verify, Qt
   install) stay BYTE-IDENTICAL (first-time-green, protected) and do
   NOT write markers; a failure there still produces the summary with
   the per-log sections and the exact former Go-failure annotation
   logic (which emits nothing for non-Go failures), while the run page
   names the red step — the same visibility class those steps had
   before, now with whole-job summary coverage. The Linux job's
   mid-job failure pairs are RECORDED (sweep table 3) and NOT changed
   (evidence does not name them; the Linux chain is four-times green).

New build-windows step map (31 steps; UI position = YAML+1): 1 Pin
checkout to LF / 2 Checkout / 3 Setup Node 24 / 4 Verify Node and
repository / 5 Synchronize release metadata / 6 Verify release metadata
/ 7 Install frontend dependencies / 8 Frontend verification / 9 Verify
embedded frontend / 10 Verify stable frontend asset contract / 11 Setup
Go 1.26 / 12 Native engine (C++) build and tests / 13 Go verification /
14 Validate engine variant assets / 15 Generate Windows resources / 16
Build Windows executable and console smoke probe / 17 Verify Windows
executable / 18 Build Qt host (Windows) and deploy runtime
[install-qt-action 6.8.2] / 19 Build Qt host (Windows) [P-CMAKE +
P-DEPLOY + tee/assert + gate tracking] / 20 Desktop runtime smoke / 21
Windows live-answer acceptance [tee + gate tracking] / 22 Prepare
Windows portable package / 23 Create Windows ZIP / 24 Verify Windows
ZIP [P-ZIP gate] / 25 Verify Windows ZIP integrity / 26 Build Windows
NSIS installer [tee + gate tracking] / 27 Verify Windows installer /
28 Upload Windows installer / 29 Upload Windows package / 30 Failure
summary (Windows) [MOVED, extended] / 31 Upload Go verification logs
(Windows) [MOVED]. No duplicate step names (YAML-parsed). The register's
UI-numbered step-22 failure target is YAML step 19 this cycle.

## Phase 4 — PREDICTED-DEFECT FIXES (each closed as a CODE DIFF; each cites its register evidence in the code comments)

P-DEPLOY (register: Qt's documented deployment model follows the target
binary's imports; v1.4.0 gate-integrity forbids Out-Null without an
assert): the deploy block inside "Build Qt host (Windows)" now stages
sheytan-ui.exe into $appRoot FIRST (Release/ path then flat path
resolved, throws when NEITHER exists, no SilentlyContinue), runs
windeployqt --qmldir ui/qt/qml --no-compiler-runtime against
"$appRoot/sheytan-ui.exe" teed to ci-logs/win-windeployqt.log with an
asserted $LASTEXITCODE, then asserts Qt6Core.dll exists beside the host
before declaring success. The acceptance-binary staging block above it
is byte-identical (v1.10.3 R16 logic preserved).

P-CMAKE (register: the repo's own CMake cache rule; the green Linux
twin pins the prefix): the same step wipes ui/qt/build unconditionally
(Remove-Item -Recurse -Force, the native-engine step's exact pattern),
configures with --fresh, and passes
-DCMAKE_PREFIX_PATH="$qtPrefix" where $qtPrefix = $env:Qt6_DIR with a
fallback to $env:CMAKE_PREFIX_PATH and a NAMED throw when neither is
set (refusing an unpinned configure). W-QT is respected: this is belt-
and-suspenders hardening, NOT a claimed discovery repair.

P-PY (register: hardcoded python3 with no fallback; e2e fixture
precedent): ui/qt/tests/tst_acceptance.cpp resolves the fixture
interpreter as a candidate list — Q_OS_WINDOWS prefers python then
python3; everything else prefers python3 then python — starts the first
candidate that reaches waitForStarted(5000), closes and skips one that
cannot start, ASSERTS the choice with
qInfo("acceptance fixture interpreter: %s (candidates tried: %s)"), and
QVERIFY2-fails with a named message only when NO candidate starts.
Later engine failures are never retried here (the real gates own them).

P-SUMS (register: the stranded Windows checksums; refined trace in
sweep table 5): the release job's "Generate SHA256 checksums" step now
iterates the three artifacts FROM THEIR ACTUAL PATHS
($WINDOWS_ZIP, $INSTALLER, $LINUX_ZIP), hashes each from its own
directory via a subshell, appends into ONE SHA256SUMS.txt at the repo
root, and asserts: non-empty, exactly 3 lines, and an awk check that
exactly three well-formed lines name exactly the three published
basenames (each once). The mv line is gone. The step was FUNCTIONALLY
SIMULATED locally (positive: one file, three assets, assertion green;
negative: a missing artifact fails the step with set -e, exit 1) — the
first hosted execution remains the runtime authority.

P-ZIP (register: defensive tripwire only, no packaging rewrite):
"Verify Windows ZIP" collects the entry names as before, then fails
with ::error:: offender lines (≤10 printed) when ANY entry name
contains a backslash, before the required-entry checks that would
otherwise misreport the same condition.

Validation after all five: YAML parse clean; bash -n over EVERY bash
run block in the file clean; the P-SUMS logic simulation green
(positive + negative); the gate-order state machine simulation green
(7 scenarios); duplicate step names: none. pwsh is unavailable in this
environment (no pwsh binary, no actionlint) — the PowerShell edits are
covered by static review only, mirroring the repo's established
local-proof class for Windows-only scripts; the hosted Windows job is
the runtime authority.

## Phase 5 — DOCS + IDENTITY (closed as DOCS + identity surfaces)

changelog.md: v1.10.13 entry prepended (run 39024607296 evidence with
the boundary labeled; W-VIS; the five fixes; W-QT; the two-run
protocol; the protected-surfaces list; the local-proof class). UPDATE.md:
retitled v1.10.13, new release story + "What v1.10.13 changes" (8
numbered items), the v1.10.12 story demoted under "v1.10.12 notes
(parent release — superseded where they describe the current release)".
agent.md: retitled v1.10.13 handoff; the current-release paragraph now
states the hosted truth (Linux green FOUR consecutive; Windows Go
verification green FIRST time; steps 17–21 green on first execution;
step 22 red with an unreadable log; visibility installed this release;
publish tag-gated; the two-run protocol) and v1.10.12 is demoted to
the parent-release slot. ROADMAP.md: v1.10.13 STATUS paragraph added to
Phase 2 (Windows chain proven through step 21, blocked at step 22
pending visibility; publish gated to tags; two-run protocol) — the
protected v1.10.12 paragraphs untouched. The v1.10.10 docs condensation
is untouched. Identity: package.json 1.10.13 → release-version.mjs
repaired internal/config/config.go, build/config.yml, SIGNATURE;
package-lock.json synced via npm install --package-lock-only; web
bundle rebuilt (npm run build + verify:web green); --check GREEN.

## Phase 6 — FULL LOCAL REGRESSION (the gate table)

| Gate | Command | Result | Class |
|------|---------|--------|-------|
| npm install | npm ci --no-audit --no-fund | PASS (139 pkgs) | local |
| typecheck | tsc --noEmit | PASS | local |
| lint | oxlint . | PASS (0 warnings, 0 errors, 111 files) | local |
| test:units | node --test (29 suites) | PASS (0 fail) | local |
| test:release | node --test release-version + codename-gate | PASS (38/0) | local |
| build | tsc -b && vite build && sync:web | PASS (bundle rebuilt) | local |
| verify:web | verify-static-assets.mjs | PASS (stable-asset contract) | local |
| gofmt | gofmt -l . | PASS (empty) | local |
| go vet | go vet ./... | PASS | local |
| headless suite | go test ./internal/... -tags headless -count=1 | PASS (all pkgs ok) | local |
| full suite | go test ./... -count=1 | PASS (65 pkgs ok, 0 FAIL lines) | local |
| race gate | go test -race -tags headless ./internal/api ./internal/agent ./internal/sessions ./internal/contextplan ./internal/histref ./internal/runtime (the workflow's exact list) | PASS | local |
| GOOS=windows | go build (cmd/sheytan-local-agent) + go vet ./... | PASS | local (compile-level; no Windows execution) |
| verify-all | node scripts/verify-all.mjs | PASS (2 NOT RUN: cmake unavailable; Qt6 package not found — environment-limited, recorded) | local |
| stress suite | go build ./scripts/stress-main + stress | PASS (47/0, hangs=0 crashes=0) | local |
| qt smoke script | bash -n scripts/qt-desktop-smoke.sh | PASS | local |
| identity | node scripts/release-version.mjs --check | GREEN at 1.10.13 | local |
| workflow YAML | PyYAML parse + step map + duplicate-name check | PASS (31 Windows steps, pair at 30–31) | local |
| workflow bash | bash -n over every bash run block | PASS | local |
| P-SUMS logic | functional simulation + negative test | PASS / PASS | local |
| gate-order machine | 7-scenario simulation | PASS | local |
| pwsh syntax | pwsh -NoProfile syntax check | NOT RUN (pwsh unavailable) — static review only | local/static |
| actionlint | actionlint | NOT RUN (unavailable) | local |
| C++ compile of tst_acceptance.cpp | g++/Qt6 headers | NOT RUN (no Qt6 toolchain) — static review only | local/static |
| HOSTED run #1 (main push) | operator push | NOT RUN (no push credentials in this environment) | hosted |
| HOSTED run #2 (v1.10.13 tag) | operator push | NOT RUN (follows run #1) | hosted |

## Phase 7 — COMMIT + OPERATOR HANDOFF

The complete change set is committed locally on main (see the commit
below). THIS ENVIRONMENT CANNOT PUSH (no credentials): the OPERATOR
pushes main (hosted run #1 — the visibility repair's first live
proving), and after it is green pushes the annotated tag v1.10.13 on
the SAME commit (hosted run #2 — the full matrix re-executes and the
publish job runs for the FIRST time in the repository's history, with
the P-SUMS contract in force). Fix-forward rule for run #1: quote the
named evidence, root-cause it, fix it as a code diff, never fix what
the evidence does not name, never mark a gate VERIFIED without its
green job.

Per-item outcome table (this cycle's register):

| Item | Outcome | Closure class |
|------|---------|---------------|
| W-GREEN | PRESERVED (protected diff) | Linux chain + Windows steps 1–21 + all pins untouched; `git diff` over the protected regions contains only the sanctioned W-VIS/P-item edits; no gate or assertion weakened |
| W-NAME | CONFIRMED | the step-22 script at HEAD matches the register's verbatim quote line-for-line; hosted annotation quoted in Phase 1 |
| W-BND-403 | OPEN (honest) | the failing command inside run 39024607296's step 22 remains UNKNOWN; no root-cause claim made anywhere; the installed visibility names it in run #1 |
| W-VIS | CLOSED (workflow diff) | pair moved to job end; tee/assert on all five post-verification commands; gate-order tracker; failing-gate annotations (redacted, capped); Go path preserved; new step map recorded above |
| P-DEPLOY | CLOSED (code diff) | windeployqt retargeted at the staged Qt host, teed, asserted, Qt6Core.dll verified |
| P-CMAKE | CLOSED (code diff) | wipe + --fresh + pinned CMAKE_PREFIX_PATH with a named refusal |
| P-PY | CLOSED (code diff) | platform-robust interpreter resolution + asserted choice |
| P-SUMS | CLOSED (code diff + local functional simulation) | one three-asset SHA256SUMS.txt + line/asset assertions |
| P-ZIP | CLOSED (workflow diff) | no-backslash entry tripwire; packaging untouched |
| P-TAG | CONFIRMED + STATED (docs) | release job tag-gated (line 2523 at b8817d9); two-run protocol recorded in changelog/UPDATE/agent/ROADMAP |
| W-QT | RECORDED (non-defects) | no fix applied to any of the three; no wasted cycles |
| D1 | CLOSED (docs + identity) | changelog/UPDATE/agent/ROADMAP/worklog entries; identity 1.10.13 --check GREEN; web bundle rebuilt |
| HOSTED #1/#2 | OPEN (operator) | no push credentials; fix-forward loop handed to the operator per Phase 7 |

---
Task ID: 2 (v1.10.13 post-release session — run 39038248810 response)
Agent: v1.10.13 full-green engineering session
Task: FIRST FULLY GREEN pipeline pass — fix the four verified defects named by run 39038248810's
visibility system (W-PIN, W-SMOKE, W-INST-VER, W-ZIP-LAUNCHER), complete W-VIS-2, restructure
docs/agent.md into the operating manual (W-DOC), keep every protected surface byte-identical,
identity frozen at 1.10.13.

EVIDENCE FIRST (hosted re-read, GitHub REST API, unauthenticated — quoted verbatim):

Run 39038248810 ("Build Desktop" #14, push of v1.10.13, commit c46a9c08683392fa4baa12a2d94de3a22331bff3,
conclusion failure, total 16m16s). Job conclusions: audit=success (2m31s), Linux x64=success (9m50s,
FIFTH consecutive green, ZIP artifact produced 6.32 MB), Windows x64=failure (13m39s), publish=skipped
(main push — the tag gate). Windows check-run annotations, re-read from the API and matching the
register verbatim:

  1. "failing gate: Qt host cmake configure"
  2. "Qt host cmake configure: (no teed log for this gate — see the red step on the run page)"
  3. "Process completed with exit code 1."

EVIDENCE CLASSES (stated up front): local proof for the Windows C++/Qt, Go-compile, and packaging
surfaces is STATIC ONLY (YAML parse, script review, source-level contract verification). The hosted
Windows job is the runtime authority. Local green is not hosted green.

SWEEP 1 — W-PIN root-cause chain (joint evidence, all four legs):
  (a) FAIL marker present in gate-order.txt (annotation 1 names the gate the visibility system
      tracked), (b) win-qt-configure.log ABSENT (annotation 2 is the exact no-teed-log branch of the
      failure summary — had cmake run, the tee would exist regardless of its outcome),
  (c) install-qt-action@v4 live README (re-read this session): "The Qt bin directory is prepended to
      your `path` environment variable." / "you will not need to set the `CMAKE_PREFIX_PATH` CMake
      variable." The action exports NEITHER Qt6_DIR NOR CMAKE_PREFIX_PATH, (d) the pinning guard at
      workflow lines 896-901 is the ONLY code path that writes "FAIL Qt host cmake configure" without
      creating the tee file. The guard was correct discipline on a wrong assumption.

SWEEP 2 — desktop.go spawn/wait/close lifecycle (static, HEAD): Run() = api.New + EnsureSetup →
  localipc.Serve binds endpoint → uiExecutable() resolves sheytan-ui.exe NEXT TO THE CORE
  (filepath.Join(filepath.Dir(self), name)) unless SHEYTAN_UI_EXECUTABLE overrides / "none" sentinel →
  exec.Command(uiPath) with endpoint + ephemeral key in the child env → cmd.Start() → select waits on
  childDone OR signal; uiSpawnTimeout = 0 (disabled: the launcher waits on the child unconditionally);
  child exit → core exits with the CHILD's exit code (exec.ExitError.ExitCode()); signal → SIGTERM to
  child, 5s grace, kill, exit 0. Consequence: the core (built -H=windowsgui) owns NO top-level window;
  the window belongs to the Qt host child; CloseMainWindow() must be delivered to the CHILD, and the
  core's clean exit is the lifecycle CONSEQUENCE of the child's exit.

SWEEP 3 — SHEYTAN-LA.bat references tree-wide (static): repo root has NO SHEYTAN-LA.bat (moved to
  packaging/ in v1.10.7 R-B; audit's shape-check enforces absence at root). References: the audit
  shape-check (packaging/SHEYTAN-LA.bat present, root absent), the Prepare step's staging copy from
  packaging/SHEYTAN-LA.bat into the package root, the ZIP verify step's DEAD gate on
  Test-Path "SHEYTAN-LA.bat" (root), installer.nsi shortcut creation (package-relative, unaffected).

SWEEP 4 — install-qt-action@v4 exports (live README): PATH prepend (bin dir), QT_ROOT_DIR,
  QT_PLUGIN_PATH, QML2_IMPORT_PATH, PKG_CONFIG_PATH, LD_LIBRARY_PATH, IQTA_TOOLS. NO Qt6_DIR, NO
  CMAKE_PREFIX_PATH export. CMAKE_PREFIX_PATH is explicitly the CONSUMER's choice ("you can set it to
  either ${QT_ROOT_DIR} or to ${QT_ROOT_DIR}/lib/cmake").

SWEEP 5 — NSIS VIProductVersion contract (source-level, kichik/nsis master): script.cpp:5005
  `_stscanf(token, "%d.%d.%d.%d", ...) == 4` — exactly four dot-separated integers; build.cpp:1842
  "Error: invalid VIProductVersion format, should be X.X.X.X"; tokens.cpp:336 usage
  `version_string_X.X.X.X`. A three-part "1.10.13" FAILS makensis; installer.nsi:129 correctly carries
  `VIProductVersion "${VERSION}.0"` → the version resource reads "1.10.13.0" while APP_VERSION is
  "1.10.13" → the step-29 gate as written throws BY CONSTRUCTION against a perfect installer.

FIXES IMPLEMENTED (each a minimal diff, register-cited; local proof class:
STATIC — YAML parse, actionlint, script review; the hosted job is the
runtime authority):

| Item | Closure class | Diff |
|------|---------------|------|
| W-PIN | CLOSED (workflow diff) | step 22 reads qmake.exe from PATH (Get-Command), derives the Qt root as the grandparent (<qt>/bin/qmake.exe → <qt>), ASSERTS lib\cmake\Qt6\Qt6Config.cmake (the file find_package(Qt6) consumes), passes -DCMAKE_PREFIX_PATH; qmake-absent fails loudly naming the missing PATH contract; wipe + --fresh + tee + gate markers retained |
| W-SMOKE | CLOSED (workflow diff) | step 23 retargeted: banner/liveness/crash-report assertions unchanged; then bounded 60s wait for a windowed sheytan-ui child (G3); CloseMainWindow() to the CHILD with BOOL checked; child exit in 15s (ignores → Stop-Process + throw); CORE exit code 0 in 30s (desktop.go lifecycle consequence); no-orphan assertion (G5) |
| W-INST-VER | CLOSED (workflow diff) | step 29 compares ProductVersion against "$($env:APP_VERSION).0" citing the NSIS four-part contract (script.cpp "%d.%d.%d.%d" == 4; build.cpp "should be X.X.X.X"); installer.nsi byte-identical |
| W-ZIP-LAUNCHER | CLOSED (workflow diff) | step 27 gates on Test-Path "packaging/SHEYTAN-LA.bat" and REQUIRES "${env:WIN_PKG_ROOT}/SHEYTAN-LA.bat" among the ZIP entries |
| W-VIS-2 | CLOSED (workflow diff) | the Windows failure upload now includes ci-logs/gate-order.txt alongside ci-logs/*.log |
| W-DOC | CLOSED (docs, ordered deliverable) | docs/agent.md restructured into the nine-section OPERATING MANUAL (what-this-is / repository map / verification ladder / evidence classes / code-before-docs / CI contract / push protocol / protected surfaces / where-to-write); every line verifiable from the tree or cited hosted evidence; status narrative retired to changelog/worklog/UPDATE |
| W-PY-OK / W-NSIS-OK / W-SUMS-OK | KEPT BYTE-IDENTICAL | verified at HEAD, untouched |
| W-QT non-defects | RECORDED, untouched | no cycle spent |
| D1 | CLOSED (docs, capped) | changelog.md hosted addendum; UPDATE.md hosted addendum; this worklog entry; NO version bump — identity 1.10.13 |

LOCAL GATE TABLE (this environment: node v24.21.0, npm 11.19.0, python3
3.12.15; go/cmake/Qt/pwsh NOT installed — Go, native, Qt, and all Windows
runtime surfaces are STATIC-ONLY here; the hosted job is the runtime
authority):

| Gate | Result |
|------|--------|
| YAML parse (PyYAML) | PASS |
| actionlint v1.7.7 | PASS (exit 0) |
| release-version.mjs --check | PASS (1.10.13, canonical surfaces + workflow contract) |
| npm ci | PASS (0 vulnerabilities) |
| npm run typecheck | PASS |
| npm run lint (oxlint) | PASS (0 warnings, 0 errors, 111 files) |
| npm run test:units | PASS (236/236) |
| npm run test:release | PASS |
| npm run build + sync:web | PASS |
| verify-static-assets --dist | PASS (web/static mirrors the current build) |
| node scripts/verify-all.mjs | PASS (3 suites NOT RUN: go/cmake/Qt unavailable — honest skips, fail-fast profile) |
| bash -n scripts/qt-desktop-smoke.sh | PASS |
| gofmt -l . / go test / go vet / GOOS=windows | NOT RUN (no go toolchain locally) — hosted authority |
| CMake configure sanity (Qt) | NOT RUN (no Qt toolchain locally) — hosted authority |
| Line endings of edited files | LF verified (no CRLF introduced) |

HOSTED STATUS (honest): run #1 (operator push of main) and run #2 (the
operator's v1.10.13 tag) have NOT happened at this tree shape. No gate
past step 21 is claimed VERIFIED. The two-run protocol is handed to the
operator exactly as the register prescribes.
---
Task ID: 2 (v1.10.13 post-run-#15 cycle)
Agent: v1.10.13 acceptance-evidence cycle
Task: run 39045528337 verdict → W-ACC evidence capture + D1 errors-only summaries + D2 docs cap
Hosted annotations (quoted verbatim, run #15 step 24): "failing gate: Qt live-answer acceptance" /
"Qt live-answer acceptance: (no teed log for this gate — see the red step on the run page)" /
"Process completed with exit code 1.". Classes: W-ACC/W-LOGS/D-DOCS = code diffs at 345266f; hosted
re-read 403 (annotations register-transcribed); launcher root cause UNKNOWN until a run names it
(fix-forward a/b/c pre-planned, none asserted). Change: step-24 try/catch writes the launch
exception/no-output exit code into ci-logs/win-qt-acceptance.log (FAIL marker + throw naming the log);
the five 200-line tail dumps are gone (audit/Windows/E2E×2/Qt-smoke) — errors only, capped 60, sources
named, NOTHING skipped; docs capped to this entry + the changelog addendum; sweeps in the session worklog.
---
Task ID: 3 (v1.10.13 goals cycle)
Agent: v1.10.13 engineering session
Task: W-FLAKE total session ordering (run 39076376006 step 14) + the roadmap Phase 2 Goals Qt surface
Forced-tie repro RED on unfixed tree (-count=50, exact hosted message), Seq repair GREEN (-count=50, -race,
vet); /api/goals verified already routed on localipc via core.Handler() — no route change needed; CoreClient gains
the seven 1:1 goal invokables, Main.qml Goals drawer, tst_acceptance list/round-trip/approve-reject slots.
Local battery green: gofmt -l . clean; headless + full go test; go vet ./...; audit race six; frontend
typecheck/lint/test:units/test:release/build; release-version --check green (1.10.13). Qt build not runnable locally
(no cmake/Qt6; sudo unavailable) — hosted run #1 authority for steps 15-30. Docs capped per D2.

Task ID: 4 (v1.10.13 acceptance gate repair)
Agent: v1.10.13 engineering session
Task: W-LOG annotation architecture + W-ACC3 capture upgrade for run 39101817218
Run 39101817218: Linux step 27 Qt live-answer acceptance exit code 2 (exactly two QtTest slots failed) but
only annotation was Process completed with exit code 2. — no gate name, no failing test lines, no artifact.
Root cause by inspection: bare acceptance binary execution without tee, failure-summary steps positioned BEFORE gate.
Fix (W-LOG): per-gate tee into ci-logs/<gate>.log with PIPESTATUS checked; gate-name ::error:: annotation on
failure; errors-only failure summary AFTER every gate it covers (9 ::error:: lines + overflow annotation);
ONE artifact upload per job after ALL gates. Windows step 22 (W-ACC3): replaced pwsh 2>&1 | Tee-Object with
Start-Process -RedirectStandardOutput/-RedirectStandardError native-handle capture into ci-logs/win-qt-acceptance.out.log
and .err.log, merged with process-facts prelude into ci-logs/win-qt-acceptance.log, keeping try/catch and
empty-output guard. Local regression: gofmt clean, sessions -count=50 green, workflow YAML valid.
