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
//
// v1.11.1 (run 41901967798): the extraction below uses the SHARED YAML-
// parsed, per-job-scoped contract helpers (workflow_contract.go) instead
// of the retired hand-rolled line scanner. The scanner was CRLF-
// sensitive: windows-latest materializes the unpinned .yml as CRLF, its
// job-boundary regexp never matched, step context leaked across jobs and
// job display names ("Linux x64", "Consolidate logs") were captured as
// artifact names — "Consolidate logs" contains "logs", so the one-log-
// upload count found 2 and the Windows headless gate went red on a
// CORRECT workflow. A real YAML parser is line-ending-proof by
// construction; fixture coverage pins the CRLF case explicitly.
package releasecontract

import (
	"fmt"
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
	WindowsPackageArtifactName,
	LinuxPackageArtifactName,
}

// mustArtifactSteps extracts the artifact steps of one action through
// the SHARED parser (the same functions the stress gate runs). A parse
// failure or an empty extraction is a broken CONTRACT, never a passing
// vacuity.
func mustArtifactSteps(t *testing.T, workflow, actionID string) []ArtifactStep {
	t.Helper()

	steps, err := ArtifactSteps(workflow, actionID)
	if err != nil {
		t.Fatalf("workflow parse failed: %v", err)
	}

	if len(steps) == 0 {
		t.Fatalf("no %s steps found in %s — the extraction is broken, not the workflow", actionID, workflowRelPath)
	}

	return steps
}

// TestWorkflowUploadArtifactPinsMatchAllowlist: every upload-artifact
// use in the REAL workflow is pinned to the reviewed immutable v7.0.2
// commit SHA (the shared stress-gate validator — one authority).
func TestWorkflowUploadArtifactPinsMatchAllowlist(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	if err := ValidateUploadArtifactPins(workflow); err != nil {
		t.Fatalf("upload-artifact pin policy violated: %v", err)
	}
}

// TestWorkflowStableArtifactNamesExact: the ONE consolidated log
// artifact and the two packages use the exact contracted names, the
// upload surface is EXACTLY those three steps (scoped per job by the
// shared parser), and the retired run-id-suffixed log name never
// reappears.
func TestWorkflowStableArtifactNamesExact(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	// The shared validator carries the exact contract semantics — the
	// stress gate runs the SAME function, so the two gates cannot drift.
	if err := ValidateUploadArtifactContract(workflow); err != nil {
		t.Fatalf("upload-artifact contract violated: %v", err)
	}

	downloads := mustArtifactSteps(t, workflow, "actions/download-artifact")

	for _, want := range stablePackageNames {
		found := false

		for _, d := range downloads {
			if d.Name == want {
				found = true
				break
			}
		}

		if !found {
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

// TestWorkflowArtifactNamesCarryNoAttemptSuffix: no upload OR download
// artifact name contains an attempt-suffix in any spelling.
func TestWorkflowArtifactNamesCarryNoAttemptSuffix(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, action := range []string{"actions/upload-artifact", "actions/download-artifact"} {
		for _, step := range mustArtifactSteps(t, workflow, action) {
			if strings.Contains(strings.ToLower(step.Name), "attempt") {
				t.Errorf("%s artifact name %q carries a run-attempt suffix — v1.11.1 stable-name contract (run id/commit/attempt live INSIDE the consolidated report)", action, step.Name)
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

// TestWorkflowUploadsOverwriteOnRerun: every upload-artifact step carries
// overwrite: true — the deterministic rerun strategy for stable names
// (verified against the pinned v7 action.yml, whose `overwrite` input
// defaults to false: without it, a rerun FAILS on the existing name).
func TestWorkflowUploadsOverwriteOnRerun(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, step := range mustArtifactSteps(t, workflow, "actions/upload-artifact") {
		if v, ok := step.With["overwrite"]; !ok || v != true {
			t.Errorf("job %q step %q (artifact %q) lacks `overwrite: true` — a rerun would collide with the existing stable-name artifact",
				step.Job, step.Step, step.Name)
		}
	}
}

// TestWorkflowHasNoLogPartArtifacts: the five-part era is retired — no
// log-part uploads/downloads and no ci-parts/ namespacing remain. The
// collector fetches every completed job's log directly from the jobs
// API, so there is nothing to flatten in the first place.
func TestWorkflowHasNoLogPartArtifacts(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, action := range []string{"actions/upload-artifact", "actions/download-artifact"} {
		for _, step := range mustArtifactSteps(t, workflow, action) {
			if strings.HasSuffix(step.Name, "-logs-part") {
				t.Errorf("retired log-part artifact %q found in a %s step (single-artifact contract)", step.Name, action)
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

// ---------------------------------------------------------------------------
// Fixture coverage (run 41901967798 §2.2/§2.3): the pin policy and the
// scoped upload contract are pinned against synthetic workflows so the
// shared validators are proven on BOTH sides of every rule — including
// the CRLF materialization that broke the retired scanner on the
// Windows runner.
// ---------------------------------------------------------------------------

// fixtureUploadWorkflow is a structurally faithful three-upload
// workflow: both package uploads plus the single consolidated log
// upload. The %s slots receive the upload-artifact refs.
const fixtureUploadWorkflow = `jobs:
  build-windows:
    name: Windows x64
    steps:
      - name: Upload Windows package
        uses: actions/upload-artifact@%s
        with:
          name: windows-x64-package
          path: |
            dist/SHEYTAN-LA-v1.11.1-windows-x64.zip
            dist/SHEYTAN-LA-v1.11.1-windows-x64-installer.exe
          retention-days: 14
          if-no-files-found: error
          overwrite: true
  build-linux:
    name: Linux x64
    steps:
      - name: Upload Linux package
        uses: actions/upload-artifact@%s
        with:
          name: linux-x64-package
          path: dist/SHEYTAN-LA-v1.11.1-linux-x64.zip
          retention-days: 14
          if-no-files-found: error
          overwrite: true
  consolidate-logs:
    name: Consolidate logs
    steps:
      - name: Upload the consolidated run log (single plain-text artifact)
        if: always()
        uses: actions/upload-artifact@%s
        with:
          name: SHEYTAN-LA-all-logs.txt
          path: SHEYTAN-LA-all-logs.txt
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false
`

// fixtureWithDuplicateLogUpload adds a SECOND consolidated log upload in
// another job — the negative contract case.
const fixtureWithDuplicateLogUpload = `jobs:
  build-windows:
    name: Windows x64
    steps:
      - name: Upload Windows package
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: windows-x64-package
          path: |
            dist/SHEYTAN-LA-v1.11.1-windows-x64.zip
            dist/SHEYTAN-LA-v1.11.1-windows-x64-installer.exe
          overwrite: true
  consolidate-logs:
    name: Consolidate logs
    steps:
      - name: Upload the consolidated run log (single plain-text artifact)
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: SHEYTAN-LA-all-logs.txt
          path: SHEYTAN-LA-all-logs.txt
          archive: false
          overwrite: true
  debug-logs:
    name: Debug logs
    steps:
      - name: Upload the consolidated run log AGAIN (contract breach)
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: SHEYTAN-LA-all-logs.txt
          path: SHEYTAN-LA-all-logs.txt
          archive: false
          overwrite: true
`

// fixtureWithExtraUpload carries an unrelated fourth upload — the
// closed-surface negative case.
const fixtureWithExtraUpload = `jobs:
  build-windows:
    name: Windows x64
    steps:
      - name: Upload Windows package
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: windows-x64-package
          path: dist/SHEYTAN-LA-v1.11.1-windows-x64.zip
          overwrite: true
  consolidate-logs:
    name: Consolidate logs
    steps:
      - name: Upload the consolidated run log (single plain-text artifact)
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: SHEYTAN-LA-all-logs.txt
          path: SHEYTAN-LA-all-logs.txt
          archive: false
          overwrite: true
  build-linux:
    name: Linux x64
    steps:
      - name: Upload Linux package
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: linux-x64-package
          path: dist/SHEYTAN-LA-v1.11.1-linux-x64.zip
          overwrite: true
  extra:
    name: Extra
    steps:
      - name: Upload something else
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: debug-bundle
          path: debug/
          overwrite: true
`

// TestUploadArtifactContractFixtures: the positive fixture (both package
// uploads + the single consolidated log upload) PASSES — including its
// CRLF materialization, the exact shape the retired scanner broke on —
// and both negative fixtures FAIL with the contract message.
func TestUploadArtifactContractFixtures(t *testing.T) {
	good := fmt.Sprintf(fixtureUploadWorkflow, "v7", "v7", "v7")

	if err := ValidateUploadArtifactContract(good); err != nil {
		t.Fatalf("the contracted three-upload fixture must pass: %v", err)
	}

	// CRLF variant — the windows-latest checkout materialization. The
	// retired scanner leaked step context across jobs exactly here; the
	// YAML-based extraction must be indifferent.
	crlf := strings.ReplaceAll(good, "\n", "\r\n")
	if err := ValidateUploadArtifactContract(crlf); err != nil {
		t.Fatalf("the CRLF-materialized fixture must pass (run 41901967798 Failure B): %v", err)
	}

	if err := ValidateUploadArtifactContract(fixtureWithDuplicateLogUpload); err == nil {
		t.Fatal("a duplicate consolidated log upload must FAIL the contract")
	} else if !strings.Contains(err.Error(), "exactly ONE consolidated log artifact upload") {
		t.Fatalf("duplicate-log failure must name the one-log rule, got: %v", err)
	}

	if err := ValidateUploadArtifactContract(fixtureWithExtraUpload); err == nil {
		t.Fatal("an extra (non-contracted) upload must FAIL the contract")
	} else if !strings.Contains(err.Error(), "debug-bundle") {
		t.Fatalf("extra-upload failure must name the offending artifact, got: %v", err)
	}

	// A package upload must never satisfy the log rule by substring:
	// a workflow whose ONLY upload is a package has ZERO log uploads.
	pkgOnly := `jobs:
  build-windows:
    steps:
      - name: Upload Windows package
        uses: actions/upload-artifact@` + "v7" + `
        with:
          name: windows-x64-package
          path: dist/SHEYTAN-LA-v1.11.1-windows-x64.zip
          overwrite: true
`
	if err := ValidateUploadArtifactContract(pkgOnly); err == nil {
		t.Fatal("a package-only upload surface must FAIL (zero consolidated log uploads)")
	}
}

// TestUploadArtifactPinPolicyFixtures: under the v1.11.1 release-tag
// policy every external uses: ref must be an upstream release tag —
// major tags (@v7) and exact version tags (@v7.0.2) are accepted;
// commit hashes (full or partial), branch refs, and malformed refs are
// rejected.
func TestUploadArtifactPinPolicyFixtures(t *testing.T) {
	fixture := func(ref string) string {
		return fmt.Sprintf(fixtureUploadWorkflow, ref, ref, ref)
	}

	// Accepted: the maintained major release tag (the workflow's
	// spelling since the v1.11.1 maintenance policy change).
	if err := ValidateUploadArtifactPins(fixture("v7")); err != nil {
		t.Fatalf("the v7 release tag must be accepted: %v", err)
	}

	// Accepted: an exact version release tag.
	if err := ValidateUploadArtifactPins(fixture("v7.0.2")); err != nil {
		t.Fatalf("an exact version release tag must be accepted: %v", err)
	}

	// Accepted: the tag with the workflow's trailing comment shape.
	// (YAML strips comments before the value reaches the validator —
	// asserted here so a comment regression cannot reappear.)
	withComment := fixture("v7" + ` # release tag`)
	if err := ValidateUploadArtifactPins(withComment); err != nil {
		t.Fatalf("the release tag with a trailing comment must be accepted: %v", err)
	}

	// Rejected: commit-hash pins (full and partial), branch refs and
	// malformed refs — the exact shapes the v1.11.1 policy forbids.
	for _, bad := range []string{
		"cf430e030ddbb5b0abf93d22962f4752f3646cd9", // full commit SHA
		"cf430e030ddbb", // partial hash
		"main",          // branch ref
		"master",        // branch ref
		"",              // empty ref
	} {
		if err := ValidateUploadArtifactPins(fixture(bad)); err == nil {
			t.Errorf("ref %q must be REJECTED under the release-tag policy", bad)
		}
	}

	// Rejected: no upload-artifact step at all.
	if err := ValidateUploadArtifactPins("jobs:\n  preflight:\n    steps:\n      - name: empty\n        run: echo hi\n"); err == nil {
		t.Fatal("a workflow without upload-artifact steps must FAIL the pin policy (the log upload is missing)")
	}
}
