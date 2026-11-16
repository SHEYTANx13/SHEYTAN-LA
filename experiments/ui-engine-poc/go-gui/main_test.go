package main

// main_test.go — headless app-level POC tests against the pinned Go-Gui
// candidate (v0.96.0). These exercise the framework's own test driver:
// render the full view, type into the composer, click buttons, switch
// navigation, and produce a software-rendered screenshot. What they do
// NOT cover (recorded in UI-ENGINE-DECISION.md): real window-system
// input, accessibility trees, IME, and physical-device behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
)

func newTestApp(t *testing.T) (*pocState, *gui.Window) {
	t.Helper()
	gui.SetTheme(gui.ThemeLight.WithPadding(false))
	app := newPOCState()
	w := gui.NewTestWindow(t, gui.WindowCfg{
		State:  app,
		Width:  windowW,
		Height: windowH,
	})
	w.TestRender(mainView)
	return app, w
}

// TestComposerSendAppendsMessage types into the composer and clicks Send,
// asserting the message list actually grew and the draft cleared — the
// wired-behavior test pattern from the framework's own todo example.
func TestComposerSendAppendsMessage(t *testing.T) {
	app, w := newTestApp(t)
	before := len(app.Messages)

	if err := w.TestType(composerID, "hello from the POC test"); err != nil {
		t.Fatalf("TestType: %v", err)
	}
	if app.Draft != "hello from the POC test" {
		t.Fatalf("draft %q, want the typed text", app.Draft)
	}
	if err := w.TestClick(sendID); err != nil {
		t.Fatalf("TestClick(send): %v", err)
	}

	if len(app.Messages) != before+1 {
		t.Fatalf("messages %d, want %d (send must append)", len(app.Messages), before+1)
	}
	got := app.Messages[len(app.Messages)-1]
	if got.Role != "user" || got.Content != "hello from the POC test" {
		t.Fatalf("appended message = %+v, want the typed user message", got)
	}
	if app.Draft != "" {
		t.Fatalf("draft %q after send, want empty", app.Draft)
	}
}

// TestNavSwitchSwapsSurface clicks the table navigation and re-renders:
// both surfaces must render without panic and the state must follow.
func TestNavSwitchSwapsSurface(t *testing.T) {
	app, w := newTestApp(t)

	if app.Nav != "chat" {
		t.Fatalf("initial nav = %q, want chat", app.Nav)
	}
	if err := w.TestClick(navTableID); err != nil {
		t.Fatalf("TestClick(nav-table): %v", err)
	}
	if app.Nav != "table" {
		t.Fatalf("nav after click = %q, want table", app.Nav)
	}
	w.TestRender(mainView) // table surface renders

	if err := w.TestClick(navChatID); err != nil {
		t.Fatalf("TestClick(nav-chat): %v", err)
	}
	if app.Nav != "chat" {
		t.Fatalf("nav after back = %q, want chat", app.Nav)
	}
	w.TestRender(mainView) // chat surface renders again
}

// TestMixedDirectionContentSurvivesStateRoundTrip pins the
// mixed-direction slice: the Persian/Arabic + LTR fixture messages must
// round-trip through render without mutation.
func TestMixedDirectionContentSurvivesStateRoundTrip(t *testing.T) {
	app, w := newTestApp(t)
	want := append([]chatMessage(nil), app.Messages...)
	w.TestRender(mainView)
	w.TestRender(mainView)

	for i := range want {
		if app.Messages[i].Content != want[i].Content {
			t.Fatalf("message %d mutated by render:\n got %q\nwant %q",
				i, app.Messages[i].Content, want[i].Content)
		}
	}
	if !strings.Contains(app.Messages[0].Content, "سلام") {
		t.Fatal("fixture lost its Persian content")
	}
}

// TestScreenshotRender produces a software-rendered PNG of the full slice
// and asserts it is a non-trivial image — the pixel-level evidence for
// UI-ENGINE-POC-RESULTS.md.
func TestScreenshotRender(t *testing.T) {
	_, w := newTestApp(t)

	path := filepath.Join(t.TempDir(), "poc-slice.png")
	if err := soft.RenderToPNG(w, 2, path); err != nil {
		t.Fatalf("RenderToPNG: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("screenshot missing: %v", err)
	}
	if fi.Size() < 10_000 {
		t.Fatalf("screenshot is %d bytes — too small to be a real render of the slice", fi.Size())
	}
}

// TestInspectorInputAcceptsText exercises the second focusable input
// (keyboard entry in the split pane's inspector). TestType inserts at the
// caret like real typing — it does not replace existing text — so the
// assertion checks the typed text arrived in the state.
func TestInspectorInputAcceptsText(t *testing.T) {
	app, w := newTestApp(t)

	if err := w.TestType(inspectorInputID, "note after render: "); err != nil {
		t.Fatalf("TestType(inspector): %v", err)
	}
	if !strings.Contains(app.InspectorNote, "note after render: ") {
		t.Fatalf("inspector note = %q, want it to contain the typed text", app.InspectorNote)
	}
}
