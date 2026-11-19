//go:build !headless

// theme_service.go — the live theme switcher (v1.11.1 UX-01 contract,
// ported from ui/fyne/theme_service.go).
//
// Contract (pinned by tests in both hosts):
//   - selecting a theme APPLIES it live: gui.SetTheme republishes the app
//     default and every window re-themes on its next frame — the core is
//     not recreated, the shell/navigation state survives, no session data
//     is touched and no provider/model setting changes;
//   - Save is the ONLY path that persists (a small preference file under
//     the app data dir — presentation state, never core config);
//   - Cancel restores the previously SAVED theme and drops the preview;
//   - a persisted unknown id falls back to the premium default.
//
// The service is the ONE write path to gui.SetTheme in the product;
// nothing else in ui/gogui switches themes underneath the user.
// NOTE: gui.SetTheme is FRAME-THREAD ONLY (v0.97.0 doc) — callers inside
// event handlers may call directly; the Engines view does. Background
// callers must ride uiDo.
package goguiui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// errUnknownTheme is the typed Apply rejection for a non-catalog id.
func errUnknownTheme(id string) error {
	return fmt.Errorf("theme %q is not in the catalog: %w", id, errors.New("unknown theme id"))
}

// prefFile is the persisted preference file (scoped under the app data
// dir; a tiny key=value store is honest for one preference).
const prefFile = "ui/gogui-prefs.txt"

// prefKeyTheme is the persisted preference key.
const prefKeyTheme = "ui.theme"

// themeService owns the active/saved theme pair.
type themeService struct {
	mu     sync.Mutex
	active string // the currently RENDERED catalog id (preview or saved)
	saved  string // the persisted catalog id
	store  prefStore
}

// prefStore is a one-key preference file (presentation state only).
type prefStore struct{ path string }

func defaultPrefStore() prefStore {
	root, _, _ := themetokensResolveRoot()
	if root == "" {
		return prefStore{}
	}
	return prefStore{path: filepath.Join(root, prefFile)}
}

// themetokensResolveRoot avoids importing config here twice; config
// already resolves the canonical data root.
func themetokensResolveRoot() (string, bool, string) {
	return configResolveRoot()
}

// Get reads the key ("" when unset or the store is disabled).
func (p prefStore) Get(key string) string {
	if p.path == "" {
		return ""
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return ""
	}
	for _, line := range splitLines(string(raw)) {
		if k, v, ok := cutKV(line); ok && k == key {
			return v
		}
	}
	return ""
}

// Set writes the key atomically.
func (p prefStore) Set(key, value string) error {
	if p.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	content := key + "=" + value + "\n"
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func cutKV(line string) (k, v string, ok bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' {
			return line[:i], line[i+1:], true
		}
	}
	return "", "", false
}

// newThemeService builds the service and applies the persisted (or
// default) theme immediately, so boot and every later switch share ONE
// write path.
func newThemeService() *themeService {
	s := &themeService{store: defaultPrefStore()}
	s.saved = s.loadSavedThemeID()
	s.active = s.saved
	guiApplyTheme(s.active)
	return s
}

// loadSavedThemeID resolves the persisted id with the fail-safe fallback:
// an unknown persisted value (hand-edited file, catalog rename) renders
// the premium default rather than a broken theme.
func (s *themeService) loadSavedThemeID() string {
	id := s.store.Get(prefKeyTheme)
	if id == "" {
		return themetokens.DefaultID
	}
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
	if _, ok := themetokens.ByID(id); !ok {
		return errUnknownTheme(id)
	}
	s.mu.Lock()
	s.active = id
	s.mu.Unlock()
	guiApplyTheme(id)
	return nil
}

// Cancel restores the previously SAVED theme and drops any preview.
func (s *themeService) Cancel() {
	s.mu.Lock()
	id := s.saved
	s.active = id
	s.mu.Unlock()
	guiApplyTheme(id)
}

// Save persists the currently active (previewed) theme as the saved one.
func (s *themeService) Save() {
	s.mu.Lock()
	s.saved = s.active
	id := s.saved
	s.mu.Unlock()
	_ = s.store.Set(prefKeyTheme, id)
}

// ResetForTest restores a known-clean state (test seam only).
func (s *themeService) ResetForTest(id string) {
	s.mu.Lock()
	s.saved = id
	s.active = id
	s.mu.Unlock()
	_ = s.store.Set(prefKeyTheme, id)
}

// guiApplyTheme applies the catalog theme on the UI thread. Callers on
// the frame thread (button handlers) run directly; anything else must
// already be inside uiDo.
func guiApplyTheme(id string) {
	ApplyTheme(id)
}
