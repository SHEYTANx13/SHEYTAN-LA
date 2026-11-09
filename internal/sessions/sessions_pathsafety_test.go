package sessions

// sessions_pathsafety_test.go — v1.11.1 path-injection remediation (run
// 42502043636 work order §4.2, 25 open go/path-injection alerts across
// sessions.go/summary.go): session ids arrive from API callers and are
// used as single path components under the sessions directory. Every
// os.* sink takes its path from Store.artifactPath, which FAILS CLOSED
// on an id outside the session-ID grammar. These tests pin the hostile
// spellings (traversal, absolute, drive/UNC, NUL, ADS colon, dotfiles)
// and prove that a malicious operation leaves files OUTSIDE the sessions
// directory untouched and returns an error instead.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostileIDs() []string {
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

func TestValidSessionIDGrammar(t *testing.T) {
	good := []string{
		newSessionID(),    // store-generated form
		"s1abcdef",        // legacy-ish
		"s_1670000000-AB", // grammar characters
	}
	for _, id := range good {
		if !validSessionID(id) {
			t.Errorf("session id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileIDs() {
		if validSessionID(id) {
			t.Errorf("hostile session id %q must be rejected by the grammar", id)
		}
	}
}

func TestArtifactPathFailsClosed(t *testing.T) {
	s := New(t.TempDir())
	for _, suffix := range []string{".json", ".activities.jsonl", ".summary.json"} {
		for _, id := range hostileIDs() {
			p, err := s.artifactPath(id, suffix)
			if err == nil {
				t.Errorf("artifactPath(%q, %q) must fail closed, got path %q", id, suffix, p)
			}
			if p != "" {
				t.Errorf("artifactPath(%q, %q) must return no path on rejection", id, suffix)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "invalid session id") {
				t.Errorf("rejection must name the invalid id cause, got: %v", err)
			}
		}
	}
}

func TestHostileIDOperationsLeaveTreeUntouched(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	// A decoy file OUTSIDE the sessions directory that a traversal payload
	// would overwrite if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.txt")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileIDs() {
		if _, err := s.Get(id); err == nil {
			t.Errorf("Get(%q) must fail", id)
		}
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q) must fail", id)
		}
		if err := s.AppendActivity(id, ActivityEntry{Type: "x"}); err == nil {
			t.Errorf("AppendActivity(%q) must fail", id)
		}
		if err := s.SaveSummary(&SessionSummary{SessionID: id}); err == nil {
			t.Errorf("SaveSummary(%q) must fail", id)
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
}

// TestValidSessionFlowsStillWork pins the positive path: the generated
// id round-trips through Create/Save/Get/AppendActivity/Delete with the
// checked path builders in place.
func TestValidSessionFlowsStillWork(t *testing.T) {
	s := New(t.TempDir())
	sess := s.Create()
	if err := s.Save(sess); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.AppendActivity(sess.ID, ActivityEntry{Type: "tool", Caption: "ok"}); err != nil {
		t.Fatalf("append activity: %v", err)
	}
	if _, err := s.Get(sess.ID); err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := s.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
