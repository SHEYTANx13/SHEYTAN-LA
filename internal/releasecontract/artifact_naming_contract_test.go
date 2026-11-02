// Package releasecontract — artifact_naming_contract_test.go
//
// v1.11.1 STABLE ARTIFACT NAME CONTRACT (operator request: remove the
// Actions artifact/log "attempt1" naming noise).
//
// The contract under test, over .github/workflows/build-desktop.yml:
//
//   - NO artifact name (upload or download) carries a run-attempt suffix
//     in ANY spelling (`-attempt${{ ... }}`, `-attempt-1`, `-attempt1`);
//   - the five log parts and the two packages use the exact STABLE names;
//   - every upload-artifact step carries overwrite: true (verified
//     against the pinned actions/upload-artifact@v7 action.yml: the
//     input exists and defaults to false) so a workflow RERUN REPLACES
//     the same stable name instead of colliding;
//   - every log-part download keeps its per-job namespaced path
//     (ci-parts/<stable-name>/) — the anti-flatten rule from run #118;
//   - the attempt number survives ONLY inside the consolidated report as
//     structured provenance metadata (--attempt flag), never in a name.
//
// The collector-side behavior (five stable parts captured, no collisions,
// rerun/overwrite shape) is pinned by scripts/ci/consolidate-run-log-fixtures.sh
// cases 10–13, which CI runs in the audit job.
package releasecontract

import (
	"strings"
	"testing"
)

// stableLogPartNames are the five contracted log-part artifact names.
var stableLogPartNames = []string{
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
	for _, line := range strings.Split(workflow, "\n") {
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
	flush := func() {
		if len(cur) > 0 {
			bodies = append(bodies, strings.Join(cur, "\n"))
		}
		cur = nil
	}
	for _, line := range strings.Split(workflow, "\n") {
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

// TestWorkflowStableArtifactNamesExact: the five log parts, the two
// packages and the consolidated report use the exact contracted names.
func TestWorkflowStableArtifactNamesExact(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	uploads := artifactStepNames(t, workflow, "upload-artifact")
	downloads := artifactStepNames(t, workflow, "download-artifact")

	for _, want := range append(append([]string{}, stableLogPartNames...), stablePackageNames...) {
		if !containsExact(uploads, want) {
			t.Errorf("stable upload artifact name %q missing from the workflow's upload steps", want)
		}
	}
	for _, want := range stableLogPartNames {
		if !containsExact(downloads, want) {
			t.Errorf("stable download artifact name %q missing from the workflow's download steps", want)
		}
	}
	for _, want := range stablePackageNames {
		if !containsExact(downloads, want) {
			t.Errorf("stable download artifact name %q missing from the release job's download steps", want)
		}
	}
	if !containsExact(uploads, "SHEYTAN-LA-all-logs-run-${{ github.run_id }}") {
		t.Errorf("the consolidated log upload must use the stable name SHEYTAN-LA-all-logs-run-<run_id> (no attempt suffix)")
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

// TestWorkflowLogPartDownloadsStayNamespaced: every log-part download
// extracts into its own ci-parts/<stable-name>/ directory — the run #118
// anti-flatten rule (same-named logs from different jobs never collide).
func TestWorkflowLogPartDownloadsStayNamespaced(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, name := range stableLogPartNames {
		wantPath := "ci-parts/" + name
		if !strings.Contains(workflow, wantPath) {
			t.Errorf("download for %q must extract into the namespaced path %s (anti-flatten rule from run #118)", name, wantPath)
		}
	}
	if strings.Contains(workflow, "merge-multiple: true") {
		t.Error("merge-multiple must never be enabled for log-part downloads — namespaced extraction is the anti-collision contract")
	}
}

// TestAttemptSurvivesOnlyAsReportMetadata: the collector still receives
// the attempt number for provenance INSIDE the report (structured
// metadata), and the fixtures script carries the stable-name + rerun
// coverage (cases 12/13).
func TestAttemptSurvivesOnlyAsReportMetadata(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)
	if !strings.Contains(workflow, `--attempt "${{ github.run_attempt }}"`) {
		t.Error("the collector must keep receiving --attempt for report-internal provenance metadata")
	}

	fixtures := readTreeFile(t, "scripts/ci/consolidate-run-log-fixtures.sh")
	for _, anchor := range []string{
		"v1.11.1 green run: 5/5 stable-name parts captured",
		"v1.11.1 rerun/overwrite",
	} {
		if !strings.Contains(fixtures, anchor) {
			t.Errorf("collector fixtures must pin the stable-name coverage class %q (case missing)", anchor)
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
