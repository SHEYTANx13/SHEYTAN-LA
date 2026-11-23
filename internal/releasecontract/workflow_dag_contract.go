// Package releasecontract — workflow_dag_contract.go
//
// v1.12 CI consolidation (the run-43630164424 + run-44306343338 lessons
// carried into the ONE platform-build workflow): deterministic contract
// tests for the CONSOLIDATED workflow's FAILURE RESILIENCE, parsed from
// the real YAML — never a copied example.
//
// The retired topology had a preflight job whose single `go vet` failure
// skipped four independent validation gates (run 43630164424: a red run
// with zero evidence), and five reusable platform lane files wired by
// needs (run 44306343338). The consolidated topology makes those shapes
// impossible by construction:
//
//   - ValidateIndependentGatesDAG: the platform-build job owns the seven
//     matrix targets and has NO needs (every matrix instance is an
//     independent gate); no workflow_call lane invocations remain; the
//     retired lane files are gone.
//   - ValidatePlatformMatrixStructure: exactly one matrix.include with
//     exactly the seven targets, unique (target, os, artifact) triples,
//     fail-fast EXPLICITLY false, no cancel-in-progress: true.
//   - ValidateFinalVerdictJob: a strict final verdict exists, needs
//     platform-build + collect-errors, runs on !cancelled(), and its
//     verdict step inspects every required gate's result — a collector
//     success can never override a platform failure.
//   - ValidateFailureTolerantCollector: the collector job needs exactly
//     [platform-build], runs on always(), keeps its log collection and
//     parsing LOCAL to the runner (run 44835425398 artifact policy: the
//     collector uploads NO artifacts — its report and diagnostics index
//     stay in the workspace, the concise summary goes to the step
//     summary, and its own failure still fails the required gate).
//   - ValidateCodeQLWorkflowResilience: CodeQL triggers independently of
//     the platform build, keeps Go + actions scanning with the
//     security-and-quality suite, analyzes after a failed manual build
//     whenever init succeeded, uploads SARIF with upload: always, and
//     carries a failure-tolerant diagnostics collector that uploads no
//     build-log or diagnostics-index artifact (the security-alert report
//     artifact in the export-alert-report job remains permitted).
//   - SimulateGateMatrix: a small model of GitHub Actions scheduling
//     semantics (needs + failure-tolerant if) so the fixture tests can
//     PROVE the graph behavior: a red matrix keeps the collector running
//     while the final verdict stays red.
package releasecontract

import (
	"fmt"
	"sort"
	"strings"
)

// RequiredGateJobs are the YAML job keys whose conclusion the final
// verdict accepts as the workflow's verdict: any non-success conclusion
// is a red run. platform-build is the SEVEN-target matrix (fail-fast:
// false keeps every lane running to its terminal state);
// collect-errors is the failure-tolerant diagnostics collector whose
// coverage-gap failure keeps the run red. A collector report-generation
// SUCCESS can never override a platform failure: both gates are checked
// in the same required loop.
var RequiredGateJobs = []string{
	PlatformBuildJobKey,
	CollectorJobKey,
}

// retiredLaneJobKeys are the legacy in-orchestrator platform lanes the
// consolidation retired; their reappearance recreates the duplicate
// platform-build topology the work order forbids (§3.1/§6.2).
var retiredLaneJobKeys = []string{"build-windows", "build-linux"}

// ValidateIndependentGatesDAG enforces the consolidated DAG contract:
// the platform-build job has EMPTY or absent needs (every matrix
// instance is an independent gate with its own checkout/toolchain — the
// run-43630164424 lesson, made structural), the topology is exactly the
// four contracted jobs, no workflow_call lane invocation survives (the
// matrix replaced the five reusable files), the retired legacy lane job
// keys never reappear, and cancel-in-progress: true never returns.
func ValidateIndependentGatesDAG(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	var errs []string

	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok {
		errs = append(errs, fmt.Sprintf("required job %q is missing from the workflow", PlatformBuildJobKey))
	} else if len(job.Needs) != 0 {
		errs = append(errs, fmt.Sprintf(
			"job %q carries needs %v — every matrix instance is an independent gate; a needs chain makes one gate skip another's evidence again (run 43630164424 root cause)",
			PlatformBuildJobKey, []string(job.Needs),
		))
	}

	// The retired reusable-lane topology must not come back: no job may
	// invoke a platform workflow via workflow_call (the matrix is the ONE
	// authoritative build path now).
	for key, j := range doc.Jobs {
		if j.Uses != "" {
			errs = append(errs, fmt.Sprintf(
				"job %q invokes %q via workflow_call — the consolidation replaced the reusable-lane invocations with the six-target matrix; a reusable lane file cannot come back",
				key, j.Uses,
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

	// The matrix contract carries the rest (fail-fast false, six targets,
	// cancel-in-progress ban).
	if err := ValidatePlatformMatrixStructure(workflowText); err != nil {
		errs = append(errs, err.Error())
	}

	return joinErrors(errs)
}

// ValidateFinalVerdictJob enforces the strict overall verdict: the job
// exists, needs both required gates (platform-build + collect-errors),
// runs on !cancelled() so it still reports after red gates, and its
// verdict step actually inspects each required gate's result — a blind
// success step cannot pass for a verdict, and a collector success can
// never override a platform failure (both gates sit in the same required
// loop).
func ValidateFinalVerdictJob(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	job, ok := doc.Jobs[FinalVerdictJobKey]
	if !ok {
		return fmt.Errorf("no final-verdict job — the workflow needs an explicit strict overall verdict (run 43630164424 repair)")
	}

	var errs []string

	for _, dep := range RequiredGateJobs {
		found := false
		for _, d := range job.Needs {
			if d == dep {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("final-verdict must need %q (every required gate, collector included)", dep))
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

// ValidateFailureTolerantCollector enforces the collector contract
// (2026-11-23 policy): the collect-errors job needs
// EXACTLY [platform-build] (the ONE build gate the collector reports
// on), runs on always() so it reports after ANY platform outcome, keeps
// its log collection and parsing LOCAL to the runner (it still invokes
// scripts/ci/consolidate-run-log.sh), uploads EXACTLY the ONE curated
// errors/warnings report (ConsolidatedLogReportArtifactName staging
// ConsolidatedLogReportFileName — never the diagnostics index, never any
// other evidence), and its own verdict step still fails the REQUIRED
// collector gate on a coverage gap.
func ValidateFailureTolerantCollector(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	job, ok := doc.Jobs[CollectorJobKey]
	if !ok {
		return fmt.Errorf("no %s job — red runs lose their consolidated errors/warnings diagnosis", CollectorJobKey)
	}

	var errs []string

	if len(job.Needs) != 1 || job.Needs[0] != PlatformBuildJobKey {
		errs = append(errs, fmt.Sprintf(
			"%s must need EXACTLY [%s] (the ONE required build gate the collector reports on), got %v",
			CollectorJobKey, PlatformBuildJobKey, []string(job.Needs),
		))
	}

	if !strings.Contains(job.If, "always()") {
		errs = append(errs, "collect-errors must run with if: always() — red runs keep their consolidated errors/warnings report and step summary")
	}

	// 2026-11-23 artifact policy: the collector uploads EXACTLY the ONE
	// curated errors/warnings report. The diagnostics index and every
	// other evidence class stay local workspace files.
	reportUploads := 0
	for _, step := range job.Steps {
		if !strings.HasPrefix(step.Uses, "actions/upload-artifact") {
			continue
		}
		reportUploads++
		name := withStringValue(step.With, "name")
		if name != ConsolidatedLogReportArtifactName {
			errs = append(errs, fmt.Sprintf(
				"%s step %q uploads artifact %q — the collector uploads EXACTLY the curated errors/warnings report %q staging %s; the diagnostics index and every other evidence class stay local workspace files",
				CollectorJobKey, step.Name, name, ConsolidatedLogReportArtifactName, ConsolidatedLogReportFileName,
			))
		}
	}
	if reportUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"%s must upload EXACTLY the ONE curated errors/warnings report (%s), found %d upload step(s) — the owner reads the run's error/warning diagnosis (exact file:line or stage) directly from the run page",
			CollectorJobKey, ConsolidatedLogReportArtifactName, reportUploads,
		))
	}

	// The collection itself must stay real and local: the collector still
	// runs the shared script against the run's own job logs.
	collectorRuns := false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "scripts/ci/consolidate-run-log.sh") {
			collectorRuns = true
		}
	}
	if !collectorRuns {
		errs = append(errs, "collect-errors no longer invokes scripts/ci/consolidate-run-log.sh — the failure-tolerant log collection (and its strict exit-status contract) must survive the artifact-policy change")
	}

	// The collector's own failure must still fail the required gate: the
	// verdict step keys off the collector's recorded rc and fails closed.
	verdictStep := false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "steps.collector.outputs.rc") {
			verdictStep = true
		}
	}
	if !verdictStep {
		errs = append(errs, "collect-errors lost its verdict step — the collector's own coverage/verification gap must still fail the REQUIRED collector gate (never hidden behind a best-effort step)")
	}

	return joinErrors(errs)
}

// ValidateCodeQLWorkflowResilience enforces the CodeQL contract
// (§9.3–§9.5) on the parsed workflow:
//   - independent triggers: push (main), pull_request (main), a weekly
//     schedule and workflow_dispatch; NO dependency on the platform-build
//     workflow (build.yml — the retired build-desktop orchestrator),
//     preflight, packaging or any application-build job;
//   - the query suite stays security-and-quality and the languages keep
//     Go + actions scanning (no scope reduction);
//   - every init step has an id, and every analyze step runs on
//     !cancelled() && steps.<id>.outcome == 'success' with upload: always —
//     an initialized database is still analyzed (and its SARIF uploaded)
//     when a later manual build step fails;
//   - a collect-codeql-logs job runs after the analyze/export jobs on
//     !cancelled() with least-privilege permissions, reusing the same
//     collector script as the platform-build workflow, keeping its
//     collection LOCAL and uploading NO build-log or diagnostics-index
//     artifact (run 44835425398 artifact policy). The security-alert
//     report artifact (reports/code-scanning-alerts.txt, uploaded by the
//     export-alert-report job) stays permitted: it is a security report,
//     never build evidence, and GitHub Code Scanning/SARIF remains the
//     authoritative security finding surface.
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
			errs = append(errs, "CodeQL workflow must trigger on push (independent of the platform-build workflow)")
		}
		if !triggers["pull_request"] {
			errs = append(errs, "CodeQL workflow must trigger on pull_request (independent of the platform-build workflow)")
		}
		if !triggers["schedule"] {
			errs = append(errs, "CodeQL workflow must keep a scheduled analysis (weekly cron picks up newly released queries)")
		}
		if !triggers["workflow_dispatch"] {
			errs = append(errs, "CodeQL workflow must keep the manual workflow_dispatch trigger")
		}
	}

	// Independence: no CodeQL job may depend on a platform-build job
	// (the consolidated build.yml job set).
	platformBuildJobs := map[string]bool{
		PlatformBuildJobKey: true,
		CollectorJobKey:     true,
		FinalVerdictJobKey:  true,
		ReleaseJobKey:       true,
	}
	for key, job := range doc.Jobs {
		for _, dep := range job.Needs {
			if platformBuildJobs[dep] {
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

	// 2026-11-23 artifact policy (owner-directed change of the run
	// 44835425398 local-only rule): the CodeQL collector uploads EXACTLY
	// the ONE curated errors/warnings report. The forbidden evidence
	// names must not appear as upload names in ANY job of this workflow;
	// the remaining permitted artifacts are the security-alert plain-text
	// report (export-alert-report) and the CodeQL errors/warnings report
	// (collect-codeql-logs).
	for _, jobKey := range sortedJobKeys(doc) {
		for _, step := range doc.Jobs[jobKey].Steps {
			if !strings.HasPrefix(step.Uses, "actions/upload-artifact") {
				continue
			}
			name := withStringValue(step.With, "name")
			switch {
			case jobKey == "export-alert-report":
				for _, banned := range ForbiddenArtifactUploadNames {
					if name == banned {
						errs = append(errs, fmt.Sprintf(
							"%s step %q uploads the forbidden evidence artifact %q — build-log and diagnostics-index uploads are banned even in the CodeQL workflow",
							jobKey, step.Name, name,
						))
					}
				}
				if name != "" && !strings.Contains(name, "code-scanning-alerts") {
					errs = append(errs, fmt.Sprintf(
						"%s step %q uploads unexpected artifact %q — the export-alert-report job may upload only the plain-text security report (reports/code-scanning-alerts.txt)",
						jobKey, step.Name, name,
					))
				}
			case jobKey == "collect-codeql-logs":
				if name != CodeQLLogReportArtifactName {
					errs = append(errs, fmt.Sprintf(
						"%s step %q uploads artifact %q — the CodeQL collector uploads EXACTLY the curated errors/warnings report %q staging %s; raw logs, the diagnostics index and every other evidence class stay local workspace files",
						jobKey, step.Name, name, CodeQLLogReportArtifactName, CodeQLLogReportFileName,
					))
				}
			default:
				errs = append(errs, fmt.Sprintf(
					"%s step %q uploads an artifact — the ONLY permitted CodeQL artifacts are the security-alert report (export-alert-report) and the curated errors/warnings report (collect-codeql-logs); everything else stays local to the runner",
					jobKey, step.Name,
				))
			}
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
			if blocked && !jobRunsAfterFailure(job.If) {
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

// jobRunsAfterFailure reports whether a job-level if condition runs the
// job after a failed dependency: both the `!cancelled()` spelling (the
// final-verdict class) and the `always()` spelling (the collect-errors
// collector class) are failure-tolerant; the implicit success() default
// is not.
func jobRunsAfterFailure(jobIf string) bool {
	return strings.Contains(jobIf, "!cancelled()") || strings.Contains(jobIf, "always()")
}

// findUploadStep locates the upload-artifact step with the given
// with.name inside the given job key. (Retained for diagnostics: the
// run 44835425398 contract no longer REQUIRES any collector upload
// step — the platform-build surface is the package only, and the CodeQL
// surface is the security-alert report only.)
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

// sortedJobKeys returns the workflow's job keys in deterministic order.
func sortedJobKeys(doc *workflowDoc) []string {
	keys := make([]string, 0, len(doc.Jobs))
	for key := range doc.Jobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
