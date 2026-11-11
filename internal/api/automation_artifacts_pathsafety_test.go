package api

// automation_artifacts_pathsafety_test.go — v1.11.1 Windows path repair:
// the automation artifacts surface must answer hostile creation requests
// with the documented 400 response (never a handler panic / dropped
// connection / EOF) and must never mutate the filesystem outside the
// task-artifacts root. Companion to the registry-level tests in
// internal/artifacts/taskmeta_windows_test.go.

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestAutomationArtifactsHostileRequestsControlled posts hostile
// filenames and task ids through the HTTP surface and asserts a
// controlled 400 for every one of them plus an untouched outside
// sentinel.
func TestAutomationArtifactsHostileRequestsControlled(t *testing.T) {
	server, _ := newTestServer(t)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel.txt")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}

	hostileFilenames := []string{
		"", " ", ".", "..",
		"../escape.md", `..\escape.md`,
		"a/../../escape.md",
		"/etc/passwd", `\Windows\evil`,
		`\\server\share\x.md`, `\\?\C:\evil\x.md`, `\\.\PhysicalDrive0`,
		"C:\\Users\\evil\\x.md", "C:evil.md", "task:stream.md",
		"sub\x00x.md",
	}
	for _, name := range hostileFilenames {
		code, _ := autoPost(t, server.URL+"/api/automation/artifacts", map[string]any{
			"taskId": "task-art", "filename": name, "content": "x",
		})
		if code != http.StatusBadRequest {
			t.Errorf("hostile filename %q: HTTP %d, want 400 (controlled error, no panic/EOF)", name, code)
		}
	}

	hostileTaskIDs := []string{"", ".", "..", "../escape", "a/b", `a\b`, "C:t", "task:1", "\x00t", " task"}
	for _, id := range hostileTaskIDs {
		code, _ := autoPost(t, server.URL+"/api/automation/artifacts", map[string]any{
			"taskId": id, "filename": "notes.md", "content": "x",
		})
		if code != http.StatusBadRequest {
			t.Errorf("hostile task id %q: HTTP %d, want 400", id, code)
		}
	}

	// Windows-only reserved device names through the API (the registry
	// gate is Windows-host-scoped; POSIX pins these as legal literals).
	if runtime.GOOS == "windows" {
		for _, name := range []string{"NUL", "CON.md", "COM1", "notes.md."} {
			code, _ := autoPost(t, server.URL+"/api/automation/artifacts", map[string]any{
				"taskId": "task-art", "filename": name, "content": "x",
			})
			if code != http.StatusBadRequest {
				t.Errorf("reserved name %q on windows: HTTP %d, want 400", name, code)
			}
		}
	}

	// The sentinel is byte-identical; nothing appeared outside.
	if got, err := os.ReadFile(outside); err != nil || string(got) != "untouched" {
		t.Fatalf("outside sentinel disturbed: %q err=%v", got, err)
	}
	if entries, err := os.ReadDir(outsideDir); err != nil || len(entries) != 1 {
		t.Errorf("hostile API requests wrote outside the data root: %d entries (%v)", len(entries), err)
	}

	// The registry itself stayed clean: no artifact was registered.
	if _, arts := autoGetAny(t, server.URL+"/api/automation/artifacts?task=task-art"); arts != nil {
		if lst, ok := arts.([]any); ok && len(lst) != 0 {
			t.Errorf("hostile requests registered artifacts: %v", arts)
		}
	}
}

// TestAutomationArtifactsNestedAndVersions: nested logical destinations
// round trip through the HTTP surface and replacement bumps the version
// while the archive stays inside the task root.
func TestAutomationArtifactsNestedAndVersions(t *testing.T) {
	server, cfg := newTestServer(t)

	code, art1 := autoPost(t, server.URL+"/api/automation/artifacts", map[string]any{
		"taskId": "task-nest", "filename": "sub/dir/report.md", "content": "v1", "title": "T",
	})
	if code != http.StatusOK {
		t.Fatalf("nested create = %d", code)
	}
	code, art2 := autoPost(t, server.URL+"/api/automation/artifacts", map[string]any{
		"taskId": "task-nest", "filename": "sub/dir/report.md", "content": "v2",
	})
	if code != http.StatusOK {
		t.Fatalf("nested replace = %d", code)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.DataDir, "task-artifacts", "task-nest", ".versions"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("api-driven version archive missing: %v (%d entries)", err, len(entries))
	}
	if name := entries[0].Name(); name != "report.v1.md" {
		t.Errorf("archived name = %q", name)
	}
	if art2["version"] == nil || intOf(art2["version"]) != 2 {
		t.Errorf("nested replacement version = %v, want 2", art2["version"])
	}
	_ = art1
}
