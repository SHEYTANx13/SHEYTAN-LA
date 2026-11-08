package tools

// archive_zipslip_test.go — v1.11.1 (run 41901967798): the archive
// tool's extractors are ROOTED (os.Root) and validate member names
// through the ONE authoritative lexical validator shared with the
// updater (updater.SafeArchiveRel). These tests pin the hostile cases:
// traversal, drive-letter and symlink members fail closed, nothing is
// written outside the destination root, and a valid member still lands
// inside it.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

func buildHostileTarGz(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)

	tw := tar.NewWriter(gz)

	// The same hostile shapes the zip side pins, in tar spelling: an
	// escaping traversal member, a symlink member and a hardlink member
	// (all three link/traversal classes the untar path must refuse).
	members := []struct {
		name     string
		typeflag byte
		linkname string
		data     string
	}{
		{name: "valid-inside.txt", typeflag: tar.TypeReg, data: "inside"},
		{name: "../escape-tar.txt", typeflag: tar.TypeReg, data: "traversal"},
		{name: "chain.txt", typeflag: tar.TypeSymlink, linkname: "../../outside-target", data: ""},
		{name: "hard.txt", typeflag: tar.TypeLink, linkname: "../../../etc/passwd", data: ""},
	}

	for _, m := range members {
		hdr := &tar.Header{
			Name:     m.name,
			Typeflag: m.typeflag,
			Linkname: m.linkname,
			Size:     int64(len(m.data)),
			Mode:     0o644,
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("member header: %v", err)
		}

		if m.data != "" {
			if _, err := tw.Write([]byte(m.data)); err != nil {
				t.Fatalf("member body: %v", err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

// TestUntarRejectsHostileMembers pins the tar-side of the same Zip Slip
// contract: traversal, symlink and hardlink members fail closed, nothing
// materializes outside the destination root, and the rollback drops the
// whole stage (the valid member that preceded the hostile one is NOT
// committed).
func TestUntarRejectsHostileMembers(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	root := filepath.Join(base, "extract-tar")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	archive := filepath.Join(base, "hostile.tar.gz")
	buildHostileTarGz(t, archive)

	tool := ArchiveTool{}

	if _, err := tool.untar("hostile.tar.gz", "extract-tar"); err == nil {
		t.Fatal("a hostile tar (traversal/symlink/hardlink members) must be rejected")
	}

	// Nothing may exist outside the root ...
	for _, hostile := range []string{
		filepath.Join(base, "escape-tar.txt"),
		filepath.Join(filepath.Dir(base), "escape-tar.txt"),
	} {
		if _, err := os.Stat(hostile); err == nil {
			t.Errorf("hostile member materialized at %s", hostile)
		}
	}

	// ... and the root must not contain any staged or committed member
	// (including the valid member that preceded the hostile one — the
	// transaction rolls the WHOLE stage back).
	for _, banned := range []string{
		filepath.Join(root, "valid-inside.txt"),
		filepath.Join(root, "escape-tar.txt"),
		filepath.Join(root, "chain.txt"),
		filepath.Join(root, "hard.txt"),
	} {
		if _, err := os.Stat(banned); err == nil {
			t.Errorf("member materialized at %s after a failed extraction", banned)
		}
	}
}
