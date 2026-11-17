//go:build !headless

// theme_service.go — the live theme switcher (v1.11.1 UX-01).
//
// Contract (the work order's §4.2 acceptance, pinned by tests):
//   - selecting a theme APPLIES it live: fyne App.Settings().SetTheme
//     repaints every widget in place — the core is not recreated, the
//     shell/navigation state survives, no session data is touched and no
//     provider/model setting changes (TestThemeSwitchKeepsNavigation);
//   - Save is the ONLY path that persists (Fyne Preferences under the
//     app id — presentation state, never core config);
//   - Cancel restores the previously SAVED theme and drops the preview;
//   - a persisted unknown id falls back to the premium default.
//
// The service is the ONE write path to Settings().SetTheme in the product;
// nothing else in ui/fyne switches themes underneath the user.
package fyneui

import (
	"errors"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"

	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// errUnknownTheme is the typed Apply rejection for a non-catalog id.
var errUnknownTheme = func(id string) error {
	return fmt.Errorf("theme %q is not in the catalog: %w", id, errors.New("unknown theme id"))
}

// prefKeyTheme is the persisted preference key (scoped to the app id by
// Fyne's preference store; in-memory under the test harness).
const prefKeyTheme = "ui.theme"

// themeService owns the active/saved theme pair.
type themeService struct {
	a fyne.App

	mu     sync.Mutex
	active string // the currently RENDERED catalog id (preview or saved)
	saved  string // the persisted catalog id
}

// newThemeService builds the service and applies the persisted (or
// default) theme immediately, so boot and every later switch share ONE
// write path.
func newThemeService(a fyne.App) *themeService {
	s := &themeService{a: a}
	s.saved = loadSavedThemeID(a)
	s.active = s.saved
	a.Settings().SetTheme(NewThemeFor(s.active))
	return s
}

// loadSavedThemeID resolves the persisted id with the fail-safe fallback:
// an unknown persisted value (hand-edited store, catalog rename) renders
// the premium default rather than a broken theme.
func loadSavedThemeID(a fyne.App) string {
	id := a.Preferences().StringWithFallback(prefKeyTheme, themetokens.DefaultID)
	if _, ok := themetokens.ByID(id); !ok {
		return themetokens.DefaultID
	}
	return id
}

// Active returns the currently rendered catalog id (the preview when one
// is open, else the saved id).
func (s *themeService) Active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Saved returns the persisted catalog id.
func (s *themeService) Saved() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved
}

// IsPreviewing reports whether an unsaved preview is rendered.
func (s *themeService) IsPreviewing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != s.saved
}

// Apply switches the rendered theme LIVE. The catalog id is validated
// first — an unknown id is a typed error, never a silent default.
// Nothing is persisted here (Save is the only persistence path).
func (s *themeService) Apply(id string) error {
	t, ok := themetokens.ByID(id)
	if !ok {
		return errUnknownTheme(id)
	}
	s.mu.Lock()
	s.active = id
	s.mu.Unlock()
	s.a.Settings().SetTheme(NewThemeFromTheme(t))
	return nil
}

// Cancel restores the previously SAVED theme and drops any preview.
func (s *themeService) Cancel() {
	s.mu.Lock()
	id := s.saved
	s.active = id
	s.mu.Unlock()
	if t, ok := themetokens.ByID(id); ok {
		s.a.Settings().SetTheme(NewThemeFromTheme(t))
	}
}

// Save persists the currently active (previewed) theme as the saved one.
func (s *themeService) Save() {
	s.mu.Lock()
	s.saved = s.active
	id := s.saved
	s.mu.Unlock()
	s.a.Preferences().SetString(prefKeyTheme, id)
}

// ResetForTest restores a known-clean state (test seam only).
func (s *themeService) ResetForTest(id string) {
	s.mu.Lock()
	s.saved = id
	s.active = id
	s.mu.Unlock()
	s.a.Preferences().SetString(prefKeyTheme, id)
}
