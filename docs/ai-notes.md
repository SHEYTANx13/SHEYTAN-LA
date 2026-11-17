# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-17, renderer-convergence milestone candidate)

The branch `work/v1.11.1-renderer-convergence` (local, unpushed)
carries the v1.11.1 renderer-convergence candidate. Baseline:
`d361692` on main — the run-43883548198 build commit AND the
run-43883548572 CodeQL commit AND current main HEAD (no drift).
Version stays 1.11.1 everywhere. The user owns every remote mutation;
nothing has been pushed.

## What changed in this candidate (the load-bearing facts)

1. **Both supplied CodeQL actions/unpinned-tag alerts are closed at
   source.** Alert #185 flagged line 817 (`msys2/setup-msys2@v2`) and
   alert #186 flagged line 2573 (`softprops/action-gh-release@v3`) of
   `.github/workflows/build-desktop.yml` at the run commit — verified
   by reading the exact YAML at those lines. Every external action in
   BOTH workflows now pins an immutable 40-hex commit with a synced
   `# vX` comment. The five run-log-table SHAs were re-verified
   against their upstream tags via git; the three un-tabled refs were
   resolved fresh from upstream (`softprops/action-gh-release@v3` →
   `8a0cd651e5b2365bd640854a3fa5dfc70e4875f3`,
   `actions/download-artifact@v8` →
   `9000827ccba6bdab643e8b6fd33ac0654aef8333`,
   `github/codeql-action@v4` →
   `33d7b85da1a04171e211befe2c739303b9a4c066`). The repo's
   release-tag action policy was REVERSED coherently:
   `internal/releasecontract.ValidateUploadArtifactPins` (shared by
   the stress gate AND the contract tests — one authority) now
   rejects mutable tags and requires SHA + synced comment, with a
   raw-uses-line parity audit; the stress gate's required-actions
   table, the Dependabot notes and the collector fixture shape
   contract all follow. Hosted scan confirmation of the two alert
   IDs becoming fixed is PENDING the user's publish.
2. **The run-43883548198 Windows root cause is fixed at source.** The
   pinned NSIS 3.13.0's `makensis /VERSION` answers `v3.13`; the
   retired `^3\.13` prefix predicate rejected that valid output and
   stopped the installer job (the package gate's "installer missing"
   message was the downstream consequence, not a second defect). The
   strict gate now lives in `scripts/ci/NsisVersion.ps1` (anchored
   `^v?3\.13(\.\d+)?$` on the trimmed output + zero process exit,
   fail-closed with the executable path and raw output recorded) and
   is shared with `NsisVersion.Tests.ps1` (accept v3.13/3.13/v3.13.0/
   3.13.0/3.13.1 + Out-String newline; reject 3.12/v3.12.9/3.130/
   3.14/v4.0/v3.13-preview/empty/whitespace/multiline/nonzero-exit)
   — locally PASS under pwsh 7.7; wired as a Windows-lane pre-gate
   step. PackageGate's fail-closed truth table is UNTOUCHED.
3. **The renderer convergence is code, not mockups.**
   `internal/theme` (six WCAG-gated themes; Hellish the premium
   default; Obsidian carries the pre-token identity VERBATIM; JSON
   form for web surfaces), `internal/renderer` (truthful manifests;
   production-is-Fyne-only; mobile-parity claims banned; the Go-Gui
   Windows-UIA correction tripwired against return), the Engine
   Gallery as the shell's ninth destination (Ctrl+1..9; honest
   not-built states with reasons — no fake launch buttons), the live
   theme switcher (select = live preview, Save = the only persistence
   path via Fyne Preferences, Cancel = restore; navigation-preserving
   — pinned by test) and the window-controls trio (guaranteed 44×44
   DIP MinSize; Close routes through `App.RequestClose()`; minimize/
   tray DISABLED with visible reasons — Fyne v2.8.2 exposes no API
   and the native OS titlebar provides the real behavior).
4. **A REAL compact-width defect was found and fixed by the gates.**
   Build-time unwrapped texts (bold engine names, long card titles)
   inflated the Engines scroll content to 388 DIP inside a 304 DIP
   viewport (the theme Select escaped the content width). Fixed in
   PRODUCT code: the engine-name labels wrap, card titles carry no
   decorative suffix, and `wrappedLabel` now arms the wrap BEFORE the
   text arrives (creation-order repair in `widgets.go`). Structural
   goldens were regenerated; the reviewed diff is EXACTLY the ninth
   destination.
5. **The Wails v3.0.0 Web Workspace POC exists and is green on its
   testable surface** (`experiments/ui-engine-poc/wails-webview`):
   closed bridge allowlist + strict schema + 64 KiB ceiling;
   trusted-origin-only navigation with file/javascript/data +
   SSRF-class blocks + CSP + bounded traversal-free downloads; the
   17-probe feature-detection manifest with mandatory fallbacks
   (WebGPU never claimed from detection). The WebView WINDOW is
   BLOCKED on this host (no WebKitGTK/Windows) — the runtime wiring
   is `wails_runtime`-tagged for capable hosts.
6. **The complete tracked-file catalog ships**: `FILE_CATALOG.md` +
   `FILE_CATALOG.json` (832 files at generation, zero needs-review),
   generated by `scripts/gen-filecatalog` from `git ls-files`, with
   the coverage contract test (`file_catalog_contract_test.go`)
   pinning catalog == tracked-minus-itself.

## Known blockers / honest states (the vocabulary, not excuses)

- Hosted GitHub runs: NOT RUN — the user owns publishing; the hosted
  CodeQL scan must confirm alerts #185/#186 as fixed after publish.
- Code Scanning alert-record API fetch: BLOCKED (unauthenticated rate
  limit). The flagged actions were identified from the exact lines at
  the run commit instead — with certainty, but the hosted record
  remains the authority.
- Native Windows runtime (real makensis/installer/launch smoke):
  NOT RUN — no Windows host. `GOOS=windows go vet` of localipc and the
  headless internal build are the local evidence.
- ImportAudit.Tests.ps1 locally: BLOCKED (Windows-path assumptions;
  the hosted Windows lane is its authority). PackageGate and
  NsisVersion fixture suites: locally green under pwsh 7.7.
- Physical touch / screen readers / real GPUs: NOT RUN anywhere.
  GoGPU's software renderer is the only backend ever executed here.

## Next actionable step (for the user or the next agent)

1. The user reviews this candidate (source ZIP + note.md +
   test-results.json in download/) and decides the publish.
2. After publish: confirm the hosted Windows lane produces the
   installer (NSIS gate now accepts `v3.13`) and the next CodeQL scan
   shows #185/#186 fixed — then record the run IDs here.
3. Roadmap rows for the next milestone: the companion-window launch
   path, physical a11y verification (the corrected Go-Gui Windows UIA
   is now a testable target), the Wails `wails_runtime` window on a
   capable host.
