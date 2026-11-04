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
- **#126 (41338124787, commit db9c816dc272673f124e30713766b8c9e3a4d2d2)**: v1.11.0 FULLY GREEN — preflight/core-audit/Fyne-UI/Windows-x64/Linux-x64/consolidated-logs all succeeded; NSIS compile + REAL silent install/uninstall round-trip smoke PASS with 8 SHA-256 user-data sentinels preserved byte-for-byte; core/headless + race gates green; stress 47/0/0/0; release/tag-only job correctly skipped. This is the baseline for the post-v1.11.0 session below.

---

## Session — 2026-11-04 (run #41548925352 stabilization work order: P1 log-artifact contract) — FINAL HAND-OFF

**Summary and success condition**
- Baseline: live clone of origin/main at 87452b69d03df794b01d90a2bf6eb3143ad8ffab (3 LICENSE-only commits ahead of the red run's 85d020d…, ancestor-verified); worktree clean; local branch `work/v1.11.1-stabilization-41548925352`. No remote mutation of any kind; no push; no tag. Hosted runs #133/#8 on HEAD are CANCELLED (neither green nor red) — the newest real code evidence remains run #130 (41548925352, failure on the Fyne UI gate) and CodeQL run #5 (41548925285, success with open alerts).
- THIS session's host has NO Go toolchain and NO GL/Wayland headers (the work order forbids installing large toolchains), so: P0 (Model Studio) was root-caused statically to exact line evidence and shipped as a PATCH SPEC (below) — no Go source was edited blind; P1 (single plain-text run-log artifact) was IMPLEMENTED and LOCALLY TESTED (bash collector + 14 offline fixtures, 14 pass / 0 fail); P2 Windows package pipeline preserved untouched, Linux TAR.GZ/DEB deferred (needs Go builds; §7.3 discipline); P3 CodeQL live inventory UNKNOWN (alerts API requires auth — HTTP 401 unauthenticated; never interpreted as zero).
- **Model Studio root cause (static, exact):** `ui/fyne/panels.go:263` seeds `studioLabel` with the STATIC text "Inherit is the default state: …" at construction, while `refreshStudio()` (panels.go:902-931) populates `studioSys.Options` only later inside `uiDo` after the async `Systems()` fetch returns. The shared helper (`model_studio_ui_test.go:40`) waits for that static label — true at t=0 — then calls `studioSys.SetSelectedIndex(1)`/`(0)` while Options is still empty; Fyne v2.8.2 drops out-of-range selections without firing OnChanged, so `studioSystemChanged()` never runs: ApplyBinding times out at line 108 ("binding selects populated") and ApplyErrorRearms at line 158 ("Apply armed") — exactly the recorded run #130 failure lines. The two passing tests wait on REAL readiness signals (options length; unavailable label). Product-truth defect confirmed: loading renders as if "inherit" were a resolved binding. Required fix (next Go-capable session): explicit loading state (label "Loading AI Systems…", selects+Apply disabled), distinct empty state, enable-on-ready, helper waits for `len(studioSys.Options) > 0` (the passing test's signal); optionally a fakecore `systemsBlock` barrier + generation counter for stale-response protection. NO P0 source change was committed (no compile/test evidence possible here — §5.1/§10.1 discipline).
- **P1 landed (3 files):** `scripts/ci/consolidate-run-log.sh` rewritten to collect the run-attempt logs archive (GET /repos/{repo}/actions/runs/{id}/attempts/{n}/logs) + paginated jobs metadata; deterministic plain-text report (header → run summary → error/warning indexes → NEVER_STARTED / MISSING LOG SOURCE → collector diagnostics → full logs in jobs-API order with job/step boundaries preserved); secret redaction (token value, ghp_/github_pat_, AWS keys, Authorization headers, URL credentials, PEM headers); token sent only to the API host — the signed redirect is https-validated and fetched by a credential-free curl; size-capped, zip-integrity + traversal/symlink rejected, fail closed AFTER the report is written. `build-desktop.yml`: per-job `*-logs-part` uploads + Windows smoke-output upload REMOVED (6 steps), consolidate-logs rewritten (job-scoped permissions: contents:read + actions:read), ONE upload: upload-artifact pinned to the verified immutable v7 commit cf430e030ddbb5b0abf93d22962f4752f3646cd9 with `archive: false` → the artifact IS the file `SHEYTAN-LA-all-logs.txt`; overwrite:true; if-no-files-found:error; a collector-verdict step fails the job after the upload when coverage is incomplete. Package uploads success-gated (no always()) + if-no-files-found:error + the same SHA pin (§7.4: no partial bundles). Fixture suite rewritten to the new contract (14 cases, incl. a workflow-shape contract and the signed-URL isolation source contract); the audit job's fixture gate re-runs it hosted.
- **Verification (LOCAL-TESTED, bash class):** YAML parses (PyYAML 6.0.3; jobs: preflight, audit, ui, build-windows, build-linux, consolidate-logs, release); `bash -n` clean; fixtures 14 pass / 0 fail. gofmt/go build/go vet/go test NOT RUN (no Go) — the changed surfaces are bash + YAML only.
- **Deferred/backlog:** P0 fix + tests (spec above; ui/fyne suite is hosted-class); Linux TAR.GZ + real .deb (§7.3: not startable without Go; existing Linux ZIP pipeline preserved); CodeQL 287-alert triage (live API needs auth; the workflow's export-alert-report artifact is the retrieval path); remaining build-desktop.yml tag pins (checkout/setup-go/cache/msys2/download-artifact/gh-release — pre-existing actions/unpinned-tag debt); Dev-Notes untouched (sha256 1f5e4244563425aeadf814c2db686b46d5871dc11df9abbd546b2b230958c01e re-verified); no version bump (AppVersion/productVersion/SIGNATURE all 1.11.1; zero 1.11.2 strings repo-wide).

---

## Session — 2026-11-03 (run #41415240891 remediation + Model Studio §5.2) — FINAL HAND-OFF

**Summary and success condition**
- Baseline: verified live clone of origin/main at the EXACT observed run commit 4e8c2b8150e192f997b610cd82bf07851fbc859b (title v1.11.1); worktree clean; branch `work/v1.11.1-model-studio` created locally from it. No remote mutation of any kind; no tag; no push.
- P0 executed: (A) the version dispatch is genuinely read-only — the Linux ZIP can no longer carry probe-created runtime data; (B) the fake-engine port fixtures are ownership-aware — the freePort TOCTOU class is retired without serializing CI; (C) the version lineage is REAL — the Go authority, packaging metadata, SIGNATURE, workflow declarations and the fail-closed contract suite all say 1.11.1. Model Studio §5.2 landed at the store/API/UI seams that already own the problem. Full local verification ladder green at the final tree for every package that compiles on a GL-less host (ui/fyne is the hosted Fyne gate's class, below). **Hosted verification of THIS candidate is PENDING — no push was authorized; NO green-run claim is made for the new changes.**

**Prior-request ledger (audited in the CURRENT tree, evidence per line)**

| Request | Status | Evidence |
|---|---|---|
| v1.11.1 version sync (Failure C) | LANDED this session | internal/config/config.go (AppVersion "1.11.1"); build/config.yml productVersion; SIGNATURE regenerated via `go run scripts/gen-signature.go` (1 line diff); workflow APP_VERSION literals ×2; cmd/printversion doc contract; new `internal/releasecontract/version_v1111_contract_test.go` (3 tests, fail-closed proven); `TestReleaseVersionSingleSource` still green |
| Read-only version dispatch (Failure A) | LANDED this session | cmd/root.go earlyVersionDispatch before config.Load (version case REMOVED from dispatch); cmd/version_purity_test.go (4 tests incl. zero-residue proof); hosted probe isolated + residue-asserted in build-desktop.yml |
| Fake-engine port race (Failure B) | LANDED this session | internal/llm/llama_test.go portReservation/reservePort/reReservePort (freePort retired); fakeEngineConfig + fakeManagedEngineConfig hold+revalidate; lifecycle_v136_test.go startBareHealthServer serves ON the held listener; adoption fixture child selects+reports its own port (--port 0 + SHEYTAN_FAKE_ADDR_FILE); internal/runtime/prewarm_model_first_test.go same discipline |
| CodeQL advanced workflow + alert report (§8.4) | LANDED (prior sessions; verified, untouched) | .github/workflows/codeql.yml at this commit carries `gh api --paginate --slurp --method GET`, security-events: read, tool filter, unarchived .txt upload — verified by inspection this session; NOT modified (no regression, no missing part) |
| Navigation ownership (§8.2) | LANDED (prior session; preserved) | ui/fyne/shell.go single-owner root + nav_ownership_test.go present at this commit; untouched |
| Model-selection smoothness + truthful markers (§8.1) | LANDED (prior session; preserved) | ui/fyne/panels.go highlight-only rows + Use action + phase poll present; extended this session ONLY by the additive Model Studio card |
| preferFastest default ON (§8.1) | LANDED (prior session; preserved) | internal/config + internal/recommendation + tests present at this commit; untouched (no duplication) |
| Minimal install root / data preservation (§8.3) | LANDED (prior session; preserved) | installer.nsi + releasecontract suites present; ZIP purity verifiers present in build-desktop.yml (they caught Failure A — retained untouched) |
| Stable CI artifact names (§8.3) | LANDED (prior session; preserved) | artifact_naming_contract_test.go green in this session's ladder |
| Engine update-check / user-controlled install (§8.3) | LANDED (prior session; preserved) | internal/api/engine_update.go + engine_update_test.go present; untouched |
| Per-agent model selection (§5.2) | LANDED this session (store/API/UI subset) | aisystem.Snapshot freezes Model+RuntimeProfile+Compaction+Memory (modelstudio_v1111_test.go); Validate rejects path-like model overrides + unknown profiles against recommendation.Tasks (ONE vocabulary); API PUT validated server-side (modelstudio_systems_v1111_test.go, 3 tests); Fyne binding card (panels.go + client.go UpdateSystem + model_studio_ui_test.go — HOSTED-CLASS: compiled/tests only in the Fyne CI gate, this host has no GL) |
| Fastest Safe default (§5.2) | LANDED (prior session; preserved) | preferFastest rides the ONE recommendation authority; the Model Studio profile picker maps to recommendation.Task ids, not new presets |
| Resource estimates + admission (§6) | PARTIAL — pre-existing honesty surface reused, no new estimator | perf/governor/recommendation untouched (no second sampler); the estimate-before-load expander is NOT built this session — deferred below with reason |
| Adaptive context (§6.3) | PARTIAL — existing ladder verified, expansion engine deferred | ResolveEffectiveContext/ResolveSessionContext + per-session ContextTokens clamps + contextplan pressure + continuum compaction all present and untouched; automatic discrete expansion between runs NOT built this session — deferred below with reason |
| Dev-Notes byte-identical (§8.5) | LANDED | git diff --exit-code -- docs/Dev-Notes.md clean at the final tree |
| No remote mutations (§4.20) | LANDED | local branch only; `git branch -a` shows no new remote ref; no push/tag/PR/dispatch |

**Fail-closed proofs run this session**
- workflow APP_VERSION forced back to "1.11.0" → `TestWorkflowAppVersionTracksAuthority` FAILED with the exact drift message; restored → green.
- Compiled headless binary `SHEYTAN-LA version` executed in an empty probe dir: prints `SHEYTAN-Local-Agent v1.11.1`, exit 0, directory byte-unchanged (the run #41415240891 probe shape, reproduced and proven clean).

**Full local verification ladder at the final tree (class local-build/local-runtime, go1.27.3 linux/amd64; this host has NO OpenGL/Wayland headers, no root)**
| Gate | Command | Result |
|---|---|---|
| formatting | `gofmt -l cmd internal ui` | EMPTY |
| build | `go build ./...` | GREEN |
| vet (headless-compilable pkgs) | `go vet ./internal/llm ./internal/runtime ./cmd ./internal/releasecontract ./internal/config ./cmd/printversion ./internal/aisystem ./internal/api …` | CLEAN |
| cmd + printversion + config + releasecontract | `go test … -count=1` | ok (incl. 4 purity + 3 version-contract tests) |
| llm (full engine lifecycle/transactions/adoption) | `go test ./internal/llm/ -count=1` | ok (60s) |
| runtime (prewarm model-first) | `go test ./internal/runtime/ -count=1` | ok |
| aisystem (Model Studio store) | `go test ./internal/aisystem/ -count=1` | ok (5 new tests) |
| api (Model Studio systems contract) | `go test ./internal/api/ -run 'TestSystemModelStudio|TestSystemModelOverride|TestSystemProfileValidated' -count=1` | ok (3 new tests) |
| ui/fyne suite | go test ./ui/fyne/ | **HOSTED-CLASS** — GL/Wayland headers absent on this host; the Fyne CI gate owns it. New code API-verified against the pinned fyne v2.8.2 module source (Select.Options/ClearSelected/SetSelected/SelectedIndex) and gofmt -e parse-clean |
| stress / NSIS / collector fixtures | not re-run this session | untouched surfaces; prior-session evidence stands |

**Deferred (honest, with reason)**
- Resource estimate-before-load expander (§6.1/§6.2): the existing perf/governor/model-card authorities render measured + predicted values today; a NEW load-preview estimate panel needs its own admission-path integration and test ladder. Blocked by scope, not by architecture: it must extend internal/perf + the engine mutation seam without duplicating them — deliberately not rushed inside this pass (work order §6: the estimator is a safety authority, not a decoration; half an estimator is worse than none).
- Adaptive context expansion engine (§6.3): the ceilings/budget/clamps exist (ResolveEffectiveContext → per-session ContextTokens → contextplan → continuum); the automatic discrete between-run expansion with hysteresis + Governor re-admission is a governor-adjacent behavioral change that deserves its own session with the full stress matrix (rapid growth, concurrent agents, low RAM, unknown VRAM). Not faked with a UI-only knob (work order §6.3: compaction is not expansion; a silent global change is forbidden).
- Advanced/Manual low-level engine settings editor (§5.4): the classification table and manual editor require the capability-verified argument surface (Caps/EngineCaps) to be exposed per-option with support evidence; deferred as a coherent unit.
- ui/fyne hosted gate: every UI change this session (client.go mirror+method, panels.go card, fakecore routes, 4 new UI tests) compiles and runs only where GL exists — the hosted Fyne gates are the definitive class, same as the prior session's canvas-only discipline.

**File-by-file change list (20 files; +6 new)**
- cmd/root.go — read-only early version dispatch (Failure A).
- cmd/version_purity_test.go (NEW) — zero-side-effect version contract ×4.
- internal/config/config.go — AppVersion 1.11.1 + authority comment.
- build/config.yml, SIGNATURE — synchronized identity consumers.
- cmd/printversion/main.go — doc contract updated.
- .github/workflows/build-desktop.yml — APP_VERSION ×2 → 1.11.1; Linux version probe isolated (RUNNER_TEMP probe dir + zero-residue assertion, log teed to the repo ci-logs by absolute path).
- internal/releasecontract/version_v1111_contract_test.go (NEW) — exact-identity + workflow-derivation contracts.
- internal/llm/llama_test.go — portReservation/reservePort/reReservePort (freePort retired), fixture handoffs revalidated, child ephemeral mode (+serveFakeEngineOn).
- internal/llm/lifecycle_v136_test.go — fakeManagedEngineConfig re-reserves; startBareHealthServer serves ON the held listener; adoption uses the reported ephemeral port.
- internal/llm/rediscover_v137_test.go — reservation discipline.
- internal/runtime/prewarm_model_first_test.go — same reservation discipline.
- internal/aisystem/aisystem.go — Snapshot freezes Model/RuntimeProfile/Compaction/Memory; Validate: bare-name model override + ONE-vocabulary profile validation.
- internal/aisystem/modelstudio_v1111_test.go (NEW) — 5 store tests.
- internal/api/modelstudio_systems_v1111_test.go (NEW) — 3 API contract tests.
- ui/fyne/client.go — AISystem mirror extended; UpdateSystem PUT.
- ui/fyne/panels.go — Model Studio binding card (inherit-first selects, single-flight Apply, full-record PUT, store-truth label).
- ui/fyne/fakecore_test.go — /api/systems GET+PUT fakes (round-trip, fail-path).
- ui/fyne/model_studio_ui_test.go (NEW) — 4 UI tests (hosted class).
- docs/changelog.md — concise 1.11.1 entry (convention: per-release entries exist).
- docs/ai-notes.md — this hand-off. NOT touched: docs/Dev-Notes.md (verified byte-identical), README, ROADMAP, LICENSE, internal/updater, internal/workspace, CodeQL workflow.

**Hand-off — what the operator should do next**
1. Review the diff 4e8c2b8 → this branch HEAD (20 files, all listed above). The deliverable ZIP beside this note carries the exact tree.
2. Push authorization is the operator's alone. On the next hosted run watch: Linux x64 (the isolated version probe prints "version probe left the probe directory clean"; ZIP verify must stay green), preflight (gofmt/vet), core audit (the llm suite with the owned-port fixtures), Fyne UI gates (the new model-studio tests run THERE for the first time), Windows x64 (PE/installer identities now 1.11.1 via gen-syso + -DVERSION chain).
3. The two deferred authorities (estimate-before-load, adaptive expansion) are the recommended NEXT session scope; the store/API/UI seams they need are exactly the ones this pass verified.

**Deliverables (this session, placed beside the tree by the work-order agent)**
- SHEYTAN-LA-v1.11.1-run41415240891-remediation-source.zip — source-only archive of the committed tree (single SHEYTAN-LA/ root; excludes .git/, dist/, runtime data, generated payload-uninstall.nsh); unzip -t verified; byte size + entry count in the note.
- SHEYTAN-LA-v1.11.1-run41415240891-UPDATE-NOTE.md — this session's operator-facing summary (evidence classes, ladder, deferred list). No patch file this session (work order: "No patch file").

---

## Session — 2026-11-02 (post-v1.11.0 operator-feedback pass) — FINAL HAND-OFF

**Summary and success condition**
- Operator scope: fix the real-PC reported UX/packaging issues on top of the FULLY GREEN run #126 v1.11.0 baseline (db9c816): (A) duplicated/cluttered main navigation, (B) unsmooth model selection, (C) Smooth+Fastest safe defaults with opt-out, (D) complicated installed directory, (E) official llama.cpp/Vulkan update-check with user-controlled install, (F) CI artifact `attempt1` naming noise. ALL SIX landed as one local-only commit; the full local verification ladder is green at the final tree. **Hosted verification of THIS candidate is PENDING — no push was authorized; NO green-run claim is made for the new changes.** The run #126 baseline green (41338124787) is the operator's own observed evidence, not proof of this tree.

**Branch/commit identity (LOCAL ONLY — never pushed)**
- Work branch: `agent/post-v1110-ux-20261102`, created from the EXACT baseline db9c816dc272673f124e30713766b8c9e3a4d2d2. HEAD: the single ordinary commit on this branch, 33 files (25 modified + 8 new) — the exact SHA is recorded in the UPDATE-NOTE deliverable beside the archive (a byte-exact self-reference inside the committed tree is unstable by construction); verify with `git rev-parse agent/post-v1110-ux-20261102`. No remote branch/ref was created (verified: `git branch -a` shows no `remotes/origin/agent/*`); local `main` untouched; `origin/main` had advanced 2 operator commits (CodeQL workflow only — NOT mixed in).
- `git diff --exit-code -- docs/Dev-Notes.md` — UNCHANGED (human-only file).

**A. Navigation ownership (P0) — the operator's duplication, reproduced and fixed**
- Root cause (concrete, in tree): `shell.root()` inserted `s.rail` BOTH as the content border's left object AND as a direct child of the responsive container — one CanvasObject, two layout parents, two paint passes at wide widths. Fail-closed proof: reintroducing the defect makes `TestShellSingleParentOwnership` FAIL with the exact root-cause message; restoring makes it pass.
- Rebuild: single-owner tree (responsive container owns exactly rail/topNav/content); utilities (History/Models/My Tools/Logs) grouped under a muted "Utilities" rail section + the compact strip's More menu (pop-up overlay — no second tree); Ctrl+1..8 covers all eight primary views; navigation IDs/order/lazy-cache/back-behavior and the #125 deadlock fix (`TestShellShowLazyBuildDoesNotDeadlock`) preserved.
- Tests: `ui/fyne/nav_ownership_test.go` — single-parent tree identity, one visible destination per zone swept at 360/500/640/800/999/1000/1280/1440/1920 (+ re-layout after resize), utilities reachable in every zone, one-invocation lazy-cache discipline, documented shortcut map.

**B. Model selection (P1)**
- Row selection now only HIGHLIGHTS (facts summary + armed action — never an engine transaction); the ONE explicit "Use this model" action is single-flight with a monotonic generation token and a settled-generation guard (a trailing poll tick can never repaint an intermediate phase over the terminal state). While the POST walks the backend state machine, the AUTHORITATIVE GET `/api/models/select` is polled at a bounded cadence off the UI thread (2 ms in tests, 500 ms in production; stops at terminal/cancel/error). Truthful separate markers: `● ACTIVE` = serving, `○ SELECTED` = authoritative selection (the previously DISCARDED `active` local is now the real badge source). Failure keeps the previous model active; every terminal path re-enables controls; already-serving selection offers no redundant action (the core's short-circuit mirrored in the UI). Tests: `ui/fyne/models_panel_test.go` (6) + the extended fake core's controllable selection state machine; `App.Ctx()` made nil-safe (Background fallback before Run()).

**C. Smooth + Fastest defaults (P1)**
- SmoothStream=true + TargetFPS=120 confirmed unchanged (already the v1.11.0 defaults; no fabricated "hidden Fastest toggle" was claimed — none existed). NEW `preferFastest` (config/API/UI, default ON): legacy configs without the field load ON (default-primed unmarshal); an explicit stored false SURVIVES reloads (pinned); round-trips through Save. It rides `recommendation.Input` — the ONE authority: ON takes the speed-oriented q8_0 KV choice under VERIFIED flash attention on any task (halved KV memory → more offload headroom INSIDE the measured limits); OFF = the balanced policy; the resource ladder still clamps context, GPU posture stays evidence-gated, MANUAL mode is never touched (applies only under AUTO — existing selection tests). Settings/Performance toggle labeled "fastest SAFE" with the honest caveat. Tests: `internal/config/preferfastest_v1111_test.go` (4), `internal/recommendation/preferfastest_v1111_test.go` (5), `TestPreferFastestPatchPersists` (api), `TestPreferFastestSettingsFieldPinned` (ui).

**D. Minimal installed root (P1)**
- New payload shape (one authority — staging tree feeds ZIP, NSIS `File /r`, and the generated uninstall manifest): root carries SHEYTAN-LA.exe, SHEYTAN-LA.bat (+ Uninstall.exe); ALL static content under `resources/docs` (README.txt, BUILD-INFO.txt, LICENSE.md, optional SIGNATURE) and `resources/brand` (sheytan.ico — now STAGED, one payload authority; shortcuts/AUMID/ARP icons point at `$INSTDIR\resources\brand\sheytan.ico`); the RETIRED root-level models/ + workspace/ placeholder directories no longer ship (the runtime's one-time `MigrateAppRootDirectData` fold of REAL legacy roots is untouched; user-created roots are never deleted).
- NSIS: /FileExists assertions follow the staged tree; license page reads `resources\docs\LICENSE.md`; the uninstaller keeps the legacy hardcoded root Deletes as no-op fallbacks and removes the v1.11.1 payload through the generated manifest (bottom-up RMDir covers resources/docs, resources/brand, resources). No RMDir /r anywhere (contract re-verified green).
- Workflow: Windows + Linux staging/BUILD-INFO/ZIP-verify rewritten (required entries include the resources tree; purity now also REJECTS retired root-level placeholder entries); install/uninstall smoke extended — installed-payload assertions on the resources tree, a NEW proof that the installer does NOT create root-level models/workspace, and the 8 SHA-256 sentinels retained with the top-level pair now explicitly USER-CREATED legacy roots (the preservation contract itself). **Class local-static for the .nsi + workflow text (no makensis on this host); the hosted Windows job remains the definitive compiler + smoke gate.**
- Contracts: `releasecontract.go` required ZIP entries (Windows+Linux+workflow spellings) + `BrandIconName`; installer_contract license/smoke contracts updated to the new paths. The stress surface re-verifies agreement (47/0/0/0 below).

**E. Engine update-check + variant reconciliation (P1/P2)**
- NEW `GET /api/engine/update/check` (read-only): official `ggml-org/llama.cpp` resolution through the ONE variant-aware resolver (`updater.LatestTagForVariant`, new read-only wrapper) — installed tag/variant/platform, latest tag whose release ACTUALLY carries the exact variant asset (exact asset name reported; releases without it are SKIPPED — pinned), honest classes: current | update-available | offline | no-asset | error (a network failure is NEVER "no asset" — pinned) | installing | installed | install-failed.
- NEW `POST /api/engine/update` (user-controlled install): gated exactly like variant provisioning (run-active 409, calibration 409, remote 400) + single-flight; resolves the exact-asset tag first; ALREADY-CURRENT = a no-op (no engine restart — pinned); otherwise drives the engine-owned `UpdateEngineNow` transaction (stop → stage → verify → restart → commit | rollback) in one background goroutine with live download progress on the check surface and cancellation through the existing cancel action + Close(). The scheduled updater's policy semantics are untouched (check ≠ install).
- UI: Models panel "Engine updates" card (Check for updates / Install update (restarts engine) / Cancel download + truthful monospace status incl. progress); all network off the UI thread; the interim-label write happens on the UI-event-loop path directly (the uiDo-from-UI-thread self-deadlock was found by the tests and fixed). Tests: `internal/api/engine_update_test.go` (8; HOST-AGNOSTIC fixtures — no SetPlatformForTest in api tests: that seam writes package globals and raced the AUTO Vulkan probe goroutines earlier tests leave behind under -race; the fixtures instead derive the host's own exact asset names) + `ui/fyne/engine_update_ui_test.go` (6).
- Variant matrix reconciled against the OFFICIAL b12032 release (2026-11-01, asset list verified live this session): `llama-b12032-bin-win-vulkan-arm64.zip` EXISTS — Windows ARM64 now advertises + names the Vulkan ZIP (same transaction as x64); `llama-b12032-bin-ubuntu-vulkan-x64.tar.gz` / all ubuntu+macos assets are `.tar.gz` — Linux/darwin stay HONESTLY not-provisionable via this build's ZIP transaction (comments corrected to cite b12032; no support boolean was flipped without a transaction). Vulkan system diagnostics/driver guidance: NOT built this session (explicitly deferred — see Remaining); no GPU/Vulkan execution claim is made anywhere in this pass.

**F. Stable CI artifact names (P2)**
- Every upload/download artifact name is stable (preflight/audit/ui/windows/linux `-logs-part`, windows/linux `-x64-package`, `SHEYTAN-LA-all-logs-run-<run_id>`); `overwrite: true` on EVERY upload (verified against the pinned actions/upload-artifact@v7 action.yml — the input exists, default false); download paths keep the per-job namespace `ci-parts/<stable-name>/` (anti-flatten rule from run #118 preserved); the attempt number survives ONLY inside the consolidated report as `--attempt` metadata. `windows-smoke-app-output` keeps run-id uniqueness, drops the attempt. Fail-closed proven: re-adding an attempt suffix fails `TestWorkflowArtifactNamesCarryNoAttemptSuffix` with the exact message.
- Collector fixtures: cases 10/11 moved to stable names; NEW case 12 (five stable parts captured, same filename in five namespaces, zero collisions) and case 13 (rerun/overwrite shape — same names, replaced content, 5/5 coverage, no attempt-shaped directories): **13 pass / 0 fail**. New contracts: `internal/releasecontract/artifact_naming_contract_test.go` (5 tests).

**Full local verification ladder at the final tree (class local-build/local-runtime, go1.27.3 linux/amd64, GOTOOLCHAIN=local, user-space CGO sysroot)**
| Gate | Command | Result |
|---|---|---|
| formatting | `gofmt -l cmd internal ui` | EMPTY |
| build | `go build ./...` | GREEN |
| vet | `go vet ./...` | CLEAN |
| headless core | `go test ./internal/... -tags headless -count=1` | 67 ok / 0 FAIL |
| release contracts | `go test ./internal/releasecontract/ -count=1` | ok (incl. 5 new artifact-naming + updated installer contracts) |
| race (7 critical pkgs) | `go test -race -tags headless -count=1 …` | 7/7 ok (api run 3× after fixing the test-seam race found by the detector) |
| stress | `go run -tags headless ./cmd/sheytan-local-agent stress` | `STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0` |
| collector fixtures | `bash scripts/ci/consolidate-run-log-fixtures.sh` | 13 pass / 0 fail |
| Fyne UI suite | `go test ./ui/fyne/ -count=1` | ok (incl. 6 nav-ownership, 6 model-selection, 6 engine-update tests) |
| desktop sweep | `go test ./... -count=1` | **70 packages ok / 0 FAIL** (same count as the #126 baseline) |
| NSIS compile | makensis NOT AVAILABLE on this host | **local Windows proof unavailable** — class local-static over the .nsi/workflow text; hosted Windows job is the definitive gate |
| Dev-Notes | `git diff --exit-code -- docs/Dev-Notes.md` | UNCHANGED |

**Fail-closed proofs run this session**: reintroduced the rail double-parent → `TestShellSingleParentOwnership` FAILED with the exact message; re-added an attempt-suffixed artifact name → `TestWorkflowArtifactNamesCarryNoAttemptSuffix` FAILED with the exact message; both restored to green.

**File-by-file change list (33 files; +8 new)**
- `ui/fyne/shell.go` — single-owner root(), Utilities grouping, More menu, Ctrl+1..8 (P0-A).
- `ui/fyne/nav_ownership_test.go` (NEW) — tree identity/zone/reachability/invocation/shortcut tests (P0-A).
- `ui/fyne/panels.go` — models panel rebuild (non-destructive selection controller, live phase poll, truthful markers, modelSummary) + Engine updates card (P1-B, P1/P2-E).
- `ui/fyne/models_panel_test.go` (NEW), `ui/fyne/engine_update_ui_test.go` (NEW) — the two UI suites.
- `ui/fyne/fakecore_test.go` — /api/models route + controllable selection state machine + engine update-check/install fakes (test-only).
- `ui/fyne/client.go` — Config mirror `preferFastest`; EngineUpdateCheck wire type + CheckNow/Install methods.
- `ui/fyne/app.go` — Ctx() nil-safe fallback (tests/composition probes).
- `ui/fyne/settings_schema.go`, `ui/fyne/view_settings.go`, `ui/fyne/logic_test.go` — the Performance toggle + its pinned test.
- `internal/config/config.go` — PreferFastest field + default.
- `internal/config/preferfastest_v1111_test.go` (NEW) — defaults/legacy/false/round-trip.
- `internal/recommendation/recommendation.go` — Input.PreferFastest + the speed-priority KV branch inside the ONE authority.
- `internal/recommendation/preferfastest_v1111_test.go` (NEW) — on/off/evidence-gates/memory-clamp.
- `internal/api/recommendation.go` — buildRecommendationInput passes cfg.PreferFastest.
- `internal/api/engine_update.go` (NEW) — GET check + POST install handlers (gated, single-flight, no-op-when-current).
- `internal/api/engine_update_test.go` (NEW) — 8 host-agnostic tests.
- `internal/api/server.go` — routes + the four engine-update state fields.
- `internal/api/lifecycle.go` — Close cancels an in-flight user-triggered engine update.
- `internal/api/config_patch_test.go` — preferFastest patch persistence test.
- `internal/updater/variant.go` — win/arm64 Vulkan reconciliation (b12032 evidence), honest linux/darwin comments, LatestTagForVariant read-only wrapper, test-facing release-list seams.
- `internal/updater/variant_test.go`, `internal/updater/variant_strict_test.go` — matrix/evidence tests updated + 3 new.
- `internal/releasecontract/releasecontract.go` — resources/ ZIP entries + BrandIconName.
- `internal/releasecontract/installer_contract_test.go` — license-page/smoke contracts follow the new paths.
- `internal/releasecontract/artifact_naming_contract_test.go` (NEW) — the stable-name contract suite.
- `packaging/nsis/installer.nsi` — minimal-root payload contract (assertions, license path, staged icon, icon references, payload comment, legacy-fallback deletes).
- `.github/workflows/build-desktop.yml` — staging/BUILD-INFO/ZIP-verify/smoke (Windows+Linux) minimal-root rework + stable artifact names + overwrite + consolidated-log naming.
- `scripts/ci/consolidate-run-log-fixtures.sh` — stable-name cases + rerun/overwrite fixtures 12/13.
- `docs/ui-contract.md` — contracts #9/#10/#11 recorded as LANDED (v1.11.1 remediation; Phases 3–5 explicitly still deferred).
- `docs/FEATURES.md` — the genuinely new user-visible capabilities only.
- `docs/ai-notes.md` — this hand-off.

**Deliverables (mechanically verified at placement)**
- `SHEYTAN-LA-v1.11.1-agent-post-v1110-ux-20261102-source.zip` — source-only archive of the committed tree (single `SHEYTAN-LA/` root; excludes .git/, dist/, runtime data, generated payload-uninstall.nsh); `unzip -t` clean; byte size + entry count recorded in the update note.
- `SHEYTAN-LA-v1.11.1-UPDATE-NOTE.md` — this hand-off's operator-facing summary.
- `SHEYTAN-LA-v1.11.1-agent.patch` — `git format-patch` bundle (applies with `git am` onto db9c816) for the operator's review/push.

**Hand-off — what the operator should do next**
1. Review the diff db9c816 → this branch HEAD (33 files, all listed above; patch bundle provided).
2. Decide whether to authorize a push of `agent/post-v1110-ux-20261102` (or a cherry-pick onto main) — NOTHING was pushed by this session; hosted verification of this candidate is PENDING by construction.
3. On the next hosted run, watch: Windows x64 job (makensis -V4 transcript + marker, the REWRITTEN install/uninstall smoke with the resources/ layout + no-placeholder proof + 8 sentinels, the package gate), the consolidated five-part log with STABLE names (a rerun must REPLACE, not collide), and the Fyne UI gates (the new suites run there).
4. Physical GPU/Vulkan execution remains the standing external hardware gate — nothing in this pass claims it (the engine-update check is a release-list resolution, not an execution claim).

**Remaining (honest, deferred with reason)**
- Secure tar.gz transaction for Linux/darwin engine packages (upstream is .tar.gz there) — requires the full safe-extractor + transaction tests; deliberately NOT claimed (documented in variant.go).
- Vulkan system diagnostics (loader/ICD enumeration, vendor driver guidance) in the UI — the truthful accelerator/evidence surfaces already render; the dedicated diagnostics card is future work. No driver/SDK downloads or ICD writes were added (by design).
- UI Phases 3–5 of the formal roadmap — untouched, still deferred (operator's phase order).
- The engine update-check surface is API+UI complete and locally tested; its first HOSTED execution (and the smoke's first hosted run with the new layout) lands with the operator's push.

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
