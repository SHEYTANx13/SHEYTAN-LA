//go:build !headless

// shell_status_test.go — v1.11.1 connection-state contract: the shell
// status surface shows Connecting / Connected / Disconnected, an
// actionable error, and a safe retry action (visible ONLY when
// disconnected).
package fyneui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func newShellUnderTest(t *testing.T) (*shell, *App, *fakeCore) {
	t.Helper()
	t.Cleanup(SetTestUIMarshal())

	srv := newFakeCore(t)
	t.Cleanup(srv.Close)
	c := srv.client()
	ap := NewApp(test.NewApp(), c)
	t.Cleanup(func() { ap.rt.Shutdown() })

	s := newShell(ap)
	// Attach like App.Run does (bootstrap reaches the shell through
	// ap.shell; the detached shell would nil-deref exactly like a
	// production wiring regression).
	ap.shell = s
	uiDoLocked(func() { s.root() })
	return s, ap, srv
}

func shellStatusText(s *shell) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return s.status.Text
}

func shellRetryVisible(s *shell) bool {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	return s.retryB.Visible()
}

func shellErrLineText(s *shell) string {
	uiTestMu.Lock()
	defer uiTestMu.Unlock()
	if !s.errLine.Visible() {
		return ""
	}
	return s.errLine.Text
}

// TestShellConnectionStates covers the full Connecting → Connected and
// Connecting → Disconnected transitions with the retry surface rules.
func TestShellConnectionStates(t *testing.T) {
	s, _, _ := newShellUnderTest(t)

	// Connecting: explicit state, retry hidden (a bootstrap is in flight).
	uiDoLocked(func() { s.setConnecting() })
	if !strings.Contains(shellStatusText(s), "connecting") {
		t.Fatalf("status = %q, want the explicit connecting state", shellStatusText(s))
	}
	if shellRetryVisible(s) {
		t.Fatal("retry must be hidden while connecting (a bootstrap is already in flight)")
	}

	// Connected: retry hidden, no error line.
	uiDoLocked(func() { s.setConnected() })
	if !strings.Contains(shellStatusText(s), "connected") {
		t.Fatalf("status = %q, want connected", shellStatusText(s))
	}
	if shellRetryVisible(s) {
		t.Fatal("retry must be hidden while connected")
	}
	if shellErrLineText(s) != "" {
		t.Fatalf("connected state must clear the error line, got %q", shellErrLineText(s))
	}

	// Disconnected with a typed error: actionable text + visible retry.
	boom := errors.New("dial unix /missing/core.sock: connect: no such file or directory")
	uiDoLocked(func() { s.setBootError(boom) })
	if !strings.Contains(shellStatusText(s), "disconnected") {
		t.Fatalf("status = %q, want disconnected", shellStatusText(s))
	}
	if !shellRetryVisible(s) {
		t.Fatal("retry must be visible in the disconnected state")
	}
	errText := shellErrLineText(s)
	if !strings.Contains(errText, "no such file or directory") {
		t.Fatalf("error line must carry the actionable error, got %q", errText)
	}
	if !strings.Contains(errText, "Retry") {
		t.Fatalf("error line must point at the retry action, got %q", errText)
	}
}

// TestShellRetryBootstrapReconnects: the retry path re-runs the real
// bootstrap against the fake core and lands in the connected state.
func TestShellRetryBootstrapReconnects(t *testing.T) {
	s, ap, _ := newShellUnderTest(t)

	uiDoLocked(func() { s.setBootError(errors.New("simulated first-boot failure")) })
	if !shellRetryVisible(s) {
		t.Fatal("retry must be visible before the retry attempt")
	}

	// The production retry entry point (the shell's button calls exactly
	// this): single-flight, off the UI goroutine.
	ap.retryBootstrap()

	// Bounded wait for the connected state (handshake + session refresh
	// against the fake core). "connected" is a substring of
	// "disconnected", so the wait checks the leading state word.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.HasPrefix(shellStatusText(s), "connected") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if status := shellStatusText(s); !strings.HasPrefix(status, "connected") {
		t.Fatalf("retry bootstrap did not reach connected: status = %q", status)
	}
	if shellRetryVisible(s) {
		t.Fatal("retry must hide itself after a successful reconnect")
	}
}
