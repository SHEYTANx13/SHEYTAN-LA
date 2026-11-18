# Requirements Traceability — Stability & Advanced UI/UX Reports → Code

> Provenance note (recorded honestly): the two source reports
> (`SHEYTAN-LA-Stability-and-Roadmap.md`,
> `SHEYTAN-LA-Advanced-UI-UX.md`) were **not attached** to the 2026-11-16
> integrity-first milestone session; this matrix is built from the
> requirement-area enumeration of those reports as bound into the
> milestone work order (its §13), and each row was verified against the
> actual repository at commit `124b08a` (branch
> `work/v1.11.1-integrity-stability-uiux` for the milestone changes).
> When a report's full text becomes available, rows must be re-verified
> against it — that re-verification is an explicit open item, not a
> completed one. The 2026-11-16 hybrid-native-UI milestone (run-43862893851
> follow-up) contributes its rows under a dedicated section below; the same
> provenance rule applies to them.

Statuses (exact set): `IMPLEMENTED_AND_TESTED`, `EXISTING_AND_VERIFIED`,
`FOUNDATION_IMPLEMENTED`, `STAGED_WITH_GATE`, `BLOCKED`.

## Stability report (SHEYTAN-LA-Stability-and-Roadmap.md)

| Report § | Requirement area | Discovered code path / evidence | Status | Owning test(s) | Doc destination | Phase |
|---|---|---|---|---|---|---|
| §1–2 | Product boundary, repository audit, preservation | Repo map intact: Go core + Fyne client + managed llama.cpp/native engine; no authority duplicated (`docs/ARCHITECTURE.md` authority table) | EXISTING_AND_VERIFIED | full test gates (see note.md verification matrix) | ARCHITECTURE.md | Now |
| §3–4 | Native UX, desktop/tablet/phone, design tokens, a11y, RTL | `ui/fyne` responsive size-class policy, button-fit matrix, structural goldens, `ui_marshal.go` threading seam, theme tokens; five-platform rollout staged | EXISTING_AND_VERIFIED (supported UI) / STAGED_WITH_GATE (five-platform) | `TestResponsiveViewportMatrix`, `TestButtonFitMatrix`, `TestShellChromeButtonsFit` | `docs/ui-contract.md` | Supported UI now; five-platform staged |
| §5 | Module boundaries: core UI-framework independent | **Zero `fyne.io/fyne` imports under `internal/` and `cmd/`** (verified by search 2026-11-16); UI adapters observe state and call the local API | EXISTING_AND_VERIFIED | `go vet ./...`, `internal/...` headless gates | ARCHITECTURE.md | Now |
| §6 | Generative UI and artifacts | `internal/presentation` (typed store + validation), `internal/studio` / `internal/workspace` as read-only viewers over Go-owned data; bytes separate from conversation JSON | EXISTING_AND_VERIFIED | `presentation_test.go`, `viewer_test.go`, `server_test.go` | CONTENT-INTEGRITY.md §4 | Foundation now; modality expansion staged |
| §7 | SPCM (personal compute mesh) | No mesh code (correctly absent); boundary documented as future work with security invariants | STAGED_WITH_GATE | n/a (no code — by design) | ROADMAP.md | LAN MVP later |
| §8 | Offline inference; detected/available/selected/executed/verified GPU states | `internal/llm` (capabilities, devices, admission), `internal/native/engine`, `internal/accelerator` (execution-truth contract) | EXISTING_AND_VERIFIED | `execution_truth_v186_test.go`, `devices_missing_test.go`, `gpu_activation_v186_test.go` | engine/platform docs | Existing verified; new runtimes staged |
| §9 | Security/reliability/data ownership/self-improvement | Approval authority (`internal/approval`), os.Root path safety across stores, atomic writes, idempotent events, migrations; **v1.11.1 adds recoverable session delete/restore/purge** | IMPLEMENTED_AND_TESTED (sessions class) + EXISTING_AND_VERIFIED (controls) | `trash_test.go` (9), `sessions_trash_api_test.go`, pathsafety suites | CONTENT-INTEGRITY.md | Core controls now; full sync later |
| §10 | WebAppHost and Godot/full 3D as separate runtimes | `internal/workspace` (loopback, token-gated, CSP-strict, zero commands) and `internal/studio` (read-only three.js viewer) are exactly that boundary; no embedded browser in the shell; the hybrid milestone adds the native WebView CONTRACT `internal/webapphost` (policy enforced + tested; adapters STAGED_WITH_GATE) | EXISTING_AND_VERIFIED (workspace boundary) / FOUNDATION_IMPLEMENTED (webapphost contract) / STAGED_WITH_GATE (native adapters) | `workspace/server_test.go`, security suites, `webapphost` 8/8 policy tests | ARCHITECTURE.md, WEBVIEW-CAPABILITY-MATRIX.md | Interfaces now; runtimes later |
| §11–13 | Roadmap, DoD, CI/release/dependencies/update rollback | `internal/releasecontract` (version, workflow-DAG, artifact-naming, packagegate, installer, oversized-asset contracts), independent CI gates, `internal/updater` (staged install, rollback identity) | EXISTING_AND_VERIFIED | `releasecontract` suite (green 2026-11-16), `rollback_identity_v181_test.go` | ROADMAP.md, changelog.md | CI/release safety now; five-platform release staged |
| §14–16 | Risks and agent contract; no-main, no-hacks, no false runtime claims | `docs/agent.md` (non-negotiable rules), Dev-Notes governance, evidence-class discipline (`local-static`/`hosted`/`physical`) | EXISTING_AND_VERIFIED | the gates themselves + agent manual | agent.md, Dev-Notes.md | Now |

## Advanced UI/UX report (SHEYTAN-LA-Advanced-UI-UX.md)

| Report § | Requirement area | Discovered code path / evidence | Status | Owning test(s) | Doc destination | Phase |
|---|---|---|---|---|---|---|
| §1–4 | Renderer strategy; Go-Gui candidate; Gio contingency; native-Go rule | Fyne v2.8.2 production (`ui/fyne`, pinned, hosted gate green); **Go-Gui v0.96.0 POC (v1.11.1)** under `experiments/ui-engine-poc/go-gui` (nested module, 5/5 headless app tests + screenshot); Gio not started (contingency only) | FOUNDATION_IMPLEMENTED (POC) | POC `main_test.go` (5 PASS), `go test ./ui/fyne/` gate | UI-PLATFORM-AUDIT.md, UI-ENGINE-DECISION.md | POC now; switch gated |
| §5 | Generative UI schema/catalog/validation | `internal/presentation` typed store; no model-supplied code/callbacks anywhere | EXISTING_AND_VERIFIED | `presentation_test.go` | CONTENT-INTEGRITY.md §4 | Foundation now |
| §6 | Event/stream lifecycle: IDs, sequence, dedup, replay | `internal/api/ws.go` hub (conflation-tested), run transport/settlement, chat settle order | EXISTING_AND_VERIFIED | `hub_conflation_v185_test.go`, `run_settlement_test.go`, `chat_settle_order_test.go` | api docs | Hardened now |
| §7 | Provider capability registry; manual choice preserved | `internal/llm/capabilities.go`, `modelcaps.go`, `internal/recommendation` (evidence-based, AUTO-only bias; manual untouched) | EXISTING_AND_VERIFIED | `capabilities_test.go`, `preferfastest_v1111_test.go` | capability docs | Now |
| §8 | Context/retrieval/memory provenance and control | `internal/memory` (trust boundary), `internal/recall`, `internal/contextcache`, `internal/histref`, `internal/contextplan` | EXISTING_AND_VERIFIED | `trust_test.go`, `recall_test.go`, `contextplan` suites | CONTENT-INTEGRITY.md §4 | Mutation safety now |
| §9 | Multimodal artifacts: metadata+blob separation, bounded decode | `internal/artifacts`, `internal/tools` (staged tx), `internal/vision`, `internal/attachments` | EXISTING_AND_VERIFIED (core) / STAGED_WITH_GATE (new decoders) | `archive_staged_tx_test.go`, `artifacts_test.go`, `vision` suites | CONTENT-INTEGRITY.md §4 | Store foundations now |
| §10 | 3D scenes/games as separate validated runtime | `internal/studio` read-only viewer only; no scene-script execution | STAGED_WITH_GATE (runtime) / EXISTING_AND_VERIFIED (boundary) | `viewer_test.go` | ROADMAP.md | Staged |
| §11 | Agent UI/source improvement: preview/accept/revert, isolated worktree | `internal/lab` (safeedit, verifier, repair, pathjail) | EXISTING_AND_VERIFIED | `safeedit_test.go`, `repair_test.go`, `pathjail_v1111_test.go` | CONTENT-INTEGRITY.md §4 | Safety contract now |
| §12 | Tools/plugins/MCP/computer use: typed schemas, authorization | `internal/customtools` (SSRF policy + dialer guard), `internal/tools`, `internal/mcp`, `internal/computer`, `internal/sandbox` | EXISTING_AND_VERIFIED (core) / STAGED_WITH_GATE (plugin market) | `ssrf_policy_test.go`, `ssrf_dialer_guard_test.go`, `customtools` suites | SECURITY.md | Existing tool safety now |
| §13 | Security/privacy: injection containment, secure storage, SSRF/path safety | `internal/logging/redact.go`, SSRF allowlist, os.Root path safety across stores, prompt-injection tests | EXISTING_AND_VERIFIED | `redact_test.go`, pathsafety suites (9 packages), `goals_log_injection_v1111_test.go` | SECURITY.md, RUNTIME-DIAGNOSTICS.md | Now |
| §14 | Design system, accessibility, i18n, RTL, reduced motion | `ui/fyne/theme.go` tokens, `ui-contract.md` (15 landed contract items), Persian/RTL content paths exercised in tests; full AT tree coverage NOT claimed (Fyne a11y is early opt-in) | EXISTING_AND_VERIFIED (existing controls) / STAGED_WITH_GATE (full AT matrix) | `ui/fyne` suites incl. goldens | ui-contract.md | Existing UI now |
| §15–17 | Concurrency, frame budgets, UI-thread ownership, benchmarks | `ui_marshal.go` (the ONE fyne.Do seam, test-mode serialization), bounded workers, startup phase telemetry (`internal/startup`), bench tests in core packages; P50/P95/P99 UI frame stats NOT measured in this pass | EXISTING_AND_VERIFIED (ownership) / STAGED_WITH_GATE (frame-time percentiles) | `workspace_hub_test.go`, `lifecycle_test.go`, startup tests | UI-ENGINE-DECISION.md, RUNTIME-DIAGNOSTICS.md | Ownership now; measurement staged |
| §18–21 | Implementation order, pinned audited dependencies, deliverables, truthful milestone report | go.mod: every direct dep pinned at audited tags (Fyne v2.8.2, x/sys v0.50.0, …); `go mod verify` + tidy idempotent; delivery = source ZIP + completed note.md | EXISTING_AND_VERIFIED | `go mod verify`, version purity tests, releasecontract | changelog.md, note.md | Now |

## Hybrid native-UI milestone (v1.11.1, run-43862893851 follow-up)

| Requirement area (work order §4) | Discovered code path / evidence | Status | Owning test(s) | Doc destination |
|---|---|---|---|---|
| Fyne compatibility / production shell untouched | `ui/fyne` unchanged in this milestone; hosted Fyne gate green at the run-43862893851 baseline; responsive/boundary/degenerate matrices already shipped | EXISTING_AND_VERIFIED | hosted Fyne UI gate; `shell_responsive_test.go` | UI-PLATFORM-AUDIT.md |
| Go-Gui v0.96.0 advanced workspace | POC extended to 10/10 incl. the mandated §4.4.5 responsive matrix; a real 640x480 containment defect found and fixed (900-unit narrow breakpoint) | FOUNDATION_IMPLEMENTED (POC) | `responsive_test.go` (matrix, screenshots, 2x proxy, close/reopen, focus at floor) | UI-PLATFORM-AUDIT.md, UI-ENGINE-DECISION.md |
| Fyne-side launch action for the Go-Gui workspace | not added: the authoring host cannot compile `ui/fyne` (missing CGO headers); no blind edit made | STAGED_WITH_GATE (prerequisite: a compile-capable host) | n/a — recorded, never claimed | UI-ENGINE-STACK.md §3 |
| GoGPU/WGPU graphics surface | isolated POC module (gogpu v0.54.1); deterministic spectrum; software render + bounded Xvfb windowed soak verified; GPU HARDWARE NOT RUN; separate surfaces, no bridge | FOUNDATION_IMPLEMENTED (software path) / STAGED_WITH_GATE (hardware, frame metrics) | gogpu POC 2/2 + screenshot/soak runs | GO-GUI-GOGPU-INTEROP.md |
| Native WebView2/WKWebView/WebKitGTK | `internal/webapphost` typed contract + enforced policy (8/8); no adapter built (Linux headers absent in authoring env; Windows/macOS hosts not exercised) | FOUNDATION_IMPLEMENTED (contract) / STAGED_WITH_GATE (adapters) | `webapphost` policy tests | WEBVIEW-SECURITY.md, WEBVIEW-CAPABILITY-MATRIX.md |
| Mobile WebView adapters (Android/iOS) | contract-level rows only; no SDK builds exist | STAGED_WITH_GATE | n/a | WEBVIEW-CAPABILITY-MATRIX.md |
| Frame/surface IPC between engines | no bridge exists by design; Go-Gui and GoGPU keep separate surfaces; no zero-copy claim anywhere | STAGED_WITH_GATE (documented decision, not a gap) | n/a | GO-GUI-GOGPU-INTEROP.md |
| Security isolation of the web layer | privileged-scheme/private-network rejection, bridge fail-closed, download handoff to the existing safe downloader — enforced code | IMPLEMENTED_AND_TESTED (policy level) | `webapphost` 8/8 | WEBVIEW-SECURITY.md |
| Performance telemetry of the new layers | adapter-truth ladder documented; frame percentiles NOT MEASURED (no present loop beyond the software soak) | STAGED_WITH_GATE | n/a — labeled, never claimed | RUNTIME-DIAGNOSTICS.md |
| Platform acceptance (five-platform) | not attempted in this milestone; the standing rule: no five-platform claim for any subsystem whose matrix is incomplete | STAGED_WITH_GATE | n/a | UI-PLATFORM-AUDIT.md |

## Renderer-convergence milestone (v1.11.1, run-43883548198/build + run-43883548572/CodeQL follow-up, 2026-11-17)

| Requirement area (work order traceability §A/D) | Discovered code path / evidence | Status | Owning test(s) | Doc destination |
|---|---|---|---|---|
| CI-01 NSIS v3.13 version gate | `scripts/ci/NsisVersion.ps1` + `.Tests.ps1` + the workflow step rewritten to the strict anchored parser; the run's actual root cause (v3.13 rejected by a `^3\.13` prefix predicate) | IMPLEMENTED_AND_TESTED (fixture level, pwsh 7.7; real makensis/installer = hosted gate) | `NsisVersion.Tests.ps1` (accept/reject matrices + verdict shape) | changelog 2026-11-17 extension; note.md |
| CI-02 NSIS pin + fail-closed package gate | choco `--version=3.13.0` pin unchanged; PackageGate truth table untouched | EXISTING_AND_VERIFIED | `PackageGate.Tests.ps1` (locally green) | packagegate contract tests |
| SEC-01/SEC-02 alerts #185/#186 | Both flagged lines identified at the run commit (817 msys2/setup-msys2@v2; 2573 softprops/action-gh-release@v3); every external action in both workflows now SHA-pinned with synced version comments; alert-record API fetch BLOCKED (rate-limited) — the mapping derived from the exact YAML lines instead | IMPLEMENTED_AND_TESTED (source level; hosted scan confirmation pending the user's publish) | `ValidateUploadArtifactPins` (inverted) + `TestCodeQLWorkflowImmutablePins` + `TestUploadArtifactPinPolicyFixtures` + stress gate | RENDERER-ENGINE-MATRIX.md is silent; changelog; note.md |
| SEC-03 CodeQL workflow robustness | analyzer config, manual C++ extraction, SARIF uploads unchanged — only the refs pinned | EXISTING_AND_VERIFIED | `ValidateCodeQLWorkflowResilience` (stress gate) | codeql.yml header comment |
| SEC-04 C++ overlay warning honesty | manual build mode retained; the non-fatal overlay-cache limitation documented; no query-suite weakening, no suppressions anywhere | EXISTING_AND_VERIFIED (documented) | n/a — the absence of suppression is the contract | codeql.yml comments |
| CI-03 Linux/Fyne/IPC gates | full local `ui/fyne` suite green (incl. regenerated goldens + the button-fit matrix that caught a real escape defect and drove its product-code fix); `GOOS=windows go vet` localipc + headless internal build green | EXISTING_AND_VERIFIED (local) / NOT_RUN (hosted) | `go test ./ui/fyne/`; `go vet` cross | note.md |
| UX-01 multiple themes, one window | `internal/theme` (six WCAG-gated themes) + `ui/fyne/theme_service.go` (live preview / cancel / save-the-only-persistence) | IMPLEMENTED_AND_TESTED | `TestThemeServiceApplyCancelSave`, `TestThemeSwitchKeepsNavigation`, `TestCatalogValidates` | docs/ui/DESIGN-SYSTEM.md |
| UX-02 Engine Gallery | `ui/fyne/view_engines.go` + `internal/renderer` registry; ninth destination (Ctrl+9); honest not-built states with reasons | IMPLEMENTED_AND_TESTED | `TestEngineGallery*` (catalog render, truthful states, honest actions, platform lines) | RENDERER-ENGINE-MATRIX.md |
| UX-03 engines cooperate, one authority | No new store; renderers stay clients (registry is read-only domain data); the Go core untouched | EXISTING_AND_VERIFIED | registry + full suites | RENDERER-HOST.md |
| UX-04 real WebView features | `experiments/ui-engine-poc/wails-webview` (wails v3.0.0 pin; bridge/security/feature suites green; the window itself `wails_runtime`-tagged — BLOCKED on this host) | FOUNDATION_IMPLEMENTED (contract level) / STAGED_WITH_GATE (runtime) | POC `go test ./...` + `go run .` selfcheck | UI-PLATFORM-AUDIT.md §2c; WEBVIEW-SECURITY.md |
| UX-05 custom close/minimize/tray | `ui/fyne/window_controls.go`: real graceful-close funnel (Enter/Space/Tap); minimize/tray honestly disabled WITH visible reasons; native OS titlebar preserved | IMPLEMENTED_AND_TESTED (the implemented subset) / STAGED_WITH_GATE (minimize+tray: no Fyne v2.8.2 API) | `TestWindowControlsHitAreas/HonestStates/CloseFunnel` | docs/ui/ACCESSIBILITY.md |
| UX-06 all touch friendly | 44×44 DIP floor structurally guaranteed on the new controls; no hover-only affordances; visible disabled reasons | IMPLEMENTED_AND_TESTED (structural) / NOT_RUN (physical touch) | `TestWindowControlsHitAreas` | docs/ui/TOUCH-RESPONSIVE-MATRIX.md |
| UX-07 resolutions/form factors | viewport/boundary/degenerate matrices extended by the ninth destination; goldens regenerated (reviewed diff = exactly that destination); the compact-escape defect fixed in product code | EXISTING_AND_VERIFIED | `TestResponsive*`, `TestButtonFitMatrix` | docs/ui/TOUCH-RESPONSIVE-MATRIX.md |
| UX-08 runtime debugging | existing System/diagnostics surfaces preserved; the gallery adds engine-truth surfaces; startup phase telemetry unchanged | EXISTING_AND_VERIFIED | existing suites | docs/diagnostics/RUNTIME-DIAGNOSTICS.md |
| DOC-01 docs + file catalog | six new focused docs; five protected docs synchronized; FILE_CATALOG.md/json (832 files, zero needs-review) + generator + coverage contract | IMPLEMENTED_AND_TESTED | `TestFileCatalogCoversEveryTrackedFile` | FILE_CATALOG.md |
| ST-01 working test version | local gates green; TEST-labeled packages ship ONLY where the full local gates ran (Linux); Windows/UNVERIFIED naming per the truth contract | PARTIAL (local) / NOT_RUN (hosted) | the gates themselves | note.md, test-results.json |
| ST-02 keep 1.11.1 | no version source changed; `config.AppVersion` 1.11.1 | EXISTING_AND_VERIFIED | `version_v1111_contract_test.go` | changelog |
| Go-Gui correction (§E) | the stale Windows-a11y-unimplemented claim retired from UI-PLATFORM-AUDIT.md §2, UI-ENGINE-DECISION.md and the registry — tripwired | IMPLEMENTED_AND_TESTED | `TestGoGuiAccessibilityCorrection` | docs/ui/ACCESSIBILITY.md |

## Open verification items (never marked done without evidence)

1. **Source-report full-text cross-check** — BLOCKED (reports not
   attached to this session; matrix built from the work order's §13
   enumeration).
2. **Code Scanning live alert inventory** — BLOCKED (unauthenticated
   GitHub API rate-limited from the execution sandbox; no token
   available). See `download/code-scanning-alert-inventory.md`.
3. **Go-Gui platform gates beyond the headless POC** — STAGED_WITH_GATE
   (accessibility trees, IME/CJK, native dialogs, physical devices).
4. **Trash/restore for non-session content classes** — STAGED_WITH_GATE.
5. **Fyne-side launch action for the Go-Gui workspace** — STAGED_WITH_GATE
   (authoring host cannot compile `ui/fyne`; no blind edit made).
6. **GoGPU hardware path and frame metrics** — STAGED_WITH_GATE (no
   Vulkan ICD / no /dev/dri in the authoring environment; software render
   verified, hardware NOT RUN).
7. **Native WebView adapters** — STAGED_WITH_GATE (see
   WEBVIEW-CAPABILITY-MATRIX.md; Linux WebKitGTK headers absent in the
   authoring environment).

---

*Proprietary material under the Proprietary License (see
[`../../LICENSE.md`](../../LICENSE.md)).*
