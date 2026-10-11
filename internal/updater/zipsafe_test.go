package updater

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeZipPathRejectsTraversal pins the v1.1.4 zip-slip fix and the
// v1.2.0 Windows hardening: the updater's extractZip previously joined
// member names with no validation, and the first separator-canonical
// rewrite still accepted "\absolute\path" because filepath.IsAbs reports
// ROOTED-RELATIVE Windows paths as non-absolute.
func TestSafeZipPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()

	blocked := []string{
		"../../outside.exe",
		"../outside.exe",
		"/absolute/path",
		`\absolute\path`,
		`C:\absolute\path`,
		`C:/absolute/path`,
		`\\server\share\path`,
		`//server/share/path`,
		`bin/../../escape`,
		`..\outside`,
		`foo/../../outside`,
		`..\/mixed`,
		`..\..\mixed\escape`,
		"NUL-bearing\x00path",
		"   ",
	}

	for _, name := range blocked {
		if _, err := safeZipPath(dir, name); err == nil {
			t.Errorf("member %q must be rejected", name)
		}
	}

	allowed := []string{
		"llama-server.exe",
		"bin/llama-server.exe",
		`sub\dir\dll.dll`,
		"deep/nested/asset.data",
		"...",
	}

	for _, name := range allowed {
		target, err := safeZipPath(dir, name)
		if err != nil {
			t.Errorf("member %q must be accepted: %v", name, err)
			continue
		}

		rel, err := filepath.Rel(dir, target)
		if err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("member %q resolved outside dir: %s", name, target)
		}
	}
}

// TestLLMAndUpdaterValidatorsAgree pins the single-validator contract:
// since v1.3.6 the engine archive extraction lives ONLY here (the llm
// package provisions through updater.InstallStaged and no longer owns an
// extractor), and SafeArchivePath remains the exported authority. The
// test keeps both spellings (internal + exported) pinned to one behavior
// so a future split cannot drift.
func TestLLMAndUpdaterValidatorsAgree(t *testing.T) {
	dir := t.TempDir()

	members := []string{
		"ok.exe",
		"../escape.exe",
		"/absolute/path",
		`C:\bad\path`,
		`\\unc\share\x`,
		`sub\dir\dll.dll`,
		"",
	}

	for _, name := range members {
		_, errUpdater := safeZipPath(dir, name)
		_, errLLM := SafeArchivePath(dir, name)

		if (errUpdater == nil) != (errLLM == nil) {
			t.Errorf("validators disagree for %q: updater=%v llm=%v",
				name, errUpdater, errLLM)
		}
	}
}

// TestExtractZipRefusesEscapingMembers builds a hostile zip and proves
// extraction refuses to write outside the target directory.
func TestExtractZipRefusesEscapingMembers(t *testing.T) {
	outside := t.TempDir()
	inside := filepath.Join(outside, "stage")

	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	zipPath := filepath.Join(outside, "evil.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	zw := zip.NewWriter(f)
	w, err := zw.Create("../../escaped.txt")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	w.Write([]byte("payload"))
	zw.Close()
	f.Close()

	if err := extractZip(zipPath, inside); err == nil {
		t.Fatal("extractZip must reject a traversal member")
	}

	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); err == nil {
		t.Fatal("payload escaped the staging directory — zip-slip regression")
	}
}
