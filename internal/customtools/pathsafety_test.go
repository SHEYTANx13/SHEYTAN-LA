package customtools

// pathsafety_test.go — v1.11.1 path-injection remediation (alerts at the
// Store save/remove sinks and the TaskStore mkdir/write/rename/removeall
// sinks): tool definition ids and task ids arrive from API callers
// (untrusted) and are used as single path components under the
// custom-tools root. Every path FAILS CLOSED through validToolID (the
// ONE grammar, same as internal/sessions) — hostile ids produce errors,
// never paths. These tests pin the hostile spellings and prove that a
// malicious id leaves files OUTSIDE the custom-tools root untouched,
// while a saved definition and a task-scoped tool still round-trip.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostileToolIDs() []string {
	return []string{
		"",                  // empty
		".",                 // dot
		"..",                // parent
		"../escape",         // POSIX traversal
		"..\\escape",        // Windows traversal
		"a/../escape",       // nested traversal
		"/etc/passwd",       // POSIX absolute
		"\\Windows\\evil",   // Windows rooted
		"C:tools",           // drive-relative
		"C:\\Users\\evil",   // drive absolute
		"\\\\server\\share", // UNC
		"id:stream",         // Windows ADS colon
		"\x00evil",          // NUL
		" tool",             // whitespace-padded
		"to ol",             // embedded space (not in grammar)
		"tool/id",           // embedded separator
		".hidden",           // leading dot
	}
}

func TestValidToolIDGrammar(t *testing.T) {
	good := []string{
		newID(),       // store-generated form ("ct-<hex>")
		"task-loop-1", // ordinary task id
		"ct-1a2b3c4d5e6f7788",
		"run_2024-05-01",
	}
	for _, id := range good {
		if !validToolID(id) {
			t.Errorf("id %q must be accepted by the grammar", id)
		}
	}
	for _, id := range hostileToolIDs() {
		if validToolID(id) {
			t.Errorf("hostile id %q must be rejected by the grammar", id)
		}
	}
}

func TestToolPathBuildersFailClosed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tasks, err := NewTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("new task store: %v", err)
	}

	for _, id := range hostileToolIDs() {
		if p, err := store.pathFor(id); err == nil {
			t.Errorf("Store.pathFor(%q) must fail closed, got %q", id, p)
		}
		if p, err := tasks.taskDir(id); err == nil {
			t.Errorf("TaskStore.taskDir(%q) must fail closed, got %q", id, p)
		}
		if p, err := tasks.taskFilePath(id, "ct-0000000000000000"); err == nil {
			t.Errorf("TaskStore.taskFilePath(taskID=%q, …) must fail closed, got %q", id, p)
		}
		if p, err := tasks.taskFilePath("task-1", id); err == nil {
			t.Errorf("TaskStore.taskFilePath(…, id=%q) must fail closed, got %q", id, p)
		}
	}
}

func TestHostileToolIDOperationsLeaveTreeUntouched(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tasks, err := NewTaskStore(dataDir)
	if err != nil {
		t.Fatalf("new task store: %v", err)
	}

	// A decoy file OUTSIDE the custom-tools root that a traversal payload
	// would overwrite or delete if containment failed.
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.json")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	for _, id := range hostileToolIDs() {
		// Global store: a caller-supplied hostile id is refused, and a
		// hostile Delete is a no-op (it can never be in the cache).
		if err := store.Save(&Definition{ID: id, Name: "x"}); err == nil {
			t.Errorf("Save(hostile id %q) must fail closed", id)
		}
		if store.Delete(id) {
			t.Errorf("Delete(%q) must be a no-op", id)
		}

		// Task store: a hostile task id never reaches the filesystem.
		if _, err := tasks.CreateTaskTool(id, "run-1", validCommandDef(t)); err == nil {
			t.Errorf("CreateTaskTool(taskID=%q) must fail closed", id)
		}
		if err := tasks.CleanupTask(id); err == nil {
			t.Errorf("CleanupTask(%q) must fail closed", id)
		}
		if got := tasks.ListForTask(id); len(got) != 0 {
			t.Errorf("ListForTask(%q) must be empty", id)
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
		t.Errorf("hostile ids touched files outside the custom-tools root: %d entries", len(entries))
	}

	// The task-scoped layer holds only its own root directory.
	taskRootEntries, err := os.ReadDir(filepath.Join(dataDir, "custom-tools", "task-scoped"))
	if err == nil && len(taskRootEntries) != 0 {
		t.Errorf("hostile task ids created directories under the task-scoped root: %d", len(taskRootEntries))
	}
}

// TestValidDefinitionAndTaskToolStillRoundTrip pins the positive paths:
// a saved definition lands at <dir>/<id>.json, a task-scoped tool lands
// at <root>/<taskID>/<id>.json, and both delete cleanly.
func TestValidDefinitionAndTaskToolStillRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	def := validCommandDef(t)
	if err := store.Save(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.HasPrefix(def.ID, "ct-") {
		t.Fatalf("generated id %q must keep the store form", def.ID)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "custom-tools", def.ID+".json")); err != nil {
		t.Fatalf("definition file missing: %v", err)
	}
	if !store.Delete(def.ID) {
		t.Fatal("delete must report the definition existed")
	}

	tasks, err := NewTaskStore(dataDir)
	if err != nil {
		t.Fatalf("new task store: %v", err)
	}
	td, err := tasks.CreateTaskTool("task-loop-1", "run-1", validCommandDef(t))
	if err != nil {
		t.Fatalf("create task tool: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "custom-tools", "task-scoped", "task-loop-1", td.ID+".json")); err != nil {
		t.Fatalf("task tool file missing: %v", err)
	}
	if err := tasks.Approve(td.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := tasks.CleanupTask("task-loop-1"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "custom-tools", "task-scoped", "task-loop-1")); !os.IsNotExist(err) {
		t.Fatal("cleaned task scope survived")
	}
}
