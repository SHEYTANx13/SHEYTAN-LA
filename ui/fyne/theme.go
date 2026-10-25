//go:build fyne

// Package fyneui — the one-Go-UI migration's first package (run-#50 R4).
//
// THE END STATE (operator charter C19): ONE Go UI replacing every other
// UI language, launched and controlled from the one Go CLI, one codebase
// unified across screen sizes. This package is the SPIKE phase of that
// migration program: a minimal shell window carrying the product's theme
// tokens, a size-driven layout, and navigation around one placeholder
// panel. It is additive and isolated — the shipped Go core, the Qt host,
// and the hosted workflow are untouched, and this package is never a
// required hosted gate (build tag `fyne`).
//
// Layout discipline: every dimension derives from container geometry or
// the token scale — NO fixed pixel layout. Fyne reflows on window resize
// by construction; the shell only chooses topologies (nav beside content,
// or stacked under it) from the window's width class.
//
// Status: SPIKE — compiles locally (status recorded per §8 in the run-#50
// report); NOT yet hosted-verified; parity with the Qt surface is the
// migration program's NEXT phase, tracked in docs/ROADMAP.md.
package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette — the product's ONE theme token source, mirrored verbatim from
// ui/qt/qml/Theme.qml (run-#48 R1a). The Qt surface and this package must
// never drift: ONE product, ONE palette. When the parity phase lands, a
// shared token file (generated once, consumed by both) replaces this
// mirror — the spike deliberately does not touch the Qt tree.
var Palette = struct {
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
}{
	Background:   mustHex("#121214"),
	Surface:      mustHex("#17171a"),
	Elevated:     mustHex("#1f1f23"),
	Control:      mustHex("#2c2c31"),
	ControlHover: mustHex("#33333a"),
	Border:       mustHex("#2a2a2e"),
	Text:         mustHex("#e8e8ea"),
	TextSoft:     mustHex("#b8b8c0"),
	Muted:        mustHex("#8a8a92"),
	Accent:       mustHex("#b8a6e8"),
	AccentHover:  mustHex("#c9bbf0"),
	AccentDim:    mustHex("#2a2440"),
	AccentText:   mustHex("#1a1524"),
	Success:      mustHex("#7dd3a0"),
	Error:        mustHex("#e8a0a0"),
	Warn:         mustHex("#e8c268"),
	Info:         mustHex("#8fb8dd"),
}

// Spacing / typography / radius scales — the same names as Theme.qml so
// the token mapping is auditable line by line.
const (
	SpaceS  = float32(8)
	SpaceM  = float32(12)
	SpaceL  = float32(16)
	SpaceXL = float32(24)

	FontXS = float32(11)
	FontS  = float32(12)
	FontM  = float32(13)
	FontL  = float32(15)
	FontXL = float32(18)

	RadiusS = float32(4)
	RadiusM = float32(6)
	RadiusL = float32(10)
)

// NavWidthClass is the boundary under which the shell stacks navigation
// above content instead of placing it in a side rail. Derived from the
// window's current width at layout time — never a fixed pixel layout.
const NavWidthClass = float32(560)

type productTheme struct {
	fyne.Theme
}

// Color overrides ONLY the token surfaces the product defines; everything
// else inherits the stock dark theme so the spike stays consistent with
// zero duplicated widget styling. The variant parameter (dark/light) is
// passed through untouched — the product ships one dark palette.
func (t productTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return Palette.Background
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return Palette.Surface
	case theme.ColorNameForeground:
		return Palette.Text
	case theme.ColorNamePrimary:
		return Palette.Accent
	case theme.ColorNameInputBackground:
		return Palette.Control
	case theme.ColorNameButton:
		return Palette.Control
	case theme.ColorNameHover:
		return Palette.ControlHover
	case theme.ColorNameInputBorder:
		return Palette.Border
	case theme.ColorNameSeparator:
		return Palette.Border
	case theme.ColorNameDisabled:
		return Palette.Muted
	default:
		return t.Theme.Color(n, v)
	}
}

// Size maps the product's typography scale onto the theme's named sizes;
// unspecified sizes inherit the stock theme.
func (t productTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return FontM
	case theme.SizeNameSeparatorThickness:
		return RadiusS
	default:
		return t.Theme.Size(n)
	}
}

// NewTheme returns the product theme wrapping fyne's dark theme.
func NewTheme() fyne.Theme {
	return productTheme{Theme: theme.DarkTheme()}
}

// mustHex parses the #rrggbb token strings mirrored from Theme.qml; a
// malformed token is a programming error and must fail loudly, never
// render a wrong color.
func mustHex(s string) color.Color {
	if len(s) != 7 || s[0] != '#' {
		panic("fyneui: malformed theme token " + s)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		panic("fyneui: malformed theme token " + s + ": " + err.Error())
	}
	return color.NRGBA{R: r, G: g, B: b, A: 0xff}
}
