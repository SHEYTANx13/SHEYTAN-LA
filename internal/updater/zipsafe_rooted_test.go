package updater

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeZipRelMatchesSafeZipPath: the ROOT-RELATIVE lexical layer
// (used by the rooted extractors) rejects exactly what the absolute
// validator rejects, and yields a clean relative name for every
// accepted member.
func TestSafeZipRelMatchesSafeZipPath(t *testing.T) {
	blocked := []string{
		"../../outside.exe",
		"../outside.exe",
		"/absolute/path",
		`\absolute\path`,
		`C:\absolute\path`,
		`C:/absolute/path`,
		`\\server\share\path`,
		"bin/../../escape",
		`..\..\mixed\escape`,
		"NUL-bearing\x00path",
		"   ",
		".",
	}

	for _, name := range blocked {
		if _, err := safeZipRel(name); err == nil {
			t.Errorf("member %q must be rejected by safeZipRel", name)
		}
	}

	allowed := []string{
		"llama-server.exe",
		"bin/llama-server.exe",
		`sub\dir\dll.dll`,
		"deep/nested/asset.data",
	}

	for _, name := range allowed {
		rel, err := safeZipRel(name)
		if err != nil {
			t.Errorf("member %q must be accepted: %v", name, err)
			continue
		}

		if filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			t.Errorf("safeZipRel(%q) = %q — must be a clean relative path", name, rel)
		}
	}
}

// TestExtractZipRejectsSymlinkMembers: a staged engine archive must be a
// plain file tree — a symlink member is refused and NOTHING is written.
func TestExtractZipRejectsSymlinkMembers(t *testing.T) {
	dir := t.TempDir()

	archive := filepath.Join(dir, "hostile.zip")
	if err := buildZipWithSymlinkMember(archive); err != nil {
		t.Fatalf("build hostile archive: %v", err)
	}

	staging := filepath.Join(dir, "staging")
	if err := extractZip(archive, staging); err == nil {
		t.Fatal("a symlink member must be rejected by extractZip")
	}

	if _, err := os.Stat(filepath.Join(staging, "link.txt")); err == nil {
		t.Fatal("the symlink member must not be materialized")
	}
}

// buildZipWithSymlinkMember writes a zip whose single entry carries the
// symlink mode bit (the way macOS and some CI tooling zips store links).
func buildZipWithSymlinkMember(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	hdr := &zip.FileHeader{
		Name:   "link.txt",
		Method: zip.Store,
	}
	hdr.SetMode(os.ModeSymlink | 0o777)

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}

	if _, err := w.Write([]byte("../../outside")); err != nil {
		return err
	}

	return zw.Close()
}
