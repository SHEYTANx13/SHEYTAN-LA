# UI Design Contract

**Binding for every UI/UX upgrade phase.** This is the single, documented design contract that every later phase of the UI/UX Hybrid Upgrade (docs/Dev-Notes.md, "UI/UX Hybrid Upgrade — Antigravity + Web UIs + Google AI Studio") obeys.

Provenance: created by the run #123 fix session, 2026-11-01, as Phase 0 of the operator-authorized UI/UX research inserted into docs/Dev-Notes.md. Phase status: **Phase 0 (Design System and Contracts) — executed**; Phases 1–5 are deferred by the operator's own execution order (review this contract before unlocking Phase 1).

---

## 1. Token source of truth

All design tokens live in `ui/fyne/theme.go` and nowhere else. Views consume tokens, never literals — from Phase 1 on, no view hardcodes a spacing, size, or color value. Defining a token is not a visual change; applying one is (each application is a Phase 1+ edit with its own evidence).

### Spacing scale (dp)

| Token | Value | Use |
|-------|-------|-----|
| `SpaceXS` | 4 | intra-control gaps, icon-to-label |
| `SpaceS` | 8 | control padding (theme `SizeNamePadding` today) |
| `SpaceM` | 12 | grouped-control gaps |
| `SpaceL` | 16 | view margins, card insets |
| `SpaceXL` | 24 | section separation |
| `SpaceXXL` | 32 | view-level breathing room |

### Typography hierarchy (dp)

| Token | Value | Use |
|-------|-------|-----|
| `TypeTitle` | 20 | view titles (PageHeader) |
| `TypeSection` | 16 | section / panel headings |
| `TypeBody` | 14 | body text — the root size (`SizeNameText` today) |
| `TypeMono` | 13 | monospace evidence, code, JSON |
| `TypeStatus` | 12 | status line, badges, timestamps |

### Color roles — dark + light

The role vocabulary: **surface, elevated, accent, success, warning, danger, muted** (plus background/control/border/text text roles). Mapping to the `roleSet` fields: surface = `Surface`, elevated = `Elevated`, accent = `Accent`, success = `Success`, warning = `Warn`, danger = `Error` (Fyne's error color name), muted = `Muted`.

- `Palette` (dark) is the product identity root — values carried verbatim from the retired Qt Theme.qml tokens, byte-identical through this contract's creation.
- `LightPalette` is the SAME role system re-expressed for light surfaces. It is defined and documented but **not wired into `appTheme`** — activating a light variant is a Phase 1+ visual change.
- `PaletteFor(v fyne.ThemeVariant)` is the one variant-aware accessor; new code must resolve roles through it or through `Palette` directly (dark identity) — never through ad-hoc hex values.

### Radius, elevation, icon, density

| Family | Tokens |
|--------|--------|
| Radius (painted surfaces only; Fyne has no per-widget radius) | `RadiusControl` 8 (chips, badges) · `RadiusCard` 10 (cards, panels — the old `cardRadius`) · `RadiusDialog` 14 (dialogs) |
| Elevation (shadow alpha steps for painted overlays) | `ElevationFlat` 0x00 · `ElevationLow` 0x28 (cards, hover lifts) · `ElevationMid` 0x50 (panels, popovers — theme shadow today) · `ElevationHigh` 0x78 (dialogs, modals) |
| Icon sizes | `IconS` 14 (inline/dense) · `IconM` 18 (default — `SizeNameInlineIcon` today) · `IconL` 24 (rail, hero actions) |
| Density (row heights) | `RowCompact` 28 (sessions, logs) · `RowRegular` 36 (standard rows) · `RowRelaxed` 44 (primary navigation) |

---

## 2. Interaction contracts

Each contract states the target behavior, what is in force today, and the phase that lands the remainder. Nothing here may be implemented in a way that moves policy or state into the UI (one authority per concern).

| # | Contract | Status today (verified against the tree) | Landing phase |
|---|----------|------------------------------------------|---------------|
| 1 | Composer **Enter = send, Shift+Enter = newline** | Chat and Agent wire `input.OnSubmitted = send` (view_chat.go:110, view_agent.go:101); the Chat placeholder documents the pairing. The explicit keyboard mapping (incl. multi-line entry semantics) is pinned by tests in Phase 1. | Phase 1 |
| 2 | **Focus returns after send** | Not yet implemented (no focus call after send). | Phase 1 |
| 3 | **Stop replaces Send while busy** (web + AI Studio pattern) | Both buttons exist side-by-side; busy DISABLES Send and ENABLES Stop (view_chat.go busy path). Target: one slot — Stop takes Send's place visually while a run is live. | Phase 1 |
| 4 | **The live bubble never becomes a final message** (settlement rule) | IN FORCE: settlement reloads the authoritative transcript (state.go SETTLEMENT path); the stream accumulator replaces, never appends (pinned by `TestStreamAccumulatorReplacesNeverAppends`). The live surface is provisional; settlement is the only path to final transcript state. | In force — preserve |
| 5 | **Empty states prime capability (sample prompts / mode chips) without violating zero-session Send** | Honest `EmptySurface` exists (widgets.go); no capability priming yet. Priming affordances never auto-send and never bypass the composer; an empty session list must not wedge the composer (zero-session send contract). | Phase 1 |
| 6 | **The status line remains truthful only** | IN FORCE: the status line reports only real state — connection, core version, active model, live-stream progress (shell.go); no fabricated telemetry, unknown rendered as unknown. | In force — preserve |
| 7 | **Primary views stay distinct and reachable** (AI Studio left-rail discipline) | IN FORCE: 7-view IA with wide rail / compact rail / top strip (shell.go); every primary view one navigation action deep; Chat vs Agent hard separation preserved. | In force — preserve |

Additional standing rules for every phase (from the Dev-Notes research, binding): native Fyne stays primary; Workspace stays on-demand, loopback-only, token-gated, CSP-strict; no second UI stack; no new authorities; prefer pure layout/theme/presentation changes over new runtime surfaces; every claim of "smoother or better" is backed by a test or an observable before/after.

---

## 3. Contract governance

- This document is amended only deliberately, in the same batch as the change it describes (the same discipline as the release-contract tests).
- Token renames or additions update section 1 in the same commit; interaction-contract status flips only with evidence (test or observable).
- The evidence ladder for UI claims: `go test ./ui/fyne/` green + the hosted Fyne UI gates job; visual claims additionally need a before/after observable. Evidence class never inflated (docs/UPDATE.md honesty rules).
- The dark palette is the identity root: any change to a `Palette` value is a deliberate identity decision recorded here, never a side effect.

---

*UI/UX Hybrid Upgrade Phase 0 artifact. Phase plan and operator execution order: docs/Dev-Notes.md.*
