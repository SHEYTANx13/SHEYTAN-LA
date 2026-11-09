package attachments

// attachments_pathsafety_test.go — v1.11.1 path-injection remediation
// (alerts at Manager.Delete's os.Remove pair and loadMeta's os.ReadFile):
// attachment ids arrive from API callers (untrusted) and are used as
// single path components under the attachment store. Every os.* sink
// takes its path from Manager.objectPath/metaPath, which FAIL CLOSED on
// an id outside the attachment-ID grammar. These tests pin the hostile
// spellings (traversal, absolute, drive/UNC, NUL, ADS colon, dotfiles)
// and prove that a malicious id leaves files OUTSIDE the store untouched
// and reports nothing, while a staged attachment still round-trips.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostileAttachmentIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:objects",         // drive-relative
		"C:\\Users\\evil",   // drive absolute
		"\\\\server\\share", // UNC
		"id:stream",         // Windows ADS colon
		"\x00evil",          // NUL
		" att",              // whitespace-padded
		"at t",              // embedded space (not in grammar)
		"att/id",            // embedded separator
		".hidden",           // leading dot
	}
}

func TestValidAttachmentIDGrammar(t *testing.T) {
	// The store-generated form is "a" + 24 hex chars.
	good := []string{"a0f2a1b2c3d4e5f6a7b8c9d0e1", "attachment-1", "aB_09-x"}
	for _, id := range good {
		if !validAttachmentID(id) {
			t.Errorf("attachment id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileAttachmentIDs() {
		if validAttachmentID(id) {
			t.Errorf("hostile attachment id %q must be rejected by the grammar", id)
		}
	}
}

func TestAttachmentPathBuildersFailClosed(t *testing.T) {
	m, err := NewManager(t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	for _, id := range hostileAttachmentIDs() {
		if p, err := m.objectPath(id); err == nil {
			t.Errorf("objectPath(%q) must fail closed, got %q", id, p)
		}
		if p, err := m.metaPath(id); err == nil {
			t.Errorf("metaPath(%q) must fail closed, got %q", id, p)
		}
	}
}

func TestHostileAttachmentIDOperationsLeaveTreeUntouched(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, Options{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	// A decoy file OUTSIDE the attachment store that a traversal payload
	// would delete or read if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileAttachmentIDs() {
		if _, ok := m.Get(id); ok {
			t.Errorf("Get(%q) must not resolve", id)
		}
		if m.Delete(id) {
			t.Errorf("Delete(%q) must report nothing existed", id)
		}
		if got := m.StagePath(id); got != "" {
			t.Errorf("StagePath(%q) must return no path, got %q", id, got)
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
		t.Errorf("hostile ids touched files outside the attachment store: %d entries", len(entries))
	}

	// The store itself holds only its objects/ and meta/ directories.
	storeEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read store dir: %v", err)
	}
	if len(storeEntries) != 2 {
		t.Errorf("hostile ids created entries inside the attachment store: %d", len(storeEntries))
	}
}

// TestStagedAttachmentStillRoundTrips pins the positive path: stage →
// get → stage path → delete with the checked path builders in place.
func TestStagedAttachmentStillRoundTrips(t *testing.T) {
	m, err := NewManager(t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	att, err := m.Add(context.Background(), "", "notes.txt", strings.NewReader("hello attachment"))
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if _, ok := m.Get(att.ID); !ok {
		t.Fatal("staged attachment must resolve by id")
	}
	stage := m.StagePath(att.ID)
	if stage == "" {
		t.Fatal("StagePath must return the object path for a staged attachment")
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("staged object missing: %v", err)
	}

	if !m.Delete(att.ID) {
		t.Fatal("delete must report the staged attachment existed")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("deleted object survived")
	}
}
