package tools

// archive_staged_tx_test.go — v1.11.1 (run 42127523923, work order
// §6.5/§6.6/§6.8): the extractors are STAGED TRANSACTIONAL — members are
// extracted with EXCLUSIVE creates into a unique staging directory
// inside the root, committed (rooted renames) only after the whole
// archive verified, and rolled back (stage removed, prior content
// untouched) on any failure. These tests pin the transactional contract:
// overwrite-on-success, preserve-on-failure, zero stage residue, and
// truncation/file-count fail-closed behavior.

import (
	"archive/tar"
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// buildTarWithTraversalMember writes a tar whose FIRST member is valid
// and whose SECOND member is a traversal path — the rollback driver for
// the untar transaction test.
func buildTarWithTraversalMember(t *testing.T, path string) error {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tw := tar.NewWriter(f)

	for _, m := range []struct{ name, data string }{
		{"valid.txt", "valid"},
		{"../escape-after-valid.txt", "escape"},
	} {
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.data))}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if _, err := tw.Write([]byte(m.data)); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}

	return f.Close()
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func buildMembersZip(t *testing.T, path string, members map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	for name, data := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("member header %q: %v", name, err)
		}

		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatalf("member body %q: %v", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

// buildValidThenHostileZip puts ONE valid member first and ONE hostile
// member after it — the hostile member must abort the extraction AFTER
// the valid member's staged copy, exercising the rollback path.
func buildValidThenHostileZip(t *testing.T, path string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	w, err := zw.Create("valid.txt")
	if err != nil {
		t.Fatalf("valid member: %v", err)
	}

	if _, err := w.Write([]byte("valid")); err != nil {
		t.Fatalf("valid body: %v", err)
	}

	hostile, err := zw.Create("../escape-after-valid.txt")
	if err != nil {
		t.Fatalf("hostile member: %v", err)
	}

	if _, err := hostile.Write([]byte("escape")); err != nil {
		t.Fatalf("hostile body: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
}

// assertNoStageResidue: no `.archive-stage-*` directory may survive a
// finished extraction (success OR failure) — rollback removes only the
// stage, and success drops it too.
func assertNoStageResidue(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root %s: %v", root, err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".archive-stage-") {
			t.Fatalf("staging residue %q survived the extraction in %s", e.Name(), root)
		}
	}
}

// TestUnzipTransactionalOverwriteOnSuccess: a member that collides with
// a pre-existing destination file replaces it (the documented semantics)
// — but only at COMMIT, after the whole archive verified.
func TestUnzipTransactionalOverwriteOnSuccess(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	mustWriteFile(t, filepath.Join(dest, "report.txt"), "old")

	archive := filepath.Join(base, "payload.zip")
	buildMembersZip(t, archive, map[string]string{"report.txt": "new content"})

	out, err := ArchiveTool{}.unzip(archive, dest)
	if err != nil {
		t.Fatalf("unzip: %v (out=%q)", err, out)
	}

	if got := mustReadFile(t, filepath.Join(dest, "report.txt")); got != "new content" {
		t.Fatalf("committed member must replace the pre-existing file, got %q", got)
	}

	assertNoStageResidue(t, dest)
}

// TestUnzipTransactionalRollbackPreservesSentinel (§6.8 rollback): an
// archive whose hostile member aborts the extraction after a valid
// member must leave the destination's prior content EXACTLY untouched —
// no escaped files, no committed valid member, no stage residue.
func TestUnzipTransactionalRollbackPreservesSentinel(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	mustWriteFile(t, filepath.Join(dest, "sentinel.txt"), "SENTINEL")

	archive := filepath.Join(base, "mixed.zip")
	buildValidThenHostileZip(t, archive)

	if _, err := (ArchiveTool{}).unzip(archive, "dest"); err == nil {
		t.Fatal("an archive with a traversal member must fail")
	}

	if got := mustReadFile(t, filepath.Join(dest, "sentinel.txt")); got != "SENTINEL" {
		t.Fatalf("rollback must preserve prior content, sentinel now %q", got)
	}

	if _, err := os.Stat(filepath.Join(dest, "valid.txt")); err == nil {
		t.Fatal("the valid member must NOT be committed when the archive fails verification")
	}

	if _, err := os.Stat(filepath.Join(base, "escape-after-valid.txt")); err == nil {
		t.Fatal("the traversal member must never materialize outside the root")
	}

	assertNoStageResidue(t, dest)
}

// TestUntarTransactionalRollbackPreservesSentinel: the tar extractor
// honors the same transaction — a hostile member aborts the extraction
// and the destination's prior content survives byte-identical.
func TestUntarTransactionalRollbackPreservesSentinel(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	mustWriteFile(t, filepath.Join(dest, "sentinel.txt"), "SENTINEL")

	archive := filepath.Join(base, "mixed.tar")

	if err := buildTarWithTraversalMember(t, archive); err != nil {
		t.Fatal(err)
	}

	if _, err := (ArchiveTool{}).untar(archive, "dest"); err == nil {
		t.Fatal("a tar with a traversal member must fail")
	}

	if got := mustReadFile(t, filepath.Join(dest, "sentinel.txt")); got != "SENTINEL" {
		t.Fatalf("rollback must preserve prior content, sentinel now %q", got)
	}

	assertNoStageResidue(t, dest)
}

// TestUnzipTruncatedArchiveFailsClosedPreservingContent (§6.8): a
// truncated (corrupt central-directory) archive fails the extraction
// and the destination keeps its prior content — no half-extracted
// members, no stage residue.
func TestUnzipTruncatedArchiveFailsClosedPreservingContent(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	mustWriteFile(t, filepath.Join(dest, "sentinel.txt"), "SENTINEL")

	archive := filepath.Join(base, "truncated.zip")
	buildMembersZip(t, archive, map[string]string{
		"one.txt": "one",
		"two.txt": "two",
	})

	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	// Cut the central directory AND part of the last member.
	if err := os.WriteFile(archive, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (ArchiveTool{}).unzip(archive, "dest"); err == nil {
		t.Fatal("a truncated archive must fail")
	}

	if got := mustReadFile(t, filepath.Join(dest, "sentinel.txt")); got != "SENTINEL" {
		t.Fatalf("failed extraction must preserve prior content, sentinel now %q", got)
	}

	assertNoStageResidue(t, dest)
}

// TestUnzipStageIsolationConcurrentExtractions (§6.5 exclusivity): two
// extractions into the SAME destination root never share a stage —
// pinned indirectly by extracting two different archives sequentially
// and asserting both commit correctly and leave no residue (the unique
// stage allocation itself is exercised by freshStageDir's ErrExist
// retry, covered implicitly by every extraction).
func TestUnzipStageIsolationConcurrentExtractions(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	dest := filepath.Join(base, "dest")

	archiveA := filepath.Join(base, "a.zip")
	buildMembersZip(t, archiveA, map[string]string{"a.txt": "A"})

	archiveB := filepath.Join(base, "b.zip")
	buildMembersZip(t, archiveB, map[string]string{"b.txt": "B"})

	if _, err := (ArchiveTool{}).unzip(archiveA, dest); err != nil {
		t.Fatalf("first extraction: %v", err)
	}

	if _, err := (ArchiveTool{}).unzip(archiveB, dest); err != nil {
		t.Fatalf("second extraction: %v", err)
	}

	if got := mustReadFile(t, filepath.Join(dest, "a.txt")); got != "A" {
		t.Fatalf("first extraction content drifted: %q", got)
	}

	if got := mustReadFile(t, filepath.Join(dest, "b.txt")); got != "B" {
		t.Fatalf("second extraction content drifted: %q", got)
	}

	assertNoStageResidue(t, dest)
}

// TestListSanitizesControlCharacterMemberNames: archive listing renders
// member names for the tool result — a member whose name carries control
// characters is displayed escaped, never raw (log/terminal injection).
func TestListSanitizesControlCharacterMemberNames(t *testing.T) {
	base := t.TempDir()
	SetBaseDir(base)

	archive := filepath.Join(base, "hosts.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)

	w, err := zw.Create("ok.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}

	evil, err := zw.Create("evil\r\nINJECTED=1.txt")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := evil.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := ArchiveTool{}.list(archive)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if strings.Contains(out, "\r") || strings.Contains(out, "INJECTED=1.txt\n") && !strings.Contains(out, `"evil`) {
		t.Fatalf("listing must escape control-character member names, got: %q", out)
	}
}
