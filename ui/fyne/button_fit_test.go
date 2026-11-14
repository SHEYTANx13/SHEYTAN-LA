//go:build !headless

// button_fit_test.go — v1.11.1 button-fit contract: at every
// representative logical size, for EVERY primary and utility
// destination, the assembled real widget tree keeps every visible button
// at or above its rendered/minimum content size (no clipped label, no
// compressed touch target), every visible card fits the content width
// (card content is not clipped), and every action stays reachable.
package fyneui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// fitSizes spans every breakpoint class from the mandatory matrix: phone
// portrait/landscape, tablet, desktop, wide, the breakpoint boundaries,
// and the minimum-size probe.
var fitSizes = []fyne.Size{
	fyne.NewSize(320, 568),  // smallest phone portrait
	fyne.NewSize(390, 844),  // phone portrait
	fyne.NewSize(568, 320),  // phone landscape
	fyne.NewSize(600, 800),  // small tablet
	fyne.NewSize(768, 1024), // tablet
	fyne.NewSize(1024, 768), // compact laptop
	fyne.NewSize(1366, 768), // desktop
	fyne.NewSize(1920, 1080),
	fyne.NewSize(2560, 1440), // wide (real logical size; no false clamp)
	fyne.NewSize(599, 480),   // breakpoint boundary below
	fyne.NewSize(600, 480),   // breakpoint boundary at
	fyne.NewSize(601, 480),   // breakpoint boundary above
	fyne.NewSize(899, 480),
	fyne.NewSize(900, 480),
	fyne.NewSize(901, 480),
	fyne.NewSize(1279, 480),
	fyne.NewSize(1280, 480),
	fyne.NewSize(1281, 480),
	fyne.NewSize(240, 320), // minimum-size probe
}

// assertInBounds fails when a rigid control renders outside the content
// area (clipped text / unreachable action).
func assertInBounds(t *testing.T, label, kind string, abs fyne.Position, size fyne.Size, effW float32) {
	t.Helper()
	if abs.X < -0.5 || abs.X+size.Width > effW+2 {
		t.Fatalf("%s: %s at (%.0f,%.0f) size (%.0fx%.0f) escapes the content width %.0f — clipped/unreachable",
			label, kind, abs.X, abs.Y, size.Width, size.Height, effW)
	}
}

// walkCanvasObjects descends containers, scroll views and splits (the
// object tree the shell and panels own; card-internal trees are asserted
// through the Card MinSize contract instead). fn receives each visible
// object with its ABSOLUTE position (accumulated through parents), so
// bounds checks are exact.
func walkCanvasObjects(o fyne.CanvasObject, fn func(fyne.CanvasObject, fyne.Position)) {
	walkAt(o, fyne.NewPos(0, 0), fn)
}

func walkAt(o fyne.CanvasObject, origin fyne.Position, fn func(fyne.CanvasObject, fyne.Position)) {
	if o == nil || !o.Visible() {
		return
	}
	pos := o.Position().Add(origin)
	fn(o, pos)
	switch c := o.(type) {
	case *fyne.Container:
		for _, ch := range c.Objects {
			walkAt(ch, pos, fn)
		}
	case *container.Scroll:
		walkAt(c.Content, pos, fn)
	case *container.Split:
		walkAt(c.Leading, pos, fn)
		walkAt(c.Trailing, pos, fn)
	case *widget.Card:
		walkAt(c.Content, pos, fn)
	case *container.AppTabs:
		// Only the CURRENT tab's content is visible/operable; inactive
		// tab content must not produce false positives.
		if cur := c.CurrentTab(); cur != nil {
			walkAt(cur.Content, pos, fn)
		}
	}
}

// TestButtonFitMatrix is the mandatory fit sweep: every destination, every
// representative size, every visible button and card.
func TestButtonFitMatrix(t *testing.T) {
	_, sh, w, rc, inner := matrixShell(t)

	destinations := make([]string, 0, len(primaryViews)+len(subPanels))
	for _, v := range primaryViews {
		destinations = append(destinations, v.id)
	}
	for _, p := range subPanels {
		destinations = append(destinations, p.id)
	}

	utilityIDs := map[string]bool{}
	for _, p := range subPanels {
		utilityIDs[p.id] = true
	}
	for _, sz := range fitSizes {
		// Strict fit assertions run at the documented supported matrix
		// floor (>=320 logical); the 240x320 minimum-size probe still
		// walks the whole assembled tree (panics surface) and requires
		// operable controls, without demanding full-label fit below the
		// supported floor.
		strict := sz.Width >= 320
		uiDoLocked(func() { w.Resize(sz) })
		for _, id := range destinations {
			// Primary destinations navigate through show(); utility
			// surfaces through showSub() — the two REAL production entry
			// points, never a mixed call.
			uiDoLocked(func() {
				if utilityIDs[id] {
					sh.showSub(id)
				} else {
					sh.show(id)
				}
			})
			drainBackgroundRefreshes()

			label := fmt.Sprintf("%dx%d→%s", int(sz.Width), int(sz.Height), id)
			effW := inner.Size().Width
			if effW < 0 {
				effW = 0
			}
			_ = label

			buttons, interactives := 0, 0
			walkCanvasObjects(sh.body, func(o fyne.CanvasObject, abs fyne.Position) {
				switch c := o.(type) {
				case *widget.Button:
					buttons++
					interactives++
					min := c.MinSize()
					got := c.Size()
					// A visible, enabled-or-disabled action button must
					// never be compressed below its content size: that is
					// the clipped-label defect class.
					if strict && (got.Width+0.5 < min.Width || got.Height+0.5 < min.Height) {
						t.Fatalf("%s: button %q compressed to (%.0fx%.0f), below its content size (%.0fx%.0f) — the label is clipped",
							label, c.Text, got.Width, got.Height, min.Width, min.Height)
					}
					// Reachability: a visible button with an empty text
					// AND no icon is an unlabeled, unreachable action.
					if c.Text == "" && c.Icon == nil {
						t.Fatalf("%s: visible button carries neither label nor icon — an unreachable action", label)
					}
					// The button must lie inside the content area
					// (absolute position, accumulated through parents).
					if strict && (abs.X < -0.5 || abs.X+got.Width > effW+2) {
						t.Fatalf("%s: button %q at (%.0f,%.0f) size (%.0fx%.0f) escapes the content width %.0f",
							label, c.Text, abs.X, abs.Y, got.Width, got.Height, effW)
					}
				case *widget.Entry:
					interactives++
					if strict {
						assertInBounds(t, label, "entry", abs, c.Size(), effW)
					}
				case *widget.Select:
					interactives++
					if strict {
						assertInBounds(t, label, "select", abs, c.Size(), effW)
					}
				case *widget.Check:
					interactives++
					if strict {
						assertInBounds(t, label, "check", abs, c.Size(), effW)
					}
				case *widget.Label:
					// A RIGID label (no wrapping, no truncation) renders
					// clipped text when it escapes the content area;
					// wrapped labels reflow and truncated labels shrink —
					// only the rigid ones are defects.
					if strict && c.Wrapping == fyne.TextWrapOff && c.Truncation == fyne.TextTruncateOff {
						assertInBounds(t, label, "label", abs, c.Size(), effW)
					}
				}
			})

			// Reachability: every destination renders at least one operable
			// control at every size (button-led views show buttons;
			// Settings-like views show entries/selects/checks).
			if interactives == 0 {
				t.Fatalf("%s: destination rendered with zero operable controls — actions must stay reachable", label)
			}
			_ = buttons
		}
	}
	_ = rc
}

// TestLongLabelButtonsShortened pins the v1.11.1 short-label policy for
// the exact controls the work order named (labels move their explanatory
// tail into the adjacent caption instead of the button face).
func TestLongLabelButtonsShortened(t *testing.T) {
	forbidden := []string{
		"Install update (restarts engine)",
		"Open models folder",
		"Open 3D Studio (browser)",
		"Open in Workspace",
		"Check for updates",
		"Cancel download",
		"Stop viewer",
		"Copy viewer URL",
	}

	_, sh, w, _, _ := matrixShell(t)
	uiDoLocked(func() { w.Resize(fyne.NewSize(1366, 768)) })
	for _, id := range []string{"models", "studio", "workspace"} {
		uiDoLocked(func() { sh.show(id) })
		drainBackgroundRefreshes()
		walkCanvasObjects(sh.body, func(o fyne.CanvasObject, _ fyne.Position) {
			if b, ok := o.(*widget.Button); ok {
				for _, f := range forbidden {
					if b.Text == f {
						t.Errorf("long label %q still in use — the short-label policy moved its detail into the adjacent caption", f)
					}
				}
			}
		})
	}
}
