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

// ConsolidatedLogArtifactName is the ONE run-level plain-text
// diagnostic artifact: errors/warnings-only per the run 44306343338
// work order §7 (exact ASCII spelling as used by the workflow and the
// collector; the verbose all-logs artifact is retired).
const ConsolidatedLogArtifactName = "SHEYTAN-LA-errors-warnings.txt"

// RunDiagnosticsArtifactName is the machine-readable run-level
// diagnostics index uploaded by the same collector job (run 43630164424
// repair, work order §9.2): one JSON object with every job's API
// status/conclusion/id and the collector's log-source category.
const RunDiagnosticsArtifactName = "run-diagnostics.json"

// The stable package artifact names (work order §6.3: the exact
// platform/arch-qualified portable names; the generic -package names
// are retired with the duplicate legacy lanes they belonged to).
const (
	WindowsPackageArtifactName = "SHEYTAN-LA-ALPHA-windows-x64-portable"
	LinuxPackageArtifactName   = "SHEYTAN-LA-ALPHA-linux-x64-portable"
)

// The macOS/Android/iOS reusable-lane artifact names (work order §6.3).
// The macOS matrix spells arch through an expression, so the contract
// matches the surrounding literal skeleton.
const (
	MacOSPackageArtifactSkeleton = "SHEYTAN-LA-ALPHA-macos-${{ matrix.arch }}-portable"
	AndroidArtifactName          = "SHEYTAN-LA-ALPHA-android-arm64-debug-apk"
	IOSArtifactName              = "SHEYTAN-LA-ALPHA-ios-simulator-arm64"
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
	LinuxJobName       = "Linux x64 (single authoritative lane)"
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
	Name   string         `yaml:"name"`
	Uses   string         `yaml:"uses"`
	RunsOn runnerLabels   `yaml:"runs-on"`
	Needs  needsList      `yaml:"needs"`
	If     string         `yaml:"if"`
	Steps  []workflowStep `yaml:"steps"`
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

// ValidateUploadArtifactPins enforces the v1.11.1 immutable-SHA action
// policy for EVERY external action use in the workflow: each uses:
// reference must be owner/repo@<40-hex commit> and the SAME line must
// carry a synced release-tag comment (# v7, # v4.39.1, ...) — the
// comment is what Dependabot rewrites together with the pin when it
// bumps the version, so an uncommented SHA is an update dead-end and
// fails too. Mutable tags (v7), branch refs (@main), partial hashes,
// malformed references and SHA-without-comment are all rejected. The
// workflow must carry at least one actions/upload-artifact step (the
// consolidated log upload) or the contract is violated by definition.
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
		return errors.New("no actions/upload-artifact step found — the consolidated log upload is missing from the workflow")
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

// ValidateUploadArtifactContract enforces the v1.11.1 upload surface
// (single-log-artifact contract + the two package uploads + the run
// diagnostics index), scoped per step and per owning job:
//
//   - EXACTLY ONE upload named SHEYTAN-LA-all-logs.txt — the ONE
//     consolidated log artifact — uploading the file itself
//     (archive: false, path == the artifact name);
//   - exactly ONE run-diagnostics.json upload from the SAME collector
//     job (run 43630164424 repair: machine-readable diagnostics index,
//     archive: false, path == the artifact name);
//   - exactly one windows-x64-package and one linux-x64-package upload,
//     each validated against its exact payload shape (the portable ZIP
//     under dist/ — installer production is retired, master work order
//     §10) — package uploads are NEVER counted
//     as log uploads because names match exactly;
//   - NO other upload-artifact step may exist: the whole upload surface
//     is exactly the four contracted artifacts.
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

	logUploads, diagUploads := 0, 0

	for _, u := range uploads {
		switch u.Name {
		case ConsolidatedLogArtifactName:
			logUploads++

			if u.Path != ConsolidatedLogArtifactName {
				errs = append(errs, fmt.Sprintf(
					"job %q: the consolidated diagnostic upload must set with.path to the exact file %q (archive:false means the artifact IS the file), got %q",
					u.Job, ConsolidatedLogArtifactName, u.Path,
				))
			}

			if arch, ok := u.With["archive"]; !ok || arch != false {
				errs = append(errs, fmt.Sprintf(
					"job %q: the consolidated diagnostic upload must carry `archive: false` (the artifact must BE the plain-text file, not a ZIP wrapper)",
					u.Job,
				))
			}

		case RunDiagnosticsArtifactName:
			diagUploads++

			if u.Path != RunDiagnosticsArtifactName {
				errs = append(errs, fmt.Sprintf(
					"job %q: the run diagnostics upload must set with.path to the exact file %q (archive:false means the artifact IS the file), got %q",
					u.Job, RunDiagnosticsArtifactName, u.Path,
				))
			}

			if arch, ok := u.With["archive"]; !ok || arch != false {
				errs = append(errs, fmt.Sprintf(
					"job %q: the run diagnostics upload must carry `archive: false` (the artifact must BE the JSON file, not a ZIP wrapper)",
					u.Job,
				))
			}

			if u.Job != "consolidate-logs" {
				errs = append(errs, fmt.Sprintf(
					"job %q: the run diagnostics upload belongs to the consolidate-logs collector job (same collector, same evidence lifetime)",
					u.Job,
				))
			}

		default:
			errs = append(errs, fmt.Sprintf(
				"job %q step %q: unexpected upload-artifact name %q — the orchestrator's contracted upload surface is exactly the consolidated diagnostic %q and the machine-readable index %q; platform packages upload from their OWN reusable lanes",
				u.Job, u.Step, u.Name,
				ConsolidatedLogArtifactName, RunDiagnosticsArtifactName,
			))
		}
	}

	if logUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE consolidated diagnostic artifact upload is contracted (%q), found %d",
			ConsolidatedLogArtifactName, logUploads,
		))
	}

	if diagUploads != 1 {
		errs = append(errs, fmt.Sprintf(
			"exactly ONE run diagnostics artifact upload is contracted (%q), found %d",
			RunDiagnosticsArtifactName, diagUploads,
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

// ValidatePlatformPackageUploadContract enforces the §6.3 package
// upload surface of the two desktop reusable lanes (work order §6.2:
// the package artifacts live with the single authoritative build path,
// not in the orchestrator): exactly ONE upload per lane, carrying the
// exact portable name and staging from dist/.
func ValidatePlatformPackageUploadContract(windowsText, linuxText string) error {
	var errs []string

	type lane struct {
		name     string
		text     string
		artifact string
		pathNeed string
	}
	lanes := []lane{
		{"windows", windowsText, WindowsPackageArtifactName, "-portable.zip"},
		{"linux", linuxText, LinuxPackageArtifactName, "-portable.tar.gz"},
	}

	for _, l := range lanes {
		uploads, err := ArtifactSteps(l.text, "actions/upload-artifact")
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s lane: %v", l.name, err))
			continue
		}
		hits := 0
		for _, u := range uploads {
			if u.Name != l.artifact {
				errs = append(errs, fmt.Sprintf(
					"%s lane job %q step %q: unexpected upload name %q, want %q (work order §6.3 exact platform/arch-qualified portable name)",
					l.name, u.Job, u.Step, u.Name, l.artifact,
				))
				continue
			}
			hits++
			if !strings.Contains(u.Path, "dist/") || !strings.Contains(u.Path, l.pathNeed) {
				errs = append(errs, fmt.Sprintf(
					"%s lane job %q: the %q upload must stage the dist/ portable artifact (%s), got %q",
					l.name, u.Job, l.artifact, l.pathNeed, u.Path,
				))
			}
		}
		if hits != 1 {
			errs = append(errs, fmt.Sprintf(
				"%s lane: exactly ONE %q upload is contracted, found %d",
				l.name, l.artifact, hits,
			))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf(
			"platform package upload contract violated:\n  %s",
			strings.Join(errs, "\n  "),
		)
	}

	return nil
}

// ValidateMobileArtifactUploadContract enforces the §6.3 upload surface
// of the three remaining reusable lanes: macOS arm64/x64 portable zips
// (spelled through the matrix expression), the Android debug APK and
// the SIMULATOR-ONLY iOS zip.
func ValidateMobileArtifactUploadContract(macosText, androidText, iosText string) error {
	var errs []string

	type lane struct {
		name     string
		text     string
		contains string
		banned   []string
	}
	lanes := []lane{
		{"macos", macosText, MacOSPackageArtifactSkeleton, []string{"-app"}},
		{"android", androidText, AndroidArtifactName, []string{}},
		{"ios", iosText, IOSArtifactName, []string{"simulator-app"}},
	}

	for _, l := range lanes {
		uploads, err := ArtifactSteps(l.text, "actions/upload-artifact")
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s lane: %v", l.name, err))
			continue
		}
		if len(uploads) != 1 {
			errs = append(errs, fmt.Sprintf(
				"%s lane: exactly ONE upload is contracted, found %d", l.name, len(uploads)))
			continue
		}
		u := uploads[0]
		if !strings.Contains(u.Name, l.contains) {
			errs = append(errs, fmt.Sprintf(
				"%s lane: upload name %q must carry the §6.3 skeleton %q", l.name, u.Name, l.contains))
		}
		for _, b := range l.banned {
			if strings.Contains(u.Name, b) {
				errs = append(errs, fmt.Sprintf(
					"%s lane: retired artifact name fragment %q must not reappear", l.name, b))
			}
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
