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
//
// Run-43718750096 correction: the arming of the overlapped accept was not
// synchronized with the fixture's return, so the caller's first dial
// could connect inside the CreateNamedPipe→ConnectNamedPipe window. In
// that window ConnectNamedPipe does NOT return ERROR_IO_PENDING: it
// completes INLINE with ERROR_PIPE_CONNECTED and the OVERLAPPED event is
// NEVER signaled (there is no outstanding I/O). The old helper waited on
// the event regardless of the completion mode, so acceptDone could not
// arrive until the 5s event-timeout path — long past the 2s readiness
// bound in waitAcceptDone — which is exactly how
// TestDialContextCallerDeadlineBoundsBusyDial and
// TestDialContextBusyBudgetExhaustsTyped failed on the hosted Windows
// runner (~4s each: 2s readiness timeout + 2s cleanup reap). Two changes,
// both fixture-only (the production dial is untouched and its deadline
// semantics are asserted independently below):
//  1. the accept is ISSUED before the fixture returns (bounded wait on
//     the issued channel), so the listening instance is deterministically
//     armed before the caller's first dial — no scheduling race;
//  2. the helper branches on the actual ConnectNamedPipe completion mode
//     (nil synchronous success / ERROR_PIPE_CONNECTED inline completion
//     / ERROR_IO_PENDING armed) instead of assuming an event will fire.
//
// No timeout was lengthened and no assertion weakened: the 5s event bound,
// the 2s reap bound and both deadline/budget assertions are unchanged.
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
	// immediately in one of exactly three modes, each with its own
	// completion path (run-43718750096: assuming the event always fires
	// is the fixture defect — ERROR_PIPE_CONNECTED means there is no
	// outstanding I/O to signal it):
	//   - ERROR_IO_PENDING: armed; the OS signals the event when a real
	//     client consumes the single listening instance (or when the
	//     cleanup cancel aborts the accept);
	//   - ERROR_PIPE_CONNECTED: a client already connected in the
	//     pre-arm window — the accept completed inline, the instance IS
	//     consumed, and no event will ever fire;
	//   - nil: the accept completed synchronously — same semantics, the
	//     instance is consumed and no event will fire.
	issued := make(chan error, 1)
	acceptDone := make(chan error, 1)
	go func() {
		cerr := windows.ConnectNamedPipe(handle, &ol)
		issued <- cerr
		switch {
		case cerr == nil:
			acceptDone <- nil
		case errors.Is(cerr, windows.ERROR_PIPE_CONNECTED):
			// Inline completion: the single instance was consumed
			// by a client before the accept was armed. This is a
			// SUCCESS of the readiness contract (the instance is
			// occupied, every further dial is busy), not an error
			// — and waiting on the event here is the old deadlock.
			acceptDone <- nil
		case errors.Is(cerr, windows.ERROR_IO_PENDING):
			// Armed: completion is carried by the event (client
			// connect, or the cleanup cancel). Bounded so this
			// goroutine always exits.
			s, werr := windows.WaitForSingleObject(evt, busyFixtureAcceptMS)
			if s == windows.WAIT_OBJECT_0 {
				var n uint32
				acceptDone <- windows.GetOverlappedResult(handle, &ol, &n, false)
				return
			}
			if werr == nil {
				// x/sys/windows reports an errno only for
				// WAIT_FAILED; a plain WAIT_TIMEOUT would
				// otherwise surface as a false nil "success".
				werr = fmt.Errorf("fixture accept event not signaled within %dms (wait state %d)",
					busyFixtureAcceptMS, s)
			}
			acceptDone <- werr
		default:
			acceptDone <- cerr
		}
	}()

	// Deterministic arming (run-43718750096): the accept must be issued
	// before the fixture is handed to the caller, otherwise the first
	// client dial can connect in the pre-arm window and force the inline
	// path above. The overlapped call returns immediately, so this wait
	// is instantaneous in practice; it is bounded only so a starved
	// fixture goroutine fails loudly instead of deadlocking the test.
	select {
	case <-issued:
	case <-time.After(busyFixtureReapWait):
		t.Fatalf("overlapped accept was not issued within %v — the fixture goroutine never ran", busyFixtureReapWait)
	}

	t.Cleanup(func() {
		// The accept was issued before the fixture returned, so there
		// is always something to reap (the overlapped call returns
		// immediately, so this is instantaneous).
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

// TestDialContextBusyDialCrossesWaitCap (v1.11.1, run 43773885283): the
// budget-exhaustion contract regressed to an early exit — ONE
// WaitNamedPipe timeout (the 2000ms per-wait cap) returned the generic
// "no listening pipe instance" error after 2s of the 10s busy-dial
// budget, so the typed ErrBusyDialBudgetExhausted was unreachable and
// TestDialContextBusyBudgetExhaustsTyped failed on the hosted Windows
// runner. This focused guard pins the loop semantics the 10s test
// depends on, without paying the full 10s budget: a busy dial bounded by
// a caller deadline LONGER than one per-wait cap must survive the first
// wait timeout, keep the documented wait/retry contract, and still end
// AT the caller's deadline with the typed context error — never early,
// never generic.
func TestDialContextBusyDialCrossesWaitCap(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises multiple busy-wait caps")
	}

	fixture := busyPipeServer(t)

	// Occupy the single instance: from here on every dial is busy.
	held, err := DialContext(context.Background(), fixture.name)
	if err != nil {
		t.Fatalf("first client dial: %v", err)
	}
	defer held.Close()

	// Deterministic readiness: the single instance is genuinely occupied
	// before the bounded busy dial.
	fixture.waitAcceptDone(t)

	// Deadline comfortably above ONE per-wait cap (2000ms) but far below
	// the full 10s busy-dial budget: the dial must cross at least one
	// full wait timeout and end at the caller's deadline.
	deadline := time.Duration(pipeBusyWaitMS)*time.Millisecond + 750*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	_, err = DialContext(ctx, fixture.name)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("busy dial must not succeed while the single instance is held")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy dial across a wait cap error = %v, want context.DeadlineExceeded (a single wait timeout must not end the dial early with a generic error)", err)
	}
	if elapsed < deadline-300*time.Millisecond {
		t.Fatalf("busy dial ended after %v — before the caller deadline %v minus tolerance; the early-exit-after-one-wait-cap defect is back", elapsed, deadline)
	}
	if elapsed > deadline+2*time.Second {
		t.Fatalf("busy dial took %v — the caller deadline did not bound it", elapsed)
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
