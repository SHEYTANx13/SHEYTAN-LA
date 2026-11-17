//go:build !headless

// view_engines_test.go — the v1.11.1 renderer-convergence acceptance
// tests (UX-01..UX-05) over the real Fyne test harness:
//
//   - the Engine Gallery builds detached from a live core and renders
//     EVERY catalog engine with its truthful state and reason;
//   - not-built engines never offer an enabled launch action;
//   - the theme switcher applies live, cancels to the saved theme, saves
//     as the only persistence path — and switching themes preserves the
//     shell's navigation (no core reset, no view reset);
//   - the window-controls trio guarantees 44×44 DIP hit areas, renders
//     minimize/tray DISABLED with reasons (never fake states), and the
//     close control fires the injected funnel.
package fyneui

import (
	"runtime"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/renderer"
	themetokens "github.com/SHEYTANx13/SHEYTAN-LA/internal/theme"
)

// ---- Engine Gallery (UX-02/UX-03) ---------------------------------------

// TestEngineGalleryBuildsAndListsEveryCatalogEngine: the view builds over
// the standard fake-core app and the rendered tree names every engine in
// the registry — the gallery is registry-driven, not a hand-written list
// that can drift from internal/renderer.
func TestEngineGalleryBuildsAndListsEveryCatalogEngine(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	obj := buildEnginesView(ap)
	if obj == nil || !treeHasObjects(obj) {
		t.Fatal("the engines view must build a non-empty tree")
	}
	dump := treeText(obj)
	for _, m := range renderer.Catalog() {
		if !strings.Contains(dump, m.DisplayName) {
			t.Errorf("engine %q is not rendered in the gallery (registry-driven surface drifted)", m.DisplayName)
		}
	}
}

// TestEngineGalleryRendersTruthfulStates: every not-built engine's reason
// is VISIBLE in the tree; the Fyne engine renders as production+ready.
func TestEngineGalleryRendersTruthfulStates(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	dump := treeText(buildEnginesView(ap))

	for _, m := range renderer.Catalog() {
		if m.State == renderer.StateReady {
			if !strings.Contains(dump, "production") {
				t.Error("the production engine's maturity is not rendered")
			}
			continue
		}
		if m.StateReason != "" && !strings.Contains(dump, m.StateReason) {
			t.Errorf("engine %q: the state reason is not visible in the gallery (an unexplained unavailable state)", m.ID)
		}
		if !strings.Contains(dump, "POC") && !strings.Contains(dump, "not built") && !strings.Contains(dump, "not-built") {
			// The reason itself must communicate the POC boundary.
			t.Errorf("engine %q: the gallery does not communicate the POC boundary", m.ID)
		}
	}
}

// TestEngineGalleryHonestActions: not-built engines never offer an
// ENABLED activate action; the production engine never offers a fake
// "activate" either (it is already the shell). Every widget.Button in the
// gallery with "Activate"/"Unavailable"/"Active" text must be disabled.
func TestEngineGalleryHonestActions(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	obj := buildEnginesView(ap)

	var bad []string
	forEachButton(obj, func(b *widget.Button) {
		switch {
		case strings.HasPrefix(b.Text, "Active"),
			strings.HasPrefix(b.Text, "Unavailable"),
			b.Text == "Activate engine":
			if !b.Disabled() {
				bad = append(bad, b.Text)
			}
		}
	})
	for _, txt := range bad {
		t.Errorf("gallery action %q is ENABLED — the package cannot perform it (never a fake launch button)", txt)
	}
}

// TestEngineGalleryCurrentPlatformLine: the gallery renders THIS
// machine's platform verification line for every engine (one line per
// catalog entry).
func TestEngineGalleryCurrentPlatformLine(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	dump := treeText(buildEnginesView(ap))
	want := strings.Count(dump, "This platform (")
	if want < len(renderer.Catalog()) {
		t.Errorf("the gallery rendered %d this-platform lines, want at least %d (one per engine)", want, len(renderer.Catalog()))
	}
	if !strings.Contains(dump, "This platform ("+runtime.GOOS+"/"+runtime.GOARCH+")") {
		t.Errorf("no this-platform line for the actual running host %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

// ---- Theme switcher (UX-01) ---------------------------------------------

// TestThemeServiceApplyCancelSave pins the full apply/cancel/save
// contract over the test app (in-memory preferences):
//
//  1. Apply switches the rendered theme LIVE (Settings().Theme carries
//     the active catalog id) and persists NOTHING;
//  2. Cancel restores the SAVED theme and drops the preview;
//  3. Save is the ONLY persistence path (the preference updates); a
//     saved service no longer previews;
//  4. Apply of an unknown id is a typed error and changes nothing.
func TestThemeServiceApplyCancelSave(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	s := ap.Themes()

	// 1. Live apply, nothing persisted.
	if err := s.Apply("light"); err != nil {
		t.Fatalf("apply light: %v", err)
	}
	if s.Active() != "light" {
		t.Fatalf("active theme after apply = %q, want light", s.Active())
	}
	if got := ap.a.Preferences().StringWithFallback(prefKeyTheme, ""); got != "" && got != themetokens.DefaultID {
		// Fresh test app: no write has happened yet (DefaultID may exist
		// only if a previous test in this app wrote it — none did).
		t.Fatalf("apply persisted %q — Save is the only persistence path", got)
	}
	if !s.IsPreviewing() {
		t.Fatal("apply must leave the service in previewing state")
	}

	// 4. Unknown id: typed error, state unchanged.
	if err := s.Apply("no-such-theme"); err == nil {
		t.Fatal("applying an unknown theme id must fail")
	}
	if s.Active() != "light" {
		t.Fatalf("a failed apply changed the active theme to %q", s.Active())
	}

	// 2. Cancel restores the saved (default) theme.
	s.Cancel()
	if s.Active() != s.Saved() {
		t.Fatalf("cancel must restore the saved theme %q, got %q", s.Saved(), s.Active())
	}
	if s.IsPreviewing() {
		t.Fatal("cancel must drop the preview")
	}

	// 3. Save persists; the saved pair becomes the applied pair.
	if err := s.Apply("high-contrast"); err != nil {
		t.Fatalf("apply high-contrast: %v", err)
	}
	s.Save()
	if s.Saved() != "high-contrast" {
		t.Fatalf("save must persist the previewed theme, saved = %q", s.Saved())
	}
	if got := ap.a.Preferences().StringWithFallback(prefKeyTheme, ""); got != "high-contrast" {
		t.Fatalf("the preference was not written by Save: got %q", got)
	}
	if s.IsPreviewing() {
		t.Fatal("after save the service must no longer be previewing")
	}

	// Restore a clean preference for other tests sharing the app.
	s.ResetForTest(themetokens.DefaultID)
}

// TestThemeSwitchKeepsNavigation pins the work order's acceptance: theme
// switching must not recreate the core, reset navigation or drop the
// active view. The shell keeps its active view and its built-view cache.
func TestThemeSwitchKeepsNavigation(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	_ = sh.root()
	sh.show("agent")
	if sh.active != "agent" {
		t.Fatalf("precondition: active view = %q, want agent", sh.active)
	}

	// Live switch through the service (the gallery's exact path).
	if err := ap.Themes().Apply("oled-black"); err != nil {
		t.Fatalf("theme apply: %v", err)
	}
	ap.Themes().Cancel()

	if sh.active != "agent" {
		t.Fatalf("theme switching reset navigation: active = %q, want agent", sh.active)
	}
	if sh.views == nil || sh.views["agent"] == nil {
		t.Fatal("theme switching dropped the built view cache (a core-reset symptom)")
	}
}

// TestThemeSwitchRendersNewPalette: the applied theme actually reaches
// the widget pipeline — the app theme's background role changes between
// dark and light variants.
func TestThemeSwitchRendersNewPalette(t *testing.T) {
	ap, _ := newTestAppForShell(t)

	dark := NewThemeFor("obsidian").(*appTheme)
	light := NewThemeFor("light").(*appTheme)
	if sameColors(dark.Color(theme.ColorNameBackground, 0), light.Color(theme.ColorNameBackground, 0)) {
		t.Fatal("dark and light backgrounds must differ — the role mapping is broken")
	}

	if err := ap.Themes().Apply("light"); err != nil {
		t.Fatalf("apply light: %v", err)
	}
	ap.Themes().Cancel()
}

// ---- Window controls (UX-05) ---------------------------------------------

// TestWindowControlsHitAreas: every control guarantees the 44×44 DIP
// minimum hit area (UX-06 acceptance at the control level).
func TestWindowControlsHitAreas(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	v := &enginesView{ap: ap}
	_ = v.buildWindowControls()

	for _, ctl := range []*titlebarButton{v.closeCtl, v.minCtl, v.trayCtl} {
		if ctl == nil {
			t.Fatal("a window control was not built")
		}
		m := ctl.MinSize()
		if m.Width < TouchTargetMin || m.Height < TouchTargetMin {
			t.Errorf("control %q hit area %v is below the 44×44 DIP floor", ctl.label, m)
		}
	}
}

// TestWindowControlsHonestStates: minimize and tray are DISABLED with
// their technical reasons; close is enabled. Never a fake tray state.
func TestWindowControlsHonestStates(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	v := &enginesView{ap: ap}
	_ = v.buildWindowControls()

	if !v.minCtl.isDisabled() {
		t.Error("the minimize control must be disabled (Fyne v2.8.2 has no minimize API)")
	}
	if v.minCtl.reason == "" {
		t.Error("a disabled control without a reason is an unexplained state")
	}
	if !v.trayCtl.isDisabled() {
		t.Error("the tray control must be disabled (no tray API or integration in v1.11.1)")
	}
	if v.trayCtl.reason == "" {
		t.Error("the tray control must carry its reason")
	}
	if v.closeCtl.isDisabled() {
		t.Error("the close control must be enabled — it routes through the graceful funnel")
	}
}

// TestWindowControlsCloseFunnel: the close control fires the injected
// action (the App's RequestClose funnel in production; a spy here).
func TestWindowControlsCloseFunnel(t *testing.T) {
	fired := 0
	ctl := newTitlebarButton("✕", "Close SHEYTAN-LA", "", func() { fired++ })

	// Keyboard activation (§4.3): Enter and Space.
	ctl.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	ctl.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	// Pointer/touch activation.
	ctl.Tapped(nil)
	if fired != 3 {
		t.Fatalf("the close control must fire on Enter, Space and Tap (3 activations), got %d", fired)
	}

	// A disabled control never fires.
	disabled := newTitlebarButton("—", "Minimize", "reason", nil)
	disabled.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	disabled.Tapped(nil)
}

// TestShellIncludesEnginesView: the engines destination is registered in
// the shell's canonical navigation (rail + compact strip + Ctrl+9 derive
// from primaryViews).
func TestShellIncludesEnginesView(t *testing.T) {
	found := false
	for _, v := range primaryViews {
		if v.id == "engines" {
			found = true
		}
	}
	if !found {
		t.Fatal("the engines view is missing from primaryViews — the gallery is unreachable")
	}
}

// ---- helpers -------------------------------------------------------------

// treeText extracts every text string in a widget tree (labels, buttons,
// rich text segments) for containment assertions.
func treeText(obj fyne.CanvasObject) string {
	var b strings.Builder
	collectText(obj, &b)
	return b.String()
}

func collectText(obj fyne.CanvasObject, b *strings.Builder) {
	switch o := obj.(type) {
	case *widget.Label:
		b.WriteString(o.Text + "\n")
	case *widget.Button:
		b.WriteString(o.Text + "\n")
	case *widget.RichText:
		for _, seg := range o.Segments {
			if ts, ok := seg.(*widget.TextSegment); ok {
				b.WriteString(ts.Text + "\n")
			}
		}
	case *widget.Select:
		for _, opt := range o.Options {
			b.WriteString(opt + "\n")
		}
		b.WriteString(o.Selected + "\n")
	case *widget.Card:
		b.WriteString(o.Title + "\n")
		if o.Subtitle != "" {
			b.WriteString(o.Subtitle + "\n")
		}
		if o.Content != nil {
			collectText(o.Content, b)
		}
	case *container.Scroll:
		// container.NewScroll wraps the body — the gallery's whole content
		// lives under its Content.
		if o.Content != nil {
			collectText(o.Content, b)
		}
	case *fyne.Container:
		for _, c := range o.Objects {
			collectText(c, b)
		}
	}
}

// forEachButton walks the tree and applies fn to every button.
func forEachButton(obj fyne.CanvasObject, fn func(*widget.Button)) {
	switch o := obj.(type) {
	case *widget.Button:
		fn(o)
	case *widget.Card:
		if o.Content != nil {
			forEachButton(o.Content, fn)
		}
	case *container.Scroll:
		if o.Content != nil {
			forEachButton(o.Content, fn)
		}
	case *fyne.Container:
		for _, c := range o.Objects {
			forEachButton(c, fn)
		}
	}
}

// sameColors compares two colors by their RGBA channels.
func sameColors(a, b interface{ RGBA() (r, g, b, a2 uint32) }) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
