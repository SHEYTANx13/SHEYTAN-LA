// Package releasecontract — artifact_naming_contract_test.go
//
// v1.11.1 SINGLE-ARTIFACT RUN-LOG CONTRACT (work order for run
// #41548925352 §6.2, implemented by the v1.11.1 collector rework):
//
//   - NO artifact name (upload or download) carries a run-attempt suffix
//     in ANY spelling (`-attempt${{ ... }}`, `-attempt-1`, `-attempt1`);
//   - the two packages keep the exact stable names on upload (build
//     jobs) and download (release job);
//   - the retired five-part log-part era (preflight/audit/ui/windows/
//     linux-logs-part, ci-parts/ namespaced downloads, per-part
//     merge-multiple) must NOT reappear — the collector now pulls every
//     completed job's log straight from the paginated jobs API, so there
//     is nothing to flatten in the first place;
//   - the attempt number survives ONLY inside the collector's LOCAL
//     report as structured provenance metadata (the workflow's
//     CONSOLIDATE_ATTEMPT env + the fixtures' --attempt rerun case),
//     never in a name.
//
// RUN 44835425398 ARTIFACT POLICY (this pass's controlling contract):
//
//   - the platform-build workflow's ENTIRE permitted artifact surface is
//     the ONE matrix package upload (${{ matrix.artifact }}) gated on
//     the explicit package-eligibility step output;
//   - build evidence of ANY kind — consolidated logs, diagnostics
//     indexes, ci-logs/ bundles, test reports, JSONL compiler output,
//     coverage/race reports — is NEVER uploaded as a workflow artifact;
//   - the CodeQL workflow's only artifact remains the security-alert
//     plain-text report; its build-log and diagnostics-index uploads are
//     banned;
//   - the regression coverage below is MUTATION-based: each banned
//     evidence class is injected into the REAL parsed workflow text and
//     the contract must reject it — the allowlist is proven on both
//     sides of every rule, not merely asserted.
//
// v1.11.1 (run 41901967798): the extraction below uses the SHARED YAML-
// parsed, per-job-scoped contract helpers (workflow_contract.go) instead
// of the retired hand-rolled line scanner. A real YAML parser is
// line-ending-proof by construction; fixture coverage pins the CRLF case
// explicitly.
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
// immutable-SHA action-reference policy as the platform-build workflow —
// both workflow files are governed (SEC-03: pinning the refs never
// changes the analyzer configuration, the manual C++ extraction or the
// SARIF uploads).
func TestCodeQLWorkflowImmutablePins(t *testing.T) {
	workflow := readTreeFile(t, ".github/workflows/codeql.yml")

	if err := ValidateUploadArtifactPins(workflow); err != nil {
		t.Fatalf("CodeQL workflow pin policy violated: %v", err)
	}
}

// TestWorkflowStableArtifactNamesExact: the TWO permitted platform-build
// uploads are the matrix package step and the consolidated errors/warnings
// report step (2026-11-23 policy); the upload surface is EXACTLY those
// steps (scoped per job by the shared parser); the release job downloads
// the exact stable package names; and the retired run-id-suffixed log
// name never reappears.
func TestWorkflowStableArtifactNamesExact(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	// The shared validators carry the exact contract semantics — the
	// stress gate runs the SAME functions, so the gates cannot drift.
	// The package upload is the ONLY upload; no evidence upload exists.
	if err := ValidateUploadArtifactContract(workflow); err != nil {
		t.Fatalf("platform-build upload-artifact contract violated: %v", err)
	}

	// All five package names resolve from the ONE matrix upload step of
	// build.yml; the per-surface validators parse that same file.
	if err := ValidatePlatformPackageUploadContract(workflow); err != nil {
		t.Fatalf("platform package upload contract violated: %v", err)
	}
	if err := ValidateMobileArtifactUploadContract(workflow); err != nil {
		t.Fatalf("mobile/platform upload contract violated: %v", err)
	}
	if err := ValidatePlatformMatrixStructure(workflow); err != nil {
		t.Fatalf("platform matrix structure violated: %v", err)
	}

	uploads := mustArtifactSteps(t, workflow, "actions/upload-artifact")
	if len(uploads) != 2 {
		t.Fatalf("the platform-build workflow must carry EXACTLY TWO uploads (the matrix package + the consolidated errors/warnings report), found %d", len(uploads))
	}
	if uploads[0].Name != MatrixPackageArtifactSlot || uploads[1].Name != MatrixPackageArtifactSlot {
		// order-insensitive: exactly one of the two is the package step
		packageSteps := 0
		for _, u := range uploads {
			if u.Name == MatrixPackageArtifactSlot {
				packageSteps++
			}
		}
		if packageSteps != 1 {
			t.Fatalf("exactly one upload must be the matrix package step %q, got %d of %d", MatrixPackageArtifactSlot, packageSteps, len(uploads))
		}
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
// artifact name contains an attempt-suffix in any spelling. Run
// 44835425398: the former build-evidence exception is GONE together with
// the upload it named — every artifact name is stable.
func TestWorkflowArtifactNamesCarryNoAttemptSuffix(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	for _, action := range []string{"actions/upload-artifact", "actions/download-artifact"} {
		for _, step := range mustArtifactSteps(t, workflow, action) {
			if strings.Contains(strings.ToLower(step.Name), "attempt") {
				t.Errorf("%s artifact name %q carries a run-attempt suffix — stable-name contract (run id/commit/attempt live INSIDE the collector's LOCAL report; no artifact carries the attempt)", action, step.Name)
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
// the attempt number for provenance INSIDE the LOCAL report (structured
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
// Fixture coverage (run 41901967798 §2.2/§2.3, redesigned by the run
// 44835425398 artifact policy): the pin policy and the closed upload
// allowlist are pinned against synthetic workflows so the shared
// validators are proven on BOTH sides of every rule — including the
// CRLF materialization that broke the retired scanner on the Windows
// runner.
// ---------------------------------------------------------------------------

// fixtureUploadWorkflow is the PERMITTED upload surface fixture: the ONE
// matrix package upload, gated on the explicit eligibility output, and
// the collector job uploading EXACTLY the ONE curated errors/warnings
// report (2026-11-23 policy — the diagnostics index stays local). The %s
// slot receives the upload-artifact ref.
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
      - name: Determine package upload eligibility (explicit step output)
        id: package-eligibility
        run: echo "upload-package=true" >> "$GITHUB_OUTPUT"
      - name: Upload the platform package
        if: always() && steps.package-eligibility.outputs.upload-package == 'true'
        uses: actions/upload-artifact@%s
        with:
          name: ${{ matrix.artifact }}
          path: dist/SHEYTAN-LA-ALPHA-*
          if-no-files-found: error
          retention-days: 14
          overwrite: true
  collect-errors:
    name: Collect errors
    steps:
      - name: Collect the run-attempt logs into one plain-text errors/warnings report
        run: |
          bash scripts/ci/consolidate-run-log.sh \
            --out SHEYTAN-LA-errors-warnings.txt \
            --json-out run-diagnostics.json || rc=$?
      - name: Upload the errors/warnings report
        if: always()
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7
        with:
          name: SHEYTAN-LA-errors-warnings
          path: SHEYTAN-LA-errors-warnings.txt
          if-no-files-found: error
          retention-days: 30
          overwrite: true
`

// fixtureWithEvidenceUpload appends ONE EXTRA upload step of the given
// name/path to the collector job — the banned-shape fixture. With the
// 2026-11-23 policy the collector must carry EXACTLY the ONE contracted
// report upload, so ANY extra upload (banned name, evidence marker, or a
// second report) fails the contract.
func fixtureWithEvidenceUpload(name, path string) string {
	return fixtureUploadWorkflow + fmt.Sprintf(`      - name: Upload %s
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@v7
        with:
          name: %s
          path: %s
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false
`, name, name, path)
}

// TestUploadArtifactContractFixtures: the positive fixture (the TWO
// permitted uploads — matrix package + curated report — and nothing
// else) PASSES — including its CRLF materialization, the exact shape the
// retired scanner broke on — and every banned evidence class FAILS the
// contract with the policy message.
func TestUploadArtifactContractFixtures(t *testing.T) {
	good := fmt.Sprintf(fixtureUploadWorkflow, "v7")

	if err := ValidateUploadArtifactContract(good); err != nil {
		t.Fatalf("the permitted single-package-upload fixture must pass: %v", err)
	}

	// CRLF variant — the windows-latest checkout materialization. The
	// retired scanner leaked step context across jobs exactly here; the
	// YAML-based extraction must be indifferent.
	crlf := strings.ReplaceAll(good, "\n", "\r\n")
	if err := ValidateUploadArtifactContract(crlf); err != nil {
		t.Fatalf("the CRLF-materialized fixture must pass (run 41901967798 Failure B): %v", err)
	}

	// Every banned evidence class must fail, each with the policy
	// message naming the prohibition.
	bannedClasses := []struct {
		desc     string
		name     string
		path     string
		contains string
	}{
		{"per-lane build-evidence bundle", "build-evidence-${{ matrix.target }}-${{ github.run_attempt }}", "ci-logs/", "PROHIBITED"},
		{"extension-carrying report name", "SHEYTAN-LA-errors-warnings.txt", "SHEYTAN-LA-errors-warnings.txt", "PROHIBITED"},
		{"second curated report upload", "SHEYTAN-LA-errors-warnings", "SHEYTAN-LA-errors-warnings.txt", "exactly ONE consolidated"},
		{"machine-readable run diagnostics index", "run-diagnostics.json", "run-diagnostics.json", "PROHIBITED"},
		{"CodeQL consolidated build log", "SHEYTAN-LA-codeql-all-logs.txt", "SHEYTAN-LA-codeql-all-logs.txt", "PROHIBITED"},
		{"CodeQL run diagnostics index", "codeql-run-diagnostics.json", "codeql-run-diagnostics.json", "PROHIBITED"},
		{"ci-logs diagnostic directory", "ci-logs", "ci-logs/", "evidence marker"},
		{"test report directory", "test-results", "test-results/", "evidence marker"},
		{"junit report", "junit-report", "reports/junit.xml", "evidence marker"},
		{"coverage report", "coverage", "coverage.out", "evidence marker"},
		{"JSONL compiler trace", "compiler-trace", "ci-logs/compile.jsonl", "evidence marker"},
		{"raw log file", "runner-log", "ci-logs/job.log", "evidence marker"},
		// A matrix-NAMED upload with a non-package path exercises the path
		// rule itself: ONLY the dist/SHEYTAN-LA-ALPHA-* package may be staged.
		{"temporary build directory (matrix-named)", "${{ matrix.artifact }}", "/home/runner/work/_temp/build", "must stage ONLY"},
	}

	for _, tc := range bannedClasses {
		mutated := fixtureWithEvidenceUpload(tc.name, tc.path)
		err := ValidateUploadArtifactContract(mutated)
		if err == nil {
			t.Errorf("%s: the %q upload must FAIL the contract", tc.desc, tc.name)

			continue
		}
		if !strings.Contains(err.Error(), tc.contains) {
			t.Errorf("%s: failure must carry %q, got: %v", tc.desc, tc.contains, err)
		}
	}

	// The eligibility condition is contracted: a package upload WITHOUT
	// the explicit eligibility gate fails (a package must never be
	// uploaded unconditionally — an intentionally absent package would
	// otherwise produce an empty or partial artifact).
	noEligibility := strings.Replace(
		good,
		"if: always() && steps.package-eligibility.outputs.upload-package == 'true'",
		"if: always()",
		1,
	)
	if err := ValidateUploadArtifactContract(noEligibility); err == nil {
		t.Fatal("a package upload without the eligibility condition must FAIL the contract")
	} else if !strings.Contains(err.Error(), "package-eligibility.outputs.upload-package") {
		t.Fatalf("missing-eligibility failure must name the gate, got: %v", err)
	}

	// A retired artifact name still fails the per-surface package
	// validator (matrix names are contracted exactly).
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
}

// TestUploadArtifactPinsPolicyFixtures: under the v1.11.1 immutable-SHA
// policy (CodeQL run 43883548572, alerts #185/#186) every external
// uses: ref must be a full 40-hex commit pin carrying a synced
// release-tag comment; mutable tags, branch refs, partial hashes,
// malformed refs and uncommented SHAs are rejected. The pin validation
// keeps working against the ONE permitted package upload (run
// 44835425398 policy — do NOT remove the pin contract together with the
// removed evidence uploads).
func TestUploadArtifactPinsPolicyFixtures(t *testing.T) {
	fixture := func(ref string) string {
		return fmt.Sprintf(fixtureUploadWorkflow, ref)
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
		t.Fatal("a workflow without upload-artifact steps must FAIL the pin policy (the package upload is missing)")
	}
}

// TestPlatformBuildArtifactAllowlistMutations (run 44835425398 §5.7
// regression): each banned evidence class is INJECTED into the REAL
// platform-build workflow text (a parseable workflow, not a few
// strings) and the shared contract must reject the mutation. The
// unmutated workflow passes — the allowlist is proven closed on both
// sides.
func TestPlatformBuildArtifactAllowlistMutations(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	if err := ValidateUploadArtifactContract(workflow); err != nil {
		t.Fatalf("the real workflow must pass its own allowlist before mutations: %v", err)
	}

	// The injection point: a step list item INSIDE the collect-errors
	// job (after the collector step, before its verdict step) — the
	// exact place a future "helpful" evidence upload would appear.
	anchor := "      - name: Collector verdict (fail the job on coverage gaps)"
	if !strings.Contains(workflow, anchor) {
		t.Fatalf("injection anchor missing from the real workflow — update the mutation test")
	}

	mutate := func(name, path string) string {
		step := fmt.Sprintf(`      - name: Upload %s
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7
        with:
          name: %s
          path: %s
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false

`, name, name, path)

		return strings.Replace(workflow, anchor, step+anchor, 1)
	}

	mutations := []struct {
		desc string
		name string
		path string
	}{
		{"build-evidence bundle", "build-evidence-macos-arm64-1", "ci-logs/"},
		{"consolidated report", ConsolidatedLogReportFileName, ConsolidatedLogReportFileName},
		{"run diagnostics index", RunDiagnosticsReportFileName, RunDiagnosticsReportFileName},
		{"ci-logs diagnostic dir", "ci-logs", "ci-logs/"},
		{"jsonl compiler output", "go-build-jsonl", "ci-logs/build.jsonl"},
		{"test report", "test-results", "test-results/"},
		{"junit file", "junit", "reports/junit.xml"},
		{"coverage report", "coverage", "coverage/"},
		{"raw workflow log", "workflow-logs", "ci-logs/job.log"},
	}

	for _, m := range mutations {
		mutated := mutate(m.name, m.path)

		// The mutation must stay parseable YAML — otherwise the rejection
		// would prove nothing about the allowlist.
		if _, err := ArtifactSteps(mutated, "actions/upload-artifact"); err != nil {
			t.Fatalf("%s: mutated workflow failed to parse (test bug): %v", m.desc, err)
		}

		if err := ValidateUploadArtifactContract(mutated); err == nil {
			t.Errorf("%s: the injected %q upload must FAIL the contract", m.desc, m.name)
		}
	}
}

// TestCodeQLWorkflowRejectsEvidenceUploadMutations (run 44835425398
// §5.7 regression): the CodeQL workflow's resilience contract must
// reject a reintroduced build-log or diagnostics-index upload, and must
// keep accepting the security-alert report.
func TestCodeQLWorkflowRejectsEvidenceUploadMutations(t *testing.T) {
	codeql := readTreeFile(t, ".github/workflows/codeql.yml")

	if err := ValidateCodeQLWorkflowResilience(codeql); err != nil {
		t.Fatalf("the real CodeQL workflow must pass its own contract before mutations: %v", err)
	}

	// The security-alert report upload must remain present in the parsed
	// workflow, together with the ONE curated errors/warnings report of
	// the CodeQL collector (2026-11-23 policy). Every other upload is
	// rejected.
	foundSecurityReport := false
	foundCollectorReport := false

	steps, err := ArtifactSteps(codeql, "actions/upload-artifact")
	if err != nil {
		t.Fatalf("CodeQL workflow parse failed: %v", err)
	}

	for _, st := range steps {
		switch {
		case st.Job == "export-alert-report" && strings.Contains(st.Path, "code-scanning-alerts"):
			foundSecurityReport = true
		case st.Job == "collect-codeql-logs" && st.Name == CodeQLLogReportArtifactName && st.Path == CodeQLLogReportFileName:
			foundCollectorReport = true
		default:
			t.Errorf("unexpected CodeQL artifact upload in job %q: %q (only the security report and the curated errors/warnings report are permitted)", st.Job, st.Name)
		}
	}

	if !foundSecurityReport {
		t.Fatal("the export-alert-report job must keep uploading reports/code-scanning-alerts.txt (the security report is NOT build evidence)")
	}
	if !foundCollectorReport {
		t.Fatal("the collect-codeql-logs job must keep uploading the curated errors/warnings report (2026-11-23 policy)")
	}

	anchor := "      - name: Collector verdict (fail the job on coverage gaps)"
	if !strings.Contains(codeql, anchor) {
		t.Fatalf("injection anchor missing from the CodeQL workflow — update the mutation test")
	}

	mutate := func(name, path string) string {
		step := fmt.Sprintf(`      - name: Upload %s
        if: ${{ !cancelled() }}
        uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7
        with:
          name: %s
          path: %s
          if-no-files-found: error
          retention-days: 14
          overwrite: true
          archive: false

`, name, name, path)

		return strings.Replace(codeql, anchor, step+anchor, 1)
	}

	for _, m := range []struct {
		desc string
		name string
		path string
	}{
		{"CodeQL build log", ConsolidatedLogReportFileName, ConsolidatedLogReportFileName},
		{"CodeQL diagnostics index", RunDiagnosticsReportFileName, RunDiagnosticsReportFileName},
		{"raw compiler jsonl", "codeql-build-jsonl", "codeql-build.jsonl"},
	} {
		if err := ValidateCodeQLWorkflowResilience(mutate(m.name, m.path)); err == nil {
			t.Errorf("%s: the injected CodeQL upload must FAIL the resilience contract", m.desc)
		}
	}
}
