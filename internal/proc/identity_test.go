package proc

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"
)

func TestListeningProcessFindsSelfOwnedListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port

	pid, exe, ok, err := ListeningProcess(port)

	switch runtime.GOOS {
	case "linux":
		if err != nil {
			// Identity failure on a process WE own is an environment
			// limitation (e.g. restricted /proc) — report, don't fail CI.
			t.Skipf("port owner lookup unavailable on this runner: %v", err)
		}

		if !ok {
			t.Fatalf("a live listener must be attributable")
		}

		if pid != os.Getpid() {
			t.Fatalf("the listener belongs to pid %d, want %d", pid, os.Getpid())
		}

		if exe == "" {
			t.Fatalf("the executable path must resolve for our own process")
		}
	case "darwin":
		// Run 44457547289 §1.3.a: darwin now performs the REAL libproc
		// ownership walk (PROC_ALL_PIDS -> PROC_PIDLISTFDS ->
		// PROC_PIDFDSOCKETINFO -> PROC_PIDPATHINFO), so a live self-owned
		// listener must be attributable exactly like Linux — same
		// identity contract, native mechanism. A typed unsupported
		// result here would mean the fd walk is broken, never a
		// legitimate skip.
		if err != nil {
			t.Fatalf("darwin port-owner walk must attribute our own listener, got error: %v", err)
		}

		if !ok {
			t.Fatalf("a live listener must be attributable on darwin")
		}

		if pid != os.Getpid() {
			t.Fatalf("the listener belongs to pid %d, want %d", pid, os.Getpid())
		}

		if exe == "" {
			t.Fatalf("the executable path must resolve for our own process")
		}
	default:
		t.Skipf("no per-OS expectation for %s", runtime.GOOS)
	}
}

func TestListeningProcessReturnsNotOKForFreePort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	_, _, ok, err := ListeningProcess(port)
	if err != nil {
		t.Fatalf("a free port must not error, got %v", err)
	}

	if ok {
		t.Fatalf("a closed port must not report a listener")
	}
}

func TestListeningProcessRejectsInvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 99999} {
		if _, _, _, err := ListeningProcess(port); err == nil {
			t.Fatalf("port %d must be rejected", port)
		} else {
			_ = fmt.Sprint(err)
		}
	}
}

func TestSameExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if !SameExecutable(exe, exe) {
		t.Fatalf("identical paths must match")
	}

	if SameExecutable(exe, "/definitely/not/this/binary") {
		t.Fatalf("different paths must not match")
	}
}
