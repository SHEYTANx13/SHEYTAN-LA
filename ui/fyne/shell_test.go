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
	"fyne.io/fyne/v2/widget"
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

// TestResponsiveLayoutBreakpoint pins the three-zone adaptation: wide
// windows show the full rail, medium windows the compact rail, and narrow
// windows the top navigation strip — navigation is reachable in every
// zone (run #54 UX contract: compact mode never silently hides it).
func TestResponsiveLayoutBreakpoint(t *testing.T) {
	rail := container.NewVBox()
	topNav := container.NewHScroll(container.NewHBox(widget.NewLabel("nav")))
	inner := container.NewStack()
	l := &responsiveLayout{rail: rail, topNav: topNav, inner: inner}

	l.Layout(nil, fyne.NewSize(1000, 700))
	if !rail.Visible() {
		t.Fatal("wide window must show the rail")
	}
	if topNav.Visible() {
		t.Fatal("wide window must hide the top strip")
	}
	if inner.Size().Width >= 1000 {
		t.Fatal("content must leave room for the rail on wide windows")
	}

	// Medium zone: the compact rail keeps its own width budget.
	l.Layout(nil, fyne.NewSize(800, 700))
	if !rail.Visible() {
		t.Fatal("medium window must show the compact rail")
	}
	if rail.Size().Width != railWidthCompact {
		t.Fatalf("compact rail width = %v, want %v", rail.Size().Width, railWidthCompact)
	}

	l.Layout(nil, fyne.NewSize(500, 700))
	if rail.Visible() {
		t.Fatal("narrow window must hide the rail (compact adaptation)")
	}
	if !topNav.Visible() {
		t.Fatal("narrow window must show the top navigation strip — navigation stays reachable")
	}
	if inner.Position().Y != topStripHeight {
		t.Fatalf("narrow window content must start below the strip, got Y=%v", inner.Position().Y)
	}
	if inner.Size().Width != 500 {
		t.Fatalf("narrow window content must use the full width, got %v", inner.Size().Width)
	}
}

// TestNavPolicySizeClasses pins the policy across the required window-size
// classes: small laptop windows, 1280x800, 1440x900, 1920x1080 — plus the
// exact breakpoint edges.
func TestNavPolicySizeClasses(t *testing.T) {
	cases := []struct {
		width float32
		want  navZone
	}{
		{360, navTopStrip},
		{500, navTopStrip},
		{639, navTopStrip},
		{640, navRailCompact},
		{800, navRailCompact},
		{999, navRailCompact},
		{1000, navRailWide},
		{1280, navRailWide},
		{1440, navRailWide},
		{1920, navRailWide},
	}
	for _, c := range cases {
		if got := navPolicy(c.width); got != c.want {
			t.Errorf("navPolicy(%v) = %v, want %v", c.width, got, c.want)
		}
	}
}

// TestResponsiveLayoutDegenerateSizes: zero, negative and tiny available
// sizes must never panic and never produce negative content geometry.
func TestResponsiveLayoutDegenerateSizes(t *testing.T) {
	rail := container.NewVBox()
	topNav := container.NewHScroll(container.NewHBox(widget.NewLabel("nav")))
	inner := container.NewStack()
	l := &responsiveLayout{rail: rail, topNav: topNav, inner: inner}

	for _, sz := range []fyne.Size{
		fyne.NewSize(0, 0),
		fyne.NewSize(1, 1),
		fyne.NewSize(10, 10),
		fyne.NewSize(200, 40),
		fyne.NewSize(-50, -50),
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Layout(%v) panicked: %v", sz, r)
				}
			}()
			l.Layout(nil, sz)
			if inner.Size().Width < 0 || inner.Size().Height < 0 {
				t.Fatalf("Layout(%v) produced negative inner size %v", sz, inner.Size())
			}
		}()
	}
}

// TestCompactNavigationReachable: in the top-strip zone the strip must be
// part of the built shell and carry a button for EVERY primary view.
func TestCompactNavigationReachable(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()
	if sh.topNav == nil {
		t.Fatal("shell must build the compact top navigation strip")
	}
	buttons := 0
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil {
			return
		}
		if b, ok := o.(*widget.Button); ok && b.Text != "" {
			buttons++
		}
		switch c := o.(type) {
		case *fyne.Container:
			for _, child := range c.Objects {
				walk(child)
			}
		case *container.Scroll:
			walk(c.Content)
		}
	}
	if r, ok := sh.topNav.(fyne.CanvasObject); ok {
		walk(r)
	} else {
		t.Fatal("topNav must be a CanvasObject")
	}
	if buttons < len(primaryViews) {
		t.Fatalf("compact strip carries %d buttons, want at least %d (one per primary view)", buttons, len(primaryViews))
	}
	_ = root
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
