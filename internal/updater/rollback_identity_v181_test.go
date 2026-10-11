package updater

// rollback_identity_v181_test.go — v1.8.1 ENGINE IDENTITY REGRESSION:
// the rollback path must leave the RECORDED identity agreeing with the
// RESTORED package.
//
// THE DEFECT: during startup verification (between the package swap and
// the commit) ensureBinary stamps the BUNDLED DEFAULT tag as the
// last-resort identity of a tagless, manifest-less binary — the exact
// transient the v1.8.0 Windows log shows ("no engine tag recorded and no
// install manifest beside …bin\llama-server.exe — recording the bundled
// default tag b10642"). When verification then FAILS and the previous
// package is rolled back, that transient stamp survived in installed.json
// while the restored manifest described the actually-serving build. The
// next "update required" decision and the UI's engine tag — both plain
// state-first reads — would claim b10642 for a b11223-class engine.
//
// The repair: Rollback() re-records the tag from the restored manifest,
// so the committed identity, the manifest and the serving binary can
// never disagree after a rollback.
import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func v181SeedOldPackage(t *testing.T, cfg *config.Config, tag string) {
	t.Helper()

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

	data, err := os.ReadFile(testBin)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}

	if err := os.WriteFile(filepath.Join(binDir, name), data, 0o755); err != nil {
		t.Fatalf("seed old engine: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(binDir, "engine-install.json"),
		[]byte(`{"tag":"`+tag+`","sha256":"old-sha"}`),
		0o644,
	); err != nil {
		t.Fatalf("seed old manifest: %v", err)
	}
}

func TestRollbackRestoresRecordedIdentityFromManifest(t *testing.T) {
	cfg := v137Config(t)

	// The pre-transaction posture of the v1.8.0 Windows machine: an old
	// package whose identity is known ONLY through the manifest (the
	// state file carries no engine tag).
	v181SeedOldPackage(t, cfg, "b11223")

	if tag := InstalledEngineTag(cfg); tag != "" {
		t.Fatalf("precondition: state must be tagless, got %q", tag)
	}
	if tag := EffectiveInstalledEngineTag(cfg); tag != "b11223" {
		t.Fatalf("precondition: effective identity must come from the manifest, got %q", tag)
	}

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b11261", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	// The swap moved the old manifest aside with the old package: the
	// active binary is tagless AND manifest-less — the exact window.
	if tag := ManifestEngineTag(cfg); tag != "" {
		t.Fatalf("post-swap manifest must be absent, got %q", tag)
	}

	// What ensureBinary does during startup verification in that window:
	// the bundled default becomes the last-resort recorded identity.
	RecordEngineTag(cfg, DefaultEngineTag)

	if tag := InstalledEngineTag(cfg); tag != DefaultEngineTag {
		t.Fatalf("stamp: recorded tag = %q, want the bundled default %q", tag, DefaultEngineTag)
	}

	// Verification fails → rollback restores the previous package.
	if err := staged.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	// THE CONTRACT: the recorded identity now agrees with the restored
	// manifest (the actually-serving engine), never with the transient
	// bundled-default stamp.
	if tag := InstalledEngineTag(cfg); tag != "b11223" {
		t.Fatalf("post-rollback recorded tag = %q, want b11223 (from the restored manifest)", tag)
	}
	if tag := EffectiveInstalledEngineTag(cfg); tag != "b11223" {
		t.Fatalf("post-rollback effective tag = %q, want b11223", tag)
	}
}

func TestCommitRemainsTheAuthoritativeIdentity(t *testing.T) {
	cfg := v137Config(t)

	v181SeedOldPackage(t, cfg, "b11223")

	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b11261", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	// The same transient stamp during the verification window…
	RecordEngineTag(cfg, DefaultEngineTag)

	// …then verification SUCCEEDS: the commit is the authority.
	staged.Commit()

	if tag := InstalledEngineTag(cfg); tag != "b11261" {
		t.Fatalf("post-commit recorded tag = %q, want b11261", tag)
	}
	if tag := ManifestEngineTag(cfg); tag != "b11261" {
		t.Fatalf("post-commit manifest tag = %q, want b11261", tag)
	}
	if tag := EffectiveInstalledEngineTag(cfg); tag != "b11261" {
		t.Fatalf("post-commit effective tag = %q, want b11261", tag)
	}

	// The second boot's update decision: current == latest == b11261 →
	// no re-download (the two-boot contract).
	if tag := EffectiveInstalledEngineTag(cfg); tag == DefaultEngineTag || tag == "" {
		t.Fatalf("identity degraded after commit: %q", tag)
	}
}

func TestRollbackLeavesStateUntouchedWhenManifestHasNoTag(t *testing.T) {
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

	data, _ := os.ReadFile(testBin)
	if err := os.WriteFile(filepath.Join(binDir, name), data, 0o755); err != nil {
		t.Fatalf("seed old engine: %v", err)
	}

	// No manifest in the old package at all.
	archive := v137ArchiveFromTestBinary(t, t.TempDir())

	staged, err := InstallStagedFromArchiveDeferred(cfg, "b11261", archive)
	if err != nil {
		t.Fatalf("deferred install: %v", err)
	}

	if err := staged.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if tag := InstalledEngineTag(cfg); strings.TrimSpace(tag) != "" {
		t.Fatalf("rollback must not invent an identity when the restored package has none, got %q", tag)
	}
}
