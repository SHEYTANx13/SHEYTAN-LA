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

// pipeSeq namespaces the fixture pipe names across tests in this process
// (package-level so two fixture instances can never collide even if a
// previous instance's handles are still being reaped).
var pipeSeq uint32

const (
	// busyFixtureAcceptMS bounds the fixture's single WaitForSingleObject
	// on the overlapped accept completion. A real client connect (or the
	// cleanup cancel) signals the event far earlier; the bound only
	// guarantees the helper goroutine always exits. Not a correctness
	// sleep: every completion path is event-driven.
	busyFixtureAcceptMS = 5000

	// busyFixtureReapWait bounds the cleanup wait on the fixture
	// channels before handles are closed. The overlapped
	// ConnectNamedPipe call itself returns immediately, so this is
	// microseconds in practice.
	busyFixtureReapWait = 2 * time.Second
)

// busyPipeFixture is the deterministic single-instance busy-pipe state
// machine: one pipe, one overlapped accept, channels for readiness and
// completion, and bounded cancellation-safe reaping.
type busyPipeFixture struct {
	name string
	// acceptDone receives the accept completion status exactly once:
	// nil once a client consumed the single listening instance (the
	// readiness signal the busy tests wait on), or the abort/timeout
	// error on every other exit path.
	acceptDone <-chan error
}

// waitAcceptDone proves the single instance was actually consumed by a
// client before the test proceeds — the deterministic readiness signal
// (never a sleep or a polling loop) that makes the following busy dial
// guaranteed to observe ERROR_PIPE_BUSY.
func (f *busyPipeFixture) waitAcceptDone(t *testing.T) {
	t.Helper()
	select {
	case err := <-f.acceptDone:
		if err != nil {
			t.Fatalf("overlapped accept did not complete successfully: %v", err)
		}
	case <-time.After(busyFixtureReapWait):
		t.Fatalf("overlapped accept never completed within %v — the fixture deadlocked", busyFixtureReapWait)
	}
}

// busyPipeServer creates a named pipe with exactly ONE instance and arms
// exactly ONE overlapped accept: after the first client connects, the
// single instance is occupied and every further CreateFile returns
// ERROR_PIPE_BUSY — the deterministic busy condition (no goroutine
// races, no load assumptions).
//
// Run-43601915826 correction: the previous fixture created a SYNCHRONOUS
// pipe handle (no FILE_FLAG_OVERLAPPED) while passing a non-NULL
// OVERLAPPED to ConnectNamedPipe. On a synchronous handle the call blocks
// until a client connects — and no client could dial because the helper
// never returned the pipe name. The fixture deadlocked inside
// windows.ConnectNamedPipe and burned the whole 25-minute Windows test
// timeout. The handle is now created WITH FILE_FLAG_OVERLAPPED so the
// accept is non-blocking and cancelable; completion is carried on
// channels; cleanup cancels the outstanding accept with CancelIoEx and
// reaps the goroutine with bounded waits before closing the handles, so
// no handle or goroutine outlives the test on any exit path.
func busyPipeServer(t *testing.T) *busyPipeFixture {
	t.Helper()

	name := fmt.Sprintf(`\\.\pipe\sheytan-test-busy-dial-%d-%d`,
		os.Getpid(), atomic.AddUint32(&pipeSeq, 1))

	// FILE_FLAG_OVERLAPPED makes the accept non-blocking and cancelable:
	// the helper must never block the test goroutine.
	handle, err := windows.CreateNamedPipe(
		windows.StringToUTF16Ptr(name),
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1, // exactly one instance: no refill, ever
		4096, 4096, 0, nil,
	)
	if err != nil {
		t.Fatalf("CreateNamedPipe: %v", err)
	}

	evt, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatalf("CreateEvent: %v", err)
	}

	var ol windows.Overlapped
	ol.HEvent = evt

	// Arm the single accept asynchronously. The overlapped call returns
	// immediately (ERROR_IO_PENDING, or ERROR_PIPE_CONNECTED if a client
	// was already queued); the OS signals the event on real completion.
	issued := make(chan error, 1)
	acceptDone := make(chan error, 1)
	go func() {
		issued <- windows.ConnectNamedPipe(handle, &ol)
		// ERROR_PIPE_CONNECTED means the accept completed inline and
		// no event will fire; otherwise wait for the event (client
		// connect, or the cleanup cancel). Bounded so this goroutine
		// always exits.
		s, werr := windows.WaitForSingleObject(evt, busyFixtureAcceptMS)
		if s == windows.WAIT_OBJECT_0 {
			var n uint32
			acceptDone <- windows.GetOverlappedResult(handle, &ol, &n, false)
			return
		}
		acceptDone <- werr
	}()

	t.Cleanup(func() {
		// The call must have been issued before any cancellation so
		// there is always something to reap (the overlapped call
		// returns immediately, so this is instantaneous).
		select {
		case <-issued:
		case <-time.After(busyFixtureReapWait):
		}
		// Cancel any outstanding accept: the abort completes the
		// overlapped operation and signals the event, so the helper
		// goroutine always exits. CancelIoEx is the documented
		// cancellation path — never a sleep, never a handle leak.
		_ = windows.CancelIoEx(handle, nil)
		// Reap the completion before the handles die so no goroutine
		// is still inside a wait that references them.
		select {
		case <-acceptDone:
		case <-time.After(busyFixtureReapWait):
		}
		_ = windows.CloseHandle(handle)
		_ = windows.CloseHandle(evt)
	})

	return &busyPipeFixture{name: name, acceptDone: acceptDone}
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
	fixture := busyPipeServer(t)

	// Occupy the single instance: from here on every dial is busy.
	held, err := DialContext(context.Background(), fixture.name)
	if err != nil {
		t.Fatalf("first client dial: %v", err)
	}
	defer held.Close()

	// Deterministic readiness: the overlapped accept completed, so the
	// single instance is genuinely occupied before the busy dial — the
	// busy condition no longer depends on fixture timing.
	fixture.waitAcceptDone(t)

	// 300ms caller deadline plus a small scheduler tolerance below: the
	// deadline must dominate the 10s internal busy-dial budget.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = DialContext(ctx, fixture.name)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("busy dial must not succeed while the single instance is held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller-deadline busy dial error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("busy dial took %v — the caller deadline did not bound it", elapsed)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("busy dial returned after %v — earlier than the caller deadline, which is not the documented busy path", elapsed)
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

	fixture := busyPipeServer(t)

	held, err := DialContext(context.Background(), fixture.name)
	if err != nil {
		t.Fatalf("first client dial: %v", err)
	}
	defer held.Close()

	// Deterministic readiness: the single instance is genuinely occupied
	// before the budget-exhaustion dial.
	fixture.waitAcceptDone(t)

	start := time.Now()
	_, err = DialContext(context.Background(), fixture.name)
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
