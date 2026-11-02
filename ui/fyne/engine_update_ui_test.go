//go:build !headless

// engine_update_ui_test.go — v1.11.1: the Models panel's Engine updates
// surface. The contract under test:
//
//   - Check now renders the TRUTHFUL official-release result (installed
//     tag, latest tag, exact asset, status class) and arms the install
//     action ONLY on update-available;
//   - a failed check renders the failure — never a fabricated "up to
//     date";
//   - the install is single-flight, renders the live installing state
//     with progress while the transaction runs, and lands the terminal
//     state (installed) with the action disarmed;
//   - every network call runs off the UI thread (uiDo renders only).
package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// newEngineUpdatePanelUnderTest builds the Models panel against a fake
// core with the install poll cadence tightened for determinism.
func newEngineUpdatePanelUnderTest(t *testing.T) (*modelsPanel, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())
	prev := engineUpdatePollInterval
	engineUpdatePollInterval = 2 * time.Millisecond
	t.Cleanup(func() { engineUpdatePollInterval = prev })

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *modelsPanel
	uiDoLocked(func() { _, p = buildModelsPanelFor(ap) })
	return p, srv
}

func updLabelText(p *modelsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.updLabel.Text
}

func updInstallDisabled(p *modelsPanel) bool {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.updInstallB.Disabled()
}

// waitForUpdText waits (bounded, mutex-yielding) for the update label to
// contain the wanted substring.
func waitForUpdText(t *testing.T, p *modelsPanel, substr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if containsStr(updLabelText(p), substr) {
			return
		}
		uiTestMu.Lock()
		uiTestMu.Unlock()
	}
	t.Fatalf("timed out waiting for %q in update label %q", substr, updLabelText(p))
}

func containsStr(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// TestEngineUpdateCheckRendersTruthfulResult: the check renders the
// official resolution honestly and arms the install action only on
// update-available.
func TestEngineUpdateCheckRendersTruthfulResult(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.setEngineUpdateCheck("update-available", "b12031", "b12032", nil)

	if !updInstallDisabled(p) {
		t.Fatal("the install action must start disarmed (no check yet)")
	}
	uiDoLocked(func() { test.Tap(p.updCheckB) })

	waitForUpdText(t, p, "update-available")
	waitForUpdText(t, p, "b12031")
	waitForUpdText(t, p, "b12032")
	waitForUpdText(t, p, "llama-b12032-bin-win-cpu-x64.zip")
	waitForUpdText(t, p, "ggml-org/llama.cpp")
	if updInstallDisabled(p) {
		t.Fatal("an update-available check must arm the install action")
	}
}

// TestEngineUpdateCheckCurrentDisarmsInstall: a current engine offers no
// install action (never a pointless engine restart).
func TestEngineUpdateCheckCurrentDisarmsInstall(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.setEngineUpdateCheck("current", "b12032", "", nil)

	uiDoLocked(func() { test.Tap(p.updCheckB) })
	waitForUpdText(t, p, "Status: current")
	if !updInstallDisabled(p) {
		t.Fatal("a current engine must not offer the install action")
	}
}

// TestEngineUpdateCheckFailureIsNeverUpToDate: a failed check renders the
// failure honestly — never a fabricated "up to date".
func TestEngineUpdateCheckFailureIsNeverUpToDate(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.engineUpdMux.Lock()
	srv.engineUpdFail = true
	srv.engineUpdMux.Unlock()

	uiDoLocked(func() { test.Tap(p.updCheckB) })
	waitForUpdText(t, p, "check failed")
	if txt := updLabelText(p); containsStr(txt, "up to date") || containsStr(txt, "Status: current") {
		t.Fatalf("a failed check must never render an up-to-date verdict: %q", txt)
	}
	if !updInstallDisabled(p) {
		t.Fatal("a failed check must leave the install action disarmed")
	}
}

// TestEngineUpdateInstallSingleFlightLiveAndTerminal: the install starts
// EXACTLY one POST, renders the live installing state with progress, and
// lands the terminal status with the action disarmed.
func TestEngineUpdateInstallSingleFlightLiveAndTerminal(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.setEngineUpdateCheck("update-available", "b12031", "b12032", nil)

	uiDoLocked(func() { test.Tap(p.updCheckB) })
	waitForUpdText(t, p, "update-available")
	if updInstallDisabled(p) {
		t.Fatal("fixture failed to arm the install action")
	}

	// While installing: the live status + progress render.
	srv.setEngineUpdateCheck("installing", "b12031", "b12032", map[string]any{
		"phase": "downloading", "bytes": 52428800, "total": 104857600,
	})
	uiDoLocked(func() { test.Tap(p.updInstallB) })
	waitForUpdText(t, p, "installing")
	waitForUpdText(t, p, "Progress: downloading")

	// Single-flight: a DIRECT second invocation while polling is a no-op.
	uiDoLocked(func() { p.installEngineUpdate() })
	if n := srv.engineUpdatePOSTs(); n != 1 {
		t.Fatalf("exactly one engine-update POST must run, got %d", n)
	}

	// Terminal: the installed status lands and the action disarms.
	srv.setEngineUpdateCheck("installed", "b12032", "b12032", nil)
	waitForUpdText(t, p, "Status: installed")
	if !updInstallDisabled(p) {
		t.Fatal("the terminal installed status must disarm the install action")
	}
	if n := srv.engineUpdatePOSTs(); n != 1 {
		t.Fatalf("exactly one engine-update POST must have run in total, got %d", n)
	}
}

// TestEngineUpdateOfflineIsItsOwnClass: the honest offline status renders
// as offline — never "no asset", never "current".
func TestEngineUpdateOfflineIsItsOwnClass(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.setEngineUpdateCheck("offline", "b12031", "", nil)

	uiDoLocked(func() { test.Tap(p.updCheckB) })
	waitForUpdText(t, p, "Status: offline")
	if !updInstallDisabled(p) {
		t.Fatal("an offline check must leave the install action disarmed")
	}
}

// TestEngineUpdateNoAssetIsExplained: the no-asset class renders with its
// honest explanation (a reachable list without a consumable asset).
func TestEngineUpdateNoAssetIsExplained(t *testing.T) {
	p, srv := newEngineUpdatePanelUnderTest(t)
	srv.setEngineUpdateCheck("no-asset", "b12031", "", nil)

	uiDoLocked(func() { test.Tap(p.updCheckB) })
	waitForUpdText(t, p, "Status: no-asset")
	if !updInstallDisabled(p) {
		t.Fatal("a no-asset check must leave the install action disarmed")
	}
}
