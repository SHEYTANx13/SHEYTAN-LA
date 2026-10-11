package llm

// paths_test.go — v1.2.0: the ONE helper for comparing filesystem paths in
// tests. Runtime code reports OS-canonical paths (backslashes on Windows,
// resolved spellings), while tests used to build expectation strings with
// raw concatenation — the same semantic path compared two different ways
// ("loaded model = C:\...\models\fake-model.gguf, want C:\.../models/
// fake-model.gguf"). Never compare raw path strings again; call this.

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sameFilePath reports whether two filesystem paths denote the same
// location under OS-aware canonicalization: both sides are made absolute
// and cleaned, and on Windows the comparison is case-insensitive (NTFS is
// case-preserving, case-insensitive).
//
// It returns false (never panics) for paths that cannot be resolved, and
// failf provides the standardized assertion message.
func sameFilePath(a, b string) bool {
	a, err := filepath.Abs(a)
	if err != nil {
		return false
	}

	b, err = filepath.Abs(b)
	if err != nil {
		return false
	}

	a = filepath.Clean(a)
	b = filepath.Clean(b)

	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}

	return a == b
}

// assertSameFilePath fails the test when the two paths do not denote the
// same location, printing both raw spellings for diagnosis.
func assertSameFilePath(t *testing.T, got, want string) {
	t.Helper()

	if sameFilePath(got, want) {
		return
	}

	t.Fatalf("path mismatch (semantically different locations):\n got = %q\nwant = %q", got, want)
}
