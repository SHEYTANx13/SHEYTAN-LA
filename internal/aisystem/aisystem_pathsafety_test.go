package aisystem

// aisystem_pathsafety_test.go — v1.11.1 path-injection remediation (work
// order §4.2, alerts at aisystem.go writeAtomic CreateTemp/Rename and
// Store.Delete): system ids arrive from API callers and are used as
// single path components under the ai-systems directory. Every os.* sink
// for a system document takes its path from Store.systemPath, which
// FAILS CLOSED on an id outside the system-ID grammar. These tests pin
// the hostile spellings (traversal, absolute, drive/UNC, NUL, ADS colon,
// dotfiles) and prove that a malicious operation leaves files OUTSIDE
// the ai-systems directory untouched and returns an error instead.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostileSystemIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:systems",         // drive-relative
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

func TestValidSystemIDGrammar(t *testing.T) {
	good := []string{
		ReservedID,   // the reserved default system
		"sys1abcdef", // store-generated form
		"sys_1670000000-AB",
	}
	for _, id := range good {
		if !validSystemID(id) {
			t.Errorf("system id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileSystemIDs() {
		if validSystemID(id) {
			t.Errorf("hostile system id %q must be rejected by the grammar", id)
		}
	}
}

func TestSystemPathFailsClosed(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	for _, id := range hostileSystemIDs() {
		p, err := s.systemPath(id)
		if err == nil {
			t.Errorf("systemPath(%q) must fail closed, got path %q", id, p)
		}
		if p != "" {
			t.Errorf("systemPath(%q) must return no path on rejection", id)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "invalid system id") {
			t.Errorf("rejection must name the invalid id cause, got: %v", err)
		}
	}
}

func TestHostileSystemIDOperationsLeaveTreeUntouched(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// A decoy file OUTSIDE the ai-systems directory that a traversal
	// payload would overwrite if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.json")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileSystemIDs() {
		if _, err := s.Get(id); err == nil {
			t.Errorf("Get(%q) must fail", id)
		}
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q) must fail", id)
		}
		if _, err := s.Select(id); err == nil {
			t.Errorf("Select(%q) must fail", id)
		}
		if _, err := s.Update(id, func(sys *System) error { return nil }); err == nil {
			t.Errorf("Update(%q) must fail", id)
		}
		if _, err := s.SnapshotOf(id); err == nil {
			t.Errorf("SnapshotOf(%q) must fail", id)
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
		t.Errorf("hostile ids created files outside the ai-systems directory: %d entries", len(entries))
	}
}

// TestValidSystemFlowsStillWork pins the positive path: a created system
// round-trips through Select/Update/SnapshotOf/Delete with the checked
// path builder in place, and the default system still boots.
func TestValidSystemFlowsStillWork(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	created, err := s.Create(System{Name: "Coder", ApprovalPolicy: ApprovalAskRisky})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Select(created.SystemID); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := s.Update(created.SystemID, func(sys *System) error {
		sys.Instructions = "be precise"
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := s.SnapshotOf(created.SystemID); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := s.Delete(created.SystemID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The reserved default is still present and active.
	if _, err := s.Active(); err != nil {
		t.Fatalf("active default after delete: %v", err)
	}
}
