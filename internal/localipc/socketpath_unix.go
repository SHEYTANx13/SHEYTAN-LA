//go:build !windows

// OS-aware Unix socket path validation (run 44378562071 repair, §4.4).
//
// Darwin's sockaddr_un.sun_path is 104 bytes INCLUDING the terminating
// NUL; Linux is 108. A path that fits the Linux limit silently fails
// with EINVAL on macOS the moment the complete path plus NUL exceeds the
// kernel capacity — the exact failure class observed on hosted macOS
// runners, whose materialized t.TempDir() roots live under long
// /var/folders/... trees and overflow once nested test names join the
// path.
//
// Both the listener and the dialer validate BEFORE any syscall and
// return this typed, descriptive error — never a raw EINVAL from bind.
// The limit is never raised and no platform's tests are skipped: the
// environment is fixed, not the gate.
package localipc

import (
	"errors"
	"fmt"
	"runtime"
	"unicode/utf8"
)

// ErrSocketPathTooLong is returned before any syscall when the complete
// socket path (UTF-8 bytes plus the terminating NUL the kernel stores in
// sun_path) exceeds the platform's sockaddr_un capacity.
var ErrSocketPathTooLong = errors.New("localipc: unix socket path exceeds the kernel sockaddr_un limit")

// unixSocketPathLimit returns the maximum number of bytes a pathname
// AF_UNIX socket address may occupy on the named GOOS, including the
// terminating NUL (the kernel copies sun_path up to and including the
// first NUL byte). darwin 104, linux 108 — the XNU and Linux headers'
// own constants, never raised by this code.
func unixSocketPathLimit(goos string) int {
	if goos == "darwin" {
		return 104
	}

	return 108
}

// ValidateUnixSocketPath verifies that the complete endpoint path fits
// the current platform's sun_path limit (UTF-8 byte count + terminating
// NUL) and is well-formed for a filesystem AF_UNIX socket. It never
// touches the filesystem and never calls into the OS socket layer.
//
// Unicode correctness: the limit counts BYTES, not runes — a path of
// multibyte characters consumes its byte length against the kernel
// capacity exactly like ASCII does.
func ValidateUnixSocketPath(path string) error {
	if path == "" {
		return errors.New("localipc: unix socket path is empty")
	}

	if !utf8.ValidString(path) {
		return errors.New("localipc: unix socket path is not valid UTF-8")
	}

	limit := unixSocketPathLimit(runtime.GOOS)
	need := len(path) + 1 // kernel stores the path bytes plus the terminating NUL

	if need > limit {
		return fmt.Errorf("%w: path is %d bytes (+NUL = %d) but %s allows at most %d bytes including NUL (104 on darwin): %.80q",
			ErrSocketPathTooLong, len(path), need, runtime.GOOS, limit, path)
	}

	return nil
}
