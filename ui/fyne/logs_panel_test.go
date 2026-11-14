//go:build !headless

// logs_panel_test.go — v1.11.1 Logs panel honest-state contract tests:
// loading, empty (ONLY after a successful fetch), error (never rendered
// as "no entries"), refreshed with the resolved directory + sink health,
// and the no-overlapping-requests guard.
package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// newLogsPanelUnderTest builds the Logs panel against a fake core.
func newLogsPanelUnderTest(t *testing.T) (*logsPanel, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	var p *logsPanel
	uiDoLocked(func() {
		_, p = buildLogsPanelFor(ap)
	})
	return p, srv
}

// logsViewText reads the entries view under the UI lock.
func logsViewText(p *logsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.view.Text
}

func logsErrText(p *logsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	if !p.err.Visible() {
		return ""
	}
	return p.err.Text
}

func logsDirText(p *logsPanel) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return p.dirInfo.Text
}

// TestLogsPanelErrorStateIsNeverEmpty: a failed request renders the
// typed ERROR surface — never "(no entries match)". This is the exact
// class the v1.11.1 work order calls out: do not show "no entries" for a
// failed request.
func TestLogsPanelErrorStateIsNeverEmpty(t *testing.T) {
	p, srv := newLogsPanelUnderTest(t)

	srv.logsMux.Lock()
	srv.logsFail = true
	srv.logsMux.Unlock()

	uiDoLocked(func() { p.load() })
	waitForText(t, func() string { return logsErrText(p) }, "logs unavailable")

	if strings.Contains(logsViewText(p), "(no entries match)") {
		t.Fatal("a failed fetch must never render as the empty state")
	}
	if !strings.Contains(logsErrText(p), "logs unavailable") {
		t.Fatalf("error surface = %q", logsErrText(p))
	}
}

// TestLogsPanelSuccessShowsEntriesAndDirectory: a successful fetch
// renders the entries AND the resolved log directory caption.
func TestLogsPanelSuccessShowsEntriesAndDirectory(t *testing.T) {
	p, srv := newLogsPanelUnderTest(t)

	srv.logsMux.Lock()
	srv.logsEntries = []LogEntry{{TS: "2026-11-14 10:00:00", Level: "info", Sub: "ipc", Msg: "core handshake successful"}}
	srv.logsDir = "/tmp/data/logs"
	srv.logsMux.Unlock()

	uiDoLocked(func() { p.load() })
	waitForText(t, func() string { return logsViewText(p) }, "core handshake successful")
	waitForText(t, func() string { return logsDirText(p) }, "Log folder: /tmp/data/logs")

	if strings.Contains(logsErrText(p), "unavailable") {
		t.Fatalf("successful fetch must clear the error surface: %q", logsErrText(p))
	}
}

// TestLogsPanelSinkHealthShown: an unhealthy or fallback sink is NAMED,
// never hidden behind a healthy-looking panel.
func TestLogsPanelSinkHealthShown(t *testing.T) {
	p, srv := newLogsPanelUnderTest(t)

	srv.logsMux.Lock()
	srv.logsDir = "/cache/SHEYTAN-LA/logs"
	srv.logsSink = map[string]any{
		"sinksReady":     false,
		"fallbackReason": "primary log dir unavailable",
		"lastErr":        "disk full",
		"emergencyLines": 3,
	}
	srv.logsMux.Unlock()

	uiDoLocked(func() { p.load() })
	waitForText(t, func() string { return logsDirText(p) }, "FALLBACK ACTIVE")
	waitForText(t, func() string { return logsDirText(p) }, "sink problem")
	waitForText(t, func() string { return logsDirText(p) }, "memory only")
}

// TestLogsPanelEmptyOnlyAfterSuccess: "(no entries match)" appears ONLY
// after a successful fetch that returned nothing.
func TestLogsPanelEmptyOnlyAfterSuccess(t *testing.T) {
	p, _ := newLogsPanelUnderTest(t)

	// Default fake returns zero entries and no failure.
	waitForText(t, func() string { return logsViewText(p) }, "(no entries match)")
	if logsErrText(p) != "" {
		t.Fatalf("empty-but-successful must not show an error: %q", logsErrText(p))
	}
}

// TestLogsPanelNoOverlappingRequests: while one fetch is blocked in
// flight, further Refresh taps are coalesced (exactly one request), and
// the blocked response still renders when it lands.
func TestLogsPanelNoOverlappingRequests(t *testing.T) {
	p, srv := newLogsPanelUnderTest(t)

	block := make(chan struct{})
	srv.logsMux.Lock()
	srv.logsBlock = block
	srv.logsEntries = []LogEntry{{TS: "2026-11-14 11:00:00", Level: "warn", Sub: "updater", Msg: "network denied (non-fatal)"}}
	srv.logsMux.Unlock()

	base := srv.logsGetCount()
	uiDoLocked(func() { p.load() })
	// Rapid double-tap while the first request is blocked.
	uiDoLocked(func() { p.load(); p.load() })

	// Let the in-flight request be served.
	close(block)

	waitForText(t, func() string { return logsViewText(p) }, "network denied")
	if got := srv.logsGetCount() - base; got != 1 {
		t.Fatalf("blocked fetch + 2 refresh taps issued %d requests, want exactly 1 (no overlapping requests)", got)
	}
}
