package config

// v1.3.6 (spec §23/§24/§31): DATA-ROOT regressions.
//
// The installed runtime's ONE canonical data root is the install-local
// <AppRoot>\data tree (models/sessions/logs/bin/workspace under it).
// These tests pin:
//
//   - the default resolution is <AppRoot>\data (not AppRoot itself);
//   - a pre-<AppRoot>\data layout (app data directly under the
//     application root) folds into the canonical root;
//   - collisions keep the canonical copy authoritative;
//   - an explicit SHEYTAN_DATA_DIR override is never touched;
//   - the legacy AppData root migration never recreates AppData.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRootDefaultIsInstallLocalData(t *testing.T) {
	t.Setenv("SHEYTAN_DATA_DIR", "")

	root, fallback, reason := ResolveRoot()

	if fallback || reason != "" {
		t.Fatalf("default resolution must not fall back (fallback=%v reason=%q)", fallback, reason)
	}

	want := filepath.Join(AppRoot(), "data")
	if root != want {
		t.Fatalf("default data root = %q, want the install-local %q", root, want)
	}

	if !strings.HasSuffix(filepath.ToSlash(root), "/data") {
		t.Fatalf("default data root must be the <AppRoot>/data subtree, got %q", root)
	}
}

func TestMigrateAppRootDirectDataFoldsStrayLayout(t *testing.T) {
	appRoot := t.TempDir()
	canonical := filepath.Join(appRoot, "data")

	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatal(err)
	}

	// The pre-<AppRoot>\data portable layout: app data DIRECTLY under
	// the application root.
	writeT(t, filepath.Join(appRoot, "config.json"), `{"provider":"local"}`)
	mkdirT(t, filepath.Join(appRoot, "models"))
	writeT(t, filepath.Join(appRoot, "models", "old-model.gguf"), "gguf")
	mkdirT(t, filepath.Join(appRoot, "sessions"))
	mkdirT(t, filepath.Join(appRoot, "logs"))
	mkdirT(t, filepath.Join(appRoot, "bin"))
	writeT(t, filepath.Join(appRoot, "bin", "llama-server.exe"), "engine")
	writeT(t, filepath.Join(appRoot, "sheytan.exe"), "THE EXECUTABLE — MUST STAY")

	report := &MigrationReport{}

	if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Every app-data entry moved into the canonical root.
	for _, must := range []string{
		filepath.Join(canonical, "config.json"),
		filepath.Join(canonical, "models", "old-model.gguf"),
		filepath.Join(canonical, "sessions"),
		filepath.Join(canonical, "logs"),
		filepath.Join(canonical, "bin", "llama-server.exe"),
	} {
		if _, err := os.Stat(must); err != nil {
			t.Fatalf("%s must be folded into the canonical root: %v", must, err)
		}
	}

	// The application root keeps its executable; the strays are gone.
	if _, err := os.Stat(filepath.Join(appRoot, "sheytan.exe")); err != nil {
		t.Fatal("the application root's own files must never be touched")
	}

	if _, err := os.Stat(filepath.Join(appRoot, "models")); !os.IsNotExist(err) {
		t.Fatalf("the stray models dir must be gone from the app root (err=%v)", err)
	}

	if !report.ReloadConfig {
		t.Fatal("a folded config.json must trigger a config reload")
	}
}

func TestMigrateAppRootDirectDataKeepsCanonicalAuthoritative(t *testing.T) {
	appRoot := t.TempDir()
	canonical := filepath.Join(appRoot, "data")

	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatal(err)
	}

	// Both sides exist: the canonical copy stays authoritative and the
	// stray is left untouched (reported as a collision).
	mkdirT(t, filepath.Join(appRoot, "models"))
	writeT(t, filepath.Join(appRoot, "models", "stray.gguf"), "stray")
	writeT(t, filepath.Join(canonical, "models", "canonical.gguf"), "canonical")

	report := &MigrationReport{}

	if err := migrateAppRootEntries(appRoot, canonical, report); err != nil {
		t.Fatalf("migration: %v", err)
	}

	if len(report.Collisions) == 0 {
		t.Fatal("a canonical-vs-stray collision must be reported")
	}

	if _, err := os.Stat(filepath.Join(canonical, "models", "canonical.gguf")); err != nil {
		t.Fatal("the canonical models dir must remain untouched")
	}

	if _, err := os.Stat(filepath.Join(appRoot, "models", "stray.gguf")); err != nil {
		t.Fatal("the stray must be LEFT IN PLACE on collision (never merged DLL-by-DLL, never deleted silently)")
	}
}

func TestMigrateAppRootDirectDataSkipsExplicitOverride(t *testing.T) {
	override := t.TempDir()

	t.Setenv("SHEYTAN_DATA_DIR", override)

	cfg := Default()

	// Even with app-data strays under the app root, an explicit override
	// means the user's root is authoritative — nothing is migrated.
	report, err := MigrateAppRootDirectData(cfg)
	if err != nil {
		t.Fatalf("migration must not fail under an override: %v", err)
	}

	if len(report.Merged) != 0 {
		t.Fatalf("explicit override must never be touched by the app-root migration, got %v", report.Merged)
	}
}

func TestLegacyAppDataRootNotRecreatedAfterMigration(t *testing.T) {
	// The end-to-end §24 proof: after the legacy AppData root migrates,
	// nothing recreates it — the canonical root is install-local and the
	// migration itself removes the legacy tree.
	local := t.TempDir()

	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("SHEYTAN_DATA_DIR", "")

	appRoot := t.TempDir()
	canonical := filepath.Join(appRoot, "data")

	if err := os.MkdirAll(canonical, 0o755); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(local, AppShortName)

	if err := os.MkdirAll(filepath.Join(legacy, "models"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeT(t, filepath.Join(legacy, "models", "legacy-model.gguf"), "legacy gguf")

	cfg := Default()
	cfg.DataDir = canonical
	cfg.ModelsDir = filepath.Join(canonical, "models")

	report, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("legacy migration: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("the legacy AppData root must be removed after a verified migration (err=%v)", err)
	}

	if _, err := os.Stat(filepath.Join(canonical, "models", "legacy-model.gguf")); err != nil {
		t.Fatal("legacy model must live in the canonical root after migration")
	}

	_ = report

	// Idempotence: a second run is a no-op (the legacy root is gone) and
	// does NOT recreate AppData.
	if _, err := MigrateLegacyAppDataRoot(cfg); err != nil {
		t.Fatalf("second migration pass must be a safe no-op: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("the legacy AppData root must NOT come back (spec §24)")
	}
}

func writeT(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirT(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
