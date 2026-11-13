//go:build !headless

// shell_responsive_test.go — the v1.11.1 responsive viewport matrix (work
// order for run 43008361835 §F4/§5.3): the REAL assembled shell is resized
// through phone, tablet, laptop, desktop and wide logical sizes plus every
// breakpoint boundary and degenerate size, asserting rendered geometry —
// not just policy decisions.
//
// Pixel-image goldens are NOT reachable through the public API of the
// locked Fyne v2.8.2 (the software painter lives in fyne's internal/
// packages and the test canvas's Capture() renders blank without it), so
// the stable goldens here are deterministic structural snapshots of the
// rendered tree (type, text, position, size, visibility) — font-independent
// and reviewable as text, per the work order's noise-avoidance rule.
package fyneui

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// viewportMatrix is the mandatory logical-size matrix from the responsive
// contract: phone portrait/landscape, small tablets, tablets, compact
// laptops, standard desktops, wide/high-resolution displays.
var viewportMatrix = []fyne.Size{
	// Compact phone portrait.
	fyne.NewSize(320, 568),
	fyne.NewSize(360, 640),
	fyne.NewSize(375, 667),
	fyne.NewSize(390, 844),
	fyne.NewSize(412, 915),
	fyne.NewSize(430, 932),
	// Phone landscape.
	fyne.NewSize(568, 320),
	fyne.NewSize(667, 375),
	fyne.NewSize(844, 390),
	// Small tablets.
	fyne.NewSize(600, 800),
	fyne.NewSize(720, 960),
	// Tablets.
	fyne.NewSize(768, 1024),
	fyne.NewSize(800, 600),
	fyne.NewSize(834, 1112),
	fyne.NewSize(1024, 768),
	// Compact laptop / desktop.
	fyne.NewSize(1280, 720),
	fyne.NewSize(1366, 768),
	// Standard desktop.
	fyne.NewSize(1440, 900),
	fyne.NewSize(1600, 900),
	fyne.NewSize(1920, 1080),
	// Wide and high-resolution displays.
	fyne.NewSize(2560, 1440),
	fyne.NewSize(3440, 1440),
	fyne.NewSize(3840, 2160),
}

// boundaryMatrix covers one logical unit below, at and above every
// breakpoint, at three representative heights.
var boundaryMatrix = []fyne.Size{
	fyne.NewSize(599, 240), fyne.NewSize(599, 320), fyne.NewSize(599, 480),
	fyne.NewSize(600, 240), fyne.NewSize(600, 320), fyne.NewSize(600, 480),
	fyne.NewSize(601, 240), fyne.NewSize(601, 320), fyne.NewSize(601, 480),
	fyne.NewSize(899, 240), fyne.NewSize(899, 320), fyne.NewSize(899, 480),
	fyne.NewSize(900, 240), fyne.NewSize(900, 320), fyne.NewSize(900, 480),
	fyne.NewSize(901, 240), fyne.NewSize(901, 320), fyne.NewSize(901, 480),
	fyne.NewSize(1279, 240), fyne.NewSize(1279, 320), fyne.NewSize(1279, 480),
	fyne.NewSize(1280, 240), fyne.NewSize(1280, 320), fyne.NewSize(1280, 480),
	fyne.NewSize(1281, 240), fyne.NewSize(1281, 320), fyne.NewSize(1281, 480),
}

// degenerateMatrix proves no panics or impossible layout calculations at
// zero, one and sub-breakpoint sizes.
var degenerateMatrix = []fyne.Size{
	fyne.NewSize(0, 0),
	fyne.NewSize(1, 1),
	fyne.NewSize(240, 320),
	fyne.NewSize(280, 480),
	fyne.NewSize(320, 240),
}

// matrixShell builds the real assembled shell inside a real test window.
// The returned rc is the responsive container itself: the canvas applies
// theme padding around the content, so rc.Size() — not the raw window size
// — is the EFFECTIVE logical width the policy resolves against, exactly as
// in production.
func matrixShell(t *testing.T) (*App, *shell, fyne.Window, *fyne.Container, *fyne.Container) {
	t.Helper()
	ap, _ := newTestAppForShell(t)
	sh := newShell(ap)
	root := sh.root()
	w := test.NewWindow(root)
	t.Cleanup(w.Close)
	// The responsive container owns exactly three children: rail, compact
	// strip, content border (v1.11.1 ownership contract).
	rc := root.(*fyne.Container)
	if len(rc.Objects) != 3 {
		t.Fatalf("responsive container carries %d children, want exactly 3 (rail, topNav, content)", len(rc.Objects))
	}
	inner := rc.Objects[2].(*fyne.Container)
	return ap, sh, w, rc, inner
}

// assertMatrixGeometry asserts the rendered geometry invariants at one
// window size: the resolved zone matches the policy against the EFFECTIVE
// width, navigation stays reachable, the content area keeps a useful
// non-negative share, and no object escapes the canvas or produces NaN.
func assertMatrixGeometry(t *testing.T, label string, sz fyne.Size, sh *shell, rc, inner *fyne.Container) {
	t.Helper()
	eff := rc.Size() // effective logical size after canvas padding
	// A padded canvas can report a negative effective size at degenerate
	// window sizes (0x0/1x1 minus 2x8 padding): clamp like the layout does.
	if eff.Width < 0 {
		eff.Width = 0
	}
	if eff.Height < 0 {
		eff.Height = 0
	}
	zone := navPolicy(eff.Width)

	if zone != navTopStrip {
		if !sh.rail.Visible() {
			t.Fatalf("%s %dx%d (zone %s): navigation rail must be visible", label, int(sz.Width), int(sz.Height), zone)
		}
		if sh.topNav.Visible() {
			t.Fatalf("%s %dx%d (zone %s): compact strip must be hidden in rail zones", label, int(sz.Width), int(sz.Height), zone)
		}
		wantRail := railWidthCompact
		if zone == navRailWide {
			wantRail = railWidthWide
		}
		if got := sh.rail.Size().Width; got != fyne.Min(wantRail, eff.Width) {
			t.Fatalf("%s %dx%d: rail width = %v, want %v", label, int(sz.Width), int(sz.Height), got, fyne.Min(wantRail, eff.Width))
		}
		if got := sh.rail.Position(); got.X != 0 || got.Y != 0 {
			t.Fatalf("%s %dx%d: rail must sit at (0,0), got %v", label, int(sz.Width), int(sz.Height), got)
		}
	} else {
		if sh.rail.Visible() {
			t.Fatalf("%s %dx%d (zone %s): rail must collapse in the compact zone", label, int(sz.Width), int(sz.Height), zone)
		}
		if !sh.topNav.Visible() {
			t.Fatalf("%s %dx%d (zone %s): compact top strip must keep navigation reachable", label, int(sz.Width), int(sz.Height), zone)
		}
		wantStrip := fyne.Min(topStripHeight, eff.Height)
		if got := sh.topNav.Size().Height; got != wantStrip {
			t.Fatalf("%s %dx%d: strip height = %v, want %v", label, int(sz.Width), int(sz.Height), got, wantStrip)
		}
		if got := sh.topNav.Size().Width; got != eff.Width {
			t.Fatalf("%s %dx%d: strip must span the effective width, got %v (want %v)", label, int(sz.Width), int(sz.Height), got, eff.Width)
		}
	}

	// Geometry sanity for every zone: no NaN, no negatives, content stays
	// inside the canvas and keeps a useful share at every matrix size.
	innerPos, innerSize := inner.Position(), inner.Size()
	for _, v := range []float32{innerPos.X, innerPos.Y, innerSize.Width, innerSize.Height} {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("%s %dx%d: inner geometry is NaN/Inf: pos=%v size=%v", label, int(sz.Width), int(sz.Height), innerPos, innerSize)
		}
	}
	if innerSize.Width < 0 || innerSize.Height < 0 {
		t.Fatalf("%s %dx%d: negative inner size %v", label, int(sz.Width), int(sz.Height), innerSize)
	}
	if innerPos.X+innerSize.Width > eff.Width+0.5 || innerPos.Y+innerSize.Height > eff.Height+0.5 {
		t.Fatalf("%s %dx%d: content overflows the canvas (pos=%v size=%v)", label, int(sz.Width), int(sz.Height), innerPos, innerSize)
	}
	// Every primary destination must stay reachable: with a visible rail the
	// rail's buttons are laid out inside it; in the strip zone the strip
	// carries one button per primary view (TestCompactNavigationReachable
	// pins the inventory itself).
	if zone == navTopStrip && eff.Width >= 240 {
		buttons := 0
		walk := func(o fyne.CanvasObject) {}
		var walkAll func(fyne.CanvasObject)
		walkAll = func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text != "" {
				buttons++
			}
			switch c := o.(type) {
			case *fyne.Container:
				for _, ch := range c.Objects {
					walkAll(ch)
				}
			case *container.Scroll:
				walkAll(c.Content)
			}
			walk(o)
		}
		if scr, ok := sh.topNav.(*container.Scroll); ok {
			walkAll(scr.Content)
		}
		if buttons < len(primaryViews) {
			t.Fatalf("%s %dx%d: compact strip carries %d primary buttons, want >= %d", label, int(sz.Width), int(sz.Height), buttons, len(primaryViews))
		}
	}
	// At every non-degenerate matrix size the primary content area keeps a
	// useful share of the canvas (never squeezed to zero by navigation).
	if eff.Width >= 240 && eff.Height >= 240 && innerSize.Width < 100 {
		t.Fatalf("%s %dx%d (zone %s): main content squeezed to %v — navigation must never consume the viewport", label, int(sz.Width), int(sz.Height), zone, innerSize)
	}
}

// TestResponsiveViewportMatrix walks the FULL mandatory logical-size matrix
// through the real assembled shell in a real window, asserting rendered
// geometry per size.
func TestResponsiveViewportMatrix(t *testing.T) {
	_, sh, w, rc, inner := matrixShell(t)
	for _, sz := range viewportMatrix {
		uiDoLocked(func() { w.Resize(sz) })
		assertMatrixGeometry(t, "matrix", sz, sh, rc, inner)
		drainBackgroundRefreshes()
	}
}

// TestResponsiveBoundaryMatrix covers the breakpoint boundaries (one unit
// below/at/above each of 600, 900, 1280) at three heights.
func TestResponsiveBoundaryMatrix(t *testing.T) {
	_, sh, w, rc, inner := matrixShell(t)
	for _, sz := range boundaryMatrix {
		uiDoLocked(func() { w.Resize(sz) })
		assertMatrixGeometry(t, "boundary", sz, sh, rc, inner)
	}
}

// TestResponsiveDegenerateMatrixInWindow proves the assembled shell neither
// panics nor produces impossible geometry at zero, one and tiny sizes.
func TestResponsiveDegenerateMatrixInWindow(t *testing.T) {
	_, sh, w, rc, inner := matrixShell(t)
	for _, sz := range degenerateMatrix {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("resize to %dx%d panicked the shell: %v", int(sz.Width), int(sz.Height), r)
				}
			}()
			uiDoLocked(func() { w.Resize(sz) })
			assertMatrixGeometry(t, "degenerate", sz, sh, rc, inner)
		}()
	}
}

// TestResizeAcrossClassesPreservesSelection resizes live across adjacent
// size classes while a non-default destination is selected: the selected
// view and the active tab must survive the reflow (no reconstruction, no
// lost state).
func TestResizeAcrossClassesPreservesSelection(t *testing.T) {
	ap, sh, w, _, _ := matrixShell(t)
	uiDoLocked(func() { w.Resize(fyne.NewSize(390, 844)) })
	uiDoLocked(func() { sh.show("settings") })
	for _, sz := range []fyne.Size{
		fyne.NewSize(390, 844),   // compact portrait
		fyne.NewSize(844, 390),   // compact landscape → medium band
		fyne.NewSize(768, 1024),  // tablet
		fyne.NewSize(1366, 768),  // desktop
		fyne.NewSize(2560, 1440), // wide
		fyne.NewSize(320, 568),   // back to compact phone
	} {
		uiDoLocked(func() { w.Resize(sz) })
		if sh.active != "settings" {
			t.Fatalf("resize to %dx%d lost the selected destination (active=%q)", int(sz.Width), int(sz.Height), sh.active)
		}
		if n := len(sh.views); n < 1 {
			t.Fatalf("resize to %dx%d discarded the built view cache", int(sz.Width), int(sz.Height))
		}
	}
	_ = ap
	drainBackgroundRefreshes()
}

// goldenSizes are the 5 representative golden layouts: compact phone
// portrait, compact landscape, medium tablet, desktop, wide.
var goldenSizes = []struct {
	name string
	sz   fyne.Size
}{
	{"compact-portrait-390x844", fyne.NewSize(390, 844)},
	{"compact-landscape-844x390", fyne.NewSize(844, 390)},
	{"medium-tablet-768x1024", fyne.NewSize(768, 1024)},
	{"desktop-1366x768", fyne.NewSize(1366, 768)},
	{"wide-2560x1440", fyne.NewSize(2560, 1440)},
}

// snapshotTree renders the assembled tree as a deterministic, reviewable
// structural snapshot: object type, text, rounded position/size and
// visibility. Font-independent, memory-address-free — stable across
// platforms and runs.
func snapshotTree(o fyne.CanvasObject, depth int) string {
	var b strings.Builder
	writeSnap := func(sb *strings.Builder, o fyne.CanvasObject, d int) {}
	var walk func(fyne.CanvasObject, int)
	walk = func(o fyne.CanvasObject, d int) {
		if o == nil {
			return
		}
		kind := fmt.Sprintf("%T", o)
		text := ""
		switch c := o.(type) {
		case *widget.Button:
			text = " text=" + c.Text
		case *widget.Label:
			text = " text=" + c.Text
		case *widget.RichText:
			text = " segments=" + fmt.Sprint(len(c.Segments))
		}
		vis := "visible"
		if !o.Visible() {
			vis = "hidden"
		}
		pos, sz := o.Position(), o.Size()
		fmt.Fprintf(&b, "%s%s%s pos=(%.0f,%.0f) size=(%.0fx%.0f) %s\n",
			strings.Repeat("  ", d), kind, text, pos.X, pos.Y, sz.Width, sz.Height, vis)
		_ = writeSnap
		switch c := o.(type) {
		case *fyne.Container:
			for _, ch := range c.Objects {
				walk(ch, d+1)
			}
		case *container.Scroll:
			walk(c.Content, d+1)
		}
	}
	walk(o, depth)
	return b.String()
}

// TestResponsiveGoldenSnapshots pins the 5 representative layouts as
// structural goldens (testdata/golden-*.txt). Regenerate deliberately with
// FYNE_UPDATE_GOLDENS=1 go test ./ui/fyne/ -run TestResponsiveGoldenSnapshots.
func TestResponsiveGoldenSnapshots(t *testing.T) {
	update := flag.Lookup("test.v") != nil && os.Getenv("FYNE_UPDATE_GOLDENS") == "1"
	_, sh, w, _, _ := matrixShell(t)
	for _, g := range goldenSizes {
		uiDoLocked(func() {
			w.Resize(g.sz)
			sh.show("chat")
		})
		drainBackgroundRefreshes()
		got := snapshotTree(sh.rail, 0) + "--- topNav ---\n" + snapshotTree(sh.topNav, 0)
		path := "testdata/golden-" + g.name + ".txt"
		if update {
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatalf("update golden %s: %v", path, err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("golden %s missing (run with FYNE_UPDATE_GOLDENS=1 to create): %v", path, err)
		}
		if string(want) != got {
			// Deterministic failure evidence: first divergence.
			wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
			for i := 0; i < len(wl) && i < len(gl); i++ {
				if wl[i] != gl[i] {
					t.Errorf("golden %s diverges at line %d:\n want: %s\n  got: %s", path, i+1, wl[i], gl[i])
					break
				}
			}
			t.Errorf("golden %s mismatch (want %d lines, got %d lines) — layout regression or deliberate change; regenerate with FYNE_UPDATE_GOLDENS=1 after review", path, len(wl), len(gl))
		}
	}
}
