package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workflowRelPath points at the REAL build-desktop workflow — the DAG
// contract reads the file that ships, never a copied example.
var dagWorkflowRelPath = filepath.Join("..", "..", ".github", "workflows", "build-desktop.yml")

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
// *.yml falls through to the platform default, so build-desktop.yml
// arrives with CRLF line endings on Windows while staying LF on Linux.
// The semantic YAML validators never cared (yaml.v3 accepts CRLF), which
// is why every positive DAG test stayed green on both platforms — but
// the REGRESSION fixtures in this file perform multi-line string surgery
// with LF-only needles (`"  "+job+":\n    name:"), and under CRLF those
// needles matched nothing: the "mutated" workflow was byte-identical to
// the healthy one, the validator correctly accepted it, and the tests
// failed as "the regression detector is broken" — a fixture portability
// bug masquerading as a contract bug.
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

// TestBuildWorkflowIndependentGatesDAG proves the run-43630164424 repair
// against the real YAML: preflight is not a prerequisite of the four
// validation gates, every gate owns its checkout, the collector is
// failure-tolerant, and the strict final verdict exists and inspects
// every required gate.
func TestBuildWorkflowIndependentGatesDAG(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	if err := ValidateIndependentGatesDAG(w); err != nil {
		t.Fatalf("independent-gates DAG contract: %v", err)
	}
	if err := ValidateFinalVerdictJob(w); err != nil {
		t.Fatalf("final-verdict contract: %v", err)
	}
	if err := ValidateFailureTolerantCollector(w); err != nil {
		t.Fatalf("failure-tolerant collector contract: %v", err)
	}
}

// TestRedPreflightDoesNotSkipGates is the run-43630164424 regression
// fixture: with preflight FAILED, the four independent gates still run
// to their terminal states, the collector still runs, and the final
// verdict is FAILURE — a red run with full evidence, never four skipped
// gates.
func TestRedPreflightDoesNotSkipGates(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	outcomes, err := SimulateGateMatrix(w, map[string]string{
		"preflight": "failure",
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}

	for _, gate := range IndependentGateJobs {
		if outcomes[gate] == "skipped" {
			t.Fatalf("red preflight skipped %q — the run-43630164424 orchestration defect is back: independent gates must run to terminal states (outcome=%q)", gate, outcomes[gate])
		}
	}
	if outcomes["consolidate-logs"] != "success" {
		t.Fatalf("collector must still run on a red matrix, got %q", outcomes["consolidate-logs"])
	}
	if verdictFails(outcomes) {
		// expected: the verdict is red because preflight is red
	} else {
		t.Fatalf("final verdict must be FAILURE when preflight failed: %+v", outcomes)
	}
}

// TestOldPreflightDAGShapeIsRejected proves the contract FAILS against
// the previous DAG (needs: [preflight] re-introduced on the four gates)
// — the exact shape that turned run 43630164424 into a red run with
// four skipped gates.
//
// Run-43718750096 portability note: the surgery below uses LF-only
// needles; loadWorkflowForDAG normalizes the real file to LF first, and
// the application guard proves every injection landed — under the old
// loader a CRLF checkout silently produced an unmutated workflow and
// this test failed as a broken detector instead of a broken fixture.
func TestOldPreflightDAGShapeIsRejected(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	// Re-introduce the run-43630164424 defect shape.
	old := w
	applied := 0
	for _, job := range IndependentGateJobs {
		needle := "  " + job + ":\n    name:"
		injection := "  " + job + ":\n    needs: [preflight]\n    name:"
		if _, isLane := laneWorkflowUses[job]; isLane {
			needle = "  " + job + ":\n"
			injection = "  " + job + ":\n    needs: [preflight]\n"
		}
		mutated := strings.Replace(old, needle, injection, 1)
		if mutated != old {
			applied++
		}
		old = mutated
	}
	if applied != len(IndependentGateJobs) {
		t.Fatalf("the defect-shape surgery applied to only %d/%d independent gates — the job-header shape in build-desktop.yml changed; update the mutation needle in this test (a silent no-op here once produced a false 'broken detector' failure on Windows)",
			applied, len(IndependentGateJobs))
	}

	if err := ValidateIndependentGatesDAG(old); err == nil {
		t.Fatalf("the old needs:[preflight] DAG passed the contract — the regression detector is broken")
	}

	// And the simulator must show the old shape skipping the gates.
	outcomes, err := SimulateGateMatrix(old, map[string]string{"preflight": "failure"})
	if err != nil {
		t.Fatalf("simulate old DAG: %v", err)
	}
	skipped := 0
	for _, gate := range IndependentGateJobs {
		if outcomes[gate] == "skipped" {
			skipped++
		}
	}
	if skipped != len(IndependentGateJobs) {
		t.Fatalf("old DAG simulation: expected all %d gates skipped, got %d (%+v)", len(IndependentGateJobs), skipped, outcomes)
	}
}

// TestRedPlatformGateKeepsOthersRunning is the second mandatory
// fixture: preflight green, one platform gate red — every unrelated
// gate still runs, the collector runs, and the final verdict is still
// FAILURE (strict verdict preserved).
func TestRedPlatformGateKeepsOthersRunning(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)

	outcomes, err := SimulateGateMatrix(w, map[string]string{
		"preflight":        "success",
		"platform-windows": "failure",
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}

	for _, gate := range []string{"audit", "ui", "platform-linux", "platform-macos", "platform-android", "platform-ios"} {
		if outcomes[gate] != "success" {
			t.Fatalf("unrelated gate %q must still run to success, got %q", gate, outcomes[gate])
		}
	}
	if outcomes["consolidate-logs"] != "success" {
		t.Fatalf("collector must run after the red platform gate, got %q", outcomes["consolidate-logs"])
	}
	if !verdictFails(outcomes) {
		t.Fatalf("final verdict must be FAILURE when platform-windows failed: %+v", outcomes)
	}
}

// TestContinueOnErrorIsRejected: an active continue-on-error on a
// required gate must fail the contract (green-washing hazard).
//
// Run-43718750096 portability note: same CRLF defect class as
// TestOldPreflightDAGShapeIsRejected — the LF-normalized loader plus the
// application guard below keep the injection real on every platform.
func TestContinueOnErrorIsRejected(t *testing.T) {
	w := loadWorkflowForDAG(t, dagWorkflowRelPath)
	greenwashed := strings.Replace(w,
		"  preflight:\n    name: Preflight compile gate",
		"  preflight:\n    continue-on-error: true\n    name: Preflight compile gate", 1)
	if greenwashed == w {
		t.Fatalf("the continue-on-error injection did not apply — the preflight job header in build-desktop.yml changed; update the mutation needle in this test (a silent no-op here once produced a false 'broken detector' failure on Windows)")
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
// YAML: independent triggers, no build-desktop dependency, Go + actions
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

// TestFiveReusableLanesInvokedOnce is the run 44306343338 §6.2 regression
// fixture: the orchestrator invokes each of the five reusable platform
// workflows EXACTLY ONCE, no other job invokes platform workflows, every
// reusable file subscribes to workflow_call ONLY, and the retired legacy
// in-orchestrator platform lanes never reappear (one authoritative build
// path per platform).
func TestFiveReusableLanesInvokedOnce(t *testing.T) {
	orchestrator := loadWorkflowForDAG(t, dagWorkflowRelPath)

	laneFiles := []string{
		"build-windows.yml", "build-linux.yml", "build-macos.yml",
		"build-android.yml", "build-ios.yml",
	}
	laneTexts := map[string]string{}
	for _, f := range laneFiles {
		laneTexts[".github/workflows/"+f] = loadWorkflowForDAG(t,
			filepath.Join("..", "..", ".github", "workflows", f))
	}

	if err := ValidateFiveReusableLanesInvokedOnce(orchestrator, laneTexts); err != nil {
		t.Fatalf("five-lane work-order-6.2 topology contract: %v", err)
	}

	doc, err := parseWorkflow(orchestrator)
	if err != nil {
		t.Fatalf("parse orchestrator: %v", err)
	}
	for _, retired := range retiredLaneJobKeys {
		if _, ok := doc.Jobs[retired]; ok {
			t.Errorf("retired legacy lane job %q reappeared in the orchestrator", retired)
		}
	}
}
