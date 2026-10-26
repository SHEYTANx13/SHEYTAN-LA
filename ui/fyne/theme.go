//go:build !headless

// Package fyneui — the product design system. ONE palette, ONE spacing
// scale, ONE type treatment for every form factor (desktop and compact).
// The palette is carried over verbatim from the retired Qt Theme.qml
// tokens so the visual identity survives the migration; the Fyne theme
// applies them across every widget class.
package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Palette — the product's ONE theme token source.
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

// Spacing scale (dp) — every layout inset derives from these tokens.
const (
	SpaceXS  = float32(4)
	SpaceS   = float32(8)
	SpaceM   = float32(12)
	SpaceL   = float32(16)
	SpaceXL  = float32(24)
	SpaceXXL = float32(32)
)

// Radius for cards/dialogs (Fyne lacks per-widget radius; used for custom
// painted surfaces).
const cardRadius = float32(10)

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
