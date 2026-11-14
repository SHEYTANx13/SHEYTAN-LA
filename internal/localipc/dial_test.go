package localipc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// --- v1.11.1 dial contract tests (the canonical DialContext) ---
//
// Neutral contract tests run on EVERY supported platform; the
// platform-specific refusal/deadline/busy shapes live in
// dial_unix_test.go (Linux/macOS fixtures) and dial_windows_test.go (real
// named-pipe busy conditions).

// TestDialContextAlreadyCanceled proves an already-canceled context is
// refused BEFORE any dial is attempted (the endpoint path never needs to
// exist for this to pass).
func TestDialContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	endpoint := `\\.\pipe\sheytan-dial-contract-unreachable`
	if runtime.GOOS != "windows" {
		endpoint = filepath.Join(t.TempDir(), "never-created.sock")
	}

	_, err := DialContext(ctx, endpoint)
	if err == nil {
		t.Fatal("canceled context must fail the dial")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dial error = %v, want context.Canceled", err)
	}
}

// TestDialContextLiveConnectionRoundTrip drives the canonical dial against
// the REAL core over the REAL localipc.Serve endpoint: an authenticated
// handshake request and one core read must round-trip through the
// connection DialContext returned.
func TestDialContextLiveConnectionRoundTrip(t *testing.T) {
	srv, cfg := newCore(t)
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	ipc := startIPC(t, srv, cfg, key)

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return DialContext(ctx, ipc.Endpoint())
			},
		},
		Timeout: 10 * time.Second,
	}

	req, _ := http.NewRequest(http.MethodGet, "http://core/api/ipc/handshake", nil)
	req.Header.Set(HeaderKey, key)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("handshake over DialContext: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handshake status = %d, want 200", resp.StatusCode)
	}

	// One real core read through the same dialed transport.
	resp2, err := client.Get("http://core/api/config")
	if err != nil {
		t.Fatalf("core read over DialContext: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("config status = %d, want 200", resp2.StatusCode)
	}
}

// TestDialContextUnsupportedPlatformRefusesTyped proves the typed refusal
// exists and is never a nil error or a panic (the guard itself is
// GOOS-conditional; this pins the contract's shape for the platforms that
// exercise it via DialContext's switch).
func TestDialContextUnsupportedPlatformRefusesTyped(t *testing.T) {
	if ErrUnsupportedTransport == nil {
		t.Fatal("ErrUnsupportedTransport must be a non-nil typed error")
	}
	// The supported platforms must not be refused.
	switch runtime.GOOS {
	case "windows", "linux", "darwin":
		// DialContext dispatches to a real transport here; exercised by the
		// tests above.
	default:
		_, err := DialContext(context.Background(), "unused")
		if !errors.Is(err, ErrUnsupportedTransport) {
			t.Fatalf("unsupported platform error = %v, want ErrUnsupportedTransport", err)
		}
	}
}

// TestDialContextPreservesOSProbeSemantics keeps the stale-endpoint probe
// (cleanStaleEndpoint) honest: dialing a live endpoint through the
// canonical dial must succeed, proving the probe still distinguishes live
// from stale after the dial refactor. (Unix fixture; named pipes vanish
// with the last handle on Windows.)
func TestDialContextPreservesOSProbeSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("probe is a unix-only concept; named pipes vanish with the last handle")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "probe.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	conn, err := DialContext(context.Background(), path)
	if err != nil {
		t.Fatalf("live probe dial: %v", err)
	}
	_ = conn.Close()

	// And a stale path (no listener behind the file) is reported missing.
	stale := filepath.Join(dir, "stale.sock")
	if f, err := os.Create(stale); err == nil {
		_ = f.Close()
	}
	removed, err := cleanStaleEndpoint(stale)
	if err != nil || !removed {
		t.Fatalf("cleanStaleEndpoint(stale) = (%v, %v), want (true, nil)", removed, err)
	}
}
