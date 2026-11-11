package artifacts

// taskmeta_windows_test.go — v1.11.1 Windows path repair regression tests.
//
// The version-archive branch of TaskRegistry.Create sliced a NATIVE
// relative path (filepath.Rel output) with a hard-coded "/" lookup:
// on Windows filepath.Rel yields backslash-separated text, the lookup
// returned -1 and the unguarded slice panicked with
// "slice bounds out of range [:-1]" on every replacement write
// (CI run 42909094540: TestAutomationArtifactsSurfaceAndVersions EOF
// via internal/api/automation.go, and TestVersionArchiveStaysInsideTaskRoot).
//
// These tests pin the repaired path contract:
//   - logical (slash-delimited) destinations are validated before the
//     native conversion, with Windows-specific spellings rejected on
//     Windows hosts and POSIX literal names preserved on POSIX hosts;
//   - the first relToRoot component is derived platform-correctly and
//     proven to equal the grammar-gated task id before any slicing;
//   - version archives stay inside the task root on every platform;
//   - hostile destinations fail with a controlled error and leave the
//     filesystem outside the task-artifacts root untouched.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFirstRelComponent pins the helper contract on every platform:
// both separators are true boundaries here (the first component is the
// grammar-gated task id), a single component and a leading separator
// have no first component to split off.
func TestFirstRelComponent(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"task-1/notes.md", "task-1", true},           // POSIX native form
		{`task-1\notes.md`, "task-1", true},           // Windows native form
		{`task-1\.versions\notes.md`, "task-1", true}, // Windows nested
		{"task-1", "", false},                         // single component: no boundary
		{"/abs", "", false},                           // leading separator: empty component
		{`\abs`, "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := firstRelComponent(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("firstRelComponent(%q) = (%q,%v); want (%q,%v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestCreateAcceptsValidPaths: valid nested destinations, both separator
// spellings where native, multi-dot filenames, and a full replacement
// round trip whose archive lands in the task's own .versions directory.
func TestCreateAcceptsValidPaths(t *testing.T) {
	dir := t.TempDir() // a drive-absolute root on Windows hosts
	r := NewTaskRegistry(dir)

	if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: "sub/dir/report.md", Content: []byte("v1")}); err != nil {
		t.Fatalf("slash-spelled nested destination rejected: %v", err)
	}
	if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: "artifact.v1.backup.json", Content: []byte("{}")}); err != nil {
		t.Errorf("multi-dot filename mistaken for traversal: %v", err)
	}
	if runtime.GOOS == "windows" {
		if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: `sub\dir\notes.md`, Content: []byte("x")}); err != nil {
			t.Errorf("Windows backslash-spelled nested destination rejected: %v", err)
		}
	}

	// Replacement through the repaired archive branch (all platforms).
	if _, err := r.Create(CreateRequest{TaskID: "task-1", Source: "tool", Filename: "sub/dir/report.md", Content: []byte("v2")}); err != nil {
		t.Fatalf("nested replacement rejected: %v", err)
	}
	vdir := filepath.Join(dir, "task-artifacts", "task-1", ".versions")
	entries, err := os.ReadDir(vdir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("version archive after nested replacement: %v (%d entries)", err, len(entries))
	}
	if got := entries[0].Name(); got != "report.v1.md" {
		t.Errorf("archived name = %q, want report.v1.md", got)
	}
	current, err := os.ReadFile(filepath.Join(dir, "task-artifacts", "task-1", "sub", "dir", "report.md"))
	if err != nil || string(current) != "v2" {
		t.Errorf("current nested version wrong: %q err=%v", current, err)
	}
}

// TestCreateRejectsHostileDestinations: every Windows-hostile spelling
// of a managed-root relative destination is rejected with a stable,
// meaningful error, and nothing is ever written outside the artifacts
// root. POSIX-only-legal literal names stay accepted on POSIX (pinned
// so the platform difference is explicit, not accidental).
func TestCreateRejectsHostileDestinations(t *testing.T) {
	dir := t.TempDir()
	r := NewTaskRegistry(dir)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "sentinel.txt")
	if err := os.WriteFile(outside, []byte("untouched"), 0o644); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}

	hostile := []string{
		"", " ", // empty / whitespace-only
		".", "..", // dot components
		"../escape.md", `..\escape.md`, // traversal, both separators
		"a/../../escape.md", `a\..\..\escape.md`,
		"/etc/passwd", `\Windows\evil`, // rooted paths
		`\\server\share\x.md`,     // UNC
		`\\?\C:\evil\x.md`,        // extended-length device prefix
		`\\.\PhysicalDrive0`,      // device namespace
		"C:\\Users\\evil\\x.md",   // drive-absolute
		"C:evil.md",               // drive-relative
		"task:stream.md",          // ADS / volume colon
		"sub\x00x.md", "x\x00.md", // NUL bytes
	}
	for _, name := range hostile {
		_, err := r.Create(CreateRequest{TaskID: "task-9", Source: "tool", Filename: name, Content: []byte("x")})
		if err == nil {
			t.Errorf("Create(Filename=%q) must be rejected", name)
			continue
		}
		if strings.Contains(err.Error(), "panic") {
			t.Errorf("Create(Filename=%q) leaked a panic value: %v", name, err)
		}
	}

	if runtime.GOOS == "windows" {
		// Reserved device basenames must never reach filesystem creation
		// on Windows; trailing dot/space components are Win32 pathologies.
		for _, name := range []string{"NUL", "CON.md", "COM1", "lpt9.txt", "aux", "notes.md.", "sub ./x.md", "sub /x.md"} {
			if _, err := r.Create(CreateRequest{TaskID: "task-9", Source: "tool", Filename: name, Content: []byte("x")}); err == nil {
				t.Errorf("Create(Filename=%q) must be rejected on windows", name)
			}
		}
	} else {
		// On POSIX these are ordinary literal filenames — the contract
		// keeps accepting them (native OS semantics, unchanged behavior).
		for _, name := range []string{"NUL", "CON.md"} {
			if _, err := r.Create(CreateRequest{TaskID: "task-9", Source: "tool", Filename: name, Content: []byte("x")}); err != nil {
				t.Errorf("POSIX literal name %q must stay accepted: %v", name, err)
			}
		}
	}

	// The sentinel is byte-identical and nothing new appeared outside.
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "untouched" {
		t.Fatalf("outside sentinel disturbed: %q err=%v", got, err)
	}
	entries, err := os.ReadDir(outsideDir)
	if err != nil || len(entries) != 1 {
		t.Errorf("hostile filenames created entries outside the artifacts root: %d entries (%v)", len(entries), err)
	}
}

// TestCreateRefusesSymlinkEscape: a symlink planted inside the task root
// (at the atomic-write temp name or the destination itself) must never
// carry a write outside the root. Runs wherever the symlink primitive is
// available; the implementation is not weakened for platforms that
// cannot run this fixture.
func TestCreateRefusesSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	r := NewTaskRegistry(dir)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("outside-original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}

	taskRoot := filepath.Join(dir, "task-artifacts", "task-7")
	if err := os.MkdirAll(taskRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant the link at the temp name the atomic write uses first.
	link := filepath.Join(taskRoot, "leak.md.tmp")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink primitive unavailable on this runner: %v", err)
	}

	if _, err := r.Create(CreateRequest{TaskID: "task-7", Source: "tool", Filename: "leak.md", Content: []byte("pwn")}); err == nil {
		t.Errorf("Create over an outside-pointing symlink must fail through the rooted sink")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "outside-original" {
		t.Errorf("outside file written through planted symlink: %q err=%v", got, err)
	}
	entries, _ := os.ReadDir(outsideDir)
	if len(entries) != 1 {
		t.Errorf("symlink escape created entries outside the root: %d", len(entries))
	}
}

// TestWindowsAbsoluteTaskRootRoundTrip: on Windows the registry root is
// a drive-absolute path (the CI temp dir); the full create → replace →
// archive → version-history cycle must work with the drive present and
// keep everything inside the task root. Runs natively on Windows.
func TestWindowsAbsoluteTaskRootRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-native drive-rooted task root check (runs in the Windows CI job)")
	}
	dir := t.TempDir()
	if !filepath.IsAbs(dir) || filepath.VolumeName(dir) == "" {
		t.Fatalf("temp dir %q is not drive-absolute on windows", dir)
	}
	r := NewTaskRegistry(dir)

	const nested = "deep/sub/notes.md"
	if _, err := r.Create(CreateRequest{TaskID: "task-win", Source: "tool", Filename: nested, Content: []byte("w1")}); err != nil {
		t.Fatalf("create on drive-absolute root: %v", err)
	}
	meta, err := r.Create(CreateRequest{TaskID: "task-win", Source: "tool", Filename: nested, Content: []byte("w2")})
	if err != nil {
		t.Fatalf("replace on drive-absolute root (the former [:-1] panic site): %v", err)
	}
	if meta.Version != 2 {
		t.Errorf("replacement version = %d, want 2", meta.Version)
	}
	if vers := r.Versions("task-win", nested); len(vers) != 2 {
		t.Errorf("version history = %d entries, want 2", len(vers))
	}
	// The archived v1 lives under the task's own .versions directory.
	entries, err := os.ReadDir(filepath.Join(dir, "task-artifacts", "task-win", ".versions"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "notes.v1.md" {
		t.Errorf("windows version archive wrong: %v (%v)", entries, err)
	}
}
