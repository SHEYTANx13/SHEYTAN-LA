package tools

// archive_listing_display_test.go — v1.11.1 (run 42502043636 work order
// §4.3, zipslip alerts #175/#176 at the list sinks): the listing renders
// every archive member name through displayArchiveMember, which routes
// the raw name through the ONE shared lexical validator
// (updater.SafeArchiveRel) and renders anything that is not a clean
// relative local path as a Go-quoted escape — so the displayed value is
// provably not a usable filesystem path. Hostile spellings (../, ..\,
// absolute, drive, UNC, ADS colon, NUL, control characters, overlong
// names) can never appear unescaped in a listing, and legitimate names
// survive unchanged.

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisplayArchiveMemberEscapesHostileNames(t *testing.T) {
	hostile := []string{
		"../escape.txt",                   // parent traversal
		"..\\escape.txt",                  // Windows traversal
		"a/../../escape.txt",              // nested traversal
		"/etc/passwd",                     // POSIX absolute
		"\\Windows\\system32\\evil.dll",   // Windows rooted
		"C:/evil.txt",                     // drive letter
		"C:\\evil.txt",                    // drive letter (native separators)
		"\\\\server\\share\\evil.txt",     // UNC
		"file.txt:ads",                    // alternate data stream
		"file\x00.txt",                    // NUL
		"\x1b[31mred\x1b[0m.txt",          // terminal escape
		"line\nbreak.txt",                 // control character
		strings.Repeat("x", 300) + ".txt", // overlong
	}
	for _, name := range hostile {
		got := displayArchiveMember(name)
		if got == name {
			t.Errorf("hostile member name %q must never be rendered verbatim", name)
		}
		if strings.Contains(got, "\n") || strings.Contains(got, "\x1b") || strings.Contains(got, "\x00") {
			t.Errorf("rendered name %q for member %q smuggled a control character", got, name)
		}
		if len(got) > 220 {
			t.Errorf("rendered name for member %q is unbounded (%d bytes)", name, len(got))
		}
	}

	clean := []string{
		"README.md",
		"docs/guide/index.html",
		"a/b/c/d.txt",
	}
	for _, name := range clean {
		if got := displayArchiveMember(name); got != name {
			t.Errorf("clean member name %q must render unchanged, got %q", name, got)
		}
	}
}

// TestListEscapesHostileZipMembers pins the zip listing sink end to end:
// a hostile member name arrives in the rendered listing only in its
// quoted escape form.
func TestListEscapesHostileZipMembers(t *testing.T) {
	dir := t.TempDir()
	SetBaseDir(dir)
	zipPath := filepath.Join(dir, "hostile.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"../escape.txt", "C:/evil.txt", "ok.txt"} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatalf("zip member: %v", err)
		}
		if _, err := w.Write([]byte("data")); err != nil {
			t.Fatalf("zip body: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	tool := ArchiveTool{}
	out, err := tool.list(zipPath)
	if err != nil {
		t.Fatalf("list hostile zip: %v", err)
	}
	if !strings.Contains(out, `"../escape.txt"`) {
		t.Errorf("the traversal member must appear quoted in the listing, got: %q", out)
	}
	if !strings.Contains(out, `"C:/evil.txt"`) {
		t.Errorf("the drive-letter member must appear quoted in the listing, got: %q", out)
	}
	if strings.Contains(out, "\n../escape.txt") {
		t.Errorf("the traversal member must never appear as a bare path in the listing")
	}
	if !strings.Contains(out, "ok.txt") {
		t.Errorf("the clean member must render normally, got: %q", out)
	}
}
