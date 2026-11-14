//go:build windows

// dial_windows_test.go — v1.11.1 Windows IPC repair: the canonical
// context-aware DialContext against the REAL named-pipe transport.
//
// Evidence classes (kept honest, same discipline as
// listener_windows_test.go):
//   - a canceled context never touches the pipe;
//   - a missing pipe surfaces the real CreateFile error;
//   - a caller deadline tighter than the busy-dial budget bounds a dial
//     against a genuinely saturated pipe (single instance, held busy by
//     another client, no refill) — the exact mid-wait deadline path the
//     Unix dial cannot reproduce (Linux answers a full Unix backlog with
//     EAGAIN instead of blocking);
//   - the full busy-dial budget (no caller deadline) exhausts with the
//     typed ErrBusyDialBudgetExhausted;
//   - the production dial round-trips real authenticated HTTP over the
//     real localipc.Serve endpoint.
//
// Compile-verified locally (GOOS=windows go vet / go build); executed by
// the hosted Windows go test job — the runtime authority for this
// transport.
package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// busyPipeServer creates a named pipe with exactly ONE instance and an
// accept that never completes a second instance: after the first client
// connects, the pipe exists but has no listening instance, so every
// further CreateFile returns ERROR_PIPE_BUSY — the deterministic busy
// condition (no goroutine races, no load assumptions).
func busyPipeServer(t *testing.T) string {
	t.Helper()

	var pipeSeq uint32
	name := fmt.Sprintf(`\\.\pipe\sheytan-test-busy-dial-%d-%d`,
		os.Getpid(), atomic.AddUint32(&pipeSeq, 1))

	handle, err := windows.CreateNamedPipe(
		windows.StringToUTF16Ptr(name),
		windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1, // exactly one instance: no refill, ever
		4096, 4096, 0, nil,
	)
	if err != nil {
		t.Fatalf("CreateNamedPipe: %v", err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(handle) })

	// Arm the (never-completing) accept so the single instance listens for
	// exactly one client.
	evt, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(evt) })

	var ol windows.Overlapped
	ol.HEvent = evt
	if err := windows.ConnectNamedPipe(handle, &ol); err != nil &&
		err != windows.ERROR_IO_PENDING && err != windows.ERROR_PIPE_CONNECTED {
		t.Fatalf("ConnectNamedPipe: %v", err)
	}

	return name
}

// TestDialContextCanceledNeverTouchesPipe: an already-canceled context is
// refused before any CreateFile — dialing a pipe name that cannot exist
// proves no OS call was made (a real dial would surface ERROR_FILE_NOT_
// FOUND, not the context error).
func TestDialContextCanceledNeverTouchesPipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := DialContext(ctx, `\\.\pipe\sheytan-test-unreachable-pipe`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dial error = %v, want context.Canceled", err)
	}
}

// TestDialContextMissingPipeSurfacesCreateFileError: a pipe name that does
// not exist surfaces the honest CreateFile error (never retried into
// existence, never masked as a context error).
func TestDialContextMissingPipeSurfacesCreateFileError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := DialContext(ctx, `\\.\pipe\sheytan-test-no-such-pipe-4f2a`)
	if err == nil {
		t.Fatal("missing pipe must fail")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing pipe must surface the CreateFile error, got context error: %v", err)
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("missing pipe error = %v, want wrapped ERROR_FILE_NOT_FOUND", err)
	}
}

// TestDialContextCallerDeadlineBoundsBusyDial: against a genuinely busy
// pipe (exists, zero listening instances, never refilled), a caller
// deadline tighter than pipeBusyDialBudget must bound the dial — the
// readiness wait is clamped to the remaining deadline, so the dial ends
// at the caller's deadline with the typed context error, not after the
// full 10s internal budget.
func TestDialContextCallerDeadlineBoundsBusyDial(t *testing.T) {
	name := busyPipeServer(t)

	// Occupy the single instance: from here on every dial is busy.
	held, err := DialContext(context.Background(), name)
	if err != nil {
		t.Fatalf("first client dial: %v", err)
	}
	defer held.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = DialContext(ctx, name)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("busy dial must not succeed while the single instance is held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller-deadline busy dial error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("busy dial took %v — the caller deadline did not bound it", elapsed)
	}
}

// TestDialContextBusyBudgetExhaustsTyped: with NO caller deadline, the
// documented busy-dial budget (10s) is the only bound and its exhaustion
// returns the typed ErrBusyDialBudgetExhausted (never an unbounded
// reconnect loop).
func TestDialContextBusyBudgetExhaustsTyped(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the full 10s busy-dial budget")
	}

	name := busyPipeServer(t)

	held, err := DialContext(context.Background(), name)
	if err != nil {
		t.Fatalf("first client dial: %v", err)
	}
	defer held.Close()

	start := time.Now()
	_, err = DialContext(context.Background(), name)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrBusyDialBudgetExhausted) {
		t.Fatalf("budget-exhausted busy dial error = %v, want ErrBusyDialBudgetExhausted", err)
	}
	if elapsed < pipeBusyDialBudget {
		t.Fatalf("budget exhaustion after %v, want >= %v", elapsed, pipeBusyDialBudget)
	}
	if elapsed > 2*pipeBusyDialBudget {
		t.Fatalf("budget exhaustion took %v — the dial ran unbounded", elapsed)
	}
}

// TestDialContextRoundTripsAuthenticatedHTTPOverServe drives the exact
// production shape: the canonical dial behind an http.Transport against
// the REAL core over the REAL localipc.Serve named pipe.
func TestDialContextRoundTripsAuthenticatedHTTPOverServe(t *testing.T) {
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
		Timeout: 15 * time.Second,
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

	// Repeated real reads over the same transport: the connection survives
	// multiple API requests (no per-request pipe teardown).
	for i := 0; i < 5; i++ {
		resp2, err := client.Get("http://core/api/config")
		if err != nil {
			t.Fatalf("core read %d over DialContext: %v", i, err)
		}
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("core read %d status = %d, want 200", i, resp2.StatusCode)
		}
	}
}
