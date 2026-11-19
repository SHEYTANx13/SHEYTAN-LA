//go:build !windows

package localipc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// newShortSocketRoot creates a private, SHORT socket-path root directly
// under a short system temp path (/tmp on darwin — NOT the long nested
// t.TempDir() tree macOS materializes under /var/folders/...), with an
// unpredictable suffix, mode 0700, verified directory type, and removal
// via t.Cleanup.
//
// Run 44378562071 §4.4.4: tests must never use a long t.TempDir() root as
// a socket path base on darwin — the complete path plus nested test names
// overflows the 104-byte sun_path limit and the bind fails with EINVAL
// before any assertion runs. The helper FAILS (never skips) if even this
// deliberately short root cannot host a valid socket path: there is no
// environment in the supported matrix where that is legitimate.
func newShortSocketRoot(t *testing.T) string {
	t.Helper()

	base := "/tmp" // short on darwin (/tmp -> /private/tmp, resolved by the kernel on bind)

	if runtime.GOOS != "darwin" {
		base = os.TempDir()
	}

	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random suffix: %v", err)
	}

	dir := filepath.Join(base, "sla-ipc-"+hex.EncodeToString(suffix))

	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("short socket root %s: %v", dir, err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat short socket root: %v", err)
	}

	if !st.IsDir() {
		t.Fatalf("short socket root %s is not a directory", dir)
	}

	if st.Mode().Perm() != 0o700 {
		t.Fatalf("short socket root %s must be private (mode %04o, want 0700)", dir, st.Mode().Perm())
	}

	if fileSt, ok := st.Sys().(*syscall.Stat_t); ok && fileSt.Uid != uint32(os.Getuid()) {
		t.Fatalf("short socket root %s must be owned by the current user (uid %d)", dir, os.Getuid())
	}

	// Defensive: prove the environment can actually host a socket path of
	// the canonical short basename. A failure here is a real environment
	// defect, reported as such — never a skip.
	probe := filepath.Join(dir, "probe.sock")
	if err := ValidateUnixSocketPath(probe); err != nil {
		t.Fatalf("environment cannot host a short unix socket path: %v", err)
	}

	return dir
}

// shortSocketPath returns a validated short socket path with the given
// basename inside the short socket root.
func shortSocketPath(t *testing.T, base string) string {
	t.Helper()

	p := filepath.Join(newShortSocketRoot(t), base)
	if err := ValidateUnixSocketPath(p); err != nil {
		t.Fatalf("short socket path: %v", err)
	}

	return p
}

// TestUnixSocketPathLimitConstants pins the kernel capacities the
// validator derives its answers from (XNU sys/un.h: 104 including NUL;
// Linux: 108 including NUL). Pure function: asserted on every platform.
func TestUnixSocketPathLimitConstants(t *testing.T) {
	if got := unixSocketPathLimit("darwin"); got != 104 {
		t.Fatalf("darwin sun_path limit = %d, want 104 (including NUL)", got)
	}

	if got := unixSocketPathLimit("linux"); got != 108 {
		t.Fatalf("linux sun_path limit = %d, want 108 (including NUL)", got)
	}
}

// TestValidateUnixSocketPathExactByteBoundary proves the validator
// accepts the exact kernel boundary and rejects one byte beyond it —
// byte-counted, including the terminating NUL — using THIS platform's
// real limit, so the boundary is exercised natively on darwin runners
// too.
func TestValidateUnixSocketPathExactByteBoundary(t *testing.T) {
	limit := unixSocketPathLimit(runtime.GOOS)

	// Exactly at the boundary: path bytes (limit-1) + NUL == limit -> OK.
	at := "/" + strings.Repeat("a", limit-2)
	if err := ValidateUnixSocketPath(at); err != nil {
		t.Fatalf("path of exactly %d bytes (+NUL = %d) must be accepted: %v", limit-1, limit, err)
	}

	// One byte beyond: path bytes (limit) + NUL = limit+1 -> typed error.
	over := "/" + strings.Repeat("a", limit-1)
	err := ValidateUnixSocketPath(over)
	if !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("path of %d bytes (+NUL = %d) must fail with ErrSocketPathTooLong, got %v", limit, limit+1, err)
	}
}

// TestValidateUnixSocketPathUnicodeBytes proves the limit counts UTF-8
// BYTES, not runes: a multibyte path consumes its byte length against the
// kernel capacity.
func TestValidateUnixSocketPathUnicodeBytes(t *testing.T) {
	limit := unixSocketPathLimit(runtime.GOOS)

	// Two-byte runes: limit/2 runes produce exactly `limit-1` bytes of
	// payload after the leading slash when (limit-2) is even.
	n := (limit - 2) / 2
	at := "/" + strings.Repeat("é", n)

	if got := len(at); got != limit-1 {
		t.Fatalf("fixture construction: path is %d bytes, want %d", got, limit-1)
	}

	if err := ValidateUnixSocketPath(at); err != nil {
		t.Fatalf("multibyte path of exactly %d bytes (+NUL = %d) must be accepted: %v", limit-1, limit, err)
	}

	// One more rune crosses the byte boundary.
	over := "/" + strings.Repeat("é", n+1)
	if err := ValidateUnixSocketPath(over); !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("multibyte path over the byte boundary must fail with ErrSocketPathTooLong, got %v", err)
	}
}

// TestValidateUnixSocketPathRejections pins the degenerate inputs.
func TestValidateUnixSocketPathRejections(t *testing.T) {
	if err := ValidateUnixSocketPath(""); err == nil {
		t.Fatal("empty socket path must be rejected")
	}

	invalid := "/" + string([]byte{0xC3, 0x28}) // invalid UTF-8 sequence
	if err := ValidateUnixSocketPath(invalid); err == nil {
		t.Fatal("non-UTF-8 socket path must be rejected")
	}
}
