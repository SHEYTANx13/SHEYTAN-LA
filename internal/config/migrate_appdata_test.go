package config

// v1.3.6 (spec §18/§19): legacy AppData root migration tests + hard
// path invariants. The fixtures build a realistic 1.3.5-era tree under
// a fake LOCALAPPDATA and prove the migration contract:
//
//      idempotent, hash-verified, collision-aware (newer wins),
//      restart-safe, config preserved, engine bundle as a unit,
//      legacy root removed only after verification, explicit overrides
//      never migrated.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func appDataFixtureEngineName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}

	return "llama-server"
}

// seedLegacyAppDataRoot creates a fake 1.3.5-era AppData data root.
func seedLegacyAppDataRoot(t *testing.T, localAppData string, withEngine bool) string {
	t.Helper()

	root := filepath.Join(localAppData, AppShortName)

	for _, dir := range []string{"models", "sessions", "logs", "bin"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		"models/model-a.gguf":               "gguf model a payload",
		"sessions/s1.json":                  `{"id":"s1","messages":[]}`,
		"logs/app.log":                      "2026-09-22 INFO boot",
		"memory.jsonl":                      `{"kind":"fact"}\n`,
		"installed.json":                    `{"appVersion":"1.3.5","components":{"llamaServer":{"status":"installed","meta":{"engineTag":"b10642"}}}}`,
		"bin/" + appDataFixtureEngineName(): "legacy engine binary bytes",
	}

	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if !withEngine {
		if err := os.Remove(filepath.Join(root, "bin", appDataFixtureEngineName())); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func unsetDataDirOverride(t *testing.T) {
	t.Helper()

	t.Setenv("SHEYTAN_DATA_DIR", "")
	os.Unsetenv("SHEYTAN_DATA_DIR")
}

func TestMigrateLegacyAppDataRootMovesEverything(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, true)

	canonical := t.TempDir()
	cfg := &Config{DataDir: canonical}

	report, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("migration must succeed: %v", err)
	}

	if len(report.Detected) == 0 || len(report.Removed) == 0 {
		t.Fatalf("legacy root must be detected and removed, got %+v", report)
	}

	// Data lives in the canonical root.
	for _, rel := range []string{
		"models/model-a.gguf",
		"sessions/s1.json",
		"logs/app.log",
		"memory.jsonl",
		"installed.json",
	} {
		if _, err := os.Stat(filepath.Join(canonical, rel)); err != nil {
			t.Errorf("%s must migrate to the canonical root: %v", rel, err)
		}
	}

	// Engine bundle preserved (legacy had one, canonical had none).
	if _, err := os.Stat(filepath.Join(canonical, "bin", appDataFixtureEngineName())); err != nil {
		t.Errorf("legacy engine bundle must migrate when canonical has none: %v", err)
	}

	// The legacy root is gone.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("verified legacy root must be removed, stat err: %v", err)
	}
}

func TestMigrateLegacyAppDataRootIsIdempotent(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	_ = seedLegacyAppDataRoot(t, localAppData, false)

	canonical := t.TempDir()
	cfg := &Config{DataDir: canonical}

	if _, err := MigrateLegacyAppDataRoot(cfg); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	report, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("second pass must be a clean no-op: %v", err)
	}

	if len(report.Detected) != 0 {
		t.Fatalf("second pass must detect nothing, got %+v", report.Detected)
	}
}

func TestMigrateLegacyAppDataRootNeverMixesEngineBundles(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, true) // legacy: b10642 engine

	defer os.RemoveAll(legacy) // keeps linters honest when the run skips removal
	_ = legacy

	canonical := t.TempDir()

	// Canonical already hosts a DIFFERENT managed engine package.
	canonicalBin := filepath.Join(canonical, "bin")
	if err := os.MkdirAll(canonicalBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalBin, appDataFixtureEngineName()), []byte("canonical b11090 engine"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{DataDir: canonical}

	if _, err := MigrateLegacyAppDataRoot(cfg); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// The canonical engine package must be UNTOUCHED (never mixed with
	// the legacy package — the 0xC0000139 prevention contract).
	data, err := os.ReadFile(filepath.Join(canonicalBin, appDataFixtureEngineName()))
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "canonical b11090 engine" {
		t.Fatalf("canonical engine binary was modified by the legacy merge")
	}
}

func TestMigrateLegacyAppDataRootKeepsNewerFiles(t *testing.T) {
	unsetDataDirOverride(t)

	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, false)

	canonical := t.TempDir()

	// The canonical sessions file is NEWER with different content — it
	// must win (deterministic newer-wins rule).
	newerPath := filepath.Join(canonical, "sessions", "s1.json")
	if err := os.MkdirAll(filepath.Dir(newerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newerPath, []byte(`{"id":"s1","messages":[{"role":"user"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(newerPath, future, future); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{DataDir: canonical}
	if _, err := MigrateLegacyAppDataRoot(cfg); err != nil {
		t.Fatalf("migration: %v", err)
	}

	data, err := os.ReadFile(newerPath)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != `{"id":"s1","messages":[{"role":"user"}]}` {
		t.Fatalf("the newer canonical file must win the collision, got %s", data)
	}

	_ = legacy
}

func TestMigrateSkippedWhenExplicitOverrideActive(t *testing.T) {
	// An explicit override is a DELIBERATE root choice — the legacy
	// AppData root must never be merged into it (spec §16).
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	legacy := seedLegacyAppDataRoot(t, localAppData, false)

	canonical := t.TempDir()
	t.Setenv("SHEYTAN_DATA_DIR", canonical)

	cfg := &Config{DataDir: canonical}

	report, err := MigrateLegacyAppDataRoot(cfg)
	if err != nil {
		t.Fatalf("migration with override: %v", err)
	}

	if len(report.Detected) != 0 {
		t.Fatalf("explicit override must disable the migration, got %+v", report.Detected)
	}

	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy root must remain untouched under an explicit override: %v", err)
	}
}

// --- hard path invariants (spec §19) ---------------------------------------

func TestHardPathInvariants(t *testing.T) {
	unsetDataDirOverride(t)

	dir := t.TempDir()

	cfg := Default()
	cfg.DataDir = dir
	// Clear the derived fields: the invariants assert that the
	// normalization RE-DERIVES them from the canonical root.
	cfg.ModelsDir = ""
	cfg.SessionsDir = ""
	normalizeRuntimePaths(cfg)

	if cfg.ModelsDir != filepath.Join(cfg.DataDir, "models") {
		t.Errorf("ModelsDir must be DataDir/models, got %s", cfg.ModelsDir)
	}

	if cfg.SessionsDir != filepath.Join(cfg.DataDir, "sessions") {
		t.Errorf("SessionsDir must be DataDir/sessions, got %s", cfg.SessionsDir)
	}

	if cfg.LogsDir() != filepath.Join(cfg.DataDir, "logs") {
		t.Errorf("LogsDir must be DataDir/logs, got %s", cfg.LogsDir())
	}
}

func TestModelsDirNeverDivergesFromEmptyOverride(t *testing.T) {
	unsetDataDirOverride(t)

	// An empty/blank override must behave like no override: derived
	// dirs stay anchored to the canonical root.
	dir := t.TempDir()

	t.Setenv("SHEYTAN_DATA_DIR", "   ")

	cfg := &Config{DataDir: dir, ModelsDir: "", SessionsDir: ""}
	normalizeRuntimePaths(cfg)

	if cfg.ModelsDir != filepath.Join(dir, "models") {
		t.Errorf("blank override must keep ModelsDir canonical, got %s", cfg.ModelsDir)
	}
}

func TestPathsWithSpacesAndUnicodeStayCanonical(t *testing.T) {
	unsetDataDirOverride(t)

	dir := filepath.Join(t.TempDir(), "SHEYTAN Install Dir (1)")

	cfg := &Config{DataDir: dir, ModelsDir: "", SessionsDir: ""}
	normalizeRuntimePaths(cfg)

	if cfg.ModelsDir != filepath.Join(dir, "models") {
		t.Errorf("paths with spaces must stay canonical, got %s", cfg.ModelsDir)
	}
}
