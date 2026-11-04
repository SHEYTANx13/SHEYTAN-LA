// Package releasecontract — artifact_naming_contract_test.go
//
// v1.11.1 SINGLE-ARTIFACT RUN-LOG CONTRACT (work order for run
// #41548925352 §6.2, implemented by the v1.11.1 collector rework):
//
//   - NO artifact name (upload or download) carries a run-attempt suffix
//     in ANY spelling (`-attempt${{ ... }}`, `-attempt-1`, `-attempt1`);
//   - the consolidated run log is the ONE log artifact: the exact stable
//     plain-text name SHEYTAN-LA-all-logs.txt (upload-artifact
//     archive:false — the artifact downloads as the file itself, and the
//     artifact display name equals the file name);
//   - the two packages keep the exact stable names on upload (build
//     jobs) and download (release job);
//   - the retired five-part log-part era (preflight/audit/ui/windows/
//     linux-logs-part, ci-parts/ namespaced downloads, per-part
//     merge-multiple) must NOT reappear — the collector now pulls every
//     completed job's log straight from the paginated jobs API, so there
//     is nothing to flatten in the first place;
//   - the attempt number survives ONLY inside the consolidated report as
//     structured provenance metadata (the workflow's CONSOLIDATE_ATTEMPT
//     env + the fixtures' --attempt rerun case), never in a name.
package releasecontract

import (
	"regexp"
	"strings"
	"testing"
)

// retiredLogPartNames are the v1.10-era per-job log-part artifact names.
// The v1.11.1 collector replaced them with job-specific API log
// retrieval; these names must never reappear in the workflow.
var retiredLogPartNames = []string{
	"preflight-logs-part",
	"audit-logs-part",
	"ui-logs-part",
	"windows-logs-part",
	"linux-logs-part",
}

// stablePackageNames are the contracted package artifact names.
var stablePackageNames = []string{
	"windows-x64-package",
	"linux-x64-package",
}

// artifactStepNames extracts every `name: <value>` key inside an
// upload-artifact / download-artifact step's with: block. A line scanner
// (Go regexp has no lookaheads) tracks step context: after
// `uses: actions/<action>@` the following with: block belongs to that
// step until the next step header. Step/JOB display names ("name:" of a
// step header) never enter the extraction.
func artifactStepNames(t *testing.T, workflow, action string) []string {
	t.Helper()
	var names []string
	inStep := false
	inWith := false
	jobBoundary := regexp.MustCompile(`^  [A-Za-z0-9_-]+:$`)
	for _, line := range strings.Split(workflow, "\n") {
		if jobBoundary.MatchString(line) { // a new JOB starts — never leak a step context across jobs
			inStep = false
			inWith = false
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "uses: actions/"+action+"@") {
			inStep = true
			inWith = false
			continue
		}
		if !inStep {
			continue
		}
		// A new step header (or a new uses:) ends the current step.
		if strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "uses:") {
			inStep = false
			inWith = false
			continue
		}
		if strings.HasPrefix(trimmed, "with:") {
			inWith = true
			continue
		}
		if inWith && strings.HasPrefix(trimmed, "name:") {
			names = append(names, strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")))
		}
	}
	return names
}

// uploadStepBodies returns the with:-block key lines of every
// upload-artifact step (same scanner discipline as artifactStepNames).
func uploadStepBodies(workflow string) []string {
	var bodies []string
	inStep := false
	inWith := false
	var cur []string
	jobBoundary := regexp.MustCompile(`^  [A-Za-z0-9_-]+:$`)
	flush := func() {
		if len(cur) > 0 {
			bodies = append(bodies, strings.Join(cur, "\n"))
		}
		cur = nil
	}
	for _, line := range strings.Split(workflow, "\n") {
		if jobBoundary.MatchString(line) { // a new JOB starts — never leak a step context across jobs
			flush()
			inStep = false
			inWith = false
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "uses: actions/upload-artifact@") {
			inStep = true
			inWith = false
			cur = nil
			continue
		}
		if !inStep {
			continue
		}
		if strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "uses:") {
			flush()
			inStep = false
			inWith = false
			continue
		}
		if strings.HasPrefix(trimmed, "with:") {
			inWith = true
			continue
		}
		if inWith && trimmed != "" {
			cur = append(cur, trimmed)
		}
	}
	flush()
	return bodies
}

// TestWorkflowArtifactNamesCarryNoAttemptSuffix: no upload OR download
// artifact name contains an attempt-suffix in any spelling.
func TestWorkflowArtifactNamesCarryNoAttemptSuffix(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, action := range []string{"upload-artifact", "download-artifact"} {
		names := artifactStepNames(t, workflow, action)
		if len(names) == 0 {
			t.Fatalf("no %s steps with artifact names found in %s — the extraction is broken, not the workflow", action, workflowRelPath)
		}
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), "attempt") {
				t.Errorf("%s artifact name %q carries a run-attempt suffix — v1.11.1 stable-name contract (run id/commit/attempt live INSIDE the consolidated report)", action, name)
			}
		}
	}

	// Belt and braces: the whole workflow text contains no attempt-shaped
	// artifact NAME anywhere (the ${{ }} spelling and the literal shapes).
	for _, banned := range []string{
		"-attempt${{", "-attempt-${{", "-attempt1", "-attempt-1",
	} {
		if strings.Contains(workflow, banned) {
			t.Errorf("workflow text still contains the attempt-suffix shape %q (artifact/log names must be stable)", banned)
		}
	}
}

// TestWorkflowStableArtifactNamesExact: the ONE consolidated log
// artifact and the two packages use the exact contracted names, and the
// retired run-id-suffixed log name never reappears.
func TestWorkflowStableArtifactNamesExact(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	uploads := artifactStepNames(t, workflow, "upload-artifact")
	downloads := artifactStepNames(t, workflow, "download-artifact")

	if !containsExact(uploads, "SHEYTAN-LA-all-logs.txt") {
		t.Errorf("the consolidated log upload must use the exact stable name SHEYTAN-LA-all-logs.txt (single plain-text artifact)")
	}
	logUploads := 0
	for _, n := range uploads {
		if strings.Contains(n, "logs") {
			logUploads++
		}
	}
	if logUploads != 1 {
		t.Errorf("exactly ONE log artifact upload is contracted (the consolidated report), found %d: %v", logUploads, uploads)
	}
	for _, want := range stablePackageNames {
		if !containsExact(uploads, want) {
			t.Errorf("stable upload artifact name %q missing from the workflow's upload steps", want)
		}
		if !containsExact(downloads, want) {
			t.Errorf("stable download artifact name %q missing from the release job's download steps", want)
		}
	}
	for _, banned := range append([]string{
		"SHEYTAN-LA-all-logs-run-${{ github.run_id }}",
	}, retiredLogPartNames...) {
		if strings.Contains(workflow, banned) {
			t.Errorf("retired artifact name/shape %q must not reappear (v1.11.1 single-artifact contract)", banned)
		}
	}
}

// TestWorkflowUploadsOverwriteOnRerun: every upload-artifact step carries
// overwrite: true — the deterministic rerun strategy for stable names
// (verified against the pinned v7 action.yml, whose `overwrite` input
// defaults to false: without it, a rerun FAILS on the existing name).
func TestWorkflowUploadsOverwriteOnRerun(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	bodies := uploadStepBodies(workflow)
	if len(bodies) == 0 {
		t.Fatalf("no upload-artifact steps found in %s", workflowRelPath)
	}
	for i, body := range bodies {
		if !strings.Contains(body, "overwrite: true") {
			t.Errorf("upload-artifact step #%d lacks `overwrite: true` — a rerun would collide with the existing stable-name artifact (body: %q)", i+1, body)
		}
	}
}

// TestWorkflowHasNoLogPartArtifacts: the five-part era is retired — no
// log-part uploads/downloads and no ci-parts/ namespacing remain. The
// collector fetches every completed job's log directly from the jobs
// API, so there is nothing to flatten in the first place.
func TestWorkflowHasNoLogPartArtifacts(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, action := range []string{"upload-artifact", "download-artifact"} {
		for _, name := range artifactStepNames(t, workflow, action) {
			if strings.HasSuffix(name, "-logs-part") {
				t.Errorf("retired log-part artifact %q found in a %s step (single-artifact contract)", name, action)
			}
		}
	}
	if strings.Contains(workflow, "ci-parts/") {
		t.Error("retired ci-parts/ namespaced download paths found (single-artifact contract)")
	}
}

// TestAttemptSurvivesOnlyAsReportMetadata: the collector still receives
// the attempt number for provenance INSIDE the report (structured
// metadata via CONSOLIDATE_ATTEMPT + the fixtures' --attempt rerun
// case), and the fixtures pin the current coverage classes.
func TestAttemptSurvivesOnlyAsReportMetadata(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)
	if !strings.Contains(workflow, "CONSOLIDATE_ATTEMPT: ${{ github.run_attempt }}") {
		t.Error("the collector must keep receiving the attempt (CONSOLIDATE_ATTEMPT) for report-internal provenance metadata")
	}

	fixtures := readTreeFile(t, "scripts/ci/consolidate-run-log-fixtures.sh")
	for _, anchor := range []string{
		"rerun: attempt 2 recorded as provenance metadata",
		"green run: 5/5 captured + skipped release",
	} {
		if !strings.Contains(fixtures, anchor) {
			t.Errorf("collector fixtures must pin the coverage class %q (case missing)", anchor)
		}
	}
	if strings.Contains(fixtures, "logs-part-attempt1") {
		t.Error("the fixtures must not encode the retired *-logs-part-attempt1 shape (v1.11.1 stable names)")
	}
}

func containsExact(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
