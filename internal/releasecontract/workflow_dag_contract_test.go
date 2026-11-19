package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workflowRelPathForDAG points at the REAL consolidated platform-build
// workflow — the DAG contract reads the file that ships, never a copied
// example. (The retired build-desktop.yml orchestrator + five reusable
// lane files were merged into this ONE file by the CI consolidation.)
var dagWorkflowRelPath = filepath.Join("..", "..", ".github", "workflows", "build.yml")

// codeqlWorkflowRelPath points at the REAL CodeQL workflow.
var codeqlWorkflowRelPath = filepath.Join("..", "..", ".github", "workflows", "codeql.yml")

// loadWorkflowForDAG reads the real workflow and returns it in the
// CANONICAL text form the DAG contract reasons about: UTF-8 without a
// BOM, LF line endings.
//
// Run-43718750096 correction (Windows x64 red, the two portability
// failures this loader fixes): the hosted Windows runner materializes
// the working tree with Git-for-Windows' default core.autocrlf=true, and
// .gitattributes deliberately pins only *.go (LF) and *.bat (CRLF) —
// *.yml falls through to the platform default, so the workflow arrives
// with CRLF line endings on Windows while staying LF on Linux. The
// semantic YAML validators never cared (yaml.v3 accepts CRLF), which is
// why every positive DAG test stayed green on both platforms — but the
// REGRESSION fixtures in this file perform multi-line string surgery
// with LF-only needles, and under CRLF those needles matched nothing:
// the "mutated" workflow was byte-identical to the healthy one, the
// validator correctly accepted it, and the tests failed as "the
// regression detector is broken" — a fixture portability bug masquerading
// as a contract bug.
//
// Normalizing HERE (the single load seam) keeps the contract tests
// reading the file that ships while making the fixture surgery
// deterministic on every platform; the mutation tests additionally guard
// that each injection actually applied, so a future workflow-header
// reshape fails loudly as a fixture-shape error instead of silently
// testing the unmutated workflow. The validators themselves are
// unchanged and remain CRLF-tolerant for any other caller.
func loadWorkflowForDAG(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read workflow %s: %v", rel, err)
	}
	// Canonical form: strip a UTF-8 BOM if present, then CRLF (and lone
	// CR) to LF. No content-bearing byte is altered.
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// TestPlatformBuildWorkflowDAG proves the consolidated DAG contract
// against the real YAML: the platform-build matrix job has no needs
// (every matrix instance is an independent gate), the matrix structure
// is exactly the six contracted targets with fail-fast false, the
// collector is failure-tolerant, and the strict final verdict exists and
// inspects every required gate.
func TestPlatformBuildWorkflowDAG(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	if err := ValidateIndependentGatesDAG(w); err != nil {
		t.Fatalf("independent-gates DAG contract: %v", err)
	}
	if err := ValidatePlatformMatrixStructure(w); err != nil {
		t.Fatalf("platform matrix structure contract: %v", err)
	}
	if err := ValidateFinalVerdictJob(w); err != nil {
		t.Fatalf("final-verdict contract: %v", err)
	}
	if err := ValidateFailureTolerantCollector(w); err != nil {
		t.Fatalf("failure-tolerant collector contract: %v", err)
	}
}

// TestRedPlatformBuildKeepsCollectorRunning is the run-43630164424
// regression fixture, recast for the matrix topology: with the
// platform-build matrix FAILED, the collector still runs to publish the
// evidence (its always() condition), the final verdict still renders,
// and the verdict is FAILURE — a red run with full evidence, never
// skipped gates.
func TestRedPlatformBuildKeepsCollectorRunning(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	outcomes, err := SimulateGateMatrix(w, map[string]string{
		PlatformBuildJobKey: "failure",
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}

	if outcomes[CollectorJobKey] == "skipped" {
		t.Fatalf("a red matrix skipped %q — the run-43630164424 orchestration defect is back: the collector must run after any platform outcome (outcome=%q)", CollectorJobKey, outcomes[CollectorJobKey])
	}
	if outcomes[CollectorJobKey] != "success" {
		t.Fatalf("collector must still run on a red matrix, got %q", outcomes[CollectorJobKey])
	}
	if outcomes[FinalVerdictJobKey] == "skipped" {
		t.Fatalf("a red matrix skipped %q — the verdict must still render", FinalVerdictJobKey)
	}
	if !verdictFails(outcomes) {
		t.Fatalf("final verdict must be FAILURE when platform-build failed: %+v", outcomes)
	}
}

// TestRedCollectorCannotHideGreenMatrix is the second mandatory fixture:
// platform-build green, the collector RED (a coverage gap) — the final
// verdict is FAILURE. The collector is a REQUIRED gate in the verdict
// loop: report-generation success can never override a failed gate, and
// a collector failure can never be waved through either.
func TestRedCollectorCannotHideGreenMatrix(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	outcomes, err := SimulateGateMatrix(w, map[string]string{
		PlatformBuildJobKey: "success",
		CollectorJobKey:     "failure",
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}

	if outcomes[PlatformBuildJobKey] != "success" {
		t.Fatalf("platform-build must stay green in this fixture, got %q", outcomes[PlatformBuildJobKey])
	}
	if !verdictFails(outcomes) {
		t.Fatalf("final verdict must be FAILURE when the collector reported a coverage gap: %+v", outcomes)
	}
}

// TestNeedsChainedPlatformBuildIsRejected proves the contract FAILS
// against the needs-chained shape (a needs list on the platform-build
// job) — the exact shape that turned run 43630164424 into a red run with
// skipped gates, and the platform-to-platform needs chain the matrix
// replaced.
//
// Run-43718750096 portability note: the surgery below uses LF-only
// needles; loadWorkflowForDAG normalizes the real file to LF first, and
// the application guard proves every injection landed — under the old
// loader a CRLF checkout silently produced an unmutated workflow and
// this test failed as a broken detector instead of a broken fixture.
func TestNeedsChainedPlatformBuildIsRejected(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	// Re-introduce the run-43630164424 defect shape on the matrix job.
	mutated := strings.Replace(w,
		"  platform-build:\n    name:",
		"  platform-build:\n    needs: [collect-errors]\n    name:", 1)
	if mutated == w {
		t.Fatalf("the defect-shape surgery did not apply — the platform-build job header in build.yml changed; update the mutation needle in this test (a silent no-op here once produced a false 'broken detector' failure on Windows)")
	}

	if err := ValidateIndependentGatesDAG(mutated); err == nil {
		t.Fatalf("a needs-chained platform-build job passed the contract — the regression detector is broken")
	}

	// And the simulator must show the chained shape as unrunnable (a
	// needs cycle), never as a silently successful run.
	if _, err := SimulateGateMatrix(mutated, map[string]string{PlatformBuildJobKey: "failure"}); err == nil {
		t.Fatalf("the needs-chained DAG simulated successfully — a platform-to-platform needs cycle must be unrunnable, not silently green")
	}
}

// TestContinueOnErrorIsRejected: an active continue-on-error on a
// required gate must fail the contract (green-washing hazard).
//
// Run-43718750096 portability note: same CRLF defect class as
// TestNeedsChainedPlatformBuildIsRejected — the LF-normalized loader
// plus the application guard below keep the injection real on every
// platform.
func TestContinueOnErrorIsRejected(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)
	greenwashed := strings.Replace(w,
		"  platform-build:\n    name:",
		"  platform-build:\n    continue-on-error: true\n    name:", 1)
	if greenwashed == w {
		t.Fatalf("the continue-on-error injection did not apply — the platform-build job header in build.yml changed; update the mutation needle in this test (a silent no-op here once produced a false 'broken detector' failure on Windows)")
	}

	if err := ValidateFinalVerdictJob(greenwashed); err == nil {
		t.Fatalf("continue-on-error on a required gate passed the contract — a failed gate must stay red, never hidden")
	}
}

// verdictFails mirrors the final-verdict step's logic: the verdict is
// FAILURE whenever any required gate is not success (a skipped required
// gate also fails — only the tag-only release may be legitimately
// skipped).
func verdictFails(outcomes map[string]string) bool {
	for _, gate := range RequiredGateJobs {
		if outcomes[gate] != "success" {
			return true
		}
	}
	return false
}

// TestCodeQLWorkflowResilience proves the CodeQL contract on the real
// YAML: independent triggers, no platform-build dependency, Go + actions
// scanning with security-and-quality, analyze-after-build-failure,
// upload: always, and the failure-tolerant collector.
func TestCodeQLWorkflowResilience(t *testing.T) {
	w := loadWorkflowForDAG(t, codeqlWorkflowRelPath)

	if err := ValidateCodeQLWorkflowResilience(w); err != nil {
		t.Fatalf("CodeQL resilience contract: %v", err)
	}
}

// TestCodeQLAnalyzeSurvivesBuildFailure is the §9.6 CodeQL fixture:
// init=success, manual build=failure — the analyze step still runs and
// uploads SARIF (upload: always), the analysis is a PARTIAL result (not
// a green scan), and the collect-codeql-logs job still publishes the
// diagnostics. The workflow must not be falsely reported as fully green.
func TestCodeQLAnalyzeSurvivesBuildFailure(t *testing.T) {
	w := loadWorkflowForDAG(t, codeqlWorkflowRelPath)

	// The condition must evaluate to true in the failure scenario:
	// !cancelled() && steps.codeql-init.outcome == 'success'.
	if !strings.Contains(w, "if: ${{ !cancelled() && steps.codeql-init.outcome == 'success' }}") {
		t.Fatalf("the analyze step condition does not match the contracted form (init-success + !cancelled)")
	}
	if !strings.Contains(w, "upload: always") {
		t.Fatalf("the analyze step must upload SARIF with upload: always")
	}

	// Simulate the scheduling semantics: analyze FAILED (its build step
	// failed — the job conclusion preserves the build failure), so the
	// export report and the collector must still run (!cancelled()).
	outcomes, err := SimulateGateMatrix(codeQLDAGFixture(), map[string]string{
		"analyze": "failure",
	})
	if err != nil {
		t.Fatalf("simulate CodeQL DAG: %v", err)
	}
	if outcomes["export-alert-report"] != "success" {
		t.Fatalf("export-alert-report must still run after a red analyze matrix (!cancelled()), got %q", outcomes["export-alert-report"])
	}
	if outcomes["collect-codeql-logs"] != "success" {
		t.Fatalf("collect-codeql-logs must still publish diagnostics after a red analyze matrix, got %q", outcomes["collect-codeql-logs"])
	}

	// The workflow is NOT green: the analyze job's own conclusion is
	// failure (preserved from the failed manual build — no
	// continue-on-error wrapped the build), even though its analyze step
	// ran and uploaded partial SARIF.
	if outcomes["analyze"] != "failure" {
		t.Fatalf("the analyze job must stay red after a failed manual build — partial SARIF is diagnostics, not a green gate")
	}
}

// codeQLDAGFixture is a minimal model of the CodeQL workflow's job DAG
// with the same needs/if semantics as the real file, so the scheduling
// simulation can run without embedding the entire workflow text.
func codeQLDAGFixture() string {
	return `name: CodeQL fixture
on:
  push:
  pull_request:
  schedule:
    - cron: "17 4 * * 1"
  workflow_dispatch:
jobs:
  analyze:
    runs-on: ubuntu-26.04
    steps:
      - name: Initialize CodeQL
        id: codeql-init
        uses: github/codeql-action/init@v4
      - name: Build all Go packages for CodeQL
        run: go build ./...
      - name: Analyze and upload CodeQL results
        if: ${{ !cancelled() && steps.codeql-init.outcome == 'success' }}
        uses: github/codeql-action/analyze@v4
        with:
          upload: always
  export-alert-report:
    needs: [analyze]
    if: ${{ !cancelled() }}
    runs-on: ubuntu-26.04
    steps:
      - run: echo report
  collect-codeql-logs:
    needs: [analyze, export-alert-report]
    if: ${{ !cancelled() }}
    runs-on: ubuntu-26.04
    steps:
      - run: bash scripts/ci/consolidate-run-log.sh
`
}

// TestCodeQLResilienceRejectsRegressions proves the CodeQL validator
// fails against the pre-repair shapes: a build-gated analyze (no
// condition), upload not always, no collector, a success-gated
// collector, an id-less init step, and a dropped schedule trigger.
// The fixture carries no language matrix, so the base fixture also
// fails the scanning-scope check — reduced scope is a contract failure,
// never a pass.
func TestCodeQLResilienceRejectsRegressions(t *testing.T) {
	base := codeQLDAGFixture()

	// Sanity: the structural mutations below must be the ONLY reason the
	// base fixture fails — the scope failure (go+actions matrix missing)
	// is asserted separately afterwards.
	mutations := []struct{ name, mutated string }{
		{"no-analyze-condition", strings.Replace(base, "if: ${{ !cancelled() && steps.codeql-init.outcome == 'success' }}\n        uses: github/codeql-action/analyze", "uses: github/codeql-action/analyze", 1)},
		{"upload-failure-only", strings.Replace(base, "upload: always", "upload: failure-only", 1)},
		{"collector-removed", base[:strings.Index(base, "  collect-codeql-logs:")]},
		{"collector-success-gated", strings.Replace(base, "needs: [analyze, export-alert-report]\n    if: ${{ !cancelled() }}", "needs: [analyze, export-alert-report]", 1)},
		{"init-no-id", strings.Replace(base, "id: codeql-init\n", "", 1)},
		{"no-schedule", strings.Replace(base, "  schedule:\n    - cron: \"17 4 * * 1\"\n", "", 1)},
	}

	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCodeQLWorkflowResilience(tc.mutated); err == nil {
				t.Fatalf("mutation %q passed the CodeQL resilience contract — the regression detector is broken", tc.name)
			}
		})
	}

	// Scoping guard: a workflow without the go+actions language coverage
	// fails the contract (the fixture intentionally carries no matrix).
	err := ValidateCodeQLWorkflowResilience(base)
	if err == nil {
		t.Fatalf("a workflow without the go+actions matrix must fail the contract (scanning scope must not be reduced)")
	}
	if !strings.Contains(err.Error(), "language") && !strings.Contains(err.Error(), "security-and-quality") {
		t.Fatalf("expected a scanning-scope failure, got: %v", err)
	}
}

// TestSixMatrixTargetsDefinedOnce is the CI-consolidation successor of
// the run 44306343338 §6.2 five-lane fixture: the ONE platform-build
// workflow defines the SIX matrix targets exactly once each (no
// duplicate platform-to-platform mapping), the collector and verdict are
// wired to the matrix job, and the retired five reusable lane files are
// GONE — a reusable-lane invocation or a reappearing lane file recreates
// the multi-file topology the consolidation removed.
func TestSixMatrixTargetsDefinedOnce(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	if err := ValidatePlatformMatrixStructure(w); err != nil {
		t.Fatalf("six-target matrix contract: %v", err)
	}

	// No reusable-lane invocation may survive.
	if strings.Contains(w, "uses: ./.github/workflows/build-") {
		t.Fatalf("the workflow still invokes a reusable platform lane via workflow_call — the six-target matrix replaced the five reusable files")
	}

	// The retired lane files must be absent from the tracked tree.
	root := findRepoRoot(t)
	for _, retired := range RetiredLaneWorkflowFiles {
		if _, err := os.Stat(filepath.Join(root, ".github", "workflows", retired)); err == nil {
			t.Errorf("retired platform workflow file %s reappeared — the consolidation requires exactly build.yml + codeql.yml", retired)
		}
	}

	// The collector and the verdict must be wired to the matrix job.
	if err := ValidateFailureTolerantCollector(w); err != nil {
		t.Fatalf("collector wiring: %v", err)
	}
	if err := ValidateFinalVerdictJob(w); err != nil {
		t.Fatalf("verdict wiring: %v", err)
	}
}
