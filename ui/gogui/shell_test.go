//go:build !headless

// shell_test.go — the test harness + navigation/status/shortcut contracts,
// ported from ui/fyne/shell_test.go patterns to the go-gui test driver.
//
// Harness: SetTestUIMarshal (inline uiDo under the process mutex) + a
// gui.NewTestWindow over the shell state + the fake core over TCP. The
// go-gui test driver settles QueueCommand callbacks on every Test* call,
// and the marshalled mutations run inline under the test lock, so the
// assertions are deterministic without a display.
package goguiui

import (
	"testing"
	"time"

	"github.com/go-gui-org/go-gui/gui"
)

// newTestShell builds the app+shell over a fresh fake core and a test
// window at the given logical size. The window renders the shell root so
// the first frame exists before assertions.
func newTestShell(t *testing.T, width, height int) (*fakeCore, *App, *shellState, *gui.Window) {
	t.Helper()
	restore := SetTestUIMarshal()
	t.Cleanup(restore)

	fc := newFakeCore(t)
	t.Cleanup(fc.Close)

	ap := NewApp(fc.client())
	s := ap.shell

	w := gui.NewTestWindow(t, gui.WindowCfg{
		State:  s,
		Width:  width,
		Height: height,
		OnInit: func(w *gui.Window) {
			w.SetView(s.rootView)
			ap.windowReady.Store(true)
			ap.installRuntimeCallbacks()
		},
		OnEvent: shortcutOnEvent(s),
	})
	s.showInitial()
	w.TestRender(nil)
	return fc, ap, s, w
}

// settle re-renders after state changes (the driver rebuilds the tree).
func settle(w *gui.Window) { w.TestRender(nil) }

// TestShellInitialViewIsChat pins the showInitial contract: the first
// frame shows the first primary view (never a blank surface).
func TestShellInitialViewIsChat(t *testing.T) {
	_, ap, s, w := newTestShell(t, 1280, 720)
	if got := s.activeView(); got != "chat" {
		t.Fatalf("initial view = %q, want chat", got)
	}
	if ap.Client() == nil {
		t.Fatal("app client missing")
	}
	settle(w) // re-render must not panic and must keep the selection
	if got := s.activeView(); got != "chat" {
		t.Fatalf("view after render = %q, want chat", got)
	}
}

// TestNavigationSwitchUpdatesStateAndFrame drives the rail buttons: every
// primary destination switches the active view and the frame re-renders
// with that view's content (the interaction test the Fyne shell ships).
func TestNavigationSwitchUpdatesStateAndFrame(t *testing.T) {
	_, _, s, w := newTestShell(t, 1280, 720)
	for _, pv := range primaryViews {
		if err := w.TestClick(absID("nav", pv.id)); err != nil {
			t.Fatalf("click nav %s: %v", pv.id, err)
		}
		if got := s.activeView(); got != pv.id {
			t.Fatalf("active view after click = %q, want %q", got, pv.id)
		}
		settle(w)
		// The dock renders the active view's panel content: the view root
		// id must be present in the layout tree.
		layout := w.TestRender(nil)
		want := absID("view-" + pv.id)
		if _, ok := layout.FindByID(want); !ok {
			t.Fatalf("view root %q not in tree after switching to %s", want, pv.id)
		}
	}
}

// TestSubPanelOpenAndBack drives the utilities: History opens over the
// shell and Back returns to the active primary view.
func TestSubPanelOpenAndBack(t *testing.T) {
	_, _, s, w := newTestShell(t, 1280, 720)
	if err := w.TestClick(absID("nav-sub", "history")); err != nil {
		t.Fatalf("click history: %v", err)
	}
	if got := s.openSub(); got != "history" {
		t.Fatalf("openSub = %q, want history", got)
	}
	settle(w)
	layout := w.TestRender(nil)
	if _, ok := layout.FindByID(absID("view-history")); !ok {
		t.Fatal("history surface not in tree")
	}
	if err := w.TestClick(absID("sub-back")); err != nil {
		t.Fatalf("click back: %v", err)
	}
	if got := s.openSub(); got != "" {
		t.Fatalf("openSub after back = %q, want empty", got)
	}
}

// TestStatusLineReflectsConnection drives the honest status contract:
// disconnected → (bootstrap over the fake) → connected with the core
// version; the retry button and error line appear ONLY when disconnected
// with a typed boot error.
func TestStatusLineReflectsConnection(t *testing.T) {
	fc, ap, s, w := newTestShell(t, 1280, 720)

	s.setBootError(errTestBoot)
	layout := w.TestRender(nil)
	if _, ok := layout.FindByID(absID("retry")); !ok {
		t.Fatal("retry button must be visible while disconnected with a boot error")
	}

	// Real bootstrap over the fake core: flips to connected.
	ap.bootstrap()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		connected, _, _, version := s.connectionSnapshot()
		if connected && version != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	connected, _, _, version := s.connectionSnapshot()
	if !connected {
		t.Fatal("bootstrap over the fake core did not reach connected")
	}
	if version == "" {
		t.Fatal("core version empty after successful handshake")
	}
	layout = w.TestRender(nil)
	if _, ok := layout.FindByID(absID("retry")); ok {
		t.Fatal("retry button must disappear once connected")
	}
	if _, ok := layout.FindByID(absID("status")); !ok {
		t.Fatal("status line missing")
	}
	_ = fc
}

// errTestBoot is a typed boot failure for the retry-visibility contract.
type testBootError struct{ msg string }

func (e testBootError) Error() string { return e.msg }

var errTestBoot = testBootError{"core unreachable (test)"}

// TestKeyboardShortcutsSwitchViews drives Ctrl+1..9 (the number row
// covers the registry) and asserts the active view follows.
func TestKeyboardShortcutsSwitchViews(t *testing.T) {
	_, _, s, w := newTestShell(t, 1280, 720)
	keyByIndex := []gui.KeyCode{gui.Key1, gui.Key2, gui.Key3, gui.Key4, gui.Key5, gui.Key6, gui.Key7, gui.Key8, gui.Key9}
	for i, pv := range primaryViews {
		if i >= len(keyByIndex) {
			break
		}
		if err := w.TestKey("", keyByIndex[i], gui.ModCtrl); err != nil {
			// Window-level shortcuts ride OnEvent, not per-widget targets;
			// TestKey needs a target id. Drive the same handler directly.
			shortcutOnEvent(s)(&gui.Event{Type: gui.EventKeyDown, KeyCode: keyByIndex[i], Modifiers: gui.ModCtrl}, w)
		}
		if got := s.activeView(); got != pv.id {
			t.Fatalf("shortcut %d: active view = %q, want %q", i+1, got, pv.id)
		}
	}
}

// TestSingleParentOwnershipStructural pins the structural analogue of the
// Fyne single-parent contract: every rendered id is unique in the tree
// (the toolkit's own duplicate-ID debug check over our registry).
func TestSingleParentOwnershipStructural(t *testing.T) {
	_, _, _, w := newTestShell(t, 1280, 720)
	w.TestRender(nil)
	if dups := w.TestDuplicateIDs(); len(dups) > 0 {
		t.Fatalf("duplicate widget ids in the rendered tree: %v", dups)
	}
}

// TestSizeClassForBreakpointMatrix pins the SINGLE size-class decision at
// every breakpoint boundary and across the work-order responsive matrix
// (§7.3): 360/430 compact phones, 600 small tablet, 768 tablet/compact
// desktop, 1024 compact desktop, 1280 standard desktop, 1366 laptop,
// 1920 large desktop. Pure, deterministic, display-free: this pins the
// classification contract; it is NOT a native rendering proof (the Fyne
// production shell and native launch smoke tests remain the hosted
// lanes' evidence — run 44835425398 honesty rules).
func TestSizeClassForBreakpointMatrix(t *testing.T) {
	cases := []struct {
		width float32
		want  sizeClass
	}{
		{360, sizeCompact},
		{430, sizeCompact},
		{599.9, sizeCompact},
		{600, sizeMedium},
		{768, sizeMedium},
		{899.9, sizeMedium},
		{900, sizeDesktop},
		{1024, sizeDesktop},
		{1279.9, sizeDesktop},
		{1280, sizeWide},
		{1366, sizeWide},
		{1920, sizeWide},
	}

	for _, tc := range cases {
		if got := sizeClassFor(tc.width); got != tc.want {
			t.Errorf("sizeClassFor(%v) = %v, want %v", tc.width, got, tc.want)
		}
	}
}
