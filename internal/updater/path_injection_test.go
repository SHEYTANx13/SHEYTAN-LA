package updater

// path_injection_test.go — v1.11.1 path-injection remediation regression
// tests (work order §4.2). Engine release tags arrive from the GitHub
// releases API / Atom feed (REMOTE data) and are embedded in the asset
// file name, the download URL, the staging paths and the download
// manager's cache key. These tests prove:
//
//   - the tag grammar (validEngineTag) accepts ordinary upstream tags and
//     rejects traversal/separator/ADS/NUL/hidden/oversized shapes;
//   - a hostile tag fails every install entry point CLOSED — before any
//     directory is created, any lease is taken or any byte is written —
//     and the managed data root is left untouched;
//   - the archive-extraction fallback names its copy after the LOCAL
//     archive, never the remote URL (a URL tail ending in ".." could once
//     point the copy at the extraction directory's parent);
//   - valid-input behavior is unchanged (real tags still install).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- grammar unit tests -----------------------------------------------------

func TestValidEngineTagGrammar(t *testing.T) {
	accepted := []string{
		"b1234",
		"b",
		"v1.6.1",
		"b-variant",
		"b_new.b1+c",
		"B12019-rc1",
	}
	for _, tag := range accepted {
		if !validEngineTag(tag) {
			t.Errorf("validEngineTag(%q) = false, want true", tag)
		}
	}

	rejected := []string{
		"",                       // empty
		".",                      // dot
		"..",                     // traversal
		".hidden",                // hidden
		"../..",                  // traversal
		"a/b",                    // POSIX separator
		"a\\b",                   // Windows separator
		"b:1",                    // colon (volume/ADS)
		"b\x001",                 // NUL
		" b",                     // whitespace-padded
		"b ",                     // trailing whitespace
		strings.Repeat("b", 129), // oversized
	}
	for _, tag := range rejected {
		if validEngineTag(tag) {
			t.Errorf("validEngineTag(%q) = true, want false", tag)
		}
	}
}

func TestCheckEngineTagWrapsSentinel(t *testing.T) {
	err := checkEngineTag("../evil")
	if !errors.Is(err, errInvalidEngineTag) {
		t.Fatalf("checkEngineTag must wrap errInvalidEngineTag, got %v", err)
	}
	if err := checkEngineTag("b1234"); err != nil {
		t.Fatalf("valid tag must pass: %v", err)
	}
}

// --- install entry points fail closed on hostile tags -----------------------

func hostileTags() []string {
	return []string{
		"../..",
		"..\\..",
		"b1/../b2",
		"a/b",
		".hidden",
		"..",
		"b:1",
		"b\x001",
		" b",
		"",
		strings.Repeat("b", 129),
	}
}

// snapshotDir returns the sorted relative tree of dir ("" entries when
// the directory does not exist) so tests can prove NOTHING changed.
func snapshotDir(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // nonexistent roots simply produce an empty snapshot
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil || rel == "." {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestInstallFromArchiveRejectsHostileTag proves the archive seam fails
// closed BEFORE anything happens: no managed bin directory is created,
// no install lease directory appears, and the DataDir tree is untouched.
func TestInstallFromArchiveRejectsHostileTag(t *testing.T) {
	cfg := v137Config(t)
	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	for _, tag := range hostileTags() {
		before := snapshotDir(t, cfg.DataDir)

		if _, err := InstallStagedFromArchive(cfg, tag, archive); !errors.Is(err, errInvalidEngineTag) {
			t.Fatalf("InstallStagedFromArchive(tag=%q) must fail closed, got %v", tag, err)
		}

		if after := snapshotDir(t, cfg.DataDir); len(after) != len(before) {
			t.Fatalf("hostile tag %q must leave the data root untouched: before=%v after=%v",
				tag, before, after)
		}
	}

	// The managed bin directory was never even created.
	if _, err := os.Stat(EngineBinDir(cfg)); !os.IsNotExist(err) {
		t.Fatalf("managed bin dir must not exist after hostile tags: %v", err)
	}
}

// TestDownloadEngineArchiveStagedRejectsHostileTag proves the download
// half also fails closed before any URL, archive name or cache key is
// built (and therefore before any network or staging I/O).
func TestDownloadEngineArchiveStagedRejectsHostileTag(t *testing.T) {
	cfg := v137Config(t)

	for _, tag := range hostileTags() {
		before := snapshotDir(t, cfg.DataDir)

		if _, err := DownloadEngineArchiveStaged(context.Background(), cfg, tag, VariantCPU, nil); !errors.Is(err, errInvalidEngineTag) {
			t.Fatalf("DownloadEngineArchiveStaged(tag=%q) must fail closed, got %v", tag, err)
		}

		if after := snapshotDir(t, cfg.DataDir); len(after) != len(before) {
			t.Fatalf("hostile tag %q must leave the data root untouched: before=%v after=%v",
				tag, before, after)
		}
	}
}

// TestStagedDownloadNameStaysInsideManagedRoot pins the positive
// counterpart: the staged archive name for a VALID tag is a plain file
// inside <binDir>/.engine-download (the asset descriptor, no tag-derived
// separators can reach it because the tag grammar ran first).
func TestStagedDownloadNameStaysInsideManagedRoot(t *testing.T) {
	for _, tag := range []string{"b1234", "b-variant", "v1.6.1"} {
		asset := AssetNameForVariant(tag, VariantCPU)
		if asset == "" {
			continue // platform has no prebuilt for this shape — nothing to pin
		}
		if strings.ContainsAny(asset, `/\`) || strings.HasPrefix(asset, ".") {
			t.Fatalf("asset name %q for tag %q is not a safe single component", asset, tag)
		}
	}
}

// --- extraction fallback uses the LOCAL archive name ------------------------

// outsideManagedRoot filters the snapshot to entries NOT under the
// extraction directory — the copy inside it is expected and allowed.
func outsideManagedRoot(snapshot []string, managedRel string) []string {
	var out []string
	for _, e := range snapshot {
		if e == managedRel || strings.HasPrefix(e, managedRel+string(filepath.Separator)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// TestExtractFallbackCopyUsesLocalArchiveName proves the unknown-suffix
// fallback copies the archive under its LOCAL name INSIDE the extraction
// directory — a remote URL whose tail ends in a traversal component
// (".../..") can never point the copy at the extraction directory's
// parent (the old code used filepath.Base(url)).
func TestExtractFallbackCopyUsesLocalArchiveName(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "stage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	payload := []byte("archive payload bytes")
	archiveDir := t.TempDir()
	archive := filepath.Join(archiveDir, "engine-payload.bin")
	if err := os.WriteFile(archive, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	before := outsideManagedRoot(snapshotDir(t, parent), "stage")

	// Hostile URL: the tail resolves to the parent of the extraction dir.
	// The suffix is unknown → the fallback copy branch runs.
	for _, url := range []string{
		"https://releases.example.invalid/pkg/..",
		"https://releases.example.invalid/pkg/../../",
		"https://releases.example.invalid/x/..%5C..%5C",
	} {
		if err := extractArchiveBySuffix(archive, url, dir); err != nil {
			t.Fatalf("extractArchiveBySuffix(url=%q): %v", url, err)
		}

		got, err := os.ReadFile(filepath.Join(dir, "engine-payload.bin"))
		if err != nil {
			t.Fatalf("fallback copy must use the LOCAL archive name inside the extraction dir: %v", err)
		}
		if string(got) != string(payload) {
			t.Fatalf("copied bytes mismatch")
		}

		// The parent of the extraction dir is untouched.
		if after := outsideManagedRoot(snapshotDir(t, parent), "stage"); strings.Join(after, "|") != strings.Join(before, "|") {
			t.Fatalf("extraction parent changed for url %q: before=%v after=%v", url, before, after)
		}

		// Clean the copy for the next iteration.
		if err := os.Remove(filepath.Join(dir, "engine-payload.bin")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSafeLocalFileName(t *testing.T) {
	accepted := map[string]string{
		"engine-payload.bin": "engine-payload.bin",
		"llama-b1234.zip":    "llama-b1234.zip",
	}
	for in, want := range accepted {
		got, err := safeLocalFileName(in)
		if err != nil || got != want {
			t.Errorf("safeLocalFileName(%q) = (%q, %v), want (%q, nil)", in, got, err, want)
		}
	}

	for _, name := range []string{
		"", ".hidden", "..", "a/b", "a\\b", "x:ads", "x\x00", "/abs", "C:/x",
	} {
		if got, err := safeLocalFileName(name); err == nil {
			t.Errorf("safeLocalFileName(%q) = %q, want error", name, got)
		}
	}
}
