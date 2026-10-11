// Process identity primitives (v1.3.6, spec §3): before SHEYTAN adopts
// or reports against a process listening on its engine port, the process
// must be identifiable through real OS APIs — never shell parsing.
//
// This file carries the shared contract; identity_windows.go and
// identity_unix.go carry the per-OS implementations.
package proc

import "errors"

var (
	// ErrProcessNotFound is returned when the PID no longer exists.
	ErrProcessNotFound = errors.New("process not found")

	// ErrPortOwnerUnknown is returned when the OS could not attribute
	// the listening port to a PID (permission or platform limits).
	ErrPortOwnerUnknown = errors.New("port owner could not be determined")
)

// ListeningProcess identifies the process listening on 127.0.0.1:port
// (loopback only — the engine never binds an external interface by
// default). It returns the owning PID and its executable path.
//
// ok is false (with a nil error) when nothing is listening on the port.
// err is non-nil only when a listener exists but identity CANNOT be
// proven — callers must treat that as "foreign process, unproven" and
// refuse adoption (fail closed).
func ListeningProcess(port int) (pid int, exe string, ok bool, err error) {
	if port <= 0 || port > 65535 {
		return 0, "", false, errors.New("invalid port")
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
