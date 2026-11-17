package theme

import (
	"encoding/json"
	"testing"
)

// TestCatalogValidates: all six themes satisfy the token contract — every
// role present and parseable, and the WCAG floors met. This is the
// "readable contrast and focus indicators" acceptance (10.5) as a
// permanent gate: a future palette regression cannot ship.
func TestCatalogValidates(t *testing.T) {
	if err := ValidateCatalog(); err != nil {
		t.Fatalf("theme catalog validation failed: %v", err)
	}
}

// TestCatalogContract: exactly the six required themes, in order, with the
// premium default (UX-01).
func TestCatalogContract(t *testing.T) {
	want := []string{"hellish", "obsidian", "graphite", "light", "high-contrast", "oled-black"}
	got := IDs()
	if len(got) != len(want) {
		t.Fatalf("catalog has %d themes, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("catalog order drifted: got %v, want %v", got, want)
		}
	}
	if DefaultID != "hellish" {
		t.Fatalf("default theme is %q, want hellish (the premium default)", DefaultID)
	}
	if _, ok := ByID(DefaultID); !ok {
		t.Fatal("the default theme id does not resolve in the catalog")
	}
	if _, ok := ByID("no-such-theme"); ok {
		t.Fatal("an unknown theme id resolved — the catalog lookup is broken")
	}
}

// TestHighContrastEnhancedFloor: the accessibility theme clears the AAA
// text floor, not just AA (10.5: readable contrast for every theme).
func TestHighContrastEnhancedFloor(t *testing.T) {
	hc, ok := ByID(HighContrastThemeID)
	if !ok {
		t.Fatal("the high-contrast theme is missing from the catalog")
	}
	ratio, err := ContrastRatio(hc.Colors.Text, hc.Colors.Surface)
	if err != nil {
		t.Fatalf("contrast computation failed: %v", err)
	}
	if ratio < MinHighContrastText {
		t.Fatalf("high-contrast text ratio %.2f is below the %.1f AAA floor", ratio, MinHighContrastText)
	}
}

// TestLightThemeIsLight: the light variant is marked light so Fyne's theme
// variant and the WebView data attribute derive correctly.
func TestLightThemeIsLight(t *testing.T) {
	light, ok := ByID("light")
	if !ok {
		t.Fatal("the light theme is missing from the catalog")
	}
	if light.Variant != "light" {
		t.Fatalf("light theme variant is %q, want light", light.Variant)
	}
	for _, id := range IDs() {
		if id == "light" {
			continue
		}
		th, _ := ByID(id)
		if th.Variant != "dark" {
			t.Errorf("theme %q has variant %q, want dark", id, th.Variant)
		}
	}
}

// TestValidateRejectsBrokenThemes: the validator fails closed on missing
// roles, bad hex, bad variants and failing contrast — a broken theme can
// never enter the catalog or serialize.
func TestValidateRejectsBrokenThemes(t *testing.T) {
	base, _ := ByID("obsidian")

	missing := base
	missing.Colors.Focus = ""
	if err := Validate(missing); err == nil {
		t.Error("a theme with a missing Focus role must be rejected")
	}

	badHex := base
	badHex.Colors.Accent = "#12g45z"
	if err := Validate(badHex); err == nil {
		t.Error("a theme with an unparseable hex role must be rejected")
	}

	badVariant := base
	badVariant.Variant = "sepia"
	if err := Validate(badVariant); err == nil {
		t.Error("a theme with an unknown variant must be rejected")
	}

	lowContrast := base
	// #6e6e76 on the obsidian surface measures ≈3.6:1 — genuinely below
	// the 4.5 AA floor (the muted #8a8a94 would actually PASS at ≈5.2:1).
	lowContrast.Colors.Text = "#6e6e76"
	if err := Validate(lowContrast); err == nil {
		t.Error("a theme whose text contrast is below the floor must be rejected")
	}
}

// TestJSONRoundTrip: the typed serialized representation carries the
// schema version, the full role set and the shared tokens; it decodes back
// to the same roles (the WebView-surface contract).
func TestJSONRoundTrip(t *testing.T) {
	for _, id := range IDs() {
		th, _ := ByID(id)
		raw, err := th.JSON()
		if err != nil {
			t.Fatalf("theme %q failed to serialize: %v", id, err)
		}
		var payload struct {
			Schema int        `json:"schema"`
			Theme  Theme      `json:"theme"`
			Touch  TouchToken `json:"touch"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("theme %q payload does not decode: %v", id, err)
		}
		if payload.Schema != SchemaVersion {
			t.Errorf("theme %q serialized schema %d, want %d", id, payload.Schema, SchemaVersion)
		}
		if payload.Theme.ID != id || payload.Theme.Colors != th.Colors {
			t.Errorf("theme %q round-trip drifted", id)
		}
		if payload.Touch.TargetMinDIP != 44 || payload.Touch.TargetPreferredDIP != 48 {
			t.Errorf("theme %q serialized the wrong touch contract: %+v", id, payload.Touch)
		}
	}
}

// TestBrokenThemeJSONFails: serialization refuses a theme that fails
// validation (never ship a renderable-looking broken payload).
func TestBrokenThemeJSONFails(t *testing.T) {
	broken := Theme{ID: "broken", Name: "Broken", Variant: "dark"}
	if _, err := broken.JSON(); err == nil {
		t.Fatal("a theme with no colors must fail serialization")
	}
}

// TestContrastRatioKnownValues: the WCAG math against hand-computed
// anchors (pure black on pure white is 21:1; identical colors are 1:1).
func TestContrastRatioKnownValues(t *testing.T) {
	r, err := ContrastRatio("#000000", "#ffffff")
	if err != nil {
		t.Fatalf("contrast computation failed: %v", err)
	}
	if r < 20.9 || r > 21.01 {
		t.Fatalf("black/white ratio %.3f, want 21", r)
	}
	r, err = ContrastRatio("#ffffff", "#ffffff")
	if err != nil {
		t.Fatalf("contrast computation failed: %v", err)
	}
	if r < 0.99 || r > 1.01 {
		t.Fatalf("identical-color ratio %.3f, want 1", r)
	}
	if _, err := ContrastRatio("#zzzzzz", "#ffffff"); err == nil {
		t.Fatal("an unparseable color must error, not silently produce a ratio")
	}
}

// TestCatalogCopyIsDefensive: mutating the returned catalog must not touch
// the registry.
func TestCatalogCopyIsDefensive(t *testing.T) {
	c := Catalog()
	if len(c) == 0 {
		t.Fatal("catalog is empty")
	}
	c[0].ID = "mutated"
	if _, ok := ByID("mutated"); ok {
		t.Fatal("mutating the returned catalog changed the registry")
	}
}
