// paths_test.go — v1.3.0 regression tests for the canonical runtime path
// resolution.
//
// The contracts under test:
//
//  1. ONE canonical root: models/sessions/logs/workspace derive from it;
//  2. environment-variable references expand ONCE and never persist;
//  3. an unresolved %TOKEN% is REJECTED (canonical root fallback), never
//     joined into a malformed path;
//  4. startup never creates paths containing "%LOCALAPPDATA%" or the
//     doubled SHEYTAN-LA nesting;
//  5. resolution is independent of the current working directory.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertNoEnvToken fails when a path still contains a literal
// environment reference.
func assertNoEnvToken(t *testing.T, label, path string) {
	t.Helper()
	if HasEnvToken(path) {
		t.Fatalf("%s = %q still contains a literal environment reference", label, path)
	}
	if strings.Contains(path, "%LOCALAPPDATA%") {
		t.Fatalf("%s = %q contains the literal %%LOCALAPPDATA%% token", label, path)
	}
}

func TestExpandEnvPathResolvesTokens(t *testing.T) {
	t.Setenv("SHEYTAN_TEST_VAR", "/tmp/sheytan-expanded")

	cases := []struct {
		name, in, want string
	}{
		{"windows token", "C:\\%SHEYTAN_TEST_VAR%\\SHEYTAN-LA", "C:\\/tmp/sheytan-expanded\\SHEYTAN-LA"},
		{"dollar form", "$SHEYTAN_TEST_VAR/models", "/tmp/sheytan-expanded/models"},
		{"brace form", "${SHEYTAN_TEST_VAR}/models", "/tmp/sheytan-expanded/models"},
		{"no tokens", `C:\Users\me\SHEYTAN-LA`, `C:\Users\me\SHEYTAN-LA`},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpandEnvPath(tc.in)
			if got != tc.want {
				t.Fatalf("ExpandEnvPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandEnvPathLeavesUnresolvableVisible(t *testing.T) {
	got := ExpandEnvPath("%SHEYTAN_MISSING_TOKEN_12345%/models")
	if !HasEnvToken(got) {
		t.Fatalf("unresolvable token disappeared: %q — rejection depends on it staying visible", got)
	}
}

func TestHasEnvToken(t *testing.T) {
	yes := []string{
		`%LOCALAPPDATA%\SHEYTAN-LA`,
		`C:\tools\SHEYTAN-LA\%LOCALAPPDATA%\SHEYTAN-LA\models`,
		"$HOME/sheytan",
		"${HOME}/sheytan",
	}
	no := []string{
		`C:\Users\me\AppData\Local\SHEYTAN-LA`,
		`C:\SHEYTAN-LA\models`,
		"",
		"models",
		"plain-relative",
	}

	for _, p := range yes {
		if !HasEnvToken(p) {
			t.Fatalf("HasEnvToken(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if HasEnvToken(p) {
			t.Fatalf("HasEnvToken(%q) = true, want false", p)
		}
	}
}

// TestResolveRootRejectsUnresolvedToken pins the fail-closed behavior
// for an UNRESOLVABLE token. The variable is a uniquely named test
// token that exists on no platform — the previous fixture used
// %LOCALAPPDATA% assuming it is never set, which is false on Windows
// (the runner HAS it) and turned the test into an accidental
// environment assumption.
func TestResolveRootRejectsUnresolvedToken(t *testing.T) {
	t.Setenv("SHEYTAN_DATA_DIR", `%SHEYTAN_MISSING_TOKEN_12345%\SHEYTAN-LA`)

	root, fallback, reason := ResolveRoot()

	if !fallback {
		t.Fatal("unresolved token must trigger the canonical fallback")
	}
	if reason == "" {
		t.Fatal("fallback reason must be actionable, not empty")
	}
	assertNoEnvToken(t, "root", root)
	if !filepath.IsAbs(root) {
		t.Fatalf("root %q is not absolute", root)
	}
	if root != filepath.Join(AppRoot(), "data") {
		t.Fatalf("fallback root %q != canonical install-local data root %q", root, filepath.Join(AppRoot(), "data"))
	}
}

func TestResolveRootExpandsResolvableToken(t *testing.T) {
	expanded := t.TempDir()
	t.Setenv("SHEYTAN_TEST_DATA", expanded)
	t.Setenv("SHEYTAN_DATA_DIR", "%SHEYTAN_TEST_DATA%/SHEYTAN-LA")

	root, fallback, _ := ResolveRoot()

	if fallback {
		t.Fatal("a resolvable token must NOT fall back")
	}
	want := filepath.Join(expanded, "SHEYTAN-LA")
	if root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
	assertNoEnvToken(t, "root", root)
}

// TestResolveRootUsesAppRootWithoutOverride — v1.3.6 (spec §23): without
// an override the canonical root is the INSTALL-LOCAL <AppRoot>\data
// subtree (the installer creates exactly this tree).
func TestResolveRootUsesAppRootWithoutOverride(t *testing.T) {
	t.Setenv("SHEYTAN_DATA_DIR", "")

	root, fallback, reason := ResolveRoot()

	if fallback || reason != "" {
		t.Fatalf("no override: fallback=%t reason=%q", fallback, reason)
	}
	if root != filepath.Join(AppRoot(), "data") {
		t.Fatalf("root = %q, want the install-local %q", root, filepath.Join(AppRoot(), "data"))
	}
}

// TestResolveRootIndependentOfWorkingDirectory — the v1.2.9 defect joined
// relative values against the CWD; v1.3.0 anchors them to the app root.
//
// v1.3.5: the CWD mutation uses t.Chdir (Go 1.24+), which restores the
// EXACT original working directory automatically. The previous version
// changed the process-global CWD and restored a DIFFERENT temp directory,
// leaking process-wide state into every later test.
func TestResolveRootIndependentOfWorkingDirectory(t *testing.T) {
	t.Setenv("SHEYTAN_DATA_DIR", "SHEYTAN-LA-data")

	wd1 := t.TempDir()
	wd2 := t.TempDir()

	t.Chdir(wd1)

	root := AbsAgainstAppRoot("SHEYTAN-LA-data")
	if !filepath.IsAbs(root) {
		t.Fatalf("relative override resolved non-absolute: %q", root)
	}
	if !strings.HasPrefix(filepath.ToSlash(root), filepath.ToSlash(AppRoot())+"/") {
		t.Fatalf("relative override %q must anchor at the app root %q, not the CWD", root, AppRoot())
	}

	t.Chdir(wd2)

	root2 := AbsAgainstAppRoot("SHEYTAN-LA-data")
	if root != root2 {
		t.Fatalf("resolution depends on the working directory: %q vs %q", root, root2)
	}
}

func TestIsMalformedRootPath(t *testing.T) {
	yes := []string{
		filepath.Join("C:", "tools", "SHEYTAN-LA", "%LOCALAPPDATA%", "SHEYTAN-LA"),
		filepath.Join("C:", "tools", "SHEYTAN-LA", "SHEYTAN-LA"),
	}
	no := []string{
		filepath.Join("C:", "tools", "SHEYTAN-LA"),
		filepath.Join("C:", "Users", "me", "AppData", "Local", "SHEYTAN-LA"),
		"",
	}

	for _, p := range yes {
		if !isMalformedRootPath(p) {
			t.Fatalf("isMalformedRootPath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if isMalformedRootPath(p) {
			t.Fatalf("isMalformedRootPath(%q) = true, want false", p)
		}
	}
}

// TestStartupDirectoryContract is the v1.3.0 STARTUP test from the
// release contract: after Load + EnsureDirs, every derived directory
// originates from the canonical root and NO path anywhere in the
// configuration contains "%LOCALAPPDATA%" or the doubled nesting.
func TestStartupDirectoryContract(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SHEYTAN_DATA_DIR", root)

	cfg, err := Load(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	if cfg.DataDir != root {
		t.Fatalf("DataDir = %q, want canonical root %q", cfg.DataDir, root)
	}

	want := map[string]string{
		"models":    cfg.ModelsDir,
		"sessions":  cfg.SessionsDir,
		"logs":      cfg.LogsDir(),
		"workspace": cfg.WorkspaceDir(),
	}
	for sub, got := range want {
		expect := filepath.Join(root, sub)
		if got != expect {
			t.Fatalf("%s = %q, want %q", sub, got, expect)
		}
		assertNoEnvToken(t, sub, got)
		if _, err := os.Stat(got); err != nil {
			t.Fatalf("%s (%q) was not created: %v", sub, got, err)
		}
	}
}

// TestLoadRejectsPersistedEnvTokens — a v1.2.9 config.json that stored
// the raw token as dataDir/modelsDir must never reach runtime state.
// The token is a uniquely named test variable that exists on no
// platform: %LOCALAPPDATA% would RESOLVE on Windows runners and the
// rejection contract would silently stop being exercised.
func TestLoadRejectsPersistedEnvTokens(t *testing.T) {
	root := t.TempDir()

	cfgFile := filepath.Join(root, "config.json")
	raw := `{
                "dataDir": "%SHEYTAN_MISSING_TOKEN_12345%\\SHEYTAN-LA",
                "modelsDir": "%SHEYTAN_MISSING_TOKEN_12345%\\SHEYTAN-LA\\models",
                "sessionsDir": "%SHEYTAN_MISSING_TOKEN_12345%\\SHEYTAN-LA\\sessions"
        }`
	if err := os.WriteFile(cfgFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertNoEnvToken(t, "dataDir", cfg.DataDir)
	assertNoEnvToken(t, "modelsDir", cfg.ModelsDir)
	assertNoEnvToken(t, "sessionsDir", cfg.SessionsDir)

	if len(cfg.PathNotes) == 0 {
		t.Fatal("rejections must be reported through PathNotes so they reach the log")
	}

	// The canonical fallback holds the tree together.
	if cfg.ModelsDir != filepath.Join(cfg.DataDir, "models") {
		t.Fatalf("modelsDir = %q, want %q", cfg.ModelsDir, filepath.Join(cfg.DataDir, "models"))
	}
}

// TestLoadExpandsPersistedResolvableTokens — a config written with a
// resolvable reference expands to the real path.
func TestLoadExpandsPersistedResolvableTokens(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SHEYTAN_TEST_ROOT", root)

	cfgFile := filepath.Join(root, "config.json")
	raw := `{
                "dataDir": "%SHEYTAN_TEST_ROOT%",
                "modelsDir": "%SHEYTAN_TEST_ROOT%/models"
        }`
	if err := os.WriteFile(cfgFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DataDir != root {
		t.Fatalf("dataDir = %q, want %q", cfg.DataDir, root)
	}
	if cfg.ModelsDir != filepath.Join(root, "models") {
		t.Fatalf("modelsDir = %q, want %q", cfg.ModelsDir, filepath.Join(root, "models"))
	}
	assertNoEnvToken(t, "dataDir", cfg.DataDir)
}

// TestSavedConfigCarriesNoEnvToken — after normalization, a Save/Load
// round-trip persists only real absolute paths.
func TestSavedConfigCarriesNoEnvToken(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SHEYTAN_TEST_ROOT", root)
	t.Setenv("SHEYTAN_DATA_DIR", "%SHEYTAN_TEST_ROOT%")

	cfg, err := Load(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := Save(cfg.ConfigPath(), cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "%LOCALAPPDATA%") || strings.Contains(text, "$SHEYTAN_TEST_ROOT") {
		t.Fatalf("persisted config still carries environment references:\n%s", text)
	}
}
