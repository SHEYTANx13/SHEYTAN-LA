package main

// responsive_test.go — v1.11.1 hybrid-native-UI milestone (work order
// §4.4.5): the POC slice rendered through the pinned Go-Gui candidate's
// headless soft backend at the work order's mandatory logical-size matrix
// (1280x720, 1024x768, 800x600, 640x480), a high-DPI proxy render (2x
// sample scale — a sampling parameter of the soft renderer, NOT a claim
// about real device scaling), and a window close/reopen cycle (fresh
// window, re-render, state intact).
//
// Honest limits (mirrored in docs/architecture/UI-PLATFORM-AUDIT.md): the
// soft backend renders; it does not exercise a real window system, real
// input, IME or accessibility. Containment here means: every matrix render
// completes without panic on both surfaces, the application state survives
// the render round-trip unmutated, typing focus works at the smallest
// mandated size, and each screenshot is a non-trivial real render.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
)

// responsiveMatrix is the work order §4.4.5 size set exactly.
var responsiveMatrix = [][2]int{
	{1280, 720},
	{1024, 768},
	{800, 600},
	{640, 480},
}

// renderBothSurfaces renders the chat surface, switches to the table
// surface, and renders that — both must complete without panic at the
// current window size.
func renderBothSurfaces(t *testing.T, w *gui.Window) {
	t.Helper()
	w.TestRender(mainView) // chat surface (initial nav)
	if err := w.TestClick(navTableID); err != nil {
		t.Fatalf("TestClick(nav-table) at size: %v", err)
	}
	w.TestRender(mainView) // table surface
	if err := w.TestClick(navChatID); err != nil {
		t.Fatalf("TestClick(nav-chat) back at size: %v", err)
	}
	w.TestRender(mainView) // chat surface again
}

// TestResponsiveMatrixRendersContained drives the full slice at every
// mandated size: both surfaces render, navigation toggles, and the state
// round-trips without mutation.
func TestResponsiveMatrixRendersContained(t *testing.T) {
	for _, sz := range responsiveMatrix {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			gui.SetTheme(gui.ThemeLight.WithPadding(false))
			app := newPOCState()
			before := len(app.Messages)
			w := gui.NewTestWindow(t, gui.WindowCfg{
				State:  app,
				Width:  sz[0],
				Height: sz[1],
			})
			renderBothSurfaces(t, w)
			if len(app.Messages) != before {
				t.Fatalf("render mutated message count at %dx%d: %d -> %d", sz[0], sz[1], before, len(app.Messages))
			}
			if !strings.Contains(app.Messages[0].Content, "سلام") {
				t.Fatalf("RTL fixture lost its Persian content at %dx%d", sz[0], sz[1])
			}
		})
	}
}

// TestScreenshotPerSize produces a software-rendered PNG at every mandated
// size and asserts each is a non-trivial real render — the pixel-level
// containment evidence for UI-ENGINE-POC-RESULTS.md.
func TestScreenshotPerSize(t *testing.T) {
	for _, sz := range responsiveMatrix {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			gui.SetTheme(gui.ThemeLight.WithPadding(false))
			app := newPOCState()
			w := gui.NewTestWindow(t, gui.WindowCfg{State: app, Width: sz[0], Height: sz[1]})
			w.TestRender(mainView)
			path := filepath.Join(t.TempDir(), fmt.Sprintf("poc-%dx%d.png", sz[0], sz[1]))
			if err := soft.RenderToPNG(w, 1, path); err != nil {
				t.Fatalf("RenderToPNG at %dx%d: %v", sz[0], sz[1], err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatalf("screenshot missing at %dx%d: %v", sz[0], sz[1], err)
			}
			if fi.Size() < 10_000 {
				t.Fatalf("screenshot at %dx%d is %d bytes — too small to be a real render of the slice", sz[0], sz[1], fi.Size())
			}
		})
	}
}

// TestHighDPIProxyRender renders at 1280x720 with the soft renderer's 2x
// sample scale. This is a high-DPI *sampling proxy only* — it is not
// evidence about real device pixel-density behavior, IME or text scaling
// on a physical display (NOT RUN, recorded in UI-PLATFORM-AUDIT.md).
func TestHighDPIProxyRender(t *testing.T) {
	gui.SetTheme(gui.ThemeLight.WithPadding(false))
	app := newPOCState()
	w := gui.NewTestWindow(t, gui.WindowCfg{State: app, Width: 1280, Height: 720})
	w.TestRender(mainView)
	path := filepath.Join(t.TempDir(), "poc-1280x720-at2x.png")
	if err := soft.RenderToPNG(w, 2, path); err != nil {
		t.Fatalf("RenderToPNG 2x: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("2x screenshot missing: %v", err)
	}
	// A 2x render of the full slice must be substantially larger than the
	// 1x render of the same layout.
	oneX := filepath.Join(t.TempDir(), "poc-1280x720-at1x.png")
	if err := soft.RenderToPNG(w, 1, oneX); err != nil {
		t.Fatalf("RenderToPNG 1x: %v", err)
	}
	oneXStat, err := os.Stat(oneX)
	if err != nil {
		t.Fatalf("1x screenshot missing: %v", err)
	}
	if fi.Size() <= oneXStat.Size() {
		t.Fatalf("2x render (%d bytes) did not exceed the 1x render (%d bytes) — not a real scaled sample", fi.Size(), oneXStat.Size())
	}
}

// TestWindowCloseReopen opens the slice in one window, then opens a FRESH
// window over a fresh state — the reopen cycle must leave a fully usable
// workspace (new state, both surfaces render, focus lands). Harness limit:
// the test harness closes windows via t.Cleanup; an explicit interactive
// close click is NOT exercised here (recorded in UI-PLATFORM-AUDIT.md).
func TestWindowCloseReopen(t *testing.T) {
	gui.SetTheme(gui.ThemeLight.WithPadding(false))
	first := newPOCState()
	w1 := gui.NewTestWindow(t, gui.WindowCfg{State: first, Width: 1024, Height: 768})
	w1.TestRender(mainView)

	second := newPOCState()
	w2 := gui.NewTestWindow(t, gui.WindowCfg{State: second, Width: 800, Height: 600})
	w2.TestRender(mainView)
	if err := w2.TestType(composerID, "after reopen"); err != nil {
		t.Fatalf("typing after reopen: %v", err)
	}
	if !strings.Contains(second.Draft, "after reopen") {
		t.Fatalf("draft after reopen = %q, want it to contain the typed text", second.Draft)
	}
	renderBothSurfaces(t, w2)
}

// TestComposerFocusAtSmallestMandatedSize pins that typing focus works at
// the smallest mandated size (640x480) — the work order's "keyboard focus"
// requirement at the containment floor.
func TestComposerFocusAtSmallestMandatedSize(t *testing.T) {
	gui.SetTheme(gui.ThemeLight.WithPadding(false))
	app := newPOCState()
	w := gui.NewTestWindow(t, gui.WindowCfg{State: app, Width: 640, Height: 480})
	w.TestRender(mainView)
	if err := w.TestType(composerID, "typed at 640x480"); err != nil {
		t.Fatalf("typing at 640x480: %v", err)
	}
	if !strings.Contains(app.Draft, "typed at 640x480") {
		t.Fatalf("draft at 640x480 = %q, want the typed text", app.Draft)
	}
	if err := w.TestClick(sendID); err != nil {
		t.Fatalf("send at 640x480: %v", err)
	}
	if len(app.Messages) != 4 {
		t.Fatalf("messages after send at 640x480 = %d, want 4", len(app.Messages))
	}
	if app.Draft != "" {
		t.Fatalf("draft after send at 640x480 = %q, want empty", app.Draft)
	}
}
