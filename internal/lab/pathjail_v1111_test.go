package lab

// pathjail_v1111_test.go — v1.11.1 CodeQL go/path-injection repair:
// adversarial coverage for the ACTIVE-WORKSPACE jail.
//
// Every edit/read/execution target is bound to the workspace root twice:
// lexically (PathFor: filepath.Rel containment + resolved-existing-prefix
// symlink check) and at the I/O itself (os.Root — a symlink created inside
// the workspace can never redirect a read or a write outside it). These
// tests attack both layers and prove that refused operations leave files
// outside the workspace untouched.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// newJailFixture builds a workspace manager + one workspace from a small
// source tree, and returns the workspace handle plus an OUTSIDE victim
// directory (a separate temp dir, never inside the workspace root).
func newJailFixture(
	t *testing.T,
	files map[string]string,
) (*Workspace, string) {
	t.Helper()

	root := t.TempDir()
	source := filepath.Join(root, "source")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	for rel, content := range files {
		full := filepath.Join(source, rel)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mgr, err := NewWorkspaceManager(filepath.Join(root, "lab", "workspaces"))
	if err != nil {
		t.Fatalf("NewWorkspaceManager: %v", err)
	}

	ws, err := mgr.Create(context.Background(), source)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	return ws, t.TempDir()
}

// newJailTool is newSafeEditTool plus the ACTIVE workspace path, so the
// test can plant escape symlinks inside the workspace itself.
func newJailTool(
	t *testing.T,
	files map[string]string,
) (*Tool, string, string) {
	t.Helper()

	root := t.TempDir()
	source := filepath.Join(root, "source")

	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	for rel, content := range files {
		full := filepath.Join(source, rel)

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	cfg.DataDir = root
	cfg.ModelsDir = filepath.Join(root, "models")
	cfg.SessionsDir = filepath.Join(root, "sessions")
	cfg.LabEnabled = true
	cfg.LabWorkspaceRoot = filepath.Join(root, "lab", "workspaces")
	cfg.LabCommandTimeoutSec = 5
	cfg.LabKeepWorkspaces = false
	cfg.LabAllowNetwork = false

	tool, err := NewTool(cfg)
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	startOutput, err := tool.Run(
		context.Background(),
		marshalLabTestArgs(t, map[string]any{
			"action": "start_task",
			"title":  "path jail test",
			"source": source,
		}),
	)
	if err != nil {
		t.Fatalf("start_task: %v", err)
	}

	taskID := extractTaskID(t, startOutput)

	// Exactly one task workspace exists in the fresh root.
	entries, err := os.ReadDir(filepath.Join(root, "lab", "workspaces"))
	if err != nil {
		t.Fatal(err)
	}

	wsPath := ""

	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "task-") {
			wsPath = filepath.Join(root, "lab", "workspaces", e.Name())
		}
	}

	if wsPath == "" {
		t.Fatal("active task workspace not found")
	}

	return tool, taskID, wsPath
}

func TestPathForRefusesHostileNames(t *testing.T) {
	ws, _ := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	for _, rel := range []string{
		"../escape.txt",
		"sub/../../escape.txt",
		"..",
		"/etc/passwd",
		"a\x00b.txt",
		"main\x00.txt",
	} {
		if _, err := ws.PathFor(rel); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("PathFor(%q) must be refused, got %v", rel, err)
		}
	}

	// The workspace root itself stays a valid target (runner default CWD).
	if got, err := ws.PathFor(""); err != nil || got == "" {
		t.Fatalf("PathFor(\"\") must return the workspace root, got %q err=%v", got, err)
	}
}

func TestPathForRefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	ws, victimDir := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	victim := filepath.Join(victimDir, "secret.txt")
	if err := os.WriteFile(victim, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlinked FILE and a symlinked DIR planted inside the workspace,
	// both pointing outside it.
	if err := os.Symlink(victim, filepath.Join(ws.Path, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, filepath.Join(ws.Path, "escdir")); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		"link.txt",
		"escdir/secret.txt",
		"escdir/nested/new.txt",
		"main.txt/../../link.txt",
	} {
		if _, err := ws.PathFor(rel); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("symlink escape %q must be refused, got %v", rel, err)
		}
	}
}

func TestRootIORefusesSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	ws, victimDir := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	victim := filepath.Join(victimDir, "secret.txt")
	if err := os.WriteFile(victim, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(victim, filepath.Join(ws.Path, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, filepath.Join(ws.Path, "escdir")); err != nil {
		t.Fatal(err)
	}

	// The os.Root layer refuses independently of PathFor: reads, stats and
	// writes through an escaping symlink all fail.
	root, err := ws.openRoot()
	if err != nil {
		t.Fatalf("openRoot: %v", err)
	}
	defer root.Close()

	if _, err := root.ReadFile("link.txt"); err == nil {
		t.Fatal("root read through an escape symlink must fail")
	}
	if _, err := root.Stat("escdir/secret.txt"); err == nil {
		t.Fatal("root stat through an escape symlink must fail")
	}
	if err := root.WriteFile("escdir/planted.txt", []byte("pwned"), 0o644); err == nil {
		t.Fatal("root write through an escape symlink must fail")
	}

	// Nothing was planted outside, and the victim is untouched.
	if _, err := os.Stat(filepath.Join(victimDir, "planted.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a file was planted outside the workspace")
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "top secret" {
		t.Fatalf("outside victim was modified: %q err=%v", data, err)
	}

	// Internal symlinks (resolving inside the root) still resolve: no
	// over-refusal. The link target is RELATIVE — os.Root refuses absolute
	// symlink targets by design, which the escape cases above rely on.
	if err := os.Symlink(
		"main.txt",
		filepath.Join(ws.Path, "inside-link.txt"),
	); err != nil {
		t.Fatal(err)
	}

	if data, err := root.ReadFile("inside-link.txt"); err != nil || string(data) != "inside\n" {
		t.Fatalf("an inside-the-root symlink must still read, err=%v data=%q", err, data)
	}
}

func TestToolReadAndEditRefuseEscapeAttempts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	tool, taskID, wsPath := newJailTool(t, map[string]string{"main.txt": "inside\n"})

	victimDir := t.TempDir()
	victim := filepath.Join(victimDir, "secret.txt")
	if err := os.WriteFile(victim, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(victim, filepath.Join(wsPath, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, filepath.Join(wsPath, "escdir")); err != nil {
		t.Fatal(err)
	}

	attempts := []map[string]any{
		{"action": "read_file", "taskId": taskID, "path": "link.txt"},
		{"action": "read_file", "taskId": taskID, "path": "escdir/secret.txt"},
		{"action": "read_file", "taskId": taskID, "path": "../secret.txt"},
		{"action": "read_file", "taskId": taskID, "path": victimDir},
		{"action": "read_file", "taskId": taskID, "path": "a\x00b"},
		{
			"action": "edit_file", "taskId": taskID, "path": "link.txt",
			"oldText": "top secret", "newText": "owned",
		},
		{
			"action": "edit_file", "taskId": taskID, "path": "escdir/secret.txt",
			"oldText": "top secret", "newText": "owned",
		},
		{
			"action": "edit_file", "taskId": taskID, "path": "../secret.txt",
			"oldText": "top secret", "newText": "owned",
		},
		{
			"action": "edit_file", "taskId": taskID, "path": "/etc/passwd",
			"oldText": "root", "newText": "owned",
		},
	}

	for _, args := range attempts {
		if _, err := tool.Run(context.Background(), marshalLabTestArgs(t, args)); err == nil {
			t.Fatalf("escape attempt %v must be refused", args["path"])
		}
	}

	// Every refused malicious operation left the outside world untouched.
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "top secret" {
		t.Fatalf("outside victim was modified: %q err=%v", data, err)
	}

	entries, err := os.ReadDir(victimDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "secret.txt" {
		t.Fatalf("outside directory gained entries: %v", entries)
	}

	// The workspace itself is intact and readable.
	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action": "read_file",
		"taskId": taskID,
		"path":   "main.txt",
	}))
	if err != nil || !strings.Contains(out, "inside") {
		t.Fatalf("workspace file must be untouched and readable: %q err=%v", out, err)
	}
}

func TestRunnerRefusesWorkingDirEscape(t *testing.T) {
	tool, taskID, wsPath := newJailTool(t, map[string]string{"main.txt": "x\n"})

	parent := filepath.Dir(wsPath)

	for _, workingDir := range []string{"../", "..", "/tmp", "a\x00b", "sub/../../../.."} {
		_, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
			"action":     "run",
			"taskId":     taskID,
			"command":    "touch pwned.txt",
			"workingDir": workingDir,
		}))
		if err == nil {
			t.Fatalf("working dir %q must be refused", workingDir)
		}
	}

	// The refused commands never executed anywhere.
	for _, p := range []string{
		filepath.Join(parent, "pwned.txt"),
		filepath.Join(wsPath, "pwned.txt"),
	} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused command left %s behind", p)
		}
	}

	// The default working dir (the workspace root) still works.
	out, err := tool.Run(context.Background(), marshalLabTestArgs(t, map[string]any{
		"action":  "run",
		"taskId":  taskID,
		"command": "pwd",
	}))
	if err != nil {
		t.Fatalf("default working dir must work: %v (%s)", err, out)
	}
}

func TestEnsureInsideWorkspaceGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	ws, victimDir := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	victim := filepath.Join(victimDir, "secret.txt")
	if err := os.WriteFile(victim, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(ws.Path, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := ensureInsideWorkspace(ws, filepath.Join(ws.Path, "main.txt")); err != nil {
		t.Fatalf("legitimate target refused: %v", err)
	}
	// A not-yet-existing target inside the workspace is fine too.
	if err := ensureInsideWorkspace(ws, filepath.Join(ws.Path, "new", "file.txt")); err != nil {
		t.Fatalf("legitimate new target refused: %v", err)
	}

	for _, target := range []string{
		filepath.Join(ws.Path, "link.txt"),
		filepath.Join(victimDir, "secret.txt"),
		filepath.Dir(ws.Path),
		"",
		"bad\x00target",
	} {
		if err := ensureInsideWorkspace(ws, target); err == nil {
			t.Fatalf("target %q must be refused", target)
		}
	}

	if err := ensureInsideWorkspace(nil, "/tmp"); err == nil {
		t.Fatal("nil workspace must be refused")
	}
}

func TestSafeJoinInsideGuard(t *testing.T) {
	root := t.TempDir()

	got, err := safeJoinInside(root, "a/b/c.txt")
	if err != nil || got != filepath.Join(root, "a", "b", "c.txt") {
		t.Fatalf("safeJoinInside = %q err=%v", got, err)
	}

	for _, rel := range []string{
		"",
		"..",
		"../escape.txt",
		"a/../../escape.txt",
		"bad\x00name",
	} {
		if _, err := safeJoinInside(root, rel); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("safeJoinInside(%q) must be refused, got %v", rel, err)
		}
	}

	if _, err := safeJoinInside("", "x"); !errors.Is(err, ErrInvalidWorkspace) {
		t.Fatal("empty root must be refused")
	}
}

func TestCreateRefusesNULSource(t *testing.T) {
	root := t.TempDir()

	mgr, err := NewWorkspaceManager(filepath.Join(root, "workspaces"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.Create(context.Background(), "bad\x00source"); err == nil {
		t.Fatal("NUL source must be refused")
	}
	if _, err := mgr.Create(context.Background(), ""); err == nil {
		t.Fatal("empty source must be refused")
	}
}

// TestPathForRefusesWindowsShapesOnWindows pins the Windows-native half
// of the workspace jail (v1.11.1 Windows repair): POSIX-spelled rooted
// paths, drive-absolute, drive-relative, UNC and device-namespace forms
// are refused before the join, never re-anchored under the workspace
// root by Join's separator handling.
func TestPathForRefusesWindowsShapesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path-syntax jail — run natively on Windows")
	}
	ws, _ := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	for _, rel := range []string{
		"/etc/passwd",    // POSIX-spelled rooted
		`\etc\passwd`,    // Windows rooted
		`C:\evil.txt`,    // drive-absolute
		"C:evil.txt",     // drive-relative
		`C:sub\evil.txt`, // drive-relative with a component
		`\\.\pipe\x`,     // device namespace
		`\\srv\share\x`,  // UNC
		"ads:name.txt",   // NTFS alternate data stream
	} {
		if _, err := ws.PathFor(rel); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("PathFor(%q) must be refused on Windows, got %v", rel, err)
		}
	}
}

// TestPathForRefusesBackslashRootedSpellings proves the backslash-spelled
// rooted and traversal forms are refused on BOTH hosts (the jail unifies
// separators before the join; on Windows the rooted-prefix and drive
// rules add the native checks): Windows path syntax cannot bypass the
// workspace jail on a POSIX host either.
func TestPathForRefusesBackslashRootedSpellings(t *testing.T) {
	ws, _ := newJailFixture(t, map[string]string{"main.txt": "inside\n"})

	for _, rel := range []string{
		`\etc\passwd`,   // backslash-spelled rooted → unified to a separator
		`..\escape.txt`, // backslash-spelled traversal
		`\\srv\share\x`, // UNC (absolute on Windows; unified rooted on POSIX)
	} {
		if _, err := ws.PathFor(rel); !errors.Is(err, ErrInvalidWorkspace) {
			t.Fatalf("PathFor(%q) must be refused on %s, got %v", rel, runtime.GOOS, err)
		}
	}
}
