package gitclone

// clone_pathsafety_test.go — v1.11.1 path-injection remediation (alerts
// at ValidateDestination's Stat/Stat/ReadDir): the destination arrives
// from the API clone request (untrusted). It is a user-CHOSEN absolute
// folder (a desktop install may clone anywhere), so it cannot be
// contained to a store root — but the hostile spellings FAIL CLOSED
// before any filesystem call: empty paths, NUL bytes and non-absolute
// destinations are rejected as invalid-destination, and every check runs
// over the cleaned absolute form.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDestinationHostileInputsFailClosed(t *testing.T) {
	cases := []struct {
		name string
		dest string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"NUL", "/tmp/dest\x00evil"},
		{"relative", "relative/repo"},
		{"dot", "."},
		{"dotdot", ".."},
		{"drive-relative", "C:repo"},
		{"windows rooted", "\\Windows\\evil"},
		{"UNC", "\\\\server\\share"},
	}

	for _, tc := range cases {
		err := ValidateDestination(tc.dest)
		if err == nil {
			t.Errorf("%s: ValidateDestination(%q) must be rejected", tc.name, tc.dest)
			continue
		}
		if got := ClassifyError(err); got != ErrInvalidDest {
			t.Errorf("%s: hostile destination %q must classify as invalid-destination, got %s", tc.name, tc.dest, got)
		}
	}
}

// TestValidateDestinationTraversalDoesNotTouchOutsideTree proves the
// rejection happens BEFORE any filesystem call: a traversal payload that
// would resolve to a decoy OUTSIDE the chosen parent is refused and the
// decoy is untouched.
func TestValidateDestinationTraversalDoesNotTouchOutsideTree(t *testing.T) {
	parent := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.md")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	dest := filepath.Join(parent, "..", filepath.Base(outsideDir), "escape.md")
	if err := ValidateDestination(dest); err == nil {
		t.Fatal("a destination resolving outside the chosen parent must be rejected")
	}

	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "outside-original" {
		t.Fatalf("outside decoy disturbed: %q err=%v", got, err)
	}
}

// TestValidateDestinationValidForms pins the positive paths: a fresh
// destination under an existing parent is accepted, an existing EMPTY
// directory is accepted, and errors stay classified for the occupied
// forms (existing file, non-empty directory, missing parent, relative).
func TestValidateDestinationValidForms(t *testing.T) {
	parent := t.TempDir()

	if err := ValidateDestination(filepath.Join(parent, "fresh-repo")); err != nil {
		t.Fatalf("fresh destination rejected: %v", err)
	}

	empty := filepath.Join(parent, "empty-dir")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDestination(empty); err != nil {
		t.Fatalf("empty destination rejected: %v", err)
	}

	busy := filepath.Join(parent, "busy")
	if err := os.MkdirAll(busy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(busy, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDestination(busy); err == nil || ClassifyError(err) != ErrDestExists {
		t.Fatalf("non-empty destination must classify destination-exists, got %v", err)
	}

	if err := ValidateDestination(filepath.Join(parent, "missing", "repo")); err == nil {
		t.Fatal("missing parent accepted")
	}

	err := ValidateDestination("relative/repo")
	if err == nil {
		t.Fatal("relative destination accepted")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative rejection must explain the absolute requirement, got: %v", err)
	}
}
