// installer_contract_test.go — master work order §10 (installer
// retirement). The former NSIS installer contracts (run #122 deep-fix,
// payload-uninstall symmetry, makensis diagnosability) are RETIRED along
// with the installer itself: the portable ZIP is the ONE supported
// Windows package, and this file is now the permanent guard that the
// installer surface never reappears in the tracked tree or the CI
// workflow. Historical run references are preserved as history; the
// surfaces they validated are gone.

package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workflowRelPath is the canonical CI workflow path the release contract
// tests read from the tracked tree.
const workflowRelPath = ".github/workflows/build-desktop.yml"

// readTreeFile reads one tracked file relative to the repository root —
// the shared helper for tree-content contract tests.
func readTreeFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract source %s unreadable: %v", rel, err)
	}
	return string(data)
}

// TestInstallerSourcesAreRemoved pins the §10 end state: the NSIS script,
// its generated payload manifest, the version-gate PowerShell fixtures and
// every other installer-production source are gone from the tracked tree.
func TestInstallerSourcesAreRemoved(t *testing.T) {
	root := findRepoRoot(t)
	for _, gone := range []string{
		"packaging/nsis",
		"scripts/ci/NsisVersion.ps1",
		"scripts/ci/NsisVersion.Tests.ps1",
	} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s must not exist — installer production is retired (master work order §10)", gone)
		}
	}
}

// TestTrackedTreeHasNoInstallerRecipes pins that no tracked file still
// carries an installer recipe: a makensis invocation or an installer.nsi
// reference anywhere in the tree means a second packaging lane came back.
// The word "NSIS" alone is allowed ONLY inside historical notes
// (changelog, worklog, retired) — enforced by the path filter below.
// The retirement-guard files themselves (internal/releasecontract tests
// and cmd/stress_release_surface.go) are exempt: they must NAME the
// forbidden spellings to keep them forbidden.
func TestTrackedTreeHasNoInstallerRecipes(t *testing.T) {
	root := findRepoRoot(t)
	historyPaths := map[string]bool{
		"docs/changelog.md": true,
		"docs/worklog.md":   true,
		"docs/retired.md":   true,
	}
	guardPrefixes := []string{
		"internal/releasecontract/",
		"cmd/stress_release_surface.go",
	}
	isGuard := func(rel string) bool {
		for _, p := range guardPrefixes {
			if strings.HasPrefix(rel, p) {
				return true
			}
		}
		return false
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			// Build/VCS directories hold no contracts; .git is not tracked.
			switch rel {
			case ".git", "dist", "ci-logs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, ".yml") && !strings.HasSuffix(rel, ".yaml") && !strings.HasSuffix(rel, ".ps1") && !strings.HasSuffix(rel, ".nsi") {
			return nil
		}
		if historyPaths[rel] || isGuard(rel) {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		if strings.Contains(text, "makensis") {
			t.Errorf("%s still references makensis — installer recipes are retired (master work order §10)", rel)
		}
		if strings.HasSuffix(rel, ".nsi") {
			t.Errorf("%s is an NSIS script — installer recipes are retired (master work order §10)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tracked tree: %v", err)
	}
}

// TestRetiredSurfaceIsRegisteredInRetiredDocs pins the repo's retirement
// discipline: docs/retired.md must carry an NSIS/installer entry so the
// retirement is discoverable and no doc still presents the installer as
// supported.
func TestRetiredSurfaceIsRegisteredInRetiredDocs(t *testing.T) {
	root := findRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "retired.md"))
	if err != nil {
		t.Fatalf("read docs/retired.md: %v", err)
	}
	if !strings.Contains(string(raw), "NSIS") {
		t.Error("docs/retired.md does not register the NSIS installer retirement (master work order §10)")
	}
}
