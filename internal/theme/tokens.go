// Package theme — the versioned SHEYTAN design-token source (v1.11.1,
// UX-01). ONE authority for the product's semantic color roles and the six
// supported themes, plus the shared typography, spacing, radius, elevation,
// touch-target and responsive-band tokens. Renderers MAP tokens to their
// native theme systems (the Go-Gui, GoGPU UI and
// Wails CSS mappings are documented degradations in docs/ui/DESIGN-SYSTEM.md)
// — no renderer owns a second palette and none silently invents a design.
//
// Every theme carries a typed serialized representation (JSON with stable
// role names) so external WebView surfaces can consume exactly the same
// tokens the native shells render.
package theme

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"
)

// SchemaVersion is the token contract version. Bump ONLY on a breaking
// role rename/removal; additive roles keep the version.
const SchemaVersion = 1

// ColorRoles are the semantic color roles. These are the ONLY names a
// renderer may consume — raw hex lives here and nowhere else.
type ColorRoles struct {
	Background   string `json:"background"`
	Surface      string `json:"surface"`
	Elevated     string `json:"elevated"`
	Control      string `json:"control"`
	ControlHover string `json:"controlHover"`
	Border       string `json:"border"`
	Text         string `json:"text"`
	TextSoft     string `json:"textSoft"`
	Muted        string `json:"muted"`
	Accent       string `json:"accent"`
	AccentHover  string `json:"accentHover"`
	AccentDim    string `json:"accentDim"`
	AccentText   string `json:"accentText"`
	Success      string `json:"success"`
	Warning      string `json:"warning"`
	Danger       string `json:"danger"`
	Info         string `json:"info"`
	Focus        string `json:"focus"`
}

// roleFields lists every role in validation order — a missing role is a
// schema violation, never a silent black.
var roleFields = []string{
	"Background", "Surface", "Elevated", "Control", "ControlHover",
	"Border", "Text", "TextSoft", "Muted", "Accent", "AccentHover",
	"AccentDim", "AccentText", "Success", "Warning", "Danger", "Info",
	"Focus",
}

func (c ColorRoles) value(field string) string {
	switch field {
	case "Background":
		return c.Background
	case "Surface":
		return c.Surface
	case "Elevated":
		return c.Elevated
	case "Control":
		return c.Control
	case "ControlHover":
		return c.ControlHover
	case "Border":
		return c.Border
	case "Text":
		return c.Text
	case "TextSoft":
		return c.TextSoft
	case "Muted":
		return c.Muted
	case "Accent":
		return c.Accent
	case "AccentHover":
		return c.AccentHover
	case "AccentDim":
		return c.AccentDim
	case "AccentText":
		return c.AccentText
	case "Success":
		return c.Success
	case "Warning":
		return c.Warning
	case "Danger":
		return c.Danger
	case "Info":
		return c.Info
	case "Focus":
		return c.Focus
	}
	return ""
}

// Theme is one named token set. Variant is "dark" or "light" — the Fyne
// theme variant and the WebView CSS data attribute derive from it.
type Theme struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Variant     string     `json:"variant"` // dark | light
	Description string     `json:"description"`
	Colors      ColorRoles `json:"colors"`
}

// Contrast minimums the catalog is validated against (WCAG 2.x relative
// luminance ratios): body text and soft text on the surface, and the focus
// indicator against the surface (non-text UI contrast).
const (
	MinTextContrast     = 4.5
	MinTextSoftContrast = 4.5
	MinFocusContrast    = 3.0
	// MinHighContrastText is the enhanced floor for HighContrastThemeID.
	MinHighContrastText = 7.0
	// HighContrastThemeID is the one theme with the enhanced floor.
	HighContrastThemeID = "high-contrast"
)

// The six supported themes (UX-01). Hellish is the premium default;
// Obsidian carries the pre-existing neutral-dark identity verbatim.
var catalog = []Theme{
	{
		ID: "hellish", Name: "SHEYTAN Hellish", Variant: "dark",
		Description: "Charcoal surfaces with the red signature accent — the premium default identity.",
		Colors: ColorRoles{
			Background: "#0d0d0f", Surface: "#151518", Elevated: "#1d1d21",
			Control: "#2b2b30", ControlHover: "#34343a", Border: "#46464e",
			Text: "#ededf0", TextSoft: "#c2c2c9", Muted: "#8b8b95",
			Accent: "#e0483c", AccentHover: "#ef5f52", AccentDim: "#38221f",
			AccentText: "#ffb4ab", Success: "#3ecf8e", Warning: "#ffb454",
			Danger: "#ff5d5d", Info: "#4cc2ff", Focus: "#ff8a7a",
		},
	},
	{
		ID: "obsidian", Name: "Obsidian", Variant: "dark",
		Description: "Neutral dark — the neutral-surface dark identity.",
		Colors: ColorRoles{
			Background: "#121214", Surface: "#17171a", Elevated: "#1f1f23",
			Control: "#2c2c31", ControlHover: "#33333a", Border: "#3a3a42",
			Text: "#e8e8ea", TextSoft: "#b9b9c0", Muted: "#8a8a94",
			Accent: "#7c6cff", AccentHover: "#8d7fff", AccentDim: "#2a2440",
			AccentText: "#c9c2ff", Success: "#3ecf8e", Warning: "#ffb454",
			Danger: "#ff5d5d", Info: "#4cc2ff", Focus: "#9d8fff",
		},
	},
	{
		ID: "graphite", Name: "Graphite", Variant: "dark",
		Description: "Low-contrast neutral dark that stays readable (soft text keeps WCAG AA).",
		Colors: ColorRoles{
			Background: "#1a1a1d", Surface: "#202024", Elevated: "#26262b",
			Control: "#2f2f35", ControlHover: "#37373e", Border: "#44444c",
			Text: "#d9d9de", TextSoft: "#ababb3", Muted: "#80808a",
			Accent: "#9aa7bd", AccentHover: "#a9b6cc", AccentDim: "#2b3038",
			AccentText: "#c9d4e4", Success: "#58c68f", Warning: "#d9a441",
			Danger: "#e06c6c", Info: "#5aa9d6", Focus: "#b8c6dd",
		},
	},
	{
		ID: "light", Name: "Light", Variant: "light",
		Description: "High-legibility light surfaces.",
		Colors: ColorRoles{
			Background: "#f5f5f7", Surface: "#ffffff", Elevated: "#ebebef",
			Control: "#e3e3e9", ControlHover: "#d9d9e1", Border: "#c6c6d0",
			Text: "#1b1b1f", TextSoft: "#46464e", Muted: "#75757f",
			Accent: "#5d4ed6", AccentHover: "#6c5ee0", AccentDim: "#e6e3fb",
			AccentText: "#4638b8", Success: "#1e9e6a", Warning: "#a8680f",
			Danger: "#d5484c", Info: "#177cc0", Focus: "#5d4ed6",
		},
	},
	{
		ID: "high-contrast", Name: "High Contrast", Variant: "dark",
		Description: "Accessibility theme: white on true black with a high-visibility accent (WCAG AAA text floor).",
		Colors: ColorRoles{
			Background: "#000000", Surface: "#000000", Elevated: "#111111",
			Control: "#1c1c1c", ControlHover: "#262626", Border: "#f2f2f2",
			Text: "#ffffff", TextSoft: "#ededed", Muted: "#d0d0d0",
			Accent: "#ffd23f", AccentHover: "#ffdd66", AccentDim: "#3a3009",
			AccentText: "#ffe28a", Success: "#4ade80", Warning: "#fbbf24",
			Danger: "#ff7b7b", Info: "#7dd3fc", Focus: "#ffff00",
		},
	},
	{
		ID: "oled-black", Name: "OLED Black", Variant: "dark",
		Description: "True black for OLED power and contrast use; the red signature at full depth.",
		Colors: ColorRoles{
			Background: "#000000", Surface: "#000000", Elevated: "#0d0d10",
			Control: "#191920", ControlHover: "#222229", Border: "#303038",
			Text: "#f4f4f6", TextSoft: "#c6c6cd", Muted: "#8f8f99",
			Accent: "#ff6b5e", AccentHover: "#ff8276", AccentDim: "#2e1416",
			AccentText: "#ffc2c2", Success: "#3ecf8e", Warning: "#ffb454",
			Danger: "#ff4757", Info: "#4cc2ff", Focus: "#ff9b9b",
		},
	},
}

// DefaultID is the premium default theme (Hellish). A fresh profile starts
// here; a saved preference always wins over the default.
const DefaultID = "hellish"

// Catalog returns the six supported themes in display order. The returned
// slice is a copy — callers cannot mutate the registry.
func Catalog() []Theme {
	out := make([]Theme, len(catalog))
	copy(out, catalog)
	return out
}

// IDs returns the catalog IDs in display order.
func IDs() []string {
	out := make([]string, 0, len(catalog))
	for _, t := range catalog {
		out = append(out, t.ID)
	}
	return out
}

// ByID resolves a theme by its catalog ID.
func ByID(id string) (Theme, bool) {
	for _, t := range catalog {
		if t.ID == id {
			return t, true
		}
	}
	return Theme{}, false
}

// Default returns the premium default theme.
func Default() Theme {
	t, _ := ByID(DefaultID)
	return t
}

// Validate checks one theme against the token contract: every role present
// and a parseable hex color, variant spelled correctly, and the contrast
// floors met (text and soft text ≥ 4.5 on the surface, focus ≥ 3.0; the
// high-contrast theme must clear 7.0).
func Validate(t Theme) error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("theme %q: id and name are required", t.ID)
	}
	if t.Variant != "dark" && t.Variant != "light" {
		return fmt.Errorf("theme %q: variant %q must be dark or light", t.ID, t.Variant)
	}
	for _, field := range roleFields {
		v := t.Colors.value(field)
		if v == "" {
			return fmt.Errorf("theme %q: role %s is missing", t.ID, field)
		}
		if _, err := ParseHex(v); err != nil {
			return fmt.Errorf("theme %q: role %s: %w", t.ID, field, err)
		}
	}
	textRatio, err := ContrastRatio(t.Colors.Text, t.Colors.Surface)
	if err != nil {
		return fmt.Errorf("theme %q: %w", t.ID, err)
	}
	softRatio, err := ContrastRatio(t.Colors.TextSoft, t.Colors.Surface)
	if err != nil {
		return fmt.Errorf("theme %q: %w", t.ID, err)
	}
	focusRatio, err := ContrastRatio(t.Colors.Focus, t.Colors.Surface)
	if err != nil {
		return fmt.Errorf("theme %q: %w", t.ID, err)
	}
	minText := MinTextContrast
	if t.ID == HighContrastThemeID {
		minText = MinHighContrastText
	}
	if textRatio < minText {
		return fmt.Errorf("theme %q: text contrast %.2f on surface is below the %.1f floor", t.ID, textRatio, minText)
	}
	if softRatio < MinTextSoftContrast {
		return fmt.Errorf("theme %q: soft-text contrast %.2f on surface is below the %.1f floor", t.ID, softRatio, MinTextSoftContrast)
	}
	if focusRatio < MinFocusContrast {
		return fmt.Errorf("theme %q: focus contrast %.2f on surface is below the %.1f floor", t.ID, focusRatio, MinFocusContrast)
	}
	return nil
}

// ValidateCatalog validates every theme and rejects duplicate IDs.
func ValidateCatalog() error {
	seen := map[string]bool{}
	for _, t := range catalog {
		if seen[t.ID] {
			return fmt.Errorf("duplicate theme id %q", t.ID)
		}
		seen[t.ID] = true
		if err := Validate(t); err != nil {
			return err
		}
	}
	return nil
}

// tokenPayload is the typed serialized representation (the form WebView
// surfaces consume): schema version, the theme, and the shared non-color
// tokens so one document carries the full cross-engine contract.
type tokenPayload struct {
	Schema  int          `json:"schema"`
	Theme   Theme        `json:"theme"`
	Spacing SpacingToken `json:"spacing"`
	Type    TypeToken    `json:"type"`
	Radius  RadiusToken  `json:"radius"`
	Touch   TouchToken   `json:"touch"`
}

// JSON returns the serialized representation. It marshals only after
// validation — a broken theme can never serialize as if it were
// renderable.
func (t Theme) JSON() ([]byte, error) {
	if err := Validate(t); err != nil {
		return nil, err
	}
	payload := tokenPayload{
		Schema:  SchemaVersion,
		Theme:   t,
		Spacing: SpacingScale,
		Type:    TypeScale,
		Radius:  RadiusScale,
		Touch:   TouchTargets,
	}
	return json.MarshalIndent(payload, "", "  ")
}

// ParseHex parses "#rrggbb" (the only spelling tokens may use) into a
// color.Color. Loud failure, never a silent fallback color.
func ParseHex(s string) (color.Color, error) {
	var r, g, b int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return nil, fmt.Errorf("invalid hex color %q (want #rrggbb)", s)
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}, nil
}

// MustHex parses a token color and returns a loud magenta on failure —
// renderers use it where the catalog validation already ran.
func MustHex(s string) color.Color {
	c, err := ParseHex(s)
	if err != nil {
		return color.NRGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}
	}
	return c
}

// SpacingToken is the shared spacing scale (logical DIPs).
type SpacingToken struct {
	XS, S, M, L, XL, XXL float32
}

// SpacingScale — the ONE spacing scale (values carried over from the
// Fyne-era contract so the visual rhythm is unchanged).
var SpacingScale = SpacingToken{XS: 4, S: 8, M: 12, L: 16, XL: 24, XXL: 32}

// TypeToken is the shared typography scale (logical DIPs).
type TypeToken struct {
	Title, Section, Body, Mono, Status float32
}

// TypeScale — the ONE text scale, rooted at Body = 14.
var TypeScale = TypeToken{Title: 20, Section: 16, Body: 14, Mono: 13, Status: 12}

// RadiusToken is the shared corner-radius scale (logical DIPs).
type RadiusToken struct {
	Control, Card, Dialog float32
}

// RadiusScale — painted-surface radii.
var RadiusScale = RadiusToken{Control: 8, Card: 10, Dialog: 14}

// TouchToken pins the touch-target contract (UX-06): 44 DIP minimum,
// 48 preferred for primary actions, 8–12 DIP of practical spacing.
type TouchToken struct {
	TargetMinDIP       float32
	TargetPreferredDIP float32
	SpacingMinDIP      float32
	SpacingMaxDIP      float32
}

// TouchTargets — the cross-engine touch contract.
var TouchTargets = TouchToken{TargetMinDIP: 44, TargetPreferredDIP: 48, SpacingMinDIP: 8, SpacingMaxDIP: 12}

// Band is one responsive width band (logical DIPs). The Fyne shell's
// four-class policy maps onto these bands (see
// docs/ui/TOUCH-RESPONSIVE-MATRIX.md); the ultrawide band is the recorded
// contract for future engines.
type Band struct {
	Name     string
	MinWidth float32 // inclusive; the first band starts at 0
}

// Bands returns the responsive width bands in ascending order.
func Bands() []Band {
	return []Band{
		{Name: "compact", MinWidth: 0},
		{Name: "tablet", MinWidth: 600},
		{Name: "desktop", MinWidth: 900},
		{Name: "wide", MinWidth: 1400},
		{Name: "ultrawide", MinWidth: 2200},
	}
}

// luminance computes the WCAG 2.x relative luminance of a hex color.
func luminance(hex string) (float64, error) {
	c, err := ParseHex(hex)
	if err != nil {
		return 0, err
	}
	nrgba, ok := c.(color.NRGBA)
	if !ok {
		return 0, fmt.Errorf("color %q did not parse to NRGBA", hex)
	}
	chanLum := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return srgbExp(s)
	}
	r := chanLum(nrgba.R)
	g := chanLum(nrgba.G)
	b := chanLum(nrgba.B)
	return 0.2126*r + 0.7152*g + 0.0722*b, nil
}

// srgbExp computes ((s+0.055)/1.055)^2.4 with a bounded Newton iteration
// for the 1/5 power — deterministic across platforms (no libm drift),
// precision far beyond the contrast floors.
func srgbExp(s float64) float64 {
	base := (s + 0.055) / 1.055
	// base^2.4 = base^2 * (base^2)^(1/5); Newton on x^5 = base^2.
	sq := base * base
	r := sq // initial guess for sq^(1/5)
	for i := 0; i < 64; i++ {
		r4 := r * r * r * r
		next := (4*r*r4 + sq) / (5 * r4)
		if next == r {
			break
		}
		r = next
	}
	return sq * r
}

// ContrastRatio returns the WCAG contrast ratio between two hex colors
// (1.0 identical … 21.0 black/white).
func ContrastRatio(aHex, bHex string) (float64, error) {
	la, err := luminance(aHex)
	if err != nil {
		return 0, err
	}
	lb, err := luminance(bHex)
	if err != nil {
		return 0, err
	}
	hi, lo := la, lb
	if lo > hi {
		hi, lo = lo, hi
	}
	return (hi + 0.05) / (lo + 0.05), nil
}
