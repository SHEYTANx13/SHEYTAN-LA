//go:build !headless

// models_panel_test.go — v1.11.1 non-destructive model-selection flow:
// browsing never starts an engine transaction; the explicit "Use this
// model" action is single-flight, renders the backend's LIVE phases while
// the POST runs, re-enables on every terminal path, and a late poll can
// never overwrite the terminal state. The fake core drives deterministic
// phase progression (no sleeps: bounded mutex-spin waits).
package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// newModelsPanelUnderTest builds the panel against a fake core with the
// poll cadence tightened for determinism.
func newModelsPanelUnderTest(t *testing.T) (*modelsPanel, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())
	prev := selectionPollInterval
	selectionPollInterval = 2 * time.Millisecond
	t.Cleanup(func() { selectionPollInterval = prev })

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *modelsPanel
	uiDoLocked(func() { _, p = buildModelsPanelFor(ap) })
	waitForText(t, func() string { return selStatusText(p) }, "Selection phase")
	return p, srv
}

// selStatusText reads the status line under the UI lock.
func selStatusText(p *modelsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.sel.Text
}

// useDisabled reads the Use button state under the UI lock.
func useDisabled(p *modelsPanel) bool {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.useB.Disabled()
}

// detailText reads the highlighted-row summary under the UI lock.
func detailText(p *modelsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.detail.Text
}

// waitForText spins (bounded, mutex-yielding — the same discipline as
// drainBackgroundRefreshes) until the read function's text contains the
// wanted substring.
func waitForText(t *testing.T, read func() string, substr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(read(), substr) {
			return
		}
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
	t.Fatalf("timed out waiting for %q in %q", substr, read())
}

// waitForPosts spins (bounded) until the fake reports >= want selection POSTs.
func waitForPosts(t *testing.T, f *fakeCore, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.selectPOSTs() >= want {
			return
		}
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
	t.Fatalf("timed out waiting for %d selection POSTs (got %d)", want, f.selectPOSTs())
}

// TestModelRowSelectIsNonDestructive: highlighting a row shows its facts
// and arms the action — but NEVER issues a selection POST.
func TestModelRowSelectIsNonDestructive(t *testing.T) {
	p, srv := newModelsPanelUnderTest(t)

	uiDoLocked(func() { p.highlightRow(0) })
	if txt := detailText(p); !strings.Contains(txt, "Q4_K_M") || !strings.Contains(txt, "llama") {
		t.Fatalf("highlighted-row summary must carry the already-available facts, got %q", txt)
	}
	if !strings.Contains(detailText(p), "context limit 32768") {
		t.Fatalf("highlighted-row summary must carry context metadata, got %q", detailText(p))
	}
	if useDisabled(p) {
		t.Fatal("the explicit Use-this-model action must be enabled once a row is highlighted")
	}
	if n := srv.selectPOSTs(); n != 0 {
		t.Fatalf("row highlight must not start an engine transaction — %d POSTs observed", n)
	}
}

// TestUseModelSingleFlightLivePhasesTerminal: the explicit action starts
// EXACTLY one POST; while it runs the live phases render, a second
// invocation is rejected, and the terminal state re-enables the controls.
func TestUseModelSingleFlightLivePhasesTerminal(t *testing.T) {
	p, srv := newModelsPanelUnderTest(t)

	srv.selectMux.Lock()
	block := make(chan struct{})
	srv.selectBlock = block
	srv.selectMux.Unlock()

	uiDoLocked(func() {
		p.highlightRow(0)
		test.Tap(p.useB)
	})
	waitForPosts(t, srv, 1)

	// In flight: the action is disabled and a DIRECT second invocation is
	// a no-op (single-flight guard, not just widget state).
	if !useDisabled(p) {
		t.Fatal("the Use-this-model action must be disabled while a selection is in flight")
	}
	uiDoLocked(func() { p.useModel() })
	if n := srv.selectPOSTs(); n != 1 {
		t.Fatalf("second invocation during flight must be rejected — %d POSTs observed", n)
	}

	// The backend's LIVE phases render while the POST runs.
	srv.setSelectPhase("analyzing")
	waitForText(t, func() string { return selStatusText(p) }, "analyzing")
	srv.setSelectPhase("loading")
	waitForText(t, func() string { return selStatusText(p) }, "loading")

	// Release the POST: the terminal state lands and the controls return
	// to a usable state.
	srv.setSelectPhase("ready")
	close(block)
	waitForText(t, func() string { return selStatusText(p) }, "Selection phase: ready — model-a.gguf")
	waitForText(t, func() string {
		if useDisabled(p) {
			return "disabled"
		}
		return "enabled"
	}, "enabled")
	if n := srv.selectPOSTs(); n != 1 {
		t.Fatalf("exactly one POST must have run — %d observed", n)
	}

	// Stale-response guard: a late phase change after the terminal render
	// can never repaint an intermediate phase over it.
	srv.setSelectPhase("configuring")
	for i := 0; i < 50; i++ { // bounded drain: let any trailing poll tick fire
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
	if txt := selStatusText(p); !strings.Contains(txt, "Selection phase: ready") {
		t.Fatalf("a late poll overwrote the terminal state: %q", txt)
	}
}

// TestUseModelFailureKeepsPreviousAndReenables: a failed selection shows
// the honest reason, keeps the previous model active, and returns the UI
// to a usable state.
func TestUseModelFailureKeepsPreviousAndReenables(t *testing.T) {
	p, srv := newModelsPanelUnderTest(t)

	srv.selectMux.Lock()
	srv.selectFail = "engine boom"
	srv.selectMux.Unlock()

	uiDoLocked(func() {
		p.highlightRow(0)
		test.Tap(p.useB)
	})
	waitForPosts(t, srv, 1)
	waitForText(t, func() string { return selStatusText(p) }, "Selection failed")
	if txt := selStatusText(p); !strings.Contains(txt, "previous model stays active") {
		t.Fatalf("the failure line must state that the previous model stays active, got %q", txt)
	}
	waitForText(t, func() string {
		if useDisabled(p) {
			return "disabled"
		}
		return "enabled"
	}, "enabled")
	uiTestMu.Lock()
	errVisible := p.err.Visible()
	errText := p.err.Text
	uiTestMu.Unlock()
	if !errVisible || !strings.Contains(errText, "engine boom") {
		t.Fatalf("the failure reason must be shown, got visible=%v text=%q", errVisible, errText)
	}
}

// TestAlreadyServingSelectionNeedsNoAction: the already-SERVING current
// selection offers no action (the core short-circuits it; the button
// avoids the pointless round-trip). A different model stays actionable.
func TestAlreadyServingSelectionNeedsNoAction(t *testing.T) {
	t.Cleanup(SetTestUIMarshal())
	prev := selectionPollInterval
	selectionPollInterval = 2 * time.Millisecond
	t.Cleanup(func() { selectionPollInterval = prev })

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	srv.lastSelectModel = "model-b.gguf" // the serving model is the current selection

	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *modelsPanel
	uiDoLocked(func() { _, p = buildModelsPanelFor(ap) })
	waitForText(t, func() string { return selStatusText(p) }, "model-b.gguf")

	uiDoLocked(func() { p.highlightRow(1) }) // model-b: serving + selected
	if !useDisabled(p) {
		t.Fatal("the serving current selection must not offer a redundant Use action")
	}
	uiDoLocked(func() { p.highlightRow(0) }) // model-a: not serving
	if useDisabled(p) {
		t.Fatal("a different model must stay actionable")
	}
	if n := srv.selectPOSTs(); n != 0 {
		t.Fatalf("no POST expected in this scenario, got %d", n)
	}
}

// TestTruthfulRowMarkers: the row markers render SEPARATE truths — SERVING
// (engine-loaded) and SELECTED (authoritative selection) never merge.
func TestTruthfulRowMarkers(t *testing.T) {
	p, _ := newModelsPanelUnderTest(t)

	uiTestMu.Lock()
	p.current = SelectionState{Phase: "ready", Model: "model-a.gguf"}
	uiTestMu.Unlock()

	// Row 0: selected (authoritative) but NOT serving → SELECTED marker.
	if name0 := rowName(p, 0); !strings.Contains(name0, "SELECTED") || strings.Contains(name0, "ACTIVE") {
		t.Fatalf("selected-but-not-serving row must show only the SELECTED marker, got %q", name0)
	}
	// Row 1: serving but NOT the current selection → ACTIVE marker only.
	if name1 := rowName(p, 1); !strings.Contains(name1, "ACTIVE") || strings.Contains(name1, "SELECTED") {
		t.Fatalf("serving-but-not-selected row must show only the ACTIVE marker, got %q", name1)
	}
}

// rowName renders one row through the panel's own renderer (the exact
// code path the widget list drives) and returns the rendered name label.
func rowName(p *modelsPanel, i int) string {
	name := widget.NewLabel("")
	detail := widget.NewLabel("")
	box := container.NewVBox(name, detail)
	uiDoLocked(func() { p.renderModelRow(widget.ListItemID(i), box) })
	return name.Text
}

// TestUseModelGuardEmptyState: with no highlighted row (or an out-of-range
// row) the action never fires — the panel starts disabled and the guard
// holds even for direct invocations.
func TestUseModelGuardEmptyState(t *testing.T) {
	p, srv := newModelsPanelUnderTest(t)

	if !useDisabled(p) {
		t.Fatal("the action must start disabled (no row highlighted)")
	}
	uiDoLocked(func() { p.useModel() })
	if n := srv.selectPOSTs(); n != 0 {
		t.Fatalf("no row highlighted — no POST expected, got %d", n)
	}
	uiTestMu.Lock()
	p.row = 99 // out of range
	uiTestMu.Unlock()
	uiDoLocked(func() { p.useModel() })
	if n := srv.selectPOSTs(); n != 0 {
		t.Fatalf("out-of-range row — no POST expected, got %d", n)
	}
}
