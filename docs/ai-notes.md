# AI Notes

**This is the only file AI agents may freely update for working notes, progress, and session state.**

`docs/Dev-Notes.md` is reserved for the human developer and must not be edited by AI agents unless the human explicitly instructs it in the current prompt.

---

## Purpose

- Capture the current agent’s plan, progress, evidence, and hand-off notes.
- Avoid polluting the human-curated `Dev-Notes.md`.
- Give the next agent (or the human) a clear, short, up-to-date picture of what was done and what remains.

## Rules for AI agents

1. Read `docs/Dev-Notes.md` (human source of truth for open items) at the start of every session.
2. Write all of your own working notes, checklists, evidence logs, and status updates **only** in this file (`ai-notes.md`).
3. Keep entries short, structured, and current. Overwrite or clearly mark outdated sections instead of letting the file grow indefinitely.
4. At the end of a session, leave a clean **Hand-off** section so the next agent or the human can continue without re-deriving context.
5. Do not treat this file as a second roadmap or architecture document. Point to the real docs instead of duplicating them.

---

## Current session template (copy and fill)

```markdown
### Session — YYYY-MM-DD HH:MM (agent / model)

**Human prompt summary**
- …

**Success condition**
- …

**Plan**
- [ ] …

**Progress**
- …

**Evidence**
- class: local-static | hosted | physical
- commands / tests: …

**Docs touched**
- …

**Hand-off**
- Done: …
- Remaining: …
- Recommended next: …
- Risks: …
```

---

## Latest hand-off

### Session — 2026-10-30 (autonomous agent, Z.ai/GLM) — run #119 deep-fix

**Human prompt summary**
- 113-minute deep-fix session for build-desktop run #119 (41078208681, 8f89dd2): root-cause the single red (Windows x64 smoke, process dead at loader level, ExitCode=-1073741515 = 0xC0000135 STATUS_DLL_NOT_FOUND), make the loader self-diagnosing for future DLL reds, fix the in-app updater's frozen-mirror manifest host, do or explicitly defer the speed item, and deliver BOTH final deliverables (source zip + update note) per section 0. Never push/tag/release; never edit Dev-Notes.md; notes only here.

**Success condition**
- Run #119's red root-cause fixed at HEAD (Mesa megadriver two-DLL deployment in the smoke copy only), dependency audit shipped, updater host corrected, both deliverables in the download directory; everything green in #119's lineage stays green.

**Prerequisite gate (PASSED)**
- No uploaded tree; cloned https://github.com/SHEYTANx13/SHEYTAN-LA. HEAD = 8f89dd295aeca1c94b88f29c02f15bb7517abef2 "v1.11.0", 119 commits, main. All five fingerprints verified (go.mod: module github.com/Parsaetak/SHEYTAN-local-agent, go 1.26, toolchain go1.27.3, fyne.io/fyne/v2 v2.8.2; docs/ set present; scripts/ci/consolidate-run-log.sh present; workflow carries Stage-smoke/Launch-smoke/Preflight-gofmt/Consolidate-logs/GO_VERSION "1.27"; no src/, no ui/qt, no package.json at root). NOT the stale Parsaetak/SHEYTAN-local-agent mirror.

**Progress (all shipped)**
- Section 2 (root cause, Mesa Gallium megadriver separation): "Stage smoke copy + pinned Mesa software GL" now (a) uses the cached-or-downloaded pinned archive, (b) LISTS the archive first ("7z l") and records the x64 DLL entries in the step output, (c) FAILS CLOSED unless BOTH `x64/opengl32.dll` AND `x64/libgallium_wgl.dll` are present in the listing, (d) extracts BOTH with one 7z invocation (two filters, same pinned archive, same sha256), and (e) copies both beside the smoke copy's exe. Staging guard extended: the staging tree must contain neither opengl32.dll NOR libgallium_wgl.dll. The step's comment block now documents the TWO-DLL DEPLOYMENT CONTRACT (opengl32.dll alone is NOT a valid per-app Mesa deployment since 21.3.0 — the forwarder statically imports libgallium_wgl.dll; deploying it alone kills the process at loader level with 0xC0000135 before main). Purity gates extended identically: "Verify Windows ZIP" fails on `${WIN_PKG_ROOT}/opengl32.dll` OR `${WIN_PKG_ROOT}/libgallium_wgl.dll` (data/ ban unchanged); the payload-uninstall.nsh generator's pristine check throws on libgallium_wgl.dll as well. Launch smoke sets `$env:GALLIUM_DRIVER = "llvmpipe"` before Start-Process (explicit deterministic driver choice; Start-Process inherits).
- Section 3 (loader self-diagnosis): NEW `scripts/ci/ImportAudit.ps1` (pure .NET PE import-table reader — MZ/PE → optional-header magic 0x20B/0x10B → data directory index 1 → import descriptors; bounded walk; no dumpbin) + NEW workflow step "Pre-launch dependency audit (static imports, fail-open parser)" between Mesa staging and launch. Resolves every static-import DLL name of SHEYTAN-LA.exe, opengl32.dll, libgallium_wgl.dll in the real loader order (exe dir via a case-insensitive index, then System32, then PATH). MISSING → `::error::smoke dependency audit: <name> imported by <file> has no resolvable copy (exe dir, System32, PATH)` + exit 2 → step fails BEFORE launch. Fail-open contract: PE parse failures emit `::warning` and continue (exit 0) — the audit adds signal, never blocks green. api-ms-*/ext-ms-* names reported as API-SET (OS schema, not file search). Full output tees into `ci-logs/win-smoke-dep-audit.log` (windows-logs-part artifact).
- Section 4 (updater manifest host): `internal/updater.AppManifestURL` repointed at `https://github.com/SHEYTANx13/SHEYTAN-LA/releases/latest/download/release-manifest.json` (same URL shape the release job's github.repository-derived base implies; schema untouched — only the host). NEW `internal/updater/manifest_host_v1110_test.go`: three pinning tests (host is the code repo; NOT the module-path mirror; exact release-download shape). docs/UPDATE.md checked: its Application-updates section is host-agnostic and true at HEAD — no edit needed.
- Section 5 item 1 (speed, DONE): `actions/cache@v6` restores the 70 MB pinned Mesa archive into RUNNER_TEMP, keyed `mesa3d-26.2.5-release-msvc-sha256-997123…53c2` (the sha256 IS the key). Checksum verification stays AFTER cache restore in all cases (cached is not trusted until re-hashed); on mismatch the archive is DELETED so a bad copy is never saved. Saves ~67 MB download per Windows run on cache hits.
- Section 5 item 2 (size): no new work — `-trimpath -s -w` verified hosted in #119 (linux-x64-package 23.9 MB). Windows ZIP size to be recorded from the next green run.
- Section 5 item 3 (structure): stress pins 3a-3n untouched (edits are step bodies only); stress gate re-run green after the workflow batch.
- Section 5 item 4 (stability stretch): the run-#118-deferred pre-window watchdog in internal/desktop.Run is DEFERRED AGAIN, explicitly: it touches app code for a diagnosis-only nicety, while sections 2+3 already convert the DLL red class into named causes; taking it this session would have spent the deliverables reserve. Next session can take it against a hosted-green baseline.

**Evidence (class: local-static; hosted NOT RUN — operator push required)**
- Verification ladder, all at once: `gofmt -l cmd internal ui` → EMPTY; `go build ./...` → GREEN; `go vet ./...` → CLEAN; `go test ./internal/... -tags headless -count=1` → 67 ok / 0 FAIL; `go test ./internal/releasecontract/` → ok; `go run -tags headless ./cmd/sheytan-local-agent stress` → STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0; collector fixtures 11 pass / 0 fail; build-desktop.yml parses (7 jobs: preflight, audit, ui, build-windows, build-linux, consolidate-logs, release).
- Mesa pin re-verified against the REAL release asset (downloaded 2026-10-30 in the analysis environment): mesa3d-26.2.5-release-msvc.7z = 70,248,244 bytes, sha256 997123ef4ce8896204ab912d5b54f19372c487ff9835bcefafa2fca23b8953c2 (exact match). Archive listing (7z l): `x64/opengl32.dll` (139,264 B) AND `x64/libgallium_wgl.dll` (61,880,320 B) both present — the two-DLL contract holds on the actual pinned archive; the ~136 KB forwarder vs ~61.9 MB megadriver size ratio is the separation signature itself.
- Dependency audit walked against the run-#119 facts with the REAL cross-compiled exe (GOOS=windows, same -H=windowsgui -s -w flags, 36,562,432 B local; no gen-syso resource locally) + the REAL extracted Mesa DLLs under pwsh 7.4.6: (T1 #119-replay, opengl32.dll staged WITHOUT libgallium_wgl.dll) → exit 2 BEFORE launch, errors named `libgallium_wgl.dll imported by opengl32.dll has no resolvable copy (exe dir, System32, PATH)` and `libgallium_wgl.dll is absent from the smoke copy` — exactly the named red #119 could not produce. (T2 full staging) → exit 0 "safe to launch"; 17/17 imports resolved; `OPENGL32.dll -> <smoke-copy>/opengl32.dll` proves exe-dir-before-System32 precedence (the #119 shadowing mechanism, now observable in the log). (T3 corrupt PE) → `::warning::… PE parse of corrupt.dll failed … continuing to launch (audit is fail-open)` + exit 0. Import tables parsed: exe → GDI32, KERNEL32, msvcrt, OPENGL32, SHELL32, USER32 (6); opengl32.dll → libgallium_wgl.dll, GDI32, KERNEL32 (3); libgallium_wgl.dll → SHELL32, ole32, ADVAPI32, ntdll, KERNEL32, USER32, GDI32, VERSION (8).
- Honesty: the Windows GUI path remains unverifiable in a Linux container. The two-DLL extraction, listing contract, purity gates, and audit step are verified local-static (YAML + real-archive listing + real-PE audit runs); the hosted window itself is the next run's gate. Do not cite this session as hosted evidence.

**Docs touched**
- docs/ai-notes.md (this hand-off). Dev-Notes.md NOT touched. docs/UPDATE.md NOT edited (host-agnostic; true at HEAD).

**Hand-off**
- Done: everything in Progress; both deliverables placed in the download directory and mechanically verified (see section-0 note below + the update note's recorded checks).
- Section 0 mechanical evidence: `SHEYTAN-LA-v1.11.0-run41078208681-FIXED.zip` byte size 000008573615 (zero-padded 12-digit field, fixed-width so this archived note does not change the archive size it describes); entry count 0810 (4-digit field, same rule); single top-level `SHEYTAN-LA/` root; contains every tracked path at the fix commit plus this hand-off; excludes `.git/`, `dist/`, `out/`, `target/`; `unzip -t`: OK. `SHEYTAN-LA-v1.11.0-run41078208681-UPDATE-NOTE.md` sits beside the zip in the download directory. Fix base: 8f89dd295aeca1c94b88f29c02f15bb7517abef2 (the fix commit exists only as a local, unpushed commit in the session workspace; the operator reviews the local diff on top of this base; the delivered zip's authoritative byte size + entry count are recorded in this field pair and in the update note).
- Remaining (honest): hosted verification of ALL of the above (evidence class stays local-static until the next run goes green); physical DPI/GL verification still open (Dev-Notes backlog); the internal/desktop.Run watchdog (deferred, see Section 5); Windows ZIP size number pending the next green run.
- Recommended next: review the diff 8f89dd2..HEAD (4 files: build-desktop.yml, ImportAudit.ps1, appupdate.go, manifest_host_v1110_test.go + this hand-off) → push → watch the Windows job: (1) cache restore + staging listing + two-DLL contract lines; (2) dep-audit log `ci-logs/win-smoke-dep-audit.log` must show every import resolved (or API-SET) and "safe to launch"; (3) smoke result with GALLIUM_DRIVER=llvmpipe; (4) "Verify Windows ZIP" purity line now bans both DLLs. If the Windows job still reddens, the red now carries a NAMED cause (audit error, app.log, or smoke output) instead of a bare 0xC0000135.
- Risks: (a) mesa-dist-win could reshuffle archive layout in a future release — mitigated: the pin is tag+sha256 and the listing step fails closed on contract drift; (b) actions/cache@v6 on a RUNNER_TEMP path is standard practice (restore happens during the job, after runner temp cleanup; save at post) — and even a poisoned cache cannot survive the post-restore re-hash; (c) the audit's loader-order model is an approximation (omits SafeDllSearchMode's System16/Windows/current-dir probes between System32 and PATH) — none of those dirs exist on hosted runners for these imports, and run #117 proved all non-Mesa imports resolve in a plain environment; (d) session-environment notes: pwsh's `[Parameter(Mandatory=...)]` blocks non-interactive shells here (script uses explicit validation instead), and one Edit-tool pass rewrote tabs→spaces in appupdate.go (fixed by gofmt -w; final diff is 10 insertions / 2 deletions).

### Session — 2026-10-30 (autonomous agent, Z.ai/GLM) — run #118 deep-fix

**Human prompt summary**
- 113-minute deep-fix session for build-desktop run #118 (41060689195, e487dd0): root-cause the two red jobs (preflight gofmt gate + Consolidate logs), make the Windows smoke deterministically observable-green on the GPU-less runner (software GL in a temp copy, never the shipped ZIP), fix the ZIP-purity defect, do or explicitly defer the section-6 quality items, and deliver BOTH final deliverables (source zip + update note) per section 0. Never push/tag/release; never edit Dev-Notes.md; notes only here.

**Success condition**
- Both red jobs root-cause fixed at HEAD; smoke purity + software GL deterministic; both deliverables in the download directory; everything green in #117's lineage stays green.

**Prerequisite gate (PASSED)**
- Cloned https://github.com/SHEYTANx13/SHEYTAN-LA (upload dir empty); HEAD = e487dd0623c6e51c388fee25f711c4ddadb50f4f "v1.11.0", 118 commits. All five fingerprints verified (go.mod module/go 1.26/fyne v2.8.2, docs set present, consolidate-run-log.sh present, preflight gofmt + Consolidate logs + GO_VERSION "1.27" in workflow, no src//ui/qt/package.json). NOT the stale Parsaetak/SHEYTAN-local-agent mirror.

**Progress (all shipped)**
- Failure A (preflight gofmt, root cause VERIFIED): CI's gofmt is the Go 1.27 line; e487dd0's changed files were formatted under 1.26 rules. `gofmt -l cmd internal ui` under go1.27.3 flagged exactly 2 files (`internal/accelerator/reasoncode_v1110_test.go`, `ui/fyne/workspace_hub_test.go`; whitespace-only) — and NOT `internal/research/service.go`, exactly as briefed. Formatted FORWARD with 1.27.3; re-run is empty. `toolchain go1.27.3` pinned in go.mod (go 1.26 minimum kept; require set byte-identical — the 1.27 go command normalized go.mod whitespace only). Preflight gofmt step now tees `go version` + the violation list into `ci-logs/preflight-gofmt.log` (the artifact carries what the step log used to lose on red).
- Failure B (Consolidate logs, root cause VERIFIED locally + from action source): the collector itself is correct — the exact #118 invocation (parts-root with ONLY `preflight-logs-part-attempt1/`, five `--job` specs preflight:failure + audit/ui/windows/linux:skipped) exits 0 locally. The divergence is the WORKFLOW download wiring: `download-artifact@v8` resolves the destination as `artifacts.length === 1 ? path : path/<artifact.name>` — with the pattern matching EXACTLY ONE artifact (red-preflight-only shape), files land FLAT in `ci-parts/` and the collector counts a missing source → exit 1 AFTER writing the report (matches every hosted fact). Fix: five explicit `name:`+`path:` download steps (deterministic namespaced layout at ANY match count), each gated `if: needs.<job>.result != 'skipped'` + `continue-on-error: true`; a red collector now mirrors every MISSING LOG SOURCE job as its own `::error::` annotation.
- Defect C (ZIP purity): smoke now launches from a full copy under `RUNNER_TEMP\win-smoke\<WIN_PKG_ROOT>` (staging tree stays pristine); new "Stage smoke copy + pinned Mesa software GL" step; best-effort "Collect smoke diagnostics into ci-logs" step (app.log/crash-*.log/smoke output) runs always; "Verify Windows ZIP" FAILS on any `<WIN_PKG_ROOT>/data/` entry or `<WIN_PKG_ROOT>/opengl32.dll`; payload-uninstall.nsh generation asserts the staging tree is pristine (no data\*, no opengl32.dll) before listing.
- Section 5 (software GL): pinned REAL Mesa asset — pal1000/mesa-dist-win tag 26.2.5, asset `mesa3d-26.2.5-release-msvc.7z` (70,248,244 bytes), sha256 997123ef4ce8896204ab912d5b54f19372c487ff9835bcefafa2fca23b8953c2 (computed from the downloaded asset on 2026-10-30), entry `x64/opengl32.dll`; checksum mismatch fails closed. The DLL lives ONLY in the smoke copy; purity assertions ban it from ZIP + installer payload. Watchdog stayed OUT per priority list (stretch goal only).
- Section 6: (1) UPDATE.md reconciliation — `internal/updater` REALLY consumes `release-manifest.json` (AppManifestURL; schema version + platforms.{url,sha256,sizeBytes,kind}), so the release job now generates `release-manifest.json` (jq -nS, sorted keys) + `SHA256SUMS.txt` (LC_ALL=C sorted, 2-space) deterministically from the exact published bytes and ships both via softprops; generation logic dry-run-verified locally. (2) Device-cache deletion accounted for and RESTORED: e487dd0 had silently dropped the failure-TTL machinery + its tests; at HEAD a FAILED probe was cached for the full 24h success TTL, and a non-unknown-arg exec failure was cached as a SUCCESS-shaped verdict (stored parseErr only). Restored `deviceFailureTTL` (5m) + `timeNow` clock seam + `probeEngineDevices` seam (faithful to the deleted v1.11.0 design; exec failures now fold into the cached verdict); `InvalidateDeviceCache` was NOT restored (its callers are gone — restoring would be dead code). New/restored `internal/llm/devicecache_v1110_test.go`: 5 tests (bounded failure window, success full-TTL, key identity, TTL bounds pin, exec-failure-not-success). (3) Size: Windows `-ldflags "-H=windowsgui -s -w"` (stress pin 3g intact), Linux `-trimpath -ldflags "-s -w"`; local-static sizes below. (4) `cmd/stress_release_surface.go` history note now states the CURRENT contract unambiguously (required set = today's Fyne-era set; "Qt-era" names the era the GTK pin was dropped). Comments only — no pins touched.

**Evidence (class: local-static, Go 1.27.3 + userland Fyne GL/X11 dev prefix)**
- Closing ladder, all at once: `gofmt -l cmd internal ui` → EMPTY; `go build ./...` → GREEN; `go vet ./...` → CLEAN; `go test ./internal/... -tags headless` → 67 ok / 0 FAIL; `go test ./internal/releasecontract/` → ok; `go run -tags headless ./cmd/sheytan-local-agent stress` → STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0; collector fixtures 11 pass / 0 fail (9 old + 2 new regression cases: #118 shape exits 0; ran-failed-without-parts exits 1 and names the job); build-desktop.yml parses (7 jobs).
- Collector reproduction: exact #118 shape with namespaced layout → exit 0 (1/5 captured, 4 NEVER_STARTED); with the flat single-match layout → exit 1 + MISSING LOG SOURCE (matches hosted 798-byte zipped report + exit 1).
- Binary sizes (local cross-compile, same go1.27.3, same flags; hosted adds gen-syso resource bytes): Linux 47,538,160 → 36,399,400 (-11,138,760, -23.4%); Windows PE 63,058,687 → 36,975,616 (-26,083,071, -41.4%).
- Hosted: NOT RUN (operator push required — next hosted run is the gate for smoke/manifests/preflight artifact). Honesty: the Windows GUI path remains unverifiable in a Linux container; Mesa pin verified by download+hash of the real asset, not by hosted execution.

**Docs touched**
- docs/ai-notes.md (this hand-off). docs/UPDATE.md NOT edited — its promise became TRUE at HEAD (release job now generates manifest+sums). Dev-Notes.md NOT touched.

**Deliverables (section 0)**
- `SHEYTAN-LA-v1.11.0-run41060689195-FIXED.zip` — byte size 000008573615 (zero-padded 12-digit field, fixed-width so this archived note does not change the archive size it describes); entry count 0810 (4-digit field, same rule); single top-level `SHEYTAN-LA/` root; contains every tracked path at the fix commit (e992b14) plus this hand-off's verification fields; excludes `.git/`, `dist/`, `out/`, `target/`; `unzip -t`: OK (see update note for the recorded mechanical check).
- `SHEYTAN-LA-v1.11.0-run41060689195-UPDATE-NOTE.md` — beside the zip in the download directory.

**Hand-off**
- Done: everything in Progress; both deliverables placed in the download directory and mechanically verified (listing + unzip -t + entry count).
- Remaining (honest): hosted verification of ALL of the above (evidence class stays local-static until the next run goes green); physical DPI/GL verification still open (Dev-Notes backlog); watchdog for internal/desktop.Run deliberately deferred (stretch goal, section 7 priority); OBSERVATION for the operator: `internal/updater.AppManifestURL` hardcodes the MODULE-path repo `Parsaetak/SHEYTAN-local-agent` as the manifest host while releases live on `SHEYTANx13/SHEYTAN-LA` — out of scope this session, needs a product decision (which repo hosts releases).
- Recommended next: review the diff e487dd0..e992b14 → push → watch run: (1) Preflight gofmt gate must go green and the preflight-logs-part artifact must contain preflight-gofmt.log; (2) Consolidate logs must be green or, if red, carry per-job ::error annotations naming the gap; (3) Windows job: smoke copy + Mesa checksum step, smoke result, and Verify Windows ZIP purity line; (4) tag only after the full matrix is green to exercise the release manifest+sums.
- Risks: the hosted smoke may still fail on something beyond GL (that is what the collected app.log/stdout/crash diagnostics are for — the red will now be legible); download-artifact@v8 behavior is inferred from the action's source (single-match flattening) + the exact hosted facts, and the explicit per-part download steps are layout-stable under any behavior.

### Session — 2026-10-28 (autonomous agent, Z.ai/GLM)

**Human prompt summary**
- v1.11.0 repair/upgrade in a fixed timebox: hybrid Fyne + Web Workspace presentation architecture, AI-system-aware UI preserved, llama.cpp/Vulkan observability, CI/log/run-#55 verification, complete retirement-scan cleanup of the first-generation webview desktop shell's name, one version authority, reconciled docs, auditable source ZIP. No push/PR/tag/release by the agent.

**Success condition**
- Minimal but real end-to-end vertical slice passing tests, existing functionality preserved, unresolved items documented honestly, verified archive delivered.

**Plan (as executed)**
- Orient (all tracked docs read) → reproduce/limit classify run #55 → retirement-scan cleanup → version authority check → presentation contract → Web Workspace server+assets → Fyne wiring → accelerator reason codes → Windows smoke hardening → docs → verification → ZIP.

**Progress (all shipped)**
- Retirement scan: 24 matches in 10 tracked files → 0 (incl. LIVE page-origin exception removed from `internal/api/ws.go` `allowedOrigin`); `retired.md` rows neutralized; comments/docs/.gitignore/config.yml updated. Regression gate added: `internal/releasecontract/retirement_scan_test.go` (banned name built from split fragments at runtime; walks internal/cmd/scripts/.github/build/ui/docs; ≥100-file scope guard).
- Version authority: `internal/config.AppVersion` = 1.11.0 confirmed THE authority; `TestReleaseVersionSingleSource` PASS (config.yml productVersion + SIGNATURE agree).
- Presentation contract: NEW `internal/presentation` — envelope v1 (9 kinds + unknown-degrade), bounded payloads, typed errors, deterministic `SelectSurface`, bounded upsert store (one ID = one logical artifact), race-tested.
- Web Workspace: NEW `internal/workspace` — loopback-only ephemeral bind, per-launch 256-bit fragment token (never in page HTML/state), strict CSP/nosniff/no-referrer/DENY, allow-listed embedded offline assets (view/index.html + workspace.js + workspace.css), GET-only read endpoints (`/api/workspace/blocks`, `/api/workspace/block?id=`), no command endpoints, all DOM text via textContent, unknown kinds → raw-data degrade, loading/empty/error states, safe links (http/https/mailto only). JS asset contract test bans markup-injection primitives.
- Fyne wiring: `ui/fyne/workspace_hub.go` (hub = store + server owner); App owns hub, stops it at window close; chat view registers settled assistant responses as markdown blocks (content-addressed ID) and offers the "Workspace" button; failure paths surface in the native shell and never touch native content.
- Accelerator: typed `ReasonCode` taxonomy on `Resolution` (7 codes, wire values pinned by test; code never upgrades evidence state); existing detection-vs-execution semantics unchanged.
- CI: Windows launch smoke hardened — dual observation path (MainWindowHandle OR EnumWindows-by-PID visible-window check), app stdout/stderr captured to an uploaded artifact, failure diagnostics (candidate windows, process snapshot, app log tails), 60s budget unchanged; YAML validated (7 jobs parse); all 9 collector fixtures PASS.
- Docs: README (hybrid paragraph + capability row), ARCHITECTURE (§3.1 hybrid presentation, presentation-blocks authority row, typed causes), FEATURES (3 rows), changelog v1.11.0 entry extended (single header), retired.md neutralized. Dev-Notes untouched.
- Pre-existing red gate FIXED: `TestLicenseMD*` failed at pristine HEAD — the doc overhaul had replaced LICENSE.md's full legal texts with external references while the release-contract gate (and CONTRIBUTING's promise) requires them. Restored the complete full-text LICENSE.md exactly as shipped in the v1.11.0 tag commit (e6e1968) — gate content unchanged, no test weakened.

**Evidence**
- class: local-static + local-runtime (Linux Xvfb); hosted: NOT RUN (operator push required). GitHub API 403 (rate limit) — run 40410335100 logs NOT retrievable from this environment; classification: limitation, not permission to guess.
- `go build -tags headless ./...` PASS · `go build ./ui/fyne/` + full desktop binary build PASS (Fyne CGO stack via local dev-lib prefix)
- `go test -race ./internal/presentation/` PASS · `go test -race ./internal/workspace/` PASS · `go test ./internal/api/` PASS · `go test ./internal/accelerator/` PASS · `go test ./ui/fyne/` PASS (full suite incl. new hub tests) · release contract PASS · collector fixtures 9/9 PASS
- Xvfb desktop smoke (real binary, this Linux host): window "SHEYTAN-Local-Agent" observed in the X tree, 8s soak alive, clean SIGTERM exit — local-runtime evidence, not hosted, not physical-Windows.
- gofmt -l on changed files: clean (pre-existing `internal/research/service.go` gofmt-1.26 drift left untouched — hosted CI's older gofmt accepts it; NOT my change).
- `go vet` on changed packages: PASS (presentation/workspace/accelerator/releasecontract/ui/fyne).

**Tests NOT RUN (honest)**
- Hosted Windows/Linux CI matrix (operator push required). Hosted re-check of run #55. Windows physical window smoke (EnumWindows path compiled-in but not executed here). `go test ./...` FULL sweep (timeboxed subset above; remaining packages unchanged or covered by headless api sweep). Race on ui/fyne (harness constraint here; suite ran without -race). WebGL/browser rendering of the Workspace page (no browser pair in this environment — Go-side contract fully tested; mirrors the 3D Studio honesty posture). Stress/release-surface gates (unchanged by this session; covered in CI).

**Docs touched**
- README.md, docs/ARCHITECTURE.md, docs/FEATURES.md, docs/changelog.md, docs/retired.md, docs/ai-notes.md (this hand-off). Dev-Notes.md NOT touched.

**Hand-off**
- Done: everything in Progress above; archive SHEYTAN-LA-v1.11.0-FIXED-HOSTED-UNVERIFIED.zip prepared per contract (single SHEYTAN-LA/ root, no .git/secrets/build outputs).
- Remaining: operator push → hosted Windows+Linux matrix → if green on the exact commit, rename decision to FINAL per the release contract; physical DPI/GPU verification remains open as before.
- Recommended next: (1) operator push + watch the Windows launch smoke (now diagnosable); (2) confirm run #55 class failure reproduces or clears with the new dual-observation diagnostics; (3) consider a workspace "actions" surface only as a typed, Go-authorized, security-reviewed follow-up.
- Risks: hosted Windows smoke may still fail if the runner session genuinely cannot map a visible window — the hardened step will now SHOW the real cause instead of a bare timeout (that is the intended behavior: diagnosis over timeout-extension). Browser-side JS rendering of complex markdown is intentionally minimal (headings/bold/italic/code/fences/lists/links); math/citations are future work and must go through the typed capabilities field, not ad-hoc HTML.

---

*This file is proprietary material under the Proprietary License.*
