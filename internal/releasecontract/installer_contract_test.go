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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// workflowRelPath is the canonical CI workflow path the release contract
// tests read from the tracked tree.
const workflowRelPath = ".github/workflows/build-desktop.yml"

// The five reusable platform lanes (run 44306343338 §6.2 topology).
const (
	windowsLaneRelPath = ".github/workflows/build-windows.yml"
	linuxLaneRelPath   = ".github/workflows/build-linux.yml"
	macosLaneRelPath   = ".github/workflows/build-macos.yml"
	androidLaneRelPath = ".github/workflows/build-android.yml"
	iosLaneRelPath     = ".github/workflows/build-ios.yml"
)

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

// installerScanSuffixes — the file shapes an installer recipe could
// hide in: Go source, workflow YAML, PowerShell scripts and NSIS
// scripts themselves.
var installerScanSuffixes = []string{".go", ".yml", ".yaml", ".ps1", ".nsi"}

// installerHistoryPaths are the historical notes where the retired
// mechanism may be NAMED (never invoked): changelog, worklog, retired.
var installerHistoryPaths = map[string]bool{
	"docs/changelog.md": true,
	"docs/worklog.md":   true,
	"docs/retired.md":   true,
}

// installerGuardPrefixes are the retirement-guard files themselves
// (internal/releasecontract tests and cmd/stress_release_surface.go):
// they must NAME the forbidden spellings to keep them forbidden.
var installerGuardPrefixes = []string{
	"internal/releasecontract/",
	"cmd/stress_release_surface.go",
}

// isInstallerScanExempt classifies one relative path for the scanner.
// Every input is normalized to forward slashes FIRST (the run
// 44205355718 repair, master work order §0.2 root cause B): the old
// scanner walked the filesystem and matched the guard prefixes against
// the RAW relative path, so on Windows — where filepath.Rel yields
// backslash separators — the forward-slash prefixes never matched and
// the scanner flagged its own guard source as an active installer
// recipe. The normalization is unconditional (strings.ReplaceAll, not
// filepath.ToSlash, which is a no-op for backslashes on non-Windows
// hosts): repo-relative paths are canonically slash-spelled (git
// ls-files emits forward slashes on every OS), and any backslash in
// such a path is a Windows-style spelling of the same file. git
// enumeration (below) already emits forward slashes; the normalization
// keeps the classifier correct for any caller that hands it an
// OS-shaped path.
func isInstallerScanExempt(rel string) (history bool, guard bool) {
	rel = strings.ReplaceAll(rel, `\`, "/")
	if installerHistoryPaths[rel] {
		return true, false
	}
	for _, p := range installerGuardPrefixes {
		if strings.HasPrefix(rel, p) {
			return false, true
		}
	}
	return false, false
}

// installerScanTargets enumerates the TRACKED tree (git ls-files — the
// inventory the contract speaks about; caches, build products and
// developer scratch files never appear) and returns the relative paths
// an installer recipe could hide in (Go source, workflow YAML,
// PowerShell and NSIS scripts). git emits forward-slash paths on every
// OS, so the guard exemptions can no longer be defeated by platform
// separators — the second half of the run 44205355718 repair.
func installerScanTargets(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed (the installer-retirement scan needs the tracked-file inventory): %w", err)
	}
	var targets []string
	for _, rel := range strings.Split(string(out), "\x00") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		for _, suffix := range installerScanSuffixes {
			if strings.HasSuffix(rel, suffix) {
				targets = append(targets, rel)
				break
			}
		}
	}
	return targets, nil
}

// installerRecipeOffenses returns the tracked files (from
// installerScanTargets) that still carry an installer recipe: a
// makensis invocation or an NSIS script.
func installerRecipeOffenses(root string) ([]string, error) {
	targets, err := installerScanTargets(root)
	if err != nil {
		return nil, err
	}
	var offenses []string
	for _, rel := range targets {
		if history, guard := isInstallerScanExempt(rel); history || guard {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			return nil, readErr
		}
		if strings.Contains(string(raw), "makensis") {
			offenses = append(offenses, rel)
		} else if strings.HasSuffix(rel, ".nsi") {
			offenses = append(offenses, rel)
		}
	}
	return offenses, nil
}

// TestTrackedTreeHasNoInstallerRecipes pins that no tracked file still
// carries an installer recipe: a makensis invocation or an installer.nsi
// reference anywhere in the tracked tree means a second packaging lane
// came back. The word "NSIS" alone is allowed ONLY inside historical
// notes (changelog, worklog, retired) — enforced by the path filter in
// isInstallerScanExempt. The retirement-guard files themselves
// (internal/releasecontract tests and cmd/stress_release_surface.go) are
// exempt: they must NAME the forbidden spellings to keep them forbidden.
// The scan enumerates TRACKED files with forward-slash paths on every OS
// (run 44205355718 repair), so the guard can no longer self-match on
// Windows.
func TestTrackedTreeHasNoInstallerRecipes(t *testing.T) {
	root := findRepoRoot(t)
	offenses, err := installerRecipeOffenses(root)
	if err != nil {
		t.Fatalf("installer-retirement scan failed: %v", err)
	}
	for _, rel := range offenses {
		t.Errorf("%s still carries an installer recipe — installer production is retired (master work order §10)", rel)
	}
	if len(offenses) == 0 {
		// The guard fixtures below pin the scanner's teeth; the empty
		// result here is the contract's healthy state.
		t.Log("no installer recipes in the tracked tree")
	}
}

// TestInstallerScanExemptPathsSurviveWindowsSeparators is the run
// 44205355718 regression pin (master work order §0.2 root cause B): the
// guard/history exemptions must classify OS-shaped (backslash) paths
// exactly like forward-slash paths, and the guard's own source files —
// which must literally contain "makensis" — must never be flagged.
func TestInstallerScanExemptPathsSurviveWindowsSeparators(t *testing.T) {
	cases := []struct {
		rel         string
		wantHistory bool
		wantGuard   bool
	}{
		{"internal/releasecontract/installer_contract_test.go", false, true},
		{`internal\releasecontract\installer_contract_test.go`, false, true},
		{"internal/releasecontract/packagegate_contract_test.go", false, true},
		{`internal\releasecontract\packagegate_contract_test.go`, false, true},
		{"cmd/stress_release_surface.go", false, true},
		{`cmd\stress_release_surface.go`, false, true},
		{"docs/changelog.md", true, false},
		{`docs\retired.md`, true, false},
		{"cmd/ordinary.go", false, false},
		{`packaging\nsis\installer.nsi`, false, false},
		{"docs/ARCHITECTURE.md", false, false},
	}
	for _, tc := range cases {
		history, guard := isInstallerScanExempt(tc.rel)
		if history != tc.wantHistory || guard != tc.wantGuard {
			t.Errorf("isInstallerScanExempt(%q) = (history=%v, guard=%v), want (history=%v, guard=%v)", tc.rel, history, guard, tc.wantHistory, tc.wantGuard)
		}
	}
	// The guard sources really do carry the forbidden literal — the
	// exemption is load-bearing, not vacuous.
	for _, guard := range []string{
		"cmd/stress_release_surface.go",
		"internal/releasecontract/installer_contract_test.go",
		"internal/releasecontract/packagegate_contract_test.go",
	} {
		raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), filepath.FromSlash(guard)))
		if err != nil {
			t.Fatalf("read guard source %s: %v", guard, err)
		}
		if !strings.Contains(string(raw), "makensis") {
			t.Errorf("guard source %s no longer contains the forbidden literal — the exemption and the guard drifted apart", guard)
		}
	}
}

// TestInstallerScanEnumeratesTrackedFilesWithForwardSlashes pins the
// enumeration contract on the real repository: every scan target comes
// from git ls-files (tracked only) and is spelled with forward slashes
// on every OS — the shape the exemption classifier requires.
func TestInstallerScanEnumeratesTrackedFilesWithForwardSlashes(t *testing.T) {
	root := findRepoRoot(t)
	targets, err := installerScanTargets(root)
	if err != nil {
		t.Fatalf("enumerate scan targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("the scan target enumeration produced nothing — the tracked-file inventory is broken")
	}
	for _, rel := range targets {
		if strings.Contains(rel, `\`) {
			t.Errorf("scan target %q contains a backslash — the enumeration must emit forward-slash paths on every OS", rel)
		}
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
