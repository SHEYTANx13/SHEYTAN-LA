//go:build !headless

// theme_test.go — the token-application contract: every catalog theme
// projects into a valid go-gui theme with the catalog's background,
// panel and accent roles; the premium default is "hellish"; the theme
// service honors Apply (live) / Save (only persistence) / Cancel
// (restore) with the unknown-id typed rejection.
package goguiui

import (
	"testing"

	"github.com/go-gui-org/go-gui/gui"

	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// TestThemeMappingCarriesCatalogRoles renders every catalog theme into a
// ThemeMaker theme and pins the three roles the whole UI leans on:
// background, panel, accent.
func TestThemeMappingCarriesCatalogRoles(t *testing.T) {
	for _, tok := range themetokens.Catalog() {
		th := ThemeFromCatalog(tok)
		wantBG := gogoColor(tok.Colors.Background)
		wantPanel := gogoColor(tok.Colors.Surface)
		wantAccent := gogoColor(tok.Colors.Accent)
		if th.ColorBackground != wantBG {
			t.Fatalf("theme %s: background = %v, want %v", tok.ID, th.ColorBackground, wantBG)
		}
		if th.ColorPanel != wantPanel {
			t.Fatalf("theme %s: panel = %v, want %v", tok.ID, th.ColorPanel, wantPanel)
		}
		if th.ColorAccent != wantAccent {
			t.Fatalf("theme %s: accent = %v, want %v", tok.ID, th.ColorAccent, wantAccent)
		}
		if th.TextStyleDef.Size != themetokens.TypeScale.Body {
			t.Fatalf("theme %s: body size = %v, want the shared TypeScale body", tok.ID, th.TextStyleDef.Size)
		}
	}
}

// TestThemeDefaultIsHellish pins the premium default (the infernal
// red/black system): the default catalog id maps to the charcoal
// background + red accent.
func TestThemeDefaultIsHellish(t *testing.T) {
	if themetokens.DefaultID != "hellish" {
		t.Fatalf("catalog default = %q, want hellish", themetokens.DefaultID)
	}
	tok, ok := themetokens.ByID(themetokens.DefaultID)
	if !ok {
		t.Fatal("hellish missing from the catalog")
	}
	th := ThemeFor(themetokens.DefaultID)
	if th.ColorAccent != gogoColor(tok.Colors.Accent) {
		t.Fatalf("default theme accent = %v, want the hellish red %v", th.ColorAccent, tok.Colors.Accent)
	}
}

// TestThemeServiceApplySaveCancel drives the ONE write path: Apply
// switches live (unknown ids rejected typed), Save is the only
// persistence, Cancel restores the saved id.
func TestThemeServiceApplySaveCancel(t *testing.T) {
	s := &themeService{store: prefStore{path: tempPrefPath(t)}}
	s.saved = themetokens.DefaultID
	s.active = s.saved

	if err := s.Apply("obsidian"); err != nil {
		t.Fatalf("Apply obsidian: %v", err)
	}
	if !s.IsPreviewing() {
		t.Fatal("Apply must open a preview (active != saved)")
	}
	if s.Saved() != themetokens.DefaultID {
		t.Fatalf("Apply must not persist: saved = %q", s.Saved())
	}

	s.Save()
	if s.IsPreviewing() {
		t.Fatal("Save closes the preview")
	}
	if s.Saved() != "obsidian" {
		t.Fatalf("Save persisted %q, want obsidian", s.Saved())
	}

	if err := s.Apply("light"); err != nil {
		t.Fatalf("Apply light: %v", err)
	}
	s.Cancel()
	if s.Active() != "obsidian" {
		t.Fatalf("Cancel restored %q, want the saved obsidian", s.Active())
	}

	if err := s.Apply("not-a-theme"); err == nil {
		t.Fatal("Apply of an unknown id must return the typed error")
	}
}

// TestThemePersistedUnknownIDFallsSafe pins the fail-safe: an unknown
// persisted value renders the premium default, never a broken theme.
func TestThemePersistedUnknownIDFallsSafe(t *testing.T) {
	path := tempPrefPath(t)
	s := &themeService{store: prefStore{path: path}}
	if err := s.store.Set(prefKeyTheme, "hand-edited-bogus"); err != nil {
		t.Fatal(err)
	}
	if got := s.loadSavedThemeID(); got != themetokens.DefaultID {
		t.Fatalf("unknown persisted theme resolved to %q, want the default", got)
	}
}

// TestThemeSwitchKeepsNavigation pins the UX-01 acceptance on the go-gui
// host: applying a theme does not touch navigation state.
func TestThemeSwitchKeepsNavigation(t *testing.T) {
	_, ap, s, w := newTestShell(t, 1280, 720)
	s.show("system")
	before := s.activeView()
	if err := ap.Themes().Apply("graphite"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	settle(w)
	if s.activeView() != before {
		t.Fatalf("theme switch moved navigation: %q -> %q", before, s.activeView())
	}
	layout := w.TestRender(nil)
	if _, ok := layout.FindByID(absID("view-system")); !ok {
		t.Fatal("themed frame lost the active view content")
	}
	_ = gui.CurrentTheme
}

// tempPrefPath is the per-test preference file path (never the real one).
func tempPrefPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/prefs.txt"
}
