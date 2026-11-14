//go:build !windows

package localipc

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Unix-specific dial contract fixtures (the Windows twins live in
// dial_windows_test.go): the saturated-endpoint refusal and the stale
// endpoint probe semantics.

// TestDialContextDeadlineExpiry proves caller deadlines are honored: an
// already-expired deadline fails at entry with the typed context error,
// and a live-but-saturated endpoint surfaces the real OS refusal (Linux
// answers a full Unix backlog with EAGAIN instead of blocking — the
// original OS error is the honest answer, never masked as a context
// error). The mid-wait deadline path (budget bounded while the transport
// is busy) is exercised by the Windows busy-pipe test in
// dial_windows_test.go.
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

	// 2. A saturated endpoint surfaces the OS refusal, not a context error.
	_, err = DialContext(context.Background(), path)
	if err == nil {
		t.Fatal("dial against a full backlog must not succeed")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("saturated endpoint must surface the OS error, got a context error: %v", err)
	}
}

// TestDialContextMissingServer proves a vanished server surfaces the real
// OS error (typed, wrapped for diagnosis) — never a context error, never
// a silent fallback transport.
func TestDialContextMissingServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-server-here.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := DialContext(ctx, path)
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial to a missing endpoint must fail")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing-server dial must surface the OS error, got context error: %v", err)
	}
}

// cloggedSocket binds a listening Unix socket whose pending-accept queue
// is already full: connects complete in the kernel only up to the queued
// slot, and the next connect is refused with the OS's honest answer.
func cloggedSocket(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "clogged.sock")

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
