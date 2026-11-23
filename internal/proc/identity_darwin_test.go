//go:build darwin && !ios

package proc

// Native darwin identity tests (run 44457547289 §1.3.a item 7): the fd
// walk must prove the RIGHT process, refuse the wrong one, and fail
// closed on every unprovable state. These tests exercise the real
// libproc bridge — a Linux test of shared helpers is not native darwin
// proof, and a CGO_ENABLED=0 build (typed-unsupported provider,
// identity_darwin_nocgo.go) cannot claim the walk either, so the
// native-walk assertions skip with an explicit reason there instead of
// faking a pass (run 44547031698 §5.4: record the exact native evidence
// class; do not treat a cross-compile or a CGO-free build as native
// proof).

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// requireNativeWalk guards every assertion that needs the linked libproc
// bridge. On the macOS runners (cgo toolchain present) this is a no-op
// and the tests are the native proof; under CGO_ENABLED=0 the build
// honestly reports identity as unsupported and the tests say so.
func requireNativeWalk(t *testing.T) {
	t.Helper()

	if !darwinIdentityNative {
		t.Skip("darwin identity walk requires the cgo libproc bridge; this CGO_ENABLED=0 build carries the typed-unsupported provider")
	}
}

// childListenMode is the env key that turns the re-exec'd test binary
// into a minimal listener child: it binds 127.0.0.1:0 (or the requested
// port), reports the bound port on the address file, and serves until
// killed.
const childListenMode = "SHEYTAN_PROC_TEST_CHILD_LISTEN"

func TestMain(m *testing.M) {
	if port := os.Getenv(childListenMode); port != "" {
		runListenerChild(port)
		return
	}

	os.Exit(m.Run())
}

// runListenerChild is the re-exec child body: bind, report, serve.
func runListenerChild(reportFile string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}

	if err := os.WriteFile(reportFile, []byte(strconv.Itoa(l.Addr().(*net.TCPAddr).Port)), 0o644); err != nil {
		os.Exit(4)
	}

	for {
		c, err := l.Accept()
		if err != nil {
			os.Exit(0)
		}

		_ = c.Close()
	}
}

// spawnListenerChild starts a real foreign-PID listener (a child process
// whose exe path is THIS test binary — the same-managed-binary scenario
// the adoption layer must accept) and returns its bound port.
func spawnListenerChild(t *testing.T) int {
	t.Helper()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	addrFile := t.TempDir() + "/child.addr"

	cmd := exec.Command(testBin)
	cmd.Env = append(os.Environ(), childListenMode+"="+addrFile)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start listener child: %v", err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)

	for {
		if data, rerr := os.ReadFile(addrFile); rerr == nil {
			if port, perr := strconv.Atoi(string(data)); perr == nil && port > 0 {
				return port
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("listener child never reported its port")
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// TestListeningProcessAttributesForeignChildListener proves the walk
// attributes a listener owned by a DIFFERENT live process (the managed
// engine scenario): the PID must be the child's, the exe path must be
// this test binary, and the start time must be the child's — not ours.
func TestListeningProcessAttributesForeignChildListener(t *testing.T) {
	requireNativeWalk(t)

	port := spawnListenerChild(t)

	pid, exe, ok, err := ListeningProcess(port)
	if err != nil {
		t.Fatalf("foreign child listener must be attributable, got error: %v", err)
	}

	if !ok {
		t.Fatalf("foreign child listener must be attributable")
	}

	if pid == os.Getpid() {
		t.Fatalf("the walk must report the CHILD pid, not our own")
	}

	if pid <= 0 {
		t.Fatalf("the child pid must be positive, got %d", pid)
	}

	testBin, _ := os.Executable()
	// Run 45086860202 repair: proc_pidpath returns the canonical vnode
	// path (/private/var/...), os.Executable() the exec path
	// (/var/...) — TMPDIR lives behind the /var symlink on every macOS
	// host, so raw string equality can never hold. SameExecutable is
	// the repo's canonical comparison (symlink-resolving).
	if !SameExecutable(exe, testBin) {
		t.Fatalf("child exe = %q, want the test binary path %q", exe, testBin)
	}

	// Start-identity binding (PID-reuse defense): the child's start time
	// must differ from our own long-running test process start time.
	childStart, serr := ProcessStartTime(pid)
	if serr != nil {
		t.Fatalf("child start time must resolve natively: %v", serr)
	}

	ownStart, oerr := ProcessStartTime(os.Getpid())
	if oerr != nil {
		t.Fatalf("own start time must resolve natively: %v", oerr)
	}

	if childStart.Before(ownStart) {
		// The child was spawned after us — this is the strict direction
		// the stale-engine check relies on.
		t.Fatalf("child start %v must not predate our own start %v", childStart, ownStart)
	}
}

// TestListeningProcessWrongPortRefused proves a listener on a DIFFERENT
// port is never attributed to the queried port (no port confusion, no
// byte-order guessing game).
func TestListeningProcessWrongPortRefused(t *testing.T) {
	requireNativeWalk(t)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	defer l.Close()

	bound := l.Addr().(*net.TCPAddr).Port

	// Find a different port number for the query.
	query := bound + 1
	if query > 65535 {
		query = bound - 1
	}

	pid, exe, ok, err := ListeningProcess(query)
	if err != nil {
		t.Fatalf("a wrong-port query must not error, got %v", err)
	}

	if ok {
		t.Fatalf("port %d has no listener; the walk must not attribute the listener on %d (got pid %d exe %q)", query, bound, pid, exe)
	}
}

// TestListeningProcessDeadOwnerRefused proves a vanished owner is not
// adoptable: a dead PID must surface ErrProcessNotFound for the
// metadata probes (never a stale identity).
func TestListeningProcessDeadOwnerRefused(t *testing.T) {
	requireNativeWalk(t)

	cmd := exec.Command("/bin/sleep", "0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a probe process: %v", err)
	}

	pid := cmd.Process.Pid

	_, _ = cmd.Process.Wait()

	if ProcessAlive(pid) {
		t.Skip("pid was recycled too fast to observe the dead state")
	}

	if _, err := ProcessExePath(pid); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("a dead pid must be typed ErrProcessNotFound, got %v", err)
	}

	if _, err := ProcessStartTime(pid); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("a dead pid must be typed ErrProcessNotFound for start time, got %v", err)
	}
}

// TestProcessStartTimeBindsIncarnation proves the start-time probe is the
// real epoch value (monotonic sanity: our own start time is in the past
// and stable across repeated reads — the PID-reuse defense contract).
func TestProcessStartTimeBindsIncarnation(t *testing.T) {
	requireNativeWalk(t)

	a, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("own start time: %v", err)
	}

	if a.IsZero() {
		t.Fatalf("own start time must not be the zero time")
	}

	if a.After(time.Now().Add(time.Minute)) {
		t.Fatalf("own start time %v is in the future", a)
	}

	b, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("second own start time: %v", err)
	}

	if !a.Equal(b) {
		t.Fatalf("start time must be stable across reads: %v vs %v", a, b)
	}
}

// TestLocalPortOfNetworkOrderConvention pins the insi_lport decoding
// convention (kernel network byte order, ntohs on the little-endian
// host): 8080 stored network-order little-endian reads 0x901F and must
// decode to 8080 — and a raw host-order value must NOT silently decode.
func TestLocalPortOfNetworkOrderConvention(t *testing.T) {
	// 8080 = 0x1F90; network-order bytes 1F 90 read little-endian = 0x901F.
	if got := localPortOf(int32(0x0000901F)); got != 8080 {
		t.Fatalf("localPortOf(0x901F) = %d, want 8080", got)
	}

	// Upper half set: unusable evidence, decoded as 0 (never guessed).
	if got := localPortOf(int32(0x0001901F)); got != 0 {
		t.Fatalf("localPortOf with upper half set = %d, want 0", got)
	}
}
