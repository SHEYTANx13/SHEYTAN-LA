//go:build !headless

// Package fyneui — the product design system. ONE palette, ONE spacing
// scale, ONE type treatment for every form factor (desktop and compact).
//
// v1.11.1 renderer convergence (UX-01): the palette is now DERIVED from
// the shared design-token catalog (internal/theme) — this file is the
// Fyne-side role mapping, not a second authority. Obsidian carries the
// pre-token dark identity VERBATIM (zero visual change, pinned by
// TestObsidianCarriesThePreTokenIdentity); every catalog theme renders
// through the same roleSet mapping, so switching themes swaps token sets
// without touching any view.
//
// Design tokens (run #123 Phase 0, docs/ui-contract.md): spacing scale,
// typography hierarchy, color roles for dark + light, radius/elevation
// rules and icon/density sizes are defined HERE (mirroring the shared
// catalog scales) and nowhere else — views consume tokens, never literals.
// Defining a token is not a visual change; applying one is (Phase 1+).
package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// roleSet — the Fyne-side projection of one catalog theme.
//
// ROLE VOCABULARY (run #123 Phase 0, docs/ui-contract.md): the fields map
// to the design-contract color roles as — surface = Surface, elevated =
// Elevated, accent = Accent, success = Success, warning = Warn, danger =
// Error (Fyne's error color name), muted = Muted.
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
	Focus        color.Color
}

// catalogTheme resolves a catalog id loudly — the catalog test pins every
// id used here, so a panic is a broken build, never a shipped fallback.
func catalogTheme(id string) themetokens.Theme {
	t, ok := themetokens.ByID(id)
	if !ok {
		panic(fmt.Sprintf("fyneui: theme %q missing from the shared catalog", id))
	}
	return t
}

// roleSetFromTheme projects one catalog theme into the Fyne-side role set.
func roleSetFromTheme(t themetokens.Theme) roleSet {
	return roleSet{
		Background:   mustHex(t.Colors.Background),
		Surface:      mustHex(t.Colors.Surface),
		Elevated:     mustHex(t.Colors.Elevated),
		Control:      mustHex(t.Colors.Control),
		ControlHover: mustHex(t.Colors.ControlHover),
		Border:       mustHex(t.Colors.Border),
		Text:         mustHex(t.Colors.Text),
		TextSoft:     mustHex(t.Colors.TextSoft),
		Muted:        mustHex(t.Colors.Muted),
		Accent:       mustHex(t.Colors.Accent),
		AccentHover:  mustHex(t.Colors.AccentHover),
		AccentDim:    mustHex(t.Colors.AccentDim),
		AccentText:   mustHex(t.Colors.AccentText),
		Success:      mustHex(t.Colors.Success),
		Error:        mustHex(t.Colors.Danger),
		Warn:         mustHex(t.Colors.Warning),
		Info:         mustHex(t.Colors.Info),
		Focus:        mustHex(t.Colors.Focus),
	}
}

// Palette — the product's dark role set, now DERIVED from the shared token
// catalog: Obsidian carries the pre-token identity (run #123 Phase 0)
// verbatim, so the rendered dark identity is byte-for-byte unchanged.
// LightPalette re-expresses the SAME roles for light surfaces (the light
// catalog theme).
var (
	Palette      = roleSetFromTheme(catalogTheme("obsidian"))
	LightPalette = roleSetFromTheme(catalogTheme("light"))
)

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
// Assigned from the shared catalog scale so ONE authority holds the
// numbers (mirrored for the other engines in internal/theme).
var (
	SpaceXS  = themetokens.SpacingScale.XS
	SpaceS   = themetokens.SpacingScale.S
	SpaceM   = themetokens.SpacingScale.M
	SpaceL   = themetokens.SpacingScale.L
	SpaceXL  = themetokens.SpacingScale.XL
	SpaceXXL = themetokens.SpacingScale.XXL
)

// Typography hierarchy (dp) — ONE text scale for every surface, rooted at
// TypeBody = the theme's SizeNameText root (14). Run #123 Phase 0.
var (
	TypeTitle   = themetokens.TypeScale.Title   // view titles (PageHeader)
	TypeSection = themetokens.TypeScale.Section // section / panel headings
	TypeBody    = themetokens.TypeScale.Body    // body text — the root size
	TypeMono    = themetokens.TypeScale.Mono    // monospace evidence, code, JSON
	TypeStatus  = themetokens.TypeScale.Status  // status line, badges, timestamps
)

// Radius rules (dp) — painted surfaces only (Fyne has no per-widget
// radius). Run #123 Phase 0; replaces the single cardRadius token.
var (
	RadiusControl = themetokens.RadiusScale.Control // chips, badges, compact controls
	RadiusCard    = themetokens.RadiusScale.Card    // cards and panels (the old cardRadius)
	RadiusDialog  = themetokens.RadiusScale.Dialog  // dialogs and modal surfaces
)

// Touch-target floor (dp) — UX-06 acceptance: 44×44 minimum, 48 preferred
// for primary actions; kept in lockstep with the shared token contract.
const (
	TouchTargetMin       = float32(44)
	TouchTargetPreferred = float32(48)
)

// Elevation rules — shadow alpha steps for painted overlays, ordered by
// visual weight. ElevationMid is the theme shadow alpha today. Run #123
// Phase 0. (Fyne-presentation specifics: not part of the cross-engine
// token catalog.)
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
// densest logs to primary navigation. RowRelaxed doubles as the rail's
// touch-comfortable row floor. Run #123 Phase 0.
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

// appTheme implements fyne.Theme over one catalog theme.
type appTheme struct {
	fyne.Theme
	active themetokens.Theme
}

// NewTheme builds the product theme from the premium default (Hellish).
func NewTheme() fyne.Theme {
	return NewThemeFor(themetokens.DefaultID)
}

// NewThemeFor builds the product theme from one catalog id. The theme
// service validates ids BEFORE calling this; an unknown id falls back to
// the default with the shared role mapping intact (fail-safe rendering,
// never a crash from a preference value).
func NewThemeFor(id string) fyne.Theme {
	t, ok := themetokens.ByID(id)
	if !ok {
		t = themetokens.Default()
	}
	return NewThemeFromTheme(t)
}

// NewThemeFromTheme builds the product theme over an already-validated
// catalog theme (the service's write path).
func NewThemeFromTheme(t themetokens.Theme) fyne.Theme {
	var base fyne.Theme = theme.DarkTheme()
	if t.Variant == "light" {
		base = theme.LightTheme()
	}
	return &appTheme{Theme: base, active: t}
}

// activeThemeID reports the catalog id the app theme renders (tests pin
// theme switching without reaching into the struct).
func (t *appTheme) activeThemeID() string { return t.active.ID }

func (t *appTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	c := t.active.Colors
	switch n {
	case theme.ColorNameBackground:
		return mustHex(c.Background)
	case theme.ColorNameForeground:
		return mustHex(c.Text)
	case theme.ColorNamePrimary:
		return mustHex(c.Accent)
	case theme.ColorNameHover:
		return mustHex(c.ControlHover)
	case theme.ColorNameInputBorder:
		return mustHex(c.Border)
	case theme.ColorNameButton:
		return mustHex(c.Control)
	case theme.ColorNameInputBackground:
		return mustHex(c.Surface)
	case theme.ColorNamePlaceHolder:
		return mustHex(c.Muted)
	case theme.ColorNameDisabled:
		return mustHex(c.Muted)
	case theme.ColorNameDisabledButton:
		return mustHex(c.Elevated)
	case theme.ColorNameSelection:
		return mustHex(c.AccentDim)
	case theme.ColorNameFocus:
		return mustHex(c.Focus)
	case theme.ColorNameSeparator:
		return mustHex(c.Border)
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0x50}
	case theme.ColorNameSuccess:
		return mustHex(c.Success)
	case theme.ColorNameError:
		return mustHex(c.Danger)
	case theme.ColorNameWarning:
		return mustHex(c.Warning)
	default:
		return t.Theme.Color(n, v)
	}
}

// size returns the product type scale (rooted at 14 for dense panels).
func (t *appTheme) Size(s fyne.ThemeSizeName) float32 {
	switch s {
	case theme.SizeNameText:
		return TypeBody
	case theme.SizeNameSeparatorThickness:
		return 1
	case theme.SizeNamePadding:
		return SpaceS
	case theme.SizeNameInlineIcon:
		return IconM
	default:
		return t.Theme.Size(s)
	}
}
