//go:build !headless

// Package fyneui — the product design system. ONE palette, ONE spacing
// scale, ONE type treatment for every form factor (desktop and compact).
// The palette is carried over verbatim from the retired Qt Theme.qml
// tokens so the visual identity survives the migration; the Fyne theme
// applies them across every widget class.
//
// Design tokens (run #123 Phase 0, docs/ui-contract.md): spacing scale,
// typography hierarchy, color roles for dark + light, radius/elevation
// rules and icon/density sizes are defined HERE and nowhere else — views
// consume tokens, never literals. Defining a token is not a visual
// change; applying one is (Phase 1+).
package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette — the product's ONE theme token source.
//
// ROLE VOCABULARY (run #123 Phase 0, docs/ui-contract.md): the fields map
// to the design-contract color roles as — surface = Surface, elevated =
// Elevated, accent = Accent, success = Success, warning = Warn, danger =
// Error (Fyne's error color name), muted = Muted. Palette is the DARK
// variant and the product identity root; LightPalette re-expresses the
// SAME roles for light surfaces.
type roleSet struct {
	Background   color.Color
	Surface      color.Color
	Elevated     color.Color
	Control      color.Color
	ControlHover color.Color
	Border       color.Color
	Text         color.Color
	TextSoft     color.Color
	Muted        color.Color
	Accent       color.Color
	AccentHover  color.Color
	AccentDim    color.Color
	AccentText   color.Color
	Success      color.Color
	Error        color.Color
	Warn         color.Color
	Info         color.Color
}

var Palette = roleSet{
	Background:   mustHex("#121214"),
	Surface:      mustHex("#17171a"),
	Elevated:     mustHex("#1f1f23"),
	Control:      mustHex("#2c2c31"),
	ControlHover: mustHex("#33333a"),
	Border:       mustHex("#3a3a42"),
	Text:         mustHex("#e8e8ea"),
	TextSoft:     mustHex("#b9b9c0"),
	Muted:        mustHex("#8a8a94"),
	Accent:       mustHex("#7c6cff"),
	AccentHover:  mustHex("#8d7fff"),
	AccentDim:    mustHex("#2a2440"),
	AccentText:   mustHex("#c9c2ff"),
	Success:      mustHex("#3ecf8e"),
	Error:        mustHex("#ff5d5d"),
	Warn:         mustHex("#ffb454"),
	Info:         mustHex("#4cc2ff"),
}

// LightPalette — the SAME role system re-expressed for light surfaces
// (run #123 Phase 0 token contract). NOT wired into appTheme yet: donning
// a light variant is a later-phase visual change under docs/ui-contract.md;
// today every surface renders from the dark identity root.
var LightPalette = roleSet{
	Background:   mustHex("#f5f5f7"),
	Surface:      mustHex("#ffffff"),
	Elevated:     mustHex("#ebebef"),
	Control:      mustHex("#e3e3e9"),
	ControlHover: mustHex("#d9d9e1"),
	Border:       mustHex("#c6c6d0"),
	Text:         mustHex("#1b1b1f"),
	TextSoft:     mustHex("#46464e"),
	Muted:        mustHex("#75757f"),
	Accent:       mustHex("#5d4ed6"),
	AccentHover:  mustHex("#6c5ee0"),
	AccentDim:    mustHex("#e6e3fb"),
	AccentText:   mustHex("#4638b8"),
	Success:      mustHex("#1e9e6a"),
	Error:        mustHex("#d5484c"),
	Warn:         mustHex("#a8680f"),
	Info:         mustHex("#177cc0"),
}

// PaletteFor returns the role set for the theme variant. Dark is the
// identity root; the light variant is the same roles, re-expressed.
func PaletteFor(v fyne.ThemeVariant) roleSet {
	if v == theme.VariantLight {
		return LightPalette
	}
	return Palette
}

// Spacing scale (dp) — every layout inset derives from these tokens
// (design contract: docs/ui-contract.md; no view hardcodes another value).
const (
	SpaceXS  = float32(4)
	SpaceS   = float32(8)
	SpaceM   = float32(12)
	SpaceL   = float32(16)
	SpaceXL  = float32(24)
	SpaceXXL = float32(32)
)

// Typography hierarchy (dp) — ONE text scale for every surface, rooted at
// TypeBody = the theme's SizeNameText root (14). Run #123 Phase 0.
const (
	TypeTitle   = float32(20) // view titles (PageHeader)
	TypeSection = float32(16) // section / panel headings
	TypeBody    = float32(14) // body text — the root size
	TypeMono    = float32(13) // monospace evidence, code, JSON
	TypeStatus  = float32(12) // status line, badges, timestamps
)

// Radius rules (dp) — painted surfaces only (Fyne has no per-widget
// radius). Run #123 Phase 0; replaces the single cardRadius token.
const (
	RadiusControl = float32(8)  // chips, badges, compact controls
	RadiusCard    = float32(10) // cards and panels (the old cardRadius)
	RadiusDialog  = float32(14) // dialogs and modal surfaces
)

// Elevation rules — shadow alpha steps for painted overlays, ordered by
// visual weight. ElevationMid is the theme shadow alpha today. Run #123
// Phase 0.
const (
	ElevationFlat = uint8(0x00) // flush with the surface
	ElevationLow  = uint8(0x28) // cards, hover lifts
	ElevationMid  = uint8(0x50) // panels, popovers (theme shadow today)
	ElevationHigh = uint8(0x78) // dialogs, modal overlays
)

// Icon sizes (dp) — the three legal icon sizes; IconM is the theme's
// SizeNameInlineIcon today. Run #123 Phase 0.
const (
	IconS = float32(14) // inline with body text, dense rows
	IconM = float32(18) // default inline icon
	IconL = float32(24) // primary rail, hero actions
)

// Density rules (dp) — row heights for list-like surfaces, from the
// densest logs to primary navigation. Run #123 Phase 0.
const (
	RowCompact = float32(28) // dense lists: sessions, logs
	RowRegular = float32(36) // standard rows, settings lines
	RowRelaxed = float32(44) // primary navigation rows
)

func mustHex(s string) color.Color {
	var r, g, b int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff} // loud, never silent
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}

// appTheme implements fyne.Theme over the product palette.
type appTheme struct {
	fyne.Theme
}

// NewTheme builds the product theme.
func NewTheme() fyne.Theme {
	return &appTheme{Theme: theme.DarkTheme()}
}

func (t *appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return Palette.Background
	case theme.ColorNameForeground:
		return Palette.Text
	case theme.ColorNamePrimary:
		return Palette.Accent
	case theme.ColorNameHover:
		return Palette.ControlHover
	case theme.ColorNameInputBorder:
		return Palette.Border
	case theme.ColorNameButton:
		return Palette.Control
	case theme.ColorNameInputBackground:
		return Palette.Surface
	case theme.ColorNamePlaceHolder:
		return Palette.Muted
	case theme.ColorNameDisabled:
		return Palette.Muted
	case theme.ColorNameDisabledButton:
		return Palette.Elevated
	case theme.ColorNameSelection:
		return Palette.AccentDim
	case theme.ColorNameFocus:
		return Palette.Accent
	case theme.ColorNameSeparator:
		return Palette.Border
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0x50}
	case theme.ColorNameSuccess:
		return Palette.Success
	case theme.ColorNameError:
		return Palette.Error
	case theme.ColorNameWarning:
		return Palette.Warn
	default:
		return t.Theme.Color(n, v)
	}
}

// size returns the product type scale (rooted at 14 for dense panels).
func (t *appTheme) Size(s fyne.ThemeSizeName) float32 {
	switch s {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNamePadding:
		return SpaceS
	case theme.SizeNameInlineIcon:
		return 18
	default:
		return t.Theme.Size(s)
	}
}
