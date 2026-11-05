package tools

// archive_hardening_v1111_test.go — v1.11.1 (run 41933333132, work order
// §8.2.4/§8.2.6/§8.2.7): the extraction ledger (duplicates, case-fold
// collisions, file-vs-dir conflicts), streamed byte caps, the
// compression-ratio bomb guard and the alternate-data-stream name
// rejection. Hostile members must fail the WHOLE extraction before any
// overwrite; a valid archive still extracts completely.

import (
	"archive/tar"
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/updater"
)

type zipMember struct {
	name string
	data string
}

func buildZipMembersOrdered(t *testing.T, path string, members []zipMember) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	for _, m := range members {
		hdr := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		hdr.SetMode(0o644)

		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("member header: %v", err)
		}
		if _, err := w.Write([]byte(m.data)); err != nil {
			t.Fatalf("member body: %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

func TestUnzipRejectsDuplicateAndCaseCollidingMembers(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	cases := [][]zipMember{
		{{"a.txt", "one"}, {"a.txt", "two"}},           // exact duplicate
		{{"Readme.txt", "one"}, {"readme.txt", "two"}}, // case-fold collision
	}

	for i, members := range cases {
		archive := filepath.Join(base, "dup.zip")
		buildZipMembersOrdered(t, archive, members)

		dest := filepath.Join(base, "out-dup")
		if _, err := (ArchiveTool{}).unzip(archive, dest); err == nil {
			t.Fatalf("duplicate/case-colliding member set %d must fail the extraction", i)
		}
	}
}

func TestUnzipRejectsFileVsDirConflicts(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	// A file at "a" then a member beneath "a/b": the file cannot become
	// a directory — and vice versa.
	cases := [][]zipMember{
		{{"a", "file"}, {"a/b.txt", "child"}},
		{{"a/b.txt", "child"}, {"a", "file"}},
	}
	for i, members := range cases {
		archive := filepath.Join(base, "conflict.zip")
		buildZipMembersOrdered(t, archive, members)

		dest := filepath.Join(base, "out-conflict")
		if _, err := (ArchiveTool{}).unzip(archive, dest); err == nil {
			t.Fatalf("conflict set %d must fail the extraction", i)
		}
	}
}

func TestUnzipRejectsAlternateDataStreamNames(t *testing.T) {
	if _, err := updater.SafeArchiveRel("file.txt:stream"); err == nil {
		t.Fatal("an ADS-colon member name must be rejected by the shared validator")
	}
	if _, err := updater.SafeArchiveRel("dir/file.txt:ads"); err == nil {
		t.Fatal("a nested ADS member name must be rejected")
	}
	// Plain names keep working.
	if _, err := updater.SafeArchiveRel("dir/file.txt"); err != nil {
		t.Fatalf("a plain member must keep validating, got: %v", err)
	}
}

func TestUnzipCompressionRatioBombGuard(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	// ~3 MiB of zeros deflate to a few KiB: far beyond the 200x ratio
	// guard, above the 1 MiB ratio floor.
	data := strings.Repeat("\x00", 3<<20)

	archive := filepath.Join(base, "bomb.zip")
	buildZipMembersOrdered(t, archive, []zipMember{{"zeros.bin", data}})

	dest := filepath.Join(base, "out-bomb")
	if _, err := (ArchiveTool{}).unzip(archive, dest); err == nil {
		t.Fatal("a decompression-ratio bomb must be refused")
	}
}

func TestUnzipValidArchiveStillExtracts(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	archive := filepath.Join(base, "valid.zip")
	buildZipMembersOrdered(t, archive, []zipMember{
		{"top.txt", "top"},
		{"nested/deep.bin", "deep"},
		{"other.txt", "other"},
	})

	dest := filepath.Join(base, "out-valid")
	msg, err := ArchiveTool{}.unzip(archive, dest)
	if err != nil {
		t.Fatalf("valid archive must extract, got: %v", err)
	}
	if !strings.Contains(msg, "3 file(s)") {
		t.Fatalf("unexpected extraction summary: %q", msg)
	}
	for _, rel := range []string{"top.txt", filepath.Join("nested", "deep.bin"), "other.txt"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatalf("member %s must exist inside the destination: %v", rel, err)
		}
	}
	// Nothing escaped the destination.
	if _, err := os.Stat(filepath.Join(base, "escape.txt")); err == nil {
		t.Fatal("no member may land outside the destination root")
	}
}

func buildCaseCollidingTar(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar: %v", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)

	for _, m := range []zipMember{{"One.txt", "one"}, {"one.txt", "two"}} {
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(m.data)); err != nil {
			t.Fatalf("tar body: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
}

func TestUntarLedgerCaseCollisions(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	archive := filepath.Join(base, "case.tar")
	buildCaseCollidingTar(t, archive)

	dest := filepath.Join(base, "out-case")
	if _, err := (ArchiveTool{}).untar(archive, dest); err == nil {
		t.Fatal("case-colliding tar members must fail the extraction")
	}
}
