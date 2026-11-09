package artifacts

// taskmeta_pathsafety_test.go — v1.11.1 path-injection remediation
// (alerts at TaskRegistry.Create's version-archive MkdirAll/Rename and
// CleanupTask's RemoveAll): task ids arrive from API callers and agent
// tool arguments (untrusted) and are used as single path components
// under the task-artifacts root. Every path built from one FAILS CLOSED
// through validTaskID — traversal-looking ids are rejected, never
// sanitized. These tests pin the hostile spellings and prove that a
// malicious task id leaves files OUTSIDE the task-artifacts root
// untouched and returns an error instead.

import (
	"os"
	"path/filepath"
	"testing"
)

func hostileTaskIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:tasks",           // drive-relative
		"C:\\Users\\evil",   // drive absolute
		"\\\\server\\share", // UNC
		"task:stream",       // Windows ADS colon
		"\x00evil",          // NUL
		" task",             // whitespace-padded
		"ta sk",             // embedded space (not in grammar)
		"task/one",          // embedded separator
		".hidden",           // leading dot
	}
}

func TestValidTaskIDGrammar(t *testing.T) {
	good := []string{"task-1", "t", "task_2024-05-01_run", "TASK1"}
	for _, id := range good {
		if !validTaskID(id) {
			t.Errorf("task id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileTaskIDs() {
		if validTaskID(id) {
			t.Errorf("hostile task id %q must be rejected by the grammar", id)
		}
	}
}

func TestHostileTaskIDCreateAndCleanupLeaveTreeUntouched(t *testing.T) {
	dir := t.TempDir()
	r := NewTaskRegistry(dir)

	// A decoy file OUTSIDE the task-artifacts root that a traversal
	// payload would overwrite or delete if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.md")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, taskID := range hostileTaskIDs() {
		if _, err := r.Create(CreateRequest{
			TaskID:   taskID,
			Source:   "artifact_create",
			Filename: "x.md",
			Content:  []byte("x"),
		}); err == nil {
			t.Errorf("Create(taskID=%q) must be rejected", taskID)
		}
		if err := r.CleanupTask(taskID); err == nil {
			t.Errorf("CleanupTask(%q) must be rejected", taskID)
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
		t.Errorf("hostile task ids created files outside the artifacts root: %d entries", len(entries))
	}

	// Nothing was written under the task-artifacts root either.
	rootEntries, err := os.ReadDir(filepath.Join(dir, "task-artifacts"))
	if err == nil && len(rootEntries) != 0 {
		t.Errorf("hostile task ids created entries under the artifacts root: %d", len(rootEntries))
	}
}

// TestVersionArchiveStaysInsideTaskRoot pins the version-archival path
// (the MkdirAll/Rename pair the alert pointed at): a replacement must
// move the superseded file into the task's own .versions directory and
// never touch anything outside the task-artifacts root.
func TestVersionArchiveStaysInsideTaskRoot(t *testing.T) {
	dir := t.TempDir()
	r := NewTaskRegistry(dir)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "notes.md")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: "notes.md", Content: []byte("v1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: "notes.md", Content: []byte("v2")}); err != nil {
		t.Fatal(err)
	}

	// The superseded file was archived under the task's .versions
	// directory (its former path is now reused by v2 — the assertion is
	// on the archive, not on the vacated name).
	vdir := filepath.Join(dir, "task-artifacts", "task-1", ".versions")
	entries, err := os.ReadDir(vdir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("version archive missing under the task root: %v (%d entries)", err, len(entries))
	}
	archived, err := os.ReadFile(filepath.Join(vdir, entries[0].Name()))
	if err != nil || string(archived) != "v1" {
		t.Fatalf("archived version content wrong: %q err=%v", archived, err)
	}
	current, err := os.ReadFile(filepath.Join(dir, "task-artifacts", "task-1", "notes.md"))
	if err != nil || string(current) != "v2" {
		t.Fatalf("current version content wrong: %q err=%v", current, err)
	}

	// The outside decoy is untouched.
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "outside-original" {
		t.Fatalf("outside file disturbed: %q err=%v", got, err)
	}
	entries, _ = os.ReadDir(outsideDir)
	if len(entries) != 1 {
		t.Errorf("version archiving created files outside the artifacts root: %d entries", len(entries))
	}
}

// TestValidTaskStillRoundTrips pins the positive path end to end.
func TestValidTaskStillRoundTrips(t *testing.T) {
	r := NewTaskRegistry(t.TempDir())

	meta, err := r.Create(CreateRequest{TaskID: "task-1", RunID: "run-1", Source: "artifact_create", Filename: "report.md", Content: []byte("# ok")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := r.List("task-1"); len(got) != 1 {
		t.Fatalf("list: %+v", got)
	}
	if err := r.CleanupTask("task-1"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(meta.Path); !os.IsNotExist(err) {
		t.Fatal("cleaned artifact file survived")
	}
}
