package updater

// staged_identity_v184_test.go — v1.8.4 ENGINE IDENTITY WINDOW (P0-B):
// the deferred-commit verification window must never misreport which
// binary is on disk.
//
// THE OBSERVED v1.8.3 CONTRADICTION (real Windows runtime log):
//
//      1. engine b11310 staged and SHA identity verified
//      2. "engine boot probe: binary build b11273"   ← the OLD recorded tag
//      3. b11310 committed and verified
//
// Between (1) and (3) the binary at EngineBinaryPath IS the staged
// candidate (byte-verified at stage time), while installed.json and
// engine-install.json still describe the previous build. The boot path
// keyed its identity on the RECORDED tag: the log named the wrong build
// and the persisted capability profile of the OLD build shadowed the NEW
// binary (the new executable was never probed that boot).
//
// The repair under test: the installer writes a window-scoped staged
// identity marker the moment the swap completes; StagedEngineIdentity
// reports the ACTUAL serving build; Commit and Rollback clear the marker
// so the settled-state authorities are never shadowed.

import (
        "os"
        "path/filepath"
        "testing"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func v184RequireStagedIdentity(t *testing.T, cfg *config.Config, wantTag string) {
        t.Helper()

        tag, sha, ok := StagedEngineIdentity(cfg)
        if !ok {
                t.Fatalf("staged identity must be pending during the verification window (want tag %q)", wantTag)
        }
        if tag != wantTag {
                t.Fatalf("staged identity tag = %q, want %q", tag, wantTag)
        }
        if sha == "" {
                t.Fatal("staged identity must carry the byte-verified sha256")
        }
}

func TestStagedIdentityMarkerPresentDuringVerificationWindow(t *testing.T) {
        cfg := v137Config(t)

        v181SeedOldPackage(t, cfg, "b11273")

        archive := v137ArchiveFromTestBinary(t, t.TempDir())

        // Pre-swap identity: the old build is known through its manifest.
        if tag := EffectiveInstalledEngineTag(cfg); tag != "b11273" {
                t.Fatalf("precondition: effective identity must be the previous build, got %q", tag)
        }

        staged, err := InstallStagedFromArchiveDeferred(cfg, "b11310", archive)
        if err != nil {
                t.Fatalf("deferred install: %v", err)
        }

        // The swap moved the old manifest aside with the old package: the
        // recorded/effective identity sources are now silent (this is the
        // exact void the old code filled with the stale recorded tag).
        if tag := EffectiveInstalledEngineTag(cfg); tag == "b11310" {
                t.Fatalf("precondition: the committed identity sources must not yet name the staged build, got %q", tag)
        }

        // THE CONTRACT: inside the window the marker names the binary that is
        // ACTUALLY on disk (b11310) — the boot path must probe and report the
        // staged build, never a stale recorded tag.
        v184RequireStagedIdentity(t, cfg, "b11310")

        staged.Commit()

        if _, _, ok := StagedEngineIdentity(cfg); ok {
                t.Fatal("staged identity must be cleared after commit (the manifest is the authority)")
        }
}

func TestStagedIdentityMarkerClearedOnRollback(t *testing.T) {
        cfg := v137Config(t)

        v181SeedOldPackage(t, cfg, "b11273")

        archive := v137ArchiveFromTestBinary(t, t.TempDir())

        staged, err := InstallStagedFromArchiveDeferred(cfg, "b11310", archive)
        if err != nil {
                t.Fatalf("deferred install: %v", err)
        }

        v184RequireStagedIdentity(t, cfg, "b11310")

        if err := staged.Rollback(); err != nil {
                t.Fatalf("rollback: %v", err)
        }

        if _, _, ok := StagedEngineIdentity(cfg); ok {
                t.Fatal("staged identity must be cleared after rollback (the restored package's manifest is the authority)")
        }

        if tag := EffectiveInstalledEngineTag(cfg); tag != "b11273" {
                t.Fatalf("post-rollback effective tag = %q, want b11273", tag)
        }
}

func TestStagedIdentityAbsentInEverySettledState(t *testing.T) {
        cfg := v137Config(t)

        if _, _, ok := StagedEngineIdentity(cfg); ok {
                t.Fatal("a fresh install state must not report a pending staged identity")
        }

        binDir := EngineBinDir(cfg)
        if err := os.MkdirAll(binDir, 0o755); err != nil {
                t.Fatalf("bin dir: %v", err)
        }

        // A corrupt or truncated marker must fail closed (no identity), never
        // supply a half-truth to the boot path.
        if err := os.WriteFile(filepath.Join(binDir, stagedIdentityName), []byte("{not json"), 0o600); err != nil {
                t.Fatalf("write corrupt marker: %v", err)
        }

        if _, _, ok := StagedEngineIdentity(cfg); ok {
                t.Fatal("a corrupt staged marker must be ignored, not trusted")
        }
}
