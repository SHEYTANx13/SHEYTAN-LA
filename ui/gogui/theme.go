//go:build !headless

// theme.go — the go-gui-side projection of the shared design-token
// catalog (internal/theme). ONE palette authority: this file MAPS catalog
// tokens into gui.ThemeCfg roles — it owns no second palette. The
// premium default is the catalog's DefaultID ("hellish": charcoal surfaces
// + red signature accent); obsidian and light ride the same mapping, so
// the Engines & Appearance theme switcher swaps token sets without
// touching any view (the exact contract ui/fyne/theme.go ships for Fyne).
package goguiui

import (
	"fmt"

	"github.com/go-gui-org/go-gui/gui"

	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// catalogTheme resolves a catalog id loudly — the catalog test pins every
// id used here, so a panic is a broken build, never a shipped fallback.
func catalogTheme(id string) themetokens.Theme {
	t, ok := themetokens.ByID(id)
	if !ok {
		panic(fmt.Sprintf("goguiui: theme %q missing from the shared catalog", id))
	}
	return t
}

// gogoColor converts a token hex into a gui.Color (straight-alpha RGBA).
func gogoColor(hex string) gui.Color {
	n, ok := hexRGBA(hex)
	if !ok {
		return gui.RGBA(0xff, 0x00, 0xff, 0xff) // loud magenta, never silent
	}
	return gui.RGBA(n[0], n[1], n[2], n[3])
}

// hexRGBA parses "#rrggbb" into straight-alpha RGBA bytes.
func hexRGBA(s string) ([4]uint8, bool) {
	var out [4]uint8
	if len(s) != 7 {
		return out, false
	}
	var v [3]int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &v[0], &v[1], &v[2]); err != nil {
		return out, false
	}
	out[0], out[1], out[2], out[3] = uint8(v[0]), uint8(v[1]), uint8(v[2]), 0xff
	return out, true
}

// ThemeFor builds the go-gui theme for one catalog id. Unknown ids fall
// back to the default with the shared role mapping intact (fail-safe
// rendering, never a crash from a preference value).
func ThemeFor(id string) gui.Theme {
	t, ok := themetokens.ByID(id)
	if !ok {
		t = themetokens.Default()
	}
	return ThemeFromCatalog(t)
}

// ThemeFromCatalog projects one catalog theme into a gui.Theme via
// ThemeMaker. The role mapping mirrors ui/fyne/theme.go's Color() switch:
//
//	catalog role        → go-gui role
//	Background          → ColorBackground
//	Surface             → ColorPanel          (cards/panels)
//	Elevated            → ColorInterior       (elevated interiors)
//	Control             → control base (buttons/inputs via ColorControl slots)
//	ControlHover        → ColorHover
//	Border              → ColorBorder + ColorSeparator
//	Text                → TextStyleDef.Color
//	TextSoft            → ColorTextSecondary
//	Muted               → ColorTextPlaceholder/Disabled
//	Accent family       → ColorAccent ramp (+ ColorSelect)
//	Success/Warning/Danger/Info → ColorSuccess/Warning/Error
//	Focus               → ColorFocus/ColorBorderFocus
//
// Spacing, radius and the text ladder come from the shared scales so the
// cross-engine contract (docs/ui/DESIGN-SYSTEM.md) stays one authority.
func ThemeFromCatalog(t themetokens.Theme) gui.Theme {
	c := t.Colors
	ng := themetokens.SpacingScale
	ts := themetokens.TypeScale
	rs := themetokens.RadiusScale
	cfg := gui.ThemeCfg{
		Name: "SHEYTAN-LA " + t.Name,

		TextStyleDef: gui.TextStyle{
			Color: gogoColor(c.Text),
			Size:  ts.Body,
		},
		MonoFontFamily: "", // toolkit mono default

		// Spacing ladder from the shared scale.
		PaddingSmall:  gui.PadAll(ng.XS),
		PaddingMedium: gui.PadAll(ng.S),
		PaddingLarge:  gui.PadAll(ng.L),

		SpacingTight:  ng.XS,
		SpacingSmall:  ng.S,
		SpacingMedium: ng.M,
		SpacingLarge:  ng.L,

		// Type ladder rooted at the shared body size.
		SizeTextSmall:  ts.Status,
		SizeTextMedium: ts.Body,
		SizeTextLarge:  ts.Section,
		SizeTextXLarge: ts.Title,

		// Shape.
		SizeBorder:   1,
		Radius:       rs.Control,
		RadiusSmall:  rs.Control,
		RadiusMedium: rs.Card,
		RadiusLarge:  rs.Dialog,

		// Surfaces.
		ColorBackground: gogoColor(c.Background),
		ColorPanel:      gogoColor(c.Surface),
		ColorInterior:   gogoColor(c.Elevated),
		ColorHover:      gogoColor(c.ControlHover),
		ColorFocus:      gogoColor(c.Focus),
		ColorActive:     gogoColor(c.ControlHover),
		ColorBorder:     gogoColor(c.Border),
		ColorSeparator:  gogoColor(c.Border),

		// Text roles.
		ColorTextSecondary:   gogoColor(c.TextSoft),
		ColorTextLabel:       gogoColor(c.TextSoft),
		ColorTextDisabled:    gogoColor(c.Muted),
		ColorTextPlaceholder: gogoColor(c.Muted),

		// Accent ramp: the catalog drives hover/dim explicitly, so the
		// mapping stays faithful to the tokens instead of deriving.
		ColorSelect:       gogoColor(c.AccentDim),
		ColorTextOnSelect: gogoColor(c.AccentText),
		ColorAccent:       gogoColor(c.Accent),
		ColorAccentHover:  gogoColor(c.AccentHover),
		ColorAccentSubtle: gogoColor(c.AccentDim),
		ColorTextOnAccent: onAccentColor(c.Accent),

		// Semantics.
		ColorSuccess: gogoColor(c.Success),
		ColorWarning: gogoColor(c.Warning),
		ColorError:   gogoColor(c.Danger),
	}
	if t.Variant == "light" {
		// Light surfaces want dark text on accent; the luminance rule
		// below already decides it per catalog, so nothing extra here.
		cfg.TitlebarDark = false
	} else {
		cfg.TitlebarDark = true
	}
	return gui.ThemeMaker(cfg)
}

// onAccentColor picks readable text on the accent by sRGB luminance
// (the same rule ThemeMaker derives ColorTextOnAccent with).
func onAccentColor(hex string) gui.Color {
	n, ok := hexRGBA(hex)
	if !ok {
		return gui.RGBA(0, 0, 0, 255)
	}
	lum := 0.2126*float64(n[0]) + 0.7152*float64(n[1]) + 0.0722*float64(n[2])
	if lum > 140 {
		return gui.RGBA(0x10, 0x10, 0x10, 0xff)
	}
	return gui.RGBA(0xff, 0xff, 0xff, 0xff)
}

// ApplyTheme installs the catalog theme id as the app default. MUST run
// on the UI thread (go-gui contract: SetTheme is frame-thread only) —
// background callers ride uiDo.
func ApplyTheme(id string) {
	gui.SetTheme(ThemeFor(id))
}

// ProductTheme applies the premium default (Hellish).
func ProductTheme() {
	ApplyTheme(themetokens.DefaultID)
}

// ThemeIDsForPicker returns the catalog ids the Engines & Appearance
// switcher offers (display order).
func ThemeIDsForPicker() []string { return themetokens.IDs() }
