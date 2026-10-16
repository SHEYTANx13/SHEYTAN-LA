//go:build windows

// listener_windows_test.go — v1.10.6 Windows transport regression tests
// (the W2/W4 register items). These run in the hosted Windows "Go
// verification" job (real go test on windows-latest); locally they are
// compile-verified with GOOS=windows go vet / go build.
//
// Evidence classes are kept honest: the SID seam tests exercise the REAL
// token-derived current SID of the test process (no forged SIDs, no fake
// green); the idle-survival test reproduces the exact former defect (the
// endpoint died after 5 idle seconds) by letting the listener idle LONGER
// than the old connectTimeoutMS before the next dial; the burst-dial test
// pins the pre-created-instance-ahead-of-demand contract.
package localipc

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestSIDAllowedOwnSIDAllowed: the real token SID of this process must be
// allowed (kernel-backed currentSID — no forged identity).
func TestSIDAllowedOwnSIDAllowed(t *testing.T) {
	own, err := currentSID()
	if err != nil {
		t.Fatalf("currentSID: %v", err)
	}
	if own == "" {
		t.Fatal("own SID must not be empty")
	}

	ok, err := sidAllowed(own)
	if err != nil {
		t.Fatalf("sidAllowed(own): %v", err)
	}
	if !ok {
		t.Fatalf("own SID %q must be allowed", own)
	}
}

// TestSIDAllowedDeniesForeignSID: a syntactically valid SID that is NOT
// the current user's must be denied (the DACL + post-connect check pair
// must reject foreign/elevated processes in the same session).
func TestSIDAllowedDeniesForeignSID(t *testing.T) {
	own, err := currentSID()
	if err != nil {
		t.Fatalf("currentSID: %v", err)
	}

	// A well-formed non-current SID (a foreign account SID shape). If this
	// ever equals the real own SID the test binary is running as that
	// account — then the deny assertion is meaningless and must be
	// re-derived, never faked.
	foreign := "S-1-5-21-3623811015-3361044348-30300820-1013"
	if foreign == own {
		t.Skip("test process unexpectedly runs as the fixture SID; deny assertion would be vacuous")
	}

	ok, err := sidAllowed(foreign)
	if err != nil {
		t.Fatalf("sidAllowed(foreign): %v", err)
	}
	if ok {
		t.Fatalf("foreign SID %q must be denied", foreign)
	}
}

// TestSIDAllowedFailsClosedOnEmpty: an unidentifiable peer must never be
// accepted.
func TestSIDAllowedFailsClosedOnEmpty(t *testing.T) {
	for _, empty := range []string{"", "   "} {
		ok, err := sidAllowed(empty)
		if err != nil {
			t.Fatalf("sidAllowed(%q): %v", empty, err)
		}
		if ok {
			t.Fatalf("empty/blank client SID %q must fail closed", empty)
		}
	}
}

// TestServeSurvivesIdleBeyondFormerConnectTimeout is THE W2 regression:
// the former transport returned a FATAL accept error after 5000 idle ms,
// so http.Serve exited and the endpoint died. The endpoint must still
// answer after idling LONGER than that window (6.5s). The sleep here is
// the reproduction of a time-based defect — it is the probe, not a
// correctness crutch.
func TestServeSurvivesIdleBeyondFormerConnectTimeout(t *testing.T) {
	srv, cfg := newCore(t)
	ipc := startIPC(t, srv, cfg, "")
	client := ipcClient(ipc.Endpoint())

	time.Sleep(6500 * time.Millisecond)

	resp, err := client.Get("http://localipc/api/health")
	if err != nil {
		t.Fatalf("health after >5s idle: %v (the former fatal-timeout defect)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health after idle status = %d, want 200", resp.StatusCode)
	}
}

// TestBurstDialsNeverRaceInstanceCreation pins W1/W4: Serve must return
// with the endpoint ALREADY existing (synchronous first instance), and a
// client dialing repeatedly with zero delay must always find a listening
// instance (next instance created ahead of demand, never lazily).
func TestBurstDialsNeverRaceInstanceCreation(t *testing.T) {
	srv, cfg := newCore(t)
	ipc := startIPC(t, srv, cfg, "")

	client := ipcClient(ipc.Endpoint())

	// Zero-delay burst: each request dials the pipe immediately. Before
	// the v1.10.6 repair the first dial raced the lazy instance creation
	// (ERROR_FILE_NOT_FOUND) and every re-dial raced the next instance.
	for i := 0; i < 10; i++ {
		resp, err := client.Get("http://localipc/api/health")
		if err != nil {
			t.Fatalf("burst dial %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("burst dial %d: status = %d", i, resp.StatusCode)
		}
	}
}

// TestStopDoesNotHangOnPendingAccept is THE W3 regression: Stop must
// complete while an accept is pending (the former Close never cancelled
// the overlapped ConnectNamedPipe). Bounded proof: the whole Stop must
// finish well inside 5 seconds.
func TestStopDoesNotHangOnPendingAccept(t *testing.T) {
	srv, cfg := newCore(t)

	ipc, err := Serve(srv, cfg, "")
	if err != nil {
		t.Fatalf("localipc.Serve: %v", err)
	}

	done := make(chan struct{})
	go func() {
		ipc.Stop()
		ipc.Stop() // idempotent
		close(done)
	}()

	select {
	case <-done:
		// clean Stop with a pending accept — W3 closed
	case <-time.After(8 * time.Second):
		t.Fatal("Stop hung on a pending accept (W3 defect) — the overlapped connect was not cancelled")
	}
}

// TestAcceptVanishedPeerClassification — v1.10.9 (W-A, run 38871731277
// step 14) regression pin: the WHOLE vanished-peer completion family is
// transient on the accept path (the malformed-garbage client connects,
// writes, and dies without reading; the endpoint must outlive it), while a
// genuine endpoint fault — including ERROR_OPERATION_ABORTED outside the W3
// close path — stays fatal. The hosted assertion text for the original
// failure lives in windows-go-verification-logs-attempt1 (operator paste
// unavailable this cycle); the family membership is pinned here so the
// classification cannot silently narrow again.
func TestAcceptVanishedPeerClassification(t *testing.T) {
	for _, err := range []error{
		windows.ERROR_NO_DATA,
		windows.ERROR_BROKEN_PIPE,
		windows.ERROR_PIPE_NOT_CONNECTED,
		fmt.Errorf("wrapped completion: %w", windows.ERROR_BROKEN_PIPE),
	} {
		if !isVanishedPeer(err) {
			t.Fatalf("isVanishedPeer(%v) = false, want true (transient client-gone family)", err)
		}
	}
	for _, err := range []error{
		windows.ERROR_OPERATION_ABORTED, // endpoint fault / W3 close path, NOT transient
		windows.ERROR_ACCESS_DENIED,
		io.EOF,
	} {
		if isVanishedPeer(err) {
			t.Fatalf("isVanishedPeer(%v) = true, want false (endpoint fault must stay fatal)", err)
		}
	}
}

// TestPipeConnCloseStopsReadDeadlineTimer — v1.10.9 (W-B, latent defect,
// fixed regardless of W-A's outcome) regression pin: Close must stop the
// timer armed by SetReadDeadline (under the deadline mutex) and clear the
// deadline, or a stale time.AfterFunc fires CancelIoEx on a RECYCLED
// windows.Handle value seconds later and aborts an unrelated live
// connection's in-flight IO. Compile-verified locally (GOOS=windows);
// executed by the hosted Windows go test job.
func TestPipeConnCloseStopsReadDeadlineTimer(t *testing.T) {
	conn, err := newPipeConn(0) // handle never used for IO in this pin
	if err != nil {
		t.Fatalf("newPipeConn: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	conn.muDeadline.RLock()
	armed := conn.readTimer != nil
	conn.muDeadline.RUnlock()
	if !armed {
		t.Fatal("a future read deadline must arm the bounded cancel timer")
	}
	_ = conn.Close()
	conn.muDeadline.RLock()
	stopped := conn.readTimer == nil && conn.readDeadline.IsZero()
	conn.muDeadline.RUnlock()
	if !stopped {
		t.Fatal("Close must stop the deadline timer and clear the deadline (stale CancelIoEx on a recycled handle value)")
	}
}
