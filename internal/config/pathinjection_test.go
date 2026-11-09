package config

// pathinjection_test.go — v1.11.1 path-injection remediation (adversarial
// regression tests for the config alerts): config-declared paths are
// semi-trusted configuration. A hostile value (NUL, control characters,
// unresolved environment tokens, traversal escaping the intended root)
// must fall back to the canonical safe derivation — or the write sink
// must refuse it — and a valid configuration must be preserved exactly.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRuntimePathGrammar(t *testing.T) {
	root := t.TempDir()

	good := map[string]string{
		root:                             root,                               // absolute: kept, cleaned
		filepath.Join(root, "sub") + "/": filepath.Join(root, "sub"),         // trailing separator cleaned
		"models":                         filepath.Join(AppRoot(), "models"), // relative: anchored at AppRoot
		"lab/workspaces":                 filepath.Join(AppRoot(), "lab", "workspaces"),
	}
	for in, want := range good {
		got, ok := safeRuntimePath(in)
		if !ok || got != want {
			t.Errorf("safeRuntimePath(%q) = %q ok=%t, want %q", in, got, ok, want)
		}
	}

	for _, in := range []string{
		"",
		"   ",
		"x\x00y",                // NUL
		"line\nbreak",           // newline (control character)
		"%LOCALAPPDATA%\\x",     // unresolved Windows token
		"$HOME/x",               // unresolved Unix token
		"../escape",             // relative traversal past AppRoot
		"relative/../../escape", // nested traversal past AppRoot
		strings.Repeat("a", 4097),
	} {
		if got, ok := safeRuntimePath(in); ok {
			t.Errorf("safeRuntimePath(%q) must be rejected, got %q", in, got)
		}
	}
}

func TestNormalizeRuntimePathsFallsBackOnHostileDeclaredPaths(t *testing.T) {
	cfg := &Config{
		DataDir:          "x\x00evil",
		ModelsDir:        "../../escape-models",
		SessionsDir:      "ok\nnewline",
		LabWorkspaceRoot: "../escape-lab",
		WorkspaceRoot:    "../escape-workspace",
	}

	notes := normalizeRuntimePaths(cfg)
	if len(notes) == 0 {
		t.Fatal("every hostile declared path must be reported through the notes")
	}

	if cfg.DataDir != defaultDataRoot() {
		t.Errorf("hostile dataDir must fall back to the canonical root, got %q", cfg.DataDir)
	}
	if cfg.ModelsDir != filepath.Join(cfg.DataDir, "models") {
		t.Errorf("hostile modelsDir must fall back to the canonical derivation, got %q", cfg.ModelsDir)
	}
	if cfg.SessionsDir != filepath.Join(cfg.DataDir, "sessions") {
		t.Errorf("hostile sessionsDir must fall back to the canonical derivation, got %q", cfg.SessionsDir)
	}
	if cfg.LabWorkspaceRoot != filepath.Join(cfg.DataDir, "lab", "workspaces") {
		t.Errorf("hostile labWorkspaceRoot must fall back to the canonical derivation, got %q", cfg.LabWorkspaceRoot)
	}
	if cfg.WorkspaceRoot != "" {
		t.Errorf("hostile workspaceRoot must be dropped to the default workspace, got %q", cfg.WorkspaceRoot)
	}
}

func TestNormalizeRuntimePathsKeepsValidDeclarationsByteForByte(t *testing.T) {
	root := t.TempDir()
	before := &Config{
		DataDir:          root,
		ModelsDir:        filepath.Join(root, "models"),
		SessionsDir:      filepath.Join(root, "sessions"),
		LabWorkspaceRoot: filepath.Join(root, "lab", "workspaces"),
		WorkspaceRoot:    filepath.Join(root, "project"),
	}

	cfg := *before // shallow copy, same strings
	notes := normalizeRuntimePaths(&cfg)

	if len(notes) != 0 {
		t.Fatalf("a valid configuration must normalize without notes, got %v", notes)
	}
	if cfg.DataDir != before.DataDir || cfg.ModelsDir != before.ModelsDir ||
		cfg.SessionsDir != before.SessionsDir ||
		cfg.LabWorkspaceRoot != before.LabWorkspaceRoot ||
		cfg.WorkspaceRoot != before.WorkspaceRoot {
		t.Errorf("valid configuration changed: %+v -> %+v", before, cfg)
	}
}

func TestSaveRefusesHostileConfigPaths(t *testing.T) {
	cfg := Default()

	for _, path := range []string{
		"",
		"   ",
		"a\x00b.json",
		"a\nb.json",
		"../escape.json",
		"sub/../../escape.json",
		"%LOCALAPPDATA%\\config.json",
		strings.Repeat("a", 4097),
	} {
		if err := Save(path, cfg); err == nil {
			t.Errorf("Save(%q) must refuse a hostile config path", path)
		}
	}

	// The positive control: the canonical absolute path still writes.
	positive := filepath.Join(t.TempDir(), "config.json")
	if err := Save(positive, cfg); err != nil {
		t.Fatalf("Save(%q): %v", positive, err)
	}
	if _, err := os.Stat(positive); err != nil {
		t.Fatalf("saved config missing: %v", err)
	}
}

func TestWriteUniqueTempRefusesHostileDir(t *testing.T) {
	for _, dir := range []string{
		"",
		"   ",
		"a\x00b",
		"a\nb",
	} {
		if _, err := writeUniqueTemp(dir, "config", []byte("{}"), 0o600); err == nil {
			t.Errorf("writeUniqueTemp(%q) must refuse a hostile directory", dir)
		}
	}
}

func TestEnsureDirsFallsBackToSafeDerivations(t *testing.T) {
	dataDir := t.TempDir()

	// Hostile DERIVED declarations under a VALID data root: nothing may be
	// created outside the data root, and the canonical derivations must
	// exist afterwards. (The hostile relative spellings would otherwise
	// resolve against the test's working directory.)
	cfg := &Config{
		DataDir:          dataDir,
		ModelsDir:        "../escape-models",
		SessionsDir:      "x\x00evil",
		LabWorkspaceRoot: "../escape-lab",
	}

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	for _, want := range []string{
		dataDir,
		filepath.Join(dataDir, "models"),
		filepath.Join(dataDir, "sessions"),
		filepath.Join(dataDir, "logs"),
		filepath.Join(dataDir, "logs", "screenshots"),
		filepath.Join(dataDir, "charts"),
		filepath.Join(dataDir, "browser-profile"),
		filepath.Join(dataDir, "sandbox"),
		filepath.Join(dataDir, "workspace"),
		filepath.Join(dataDir, "lab", "workspaces"),
		filepath.Join(dataDir, "lab"),
		filepath.Join(dataDir, "research"),
		filepath.Join(dataDir, "bin"),
	} {
		if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
			t.Errorf("canonical dir %s must exist after EnsureDirs (err=%v)", want, err)
		}
	}

	// Nothing escaped next to the test's working directory.
	for _, hostile := range []string{"escape-models", "escape-lab"} {
		if _, err := os.Stat(hostile); err == nil {
			t.Errorf("hostile derived path %q was created outside the data root", hostile)
		}
	}
}
