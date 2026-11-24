// five_platform_contract_test.go — master work order §10/§14 (as carried
// into the CI consolidation): the per-lane build scripts must exist
// as real definitions under scripts/build/, be dispatched by the ONE
// platform-build workflow's five-target matrix, and be REQUIRED by its
// final verdict. The iOS lane script is retired with its lane (run
// 45323044820 repair, owner direction) — the FIVE platform targets are
// windows-x64, linux-x64, macos-arm64, macos-x64, android-arm64. A lane that exists but is not dispatched (or a red lane
// that floats outside the verdict) is a green-washing hazard (§1 rule
// 12: never obtain green status by skipping or masking a gate), so the
// wiring is pinned here, not just the file names.

package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// platformLaneScripts — the exact §10 lane-script set (the successors of
// the retired reusable workflow files; the retired ios.sh belongs to the
// retired iOS lanes and must NOT reappear).
var platformLaneScripts = []string{
	"windows.ps1",
	"linux.sh",
	"macos.sh",
	"android.sh",
}

// retiredLaneScripts — lane scripts that must never reappear in the
// tracked tree (their lanes are retired, run 45323044820 repair).
var retiredLaneScripts = []string{
	"ios.sh",
	"ios-device.sh",
}

// TestPlatformLaneScriptsAreRealDefinitions pins that each §10 lane
// script exists, carries its verification gates (a core `go test`
// invocation), writes the upstream-failure marker the if:always()
// package-gate reporter consumes, and finalizes its package under the
// contracted dist/ name. Stubs would fail these honesty checks.
func TestPlatformLaneScriptsAreRealDefinitions(t *testing.T) {
	root := findRepoRoot(t)
	for _, name := range retiredLaneScripts {
		rel := filepath.Join("scripts", "build", name)
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Errorf("retired lane script %s reappeared — its lane is retired (run 45323044820 repair) and must not come back", rel)
		}
	}
	for _, name := range platformLaneScripts {
		rel := filepath.Join("scripts", "build", name)
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("§10 lane script %s is missing — the five-target matrix is not deliverable", rel)
			continue
		}
		text := string(raw)

		// Real definitions carry at least one core gate — stubs would
		// fail this honesty check.
		if !strings.Contains(text, "go test") {
			t.Errorf("%s runs no test suite — a §10 lane without its core gates is a stub, not a build definition", rel)
		}

		// The upstream-failure marker contract: the first failed gate's
		// step name lands in ci-logs/<target>-upstream-failure.txt so the
		// package-gate reporter names ONE root error.
		if !strings.Contains(text, "upstream-failure.txt") {
			t.Errorf("%s never writes the upstream-failure marker — the if:always() package-gate reporter cannot name the first failed gate", rel)
		}

		// The packaging honesty: the lane references its dist/ output.
		if !strings.Contains(text, "dist/") {
			t.Errorf("%s produces no dist/ package — the platform's portable output must be produced and preserved", rel)
		}
	}
}

// TestPlatformMatrixTargetsDispatchTheLanesAndRequiredByTheVerdict pins
// the consolidated wiring: build.yml defines the five matrix targets
// exactly once (validated structurally), dispatches to the lane scripts,
// and the final verdict needs + inspects the platform-build result — a
// red lane must render the whole run red.
func TestPlatformMatrixTargetsDispatchTheLanesAndRequiredByTheVerdict(t *testing.T) {
	text := readTreeFile(t, workflowRelPath)

	// Structural matrix validation (five targets, unique triples,
	// fail-fast false, no cancel-in-progress).
	if err := ValidatePlatformMatrixStructure(text); err != nil {
		t.Fatalf("five-target matrix contract: %v", err)
	}

	// Every lane script must be dispatched by the workflow.
	for _, name := range platformLaneScripts {
		if !strings.Contains(text, "scripts/build/"+name) {
			t.Errorf("build.yml never dispatches scripts/build/%s — the §10 lane is not wired into the run", name)
		}
	}

	// The final verdict must require and inspect the platform-build
	// result (the five lanes run as ONE matrix job with fail-fast: false —
	// a red lane keeps the run red through the matrix job's conclusion).
	if !strings.Contains(text, "needs."+PlatformBuildJobKey+".result") {
		t.Errorf("the final verdict never inspects needs.%s.result — a red lane must fail the overall verdict, not float outside it", PlatformBuildJobKey)
	}

	// The retired lane files must not reappear.
	root := findRepoRoot(t)
	for _, retired := range RetiredLaneWorkflowFiles {
		if _, err := os.Stat(filepath.Join(root, ".github", "workflows", retired)); err == nil {
			t.Errorf("retired platform workflow file %s reappeared — the consolidation requires exactly build.yml + codeql.yml", retired)
		}
	}
}
