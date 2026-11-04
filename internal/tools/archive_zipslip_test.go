package tools

// archive_zipslip_test.go — v1.11.1 (run 41901967798): the archive
// tool's extractors are ROOTED (os.Root) and validate member names
// through the ONE authoritative lexical validator shared with the
// updater (updater.SafeArchiveRel). These tests pin the hostile cases:
// traversal, drive-letter and symlink members fail closed, nothing is
// written outside the destination root, and a valid member still lands
// inside it.

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func buildHostileZip(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	members := []struct {
		name string
		mode os.FileMode
		data string
	}{
		{name: "../escape.txt", mode: 0o644, data: "traversal"},
		{name: "C:/evil.txt", mode: 0o644, data: "drive"},
		{name: `\\server\share\evil.txt`, mode: 0o644, data: "unc"},
	}

	for _, m := range members {
		hdr := &zip.FileHeader{Name: m.name, Method: zip.Store}
		hdr.SetMode(m.mode)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("member header: %v", err)
		}

		if _, err := w.Write([]byte(m.data)); err != nil {
			t.Fatalf("member body: %v", err)
		}
	}

	// A symlink member must be refused regardless of its target.
	hdr := &zip.FileHeader{Name: "link.txt", Method: zip.Store}
	hdr.SetMode(os.ModeSymlink | 0o777)

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("symlink header: %v", err)
	}

	if _, err := w.Write([]byte("../../outside")); err != nil {
		t.Fatalf("symlink body: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

func TestUnzipRejectsHostileMembers(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	root := filepath.Join(base, "extract-here")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	archive := filepath.Join(base, "hostile.zip")
	buildHostileZip(t, archive)

	tool := ArchiveTool{}

	if _, err := tool.unzip("hostile.zip", "extract-here"); err == nil {
		t.Fatal("a hostile archive (traversal/drive/UNC/symlink members) must be rejected")
	}

	// Nothing may exist outside the root, and the root must not contain
	// any of the hostile members.
	for _, hostile := range []string{
		filepath.Join(base, "escape.txt"),
		filepath.Join(root, "escape.txt"),
		filepath.Join(root, "evil.txt"),
		filepath.Join(root, "link.txt"),
	} {
		if _, err := os.Stat(hostile); err == nil {
			t.Errorf("hostile member materialized at %s", hostile)
		}
	}
}

func TestUnzipStillExtractsValidMembers(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	root := filepath.Join(base, "out")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	archive := filepath.Join(base, "good.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	zw := zip.NewWriter(f)

	for _, name := range []string{"ok.txt", "nested/deep/ok.txt"} {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetMode(0o644)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("member header: %v", err)
		}

		if _, err := w.Write([]byte("payload of " + name)); err != nil {
			t.Fatalf("member body: %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	tool := ArchiveTool{}

	out, err := tool.unzip("good.zip", "out")
	if err != nil {
		t.Fatalf("a valid two-member archive must extract: %v (%s)", err, out)
	}

	for _, name := range []string{"ok.txt", filepath.Join("nested", "deep", "ok.txt")} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Errorf("valid member %q must be extracted inside the root: %v", name, err)
			continue
		}

		if len(data) == 0 {
			t.Errorf("valid member %q extracted empty", name)
		}
	}
}
