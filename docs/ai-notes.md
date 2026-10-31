# AI Notes

**This is the only file AI agents may freely update for working notes, progress, and session state.**

`docs/Dev-Notes.md` is reserved for the human developer and must not be edited by AI agents unless the human explicitly instructs it in the current prompt.

---

## Purpose

- Capture the current agent's plan, progress, evidence, and hand-off notes.
- Avoid polluting the human-curated `Dev-Notes.md`.
- Give the next agent (or the human) a clear, short, up-to-date picture of what was done and what remains.

## Rules for AI agents

1. Read `docs/Dev-Notes.md` (human source of truth for open items) at the start of every session.
2. Write all of your own working notes, checklists, evidence logs, and status updates **only** in this file (`ai-notes.md`).
3. Keep entries short, structured, and current. Overwrite or clearly mark outdated sections instead of letting the file grow indefinitely.
4. At the end of a session, leave a clean **Hand-off** section so the next agent or the human can continue without re-deriving context.
5. Do not treat this file as a second roadmap or architecture document. Point to the real docs instead of duplicating them.

## Session log discipline (operator standing instruction, in force since run #120)

After EVERY edit batch this file is CLEANLY REWRITTEN to describe the tree AS IT IS at that moment: plan checkboxes reflect actual state, progress matches edits actually made, evidence matches commands actually run. A stale claim left for "later" is a defect. Prior sessions are compressed to one line each. Honesty bound: every claim must be true of the tree at the moment it is written; evidence lines carry their class and the exact command.

---

## Prior-session lineage (one line each)

- **2026-10-28, v1.11.0 repair/upgrade session**: hybrid Fyne + Web Workspace architecture shipped, retirement scan cleaned, release contract hardened; archive delivered.
- **2026-10-30, run #118 session (41060689195)**: gofmt 1.27 toolchain pin + preflight log tee; collector fixed via five explicit per-part downloads; smoke moved to RUNNER_TEMP copy with ZIP-purity gates; pinned Mesa 26.2.5 asset verified; release-manifest.json generation added.
- **2026-10-30, run #119 session (41078208681)**: Mesa two-DLL deployment contract (opengl32.dll + libgallium_wgl.dll, megadriver separation since 21.3.0) + `scripts/ci/ImportAudit.ps1` pre-launch static-import audit; updater manifest host repointed to the code repo.

---

## Run #120 context (the knowledge base for the next session)

**Run 41223763993** (push of 049f557 to main, 2026-10-31 17:17 UTC, 15m34s, FAILURE): Preflight GREEN 40s; Core audit GREEN 5m18s; Fyne UI gates GREEN 4m34s; **Windows x64 RED 14m31s**; Linux x64 GREEN 1m0s; Consolidate logs GREEN 12s. Exactly ONE annotation: Windows x64 "Process completed with exit code 1." — bare. Artifacts: consolidated all-logs 6.23 KB, linux-x64-package 23.9 MB, windows-logs-part 1.99 KB, preflight part 653 B — **windows-smoke-app-output and windows-x64-package ABSENT**.

**Elimination chain (verified against the workflow text at 049f557):** absent smoke-app-output artifact → Start-Process never ran. windows-logs-part 1.99 KB (≈ #119's 2.13 KB minus the two empty redirected-stream files) with NO `win-smoke-dep-audit.log` → the audit step produced nothing. The audit's own failure modes leave signatures that are ABSENT (missing-DLL branch = titled ::error + exit 2; an unhandled crash inside ImportAudit.ps1 does not stop the step). ⇒ the red was in **"Stage smoke copy + pinned Mesa software GL"**, a pwsh step where EVERY throw was bare.

**Candidate root causes — both HYPOTHESIS with mechanism at session start; both deterministically closed this session:**
- **A — listing separator sensitivity** (fix 2A): the two-entry contract check compared literal `"x64/opengl32.dll"` strings against raw `7z.exe l` tokens; Windows 7-Zip may render entry paths with backslashes → false contract failure → bare throw. CLOSED BY: token normalization (`-replace '\\','/'`) + case-insensitive compare + RAW listing tail teed into the staging log on success AND on contract failure. Fixture-proven: the OLD pipeline fails on a backslash rendering; the new one passes fwd/backslash/mixed/odd-case and fails closed on a genuinely absent entry.
- **B — acquisition fragility** (fix 2B): single `Invoke-WebRequest` of the 67 MB asset with default progress rendering; one transient error killed a 14-minute job at minute 13. CLOSED BY: `$ProgressPreference='SilentlyContinue'` + 3 IWR attempts + `curl.exe -L --retry 3` fallback + hash verify in all cases + titled ::error naming attempts/got/want; the acquisition now runs at minute ~1 in its own step (fix 3).

**Known trap (unchanged):** `github.com/Parsaetak/SHEYTAN-local-agent` is a stale v1.10.1 mirror (Qt-era, 348 commits, no 049f557) — never clone it, never build against it. The module path is now `github.com/SHEYTANx13/SHEYTAN-LA` everywhere (go.mod line 1 = operator edit; the rest swept this session).

---

## Session — 2026-10-31 (autonomous agent, Z.ai/GLM) — run #120 deep-fix — FINAL HAND-OFF

**Human prompt summary**
- 113-minute deep-fix session: make the next hosted build-desktop run GREEN and self-diagnosing. Fix run #120's Windows red at both candidate root causes; harden every custom step (::error + tee discipline); complete the module-path conversion; enforce the ai-notes clean-rewrite protocol; deliver BOTH deliverables (FIXED.zip + UPDATE-NOTE.md) with mechanical verification. Never push/tag/release; never edit Dev-Notes.md.

**Success condition (MET — see Evidence and Deliverables)**
- Every P1 fix implemented and locally verified; no custom step in the Windows job can fail without a titled ::error naming the failing variable; module path `github.com/SHEYTANx13/SHEYTAN-LA` across the tree with LICENSE untouched; ai-notes cleanly rewritten per protocol; BOTH deliverables in the download directory with verification evidence.

**Prerequisite gate (PASSED)**
- Cloned https://github.com/SHEYTANx13/SHEYTAN-LA; HEAD = bfa026398b71639818a1ee4190fba2207c50f1af "Update go.mod" (2026-10-31 22:33 +0330), a DIRECT DESCENDANT of 049f557 "v1.11.0" carrying the operator's go.mod line-1 edit; 121 commits. All five fingerprints verified (go.mod module/go 1.26/toolchain go1.27.3/fyne v2.8.2; docs/ set; both scripts/ci files; the three Mesa workflow steps + `GO_VERSION: "1.27"`; no src/, no ui/qt, no package.json at root). NOT the stale mirror.

**Plan (final state — all shipped)**
- [x] Prerequisite: acquire + fingerprint the tree
- [x] Read ai-notes.md end-to-end, agent.md, Dev-Notes.md (read-only), UPDATE.md; context merge
- [x] Fix 1: module-path sweep, LICENSE untouched
- [x] Baseline ladder green after the sweep
- [x] Fix 2: listing separator normalization (A) + download hardening (B)
- [x] Fix 3: split Mesa acquisition into its own step before the build
- [x] Fix 4: audit wrapper fail-open hardening
- [x] Fix 5: ::error + tee discipline across every custom Windows step
- [x] Fix 6 (P3): cosmetic redirect parenthesization in the smoke step
- [x] Parser fixtures (both separator renderings + fail-closed) + real-archive two-DLL re-verification
- [x] Final DoD ladder at the final tree; clean ai-notes rewrite (this file)
- [x] Deliverables: zip + update note in download/ with mechanical verification

**Progress (all shipped)**
- **Fix 1 (module-path sweep)**: mechanical string-exact replace of `github.com/Parsaetak/SHEYTAN-local-agent` → `github.com/SHEYTANx13/SHEYTAN-LA` in 274 files (684 hits; the 275th hit file is docs/ai-notes.md, owned by this protocol — its one hit was a historical hand-off line, now gone with the rewrite). Recorded exceptions (deliberate, not oversights): LICENSE.md byte-identical (attribution); `internal/updater/manifest_host_v1110_test.go` L29 banned-value pin `Parsaetak/SHEYTAN-local-agent/releases` (a NEGATIVE guard — converting it would weaken the test) and its L6 historical comment; bare `Parsaetak` author/attribution/AUMID/trademark strings in SIGNATURE, SECURITY.md, README.md, internal/brand, internal/config AppUserModelID, internal/platform firewall rule names, packaging (NSIS PUBLISHER/AUMID/REGKEY, .bat), build/config.yml, gen-syso comments — product identity, not importable paths, protected by the spec's boundary clause. No stress-gate pin contained the old path as an asserted string (stress gate green throughout).
- **Fixes 2-6 (workflow batch, build-desktop.yml only)**:
  - **Fix 3 (step split)**: NEW step "Acquire pinned Mesa archive (cache-or-download, verify checksum)" immediately after "Resolve APP_VERSION from the Go authority", BEFORE "Setup MSYS2"/the ~10-minute CGO build; the `actions/cache@v6` restore moved beside it. Acquisition failure now reds the job at minute ~1 with a named cause.
  - **Fix 2B (in the acquire step)**: `$ProgressPreference='SilentlyContinue'` before any IWR; up to 3 IWR attempts (each logged); `curl.exe -L --retry 3 --fail` fallback with output teed; checksum gate re-applied in ALL cases (same sha256 constant, delete-on-mismatch so a bad copy is never cached); titled ::error on every failure path naming the stage, attempt count, got/want hashes. Transcript tees into `ci-logs/win-mesa-acquire.log`.
  - **Fix 2A (in the stage step)**: every parsed listing token normalized with `-replace '\\','/'` before the `-like "x64/*.dll"` filter and the required-entry comparison; comparison case-insensitive (`ToLowerInvariant` both sides); the RAW 7z listing tail (last 12 lines) teed into `ci-logs/win-smoke-staging.log` unconditionally — the artifact that settles A-vs-B without a hosted log; the normalized parsed entry list is logged. The two-filter extraction is unchanged (proven #119; 7z matches archive-internal paths, not console renderings).
  - **Fix 4 (audit wrapper)**: ImportAudit.ps1 invocation wrapped in try/catch — script-level crash (parse error / unhandled terminating error) emits `::warning::… ImportAudit.ps1 crashed … continuing to launch` and CONTINUES; only the script's OWN exit 2 fails the step via titled ::error. Semantics verified under pwsh 7.4.6: `exit 2` in a child script inside a piped try/catch does NOT raise a catchable exception ($LASTEXITCODE carries it — probe: auditExit=2), unhandled throws and parse errors ARE caught (RuntimeException / ParseException). Tee to `ci-logs/win-smoke-dep-audit.log` unchanged.
  - **Fix 5 (::error + tee discipline)**: every explicit throw in EVERY custom pwsh step of the Windows job (staging, audit wrapper, smoke, PE verify, BUILD-INFO, ZIP create/verify, NSIS payload/installer, package gate, test/vet/gen-syso/build gates) is preceded by a titled `::error::` naming the failing variable/guard/path/got-want; the acquire + stage steps wrap their whole bodies in try/catch so UNEXPECTED cmdlet terminating errors also land as titled ::error naming the stage. Staging → `ci-logs/win-smoke-staging.log`; acquisition → `ci-logs/win-mesa-acquire.log`; audit → `ci-logs/win-smoke-dep-audit.log`; all three reach the windows-logs-part artifact.
  - **Fix 6 (cosmetic)**: `-RedirectStandardError ($smokeLog + ".err")` parenthesized; launch/close logic otherwise untouched (proven in #119 — restructured nothing).
- Windows job = 28 steps; 7 jobs unchanged; no pinned workflow string touched (stress gate green after the batch).

**Evidence (class: local-static, go1.27.3 = the pinned toolchain, final tree)**
- `gofmt -l cmd internal ui` → EMPTY.
- `go build ./...` → GREEN (exit 0).
- `go vet ./...` → CLEAN (exit 0).
- `go test ./internal/... -tags headless -count=1 -timeout 25m` → 67 ok / 0 FAIL (package paths print `github.com/SHEYTANx13/SHEYTAN-LA/...`).
- `go test ./internal/releasecontract/ -count=1` → ok.
- `go run -tags headless ./cmd/sheytan-local-agent stress` → `STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0` (run after the workflow batch and at the final tree).
- `bash scripts/ci/consolidate-run-log-fixtures.sh` → 11 pass / 0 fail.
- build-desktop.yml parses (PyYAML): exactly 7 jobs — preflight, audit, ui, build-windows, build-linux, consolidate-logs, release; build-windows = 28 steps.
- **pwsh parser gate**: every `run:` block of the Windows job parsed by the real PowerShell 7.4.6 Language Parser (`Parser::ParseInput`) after substituting the GitHub `${{ }}` expressions the runner expands pre-parse — 0 syntax errors.
- **Throw-pairing gate**: 23 explicit throw lines in the Windows job, each with a titled `::error::` within the 3 preceding lines — 0 unpaired.
- **Parser fixtures** (`scripts` outside the tree; pwsh 7.4.6): 10 pass / 0 fail — the shipped pipeline passes synthetic listings with forward-slash, backslash, mixed and odd-case entry renderings and FAILS CLOSED on a genuinely absent entry; the OLD (run-#120-tree) pipeline demonstrably fails on the backslash rendering (the candidate-A mechanism) and passes on forward slashes (the #119 discriminating fact); three sync assertions confirm the copied fragments match build-desktop.yml verbatim.
- **Real-archive re-verification**: mesa3d-26.2.5-release-msvc.7z re-downloaded 2026-10-31 = 70,248,244 bytes, sha256 997123ef4ce8896204ab912d5b54f19372c487ff9835bcefafa2fca23b8953c2 (exact match); the shipped normalized contract pipeline run over the REAL 7z listing parses 19 x64 DLL entries and finds BOTH `x64/opengl32.dll` and `x64/libgallium_wgl.dll`; the two-filter extraction yields opengl32.dll = 139,264 bytes and libgallium_wgl.dll = 61,880,320 bytes (both SIZE-OK). The two-DLL deployment contract holds on the real pinned archive — unchanged from #119, as required.
- pwsh semantics probe (fix-4 basis): /tmp/auditprobe (child exit-2 through piped try/catch → not caught; unhandled throw → RuntimeException caught; parse error → ParseException caught).
- Honesty: the Windows GUI path remains unverifiable in a Linux container; everything above is local-static. The next hosted run is the gate; hosted verification is NOT claimed by this session.

**Deliverables (section 6 mechanical verification, recorded here per contract)**
- `SHEYTAN-LA-v1.11.0-run41223763993-FIXED.zip` — byte size 000008542109 (zero-padded 12-digit fixed-width field so this note does not change the archive size it describes); entry count 0704 (4-digit field, same rule); single top-level `SHEYTAN-LA/` root; contains every tracked path at the fix tree plus this hand-off; excludes `.git/`, `dist/`, `out/`, `target/`; `unzip -t`: OK (no errors detected in the 704-entry test extraction).
- `SHEYTAN-LA-v1.11.0-run41223763993-UPDATE-NOTE.md` — beside the zip in the download directory; contains the elimination chain, both candidate root causes and which edits close which, the module-path conversion outcome with grep evidence, the ai-notes protocol now in force, the complete file-by-file change list, verification evidence (class local-static), operator next steps, and remaining open items.

**Docs touched**
- docs/ai-notes.md (this clean rewrite — the file's final state). .github/workflows/build-desktop.yml (fixes 2-6). 274 module-path files (fix 1). Dev-Notes.md NOT touched. No other docs edited.

**Hand-off**
- Done: everything in Progress; all section-9 DoD gates green at the final tree; both deliverables placed in the download directory and mechanically verified (byte size, entry count, test extraction — values in the Deliverables block above).
- Remaining (honest): hosted verification of ALL of the above (evidence class stays local-static until the next run goes green); the Windows ZIP size number for the release notes is pending the first green hosted run (record it then); physical DPI/GL verification remains open (Dev-Notes backlog).
- Recommended next: review the diff bfa0263..HEAD-equivalent (276 files: build-desktop.yml + 274 module-path files + this hand-off) → push → watch the Windows job. What to look for, in order: (1) "Acquire pinned Mesa archive" runs at minute ~1 — cache hit or 3-attempt download + curl fallback lines + got/want sha256 in the step and in ci-logs/win-mesa-acquire.log; (2) "Stage smoke copy (purity + two-DLL placement)" — raw 7z listing tail + normalized parsed entries + contract pass in ci-logs/win-smoke-staging.log; (3) dep-audit log "safe to launch"; (4) smoke result with GALLIUM_DRIVER=llvmpipe; (5) "Verify Windows ZIP" purity line. If the Windows job still reddens, the red will now NAME its cause (titled ::error naming the stage, the guard, the got/want values — never a bare exit 1), and the transcripts that matter are in the windows-logs-part artifact.
- Deferred (P3, with reason): the bounded pre-window watchdog in internal/desktop.Run — deferred for the third time: it touches app code for a diagnosis-only nicety; the diagnosability layer shipped this session (named-stage ::error + teed transcripts) already converts the red classes of #117-#120 into named causes, and taking the watchdog would have spent the deliverables reserve. Take it against a hosted-green baseline.
- Risks: (a) candidate A vs B remains a hypothesis pair by nature — BOTH are closed deterministically and the raw listing + download-attempt transcripts make any residual failure name itself on the next run; (b) 7z.exe console rendering could change across 7-Zip versions — mitigated by the normalization + raw-tail teeing; (c) mesa-dist-win could reshuffle archive layout in a future release — mitigated: the pin is tag+sha256 and the listing step fails closed on contract drift; (d) the updater host fix from #119 is intact (headless suite green on both Linux jobs that run those tests — do not regress it).

---

*This file is proprietary material under the Proprietary License.*

<!-- pad:xx -->
