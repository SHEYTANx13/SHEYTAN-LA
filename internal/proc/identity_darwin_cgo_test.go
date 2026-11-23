//go:build darwin && !ios && cgo

package proc

// Native CGo-scoped regression tests for the run 44835425398 socket-union
// repair. This file compiles ONLY when the libproc bridge is linked (the
// build tag above is the guard, so every symbol it uses — socketRecord,
// isTCPListenerOn, listProcSocketFDs, readSocketFDInfo — is the real
// implementation from identity_darwin.go). Under CGO_ENABLED=0 the file is
// simply not part of the build, so the CGO-free variant cannot be asked to
// prove bridge behavior it does not carry.
//
// Run 45005960459 repair: the helpers take the SyscallConn-capable
// socket interface directly, and every net.Listener value is asserted to
// its concrete type (*net.TCPListener / *net.UnixListener) BEFORE it is
// passed on. The static net.Listener interface does NOT carry
// SyscallConn, so passing the interface value is a compile error on the
// Darwin lanes that build this file (the Linux lanes never see it — the
// same visibility gap that let the defect reach main).
//
// Evidence class: these tests execute against the REAL Darwin kernel
// records of THIS process (self-inspection needs no special privilege).
// A Linux or CGO-free build never runs them, and no test fabricates a
// kernel record — every record comes from the actual OS.

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// rawFDOf returns the real kernel fd of a live Go socket through the
// documented syscall.RawConn.Control contract.
func rawFDOf(t *testing.T, s interface {
	SyscallConn() (syscall.RawConn, error)
}) int32 {
	t.Helper()

	rc, err := s.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}

	var fd int32

	// syscall.RawConn.Control takes func(fd uintptr) — no error
	// return (run 45005960459: a func(fd uintptr) error literal does
	// not compile against the real RawConn contract).
	if cerr := rc.Control(func(f uintptr) {
		fd = int32(f)
	}); cerr != nil {
		t.Fatalf("Control: %v", cerr)
	}

	if fd <= 0 {
		t.Fatalf("socket reported invalid fd %d", fd)
	}

	return fd
}

// recordOfListener reads the PROC_PIDFDSOCKETINFO record of a live Go
// socket owned by THIS process.
func recordOfListener(t *testing.T, s interface {
	SyscallConn() (syscall.RawConn, error)
}) socketRecord {
	t.Helper()

	fd := rawFDOf(t, s)

	fds, err := listProcSocketFDs(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("own fd walk failed: %v", err)
	}

	found := false
	var rec socketRecord

	for _, f := range fds {
		if f == fd {
			found = true

			var rerr error

			rec, rerr = readSocketFDInfo(int32(os.Getpid()), fd)
			if rerr != nil {
				t.Fatalf("own socket fd %d must be inspectable, got: %v", fd, rerr)
			}

			break
		}
	}

	if !found {
		t.Fatalf("kernel fd %d of the live socket is missing from the PROC_PIDLISTFDS walk (%d socket fds)", fd, len(fds))
	}

	return rec
}

// TestTCPListenerRecordCarriesTCPKindAndCorrectPort pins the repaired
// union path end-to-end at the record level: a real listening TCP socket
// must be decoded as a SOCKINFO_TCP record whose local port, read through
// soi_proto.pri_tcp.tcpsi_ini.insi_lport and ntohs, equals the bound port.
func TestTCPListenerRecordCarriesTCPKindAndCorrectPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	defer l.Close()

	bound := l.Addr().(*net.TCPAddr).Port

	// *net.TCPListener implements SyscallConn; the static
	// net.Listener interface does not (run 45005960459).
	tcpListener, ok := l.(*net.TCPListener)
	if !ok {
		t.Fatalf("net.Listen returned %T, want *net.TCPListener", l)
	}

	rec := recordOfListener(t, tcpListener)

	if !rec.KindIsTCP {
		t.Fatalf("a real TCP listener must decode as a SOCKINFO_TCP record (soi_kind gate), got KindIsTCP=false")
	}

	if rec.Family != int32(syscall.AF_INET) && rec.Family != int32(syscall.AF_INET6) {
		t.Fatalf("tcp listener family = %d, want AF_INET/AF_INET6", rec.Family)
	}

	if got := localPortOf(rec.RawLPort); got != bound {
		t.Fatalf("pri_tcp.tcpsi_ini.insi_lport decoded to %d, want the bound port %d", got, bound)
	}

	if !isTCPListenerOn(&rec, bound) {
		t.Fatalf("the decoded record must satisfy the listener contract for port %d", bound)
	}

	if rec.RawLPort == -1 {
		t.Fatalf("a TCP-kind record must never carry the non-port sentinel")
	}
}

// TestNonTCPKindRecordsSkipWithoutUnionInterpretation proves the run
// 44835425398 gate on REAL records this process owns: a UDP socket
// (SOCKINFO_IN) and an AF_UNIX socket (SOCKINFO_UN) must decode without
// error, be marked NOT TCP, carry the -1 non-port sentinel (never a
// misread union value), and never satisfy the TCP listener contract.
func TestNonTCPKindRecordsSkipWithoutUnionInterpretation(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("udp listen: %v", err)
	}

	defer udp.Close()

	unixPath := filepath.Join(t.TempDir(), "probe.sock")

	unix, err := net.Listen("unix", unixPath)
	if err != nil {
		t.Fatalf("unix listen: %v", err)
	}

	defer unix.Close()

	// The TCP port the non-TCP records must never claim.
	tcp, terr := net.Listen("tcp", "127.0.0.1:0")
	if terr != nil {
		t.Fatalf("tcp listen: %v", terr)
	}

	defer tcp.Close()

	tcpPort := tcp.Addr().(*net.TCPAddr).Port

	// Run 45005960459: assert the concrete listener types before the
	// interface{ SyscallConn() ... } slice — the static net.Listener
	// interface value does not implement SyscallConn and would not
	// compile into the slice on the Darwin lanes.
	unixListener, ok := unix.(*net.UnixListener)
	if !ok {
		t.Fatalf("net.Listen(unix) returned %T, want *net.UnixListener", unix)
	}

	for _, s := range []interface {
		SyscallConn() (syscall.RawConn, error)
	}{udp, unixListener} {
		rec := recordOfListener(t, s)

		if rec.KindIsTCP {
			t.Fatalf("non-TCP socket decoded as SOCKINFO_TCP — the soi_kind union gate failed")
		}

		if rec.RawLPort != -1 {
			t.Fatalf("non-TCP record must carry the -1 non-port sentinel, got RawLPort=%d (a union value must never be interpreted as TCP data)", rec.RawLPort)
		}

		if isTCPListenerOn(&rec, tcpPort) {
			t.Fatalf("non-TCP record must never satisfy the TCP listener contract for port %d", tcpPort)
		}
	}
}

// TestOwnProcessListenerAttributed proves the walk attributes a listener
// owned by the EXPECTED process — this one. Same-executable identity and
// a positive pid are the adoption contract.
func TestOwnProcessListenerAttributed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port

	pid, exe, ok, err := ListeningProcess(port)
	if err != nil {
		t.Fatalf("own listener must be attributable, got error: %v", err)
	}

	if !ok {
		t.Fatalf("own listener must be attributable")
	}

	if pid != os.Getpid() {
		t.Fatalf("walk reported pid %d, want our own pid %d", pid, os.Getpid())
	}

	testBin, _ := os.Executable()
	// Run 45086860202 repair: proc_pidpath answers the CANONICAL vnode
	// path (/private/var/...), while os.Executable() answers the exec
	// path (/var/... — TMPDIR sits behind the /var symlink on every
	// macOS host). Raw string equality can never hold across that
	// boundary; SameExecutable is the repo's canonical comparison
	// (it resolves symlinks exactly for this purpose).
	if !SameExecutable(exe, testBin) {
		t.Fatalf("exe = %q, want our own binary %q", exe, testBin)
	}
}

// TestIPv6ListenerAttributed proves the AF_INET6 branch of the family
// contract: a loopback IPv6 listener is attributed exactly like the IPv4
// one (same union path, same byte-order convention).
func TestIPv6ListenerAttributed(t *testing.T) {
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable on this host: %v", err)
	}

	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port

	pid, _, ok, err := ListeningProcess(port)
	if err != nil {
		t.Fatalf("IPv6 listener must be attributable, got error: %v", err)
	}

	if !ok {
		t.Fatalf("IPv6 listener on port %d must be attributable", port)
	}

	if pid != os.Getpid() {
		t.Fatalf("IPv6 walk reported pid %d, want our own pid %d", pid, os.Getpid())
	}
}

// TestProvenFreePortReturnsTypedNoOwner pins the honest negative answer:
// a port PROVEN free (listener closed) must return ok=false with a NIL
// error — no fabricated owner, no typed-unknown false alarm.
func TestProvenFreePortReturnsTypedNoOwner(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := l.Addr().(*net.TCPAddr).Port

	if cerr := l.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	pid, exe, ok, err := ListeningProcess(port)
	if err != nil {
		t.Fatalf("a proven-free port must not error, got %v", err)
	}

	if ok || pid != 0 || exe != "" {
		t.Fatalf("a proven-free port must return the empty answer, got pid=%d exe=%q ok=%v", pid, exe, ok)
	}
}
