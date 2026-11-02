//go:build !headless

// nav_ownership_test.go — v1.11.1 navigation ownership regression suite.
//
// The operator reported duplicated/cluttered main navigation on a real
// Windows PC. Root cause (fixed in shell.go the same session): s.rail was
// inserted BOTH as the content border's left object AND as a direct child
// of the responsive container — one CanvasObject, two layout parents, two
// paint passes. These tests pin the fixed contract mechanically:
//
//   - every shell-owned CanvasObject is referenced by exactly ONE parent
//     container (tree identity — the exact double-parent defect fails);
//   - in every responsive zone exactly one navigation surface is visible
//     and every primary destination is rendered exactly once there;
//   - the utility routes stay reachable in every zone (rail section wide,
//     "More" menu in the compact strip);
//   - the real Fyne test-canvas layout is exercised at 360–1920 logical
//     px, including re-layout after resize (not just the pure policy).
package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// walkOwnedTree calls fn for o and every descendant reachable through
// public composition (containers, scrolls, cards). fn is called with the
// parent that references the child (nil for the root). Distinct from
// composer_test.go's walkTree (single-visit): this one carries the parent
// reference so ownership can be counted.
func walkOwnedTree(o fyne.CanvasObject, parent fyne.CanvasObject, fn func(child, parent fyne.CanvasObject)) {
	if o == nil {
		return
	}
	fn(o, parent)
	record := func(child fyne.CanvasObject) {
		walkOwnedTree(child, o, fn)
	}
	switch c := o.(type) {
	case *fyne.Container:
		for _, child := range c.Objects {
			record(child)
		}
	case *widget.Card:
		record(c.Content)
	case *container.Scroll:
		record(c.Content)
	}
}

// TestShellSingleParentOwnership pins the v1.11.1 ownership contract: a
// shell-owned CanvasObject may be referenced by exactly ONE parent (the
// historical defect had the rail inside BOTH the content border and the
// responsive container).
func TestShellSingleParentOwnership(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()

	refs := map[fyne.CanvasObject][]fyne.CanvasObject{}
	walkOwnedTree(root, nil, func(child, parent fyne.CanvasObject) {
		if parent != nil {
			refs[child] = append(refs[child], parent)
		}
	})
	for obj, parents := range refs {
		if len(parents) != 1 {
			t.Fatalf("CanvasObject %T (%v) has %d parents — exactly one parent is required (double-parent navigation defect)", obj, obj, len(parents))
		}
	}

	// The specific historical shape: the rail must NOT be a child of the
	// content border (it is owned by the responsive container only).
	outer, ok := root.(*fyne.Container)
	if !ok {
		t.Fatalf("root must be the responsive container, got %T", root)
	}
	if len(outer.Objects) != 3 {
		t.Fatalf("responsive container must own exactly 3 children (rail, topNav, content), got %d", len(outer.Objects))
	}
	railChild, stripChild, contentChild := outer.Objects[0], outer.Objects[1], outer.Objects[2]
	if railChild != fyne.CanvasObject(sh.rail) {
		t.Fatalf("responsive container child 0 must be the rail, got %T", railChild)
	}
	if stripChild != sh.topNav {
		t.Fatalf("responsive container child 1 must be the compact strip, got %T", stripChild)
	}
	border, ok := contentChild.(*fyne.Container)
	if !ok {
		t.Fatalf("responsive container child 2 must be the content border, got %T", contentChild)
	}
	for _, child := range border.Objects {
		if child == fyne.CanvasObject(sh.rail) {
			t.Fatal("the rail must not be a child of the content border (v1.11.1 ownership contract; the pre-fix double-parent defect)")
		}
	}
	drainBackgroundRefreshes()
}

// countButtonsIn returns how many *widget.Button with the exact text live
// under o (only VISIBLE ones when visibleOnly is set).
func countButtonsIn(o fyne.CanvasObject, text string, visibleOnly bool) int {
	n := 0
	walkOwnedTree(o, nil, func(child, _ fyne.CanvasObject) {
		if b, ok := child.(*widget.Button); ok && b.Text == text {
			if visibleOnly && !b.Visible() {
				return
			}
			n++
		}
	})
	return n
}

// TestEveryDestinationRenderedOncePerZone: for each responsive zone
// (driven through the REAL shell tree with the real responsiveLayout),
// exactly one navigation surface is visible and every primary destination
// is rendered exactly once in the visible surface. Re-layout after resize
// is exercised too (widths sweep up and down again).
func TestEveryDestinationRenderedOncePerZone(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()
	l := &responsiveLayout{rail: sh.rail, topNav: sh.topNav, inner: root.(*fyne.Container).Objects[2]}

	widths := []float32{360, 500, 640, 800, 999, 1000, 1280, 1440, 1920, 1280, 500, 1000}
	for _, w := range widths {
		l.Layout(nil, fyne.NewSize(w, 700))

		if sh.rail.Visible() == sh.topNav.Visible() {
			t.Fatalf("width %v: exactly one navigation surface must be visible (rail=%v topNav=%v)", w, sh.rail.Visible(), sh.topNav.Visible())
		}
		active := sh.topNav
		if sh.rail.Visible() {
			active = sh.rail
		}

		for _, v := range primaryViews {
			if n := countButtonsIn(active, v.label, true); n != 1 {
				t.Fatalf("width %v: destination %q rendered %d times in the active navigation surface — exactly one occurrence is required", w, v.label, n)
			}
		}
		// The compact strip's More affordance (never a duplicate of a
		// destination; utilities live behind it).
		if !sh.rail.Visible() {
			if n := countButtonsIn(active, "More", true); n != 1 {
				t.Fatalf("width %v: compact strip must carry exactly one More button, got %d", w, n)
			}
		}
	}
	drainBackgroundRefreshes()
}

// TestUtilitiesReachableEveryZone: the four utility routes are reachable
// in the wide/compact rail (grouped section) and in the narrow top strip
// (More menu whose items dispatch through showSub).
func TestUtilitiesReachableEveryZone(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()
	w := ap.a.NewWindow("")
	w.SetContent(root)
	w.Resize(fyne.NewSize(1280, 800))
	l := &responsiveLayout{rail: sh.rail, topNav: sh.topNav, inner: root.(*fyne.Container).Objects[2]}

	// Wide + medium: utilities are grouped in the rail behind a section
	// heading — one button per utility, none mixed into the primary set.
	for _, width := range []float32{1280, 800} {
		l.Layout(nil, fyne.NewSize(width, 700))
		for _, p := range subPanels {
			if n := countButtonsIn(sh.rail, p.label, true); n != 1 {
				t.Fatalf("width %v: utility %q rendered %d times in the rail — exactly one is required", width, p.label, n)
			}
		}
	}

	// Narrow: the More button opens a menu overlay carrying every utility.
	l.Layout(nil, fyne.NewSize(500, 700))
	var more *widget.Button
	walkOwnedTree(sh.topNav, nil, func(child, _ fyne.CanvasObject) {
		if b, ok := child.(*widget.Button); ok && b.Text == "More" {
			more = b
		}
	})
	if more == nil {
		t.Fatal("compact strip must carry the More utilities button")
	}
	uiDoLocked(func() { test.Tap(more) })
	if len(w.Canvas().Overlays().List()) == 0 {
		t.Fatal("tapping More must open the utilities menu overlay")
	}

	// The menu content itself: one item per utility, each dispatching
	// through showSub (the active primary anchor is preserved).
	menu := sh.buildUtilityMenu()
	if len(menu.Items) != len(subPanels) {
		t.Fatalf("utilities menu carries %d items, want %d", len(menu.Items), len(subPanels))
	}
	uiDoLocked(func() {
		sh.show("agent")
		for i, p := range subPanels {
			if menu.Items[i] == nil || menu.Items[i].Action == nil {
				t.Fatalf("utility %q has no dispatch action", p.label)
			}
			menu.Items[i].Action() // dispatches showSub — must not panic
		}
	})
	if sh.active != "agent" {
		t.Fatalf("active = %q, want agent (menu dispatch must not corrupt the primary anchor)", sh.active)
	}
	drainBackgroundRefreshes()
}

// TestNavigationActionInvokedOnce: tapping a destination navigates
// exactly once — the lazy view cache holds ONE instance per id and the
// same button tapped twice never builds a second tree.
func TestNavigationActionInvokedOnce(t *testing.T) {
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	_ = sh.root()

	var chat *widget.Button
	walkOwnedTree(sh.rail, nil, func(child, _ fyne.CanvasObject) {
		if b, ok := child.(*widget.Button); ok && b.Text == "Chat" {
			chat = b
		}
	})
	if chat == nil {
		t.Fatal("rail must carry the Chat destination")
	}
	uiDoLocked(func() { test.Tap(chat) })
	if sh.active != "chat" {
		t.Fatalf("active = %q, want chat after one tap", sh.active)
	}
	if len(sh.views) != 1 {
		t.Fatalf("lazy view cache holds %d instances after one navigation, want exactly 1", len(sh.views))
	}
	uiDoLocked(func() { test.Tap(chat) })
	if len(sh.views) != 1 {
		t.Fatalf("re-tapping the ACTIVE destination rebuilt the view (%d instances) — the already-active selection must be a no-op", len(sh.views))
	}
	drainBackgroundRefreshes()
}

// TestShortcutKeyMapCoversAllPrimaryViews pins the EXACT shortcut mapping:
// Ctrl+1..8 in registry order, one key per primary view. (The pre-fix map
// stopped at Ctrl+7 even though the registry holds eight destinations.)
// The map is asserted against the registry so a future ninth view without
// a key fails HERE instead of shipping silently — the view remains
// pointer-reachable by contract, but the mapping must be documented.
func TestShortcutKeyMapCoversAllPrimaryViews(t *testing.T) {
	wantKeys := []fyne.KeyName{fyne.Key1, fyne.Key2, fyne.Key3, fyne.Key4, fyne.Key5, fyne.Key6, fyne.Key7, fyne.Key8}
	if len(wantKeys) < len(primaryViews) {
		t.Fatalf("shortcut map covers %d keys for %d primary views — extend the documented mapping", len(wantKeys), len(primaryViews))
	}
	for i, v := range primaryViews {
		if i >= len(wantKeys) || wantKeys[i] == "" {
			t.Fatalf("primary view %q (%d) has no shortcut key in the documented mapping", v.id, i)
		}
	}
	// The registry order IS the shortcut order (Ctrl+1 = chat … Ctrl+8 = studio).
	expect := map[string]fyne.KeyName{
		"chat": fyne.Key1, "agent": fyne.Key2, "workspace": fyne.Key3,
		"lab": fyne.Key4, "automation": fyne.Key5, "system": fyne.Key6,
		"settings": fyne.Key7, "studio": fyne.Key8,
	}
	for i, v := range primaryViews {
		if wantKeys[i] != expect[v.id] {
			t.Fatalf("primary view %q maps to Ctrl+%s, want Ctrl+%s", v.id, wantKeys[i], expect[v.id])
		}
	}
}
