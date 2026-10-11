package artifacts

// Same-size content
// modification must be detected (the size-only fingerprint could not
// see it).

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSameSizeModificationDetected pins the richer fingerprint: a file
// rewritten with DIFFERENT content but the IDENTICAL byte length (and a
// fresh mtime, as any real rewrite produces) must surface in EndTurn.
func TestSameSizeModificationDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")

	if err := os.WriteFile(path, []byte(`{"status":"ok","v":1}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tr := New([]string{dir})
	tr.BeginTurn()

	// Same length, different content, and a FORCED same-ish mtime: even
	// when the modtime is pinned back (the pathological case), the
	// bounded digest must catch the rewrite.
	if err := os.WriteFile(path, []byte(`{"status":"ko","v":2}`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	arts := tr.EndTurn()
	if len(arts) != 1 {
		t.Fatalf("same-size rewrite NOT detected: %d artifacts (want 1)", len(arts))
	}
	if arts[0].Path != path {
		t.Fatalf("artifact path = %q", arts[0].Path)
	}
}

// TestPinnedMtimeSameSizeRewriteDetected covers the extreme case: the
// rewrite also restores the original mtime (timestamp-forgery), so only
// the CONTENT digest can detect it.
func TestPinnedMtimeSameSizeRewriteDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pinned.txt")

	orig := []byte("AAAA content identical length")
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	tr := New([]string{dir})
	tr.BeginTurn()

	if err := os.WriteFile(path, []byte("BBBB content identical length"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// Restore the original mtime — size AND modtime now match; only the
	// bounded digest distinguishes the file.
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	arts := tr.EndTurn()
	if len(arts) != 1 {
		t.Fatalf("content-only change NOT detected (digest must catch a pinned-mtime same-size rewrite): %d artifacts", len(arts))
	}
}

// TestUnchangedFilesNotReported guards the inverse: untouched files (and
// files whose mtime moved without a content change — an atime/ctime-only
// touch) stay silent.
func TestUnchangedFilesNotReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stable.txt")

	if err := os.WriteFile(path, []byte("never changes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tr := New([]string{dir})
	tr.BeginTurn()

	// A pure mtime touch with identical content: the digest matches, so
	// the fingerprint comparison must NOT report it. (If the filesystem
	// refuses sub-second precision this is still a no-op for the test.)
	time.Sleep(2 * time.Millisecond)
	_ = os.Chtimes(path, time.Now(), time.Now())

	if arts := tr.EndTurn(); len(arts) != 0 {
		t.Fatalf("unchanged file reported: %+v", arts)
	}
}
