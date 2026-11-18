package releasecontract

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/brand"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// TestReleaseVersionSingleSource pins the ONE release-identity authority
// (v1.11.0 one-Go-UI migration): internal/config.AppVersion as the numeric
// BUILD IDENTIFIER (CI/packaging plumbing), plus the ALPHA user-facing
// product identity (master work order §12.1). The former Node authority
// (package.json + scripts/release-version.mjs) is retired; these surfaces
// must agree:
//
//   - internal/config.AppVersion           (THE numeric build identifier)
//   - build/config.yml  productVersion     (packaging metadata the
//     workflow greps) + productStage (ALPHA display identity)
//   - SIGNATURE first line                 (ALPHA release identity)
//
// A build-identifier bump edits AppVersion and regenerates SIGNATURE;
// this test fails the build if any surface drifts.
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

	// SIGNATURE line 1: ALPHA product identity (master work order §12.1);
	// the numeric build identifier stays in build/config.yml productVersion
	// and cmd/printversion (CI plumbing), never in user-facing branding.
	sig, err := os.ReadFile(filepath.Join(root, "SIGNATURE"))
	if err != nil {
		t.Fatalf("read SIGNATURE: %v", err)
	}
	first := strings.SplitN(string(sig), "\n", 2)[0]
	want := brand.FullName + " " + brand.Stage
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

// findRepoRoot resolves the module root (the directory containing
// go.mod) WITHOUT depending on the process working directory.
//
// Primary resolution: runtime.Caller(0) — this helper's own compiled
// source path IS the releasecontract package directory, so the walk up
// to go.mod works no matter where the test binary was launched from
// (go test happens to set the package dir as CWD; a manually executed
// test binary does not, and the run-42405993776 Windows investigation
// requires loader resolution that is independent of both).
//
// Fallback: the historical getwd walk (covers rewritten Caller paths,
// e.g. -trimpath builds, where the primary walk cannot find go.mod).
func findRepoRoot(t *testing.T) string {
	t.Helper()

	if _, thisFile, _, ok := runtime.Caller(0); ok {
		if root, found := walkUpToGoMod(filepath.Dir(thisFile), 8); found {
			return root
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	if root, found := walkUpToGoMod(dir, 8); found {
		return root
	}

	t.Fatal("go.mod not found above the test package (searched from the test source directory and from the working directory)")
	return ""
}

// walkUpToGoMod walks up from dir at most max levels looking for go.mod.
func walkUpToGoMod(dir string, max int) (string, bool) {
	for i := 0; i < max; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	return "", false
}
