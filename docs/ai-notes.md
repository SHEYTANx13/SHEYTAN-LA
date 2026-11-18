# AI Notes — Current Technical Hand-Off

**This is the only file AI agents freely update.** It carries the
current working state an agent needs BEFORE reading code: live
constraints, blockers, known evidence and the next actionable step.
It is a snapshot, not a diary — per-session narratives, per-run recaps
and finished task lists live in git history and
[`changelog.md`](changelog.md). Do not append a new diary entry each
session; rewrite this note so it stays true.

---

## Current state (2026-11-17, master-work-order ALPHA transition candidate)

The branch `work/alpha-transition` (local, unpushed) carries the first
slice of the master engineering work order (ALPHA, single authoritative
prompt), derived from main HEAD `f29d759` = the commit that hosted run
44069246344 verified green. Version stays 1.11.1 as the numeric BUILD
IDENTIFIER (internal/config.AppVersion); the user-facing product
identity is now `internal/brand.Stage = "ALPHA"` (master work order
§12.1). The user owns every remote mutation; nothing has been pushed.

Completed on this branch (all locally verified, evidence class 2):

1. **ALPHA identity split (§12.1)** — every user-facing surface
   (version command, help, boot banner, ask/context banners, serve/setup
   banners) renders `brand.Stage`; `productStage: ALPHA` added to
   `build/config.yml`; SIGNATURE identity line regenerated to
   `SHEYTAN-Local-Agent ALPHA` via `scripts/gen-signature.go`; the API
   state/governor surfaces gained `productStage` (numeric `appVersion`
   kept for the legacy UI contract until the UI migration lands);
   `cmd/printversion` unchanged (CI plumbing, strict semver resolver).
   Contract tests updated to pin the split; the version-purity tests now
   FAIL if the numeric identifier leaks into user-facing branding.
2. **Installer retirement (§10)** — `packaging/nsis/`,
   `scripts/ci/NsisVersion*.ps1` and every NSIS/makensis step deleted
   from `build-desktop.yml`; the Windows lane now builds the portable
   ZIP only; `PackageGate.ps1` rewritten portable-only with the same
   machine-readable contract (`PACKAGE_GATE_RESULT <TOKEN>`) plus a new
   `INSTALLER_REMNANT` state that rejects stale installer artifacts in
   `dist/` (§10.2); `PackageGate.Tests.ps1` rewritten to the portable
   truth table; the release job's artifact set, SHA256SUMS and publish
   list dropped the installer; `internal/releasecontract` flipped from
   installer-pinning to installer-retirement pinning
   (`installer_contract_test.go`, `TestWindowsInstallerSurfaceIsRetired`,
   forbidden-fragment gates in `cmd/stress_release_surface.go`);
   `docs/retired.md` registers the retirement; file catalog regenerated
   (863 tracked files).
3. **Preflight refusal codes (§3.2.1 + §3.2.5)** — `preflight.Report`
   gained a stable machine-readable `Code` (MODEL_NOT_SELECTED,
   MODEL_FILE_MISSING, BACKEND_UNAVAILABLE, UNSUPPORTED_BACKEND,
   CONTEXT_EXCEEDS_MODEL, INSUFFICIENT_RESOURCES,
   INSUFFICIENT_RESOURCES_NOW, CAPABILITY_MISMATCH); the Stack gate
   returns `MODEL_NOT_SELECTED` for an empty selection (guided setup
   state, never a post-submit chat failure) and `MODEL_FILE_MISSING`
   through the ONE path-resolution authority
   (`llm.ResolveModelPath`) — `preflight.Evaluate` stays the pure fit
   layer. Tests: `internal/preflight/preflight_codes_test.go` + extended
   `internal/runtime/preflight_gate_test.go`.

Honest limits of this pass (do not call them done):

- The §4/§5 Go-Gui/GoGPU UI migration, §5.5 download-manager redesign,
  §6 in-app Git clone UX, §7 conversation continuation, §8 queue
  governor, §9 security broker and Wails Web Workspace hardening are
  NOT STARTED on this branch beyond what previous candidates already
  carry (renderer registry, design tokens, engine gallery, Wails POC).
- `PackageGate.Tests.ps1` / `ImportAudit.Tests.ps1` are NOT RUN on this
  host (no PowerShell); their authority stays the hosted Windows lane.
- Hosted CI has NOT run for this candidate (no push authorized).
  Native Windows/macOS/mobile runtime evidence: NOT RUN (Linux sandbox,
  no GL headers — the Fyne CGO lane compiles only on the hosted runner;
  `ui/fyne` was verified CGO-free via `GOOS=js GOARCH=wasm` compile).

## Local gate matrix for this candidate

- gofmt: clean (whole tree). vet `-tags headless` cmd+internal: clean.
- `go build -tags headless` cmd+internal+scripts: OK (CGO_ENABLED=0).
- Baseline `go test ./internal/... -tags headless -count=1`: 76/76
  packages OK on the BASE commit `f29d759` before any edit.
- Post-change: `internal/preflight`, `internal/runtime`,
  `internal/releasecontract`, `cmd`, `cmd/printversion`,
  `internal/config`, `internal/brand`, `internal/api`,
  `internal/logging` re-run green after each edit.
- `GOOS=js GOARCH=wasm go build ./ui/fyne/...`: NOT RUN — the js/wasm
  target does not compile the app's dependency graph (internal/proc,
  chromedp use OS syscalls). ui/fyne itself is UNTOUCHED by this pass
  (no user-facing version render sites exist in it), so its compile
  gate remains the hosted CGO Linux lane's job, unchanged.
- INFERENCE-VERIFIED: NOT RUN (no model selected/executed).
- Physical touch / screen readers / real GPUs / Windows / macOS /
  Android / iOS: NOT RUN anywhere in this pass.

## Next actionable step (for the user or the next agent)

1. The user reviews this candidate (source ZIP + note.md in download/)
   and authorizes (or not) the push of `work/alpha-transition` to the
   hosted CI for the exact candidate commit.
2. Hosted verification must include: the Windows lane with the new
   portable-only PackageGate (PowerShell fixtures), the Fyne UI gates,
   and the ALPHA-branding contract tests.
3. The remaining master-work-order phases continue on the SAME branch in
   priority order: renderer integration proof (§16–31 of the schedule)
   against the pinned go-gui v0.96.0 / gogpu v0.54.1 / wails v3.1.0
   versions, then the §5 UI migration, download manager, Git clone,
   continuation, queue governor, security and Web Workspace phases.
