package updater

// v1.3.7 installer regressions:
//
//  1. ROLLBACK COMPANION PRESERVATION — the v1.3.6 mergeCompanionFiles
//     MOVED allowlisted companions out of the old package directory
//     before every rollback point; a failed update then deleted the
//     directory holding the only remaining copy, permanently destroying
//     shtn-engine-host* and metadata. v1.3.7: companions are COPIED;
//     rollback restores the previous package byte-for-byte.
//
//  2. UNMANAGED-DIRECTORY SWAP REFUSAL — the transactional install swaps
//     the WHOLE bin directory aside; for a user-set llamaBinPath outside
//     the SHEYTAN data root that would permanently delete every
//     non-companion file in it (a shared tools directory, a hand-built
//     llama.cpp tree). v1.3.7: the swap is refused with guidance; custom
//     engine paths keep working, they are simply never auto-swapped.

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// v137Config builds a config whose managed engine dir is <DataDir>/bin.
func v137Config(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.LlamaBinPath = ""

	return cfg
}

// v137ArchiveFromTestBinary zips the test executable as the engine
// release archive's server binary (a real host-arch executable).
func v137ArchiveFromTestBinary(t *testing.T, dir string) string {
	t.Helper()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	name := "llama-server"
	if filepath.Ext(testBin) == ".exe" {
		name = "llama-server.exe"
	}

	archivePath := filepath.Join(dir, "engine-release.zip")

	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer out.Close()

	zw := zip.NewWriter(out)

	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	hdr.SetMode(0o755)

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatalf("zip member: %v", err)
	}

	src, err := os.Open(testBin)
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer src.Close()

	if _, err := io.Copy(w, src); err != nil {
		t.Fatalf("zip copy: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return archivePath
}

// TestMergeCompanionFilesCopiesNeverMoves pins the copy semantics: after
// the merge the ORIGINALS remain in the old package directory, so the
// rollback path still has them to restore.
func TestMergeCompanionFilesCopiesNeverMoves(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()

	write(t, filepath.Join(oldDir, "shtn-engine-host.exe"), "native host payload")
	write(t, filepath.Join(oldDir, "LICENSE"), "license text")

	mergeCompanionFiles(oldDir, newDir)

	for _, name := range []string{"shtn-engine-host.exe", "LICENSE"} {
		// Present in the new package (carried)…
		if _, err := os.Stat(filepath.Join(newDir, name)); err != nil {
			t.Fatalf("companion %s must be carried into the new package: %v", name, err)
		}

		// …AND still present in the old package (copied, not moved —
		// the v1.3.6 move destroyed the only copy on rollback).
		if _, err := os.Stat(filepath.Join(oldDir, name)); err != nil {
			t.Fatalf("companion %s must REMAIN in the old package for rollback: %v", name, err)
		}
	}
}

// TestRollbackRestoresCompanionsByteForByte drives the full transaction:
// an old package (engine + native host + metadata) is swapped aside, the
// companions merge into the new package, and the ROLLBACK must restore
// the old package COMPLETE — including the companions that were merged
// forward (the v1.3.6 move-based merge lost them forever).
func TestRollbackRestoresCompanionsByteForByte(t *testing.T) {
	cfg := v137Config(t)

	binDir := EngineBinDir(cfg)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("bin dir: %v", err)
	}

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	name := "llama-server"
	if filepath.Ext(testBin) == ".exe" {
		name = "llama-server.exe"
	}

	// The OLD package: engine binary + native host + metadata.
	if err := copyFileMode(testBin, filepath.Join(binDir, name), 0o755); err != nil {
		t.Fatalf("seed old engine: %v", err)
	}

	hostPayload := strings.Repeat("native host payload v1.3.7 ", 512)

	if err := os.WriteFile(filepath.Join(binDir, "shtn-engine-host.exe"), []byte(hostPayload), 0o755); err != nil {
		t.Fatalf("seed native host: %v", err)
	}

	write(t, filepath.Join(binDir, "engine-install.json"), `{"tag":"b-old","sha256":"x"}`)

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b-new", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	// The swap carried the native host forward (copy)…
	if b, err := os.ReadFile(filepath.Join(binDir, "shtn-engine-host.exe")); err != nil || string(b) != hostPayload {
		t.Fatalf("the native host must be carried into the new package: err=%v", err)
	}

	// …and the rollback restores the OLD package complete.
	if err := staged.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if b, err := os.ReadFile(filepath.Join(binDir, "shtn-engine-host.exe")); err != nil || string(b) != hostPayload {
		t.Fatalf("rollback must restore the native host byte-for-byte — the v1.3.6 move destroyed it: err=%v", err)
	}

	if b, err := os.ReadFile(filepath.Join(binDir, "engine-install.json")); err != nil || !strings.Contains(string(b), "b-old") {
		t.Fatalf("rollback must restore the previous manifest: err=%v", err)
	}

	if _, err := os.Stat(binDir + ".update-old"); !os.IsNotExist(err) {
		t.Fatal("rollback must leave no .update-old directory behind")
	}
}

// TestSwapRefusedOutsideDataRoot pins the v1.3.7 guard: the transactional
// directory swap never touches a llamaBinPath location outside the
// SHEYTAN data root — a custom engine directory is user-managed.
func TestSwapRefusedOutsideDataRoot(t *testing.T) {
	root := t.TempDir()

	cfg := v137Config(t)

	// A custom engine location OUTSIDE the data root, holding files
	// SHEYTAN does not own.
	customDir := filepath.Join(root, "custom-tools")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("custom dir: %v", err)
	}

	write(t, filepath.Join(customDir, "unrelated-tool.exe"), "user file")

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	name := "llama-server"
	if filepath.Ext(testBin) == ".exe" {
		name = "llama-server.exe"
	}

	if err := copyFileMode(testBin, filepath.Join(customDir, name), 0o755); err != nil {
		t.Fatalf("seed custom engine: %v", err)
	}

	cfg.LlamaBinPath = filepath.Join(customDir, name)

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	_, err = InstallStagedFromArchive(cfg, "b-new", archive)
	if err == nil {
		t.Fatal("the directory swap must be REFUSED for a llamaBinPath outside the data root")
	}

	if !strings.Contains(err.Error(), "refusing to swap engine directory") {
		t.Fatalf("the refusal must explain the guard, got: %v", err)
	}

	// The custom directory is UNTOUCHED — no swap, no staging, no loss.
	if b, err := os.ReadFile(filepath.Join(customDir, "unrelated-tool.exe")); err != nil || string(b) != "user file" {
		t.Fatalf("the user's files in the custom engine directory must be untouched: err=%v", err)
	}

	if _, err := os.Stat(customDir + ".update-old"); !os.IsNotExist(err) {
		t.Fatal("the guard must leave no .update-old directory in the custom location")
	}

	if _, err := os.Stat(filepath.Join(customDir, ".update-stage")); !os.IsNotExist(err) {
		t.Fatal("the guard must leave no staging directory in the custom location")
	}
}

// TestEngineDirIsManaged pins the boundary itself.
func TestEngineDirIsManaged(t *testing.T) {
	cfg := v137Config(t)

	dataDir := filepath.Clean(cfg.DataDir)

	cases := []struct {
		dir  string
		want bool
	}{
		{filepath.Join(dataDir, "bin"), true},
		{filepath.Join(dataDir, "engines", "llama"), true},
		{dataDir, true},
		{filepath.Dir(dataDir), false},
		{filepath.Join(filepath.Dir(dataDir), "bin"), false},
		{"/usr/local/bin", false},
	}

	for _, tc := range cases {
		if got := engineDirIsManaged(cfg, tc.dir); got != tc.want {
			t.Fatalf("engineDirIsManaged(%q) = %v, want %v", tc.dir, got, tc.want)
		}
	}
}
