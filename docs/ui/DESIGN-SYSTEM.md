# SHEYTAN Design System — the shared token contract (v1.11.1)

> The ONE token authority is CODE: `internal/theme` (schema-versioned,
> WCAG-contrast-validated, JSON-serializable). This page documents the
> catalog and each engine's mapping status. A renderer that cannot
> implement a token documents the degradation — it never invents a
> different design.

## 1. The six themes (UX-01)

| Theme | Variant | Identity | Contrast floor (measured, gated) |
|---|---|---|---|
| **SHEYTAN Hellish** | dark | Charcoal + red signature — the **premium default** | text ≥ 4.5:1 on surface; focus ≥ 3:1 |
| **Obsidian** | dark | The pre-token neutral dark identity (purple accent) — carried VERBATIM from the run-#123 palette | same |
| **Graphite** | dark | Low-contrast neutral dark that stays readable | soft text ≥ 4.5:1 (AA held) |
| **Light** | light | High-legibility light surfaces | same |
| **High Contrast** | dark | White on true black, high-visibility accent | text ≥ **7:1** (AAA floor) |
| **OLED Black** | dark | True black for OLED power/contrast; the red signature at full depth | same |

The catalog test (`internal/theme/tokens_test.go`) validates every theme
against the contrast floors on EVERY build — a palette regression cannot
ship. The light variant is the ONLY light theme; all others are dark.

## 2. Live switching contract

Selecting a theme in the Engines & Appearance view applies it LIVE
(`fyne.App.Settings().SetTheme` repaints every widget in place — the
core is never recreated, navigation is never reset, no session data is
touched: pinned by `TestThemeSwitchKeepsNavigation`). **Save** is the
only persistence path (Fyne Preferences under the app id — presentation
state, never core config); **Cancel** restores the saved theme. An
unknown persisted id falls back to the premium default (fail-safe).

## 3. Semantic color roles (the only names renderers may consume)

Background, Surface, Elevated, Control, ControlHover, Border, Text,
TextSoft, Muted, Accent, AccentHover, AccentDim, AccentText, Success,
Warning, Danger, Info, Focus. Hex lives in `internal/theme` and nowhere
else. The typed serialized form (for web surfaces) is
`internal/theme`'s `Theme.JSON()` — schema version, the theme, and the
shared spacing/type/radius/touch tokens in one document.

## 4. Shared non-color tokens

- Spacing: XS 4 / S 8 / M 12 / L 16 / XL 24 / XXL 32 (DIP).
- Type scale: Title 20 / Section 16 / Body 14 (root) / Mono 13 / Status 12.
- Radius: Control 8 / Card 10 / Dialog 14 (painted surfaces only).
- Touch: **44×44 DIP minimum, 48 preferred**, 8–12 DIP practical spacing.
- Elevation (Fyne-presentation): shadow alpha steps 0x00/0x28/0x50/0x78.
- Responsive bands: compact <600 · tablet 600–899 · desktop 900–1399 ·
  wide 1400–2199 · ultrawide ≥2200 (the Fyne shell's tested four-class
  policy maps onto these bands — see
  [`TOUCH-RESPONSIVE-MATRIX.md`](TOUCH-RESPONSIVE-MATRIX.md)).

## 5. Per-engine mapping status (honesty rows)

| Engine | Mapping | Status |
|---|---|---|
| **Fyne** | `ui/fyne/theme.go` — every role maps to a Fyne ThemeColorName; Obsidian carries the pre-token identity verbatim | **full — wired** |
| Go-Gui | role → its theming surface | **designed** (this document); wired only inside the POC |
| GoGPU UI | upstream documents 4 theme systems | **designed**; unwired |
| Wails WebView | tokens → CSS custom properties via the JSON form | **designed**; unwired (POC serializes; runtime is `wails_runtime`-tagged) |

## 6. Custom themes

Optional custom theme editing with validated color/typography/spacing
values is roadmap work (the catalog's `Validate` is the validation
authority for it); v1.11.1 ships the six curated themes.
