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

// UploadArtifactV7SHA is the official actions/upload-artifact v7.0.2
// release commit — the verified immutable pin the desktop workflow uses
// (verified: the tag v7.0.2 of actions/upload-artifact points at this
// commit; archive:false semantics were checked against this revision's
// action.yml).
const UploadArtifactV7SHA = "cf430e030ddbb5b0abf93d22962f4752f3646cd9"

// uploadArtifactAllowlist is the reviewed immutable-ref allowlist for
// actions/upload-artifact. The pin policy is strict: ONLY a verified,
// full-length commit SHA from this list satisfies the gate. Moving
// major tags (@v7), branch names (@main), short/partial hashes,
// malformed references and any unapproved SHA are rejected.
var uploadArtifactAllowlist = map[string]bool{
	UploadArtifactV7SHA: true,
}

// workflowStep is the decoded subset of a workflow step the contract
// needs. The YAML parser decodes with-block scalars (multi-line path
// lists) and strips comments — including the trailing `# v7 (immutable
// SHA)` annotation on the uses: line.
type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
}

// workflowJob is the decoded subset of a job.
type workflowJob struct {
	Name  string         `yaml:"name"`
	Steps []workflowStep `yaml:"steps"`
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

// ValidateUploadArtifactPins enforces the immutable-SHA pin policy for
// EVERY actions/upload-artifact use in the workflow: each reference
// must be a reviewed full-length commit SHA from the allowlist. The
// workflow must carry at least one such step (the consolidated log
// upload) or the contract is violated by definition.
func ValidateUploadArtifactPins(workflowText string) error {
	steps, err := ArtifactSteps(workflowText, "actions/upload-artifact")
	if err != nil {
		return err
	}

	if len(steps) == 0 {
		return errors.New(
			"no actions/upload-artifact step found — the consolidated log upload is missing from the workflow",
		)
	}

	var bad []string

	for _, s := range steps {
		if !uploadArtifactAllowlist[s.Ref] {
			bad = append(bad, fmt.Sprintf(
				"job %q step %q uses %q — not the reviewed immutable upload-artifact v7.0.2 commit SHA %s (moving tags, branch names, partial hashes and unapproved SHAs are rejected)",
				s.Job, s.Step, s.Uses, UploadArtifactV7SHA,
			))
		}
	}

	if len(bad) > 0 {
		return fmt.Errorf(
			"upload-artifact pin policy violated (%d of %d steps unapproved):\n  %s",
			len(bad), len(steps), strings.Join(bad, "\n  "),
		)
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
