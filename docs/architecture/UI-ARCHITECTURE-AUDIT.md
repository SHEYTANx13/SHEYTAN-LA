# UI Architecture Audit — v1.11.1 Renderer Convergence

> The v1.11.1 milestone's placement audit: what the current tree actually
> holds, where the new renderer architecture landed, and the conventions
> that were followed. Companion evidence:
> [`RENDERER-ENGINE-MATRIX.md`](RENDERER-ENGINE-MATRIX.md) (the engines),
> [`RENDERER-HOST.md`](RENDERER-HOST.md) (the authority contract),
> [`FILE_CATALOG.md`](FILE_CATALOG.md) (every tracked file).

## 1. Audit scope and method

- Baseline: `main` HEAD `d361692` (= the build-run 43883548198 and
  CodeQL-run 43883548572 commit; **no drift**). Work executed on the local
  non-main branch `work/v1.11.1-renderer-convergence`; no remote
  mutations.
- Inventory: `git ls-files` (832 tracked files at baseline) — the same
  authority the file catalog generates from; no filesystem recursion.
- The audit BEFORE editing confirmed: existing conventions to preserve —
  view-builder registry (`primaryViews` in `ui/fyne/shell.go`), the
  settings-field registry (`ui/fyne/settings_schema.go`), the design
  tokens (`ui/fyne/theme.go` run #123), the release-contract gates
  (`internal/releasecontract`), the protected five docs
  (`agent.md`/`ai-notes.md`/`Dev-Notes.md`/`worklog.md`/`retired.md`).

## 2. Where the new architecture landed (and why)

| Surface | Chosen location | Reason |
|---|---|---|
| Design tokens (cross-engine) | `internal/theme` (new package) | Pure Go, importable by every renderer adapter; the Fyne projection stays in `ui/fyne/theme.go` derived FROM the catalog — one authority, zero view churn (only `theme.go` was rewritten; views keep consuming the same identifiers) |
| Renderer/engine registry | `internal/renderer` (new package) | The engine catalog is domain data (manifests, maturity, capabilities), not UI code; the gallery is registry-driven so it cannot drift from the truth source |
| Engine Gallery surface | `ui/fyne/view_engines.go` + the ninth `primaryViews` entry | "An in-app Engines/Appearance page reachable from the current shell using existing navigation patterns" — the registry IS the existing pattern (rail + compact strip + Ctrl+number all derive from it) |
| Live theme switcher | `ui/fyne/theme_service.go` wired through `App.Themes()` | Apply/preview live via `Settings().SetTheme` (repaint in place — no core reset); Save is the only persistence path (Fyne Preferences, never core config); Cancel restores |
| Window controls trio | `ui/fyne/window_controls.go`, rendered in the Engines view | Custom widgets (the repo's `dotWidget` idiom) with a GUARANTEED 44×44 DIP hit floor; Close routes through `App.RequestClose()` (the one graceful shutdown funnel); minimize/tray are disabled WITH reasons — Fyne v2.8.2 exposes no minimize/tray API, and the native OS titlebar provides the real behavior |
| Wails Web Workspace POC | `experiments/ui-engine-poc/wails-webview` (nested module) | Same isolation as the go-gui/gogpu POCs — outside the production dependency graph; the production build gains ZERO new dependencies |

## 3. Conventions honored (no speculative mass moves)

- No file relocations: the five protected docs kept their paths and
  roles; `Dev-Notes.md` untouched (no authorization in the current
  work order).
- New docs only where absent: `docs/ui/` (design system, touch matrix,
  accessibility) and `docs/architecture/RENDERER-*`; existing canonical
  docs (`UI-PLATFORM-AUDIT.md`, `RUNTIME-DIAGNOSTICS.md`,
  `WEBVIEW-SECURITY.md`) were UPDATED in place.
- The `headless` build tag discipline: every non-test `ui/fyne` file
  carries `//go:build !headless`; the new packages (`internal/theme`,
  `internal/renderer`) are pure Go and compile under both modes.
- One Go UI unchanged: the Fyne shell stays the production renderer;
  no renderer POC was added to the production module graph.

## 4. Honest capability accounting (vocabulary per the milestone)

- PASS (local): token catalog + contrast gates; registry truthfulness
  gates; gallery/theme/controls tests; full `ui/fyne` suite (structural
  goldens regenerated and reviewed — the only diff is the ninth
  destination); NSIS version-gate fixtures (pwsh 7.7); Windows
  `GOOS=windows go vet` of `internal/localipc` + headless `internal/...`
  build; the three POC modules' `go test ./...`.
- BLOCKED (this host): hosted GitHub runs (never triggered — user owns
  publishing); native Windows runtime (no Windows host); physical
  touch/screen-reader/GPU verification; the Wails WebView WINDOW (no
  WebKitGTK here — its bridge/security/feature layers are green, the
  runtime is `wails_runtime`-tagged for capable hosts).
- NOT RUN: physical Narrator/NVDA/AT-SPI/VoiceOver; real GPU adapters
  (the GoGPU POC's software renderer is the only backend ever executed
  here).

## 5. Follow-on obligations recorded in the roadmap

Companion-window launch path for preview engines; AT-SPI/VoiceOver
matrix rows; hosted-gate verification of the NSIS fix and the SHA pins
(after the user publishes); per-frame telemetry; the promotion gates
themselves (docs/ROADMAP.md).
