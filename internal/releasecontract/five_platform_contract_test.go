// five_platform_contract_test.go — master work order §10/§14: five
// dedicated platform workflows must exist as REAL reusable definitions,
// be invoked by the build-desktop orchestrator via workflow_call, and be
// REQUIRED by its final verdict. A platform workflow that exists but is
// not required by the verdict is a green-washing hazard (§1 rule 12:
// never obtain green status by skipping or masking a gate), so the
// orchestrator wiring is pinned here, not just the file names.

package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fivePlatformWorkflows — the exact §10 file set.
var fivePlatformWorkflows = []string{
	"build-windows.yml",
	"build-linux.yml",
	"build-macos.yml",
	"build-android.yml",
	"build-ios.yml",
}

// TestFivePlatformWorkflowsAreRealReusableDefinitions pins that each §10
// workflow file exists, is triggered ONLY by workflow_call (the
// orchestrator owns the push/PR triggers — duplicate push builds are a
// documented anti-goal), carries least-privilege permissions, and pins
// its external actions to immutable 40-hex SHAs (the repo-wide
// action-reference policy).
func TestFivePlatformWorkflowsAreRealReusableDefinitions(t *testing.T) {
	root := findRepoRoot(t)
	for _, name := range fivePlatformWorkflows {
		rel := filepath.Join(".github", "workflows", name)
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("§10 workflow %s is missing — the five-platform matrix is not deliverable", rel)
			continue
		}
		text := string(raw)

		if !strings.Contains(text, "workflow_call") {
			t.Errorf("%s does not declare a workflow_call trigger — it must be a reusable workflow invoked by the orchestrator", rel)
		}
		if strings.Contains(text, "push:") || strings.Contains(text, "pull_request:") {
			t.Errorf("%s declares its own push/pull_request trigger — the orchestrator owns the triggers (duplicate push builds are forbidden)", rel)
		}
		if !strings.Contains(text, "contents: read") {
			t.Errorf("%s does not carry least-privilege permissions (contents: read)", rel)
		}
		// Real definitions carry at least one build/test/verify step —
		// stubs would fail this honesty check.
		if !strings.Contains(text, "go test") {
			t.Errorf("%s runs no test suite — a §10 platform lane without its core gates is a stub, not a build definition", rel)
		}
		if !strings.Contains(text, "actions/upload-artifact@") {
			t.Errorf("%s uploads no artifact — the platform's portable output must be produced and preserved", rel)
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "uses: ") && !strings.HasPrefix(line, "uses: ./.github/workflows/") {
				ref := strings.TrimPrefix(line, "uses: ")
				if i := strings.IndexByte(ref, ' '); i >= 0 {
					ref = ref[:i]
				}
				at := strings.LastIndexByte(ref, '@')
				sha := ref[at+1:]
				if len(sha) != 40 {
					t.Errorf("%s references %q — every external action must be pinned to an immutable 40-hex commit SHA", rel, ref)
				}
			}
		}
	}
}

// TestFivePlatformWorkflowsAreInvokedAndRequiredByTheVerdict pins the
// orchestrator wiring: build-desktop.yml calls all five via
// `uses: ./.github/workflows/build-*.yml` jobs, the collector waits for
// them, and the final verdict needs + inspects every platform job's
// result — a red platform lane must render the whole run red.
func TestFivePlatformWorkflowsAreInvokedAndRequiredByTheVerdict(t *testing.T) {
	text := readTreeFile(t, workflowRelPath)

	invocations := map[string]bool{
		"./.github/workflows/build-windows.yml": false,
		"./.github/workflows/build-linux.yml":   false,
		"./.github/workflows/build-macos.yml":   false,
		"./.github/workflows/build-android.yml": false,
		"./.github/workflows/build-ios.yml":     false,
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "uses: ") {
			continue
		}
		ref := strings.TrimPrefix(line, "uses: ")
		if _, wanted := invocations[ref]; wanted {
			invocations[ref] = true
		}
	}
	for uses, found := range invocations {
		if !found {
			t.Errorf("build-desktop.yml never invokes %s — the orchestrator must call all five §10 platform workflows via workflow_call", uses)
		}
	}

	// The final verdict must require and inspect every platform job.
	for _, job := range []string{
		"platform-windows",
		"platform-linux",
		"platform-macos",
		"platform-android",
		"platform-ios",
	} {
		if !strings.Contains(text, "needs."+job+".result") {
			t.Errorf("the final verdict never inspects needs.%s.result — a red platform lane must fail the overall verdict, not float outside it", job)
		}
		if !strings.Contains(text, job) {
			t.Errorf("build-desktop.yml has no %s job — the §10 lane is not wired into the run", job)
		}
	}
}
