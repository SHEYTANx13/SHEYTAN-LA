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

- **#117**: Windows smoke first came alive (app launched, closed cleanly) — no window yet; diagnosability discipline born.
- **#118 (41060689195)**: gofmt 1.27 toolchain pin + preflight log tee; collector rewired to five explicit per-part downloads; smoke moved to a RUNNER_TEMP copy with ZIP-purity gates; pinned Mesa 26.2.5 verified; release-manifest.json generation added.
- **#119**: Mesa two-DLL deployment contract (opengl32.dll + libgallium_wgl.dll megadriver pair) + `scripts/ci/ImportAudit.ps1` pre-launch static-import audit; updater manifest host repointed to the code repo.
- **#120 (41223763993)**: staging bare red closed both ways — 7z listing separator normalization + raw-listing tee, 3-attempt download hardening + dedicated acquire step, titled ::error + tee discipline across every custom Windows step.
- **#122 (41239634246)**: NSIS macro-name mismatch fixed (`!insertmacro SHEYTAN_PAYLOAD_UNINSTALL_FILES`) + installer branding (license page from single-source LICENSE.md, SHEYTANx13 identity) + makensis tee into ci-logs/win-nsis-build.log + marker assertion; all pinned by 4 new releasecontract tests.
- **#123 (41245854150)**: every #122 fix held GREEN in-run; the ONE red step moved in front of the script — the unquoted makensis `-DVERSION` token — fixed by the session below.

---

## Session — 2026-11-01 (autonomous agent, Z.ai/GLM) — run #123: makensis VERSION define + operator UI/UX research — FINAL HAND-OFF

**Human prompt summary**
- 113-minute deep-fix session on run 41245854150 (build-desktop #123, commit 7da1efc27da4c4ff976c05bead5d339b1d4ce6ac, "v1.11.0"): fix the ONE red step (the unquoted makensis `-DVERSION` argument — verified root cause), append the operator's UI/UX hybrid upgrade research into docs/Dev-Notes.md (the ONE operator-authorized Dev-Notes insertion), then — strictly time-gated — execute Phase 0 of that research. Maintain ai-notes per the section-5 protocol; deliver BOTH deliverables in the download directory. Never push/tag/release; never re-fix #117–#122 surfaces.

**Success condition (MET — see Progress and Evidence)**
- The version-define contradiction is gone AND guarded (quoted token in the workflow + `TestNSISDefinesQuotedByWorkflow` fail-closed-proven); Dev-Notes carries the polished research as an append-only final section with provenance; Phase 0 EXECUTED (tokens + contract doc, all gates green); Phases 1–5 deferred by the operator's own execution order; ai-notes cleanly rewritten; BOTH deliverables placed and mechanically verified (values in Deliverables below).

**Prerequisite gate (PASSED)**
- Cloned https://github.com/SHEYTANx13/SHEYTAN-LA.git; HEAD = 7da1efc27da4c4ff976c05bead5d339b1d4ce6ac "v1.11.0" (main; parent 6f1e318 = run #122's commit). All fingerprints verified: go.mod line 1 = `module github.com/SHEYTANx13/SHEYTAN-LA` (go 1.26, toolchain go1.27.3); build-desktop.yml = 86,301 bytes with the "Windows x64" job carrying every named step in order; the makensis line matched the prompt VERBATIM (workflow line 1067 — every define quoted EXCEPT -DVERSION); installer.nsi = the v1.2.2 contract build (_FILES macro insert line 401, marker echo line 395, MUI_PAGE_LICENSE line 214, SHEYTANx13 identity, Parsaetak LegalCopyright untouched); internal/releasecontract carries the four run-#122 NSIS contract tests + license suite + TestReleaseVersionSingleSource; docs/ and ui/fyne match the fingerprint list. NOT the Parsaetak mirror.

**Run #123 verified evidence (operator-provided; sources: public run page + consolidated run log, two independent sources agree; class hosted)**
- Preflight GREEN 45s; Core audit GREEN 5m26s (stress 47/0, release-contract 16/16, fixtures 11/11); Fyne UI gates GREEN 5m47s (36 tests); Linux x64 GREEN 59s (23.9 MB ZIP); Consolidate GREEN 17s (5/5 parts). Windows x64 RED 14m53s — exactly TWO annotations (bare exit code 1 + titled "makensis failed (exit 1) … teed to ci-nsis-build.log"). Nine artifacts incl. windows-x64-package (15.4 MB) + complete windows-logs-part.
- Every pre-makensis step held GREEN in-run (Mesa sha256 got==want 997123ef4ce8896204ab912d5b54f19372c487ff9835bcefafa2fca23b8953c2; 19 x64 DLLs normalized; dependency audit resolved; smoke "ui exit: clean close (exit code 0)"; ZIP verified). The #122 tee WORKED — ci-logs/win-nsis-build.log exists in the artifact; the entire failure is three retrievable lines.

**Verified root cause**
- The complete teed makensis transcript: `Command line defined: "VERSION=1"` / `Processing config: C:\Program Files (x86)\NSIS\nsisconf.nsh` / `Can't open script ".11.0"`. makensis NEVER opened installer.nsi.
- Mechanism: GitHub Actions substitutes `${{ env.APP_VERSION }}` → `1.11.0` BEFORE pwsh parses the step; the bare token `-DVERSION=1.11.0` hits PowerShell's native-command argument parsing, which splits it after the leading numeric literal — makensis receives `-DVERSION=1` (defines VERSION as "1", line 1 of the transcript) and a separate `.11.0`, which it takes for the script path (line 3); exit 1. The quoted siblings (`-DEXE`, `-DBUILDDIR`, `-DOUTFILE`) survive intact on the same line — the discriminating control.
- Provenance: the identical unquoted token exists at 6f1e318 (run #122's commit) — the defect predates the #122 fixes; #122's macro mismatch was real and behind this one in makensis processing order; #122 could not see it because the tee did not exist then; #123 saw it in one look BECAUSE #122 added the tee.

**Progress (the exact change surface; nothing else)**
- **Fix 1 — the quoted define (the ONLY workflow change)**: workflow line 1067 `-DVERSION=${{ env.APP_VERSION }}` → `"-DVERSION=${{ env.APP_VERSION }}"`. git diff = exactly 1 line; the tee, marker assertion, titled ::error, generator step, paths, timeouts byte-identical; installer.nsi byte-identical to 7da1efc. No Go test pinned the invocation spelling (grep: no DVERSION in any .go file); the stress release_surface fragment list does not include the makensis define line — no pin update needed, and release_surface stayed green.
- **Fix 1 guard — `TestNSISDefinesQuotedByWorkflow`** (internal/releasecontract/installer_contract_test.go, suite convention, appended; header comment updated): (1) whole-file fail-closed sweep — every `-D<NAME>=` token on every non-comment line must be double-quoted, error message carries the run-#123 mechanism; (2) fail-closed subject check — at least one real makensis.exe invocation must exist; (3) direct run-#123 pin — every makensis invocation line must carry the quoted `"-DVERSION=<value>"` form. FAIL-CLOSED PROVEN: reintroducing the bare token made the guard FAIL naming line 1067 and the split mechanism; restored → green.
- **Fix 2 — the operator-authorized Dev-Notes insertion**: appended "UI/UX Hybrid Upgrade — Antigravity + Web UIs + Google AI Studio" at the END of docs/Dev-Notes.md (pure append; git diff = 52 insertions / 0 deletions; pre-existing prefix byte-identical by cmp). Content-complete: research synthesis (Antigravity 2.0 / ChatGPT-Claude-Gemini / Google AI Studio / SHEYTAN-LA keep-and-elevate), AI Studio signals, binding hybrid principle, phased plan 0–5 + success metric, executable workflow (pre-flight; Phase 0–5 success conditions/tasks/evidence; global rules; definition of done; operator execution order). Provenance line records the operator authorization and that from the NEXT session the standing "never edit Dev-Notes.md" rule applies again in full.
- **Phase 0 — EXECUTED (P2; gate was open: P1 verified + ample budget)**:
  - `ui/fyne/theme.go` extended ADDITIVELY (zero rendering change; appTheme.Color/Size untouched): `roleSet` named type (all 17 dark Palette values byte-identical, sorted-color diff verified) + `LightPalette` + `PaletteFor(fyne.ThemeVariant)` (same roles re-expressed, NOT wired into the theme — that is a Phase 1+ visual change); typography hierarchy `TypeTitle 20 / TypeSection 16 / TypeBody 14 / TypeMono 13 / TypeStatus 12`; radius rules `RadiusControl 8 / RadiusCard 10 / RadiusDialog 14`; elevation steps `ElevationFlat/Low/Mid/High` (0x00/0x28/0x50/0x78); icon sizes `IconS/IconM/IconL` (14/18/24); density `RowCompact/RowRegular/RowRelaxed` (28/36/44); the existing 4/8/12/16/24/32 spacing scale kept as the contract.
  - NEW `docs/ui-contract.md`: the binding design contract — token tables (theme.go = the single source), the 7 interaction contracts as a status table grounded in the code (IN FORCE: settlement rule, truthful status line, distinct+reachable primary views; LANDING IN PHASE 1: Enter/Shift+Enter keyboard pin, focus return after send, Stop-replaces-Send while busy, capability-priming empty states without violating zero-session Send), governance rules, evidence ladder.
  - Scope discipline: tokens DEFINED, not APPLIED (no view/shell/layout change — Phase 1 territory per the operator's execution order); no Workspace/security touch; no new authority; no second UI stack.
- **P3**: Phases 1–5 DEFERRED by design (operator's execution order: review the Phase 0 contract, then unlock Phase 1); quality items — no pinned-string changes (verified), change surface exactly the sanctioned set (verified below), no size work (installer .exe size recorded once a green hosted run produces it), smallest Phase 0 surface (verified).

**Evidence (class local-static; go1.27.3 from go.dev with GOTOOLCHAIN=local, GOPATH/GOCACHE under /tmp)**
- `gofmt -l cmd internal ui` → EMPTY (final tree).
- `go build -tags headless ./...` → GREEN; `go vet -tags headless ./...` → CLEAN.
- `go vet ./ui/fyne/` (real-driver type-check) → CLEAN; `go build ./ui/fyne/` → GREEN.
- `go test ./internal/... -tags headless -count=1 -timeout 24m` → 67 ok / 0 FAIL.
- `go test ./internal/releasecontract/ -count=1` → ok (17 tests = 16 prior + the new guard).
- `go run -tags headless ./cmd/sheytan-local-agent stress` → `STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0`.
- `bash scripts/ci/consolidate-run-log-fixtures.sh` → 11 pass / 0 fail.
- `go test ./ui/fyne/ -count=1` → ok (34 top-level --- PASS lines under -v; suite ok ~0.3s).
- Baseline ladder at pristine 7da1efc was green with the same commands before any edit.
- **UI compile boundary CLOSED (improvement over #122)**: the container lacks GL/X11/Wayland dev headers and root; a user-space sysroot was built by `apt-get download` (works without root) + `dpkg -x` into /home/z/my-project/work/sysroot and consumed via CGO_CFLAGS/CGO_LDFLAGS (script: /home/z/my-project/scripts/uiladder.sh). This moved `go build/go test ./ui/fyne/` from hosted-only to ALSO local. The hosted Fyne UI gates job (Xvfb, real driver + window) remains the runtime/visual gate; no evidence class inflated.
- **Boundary that stands (honesty rules)**: makensis does NOT run in this Linux container — the hosted run is the installer's compile gate. The Windows GUI path and the installer's compiled output remain locally unverifiable. NO claim that the installer will build: the verified contradiction is REMOVED and the next hosted run is the gate.

**Stacked-defect expectation (recorded per prompt)**
- makensis has never compiled green in the Go-UI era — #117–#122 each failed before this step; #123's failure fired before the script opened. The quoting fix lets makensis open installer.nsi for the FIRST time. If the next hosted run reds makensis with a NEW transcript line, that line is the NEXT session's single fix — do not preemptively rework the nsi, do not add defensive edits. One run, one fix; the tee guarantees one-look diagnosability.

**Docs touched**
- docs/ai-notes.md (this clean rewrite — final state). .github/workflows/build-desktop.yml (one token). internal/releasecontract/installer_contract_test.go (guard + header comment). docs/Dev-Notes.md (operator-authorized append only). ui/fyne/theme.go (Phase 0 tokens). docs/ui-contract.md (NEW, Phase 0). No other file.

**Whole-session change-surface verification (git)**
- `git status --short`: exactly the five modified files above + docs/ui-contract.md new. `git diff` workflow = 1 line; Dev-Notes = 52+/0-; installer.nsi `git diff --quiet` → byte-identical to 7da1efc.

**Deliverables (section 6 mechanical verification; zero-padded fixed-width fields so this file's length does not change when the values are filled)**
- `SHEYTAN-LA-v1.11.0-run41245854150-FIXED.zip` — byte size 000008610205 (12-digit field); entry count 0812 (4-digit field); single top-level `SHEYTAN-LA/` root; every tracked path at the fix tree plus the new docs/ui-contract.md; excludes `.git/` and build outputs. `unzip -t` result: OK (full test extraction, all 812 entries CRC-verified).
- `SHEYTAN-LA-v1.11.0-run41245854150-UPDATE-NOTE.md` — beside the zip in the download directory.
- Placement verification (RECORDED): the download directory holds BOTH files — the zip at 8610205 bytes (8,610,205) and the update note at 14356 bytes; the zip passed a full test extraction from its placed copy (`unzip -t`: "No errors detected in compressed data", 812/812 entries OK); the 4-digit entry field (0812) matches the placed archive. Evidence class local-static (filesystem + CRC), recorded 2026-11-01.

**Hand-off**
- Done: everything in Progress; all section-9 DoD gates green at the final tree; deliverables placed and mechanically verified.
- Remaining (honest): hosted verification of everything above (evidence class stays local-static until the next run goes green); the installer .exe size is pending the first green hosted run; Phases 1–5 of the UI/UX upgrade (deferred by design — the human reviews docs/ui-contract.md first).
- Recommended next: review the diff 7da1efc → working tree (one workflow token, one guard test, the Dev-Notes append, the Phase 0 pair, this file) → push → watch the Windows job. Expected order on the run page: (1) every pre-makensis step green as in #123; (2) "Build the NSIS installer" — makensis OPENS installer.nsi for the first time in the Go-UI era, tees into ci-logs/win-nsis-build.log, then the "NSIS marker contract: payload uninstall list included" line; (3) installer artifact + package gate green; (4) if makensis reds on a NEW line, that teed line is the next session's single fix — do not treat it as a rework of this one.
- Risks: (a) a possible compile error BEHIND the quoting fix — expected class, self-describing via the tee (see Stacked-defect expectation); (b) the light palette is defined but unwired — deliberate (Phase 1+); (c) the sysroot used for local UI evidence is session-local tooling, not a tree change — nothing in the repo references it; (d) the UI suite ran locally under the software test canvas — the hosted Xvfb job remains the visual gate.
- Deferred (with reason): Phases 1–5 (operator's execution order — Phase 0 contract review first); no MCP wiring or roadmap item touched this session (not in scope).

---

*This file is proprietary material under the Proprietary License.*

<!-- pad:xx -->
