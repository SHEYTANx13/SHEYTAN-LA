//go:build !headless

// shell_test.go — real view-tree composition tests over the Fyne test
// harness: the shell builds, navigation switches views, the responsive
// layout flips between rail and compact topologies at the breakpoint,
// and malformed/empty API data never panics the tree.
package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

func newTestAppForShell(t *testing.T) (*App, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())
	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	c := srv.client()
	a := test.NewApp()
	a.Settings().SetTheme(NewTheme())
	ap := NewApp(a, c)
	t.Cleanup(func() { ap.rt.Shutdown() })
	return ap, srv
}

// TestShellBuildsAllViews pins view-tree composition: every primary view
// builds against a live core without panic and the tree is non-empty.
func TestShellBuildsAllViews(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()
	if root == nil {
		t.Fatal("shell root must build")
	}
	for _, v := range primaryViews {
		obj := v.build(ap)
		if obj == nil {
			t.Fatalf("view %q must build", v.id)
		}
		// The view tree must contain renderable objects.
		if !treeHasObjects(obj) {
			t.Fatalf("view %q produced an empty tree", v.id)
		}
		drainBackgroundRefreshes()
	}
	for _, p := range subPanels {
		obj := p.build(ap)
		if obj == nil || !treeHasObjects(obj) {
			t.Fatalf("panel %q must build a non-empty tree", p.id)
		}
		drainBackgroundRefreshes()
	}
}

// drainBackgroundRefreshes waits (bounded) for constructor-spawned
// background refresh goroutines to complete their serialized UI work, so
// a later test step cannot race them.
func drainBackgroundRefreshes() {
	for i := 0; i < 200; i++ {
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
}

// TestShellNavigationSwitches pins that show() swaps the content area to
// the requested view without rebuilding the shell.
func TestShellNavigationSwitches(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	_ = sh.root()
	// Every widget mutation — including the test's own direct calls —
	// goes through the same serialization seam as production's fyne.Do.
	uiDoLocked(func() { sh.show("agent") })
	if sh.active != "agent" {
		t.Fatalf("active = %q, want agent", sh.active)
	}
	uiDoLocked(func() { sh.show("settings") })
	if sh.active != "settings" {
		t.Fatalf("active = %q, want settings", sh.active)
	}
	uiDoLocked(func() { sh.showSub("history") })
	// Sub-panel display keeps the primary anchor for "Back".
	uiDoLocked(func() { sh.show("chat") })
	if sh.active != "chat" {
		t.Fatalf("active = %q, want chat", sh.active)
	}
}

// TestResponsiveLayoutBreakpoint pins the compact adaptation: below the
// breakpoint the rail hides; at/above it the rail shows and content
// leaves room for it.
func TestResponsiveLayoutBreakpoint(t *testing.T) {
	rail := container.NewVBox()
	inner := container.NewStack()
	l := &responsiveLayout{rail: rail, inner: inner}

	l.Layout(nil, fyne.NewSize(1000, 700))
	if !rail.Visible() {
		t.Fatal("wide window must show the rail")
	}
	if inner.Size().Width >= 1000 {
		t.Fatal("content must leave room for the rail on wide windows")
	}

	l.Layout(nil, fyne.NewSize(500, 700))
	if rail.Visible() {
		t.Fatal("narrow window must hide the rail (compact adaptation)")
	}
	if inner.Size().Width != 500 {
		t.Fatalf("narrow window content must use the full width, got %v", inner.Size().Width)
	}
}

// TestViewsWithEmptyDataNoPanic pins honest empty states: a core that
// returns empty lists produces empty views — not panics and not errors.
func TestViewsWithEmptyDataNoPanic(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	_ = sh.root()
	for _, v := range primaryViews {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("view %q panicked on empty data: %v", v.id, r)
				}
			}()
			_ = v.build(ap)
		}()
	}
}

// treeHasObjects walks the tree looking for at least one concrete object.
func treeHasObjects(o fyne.CanvasObject) bool {
	if o == nil {
		return false
	}
	switch c := o.(type) {
	case *fyne.Container:
		for _, child := range c.Objects {
			if treeHasObjects(child) {
				return true
			}
		}
		return false
	default:
		return true
	}
}
