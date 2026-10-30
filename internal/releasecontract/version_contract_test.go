package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// TestReleaseVersionSingleSource pins the ONE release-identity authority
// (v1.11.0 one-Go-UI migration): internal/config.AppVersion. The former
// Node authority (package.json + scripts/release-version.mjs) is retired;
// these surfaces must agree with the Go value:
//
//   - internal/config.AppVersion           (THE authority)
//   - build/config.yml  productVersion     (retained as
//     packaging metadata the workflow greps)
//   - SIGNATURE first line                 (release identity)
//
// A version bump edits AppVersion and SIGNATURE; this test fails the
// build if any surface drifts.
func TestReleaseVersionSingleSource(t *testing.T) {
	root := findRepoRoot(t)

	// build/config.yml
	raw, err := os.ReadFile(filepath.Join(root, "build", "config.yml"))
	if err != nil {
		t.Fatalf("read build/config.yml: %v", err)
	}
	if !strings.Contains(string(raw), `productVersion: "`+config.AppVersion+`"`) {
		t.Errorf("build/config.yml productVersion must equal AppVersion %q", config.AppVersion)
	}

	// SIGNATURE line 1: "SHEYTAN-Local-Agent v<version>"
	sig, err := os.ReadFile(filepath.Join(root, "SIGNATURE"))
	if err != nil {
		t.Fatalf("read SIGNATURE: %v", err)
	}
	first := strings.SplitN(string(sig), "\n", 2)[0]
	want := "SHEYTAN-Local-Agent v" + config.AppVersion
	if strings.TrimSpace(first) != want {
		t.Errorf("SIGNATURE identity = %q, want %q", strings.TrimSpace(first), want)
	}
}

// TestNoNodeReleaseIdentityRemains pins the retirement of the Node-based
// version authority: no package.json, no release-version script, no
// node_modules manifest may exist in the tracked tree.
func TestNoNodeReleaseIdentityRemains(t *testing.T) {
	root := findRepoRoot(t)
	for _, gone := range []string{
		"package.json",
		"package-lock.json",
		"scripts/release-version.mjs",
		"scripts/release-version.test.mjs",
	} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s must not exist (the Go version contract is the only authority)", gone)
		}
	}
}

// TestNoLegacyUISourceRemains pins the one-Go-UI end state: the retired
// Qt/QML and React/TypeScript/Vite/Playwright UI implementations and
// their build plumbing are gone from the tracked tree.
func TestNoLegacyUISourceRemains(t *testing.T) {
	root := findRepoRoot(t)
	for _, gone := range []string{
		"ui/qt",
		"src",
		"web",
		"e2e",
		"index.html",
		"vite.config.ts",
		"tsconfig.json",
		"scripts/sync-web.mjs",
		"scripts/qt-desktop-smoke.sh",
		"scripts/verify-static-assets.mjs",
		"scripts/closewin.c",
		"ui/fyne/shell.go.bak",
	} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("legacy UI artifact %s must not exist after the migration", gone)
		}
	}
}

// findRepoRoot walks up from this package to the module root (the
// directory containing go.mod).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("go.mod not found above the test package")
	return ""
}
