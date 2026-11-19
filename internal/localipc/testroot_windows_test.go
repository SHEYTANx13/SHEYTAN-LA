//go:build windows

package localipc

import "testing"

// newShortSocketRoot on Windows: the transport is named pipes and there
// is no sun_path capacity to manage, so the canonical temp root is fine.
// The unix-only twin (testroot_test.go) owns the real short-root
// contract; this stub exists so cross-platform dial contract tests
// compile unchanged on Windows.
func newShortSocketRoot(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}
