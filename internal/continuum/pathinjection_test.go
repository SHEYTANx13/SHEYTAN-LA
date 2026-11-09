package continuum

// pathinjection_test.go — v1.11.1 path-injection remediation (adversarial
// regression tests for the continuum sidecar alerts): session ids are used
// as single path components under the sessions directory for the Framework
// sidecar files. A hostile id must be rejected with a clear error (or the
// documented nil fallback) and must never touch files outside the sessions
// directory.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func hostileSidecarIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:sessions",        // drive-relative
		"C:\\Users\\evil",   // drive absolute
		"\\\\server\\share", // UNC
		"id:stream",         // Windows ADS colon
		"\x00evil",          // NUL
		" evil",             // whitespace-padded
		"ev il",             // embedded space (not in grammar)
		"evil/id",           // embedded separator
		".hidden",           // leading dot
	}
}

func TestValidSidecarSessionIDGrammar(t *testing.T) {
	good := []string{
		"sess1",           // test-style legacy id
		"s1670000000ab",   // store-generated-ish
		"s_1670000000-AB", // grammar characters
	}
	for _, id := range good {
		if !validSidecarSessionID(id) {
			t.Errorf("session id %q must be accepted by the sidecar grammar", id)
		}
	}
	for _, id := range hostileSidecarIDs() {
		if validSidecarSessionID(id) {
			t.Errorf("hostile session id %q must be rejected by the sidecar grammar", id)
		}
	}
}

func TestFrameworkPathFailsClosed(t *testing.T) {
	dir := t.TempDir()
	for _, id := range hostileSidecarIDs() {
		p, err := frameworkPath(dir, id)
		if err == nil {
			t.Errorf("frameworkPath(%q) must fail closed, got path %q", id, p)
		}
		if p != "" {
			t.Errorf("frameworkPath(%q) must return no path on rejection", id)
		}
		if !errors.Is(err, errInvalidSidecarSessionID) {
			t.Errorf("rejection must wrap the invalid-session-id cause, got: %v", err)
		}
	}

	p, err := frameworkPath(dir, "sess1")
	if err != nil || p != filepath.Join(dir, "sess1.framework.json") {
		t.Fatalf("valid id must resolve to the sidecar name, got %q err=%v", p, err)
	}
}

func TestHostileSidecarOperationsLeaveTreeUntouched(t *testing.T) {
	dir := t.TempDir()

	// A decoy OUTSIDE the sessions directory that a traversal payload
	// would overwrite if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.framework.json")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileSidecarIDs() {
		if err := SaveFramework(dir, id, &Framework{Version: FrameworkVersion}); !errors.Is(err, errInvalidSidecarSessionID) {
			t.Errorf("SaveFramework(%q) must fail with the invalid-session-id cause, got %v", id, err)
		}
		if fw := LoadFramework(dir, id); fw != nil {
			t.Errorf("LoadFramework(%q) must fall back to nil for a hostile id", id)
		}
	}

	// The outside decoy is byte-identical and nothing new appeared outside.
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("outside file vanished: %v", err)
	}
	if string(got) != "outside-original" {
		t.Errorf("outside file content changed: %q", got)
	}
	entries, err := os.ReadDir(outsideDir)
	if err != nil {
		t.Fatalf("read outside dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("hostile ids created files outside the sessions directory: %d entries", len(entries))
	}

	// Nothing was written into the sessions directory either.
	if side, err := os.ReadDir(dir); err == nil && len(side) != 0 {
		t.Errorf("hostile ids left entries in the sessions directory: %v", side)
	}
}

func TestSidecarFlowsStillWork(t *testing.T) {
	// The positive path pins the contract the remediation must preserve:
	// a valid id round-trips through Save/Load with the checked path
	// builder in place.
	dir := t.TempDir()
	fw := &Framework{Version: FrameworkVersion, Summary: "chapter one"}
	if err := SaveFramework(dir, "sess1", fw); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := LoadFramework(dir, "sess1"); got == nil || got.Summary != "chapter one" {
		t.Fatalf("load did not round-trip: %+v", got)
	}
}
