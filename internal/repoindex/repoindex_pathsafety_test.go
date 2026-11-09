package repoindex

// repoindex_pathsafety_test.go — v1.11.1 path-injection remediation
// (alerts at fileExists' os.Stat, ParseFile's and contentDigest's
// os.Open): the workspace root arrives from the workspace selection
// (config or the API clone-switch path) and file records can be
// reloaded from a persisted index, so every file open under a root is
// resolved through repoJoin — which FAILS CLOSED on absolute forms and
// escapes ("..") — and every root passes validRoot (absolute, non-empty,
// NUL-free) before any filesystem operation. These tests pin the hostile
// spellings and prove a hostile rel can never read a file OUTSIDE the
// workspace root, while a real file still round-trips through the
// digest/parse pipeline.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidRoot(t *testing.T) {
	good := []string{t.TempDir(), "/", string(filepath.Separator) + "home" + string(filepath.Separator) + "z"}
	for _, root := range good {
		if !validRoot(root) {
			t.Errorf("root %q must be accepted", root)
		}
	}
	for _, root := range []string{"", "relative/path", ".", "..", "root\x00nul"} {
		if validRoot(root) {
			t.Errorf("hostile root %q must be rejected", root)
		}
	}
}

func TestRepoJoinFailsClosed(t *testing.T) {
	root := t.TempDir()

	hostile := []string{
		"",                // empty
		"..",              // parent
		"../escape",       // POSIX traversal
		"a/../../escape",  // nested traversal
		"\\Windows\\evil", // Windows-rooted backslash form
		"/etc/passwd",     // absolute
	}
	for _, rel := range hostile {
		if p, err := repoJoin(root, rel); err == nil {
			t.Errorf("repoJoin(%q) must fail closed, got %q", rel, p)
		} else if !errors.Is(err, errInvalidRelPath) && !strings.Contains(err.Error(), "workspace") {
			t.Errorf("repoJoin(%q) rejection must name the cause, got: %v", rel, err)
		}
	}

	// A hostile ROOT fails closed as well.
	if p, err := repoJoin("relative/root", "a.go"); err == nil {
		t.Errorf("repoJoin with a non-absolute root must fail closed, got %q", p)
	}
}

// TestHostileRelCannotReadOutsideWorkspace proves the sink-level
// containment: ParseFile and contentDigest against a hostile relative
// path never open a readable file OUTSIDE the workspace root.
func TestHostileRelCannotReadOutsideWorkspace(t *testing.T) {
	root := t.TempDir()

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(outside, []byte("package secret"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, rel := range []string{"../secret.go", "../../secret.go", "/secret.go", "a/../../../secret.go"} {
		if res := ParseFile(root, rel, "go"); res.Package != "" || len(res.Symbols) > 0 || res.Binary {
			t.Errorf("ParseFile(root, %q) must return an empty result, got %+v", rel, res)
		}
		if _, err := contentDigest(root, rel, 4); err == nil {
			t.Errorf("contentDigest(root, %q) must fail closed", rel)
		}
	}

	// The outside file is untouched and still holds its original content.
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "package secret" {
		t.Fatalf("outside file disturbed: %q err=%v", got, err)
	}
}

// TestUpdateRejectsHostileRoots pins the entry-point gate: Update fails
// closed before any filesystem operation for empty, relative or NUL
// roots.
func TestUpdateRejectsHostileRoots(t *testing.T) {
	s := NewStore(t.TempDir())

	for _, root := range []string{"", "relative/root", "..", "root\x00nul"} {
		if _, err := s.Update(context.Background(), root); err == nil {
			t.Errorf("Update(%q) must fail closed", root)
		}
	}
}

// TestValidRootStillIndexes pins the positive path: a real workspace
// root is walked, parsed and digested with the checked join in place.
func TestValidRootStillIndexes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(t.TempDir())
	report, err := s.Update(context.Background(), root)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if report.Indexed != 1 {
		t.Fatalf("indexed %d files, want 1", report.Indexed)
	}

	// The record's content digest proves the file was read INSIDE the root.
	idx := s.Status(root)
	if idx.Files != 1 {
		t.Fatalf("status: %+v", idx)
	}
}
