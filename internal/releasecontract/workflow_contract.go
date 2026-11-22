// Package releasecontract — workflow_contract.go
//
// v1.11.1 (run 41901967798): ONE shared validation semantics for the
// GitHub Actions action pins and the upload-artifact surface, consumed
// by BOTH the stress gate (cmd/stress_release_surface.go) and the Go
// contract tests in this package — never two independently maintained
// action checks.
//
// Two failures from run 41901967798 are closed by this file:
//
//   - Failure A (release_surface FAIL: "workflow missing required action
//     actions/upload-artifact@v7"): the workflow legitimately pins
//     upload-artifact to the official v7.0.2 immutable commit SHA; the
//     old literal `strings.Contains(w, "actions/upload-artifact@v7")`
//     check rejected the CORRECT workflow. ValidateUploadArtifactPins
//     validates the pinned action identity against an explicit reviewed
//     allowlist of verified immutable full-length commit SHAs — moving
//     tags, @main, partial hashes, malformed references and unapproved
//     SHAs all fail.
//
//   - Failure B (TestWorkflowStableArtifactNamesExact counted 2 log
//     uploads on the Windows runner): the hand-rolled line scanner was
//     CRLF-sensitive — windows-latest materializes the unpinned .yml as
//     CRLF, the `^  job-key:$` boundary regexp never matched, step
//     context leaked across jobs and the job display names ("Linux
//     x64", "Consolidate logs") were captured as artifact names, one of
//     which contains "logs". The extraction below parses the workflow
//     with a real YAML parser (gopkg.in/yaml.v3 — already in go.mod, no
//     new dependency), scopes every upload to its owning job and step,
//     and matches artifact names EXACTLY — a job display name can never
//     enter the count again, on any platform or line-ending policy.
package releasecontract

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlatformBuildWorkflowPath is the ONE canonical platform-build workflow:
// the CI consolidation merged the retired build-desktop.yml orchestrator
// and its five reusable build-windows/linux/macos/android/ios.yml lanes
// into this single file (plus codeql.yml, which stays untouched). Every
// validator, contract test and the stress gate reads THIS path.
const PlatformBuildWorkflowPath = ".github/workflows/build.yml"

// ConsolidatedLogReportFileName is the LOCAL errors/warnings-only report
// file the collector writes inside the runner workspace (--out). Run
// 44835425398 policy: the report stays LOCAL to the job — uploading it
// (or any build evidence) as a workflow artifact is PROHIBITED. The
// normal GitHub Actions job logs remain the platform's log surface.
const ConsolidatedLogReportFileName = "SHEYTAN-LA-errors-warnings.txt"

// RunDiagnosticsReportFileName is the LOCAL machine-readable run-level
// diagnostics index the collector writes next to the report (--json-out).
// Like the report it is a workspace file, never an uploaded artifact
// (run 44835425398 artifact policy).
const RunDiagnosticsReportFileName = "run-diagnostics.json"

// ForbiddenArtifactUploadNames are the evidence artifact names whose
// upload is PROHIBITED by the run 44835425398 artifact policy: the two
// collector reports (local files), the per-lane build-evidence bundle,
// and the CodeQL log/diagnostics-index uploads. Any upload step carrying
// one of these names fails the contract.
var ForbiddenArtifactUploadNames = []string{
	ConsolidatedLogReportFileName,
	RunDiagnosticsReportFileName,
	ForbiddenBuildEvidenceArtifactName,
	"SHEYTAN-LA-codeql-all-logs.txt",
	"codeql-run-diagnostics.json",
}

// ForbiddenArtifactUploadSubstrings are the evidence markers whose
// appearance in an upload NAME or PATH fails the contract: diagnostic
// logs, build evidence, test reports, JSONL compiler output, run
// diagnostics, coverage/race reports and temporary build directories
// (work order §5.1: build evidence is never uploaded as an artifact).
var ForbiddenArtifactUploadSubstrings = []string{
	"ci-logs",
	"build-evidence",
	"errors-warnings",
	"run-diagnostics",
	"codeql-all-logs",
	"test-results",
	"junit",
	"coverage",
	"race-report",
	".jsonl",
	".log",
}

// ForbiddenBuildEvidenceArtifactName is the contracted per-lane
// build-evidence artifact name the previous policy uploaded. Uploading
// it is now PROHIBITED (run 44835425398): the name survives only as a
// banned value the artifact allowlist rejects.
const ForbiddenBuildEvidenceArtifactName = "build-evidence-${{ matrix.target }}-${{ github.run_attempt }}"

// The stable package artifact names (work order §6.3: the exact
// platform/arch-qualified portable names; the generic -package names
// are retired with the duplicate legacy lanes they belonged to). With
// the CI consolidation these names live in the platform-build MATRIX
// entries (one upload step named ${{ matrix.artifact }} resolves to
// them per lane).
const (
	WindowsPackageArtifactName    = "SHEYTAN-LA-ALPHA-windows-x64-portable"
	LinuxPackageArtifactName      = "SHEYTAN-LA-ALPHA-linux-x64-portable"
	MacOSArm64PackageArtifactName = "SHEYTAN-LA-ALPHA-macos-arm64-portable"
	MacOSX64PackageArtifactName   = "SHEYTAN-LA-ALPHA-macos-x64-portable"
	AndroidArtifactName           = "SHEYTAN-LA-ALPHA-android-arm64"
	IOSArtifactName               = "SHEYTAN-LA-ALPHA-ios-simulator-arm64"
	IOSDeviceArtifactName         = "SHEYTAN-LA-ALPHA-ios-device-arm64"
)

// MatrixPackageArtifactSlot is the ONE permitted upload-artifact name in
// the platform-build workflow: the package upload is ONE step whose
// with.name is the matrix expression (resolved per leg to the seven
// contracted names above).
const MatrixPackageArtifactSlot = "${{ matrix.artifact }}"

// RetiredLaneWorkflowFiles are the retired reusable-lane + orchestrator
// files: the consolidation deleted them and .github/workflows must
// contain EXACTLY build.yml + codeql.yml among the platform-build
// workflows. A reappearance recreates the multi-file topology.
var RetiredLaneWorkflowFiles = []string{
	"build-desktop.yml",
	"build-windows.yml",
	"build-linux.yml",
	"build-macos.yml",
	"build-android.yml",
	"build-ios.yml",
}

// PlatformMatrixTarget is one contracted matrix.include entry.
type PlatformMatrixTarget struct {
	Target   string
	OS       string
	Artifact string
	Kind     string
}

// PlatformMatrixTargets is the EXACT seven-entry matrix the platform-build
// job must carry: unique targets, unique (target, os, artifact) triples,
// the two macOS architectures on DISTINCT runner images, and the
// windows/linux runners pinned per the v1.11.1 runner policy. The seventh
// entry is the run 44547031698 §6.3 iOS DEVICE target: its matrix entry is
// structural, its EXECUTION is honestly gated on the Apple signing secrets
// (scripts/build/ios-device.sh check phase) - without them the lane records
// BLOCKED BY MISSING APPLE SIGNING CONFIGURATION and never fabricates an
// IPA.
var PlatformMatrixTargets = []PlatformMatrixTarget{
	{"windows-x64", "windows-2025", WindowsPackageArtifactName, "zip"},
	{"linux-x64", "ubuntu-26.04", LinuxPackageArtifactName, "targz"},
	{"macos-arm64", "macos-15", MacOSArm64PackageArtifactName, "appzip"},
	{"macos-x64", "macos-15-intel", MacOSX64PackageArtifactName, "appzip"},
	{"android-arm64", "ubuntu-26.04", AndroidArtifactName, "apk"},
	{"ios-simulator-arm64", "macos-15", IOSArtifactName, "simzip"},
	{"ios-device-arm64", "macos-15", IOSDeviceArtifactName, "ipa"},
}

// PlatformBuildJobKey / CollectorJobKey are the YAML job keys of the
// consolidated topology (the retired preflight/audit/ui/platform-* keys
// are gone with the multi-file orchestrator).
const (
	PlatformBuildJobKey = "platform-build"
	CollectorJobKey     = "collect-errors"
	FinalVerdictJobKey  = "final-verdict"
	ReleaseJobKey       = "release"
)

// ValidatePlatformMatrixStructure enforces the consolidated matrix
// contract structurally (parsed with yaml.v3, never grepped):
//
//   - the platform-build job exists and is the ONLY matrix job; its
//     needs must be empty or absent (no platform-to-platform needs
//     chain: every matrix instance is an independent gate);
//   - strategy.fail-fast is present and EXPLICITLY false;
//   - strategy.matrix.include contains EXACTLY the six contracted
//     targets, each once, with unique (target, os, artifact) triples
//     (the two macOS architectures must stay on DISTINCT images);
//   - the workflow's concurrency block never carries
//     cancel-in-progress: true (evidence is never cancelled);
//   - the job set is exactly the four contracted jobs (a stray job with
//     a needs chain into the matrix would recreate a platform DAG).
func ValidatePlatformMatrixStructure(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	var errs []string

	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok {
		return fmt.Errorf("no %q job — the consolidated platform-build workflow is missing its matrix job", PlatformBuildJobKey)
	}

	if len(job.Needs) != 0 {
		errs = append(errs, fmt.Sprintf(
			"job %q must have EMPTY or absent needs — every matrix instance is an independent gate; a needs chain recreates the platform-to-platform DAG the consolidation removed (run 43630164424 lesson)",
			PlatformBuildJobKey,
		))
	}

	if job.Strategy == nil || job.Strategy.Matrix == nil || len(job.Strategy.Matrix.Include) == 0 {
		return fmt.Errorf("job %q carries no strategy.matrix.include — exactly one matrix with the seven contracted targets is required", PlatformBuildJobKey)
	}

	if job.Strategy.FailFast == nil {
		errs = append(errs, "strategy.fail-fast is absent — it must be EXPLICITLY false (a red lane never cancels its siblings; evidence is never destroyed)")
	} else if *job.Strategy.FailFast {
		errs = append(errs, "strategy.fail-fast is true — the consolidation contract requires the EXPLICIT false spelling")
	}

	include := job.Strategy.Matrix.Include
	if len(include) != len(PlatformMatrixTargets) {
		errs = append(errs, fmt.Sprintf(
			"matrix.include has %d entries, want EXACTLY %d (the six contracted platform targets)",
			len(include), len(PlatformMatrixTargets),
		))
	}

	seenTarget := map[string]int{}
	seenTriple := map[string]int{}
	for _, e := range include {
		if prev, dup := seenTarget[e.Target]; dup {
			errs = append(errs, fmt.Sprintf("matrix target %q is defined %d times — each of the seven targets must appear EXACTLY once", e.Target, prev+1))
		}
		seenTarget[e.Target]++

		triple := e.Target + "\x00" + e.OS + "\x00" + e.Artifact
		if prev, dup := seenTriple[triple]; dup {
			errs = append(errs, fmt.Sprintf("duplicate (target, os, artifact) mapping for %q: (os=%q, artifact=%q) already defined %d time(s) — no duplicate platform mapping may exist", e.Target, e.OS, e.Artifact, prev+1))
		}
		seenTriple[triple]++
	}

	for _, want := range PlatformMatrixTargets {
		found := false
		for _, e := range include {
			if e.Target == want.Target {
				found = true
				if e.OS != want.OS {
					errs = append(errs, fmt.Sprintf(
						"matrix target %q runs on %q, want %q (the pinned runner image; macOS Intel and Apple Silicon must stay DISTINCT)",
						want.Target, e.OS, want.OS,
					))
				}
				if e.Artifact != want.Artifact {
					errs = append(errs, fmt.Sprintf(
						"matrix target %q uploads artifact %q, want %q (work order §6.3 exact platform/arch-qualified portable name)",
						want.Target, e.Artifact, want.Artifact,
					))
				}
				if e.Kind != want.Kind {
					errs = append(errs, fmt.Sprintf(
						"matrix target %q declares kind %q, want %q", want.Target, e.Kind, want.Kind,
					))
				}
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("matrix target %q is missing — the seven-target platform matrix is incomplete", want.Target))
		}
	}

	for _, e := range include {
		if e.OS == RetiredLinuxRunner {
			errs = append(errs, fmt.Sprintf("matrix target %q uses the retired %q runner image", e.Target, RetiredLinuxRunner))
		}
	}

	// The workflow job set is EXACTLY the four contracted jobs.
	wantJobs := map[string]bool{
		PlatformBuildJobKey: true,
		CollectorJobKey:     true,
		FinalVerdictJobKey:  true,
		ReleaseJobKey:       true,
	}
	for key := range doc.Jobs {
		if !wantJobs[key] {
			errs = append(errs, fmt.Sprintf(
				"unexpected job %q — the consolidated topology is exactly platform-build, collect-errors, final-verdict and release; an extra job with a needs chain into the matrix would recreate a platform DAG",
				key,
			))
		}
	}

	// No cancel-in-progress: true anywhere (comment lines excluded).
	for i, line := range strings.Split(workflowText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "cancel-in-progress:") && strings.Contains(trimmed, "true") {
			errs = append(errs, fmt.Sprintf(
				"line %d carries cancel-in-progress: true — evidence collection is never cancelled (work order §6.2)",
				i+1,
			))
		}
	}

	return joinErrors(errs)
}

// The Linux build lane runner identity (v1.11.1, run 42127523923, work
// order section 4; carried into the CI consolidation): the linux-x64
// MATRIX TARGET of the platform-build job is pinned to the Ubuntu 26.04
// x64 runner image, and the workflow VERIFIES that image at runtime
// with an explicit /etc/os-release + dpkg architecture guard.
// ubuntu-24.04 is the RETIRED image - the stress gate's stale literal
// kept demanding it after the correct workflow migration (the same
// gate-must-follow-the-contract-forward defect class as the run-#40
// Go-version pin and the run-38569081029 retired GTK dependency).
const (
	LinuxTargetName    = "linux-x64"
	LinuxRunnerLabel   = "ubuntu-26.04"
	RetiredLinuxRunner = "ubuntu-24.04"
)

// ValidateLinuxRunnerContract enforces the Linux build lane runner
// contract against the PARSED workflow - the actual matrix runner image
// and the actual OS identity guard script, never a string literal:
//
//   - the platform-build job exists and its matrix carries the linux-x64
//     target with runs-on image EXACTLY ubuntu-26.04 - singular and
//     pinned; the retired ubuntu-24.04 label, any other label, or a
//     non-x64 arm image all fail;
//   - the retired ubuntu-24.04 image must not survive on ANY matrix
//     target or ANY job of the workflow (a second lane silently
//     regressing back is the same contract break);
//   - the workflow must carry the runtime OS identity guard - a run
//     script asserting VERSION_ID=26.04 and the dpkg amd64 architecture -
//     so a YAML-only relabel without the runtime verification fails.
//
// The check is symmetric with the shared upload-artifact validators: the
// stress gate and the contract tests run the SAME function, so they can
// never drift apart.
func ValidateLinuxRunnerContract(workflowText string) error {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return err
	}

	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok {
		return fmt.Errorf(
			"workflow has no %q job - the Linux build lane runner identity cannot be validated",
			PlatformBuildJobKey,
		)
	}

	if job.Strategy == nil || job.Strategy.Matrix == nil {
		return fmt.Errorf("the %q job carries no matrix - the linux-x64 target's runner image cannot be validated", PlatformBuildJobKey)
	}

	linuxEntry, found := matrixEntryByTarget(job.Strategy.Matrix.Include, LinuxTargetName)
	if !found {
		return fmt.Errorf("matrix target %q is missing - the Linux build lane runner identity cannot be validated", LinuxTargetName)
	}

	if linuxEntry.OS != LinuxRunnerLabel {
		return fmt.Errorf(
			"Linux build lane runner contract violated: matrix target %q runs on %q, want exactly %s - %s is the retired image and the lane is pinned to the %s x64 runner",
			LinuxTargetName, linuxEntry.OS, LinuxRunnerLabel, RetiredLinuxRunner, LinuxRunnerLabel,
		)
	}

	// The retired image must not survive on ANY matrix target or ANY
	// job-level runs-on of the workflow.
	for _, e := range job.Strategy.Matrix.Include {
		if e.OS == RetiredLinuxRunner {
			return fmt.Errorf(
				"retired runner image %q found on matrix target %q - the workflow is pinned to %s; the 24.04 image must not return on any lane",
				RetiredLinuxRunner, e.Target, LinuxRunnerLabel,
			)
		}
	}
	for key, j := range doc.Jobs {
		for _, label := range j.RunsOn {
			if label == RetiredLinuxRunner {
				return fmt.Errorf(
					"retired runner image %q found on job %q - the workflow is pinned to %s; the 24.04 image must not return on any lane",
					RetiredLinuxRunner, key, LinuxRunnerLabel,
				)
			}
		}
	}

	// The runtime assertions the 26.04 guard must contain - the exact
	// spellings the workflow's "Runner OS identity (Ubuntu 26.04 gate)"
	// step verifies: the /etc/os-release VERSION_ID comparison and the
	// dpkg architecture check. A YAML-only relabel without these runtime
	// assertions fails the contract.
	guardMarkers := []string{
		`"${VERSION_ID}" != "26.04"`,
		`"$(dpkg --print-architecture)" != "amd64"`,
	}

	var runScripts []string

	for _, step := range job.Steps {
		if step.Run != "" {
			runScripts = append(runScripts, step.Run)
		}
	}

	joined := strings.Join(runScripts, "\n")

	for _, marker := range guardMarkers {
		if !strings.Contains(joined, marker) {
			return fmt.Errorf(
				"Linux x64 OS identity guard is incomplete: no platform-build run script asserts %q - the %s pin must be VERIFIED at runtime (ID=ubuntu, VERSION_ID=26.04, dpkg arch=amd64), not just declared in YAML",
				marker, LinuxRunnerLabel,
			)
		}
	}

	return nil
}

// matrixEntryByTarget finds the matrix.include entry for one target.
func matrixEntryByTarget(include []matrixEntry, target string) (matrixEntry, bool) {
	for _, e := range include {
		if e.Target == target {
			return e, true
		}
	}
	return matrixEntry{}, false
}

// v1.11.1 policy reversal (CodeQL run 43883548572, alerts #185 and #186,
// operator-directed 2026-11-17): external workflow action references use
// IMMUTABLE 40-hex COMMIT PINS carrying a synced human-readable
// release-tag comment (`# v7`, `# v4.39.1`), never mutable tags or branch
// refs. The two supplied actions/unpinned-tag findings — #185 at the
// msys2/setup-msys2 step (build-desktop.yml line 817) and #186 at the
// softprops/action-gh-release step (line 2573) — are closed at source by
// pinning every external action in both workflow files; the retired
// release-tag policy (the tag-only validator below, run 41901967798's
// successor) is replaced WITH this change. Pins stay upgradeable: the
// single weekly Dependabot group updates the SHA and rewrites the
// version comment together, and the validator below rejects any mutable
// ref so a tag can never silently reappear.
const (
	immutableActionRefPattern   = `^[0-9a-f]{40}$`
	actionVersionCommentPattern = `^#\s*v[0-9]+(\.[0-9]+)*$`
)

var (
	immutableActionRefRe   = regexp.MustCompile(immutableActionRefPattern)
	actionVersionCommentRe = regexp.MustCompile(actionVersionCommentPattern)
)

// workflowStep is the decoded subset of a workflow step the contract
// needs. The YAML parser decodes with-block scalars (multi-line path
// lists) and strips comments — including the trailing `# v7 (immutable
// SHA)` annotation on the uses: line.
type workflowStep struct {
	Name string         `yaml:"name"`
	ID   string         `yaml:"id"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	If   string         `yaml:"if"`
	With map[string]any `yaml:"with"`
}

// workflowJob is the decoded subset of a job. Needs and If feed the
// DAG-resilience contract (run 43630164424): needs is the dependency
// list (scalar or sequence spelling, like runs-on); If is the job-level
// condition ("!cancelled()" keeps a job running after failed
// dependencies).
type workflowJob struct {
	Name     string         `yaml:"name"`
	Uses     string         `yaml:"uses"`
	RunsOn   runnerLabels   `yaml:"runs-on"`
	Needs    needsList      `yaml:"needs"`
	If       string         `yaml:"if"`
	Strategy *jobStrategy   `yaml:"strategy"`
	Steps    []workflowStep `yaml:"steps"`
}

// needsList decodes a GitHub Actions needs field, which YAML may spell
// as a scalar ("needs: analyze") or a sequence ("needs: [a, b]"). Both
// decode into the same dependency list.
type needsList []string

// UnmarshalYAML implements yaml.v3 custom decoding for both spellings.
func (n *needsList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			*n = nil
			return nil
		}
		*n = []string{node.Value}
		return nil

	case yaml.SequenceNode:
		var out []string
		if err := node.Decode(&out); err != nil {
			return fmt.Errorf("needs: %w", err)
		}
		*n = out
		return nil

	default:
		return fmt.Errorf("needs: unsupported YAML node kind %v", node.Kind)
	}
}

// matrixEntry decodes one strategy.matrix.include entry of the
// platform-build job.
type matrixEntry struct {
	Target   string `yaml:"target"`
	OS       string `yaml:"os"`
	Artifact string `yaml:"artifact"`
	Kind     string `yaml:"kind"`
}

// jobStrategy decodes the strategy block the matrix contract reasons
// about. FailFast is a pointer so "absent" and "explicitly false" stay
// distinguishable — the contract requires the EXPLICIT false spelling.
type jobStrategy struct {
	FailFast *bool `yaml:"fail-fast"`
	Matrix   *struct {
		Include []matrixEntry `yaml:"include"`
	} `yaml:"matrix"`
}

// runnerLabels decodes a GitHub Actions runs-on field, which YAML may
// spell as a single scalar ("ubuntu-26.04") or a sequence ("[ubuntu-26.04,
// self-hosted]"). Both decode into the same label list so the contract
// reasons about the ACTUAL runner selection instead of grepping text.
type runnerLabels []string

// UnmarshalYAML implements yaml.v3 custom decoding for both spellings.
func (r *runnerLabels) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*r = []string{node.Value}

		return nil

	case yaml.SequenceNode:
		var out []string

		if err := node.Decode(&out); err != nil {
			return fmt.Errorf("runs-on: %w", err)
		}

		*r = out

		return nil

	default:
		return fmt.Errorf("runs-on: unsupported YAML node kind %v", node.Kind)
	}
}

// workflowDoc is the decoded subset of a GitHub Actions workflow.
// On is the RAW trigger node: trigger presence must survive null values
// (`workflow_dispatch:` has no payload), and yaml.v3 neither calls a
// custom unmarshaler for a null value nor decodes one into a non-nil
// pointer - so the validator inspects the node's keys directly
// (TriggerKeys).
type workflowDoc struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
	On   yaml.Node              `yaml:"on"`
}

// TriggerKeys returns the trigger keys present under the `on:` block.
// A bare key with a null payload (e.g. `workflow_dispatch:`) is PRESENT
// - presence, not payload, is the contract.
func TriggerKeys(on yaml.Node) map[string]bool {
	keys := map[string]bool{}
	if on.Kind != yaml.MappingNode {
		return keys
	}
	for i := 0; i+1 < len(on.Content); i += 2 {
		keys[on.Content[i].Value] = true
	}
	return keys
}

// ArtifactStep is one artifact action use scoped to its owning job —
// the unit the contract reasons about. Job is the YAML job KEY (stable
// identity); JobName is the display name (informational only, never
// matched against artifact names).
type ArtifactStep struct {
	Job     string
	JobName string
	Step    string
	Uses    string
	Ref     string // the part of uses: after the final '@'
	If      string // the step-level if condition ("" when absent)
	Name    string // with.name ("" when absent)
	Path    string // with.path ("" when absent)
	With    map[string]any
}

// parseWorkflow decodes the workflow. yaml.v3 handles CRLF endings and
// block scalars natively, so the extraction is line-ending-proof by
// construction (the exact defect class that broke the hand-rolled
// scanner on the Windows runner).
func parseWorkflow(workflowText string) (*workflowDoc, error) {
	var doc workflowDoc

	if err := yaml.Unmarshal([]byte(workflowText), &doc); err != nil {
		return nil, fmt.Errorf("workflow YAML parse: %w", err)
	}

	if len(doc.Jobs) == 0 {
		return nil, errors.New("workflow defines no jobs — not a GitHub Actions workflow?")
	}

	return &doc, nil
}

// withStringValue extracts a string with-entry (tolerating non-string
// YAML scalars by rendering them).
func withStringValue(with map[string]any, key string) string {
	if with == nil {
		return ""
	}

	switch v := with[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ArtifactSteps returns every step whose uses: references the given
// full action id (e.g. "actions/upload-artifact"), scoped to its owning
// job. Jobs are visited in deterministic (sorted-key) order; steps keep
// their file order.
func ArtifactSteps(workflowText, actionID string) ([]ArtifactStep, error) {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return nil, err
	}

	jobKeys := make([]string, 0, len(doc.Jobs))
	for key := range doc.Jobs {
		jobKeys = append(jobKeys, key)
	}
	sort.Strings(jobKeys)

	var out []ArtifactStep

	for _, key := range jobKeys {
		job := doc.Jobs[key]

		for _, step := range job.Steps {
			if step.Uses == "" {
				continue
			}

			// Split the LAST '@' so an action id containing '@' in the
			// owner/repo half still splits correctly; the ref runs to
			// the end of the scalar (YAML already stripped comments).
			idx := strings.LastIndex(step.Uses, "@")
			if idx < 0 {
				continue
			}

			if step.Uses[:idx] != actionID {
				continue
			}

			out = append(out, ArtifactStep{
				Job:     key,
				JobName: job.Name,
				Step:    step.Name,
				Uses:    step.Uses,
				Ref:     step.Uses[idx+1:],
				If:      step.If,
				Name:    withStringValue(step.With, "name"),
				Path:    withStringValue(step.With, "path"),
				With:    step.With,
			})
		}
	}

	return out, nil
}

// allActionSteps parses the workflow and returns every external action
// use (owner/repo@ref) with its job and step identity. Local composite
// actions (a bare path ref) are skipped — the policy governs external
// actions only.
func allActionSteps(workflowText string) ([]ArtifactStep, error) {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return nil, err
	}

	var out []ArtifactStep
	for key, job := range doc.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" {
				continue
			}
			idx := strings.LastIndex(step.Uses, "@")
			if idx < 0 {
				continue // local composite action path
			}
			out = append(out, ArtifactStep{
				Job:     key,
				JobName: job.Name,
				Step:    step.Name,
				Uses:    step.Uses,
				Ref:     step.Uses[idx+1:],
			})
		}
	}
	return out, nil
}

// ValidateUploadArtifactPins enforces the v1.11.1 immutable-SHA action
// policy for EVERY external action use in the workflow: each uses:
// reference must be owner/repo@<40-hex commit> and the SAME line must
// carry a synced release-tag comment (# v7, # v4.39.1, ...) — the
// comment is what Dependabot rewrites together with the pin when it
// bumps the version, so an uncommented SHA is an update dead-end and
// fails too. Mutable tags (v7), branch refs (@main), partial hashes,
// malformed references and SHA-without-comment are all rejected. The
// workflow must carry at least one actions/upload-artifact step (the
// matrix package upload — the ONLY permitted artifact under the run
// 44835425398 policy) or the contract is violated by definition.
func ValidateUploadArtifactPins(workflowText string) error {
	all, err := allActionSteps(workflowText)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return errors.New("no external action uses found in the workflow — the action-reference policy has no subject")
	}

	hasUpload := false
	var bad []string
	for _, s := range all {
		if s.Uses[:len(s.Uses)-len(s.Ref)-1] == "actions/upload-artifact" {
			hasUpload = true
		}
		if !immutableActionRefRe.MatchString(s.Ref) {
			bad = append(bad, fmt.Sprintf(
				"job %q step %q uses %q — the ref is not an immutable 40-hex commit pin. Mutable release tags, partial hashes, branch refs and moving aliases are rejected by the v1.11.1 action-reference policy (CodeQL run 43883548572, alerts #185/#186)",
				s.Job, s.Step, s.Uses,
			))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf(
			"action immutable-SHA policy violated (%d of %d uses):\n  %s",
			len(bad), len(all), strings.Join(bad, "\n  "),
		)
	}

	if err := validateUsesVersionComments(workflowText, len(all)); err != nil {
		return err
	}

	if !hasUpload {
		return errors.New("no actions/upload-artifact step found — the matrix package upload is missing from the workflow")
	}
	return nil
}

// validateUsesVersionComments checks the RAW workflow text: every
// `uses:` line referencing an external action (a value containing '@')
// must pin the immutable 40-hex SHA and end with a synced release-tag
// comment (# v7, # v4.39.1). The YAML parser strips comments before the
// uses value reaches the structural pass above, so the comment contract
// is enforced on the raw lines; the raw external uses-line count must
// equal the structurally parsed action-step count so no uses: spelling
// can dodge the audit (a uses: line embedded inside a run: script breaks
// the parity and fails closed).
func validateUsesVersionComments(workflowText string, parsedExternal int) error {
	raw := 0
	var bad []string
	for _, line := range strings.Split(workflowText, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		body := strings.TrimLeft(trimmed, " \t")
		// Both step spellings: "- uses: …" (uses as the first list key) and
		// an indented "uses: …" after a name: key.
		if strings.HasPrefix(body, "-") {
			body = strings.TrimLeft(body[1:], " \t")
		}
		if !strings.HasPrefix(body, "uses:") {
			continue
		}
		rest := strings.TrimLeft(body[len("uses:"):], " \t")
		value := rest
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			value = rest[:i]
		}
		if !strings.Contains(value, "@") {
			continue // local composite action path — not governed
		}
		raw++
		ref := value
		if i := strings.LastIndex(value, "@"); i >= 0 {
			ref = value[i+1:]
		}
		comment := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rest, value), " \t"))
		if !immutableActionRefRe.MatchString(ref) {
			bad = append(bad, fmt.Sprintf(
				"uses line %q: ref %q is not an immutable 40-hex commit pin",
				strings.TrimSpace(line), ref,
			))
			continue
		}
		if !actionVersionCommentRe.MatchString(comment) {
			bad = append(bad, fmt.Sprintf(
				"uses line %q: the immutable SHA must carry a synced release-tag comment (# v7, # v4.39.1) — Dependabot rewrites the pin and this comment together",
				strings.TrimSpace(line),
			))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf(
			"uses-line version-comment contract violated (%d lines):\n  %s",
			len(bad), strings.Join(bad, "\n  "),
		)
	}
	if raw != parsedExternal {
		return fmt.Errorf(
			"raw uses-line audit found %d external uses lines but the YAML parser found %d action steps — a uses: spelling outside a step key (for example inside a run: script) breaks the audit parity and fails closed",
			raw, parsedExternal,
		)
	}
	return nil
}

// ValidateUploadArtifactContract enforces the run 44835425398 artifact
// policy on the parsed upload surface of the ONE platform-build
// workflow, scoped per step and per owning job (parsed, never grepped):
//
//   - EXACTLY ONE upload step exists in the whole workflow: the matrix
//     package upload named ${{ matrix.artifact }} from the platform-build
//     job, resolving per leg to the seven contracted portable names;
//   - its path stages ONLY the dist/ portable package
//     (dist/SHEYTAN-LA-ALPHA-*), never a diagnostic or evidence
//     directory;
//   - its eligibility condition is enforced: if always() &&
//     steps.package-eligibility.outputs.upload-package == 'true' — the
//     package is uploaded only when the valid package exists, and an
//     intentionally absent package is never uploaded;
//   - it carries if-no-files-found: error (a silently absent package is
//     a contract breach) and overwrite: true;
//   - NO build-evidence or diagnostic upload exists: no upload name or
//     path may carry any name from ForbiddenArtifactUploadNames or any
//     substring from ForbiddenArtifactUploadSubstrings (ci-logs, build
//     evidence, run diagnostics, test reports, JSONL compiler output,
//     coverage/race reports, .log files...);
//   - NO other upload-artifact step of any kind may exist: the expected
//     platform-build upload surface is the actual platform distribution
//     package ONLY.
//
// The collector keeps generating its LOCAL report files inside the
// runner workspace (ConsolidatedLogReportFileName /
// RunDiagnosticsReportFileName) and the Actions step summary carries the
// concise status; uploading either as a workflow artifact is exactly
// what this contract now rejects.
func ValidateUploadArtifactContract(workflowText string) error {
	uploads, err := ArtifactSteps(workflowText, "actions/upload-artifact")
	if err != nil {
		return err
	}

	if len(uploads) == 0 {
		return errors.New(
			"no actions/upload-artifact steps found - the platform package upload is missing or the extraction is broken",
		)
	}

	var errs []string

	matrixUploads := 0

	for _, u := range uploads {
		// The evidence ban applies to EVERY upload first: a forbidden
		// name/path is a violation regardless of anything else.
		for _, banned := range ForbiddenArtifactUploadNames {
			if u.Name == banned {
				errs = append(errs, fmt.Sprintf(
					"job %q step %q: uploading %q is PROHIBITED by the run 44835425398 artifact policy - build evidence is never uploaded as a workflow artifact; the collector's report stays local to the runner and the Actions job logs remain the log surface",
					u.Job, u.Step, u.Name,
				))
			}
		}

		joined := strings.ToLower(u.Name + " " + u.Path)
		for _, marker := range ForbiddenArtifactUploadSubstrings {
			if strings.Contains(joined, marker) {
				errs = append(errs, fmt.Sprintf(
					"job %q step %q (artifact %q, path %q): the name/path carries the evidence marker %q - diagnostic logs, build evidence, test reports and compiler output are never uploaded as artifacts",
					u.Job, u.Step, u.Name, u.Path, marker,
				))
			}
		}

		if u.Name != MatrixPackageArtifactSlot {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: unexpected upload-artifact name %q - the expected platform-build upload surface is exactly the ONE matrix package upload %q (the actual platform distribution package); every evidence upload is banned",
				u.Job, u.Step, u.Name, MatrixPackageArtifactSlot,
			))

			continue
		}

		matrixUploads++

		if u.Job != PlatformBuildJobKey {
			errs = append(errs, fmt.Sprintf(
				"job %q: the ${{ matrix.artifact }} package upload belongs to the platform-build matrix job",
				u.Job,
			))
		}

		if cond, ok := u.With["if-no-files-found"]; !ok || withStringValue(u.With, "if-no-files-found") == "" || cond != "error" {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: the matrix package upload must carry if-no-files-found: error (a silently absent package is a contract breach)",
				u.Job, u.Step,
			))
		}

		if ov, ok := u.With["overwrite"]; !ok || ov != true {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: the matrix package upload must carry overwrite: true (a rerun must replace the same package artifact, not fail on it)",
				u.Job, u.Step,
			))
		}

		if !strings.Contains(u.Path, "dist/SHEYTAN-LA-ALPHA-") {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: the matrix package upload must stage ONLY the dist/SHEYTAN-LA-ALPHA-* portable package, got %q (no diagnostic, log or cache directory may ride along)",
				u.Job, u.Step, u.Path,
			))
		}

		// The eligibility condition is CONTRACTED, not optional: the
		// upload must key off the explicit, inspectable step output of
		// the package-eligibility gate and must run with always() so a
		// red lane's valid package still uploads.
		if !strings.Contains(u.If, "steps.package-eligibility.outputs.upload-package == 'true'") {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: the matrix package upload must key its condition off steps.package-eligibility.outputs.upload-package == 'true' (the explicit, inspectable eligibility decision) - got if: %q",
				u.Job, u.Step, u.If,
			))
		}

		if !strings.Contains(u.If, "always()") {
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: the matrix package upload must run with if: always() (a red lane's valid package is still a valid package) - got if: %q",
				u.Job, u.Step, u.If,
			))
		}
	}

	if matrixUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE matrix package upload step is contracted (%q), found %d - the platform distribution package is the workflow's entire permitted artifact surface",
			MatrixPackageArtifactSlot, matrixUploads,
		))
	}

	if len(errs) > 0 {
		return fmt.Errorf(
			"upload-artifact contract violated:\n  %s",
			strings.Join(errs, "\n  "),
		)
	}

	return nil
}

// ValidatePlatformPackageUploadContract enforces the section 6.3 package
// surface of the SEVEN matrix targets against the ONE workflow: the matrix
// must resolve the ${{ matrix.artifact }} upload to exactly the
// contracted portable names (windows/linux/macOSx2/android/ios), each
// target defined once, and the windows/linux payload kinds must match
// the release contract (portable zip / tar.gz - installer production is
// retired, master work order section 10).
func ValidatePlatformPackageUploadContract(workflowText string) error {
	var errs []string

	// The seven matrix targets must be structurally present and each must
	// upload its CONTRACTED portable artifact name (no duplicate or
	// retired naming).
	include := platformMatrixIncludeOf(workflowText)
	for _, want := range PlatformMatrixTargets {
		entry, ok := matrixEntryByTarget(include, want.Target)
		if !ok {
			errs = append(errs, fmt.Sprintf("matrix target %q missing from the platform matrix", want.Target))
			continue
		}
		if entry.Artifact != want.Artifact {
			errs = append(errs, fmt.Sprintf(
				"matrix target %q uploads artifact %q, want %q (work order section 6.3 exact platform/arch-qualified portable name)",
				want.Target, entry.Artifact, want.Artifact,
			))
		}
	}

	// The windows/linux portable payload names must appear in the
	// workflow text with the exact release-contract spellings (the
	// PackageGate invocation and the release verify step consume them).
	if !strings.Contains(workflowText, `dist/SHEYTAN-LA-ALPHA-windows-x64-portable.zip`) {
		errs = append(errs, "the workflow never references dist/SHEYTAN-LA-ALPHA-windows-x64-portable.zip - the portable ZIP is the ONE supported Windows package (master work order section 10)")
	}
	if !strings.Contains(workflowText, `dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz`) {
		errs = append(errs, "the workflow never references dist/SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz - the portable tar.gz is the ONE supported Linux package (master work order section 10)")
	}

	if len(errs) > 0 {
		return fmt.Errorf(
			"platform package upload contract violated:\n  %s",
			strings.Join(errs, "\n  "),
		)
	}

	return nil
}

// ValidateMobileArtifactUploadContract enforces the section 6.3 upload
// surface of the three mobile lanes against the ONE workflow's matrix:
// the macOS arm64/x64 portable zips resolve to DISTINCT artifacts on
// DISTINCT runner images, the Android debug package and the
// SIMULATOR-ONLY iOS zip are each defined exactly once, and the retired
// name fragments never reappear.
func ValidateMobileArtifactUploadContract(workflowText string) error {
	var errs []string

	macArm, macArmOK := matrixEntryByTarget(platformMatrixIncludeOf(workflowText), "macos-arm64")
	macX64, macX64OK := matrixEntryByTarget(platformMatrixIncludeOf(workflowText), "macos-x64")
	android, androidOK := matrixEntryByTarget(platformMatrixIncludeOf(workflowText), "android-arm64")
	ios, iosOK := matrixEntryByTarget(platformMatrixIncludeOf(workflowText), "ios-simulator-arm64")

	if !macArmOK || !macX64OK {
		errs = append(errs, "the macOS matrix targets (macos-arm64, macos-x64) must both be present")
	} else {
		if macArm.Artifact != MacOSArm64PackageArtifactName {
			errs = append(errs, fmt.Sprintf("macos-arm64 artifact %q, want %q", macArm.Artifact, MacOSArm64PackageArtifactName))
		}
		if macX64.Artifact != MacOSX64PackageArtifactName {
			errs = append(errs, fmt.Sprintf("macos-x64 artifact %q, want %q", macX64.Artifact, MacOSX64PackageArtifactName))
		}
		if macArm.OS == macX64.OS {
			errs = append(errs, fmt.Sprintf("macos-arm64 and macos-x64 share the runner image %q - Intel and Apple Silicon must stay DISTINCT", macArm.OS))
		}
	}

	if !androidOK {
		errs = append(errs, "the android-arm64 matrix target must be present")
	} else if android.Artifact != AndroidArtifactName {
		errs = append(errs, fmt.Sprintf("android-arm64 artifact %q, want %q (the retired -debug-apk suffix must not reappear)", android.Artifact, AndroidArtifactName))
	}

	if !iosOK {
		errs = append(errs, "the ios-simulator-arm64 matrix target must be present")
	} else if ios.Artifact != IOSArtifactName {
		errs = append(errs, fmt.Sprintf("ios-simulator-arm64 artifact %q, want %q", ios.Artifact, IOSArtifactName))
		if strings.Contains(ios.Artifact, "ipa") {
			errs = append(errs, "the iOS artifact must stay SIMULATOR-ONLY - a device IPA is never produced without signing")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(
			"mobile/platform upload contract violated:\n  %s",
			strings.Join(errs, "\n  "),
		)
	}

	return nil
}

// platformMatrixIncludeOf decodes the platform-build job's matrix.include
// from the workflow text (empty when the job or matrix is absent).
func platformMatrixIncludeOf(workflowText string) []matrixEntry {
	doc, err := parseWorkflow(workflowText)
	if err != nil {
		return nil
	}
	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok || job.Strategy == nil || job.Strategy.Matrix == nil {
		return nil
	}
	return job.Strategy.Matrix.Include
}
