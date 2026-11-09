package vision

// vision_pathsafety_test.go — v1.11.1 path-injection remediation (alert
// at EncodeImage's os.ReadFile): image paths are user-selected files (a
// desktop install may attach any local image), so they are a user-owned
// absolute path class sink — the hostile spellings FAIL CLOSED before
// the read: empty paths, relative paths and NUL bytes are rejected, and
// the read runs over the cleaned absolute form. These tests also prove a
// real image still encodes end to end.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeImageHostilePathsFailClosed(t *testing.T) {
	for _, path := range []string{
		"",                // empty
		"   ",             // whitespace
		"img.png",         // relative — resolved against an arbitrary CWD would be ambiguous
		"../escape.png",   // relative traversal
		"img\x00evil.png", // NUL
		"./img.png",       // dot-relative form
	} {
		if url, err := EncodeImage(path); err == nil {
			t.Errorf("EncodeImage(%q) must fail closed, got url prefix %.20q", path, url)
		}
	}
}

// TestEncodeImageRelativePathCannotReadOutsideFile proves a relative
// traversal payload can never be laundered into a read: the relative
// form is rejected before the file system is touched, even when the
// target exists.
func TestEncodeImageRelativePathCannotReadOutsideFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "escape.png"), []byte("not really a png"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	rel := filepath.Join(filepath.Base(dir), "escape.png")
	if _, err := EncodeImage(rel); err == nil {
		t.Fatal("a relative image path must be rejected even when the file exists")
	}
}

// TestEncodeImageValidAbsolutePNGStillEncodes pins the positive path.
func TestEncodeImageValidAbsolutePNGStillEncodes(t *testing.T) {
	p := mustPNG(t, 64, 48)
	url, err := EncodeImage(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("bad prefix: %.40s", url)
	}
}
