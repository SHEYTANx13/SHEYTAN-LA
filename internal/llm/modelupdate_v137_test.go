// modelupdate_v137_test.go — v1.3.7 model-architecture auto-update
// transaction regressions (the updateEngineForModel path).
//
// The v1.3.6 path passed eng=nil to updater.UpdateEngineWithProgress,
// selecting the legacy choreography: InstallStaged committed IMMEDIATELY
// (no startup verification, no rollback — the last-known-good package was
// deleted by Commit before the new binary had ever been launched).
//
// v1.3.7: the auto-update uses the SAME deferred-commit transaction as
// every other engine update. The compat ladder's verification pass
// decides: COMMIT on ready, ROLLBACK (previous package restored
// byte-for-byte) on failure.
package llm

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
)

// seedCommittedEngine installs and COMMITS a good engine package so the
// managed dir holds a verified binary + install manifest; it returns the
// seeded binary's bytes for byte-for-byte rollback assertions.
func seedCommittedEngine(t *testing.T, cfg *config.Config, tag string) []byte {
	t.Helper()

	archive := buildFakeEngineArchive(t, t.TempDir())

	if _, err := updater.InstallStagedFromArchive(cfg, tag, archive); err != nil {
		t.Fatalf("seed good engine %s: %v", tag, err)
	}

	data, err := os.ReadFile(expectedEngineBinPath(cfg))
	if err != nil {
		t.Fatalf("seeded binary missing: %v", err)
	}

	return data
}

// buildMarkedEngineArchive builds a release archive whose binary is the
// test executable PLUS a trailing marker string — byte-distinct from any
// archive built by buildFakeEngineArchive (trailing bytes are ignored by
// the ELF/PE loader and by StaticValidate's header sniff, but prove the
// swap landed a DIFFERENT file).
func buildMarkedEngineArchive(t *testing.T, dir, marker string) string {
	t.Helper()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}

	raw, err := os.ReadFile(testBin)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}

	marked := append([]byte{}, raw...)
	marked = append(marked, []byte("\n// SHEYTAN-TEST-MARKER: "+marker+"\n")...)

	name := "llama-server"
	if filepath.Ext(testBin) == ".exe" {
		name = "llama-server.exe"
	}

	archivePath := filepath.Join(dir, "llama-marked-bin.zip")

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

	if _, err := w.Write(marked); err != nil {
		t.Fatalf("zip write: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return archivePath
}

// TestAutoUpdateForModelCommitsOnlyAfterVerification pins the v1.3.7
// commit ordering on the model-architecture auto-update path: the
// deferred install swaps the package, the ladder verifies the NEW engine
// serves, and ONLY THEN is the update committed (tag recorded, manifest
// written).
func TestAutoUpdateForModelCommitsOnlyAfterVerification(t *testing.T) {
	cfg, _ := fakeManagedEngineConfig(t, "arch-fail-committed")

	seedBytes := seedCommittedEngine(t, cfg, "b10642")

	// Deterministic release probe: the "newer" engine exists.
	updater.SetLatestTagForTest(func(context.Context) (string, error) {
		return "b13000", nil
	})
	t.Cleanup(func() { updater.SetLatestTagForTest(nil) })

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	srv.SetStagedArchiveForTest(buildMarkedEngineArchive(t, t.TempDir(), "auto-update-candidate"))

	if err := srv.Start(); err != nil {
		t.Fatalf("start with a verified auto-updated engine must succeed: %v", err)
	}

	if !srv.IsRunning() {
		t.Fatal("the auto-updated engine must be running after verification")
	}

	if got := updater.InstalledEngineTag(cfg); got != "b13000" {
		t.Fatalf("the auto-update must COMMIT only after verification — recorded tag = %q, want b13000", got)
	}

	// The committed manifest names the verified package.
	manifest := filepath.Join(updater.EngineBinDir(cfg), "engine-install.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("committed manifest missing: %v", err)
	}

	var m struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Tag != "b13000" {
		t.Fatalf("committed manifest tag = %q (err=%v), want b13000", m.Tag, err)
	}

	// The active binary is NO LONGER the seeded package (it was replaced
	// by the verified update).
	after, err := os.ReadFile(expectedEngineBinPath(cfg))
	if err != nil {
		t.Fatalf("updated binary missing: %v", err)
	}

	if string(after) == string(seedBytes) {
		t.Fatal("the seeded package is still active — the deferred swap never happened")
	}

	if !strings.Contains(string(after[len(after)-64:]), "auto-update-candidate") {
		t.Fatal("the active binary is not the marked auto-update candidate")
	}

	// No staging leftovers (spec §20).
	if _, err := os.Stat(updater.EngineBinDir(cfg) + ".update-old"); !os.IsNotExist(err) {
		t.Fatal("commit must remove the previous package directory")
	}
}

// TestAutoUpdateForModelRollsBackWhenVerificationFails pins the v1.3.7
// rollback: when the newly installed engine cannot verify (every launch
// fails with the architecture error), the last-known-good package is
// restored byte-for-byte, the tag stays at the previous release, and the
// failure surfaces from Start().
func TestAutoUpdateForModelRollsBackWhenVerificationFails(t *testing.T) {
	cfg, _ := fakeManagedEngineConfig(t, "arch-fail-always")

	seedBytes := seedCommittedEngine(t, cfg, "b10642")

	updater.SetLatestTagForTest(func(context.Context) (string, error) {
		return "b13000", nil
	})
	t.Cleanup(func() { updater.SetLatestTagForTest(nil) })

	srv := NewLlamaServer(config.NewSource(cfg))
	defer func() { _ = srv.Stop() }()

	srv.SetStagedArchiveForTest(buildFakeEngineArchive(t, t.TempDir()))

	startErr := srv.Start()
	if startErr == nil {
		t.Fatal("an auto-update that never verifies must FAIL the start — never succeed on an unverified engine")
	}

	// The last-known-good package is restored byte-for-byte.
	after, err := os.ReadFile(expectedEngineBinPath(cfg))
	if err != nil {
		t.Fatalf("previous package missing after rollback: %v", err)
	}

	if string(after) != string(seedBytes) {
		t.Fatal("the rollback must restore the previous package byte-for-byte — the last-known-good was destroyed")
	}

	// The manifest stays at the previous release (the unverified update
	// never committed).
	data, err := os.ReadFile(filepath.Join(updater.EngineBinDir(cfg), "engine-install.json"))
	if err != nil {
		t.Fatalf("previous manifest missing after rollback: %v", err)
	}

	var m struct {
		Tag string `json:"tag"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Tag != "b10642" {
		t.Fatalf("manifest tag after rollback = %q (err=%v), want b10642", m.Tag, err)
	}

	// No staging leftovers.
	if _, err := os.Stat(updater.EngineBinDir(cfg) + ".update-old"); !os.IsNotExist(err) {
		t.Fatal("rollback must leave no .update-old directory behind")
	}
}
