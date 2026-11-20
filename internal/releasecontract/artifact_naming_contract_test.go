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

// TestWorkflowUploadArtifactPinsMatchAllowlist: every external action
// use in the REAL workflow is an immutable 40-hex commit pin carrying a
// synced release-tag comment (the shared stress-gate validator — one
// authority; the CodeQL run 43883548572 alerts #185/#186 policy).
func TestWorkflowUploadArtifactPinsMatchAllowlist(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	if err := ValidateUploadArtifactPins(workflow); err != nil {
		t.Fatalf("upload-artifact pin policy violated: %v", err)
	}
}

// TestCodeQLWorkflowImmutablePins: the CodeQL workflow follows the SAME
// immutable-SHA action-reference policy as the desktop workflow — both
// workflow files are governed, not just build-desktop.yml (SEC-03:
// pinning the refs never changes the analyzer configuration, the
// manual C++ extraction or the SARIF uploads).
func TestCodeQLWorkflowImmutablePins(t *testing.T) {
	workflow := readTreeFile(t, ".github/workflows/codeql.yml")

	if err := ValidateUploadArtifactPins(workflow); err != nil {
		t.Fatalf("CodeQL workflow pin policy violated: %v", err)
	}
}

// TestWorkflowStableArtifactNamesExact: the ONE consolidated log
// artifact and the two packages use the exact contracted names, the
// upload surface is EXACTLY those three steps (scoped per job by the
// shared parser), and the retired run-id-suffixed log name never
// reappears.
func TestWorkflowStableArtifactNamesExact(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	// The shared validators carry the exact contract semantics — the
	// stress gate runs the SAME functions, so the gates cannot drift.
	// The orchestrator owns the two evidence uploads; the package
	// uploads live in their single authoritative reusable lanes.
	if err := ValidateUploadArtifactContract(workflow); err != nil {
		t.Fatalf("orchestrator upload-artifact contract violated: %v", err)
	}

	// The CI consolidation: all six package uploads resolve from the ONE
	// matrix upload step of build.yml; the per-surface validators parse
	// that same file.
	if err := ValidatePlatformPackageUploadContract(workflow); err != nil {
		t.Fatalf("platform package upload contract violated: %v", err)
	}
	if err := ValidateMobileArtifactUploadContract(workflow); err != nil {
		t.Fatalf("mobile/platform upload contract violated: %v", err)
	}
	if err := ValidatePlatformMatrixStructure(workflow); err != nil {
		t.Fatalf("platform matrix structure violated: %v", err)
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
			// The build-evidence artifact is the ONE contracted exception:
			// its name carries ${{ github.run_attempt }} so parallel rerun
			// attempts never collide (the consolidation contract). Every
			// other artifact name must be stable.
			if step.Name == BuildEvidenceArtifactSkeleton {
				continue
			}
			if strings.Contains(strings.ToLower(step.Name), "attempt") {
				t.Errorf("%s artifact name %q carries a run-attempt suffix — stable-name contract (run id/commit/attempt live INSIDE the consolidated report; only the build-evidence artifact may carry the attempt)", action, step.Name)
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

// fixtureUploadWorkflow is the CONSOLIDATED upload surface fixture:
// the platform-build matrix job's package + evidence uploads and the
// collector job's consolidated diagnostic + machine-readable index.
// The %s slots receive the upload-artifact refs.
const fixtureUploadWorkflow = `jobs:
  platform-build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - target: windows-x64
            os: windows-2025
            artifact: SHEYTAN-LA-ALPHA-windows-x64-portable
            kind: zip
    steps:
      - name: Upload the platform package
        if: always()
        uses: actions/upload-artifact@%s
        with:
          name: ${{ matrix.artifact }}
          path: dist/SHEYTAN-LA-ALPHA-*
          if-no-files-found: error
          overwrite: true
      - name: Upload build evidence (ci-logs)
        if: always()
        uses: actions/upload-artifact@%s
        with:
          name: build-evidence-${{ matrix.target }}-${{ github.run_attempt }}
          path: ci-logs/
          if-no-files-found: warn
          overwrite: true
  collect-errors:
    name: Collect errors
    steps:
      - name: Upload the errors-warnings-only consolidated diagnostic (single plain-text artifact)
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@%s
        with:
          name: SHEYTAN-LA-errors-warnings.txt
          path: SHEYTAN-LA-errors-warnings.txt
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false
      - name: Upload the machine-readable run diagnostics index
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@%s
        with:
          name: run-diagnostics.json
          path: run-diagnostics.json
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false
`

// fixtureWithDuplicateLogUpload adds a SECOND consolidated log upload in
// another job — the negative contract case.
const fixtureWithDuplicateLogUpload = `jobs:
  collect-errors:
    name: Collect errors
    steps:
      - name: Upload the consolidated run log (single plain-text artifact)
        uses: actions/upload-artifact@v7
        with:
          name: SHEYTAN-LA-errors-warnings.txt
          path: SHEYTAN-LA-errors-warnings.txt
          archive: false
          overwrite: true
  debug-logs:
    name: Debug logs
    steps:
      - name: Upload the consolidated run log AGAIN (contract breach)
        uses: actions/upload-artifact@v7
        with:
          name: SHEYTAN-LA-errors-warnings.txt
          path: SHEYTAN-LA-errors-warnings.txt
          archive: false
          overwrite: true
`

// fixtureWithExtraUpload carries an unrelated fifth upload — the
// closed-surface negative case.
const fixtureWithExtraUpload = `jobs:
  collect-errors:
    name: Collect errors
    steps:
      - name: Upload the consolidated run log (single plain-text artifact)
        uses: actions/upload-artifact@v7
        with:
          name: SHEYTAN-LA-errors-warnings.txt
          path: SHEYTAN-LA-errors-warnings.txt
          archive: false
          overwrite: true
      - name: Upload the machine-readable run diagnostics index
        uses: actions/upload-artifact@v7
        with:
          name: run-diagnostics.json
          path: run-diagnostics.json
          archive: false
          overwrite: true
  extra:
    name: Extra
    steps:
      - name: Upload something else
        uses: actions/upload-artifact@v7
        with:
          name: debug-bundle
          path: debug/
          overwrite: true
`

// TestUploadArtifactContractFixtures: the positive fixture (the four
// contracted upload shapes) PASSES — including its CRLF materialization,
// the exact shape the retired scanner broke on — and both negative
// fixtures FAIL with the contract message.
func TestUploadArtifactContractFixtures(t *testing.T) {
	good := fmt.Sprintf(fixtureUploadWorkflow, "v7", "v7", "v7", "v7")

	if err := ValidateUploadArtifactContract(good); err != nil {
		t.Fatalf("the contracted four-upload fixture must pass: %v", err)
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
	} else if !strings.Contains(err.Error(), "exactly ONE consolidated diagnostic artifact upload") {
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
  platform-build:
    steps:
      - name: Upload the portable ZIP
        uses: actions/upload-artifact@v7
        with:
          name: ${{ matrix.artifact }}
          path: dist/SHEYTAN-LA-ALPHA-*
          overwrite: true
`

	// The matrix package upload contract (section 6.3 names) and its
	// negative case: a retired artifact name fails the per-surface
	// validator.
	laneGood := `jobs:
  platform-build:
    strategy:
      fail-fast: false
      matrix:
        include:
          - target: windows-x64
            os: windows-2025
            artifact: SHEYTAN-LA-ALPHA-windows-x64-portable
            kind: zip
          - target: linux-x64
            os: ubuntu-26.04
            artifact: SHEYTAN-LA-ALPHA-linux-x64-portable
            kind: targz
          - target: macos-arm64
            os: macos-15
            artifact: SHEYTAN-LA-ALPHA-macos-arm64-portable
            kind: appzip
          - target: macos-x64
            os: macos-15-intel
            artifact: SHEYTAN-LA-ALPHA-macos-x64-portable
            kind: appzip
          - target: android-arm64
            os: ubuntu-26.04
            artifact: SHEYTAN-LA-ALPHA-android-arm64
            kind: apk
          - target: ios-simulator-arm64
            os: macos-15
            artifact: SHEYTAN-LA-ALPHA-ios-simulator-arm64
            kind: simzip
          - target: ios-device-arm64
            os: macos-15
            artifact: SHEYTAN-LA-ALPHA-ios-device-arm64
            kind: ipa
    steps:
      - name: Upload the platform package
        uses: actions/upload-artifact@v7
        with:
          name: ${{ matrix.artifact }}
          path: dist/SHEYTAN-LA-ALPHA-*
          overwrite: true
      - name: Verify the portable packages
        run: |
          test -f dist/SHEYTAN-LA-ALPHA-windows-x64-portable.zip
          test -f dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz
`
	if err := ValidatePlatformPackageUploadContract(laneGood); err != nil {
		t.Fatalf("the contracted matrix package fixture must pass: %v", err)
	}

	laneBad := strings.Replace(laneGood, "SHEYTAN-LA-ALPHA-windows-x64-portable\n", "windows-x64-package\n", 1)
	if err := ValidatePlatformPackageUploadContract(laneBad); err == nil {
		t.Fatal("a matrix entry carrying a retired package name must FAIL the per-surface contract")
	}

	if err := ValidateUploadArtifactContract(pkgOnly); err == nil {
		t.Fatal("a package-only upload surface must FAIL (zero consolidated log uploads)")
	}
}

// TestUploadArtifactPinPolicyFixtures: under the v1.11.1 immutable-SHA
// policy (CodeQL run 43883548572, alerts #185/#186) every external
// uses: ref must be a full 40-hex commit pin carrying a synced
// release-tag comment; mutable tags, branch refs, partial hashes,
// malformed refs and uncommented SHAs are rejected.
func TestUploadArtifactPinPolicyFixtures(t *testing.T) {
	fixture := func(ref string) string {
		return fmt.Sprintf(fixtureUploadWorkflow, ref, ref, ref, ref)
	}

	// Accepted: the reviewed immutable v7 commit pin with the synced
	// release-tag comment — the workflow's spelling since the policy
	// reversal (the SHA is the verified actions/upload-artifact v7 pin,
	// resolved from the upstream tag and matching the run-log table).
	if err := ValidateUploadArtifactPins(fixture("cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7")); err != nil {
		t.Fatalf("the immutable SHA pin with a synced version comment must be accepted: %v", err)
	}

	// Accepted: an exact-version comment on the same pinned SHA.
	if err := ValidateUploadArtifactPins(fixture("cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2")); err != nil {
		t.Fatalf("an exact-version comment must be accepted: %v", err)
	}

	// Rejected: mutable tags (the retired spelling), partial hashes,
	// branch refs, an uncommented SHA (a Dependabot update dead-end), a
	// non-version comment and an empty ref.
	for _, bad := range []string{
		"v7",            // mutable major tag — the retired policy's spelling
		"v7.0.2",        // mutable exact tag
		"cf430e030ddbb", // partial hash
		"main",          // branch ref
		"cf430e030ddbb5b0abf93d22962f4752f3646cd9",          // immutable SHA WITHOUT the version comment
		"cf430e030ddbb5b0abf93d22962f4752f3646cd9 # pinned", // SHA with a non-version comment
		"", // empty ref
	} {
		if err := ValidateUploadArtifactPins(fixture(bad)); err == nil {
			t.Errorf("ref %q must be REJECTED under the immutable-SHA policy", bad)
		}
	}

	// Rejected: no upload-artifact step at all.
	if err := ValidateUploadArtifactPins("jobs:\n  preflight:\n    steps:\n      - name: empty\n        run: echo hi\n"); err == nil {
		t.Fatal("a workflow without upload-artifact steps must FAIL the pin policy (the log upload is missing)")
	}
}
