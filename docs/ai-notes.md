# AI Notes

**This is the only file AI agents may freely update for working notes, progress, and session state.**

`docs/Dev-Notes.md` is reserved for the human developer and must not be edited by AI agents unless the human explicitly instructs it in the current prompt.

---

## Prior-session lineage (one line each)

- **#117**: Windows smoke first came alive (app launched, closed cleanly) — diagnosability discipline born.
- **#118 (41060689195)**: gofmt 1.27 pin + preflight tee; collector five-part rewiring; RUNNER_TEMP smoke + ZIP-purity gates; pinned Mesa 26.2.5; release-manifest.json.
- **#119**: Mesa two-DLL smoke-only contract + ImportAudit; updater manifest host repointed to the code repo.
- **#120 (41223763993)**: staging bare red closed both ways; 3-attempt download hardening; titled ::error + tee discipline everywhere.
- **#122 (41239634246)**: NSIS macro-name fix + installer branding/license page/makensis tee/marker assertion; pinned by 4 releasecontract tests.
- **#123 (41245854150)**: quoted `"-DVERSION=…"` fix + `TestNSISDefinesQuotedByWorkflow`; Dev-Notes UI/UX research append (operator-authorized); Phase 0 executed (theme tokens + docs/ui-contract.md).
- **#124 (41256606070)**: quoting fix HELD; the NEXT stacked defect fired — the generator's `!endmacro` terminator; `!macroend` fix + post-write validation + fail-closed Go guard; typed missing-engine state + gate-aware pending diagnostics; UI Phase 1 (composer contract, primary slot, focus return, empty states); all pre-makensis surfaces GREEN.
- **#125 (41304350626, commit c8bad23f7e3e6c052538ad18b16dadc952648d14)**: all pre-makensis surfaces GREEN; the ONE red step was the post-compile marker assertion — makensis compiled the installer but the marker never appeared. #124's `!macroend` and quoted `-DVERSION` fixes HELD.

---

## Session — 2026-11-01 (run #125 deep repair + installer/uninstaller round-trip proof + UI Phase 2) — FINAL HAND-OFF

**Summary and success condition**
- P0 executed: the verified NSIS include-path defect was REPRODUCED and FIXED on real makensis 3.11 (not inferred), the reproduction exposed TWO more stacked defects in the same marker chain (relative `${__FILEDIR__}` under a relative script path; `!echo` output being verbosity-4-only), the installer payload contract was audited against the actual workflow staging tree (stale README.md removed, required-file assertions added, wrong comments corrected), a REAL silent install → user-data preservation → uninstall smoke was added to the hosted Windows job, and the whole chain was pinned by three new relationship-aware release contracts (fail-closed proven). P1 executed: UI/UX Phase 2 (native settled-artifact card + truthful Workspace affordance on Chat AND Agent, stale-reference-safe, settle-path-only) with 9 new tests, plus one REAL latent production deadlock found and fixed on the primary navigation path. P2 executed: the early engine-enumeration warning was traced to the exact pre-gate boot window and reclassified with precise non-pending diagnostics. Full local verification ladder green at the final tree. **Hosted verification remains PENDING: this session has no repository credentials, so the one authorized ordinary commit was created LOCALLY and must be pushed by the operator** — no green-run claim is made (evidence class boundary held).

**Target run/commit and actual live starting HEAD**
- Operator target: run 41304350626 (build-desktop #125), commit c8bad23f7e3e6c052538ad18b16dadc952648d14, refs/heads/main, v1.11.0, Go 1.27.3. Verified live via `git ls-remote`: origin/main HEAD == c8bad23 at session start (main had NOT advanced; ancestry verified; worktree clean). Public run page verified: FAILURE 13m23s, Windows x64 red at step "Build the NSIS installer", every other job green.
- Run #125 verified evidence boundary: the ONLY red step is the post-compile marker assertion; makensis itself exited 0 and wrote the installer (never describe this as an NSIS compiler failure); warning 7010 (`README.md` no files found, installer.nsi:285) was the only visible warning.

**Baseline (recorded at c8bad23 BEFORE edits; class local-build/local-runtime, go1.27.3 linux/amd64, GOTOOLCHAIN=local)**
- `gofmt -l cmd internal ui` EMPTY; `go build ./...` GREEN (user-space CGO sysroot rebuilt at /tmp/sysroot — session-local tooling only); `go vet ./...` CLEAN; `go test ./internal/... -tags headless -count=1` 67 ok / 0 FAIL; `go test ./internal/releasecontract/` ok; race 7/7 critical packages ok; `go test ./ui/fyne/` ok. Baseline matches run #125's green surfaces — no pre-existing local red.

**Exact root cause and the include-path fix (reproduced on real makensis 3.11 — evidence class local-runtime; hosted Windows stays the definitive compiler gate)**
- Local reproduction used the REAL installer.nsi, the REAL workflow generator logic, and the exact CI invocation shape (repository-root CWD, relative script path). Result, byte-for-byte the run #125 shape: makensis exit 0, installer written, warning 7010, NO marker — and the `/nonfatal` include miss is SILENT on 3.11 (not even a warning), which is why the #125 transcript shows only 7010.
- THREE stacked defects, all in one chain, all fixed:
  1. `!include /nonfatal "payload-uninstall.nsh"` resolves against the compiler's working directory (plus NSIS include dirs), NOT the script directory. Fix: `!include /nonfatal "${__FILEDIR__}\payload-uninstall.nsh"` (installer.nsi, with the root cause documented in-file).
  2. `${__FILEDIR__}` mirrors the script-path SPELLING: a relative script path makes it relative and the include still misses (reproduced: warning 7000). Fix: the workflow passes the ABSOLUTE resolved script path (`$nsiAbs = (Resolve-Path $nsi).Path`) — completing the v1.2.0 absolute-path contract for the third makensis path (BUILDDIR/OUTFILE/script).
  3. The marker is `!echo` output and makensis prints `!echo` ONLY at verbosity 4: at the default verbosity the marker NEVER reaches the tee'd log, so the hosted marker assertion was unsatisfiable on every run so far (explains why the marker has never fired hosted). Fix: the invocation carries `"-V4"`, plus two additive fail-closed transcript assertions (`Processing script file`, `Output:`) with titled ::error.
- End-to-end mechanism proof: with the resolving include the uninstaller compiles **76 instructions**; with the miss, **65** — the 8-Delete/2-RMDir payload list is physically absent from the uninstaller without the fix. Verification matrix (real 3.11): repo-root + relative script path = miss (proves the invocation fix is required); repo-root + ABSOLUTE script path + -V4 = marker PRESENT, **zero warnings**; developer shape `cd packaging/nsis && makensis installer.nsi` with generated file = marker PRESENT; without the generated file = compiles (the intentional `/nonfatal` developer behavior preserved). Define/macro names, marker text, `!macroend`, quoted `-D` args: all unchanged.

**Installer payload audit and README warning disposition (class local-static over the workflow + tree)**
- Ground truth: the staging tree at NSIS time contains EXACTLY SHEYTAN-LA.exe, SHEYTAN-LA.bat, LICENSE.md, SIGNATURE, README.txt, BUILD-INFO.txt, models/README.txt, workspace/README.txt (+ models/ and workspace/). NOTHING is excluded before makensis — the old installer.nsi comment claimed the opposite and was corrected. One payload root provably feeds three consumers: portable ZIP, generated uninstall manifest, installer `File /r`.
- installer.nsi changes: stale `File /nonfatal "${BUILDDIR}\README.md"` REMOVED (README.md is never staged — the warning 7010 source; the canonical install README is README.txt); new fail-fast `/FileExists` assertions for README.txt and BUILD-INFO.txt (verified: removing README.txt aborts makensis exit 1 with the exact message); SIGNATURE stays deliberately optional; the two explicit required `File` lines kept as a second guard layer; the uninstaller's hardcoded `Delete "$INSTDIR\README.md"` deliberately RETAINED as legacy fallback (harmless no-op on modern installs).

**Hosted Windows install/uninstall smoke (added; the assertions it will make on the next run — class local-static for the step contract, hosted for its first execution)**
- NEW step "NSIS install/uninstall smoke (payload symmetry + data preservation)" between "Verify the installer exists" and the package gate: real silent install of the ACTUAL compiled installer (`/S`, final unquoted `/D=` into a unique GUID path under RUNNER_TEMP, bounded 180s) → installed-payload assertions (every staged file + icon + Uninstall.exe; SIGNATURE only when staged) → ARP entry (both registry views — the x86-unicode installer writes the WOW64 view), AUMID `SHEYTANx13.SHEYTAN-LA`, Start Menu + desktop shortcuts (silent default CHECKED) → 7 install-local sentinels (data root, data\models/sessions/logs/workspace, top-level models/ and workspace/) + external-data sentinel + a legacy README.md, SHA-256 hashed → silent uninstall (`Uninstall.exe /S _?=<path>`, bounded 120s) → every payload file gone, legacy README.md gone, Uninstall.exe present (documented `_?=` behavior), ARP/AUMID/shortcuts gone, EVERY sentinel byte-identical, staging tree untouched. Transcript via Start-Transcript → `ci-logs/win-nsis-install-uninstall-smoke.log`, uploaded by the existing always() windows-logs-part upload on red AND green; every failure emits titled `::error::NSIS install/uninstall smoke (<stage>): …`; cleanup strictly bounded to the unique fixture root. No sleeps, no retry storms, no unbounded waits.

**Release-contract additions (all green; fail-closed proven)**
- `nsiCodeLines` helper (NSIS comment lines stripped — a comment quoting a forbidden directive neither satisfies nor fails a contract).
- `TestNSISPayloadUninstallIncludeResolvesFromScriptDir`: rejects the bare include, requires ${__FILEDIR__} + the generator/include same-file relationship (`packaging/nsis/payload-uninstall.nsh`) + the absolute script path + the -V4 invocation + the marker inside the !ifdef branch with the `_FILES` insertmacro. FAIL-CLOSED PROVEN: reintroducing the bare include made it FAIL with the full root-cause message; restoring made the suite ok.
- `TestNSISUninstallerIsDataSafe`: no `RMDir /r`, no `Delete /r`, no `Remove-Item`, no install-root/data-root wildcards; plain `RMDir "$INSTDIR"`; generator must enumerate the pristine tree, reject `data\*` and both Mesa DLLs, sort directories bottom-up, emit only ordinary Delete/RMDir.
- `TestNSISInstallUninstallSmokeEnforcedByWorkflow`: the smoke step exists, is ordered after "Verify the installer exists" and before the package gate, and carries every mandated mechanism (real installer, /S + final /D=, _?= uninstall, all sentinel paths, hash comparison, ARP/AUMID/shortcuts, titled ::error, teed transcript, bounded timeouts, RUNNER_TEMP-bounded cleanup, always() upload).
- All pre-existing contracts remain green (6 NSIS contracts + license/version/retirement suites).

**UI Phase 2 — Artifact / Workspace Integration (class local-build/local-runtime via the Fyne test canvas; hosted Xvfb remains the real-driver gate)**
- NEW `ui/fyne/artifact_card.go`: the native-first settled-artifact card — kind · title line, one bounded single-line plain-text excerpt (Label = text-only, safe by construction), the TRUTHFUL Workspace status (closed/open in browser loopback; eviction degradation), and the contextual Open-in-Workspace action. Settle-path-only promotion (`promoteSettledAssistant` on BOTH chatView and agentView): the live bubble can never promote itself; empty/non-assistant content registers nothing; a rebuilt view re-seeds from the hub projection; a stale (evicted) reference re-resolves at open time and degrades VISIBLY (status + disabled action) instead of opening a blank surface. `syncWorkspaceAffordance`: the general Workspace entry is actionable only once a settled artifact exists (no blank surface). The hub gained a mutex-guarded `Latest()` projection + `Running()` + `RegisterAgentResponse` (agent gets the same affordance; Source records ui/chat vs ui/agent; openWorkspaceURL factored out). No webview, no new authority, no state machine in the UI; Workspace security untouched (loopback-only, fragment token, CSP, zero commands — server code unchanged).
- REAL latent production deadlock found and fixed: `shell.show()` built a lazy view while holding the shell mutex, and the view's `shell.register()` re-took it — the FIRST navigation to a not-yet-built view froze the UI thread (production shape: non-nil shell; earlier tests built views detached from a shell, and the Xvfb smoke never clicks navigation — that is why CI never caught it). Build now runs outside the critical section; pinned by `TestShellShowLazyBuildDoesNotDeadlock`.
- `presentation.SelectSurface` stays pure; the deterministic mapping test now also pins `Kind("")` and `Kind("\n")` → native.
- docs/ui-contract.md: contract #8 recorded as LANDED (Phase 2) with the exact tests; Phases 3–5 deferred with reason. docs/FEATURES.md: one line for the new user-visible native artifact card. ARCHITECTURE.md: no change (no architectural shift — the card renders the existing store).

**Startup engine-enumeration warning — status (class local-static; the run #125 timestamps explained)**
- Traced to the exact code path: the accelerator evidence gatherer is poll-driven (`/api/perf` / `/api/engine` refresh the memo); at 12:12:06.959 the first UI poll raced ahead of `EnsureSetup`'s gate arming (dirs + installer + hardware warm happen BEFORE `startStartupMaintenance`), so `gateValue()` was still nil and the typed missing-engine failure fell to the generic WARN — 0.9s before the gate's own decision log. The probe is intentional (no new worker, no reorder needed); the gap was the missing PRE-GATE disposition.
- Fix (typed, additive): third disposition `enumReportPreGate` — typed missing-engine + gate-not-yet-armed → precise INFO ("startup engine probe: engine binary not present yet (pre-maintenance window: the startup gate has not announced a phase) — the startup gate owns the next step"), same changed/unchanged chatter discipline. NOT labeled pending (no phase exists yet), NOT suppressed, and every other class unchanged: in-flight → pending INFO; gate armed in READY/DEFERRED/FAILED/BLOCKED → genuine WARN; non-missing errors → WARN; `ErrEngineBinaryMissing`, bounded failure cache, and the gate-aware tests preserved (the nil-gate case updated deliberately in the same batch, with the run #125 evidence recorded in the test comment).
- Vulkan evidence boundary (unchanged): no model was selected in run #125 → prewarm deferred, AUTO Vulkan candidate evaluation skipped, no GGUF loaded, no GPU inference; nothing in this session claims Vulkan execution.

**Full local verification ladder at the final tree (class local-build/local-runtime)**
| Gate | Command | Result |
|---|---|---|
| formatting | `gofmt -l cmd internal ui` | EMPTY |
| build | `go build ./...` (user-space sysroot) | GREEN |
| vet | `go vet ./...` | CLEAN |
| headless core | `go test ./internal/... -tags headless -count=1` | 67 ok / 0 FAIL |
| installer contracts | `go test ./internal/releasecontract/ -count=1` | ok (incl. 3 new guards; fail-closed proof run) |
| api suite (perf change) | `go test ./internal/api/ -tags headless -count=1` | ok |
| race (7 critical pkgs) | `go test -race -tags headless -count=1 …` | 7/7 ok |
| stress | `go run -tags headless ./cmd/sheytan-local-agent stress` | `STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0` |
| collector fixtures | `bash scripts/ci/consolidate-run-log-fixtures.sh` | 11 pass / 0 fail |
| Fyne UI suite | `go test ./ui/fyne/ -count=1` | ok (incl. 9 new Phase 2 tests) |
| desktop sweep | `go test ./... -count=1` | **70 packages ok / 0 FAIL** |
| NSIS matrix (makensis 3.11) | scripts/nsis_repro.sh / nsis_verify.sh | fixed shapes GREEN, zero warnings; negative tests abort exactly |
| Dev-Notes | `git diff --exit-code -- docs/Dev-Notes.md` | UNCHANGED |

**File-by-file change list (16 files; +2 new)**
- `packaging/nsis/installer.nsi` — include fix + payload audit + truthful comments (P0).
- `.github/workflows/build-desktop.yml` — absolute script path + `-V4` + transcript assertions + the install/uninstall smoke step (P0).
- `internal/releasecontract/installer_contract_test.go` — nsiCodeLines + 3 new relationship-aware contracts (P0).
- `internal/api/perf.go` — pre-gate disposition + precise reporter (P2).
- `internal/api/engine_enum_pending_test.go` — pre-gate classification pin (P2).
- `ui/fyne/artifact_card.go` (NEW) — native artifact card + affordance sync (P1).
- `ui/fyne/artifact_card_test.go` (NEW) — 9 Phase 2 tests incl. the navigation-deadlock regression (P1).
- `ui/fyne/workspace_hub.go` — Latest()/Running()/RegisterAgentResponse/openWorkspaceURL factoring (P1).
- `ui/fyne/view_chat.go`, `ui/fyne/view_agent.go` — settle-path promotion + card + truthful Workspace entry (P1).
- `ui/fyne/shell.go` — navigation deadlock fix (P1, discovered by Phase 2 tests).
- `ui/fyne/app.go` — shell-owned chat/agent view seam (P1).
- `internal/presentation/presentation_test.go` — empty-kind mapping pins (P1).
- `docs/ui-contract.md`, `docs/FEATURES.md`, `docs/ai-notes.md` — contract landing + capability line + this hand-off.
- NOT touched: docs/Dev-Notes.md (verified byte-identical), LICENSE.md, internal/config (1.11.0), internal/workspace (security surface), internal/updater, Mesa pins, quoted -D, `!macroend` generator.

**Deliverables (mechanically verified at placement)**
- `SHEYTAN-LA-v1.11.0-run41304350626-fix-source.zip` — source-only ZIP of the committed tree, single top-level `SHEYTAN-LA/` root; excludes `.git/`, dist/, logs, runtime data, engine binaries, Mesa DLLs, and the generated `payload-uninstall.nsh`. Byte size and entry count recorded beside the archive in the update note (a byte-exact self-reference inside the archive is unstable by construction); `unzip -t` full extraction clean.
- `SHEYTAN-LA-v1.11.0-run41304350626-UPDATE-NOTE.md` — run identity, the three-defect chain + transcripts, smoke step spec, Phase 2 changes, verification table with evidence classes, remaining gates.
- `SHEYTAN-LA-run125-fix.patch` — `git format-patch` single commit bundle (applies with `git am` to c8bad23) for the operator's push.

**Hand-off**
- Done: everything above; the local ladder is green at the final tree; the authorized ordinary commit exists LOCALLY on top of c8bad23 (no force, no tag, no release, no remote mutation).
- Remaining (honest): (1) **hosted verification is PENDING** — no repository credentials in this session; the operator must push the prepared commit (or apply the patch bundle) and watch the new build-desktop run: the Windows x64 job must now show makensis at -V4, the marker in the tee'd log, the install/uninstall smoke PASS, and the package gate. Never claim green without that observed run. (2) The install/uninstall smoke's first hosted execution is also its first real-Windows proof (UAC/elevation behavior on the hosted runner is the one environment variable this smoke cannot prove locally; failures will be titled and diagnosed in `ci-logs/win-nsis-install-uninstall-smoke.log`). (3) Physical Vulkan test with a real device + selected GGUF (operator's standing external gate). (4) UI Phases 3–5 remain deferred (operator's phase order).
- Risks: (a) a stacked makensis/NSIS-version difference behind the -V4 behavior — the 3.11 reproduction is strong evidence, but choco could deliver a different 3.x; the transcript contract asserts the exact three greps so any drift is one-look diagnosable; (b) the smoke's ARP assertion accepts either registry view (32-bit WOW64 view is the expected one for an x86-unicode NSIS binary); (c) the artifact card is canvas-class evidence locally — hosted Xvfb stays the visual gate; (d) `shell.show()`'s unlocked build relies on navigation callbacks running on the single UI event loop (true for Fyne) — if a future caller builds views from a background goroutine, the map write must be re-guarded.
- Recommended next: review the diff c8bad23 → the prepared commit (16 files, all listed above) → push → inspect the new Windows x64 job (makensis transcript + smoke log + consolidated five-part log) → if green, the #125 defect class closes permanently and the installer artifact gains a real round-trip proof on every future run.
- Deferred (with reason): UI Phases 3–5; no engine-reorder change (the pre-gate reclassification answers the timing question with evidence; no engine process can start before the gate permits — that v1.6.0 invariant was not touched); no version bump anywhere.

---

*This file is proprietary material under the Proprietary License.*
