// Process identity primitives (v1.3.6, spec §3): before SHEYTAN adopts
// or reports against a process listening on its engine port, the process
// must be identifiable through real OS APIs — never shell parsing.
//
// This file carries the shared contract; identity_windows.go,
// identity_linux.go and identity_darwin.go carry the per-OS
// implementations (run 44306343338 repair §4.7: the Linux /proc path is
// Linux-tagged; darwin carries typed UNSUPPORTED results instead of
// compiling Linux-only code).
package proc

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

var (
	// ErrProcessNotFound is returned when the PID no longer exists.
	ErrProcessNotFound = errors.New("process not found")

	// ErrPortOwnerUnknown is returned when the OS could not attribute
	// the listening port to a PID (permission or platform limits).
	ErrPortOwnerUnknown = errors.New("port owner could not be determined")

	// ErrPortOwnerUnsupported is the typed UNSUPPORTED answer for port
	// ownership on platforms whose discovery mechanism is not linked
	// in this build (run 44306343338 repair §4.7: darwin). Callers
	// keep the operation fail-closed and surface the limitation —
	// ownership is never faked.
	ErrPortOwnerUnsupported = errors.New("port ownership lookup unsupported on this platform")

	// ErrProcessMetadataUnsupported is the typed UNSUPPORTED answer for
	// other process-metadata probes (image path, start time) on
	// platforms whose mechanism is not linked in this build.
	ErrProcessMetadataUnsupported = errors.New("process metadata probe unsupported on this platform")
)

// ListeningProcess identifies the process listening on 127.0.0.1:port
// (loopback only — the engine never binds an external interface by
// default). It returns the owning PID and its executable path.
//
// ok is false (with a nil error) when nothing is listening on the port —
// proven by the OS refusing a loopback connect, which requires no
// ownership API at all (run 44378562071 §4.5: a free port must never be
// answered with the typed UNSUPPORTED error). err is non-nil only when a
// listener EXISTS but identity CANNOT be proven — callers must treat
// that as "foreign process, unproven" and refuse adoption (fail closed).
func ListeningProcess(port int) (pid int, exe string, ok bool, err error) {
	if port <= 0 || port > 65535 {
		return 0, "", false, errors.New("invalid port")
	}

	// Liveness pre-probe: a loopback connect refusal PROVES nothing is
	// listening (no PID claimed, none invented) without any
	// platform-specific ownership mechanism. Inconclusive dial errors
	// fall through to the ownership probe, which keeps the fail-closed
	// answer.
	if exists, proven := loopbackListenerExists(port); proven && !exists {
		return 0, "", false, nil
	}

	return listeningProcessImpl(port)
}

// ProcessExePath resolves the image path of a live PID through the OS
// (QueryFullProcessImageName on Windows, /proc/<pid>/exe on Unix).
func ProcessExePath(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrProcessNotFound
	}

	return processExePathImpl(pid)
}

// SameExecutable reports whether two executable paths denote the same
// program file. The comparison is case-insensitive on Windows and
// resolves symlinks where possible.
func SameExecutable(a, b string) bool {
	return sameExecutableImpl(a, b)
}

// loopbackListenerExists probes 127.0.0.1:port with a short connect.
// proven=true means the OS gave a definitive answer: exists=true when a
// listener accepted (or queued) the connection, false when the connect
// was refused (nothing listening). proven=false covers inconclusive
// dial failures (timeouts, unreachable networks) — callers keep the
// fail-closed path in that case.
// loopbackListenerExists probes the loopback families with a short
// connect. proven=true means the OS gave a definitive answer: exists=true
// when a listener accepted (or queued) the connection, false when every
// refused connect proves nothing listening. proven=false covers
// inconclusive dial failures (timeouts, unreachable networks) - callers
// keep the fail-closed path in that case.
//
// Run 45287161747 repair (TestIPv6ListenerAttributed, reproduced on
// linux): the probe must cover BOTH loopback families. A listener bound
// to [::1] only never answers an IPv4 connect - the old IPv4-only probe
// read that refusal as a definitive "nothing is listening" and
// short-circuited the ownership walk BEFORE it could attribute the live
// IPv6 listener (ok=false, nil error). A family only contributes a
// definitive "absent" verdict when its connect is REFUSED; an unreachable
// or inconclusive family downgrades the whole answer to not-proven so
// the ownership walk runs.
func loopbackListenerExists(port int) (exists bool, proven bool) {
	allProven := true

	for _, addr := range []string{"127.0.0.1", "[::1]"} {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", addr, port), 250*time.Millisecond)
		if err != nil {
			// A refused connect on a loopback family is definitive for
			// that family: no listener there. Anything else (unreachable,
			// timeout) is inconclusive and downgrades the answer.
			if isConnectRefused(err) {
				continue
			}

			allProven = false
			continue
		}

		_ = conn.Close()

		return true, true
	}

	return false, allProven
}

// isConnectRefused classifies a loopback dial error as "definitively
// nothing listening". On unix the refused connect surfaces as ECONNREFUSED
// (SyscallError around it); on Windows it is WSAECONNREFUSED.
func isConnectRefused(err error) bool {
	var serr *net.OpError
	if !errors.As(err, &serr) {
		return false
	}

	return errors.Is(serr.Err, syscall.ECONNREFUSED)
}
