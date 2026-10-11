package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanBreakdown(t *testing.T) {
	root := t.TempDir()
	// Seed known folders.
	mustWrite(t, filepath.Join(root, "models", "model.gguf"), 5000)
	mustWrite(t, filepath.Join(root, "sessions", "s1.json"), 300)
	mustWrite(t, filepath.Join(root, "workspace", "out.csv"), 900)
	mustWrite(t, filepath.Join(root, "config.json"), 42)

	got := Scan(root)
	if len(got) == 0 {
		t.Fatal("Scan returned nothing")
	}
	var models, sessions, workspace, appFiles int64
	for _, f := range got {
		switch f.Name {
		case "Models":
			models = f.Bytes
		case "Sessions":
			sessions = f.Bytes
		case "Workspace":
			workspace = f.Bytes
		case "App files":
			appFiles = f.Bytes
		}
	}
	if models != 5000 || sessions != 300 || workspace != 900 {
		t.Errorf("sizes wrong: models=%d sessions=%d workspace=%d", models, sessions, workspace)
	}
	if appFiles < 42 {
		t.Errorf("app files = %d, want >= 42", appFiles)
	}
	// Largest first.
	for i := 1; i < len(got); i++ {
		if got[i].Bytes > got[i-1].Bytes {
			t.Errorf("scan not sorted: %v", got)
		}
	}
}

func mustWrite(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrimSessions(t *testing.T) {
	// Newest-first order (the store's natural order).
	ids := []string{"new2", "new1", "old3", "old2", "old1"}
	var deleted []string
	removed := TrimSessions(3,
		func() ([]string, error) { return ids, nil },
		func(id string) error { deleted = append(deleted, id); return nil },
	)
	if removed != 2 || len(deleted) != 2 || deleted[0] != "old2" || deleted[1] != "old1" {
		t.Errorf("removed=%d deleted=%v", removed, deleted)
	}
	if TrimSessions(0, func() ([]string, error) { return ids, nil }, func(string) error { return nil }) != 0 {
		t.Error("keep=0 must keep everything")
	}
	// Fewer sessions than the cap: nothing deleted.
	if removed := TrimSessions(10, func() ([]string, error) { return ids, nil }, func(string) error { return nil }); removed != 0 {
		t.Errorf("keep>count must delete nothing, got %d", removed)
	}
}

func TestTrimLogsRotatesTail(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "app.log")
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("0123456789abcdef\n")
	}
	if err := os.WriteFile(big, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	freed := TrimLogs(dir, 1) // 1 MB budget — the ~320KB file is under it
	if freed != 0 {
		t.Errorf("small log folder should not be trimmed, freed=%d", freed)
	}
	// Now a folder far over budget.
	var huge strings.Builder
	for i := 0; i < 200000; i++ {
		huge.WriteString("0123456789abcdef0123456789abcdef0123456789abcdef\n")
	}
	if err := os.WriteFile(big, []byte(huge.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	freed = TrimLogs(dir, 1)
	if freed <= 0 {
		t.Fatal("expected bytes freed")
	}
	fi, err := os.Stat(big)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 2<<20 {
		t.Errorf("log still %d bytes after trim", fi.Size())
	}
	// The surviving tail must END with a newline (still valid JSONL lines).
	data, _ := os.ReadFile(big)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		t.Error("rotated log must end at a line boundary")
	}
}

func TestClearDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), 10)
	mustWrite(t, filepath.Join(dir, "sub", "b.txt"), 10)
	n, err := ClearDir(dir)
	if err != nil || n != 2 {
		t.Fatalf("ClearDir = %d, %v", n, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("dir not empty: %v", entries)
	}
	if n, err := ClearDir(filepath.Join(dir, "missing")); n != 0 || err != nil {
		t.Errorf("missing dir should be a no-op, got %d %v", n, err)
	}
}

func TestProcRAMSelf(t *testing.T) {
	// The test process always exists.
	if _, err := ProcRAM(os.Getpid()); err != nil {
		t.Errorf("ProcRAM(self) failed: %v", err)
	}
	if _, err := ProcRAM(-1); err == nil {
		t.Error("ProcRAM(-1) must fail")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:                     "512 B",
		2048:                    "2.0 KB",
		5 << 20:                 "5.0 MB",
		(3 << 30) + (500 << 20): "3.5 GB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
