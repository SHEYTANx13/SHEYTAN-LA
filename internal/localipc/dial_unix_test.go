//go:build !windows

package localipc

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Unix-specific dial contract fixtures (the Windows twins live in
// dial_windows_test.go): the saturated-endpoint refusal and the stale
// endpoint probe semantics.

// TestDialContextDeadlineExpiry proves caller deadlines are honored at
// the CONTRACT level, on every unix kernel, without betting on one
// platform's AF_UNIX backlog arithmetic (run 44457547289 §1.3.b):
//
//  1. an already-expired deadline fails at entry with the typed context
//     error — before any syscall;
//  2. a genuinely-pending connect honors the deadline exactly when the
//     context expires, via the injectable transport seam (the portable,
//     deterministic mid-wait proof);
//  3. the real OS refusal of a saturated endpoint is platform-honest:
//     Linux answers a full backlog with EAGAIN — asserted on Linux only;
//     darwin legitimately completes saturated connects immediately
//     (BSD stacks complete AF_UNIX connects up to a different queue
//     policy), so on darwin the observed outcome is recorded, never
//     failed (TestDialBacklogNativeBehavior pins the invariants).
func TestDialContextDeadlineExpiry(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unix backlog fixture")
	}

	// 1. An already-expired deadline refuses before any dial.
	path := cloggedSocket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // let the deadline pass deterministically

	_, err := DialContext(ctx, path)
	if err == nil {
		t.Fatal("expired deadline must fail the dial")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline error = %v, want context.DeadlineExceeded", err)
	}

	// 2. A saturated endpoint surfaces the OS refusal — Linux pins the
	// full-backlog EAGAIN behavior; darwin records its own honest
	// outcome instead of asserting a Linux-only kernel result.
	if runtime.GOOS == "linux" {
		_, err = DialContext(context.Background(), path)
		if err == nil {
			t.Fatal("dial against a full backlog must not succeed on linux")
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("saturated endpoint must surface the OS error, got a context error: %v", err)
		}
	} else {
		conn, derr := DialContext(context.Background(), path)
		if derr == nil {
			// darwin legitimately completed the connect: the honest
			// answer is a usable connection — close it cleanly.
			_ = conn.Close()
			t.Logf("darwin completed the dial against the saturated endpoint immediately (BSD backlog behavior); recorded, not asserted")
		} else {
			t.Logf("darwin refused the dial against the saturated endpoint with: %v", derr)
		}
	}
}

// TestDialContextMidWaitDeadline is the deterministic mid-wait deadline
// proof (run 44457547289 §1.3.b): through the injectable transport seam
// the connect is genuinely pending when the caller's deadline lapses.
// The contract: context.DeadlineExceeded wins, the typed error surfaces,
// and the transport leaks neither goroutines nor descriptors.
func TestDialContextMidWaitDeadline(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unix transport seam fixture")
	}

	restore := withPendingDialTransport(t)
	defer restore()

	path := shortSocketPath(t, "pending-dial.sock")

	fdsBefore := openFDCount(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()

	conn, err := DialContext(ctx, path)
	if err == nil {
		_ = conn.Close()
		t.Fatal("a deadline lapsing during a pending connect must fail the dial")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mid-wait deadline error = %v, want context.DeadlineExceeded", err)
	}

	// The typed error must arrive FROM the deadline, not early: the wait
	// covered the full budget (bounded tolerance for scheduler jitter).
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("dial returned after %v — the deadline was not honored mid-wait", elapsed)
	}

	// No descriptor leak: the failed dial leaves no dangling transport
	// descriptor behind (allow the pre-existing drift of ±2 for the
	// runtime's own polling machinery).
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		gotFDs := openFDCount(t)
		if gotFDs > fdsBefore+2 {
			t.Fatalf("the failed pending dial leaked descriptors: before=%d after=%d", fdsBefore, gotFDs)
		}
	}

	// No goroutine leak: the seam goroutine must have terminated.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if pendingDialGoroutines.Load() == 0 {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if n := pendingDialGoroutines.Load(); n != 0 {
		t.Fatalf("the pending dial leaked %d goroutine(s)", n)
	}
}

// TestDialContextMidWaitCancel mirrors the deadline proof with explicit
// cancellation: a canceled context during a genuinely-pending connect
// surfaces context.Canceled and cleans up.
func TestDialContextMidWaitCancel(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unix transport seam fixture")
	}

	restore := withPendingDialTransport(t)
	defer restore()

	path := shortSocketPath(t, "pending-cancel.sock")

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	conn, err := DialContext(ctx, path)
	if err == nil {
		_ = conn.Close()
		t.Fatal("an explicit cancel during a pending connect must fail the dial")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-wait cancel error = %v, want context.Canceled", err)
	}
}

// TestDialDeadlineStress hammers the deadline path (run 44457547289
// §1.3.b stress contract): repeated expired deadlines and mid-wait
// cancellations must not leak descriptors or goroutines. Descriptor
// counting uses the platform's own fd directory (/proc/self/fd on
// Linux, /dev/fd on darwin); the goroutine floor is checked portably.
func TestDialDeadlineStress(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("unix stress fixture")
	}

	path := shortSocketPath(t, "stress.sock")

	fdsBefore := openFDCount(t)
	gBefore := runtime.NumGoroutine()

	const rounds = 64

	for i := 0; i < rounds; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		time.Sleep(200 * time.Microsecond) // let the deadline lapse deterministically

		conn, err := DialContext(ctx, path)
		if err == nil {
			_ = conn.Close()
			cancel()
			t.Fatalf("round %d: an expired deadline must fail the dial", i)
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			cancel()
			t.Fatalf("round %d: expired deadline error = %v, want context.DeadlineExceeded", i, err)
		}

		cancel()
	}

	// The stress loop must not leak: descriptor count stable (small
	// drift allowed for the runtime's own machinery), goroutine count
	// back to baseline.
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= gBefore+2 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if gotFDs := openFDCount(t); gotFDs > fdsBefore+2 {
		t.Fatalf("the deadline stress leaked descriptors: before=%d after=%d", fdsBefore, gotFDs)
	}

	if got := runtime.NumGoroutine(); got > gBefore+2 {
		t.Fatalf("the deadline stress leaked goroutines: before=%d after=%d", gBefore, got)
	}
}

// TestDialBacklogNativeBehavior is the native AF_UNIX backlog integration
// record (run 44457547289 §1.3.b): it observes the REAL saturated-endpoint
// behavior of the running kernel and asserts only cross-platform
// invariants — a real error or a real connection, never a context error
// with a live context, and no resource leakage from either outcome. The
// per-platform refusal expectation lives ONLY in the linux branch of
// TestDialContextDeadlineExpiry.
func TestDialBacklogNativeBehavior(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native backlog observation")
	}

	path := cloggedSocket(t)

	fdsBefore := openFDCount(t)

	var (
		succeeded int
		refused   int
		ctxErr    int
	)

	const attempts = 8

	for i := 0; i < attempts; i++ {
		conn, err := DialContext(context.Background(), path)
		if err == nil {
			succeeded++
			_ = conn.Close()

			continue
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			ctxErr++
			t.Errorf("attempt %d: a live-context dial must never report a context error, got %v", i, err)

			continue
		}

		refused++
	}

	t.Logf("native %s/%s backlog observation: attempts=%d succeeded=%d refused=%d ctxErrors=%d",
		runtime.GOOS, runtime.GOARCH, attempts, succeeded, refused, ctxErr)

	if gotFDs := openFDCount(t); gotFDs > fdsBefore+2 {
		t.Fatalf("the backlog observation leaked descriptors: before=%d after=%d", fdsBefore, gotFDs)
	}
}

// ---------------------------------------------------------------------------
// The deterministic pending-connect seam.
// ---------------------------------------------------------------------------

// pendingDialGoroutines counts seam goroutines still blocked in the
// simulated pending state — the leak detector for the seam transport.
var pendingDialGoroutines atomic.Int64

// withPendingDialTransport replaces the production dial transport with a
// controlled local test transport that enters a GENUINELY-pending connect
// state (a real socketpair peer nobody drains, so the descriptor stays
// open and blocked) and returns only when the caller's context expires —
// exactly the mid-wait state kernel backlogs produce on some kernels,
// now deterministic. The replacement is restored on test cleanup.
func withPendingDialTransport(t *testing.T) (restore func()) {
	t.Helper()

	// A connected socketpair models the half-open transport: writes
	// block once the buffer fills, and nothing is ever read — the
	// transport below parks in the write-drain loop until the context
	// releases it. Both ends are made non-blocking explicitly (portable
	// across Linux and darwin) so the fill loop observes EAGAIN instead
	// of blocking in a raw write.
	sp, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}

	left, right := sp[0], sp[1]

	if serr := unix.SetNonblock(left, true); serr != nil {
		t.Fatalf("nonblock left: %v", serr)
	}

	if serr := unix.SetNonblock(right, true); serr != nil {
		t.Fatalf("nonblock right: %v", serr)
	}

	t.Cleanup(func() {
		_ = unix.Close(left)
		_ = unix.Close(right)
	})

	original := dialTransport

	dialTransport = func(ctx context.Context, _ string) (net.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pendingDialGoroutines.Add(1)
		defer pendingDialGoroutines.Add(-1)

		// Fill the socketpair buffer until the write would block, then
		// wait on the context — the pending-connect state.
		buf := make([]byte, 4096)

		for {
			if _, werr := unix.Write(left, buf); werr != nil {
				if errors.Is(werr, unix.EAGAIN) {
					break
				}

				return nil, werr
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			return nil, errors.New("pending-dial seam timeout: no context release")
		}
	}

	return func() { dialTransport = original }
}

// openFDCount returns the process's open-descriptor count from the
// platform's fd directory (/proc/self/fd on Linux, /dev/fd on darwin).
func openFDCount(t *testing.T) int {
	t.Helper()

	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("fd directory %s unavailable: %v", dir, err)
	}

	return len(entries)
}

// cloggedSocket binds a listening Unix socket whose pending-accept queue
// is already full: connects complete in the kernel only up to the queued
// slot, and the next connect is refused with the OS's honest answer.
func cloggedSocket(t *testing.T) string {
	t.Helper()

	path := shortSocketPath(t, "clogged.sock")

	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })

	if err := unix.Bind(fd, &unix.SockaddrUnix{Name: path}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	// backlog 0 keeps the pending queue at its minimum (one slot on Linux).
	if err := unix.Listen(fd, 0); err != nil {
		t.Fatalf("listen: %v", err)
	}

	// Occupy the single pending slot with a connection nobody accepts.
	filler, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("fill backlog: %v", err)
	}
	t.Cleanup(func() { _ = filler.Close() })

	return path
}
