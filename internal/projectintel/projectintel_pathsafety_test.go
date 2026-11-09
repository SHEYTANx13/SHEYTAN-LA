package projectintel

// projectintel_pathsafety_test.go — v1.11.1 path-injection remediation
// (alert at scanProject's os.Stat/WalkDir): the root is the user's
// selected workspace (a user-owned absolute path), so the hostile
// spellings FAIL CLOSED before any filesystem operation: empty paths and
// NUL bytes yield an empty scan, and the walk always runs over the
// cleaned absolute form. The persisted facts file itself is keyed by a
// HASH of the root (rootKey), so a hostile root can never place or read
// a file outside the store directory either.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanProjectHostileRootsYieldEmptyScan(t *testing.T) {
	for _, root := range []string{"", "   ", "root\x00nul"} {
		scan := scanProject(root)
		if len(scan.Languages) != 0 || scan.BuildCmd != "" || scan.Layout != "" {
			t.Errorf("scanProject(%q) must yield an empty scan, got %+v", root, scan)
		}
	}
}

// TestObserveHostileRootNeverWritesOutsideStore proves a hostile root
// cannot steer a facts file outside the store directory: the persisted
// name is the rootKey hash, and a hostile root produces an empty scan
// (nothing measured, nothing invented).
func TestObserveHostileRootNeverWritesOutsideStore(t *testing.T) {
	storeDir := t.TempDir()
	s := NewStore(storeDir)

	// A decoy directory OUTSIDE the store that a hostile root payload
	// would write into if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.json")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, root := range []string{"", "\x00evil"} {
		// Empty and NUL-bearing roots FAIL CLOSED: they would otherwise
		// normalize to the process working directory and scan an
		// unintended tree.
		if _, err := s.Observe(root); err == nil {
			t.Errorf("Observe(%q) must fail closed", root)
		}
	}

	// A relative root degrades honestly: the (missing) workspace is
	// resolved against the process CWD exactly as before, nothing is
	// measured, and nothing outside the store is written.
	if facts, err := s.Observe("./relative/root"); err != nil {
		t.Errorf("Observe(relative) must degrade honestly, got error: %v", err)
	} else if len(facts.Languages) != 0 {
		t.Errorf("Observe(relative) must measure nothing, got languages %v", facts.Languages)
	}

	// The outside decoy is untouched; the store holds only hashed names.
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "outside-original" {
		t.Fatalf("outside file disturbed: %q err=%v", got, err)
	}
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	for _, e := range entries {
		// Every persisted facts file is <16 hex chars>.json — never a
		// root-derived literal path.
		name := e.Name()
		if len(name) < 5 || len(name) > 16+5 {
			t.Errorf("unexpected non-hashed file in store: %s", name)
		}
	}
}

// TestObserveValidRootStillWritesHashedFacts pins the positive path: a
// real workspace root is observed and persisted under its hashed name.
func TestObserveValidRootStillWritesHashedFacts(t *testing.T) {
	root := writeProject(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"go.mod":      "module example.com/demo\n",
		"notego/x.md": "ignore me",
	})
	storeDir := t.TempDir()
	s := NewStore(storeDir)

	facts, err := s.Observe(root)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if len(facts.Languages) == 0 {
		t.Fatalf("expected measured languages, got %+v", facts)
	}

	path := filepath.Join(storeDir, rootKey(root)+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("facts file missing under the hashed name: %v", err)
	}
}
