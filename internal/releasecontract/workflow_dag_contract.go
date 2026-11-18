// Package releasecontract — workflow_dag_contract.go
//
// v1.11.1 (run 43630164424 repair, work order §9.6): deterministic
// contract tests for the two CI workflows' FAILURE RESILIENCE, parsed
// from the real YAML — never a copied example.
//
// The run-43630164424 root cause was orchestration, not code: preflight
// was a hard `needs:` prerequisite of the four independent validation
// gates, so a single `go vet` failure skipped Core audit, Fyne UI,
// Windows x64 and Linux x64 — a red run with zero gate evidence. The
// validators below make that DAG shape impossible to reintroduce
// silently:
//
//   - ValidateIndependentGatesDAG: preflight must not gate the four
//     validation jobs; each gate owns its checkout/toolchain steps.
//   - ValidateFinalVerdictJob: a strict final verdict exists, needs every
//     required gate plus the collector, runs on !cancelled(), and its
//     verdict step inspects every required gate's result.
//   - ValidateFailureTolerantCollector: the collector job and both its
//     uploads run on !cancelled() (red runs keep evidence); no required
//     gate hides behind continue-on-error.
//   - ValidateCodeQLWorkflowResilience: CodeQL triggers independently of
//     build-desktop, keeps Go + actions scanning with the
//     security-and-quality suite, analyzes after a failed manual build
//     whenever init succeeded, uploads SARIF with upload: always, and
//     carries a failure-tolerant diagnostics collector.
//   - SimulateGateMatrix: a small model of GitHub Actions scheduling
//     semantics (needs + if: !cancelled()) so the fixture tests can
//     PROVE the graph behavior: a red preflight leaves the four gates
//     running while the final verdict stays red.
package releasecontract

import (
	"fmt"
	"sort"
	"strings"
)

// RequiredGateJobs are the YAML job keys whose conclusion the final
// verdict accepts as the workflow's verdict: any non-success conclusion
// is a red run.
var RequiredGateJobs = []string{
	"preflight",
	"audit",
	"ui",
	"platform-windows",
	"platform-linux",
	"platform-macos",
	"platform-android",
	"platform-ios",
}

// IndependentGateJobs are the validation jobs that must NOT depend on
// preflight: each owns its checkout/setup and starts on the same commit
// regardless of the preflight outcome.
var IndependentGateJobs = []string{
	"audit",
	"ui",
	"platform-windows",
	"platform-linux",
	"platform-macos",
	"platform-android",
	"platform-ios",
}

// laneWorkflowUses maps each platform gate to the reusable workflow file
// it must invoke EXACTLY ONCE (run 44306343338 work order §6.2: one
// authoritative build path per platform, five reusable files, no
// duplicate legacy lanes).
var laneWorkflowUses = map[string]string{
	"platform-windows": "./.github/workflows/build-windows.yml",
	"platform-linux":   "./.github/workflows/build-linux.yml",
	"platform-macos":   "./.github/workflows/build-macos.yml",
	"platform-android": "./.github/workflows/build-android.yml",
	"platform-ios":     "./.github/workflows/build-ios.yml",
}

// retiredLaneJobKeys are the legacy in-orchestrator platform lanes the
// consolidation retired; their reappearance recreates the duplicate
// platform-build topology the work order forbids (§3.1/§6.2).
var retiredLaneJobKeys = []string{"build-windows", "build-linux"}

// ValidateIndependentGatesDAG enforces the run-43630164424 repair: the
// four validation gates are parallel, self-contained jobs — preflight is
// NOT their prerequisite. Each gate must exist and own a checkout step;
// the collector keeps needing every gate so it still runs after each one.
func ValidateIndependentGatesDAG(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	var errs []string

	for _, key := range append([]string{"preflight"}, IndependentGateJobs...) {
		job, ok := doc.Jobs[key]
		if !ok {
			errs = append(errs, fmt.Sprintf("required job %q is missing from the workflow", key))
			continue
		}
		if key != "preflight" {
			for _, dep := range job.Needs {
				if dep == "preflight" {
					errs = append(errs, fmt.Sprintf(
						"job %q still needs %q — a red preflight would skip this independent gate again (run 43630164424 root cause); remove the dependency, the final verdict owns strictness",
						key, dep,
					))
				}
			}
		}
		// Self-contained: step-owned gates check out their own tree;
		// reusable-lane gates invoke their ONE authoritative file.
		if want, ok := laneWorkflowUses[key]; ok {
			if job.Uses != want {
				errs = append(errs, fmt.Sprintf(
					"job %q must invoke %q via workflow_call exactly once (the single authoritative build path per platform), got %q",
					key, want, job.Uses,
				))
			}
			continue
		}
		hasCheckout := false
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout") {
				hasCheckout = true
				break
			}
		}
		if !hasCheckout {
			errs = append(errs, fmt.Sprintf(
				"job %q has no actions/checkout step — an independent gate must own its checkout (own prerequisites, same github.sha)",
				key,
			))
		}
	}

	for _, retired := range retiredLaneJobKeys {
		if _, ok := doc.Jobs[retired]; ok {
			errs = append(errs, fmt.Sprintf(
				"retired legacy lane job %q is present again — duplicate platform builds are forbidden (work order §6.2/§3.1: ONE authoritative build path per platform)",
				retired,
			))
		}
	}

	return joinErrors(errs)
}

// ValidateFinalVerdictJob enforces the strict overall verdict: the job
// exists, needs every required gate plus the collector, runs on
// !cancelled() so it still reports after red gates, and its verdict step
// actually inspects each required gate's result — a blind success step
// cannot pass for a verdict.
func ValidateFinalVerdictJob(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	job, ok := doc.Jobs["final-verdict"]
	if !ok {
		return fmt.Errorf("no final-verdict job — the workflow needs an explicit strict overall verdict (run 43630164424 repair)")
	}

	var errs []string

	for _, dep := range append(append([]string{}, RequiredGateJobs...), "consolidate-logs") {
		found := false
		for _, d := range job.Needs {
			if d == dep {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("final-verdict must need %q (every required gate plus the evidence collector)", dep))
		}
	}

	if !strings.Contains(job.If, "!cancelled()") {
		errs = append(errs, "final-verdict must run with if: !cancelled() — it must render its verdict after red or skipped gates, not only on green runs")
	}

	// The verdict step must inspect every required gate's result.
	joined := make([]string, 0, len(job.Steps))
	for _, step := range job.Steps {
		if step.Run != "" {
			joined = append(joined, step.Run)
		}
	}
	script := strings.Join(joined, "\n")

	for _, gate := range RequiredGateJobs {
		if !strings.Contains(script, "needs."+gate+".result") {
			errs = append(errs, fmt.Sprintf(
				"final-verdict never inspects needs.%s.result — a blind verdict step cannot accept the gate's conclusion",
				gate,
			))
		}
	}

	// A required gate hidden behind continue-on-error is a green-washing
	// hazard the work order forbids (§3.3): continuation and result
	// acceptance are different concerns.
	if countActiveContinueOnError(workflowText) > 0 {
		errs = append(errs, "the workflow still contains an active continue-on-error directive — a failed required gate must stay red (never hidden); job continuation and result acceptance are different concerns")
	}

	return joinErrors(errs)
}

// ValidateFailureTolerantCollector enforces the collector contract: the
// consolidate-logs job needs every required gate, runs on !cancelled(),
// and both run-level uploads (plain-text report + machine-readable
// diagnostics index) carry the same failure-tolerant condition.
func ValidateFailureTolerantCollector(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	job, ok := doc.Jobs["consolidate-logs"]
	if !ok {
		return fmt.Errorf("no consolidate-logs job — red runs lose their consolidated evidence")
	}

	var errs []string

	for _, gate := range RequiredGateJobs {
		found := false
		for _, d := range job.Needs {
			if d == gate {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("consolidate-logs must need %q (every required gate) so it runs after each one", gate))
		}
	}

	if !strings.Contains(job.If, "!cancelled()") {
		errs = append(errs, "consolidate-logs must run with if: !cancelled() — red runs keep their consolidated log and diagnostics index")
	}

	// Both collector uploads must be failure-tolerant (a step without its
	// own condition defaults to success() even inside a !cancelled() job).
	for _, want := range []string{ConsolidatedLogArtifactName, RunDiagnosticsArtifactName} {
		step, ok := findUploadStep(doc, "consolidate-logs", want)
		if !ok {
			errs = append(errs, fmt.Sprintf("consolidate-logs has no upload step named %q", want))
			continue
		}
		if !strings.Contains(step.If, "!cancelled()") {
			errs = append(errs, fmt.Sprintf("upload step %q must carry if: !cancelled() — the default implicit success() drops the artifact exactly when it is needed most: on a red run", want))
		}
	}

	return joinErrors(errs)
}

// ValidateCodeQLWorkflowResilience enforces the CodeQL contract
// (§9.3–§9.5) on the parsed workflow:
//   - independent triggers: push (main), pull_request (main), a weekly
//     schedule and workflow_dispatch; NO dependency on build-desktop,
//     preflight, packaging or any application-build job;
//   - the query suite stays security-and-quality and the languages keep
//     Go + actions scanning (no scope reduction);
//   - every init step has an id, and every analyze step runs on
//     !cancelled() && steps.<id>.outcome == 'success' with upload: always —
//     an initialized database is still analyzed (and its SARIF uploaded)
//     when a later manual build step fails;
//   - a collect-codeql-logs job runs after the analyze/export jobs on
//     !cancelled() with least-privilege permissions and failure-tolerant
//     uploads, reusing the same collector script as build-desktop.
func ValidateCodeQLWorkflowResilience(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	var errs []string

	if doc.On.Kind == 0 {
		errs = append(errs, "CodeQL workflow has no trigger block")
	} else {
		triggers := TriggerKeys(doc.On)
		if !triggers["push"] {
			errs = append(errs, "CodeQL workflow must trigger on push (independent of build-desktop)")
		}
		if !triggers["pull_request"] {
			errs = append(errs, "CodeQL workflow must trigger on pull_request (independent of build-desktop)")
		}
		if !triggers["schedule"] {
			errs = append(errs, "CodeQL workflow must keep a scheduled analysis (weekly cron picks up newly released queries)")
		}
		if !triggers["workflow_dispatch"] {
			errs = append(errs, "CodeQL workflow must keep the manual workflow_dispatch trigger")
		}
	}

	// Independence: no CodeQL job may depend on a build-desktop job.
	buildDesktopJobs := map[string]bool{
		"preflight": true, "audit": true, "ui": true,
		"build-windows": true, "build-linux": true,
		"consolidate-logs": true, "final-verdict": true, "release": true,
	}
	for key, job := range doc.Jobs {
		for _, dep := range job.Needs {
			if buildDesktopJobs[dep] {
				errs = append(errs, fmt.Sprintf("CodeQL job %q needs %q — CodeQL must be an independent security gate for the same commit, never a child of the application build", key, dep))
			}
		}
	}

	analyze, ok := doc.Jobs["analyze"]
	if !ok {
		errs = append(errs, "CodeQL workflow has no analyze job")
		return joinErrors(errs)
	}

	// The Go + actions language coverage and the security-and-quality suite
	// must survive (no scope reduction to look greener).
	languages := codeQLMatrixLanguages(workflowText)
	for _, want := range []string{"go", "actions"} {
		if !languages[want] {
			errs = append(errs, fmt.Sprintf("CodeQL matrix no longer scans the %q language — scanning scope must not be reduced", want))
		}
	}
	if !strings.Contains(workflowText, "security-and-quality") {
		errs = append(errs, "CodeQL query suite is no longer security-and-quality — the suite must not be reduced")
	}

	// init must carry an id; analyze must survive a failed manual build.
	var initIDs []string
	for _, step := range analyze.Steps {
		if strings.HasPrefix(step.Uses, "github/codeql-action/init") {
			if step.ID == "" {
				errs = append(errs, "the CodeQL init step has no id — the analyze step cannot key its condition off the init outcome")
			} else {
				initIDs = append(initIDs, step.ID)
			}
		}
	}
	if len(initIDs) == 0 {
		errs = append(errs, "CodeQL analyze job has no github/codeql-action/init step")
	}

	analyzeFound := false
	for _, step := range analyze.Steps {
		if !strings.HasPrefix(step.Uses, "github/codeql-action/analyze") {
			continue
		}
		analyzeFound = true
		if !strings.Contains(step.If, "!cancelled()") {
			errs = append(errs, "the CodeQL analyze step lacks !cancelled() — after a failed manual build (or a cancelled sibling matrix leg) the default implicit success() skips analysis and the SARIF upload entirely")
		}
		keyedToInit := false
		for _, id := range initIDs {
			if strings.Contains(step.If, "steps."+id+".outcome == 'success'") {
				keyedToInit = true
			}
		}
		if !keyedToInit {
			errs = append(errs, "the CodeQL analyze step must run when the init step succeeded (steps.<init-id>.outcome == 'success') even if a later manual build step failed — an initialized database still produces SARIF diagnostics")
		}
		if upload, ok := step.With["upload"]; !ok || fmt.Sprintf("%v", upload) != "always" {
			errs = append(errs, "the CodeQL analyze step must set upload: always explicitly — SARIF is uploaded for successful AND failed runs")
		}
	}
	if !analyzeFound {
		errs = append(errs, "CodeQL analyze job has no github/codeql-action/analyze step")
	}

	// The failure-tolerant CodeQL diagnostics collector.
	collector, ok := doc.Jobs["collect-codeql-logs"]
	if !ok {
		errs = append(errs, "no collect-codeql-logs job — red CodeQL runs (failed build/analyze) lose their job logs and diagnostics")
		return joinErrors(errs)
	}
	if !strings.Contains(collector.If, "!cancelled()") {
		errs = append(errs, "collect-codeql-logs must run with if: !cancelled() — it must publish diagnostics after failed language jobs")
	}
	needsAnalyze, needsExport := false, false
	for _, dep := range collector.Needs {
		if dep == "analyze" {
			needsAnalyze = true
		}
		if dep == "export-alert-report" {
			needsExport = true
		}
	}
	if !needsAnalyze {
		errs = append(errs, "collect-codeql-logs must need the analyze job(s)")
	}
	if !needsExport {
		errs = append(errs, "collect-codeql-logs must need the export-alert-report job so its log is also collected")
	}
	// Least privilege: the collector must NOT carry security-events: write.
	joined := make([]string, 0, len(collector.Steps))
	for _, step := range collector.Steps {
		if step.Run != "" {
			joined = append(joined, step.Run)
		}
	}
	script := strings.Join(joined, "\n")
	if !strings.Contains(script, "consolidate-run-log.sh") {
		errs = append(errs, "collect-codeql-logs must reuse scripts/ci/consolidate-run-log.sh — no second copy of the URL/token collector logic")
	}

	// The CodeQL uploads must be failure-tolerant.
	for _, want := range []string{"SHEYTAN-LA-codeql-all-logs.txt", "codeql-run-diagnostics.json"} {
		step, ok := findUploadStep(doc, "collect-codeql-logs", want)
		if !ok {
			errs = append(errs, fmt.Sprintf("collect-codeql-logs has no upload step named %q", want))
			continue
		}
		if !strings.Contains(step.If, "!cancelled()") {
			errs = append(errs, fmt.Sprintf("upload step %q must carry if: !cancelled() — CodeQL logs/diagnostics/SARIF artifacts must be retained after build/analyze failures", want))
		}
	}

	return joinErrors(errs)
}

// SimulateGateMatrix models GitHub Actions scheduling for the gate DAG:
// given the parsed needs/if of the workflow and a fixture of ROOT job
// outcomes (e.g. preflight=failure), it resolves every job's outcome.
//
// Semantics modeled (the subset the resilience contract depends on):
//   - a job whose needs are all success runs (its outcome comes from the
//     fixture, default success);
//   - a job with a non-success need is SKIPPED — unless its `if` contains
//     !cancelled(), in which case it runs (the collector/verdict class);
//   - resolution iterates to a fixed point, so multi-level needs chains
//     resolve in dependency order.
//
// The simulator is intentionally small and deterministic: it exists so
// the fixture tests can prove "red preflight ⇒ four gates still run ⇒
// final verdict red" against the REAL parsed YAML, and would flag the
// old needs:[preflight] DAG as skipped gates.
func SimulateGateMatrix(workflowText string, fixture map[string]string) (map[string]string, error) {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(doc.Jobs))
	for key, job := range doc.Jobs {
		if outcome, ok := fixture[key]; ok && len(job.Needs) == 0 {
			out[key] = outcome
		}
	}

	for {
		progress := false
		for key, job := range doc.Jobs {
			if _, done := out[key]; done {
				continue
			}
			ready := true
			blocked := false
			for _, dep := range job.Needs {
				res, ok := out[dep]
				if !ok {
					ready = false
					break
				}
				if res != "success" {
					blocked = true
				}
			}
			if !ready {
				continue
			}
			progress = true
			if blocked && !strings.Contains(job.If, "!cancelled()") {
				out[key] = "skipped"
				continue
			}
			if outcome, ok := fixture[key]; ok {
				out[key] = outcome
			} else {
				out[key] = "success"
			}
		}
		if len(out) == len(doc.Jobs) || !progress {
			break
		}
	}

	// Any unresolved job (a needs cycle) is an error, not a silent pass.
	keys := make([]string, 0, len(doc.Jobs))
	for key := range doc.Jobs {
		if _, done := out[key]; !done {
			keys = append(keys, key)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return out, fmt.Errorf("jobs %v never resolved — needs cycle or unknown dependency", keys)
	}

	return out, nil
}

// findUploadStep locates the upload-artifact step with the given
// with.name inside the given job key.
func findUploadStep(doc *workflowDoc, jobKey, artifactName string) (workflowStep, bool) {
	job, ok := doc.Jobs[jobKey]
	if !ok {
		return workflowStep{}, false
	}
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "actions/upload-artifact") {
			continue
		}
		if withStringValue(step.With, "name") == artifactName {
			return step, true
		}
	}
	return workflowStep{}, false
}

// codeQLMatrixLanguages extracts the language entries of the analyze
// matrix from the raw text (the decoded subset does not carry strategy
// matrices; a focused scan keeps the parser simple).
func codeQLMatrixLanguages(workflowText string) map[string]bool {
	langs := map[string]bool{}
	for _, line := range strings.Split(workflowText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- language:") {
			continue
		}
		lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "- language:"))
		langs[lang] = true
	}
	return langs
}

// countActiveContinueOnError counts ACTIVE continue-on-error directives
// (a YAML key at any indentation), ignoring comment mentions.
func countActiveContinueOnError(workflowText string) int {
	count := 0
	for _, line := range strings.Split(workflowText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "continue-on-error:") {
			count++
		}
	}
	return count
}

// joinErrors renders a validator's accumulated failures.
func joinErrors(errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("workflow DAG contract violated (%d):\n  %s", len(errs), strings.Join(errs, "\n  "))
}

// ValidateFiveReusableLanesInvokedOnce enforces the §6.2 topology end to
// end: the orchestrator invokes each of the five reusable platform
// workflows EXACTLY ONCE, no job outside the five platform-* lanes
// invokes a platform workflow, and every reusable file subscribes to
// workflow_call ONLY (the orchestrator owns push/PR triggers; a platform
// file with its own push trigger would silently rebuild a platform
// outside the orchestrated DAG).
func ValidateFiveReusableLanesInvokedOnce(orchestratorText string, laneTexts map[string]string) error {
	doc, err := parseWorkflow(orchestratorText)
	if err != nil {
		return err
	}

	var errs []string

	counts := map[string]int{}
	for jobKey, job := range doc.Jobs {
		if job.Uses == "" {
			continue
		}
		counts[job.Uses]++
		if _, isLane := laneWorkflowUses[jobKey]; !isLane {
			errs = append(errs, fmt.Sprintf(
				"job %q invokes %q — the orchestrator must not invoke platform workflows outside the five contracted platform-* lanes",
				jobKey, job.Uses,
			))
		}
	}

	for _, want := range laneWorkflowUses {
		if n := counts[want]; n != 1 {
			errs = append(errs, fmt.Sprintf(
				"%q must be invoked EXACTLY ONCE by the orchestrator (work order §6.2), found %d invocations",
				want, n,
			))
		}
	}

	for laneFile, text := range laneTexts {
		laneDoc, err := parseWorkflow(text)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", laneFile, err))
			continue
		}
		triggers := TriggerKeys(laneDoc.On)
		if len(triggers) != 1 || !triggers["workflow_call"] {
			errs = append(errs, fmt.Sprintf(
				"%s must subscribe to workflow_call ONLY (the orchestrator owns push/PR triggers), got %v",
				laneFile, triggers,
			))
		}
	}

	return joinErrors(errs)
}
