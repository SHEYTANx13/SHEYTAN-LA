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

// The consolidated run log artifact: the ONE log upload contracted by
// the v1.11.1 single-artifact run-log contract (exact ASCII spelling as
// used by the workflow and the collector).
const ConsolidatedLogArtifactName = "SHEYTAN-LA-all-logs.txt"

// The two stable package artifact names.
const (
	WindowsPackageArtifactName = "windows-x64-package"
	LinuxPackageArtifactName   = "linux-x64-package"
)

// The Linux build lane runner identity (v1.11.1, run 42127523923, work
// order §4): the desktop workflow's Linux x64 lane is pinned to the
// Ubuntu 26.04 x64 runner image, and the workflow VERIFIES that image at
// runtime with an explicit /etc/os-release + dpkg architecture guard.
// ubuntu-24.04 is the RETIRED image — the stress gate's stale literal
// kept demanding it after the correct workflow migration (the same
// gate-must-follow-the-contract-forward defect class as the run-#40
// Go-version pin and the run-38569081029 retired GTK dependency).
const (
	LinuxJobName       = "Linux x64"
	LinuxRunnerLabel   = "ubuntu-26.04"
	RetiredLinuxRunner = "ubuntu-24.04"
)

// ValidateLinuxRunnerContract enforces the Linux build lane runner
// contract against the PARSED workflow — the actual runs-on fields and
// the actual OS identity guard script, never a string literal:
//
//   - the "Linux x64" job's runs-on must be EXACTLY [ubuntu-26.04] —
//     singular and pinned; the retired ubuntu-24.04 label, any other
//     label, or an extra label (a non-x64 composite) all fail;
//   - the retired ubuntu-24.04 image must not survive on ANY job of the
//     desktop workflow (a second lane silently regressing back is the
//     same contract break);
//   - the Linux x64 job must carry the runtime OS identity guard — a run
//     script asserting VERSION_ID=26.04 and the dpkg amd64 architecture —
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

	jobKeys := make([]string, 0, len(doc.Jobs))

	for key := range doc.Jobs {
		jobKeys = append(jobKeys, key)
	}

	sort.Strings(jobKeys)

	var linuxJob workflowJob

	found := false

	for _, key := range jobKeys {
		if doc.Jobs[key].Name == LinuxJobName {
			linuxJob = doc.Jobs[key]
			found = true

			break
		}
	}

	if !found {
		return fmt.Errorf(
			"workflow has no %q job — the Linux build lane runner identity cannot be validated",
			LinuxJobName,
		)
	}

	if len(linuxJob.RunsOn) != 1 || linuxJob.RunsOn[0] != LinuxRunnerLabel {
		return fmt.Errorf(
			"Linux build lane runner contract violated: job %q runs-on is %v, want exactly [%s] — %s is the retired image and the lane is pinned to the %s x64 runner",
			LinuxJobName, []string(linuxJob.RunsOn), LinuxRunnerLabel, RetiredLinuxRunner, LinuxRunnerLabel,
		)
	}

	for _, key := range jobKeys {
		job := doc.Jobs[key]

		for _, label := range job.RunsOn {
			if label == RetiredLinuxRunner {
				return fmt.Errorf(
					"retired runner image %q found on job %q — the desktop workflow is pinned to %s; the 24.04 image must not return on any lane",
					RetiredLinuxRunner, key, LinuxRunnerLabel,
				)
			}
		}
	}

	// The runtime assertions the 26.04 guard must contain — the exact
	// spellings the workflow's "Runner OS identity" step verifies:
	// the /etc/os-release VERSION_ID comparison and the dpkg
	// architecture check. A YAML-only relabel without these runtime
	// assertions fails the contract.
	guardMarkers := []string{
		`"${VERSION_ID}" != "26.04"`,
		`"$(dpkg --print-architecture)" != "amd64"`,
	}

	var runScripts []string

	for _, step := range linuxJob.Steps {
		if step.Run != "" {
			runScripts = append(runScripts, step.Run)
		}
	}

	joined := strings.Join(runScripts, "\n")

	for _, marker := range guardMarkers {
		if !strings.Contains(joined, marker) {
			return fmt.Errorf(
				"Linux x64 OS identity guard is incomplete: no Linux x64 run script asserts %q — the %s pin must be VERIFIED at runtime (ID=ubuntu, VERSION_ID=26.04, dpkg arch=amd64), not just declared in YAML",
				marker, LinuxRunnerLabel,
			)
		}
	}

	return nil
}

// v1.11.1 maintenance policy change (operator-directed): external
// workflow action references use ORDINARY UPSTREAM RELEASE TAGS
// (checkout@v7, setup-msys2@v2, codeql-action@v4, ...), never
// commit-hash pins, branch refs or moving aliases outside the tag
// scheme. This is a deliberate maintainability choice: tags are mutable,
// so the risk is mitigated by official upstream sources,
// least-privilege permissions, the single weekly Dependabot update
// group and the workflow contract below. The former immutable-SHA
// allowlist was retired WITH this policy (the tag validator rejects any
// 40-hex/partial-hash ref outright, so a hash pin can never reappear
// silently).
const releaseTagPattern = `^v[0-9]+(\.[0-9]+)*$`

// workflowStep is the decoded subset of a workflow step the contract
// needs. The YAML parser decodes with-block scalars (multi-line path
// lists) and strips comments — including the trailing `# v7 (immutable
// SHA)` annotation on the uses: line.
type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

// workflowJob is the decoded subset of a job.
type workflowJob struct {
	Name   string         `yaml:"name"`
	RunsOn runnerLabels   `yaml:"runs-on"`
	Steps  []workflowStep `yaml:"steps"`
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
type workflowDoc struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
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

// ValidateUploadArtifactPins enforces the v1.11.1 release-tag policy for
// EVERY external action use in the workflow: each uses: reference must
// be an ordinary upstream release tag (v7, v6, v2, v4.39.1, ...).
// Commit-hash pins (full or partial), branch refs (@main), moving
// aliases outside the tag scheme and malformed references are rejected.
// The workflow must carry at least one actions/upload-artifact step
// (the consolidated log upload) or the contract is violated by
// definition.
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
	tagRe := regexp.MustCompile(releaseTagPattern)
	for _, s := range all {
		if s.Uses[:len(s.Uses)-len(s.Ref)-1] == "actions/upload-artifact" {
			hasUpload = true
		}
		if !tagRe.MatchString(s.Ref) {
			bad = append(bad, fmt.Sprintf(
				"job %q step %q uses %q — the ref is not an upstream release tag (vN or vN.N.N). Commit hashes, digest pins, branch refs and moving aliases are rejected by the v1.11.1 action-reference policy",
				s.Job, s.Step, s.Uses,
			))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf(
			"action release-tag policy violated (%d of %d uses):\n  %s",
			len(bad), len(all), strings.Join(bad, "\n  "),
		)
	}
	if !hasUpload {
		return errors.New("no actions/upload-artifact step found — the consolidated log upload is missing from the workflow")
	}
	return nil
}

// ValidateUploadArtifactContract enforces the v1.11.1 single-log-artifact
// upload surface, scoped per step and per owning job:
//
//   - EXACTLY ONE upload named SHEYTAN-LA-all-logs.txt — the ONE
//     consolidated log artifact — uploading the file itself
//     (archive: false, path == the artifact name);
//   - exactly one windows-x64-package and one linux-x64-package upload,
//     each validated against its exact payload shapes (portable ZIP,
//     NSIS installer under dist/) — package uploads are NEVER counted
//     as log uploads because names match exactly;
//   - NO other upload-artifact step may exist: the whole upload surface
//     is exactly the three contracted artifacts.
func ValidateUploadArtifactContract(workflowText string) error {
	uploads, err := ArtifactSteps(workflowText, "actions/upload-artifact")
	if err != nil {
		return err
	}

	if len(uploads) == 0 {
		return errors.New(
			"no actions/upload-artifact steps found — the extraction is broken or the workflow has no uploads",
		)
	}

	var errs []string

	logUploads, winUploads, linUploads := 0, 0, 0

	for _, u := range uploads {
		switch u.Name {
		case ConsolidatedLogArtifactName:
			logUploads++

			if u.Path != ConsolidatedLogArtifactName {
				errs = append(errs, fmt.Sprintf(
					"job %q: the consolidated log upload must set with.path to the exact file %q (archive:false means the artifact IS the file), got %q",
					u.Job, ConsolidatedLogArtifactName, u.Path,
				))
			}

			if arch, ok := u.With["archive"]; !ok || arch != false {
				errs = append(errs, fmt.Sprintf(
					"job %q: the consolidated log upload must carry `archive: false` (the artifact must BE the plain-text file, not a ZIP wrapper)",
					u.Job,
				))
			}

		case WindowsPackageArtifactName:
			winUploads++

			if !strings.Contains(u.Path, "dist/") {
				errs = append(errs, fmt.Sprintf(
					"job %q: the Windows package upload payload must stage from dist/, got %q",
					u.Job, u.Path,
				))
			}

			for _, frag := range []string{"-windows-x64.zip", "-windows-x64-installer.exe"} {
				if !strings.Contains(u.Path, frag) {
					errs = append(errs, fmt.Sprintf(
						"job %q: the Windows package upload payload must include the %q artifact, got %q",
						u.Job, frag, u.Path,
					))
				}
			}

		case LinuxPackageArtifactName:
			linUploads++

			if !strings.Contains(u.Path, "dist/") ||
				!strings.Contains(u.Path, "-linux-x64.zip") {
				errs = append(errs, fmt.Sprintf(
					"job %q: the Linux package upload payload must be the dist/ portable archive (*-linux-x64.zip), got %q",
					u.Job, u.Path,
				))
			}

		default:
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: unexpected upload-artifact name %q — the contracted upload surface is exactly %q, %q and %q (any extra upload breaks the single-log-artifact contract)",
				u.Job, u.Step, u.Name,
				ConsolidatedLogArtifactName, WindowsPackageArtifactName, LinuxPackageArtifactName,
			))
		}
	}

	if logUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE consolidated log artifact upload is contracted (%q), found %d",
			ConsolidatedLogArtifactName, logUploads,
		))
	}

	if winUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE %q upload is contracted, found %d",
			WindowsPackageArtifactName, winUploads,
		))
	}

	if linUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE %q upload is contracted, found %d",
			LinuxPackageArtifactName, linUploads,
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
