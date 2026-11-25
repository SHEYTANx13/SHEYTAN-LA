//go:build windows && !headless

// ipc_mismatched_windows_test.go — v1.11.1 coverage completion: the
// mismatched-protocol refusal on the Windows lane.
//
// The REAL transport is proven over the named pipe by
// TestCoreClientEndToEndOverWindowsNamedPipe; the mismatch VALIDATION is
// client-side logic identical on every platform. The real
// localipc.Serve takes the concrete *api.Server and cannot host a fake
// handshake, so this test drives the validation through the documented
// TCP test seam (NewCoreClientForTest) — the seam the fake-core UI
// contract tests own. The Unix twin (TestCoreClientRefusesMismatchedProtocol)
// proves the same refusal over a real socket.
package coreclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCoreClientRefusesMismatchedProtocolWindows(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ipc/handshake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":"something-else","protocolVersion":99,"coreVersion":"0.0.0","authenticated":true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewCoreClientForTest(strings.TrimPrefix(srv.URL, "http://"), "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.Handshake(ctx)
	if err == nil {
		t.Fatal("mismatched protocol must be refused")
	}
	if !strings.Contains(err.Error(), "mismatch") && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refusal should name the protocol mismatch, got: %v", err)
	}
}
