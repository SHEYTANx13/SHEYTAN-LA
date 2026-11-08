package releasecontract

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// loadDesktopWorkflow reads the real desktop workflow for the contract
// tests (same repo-relative identity the installer contract pins). The
// failure message carries the resolved ABSOLUTE path and the repo root
// so a broken checkout or loader is diagnosable on any runner instead
// of surfacing as an opaque precondition red.
func loadDesktopWorkflow(t *testing.T) string {
	t.Helper()

	root := findRepoRoot(t)
	path := filepath.Join(root, workflowRelPath)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read desktop workflow: file %s (repo root %s, relative %s): %v", path, root, workflowRelPath, err)
	}

	return string(data)
}

// TestLinuxRunnerContractAcceptsCurrentWorkflow pins the CURRENT Linux
// build lane identity (v1.11.1, run 42127523923): the parsed "Linux x64"
// job runs on exactly the pinned ubuntu-26.04 x64 image AND the workflow
// verifies that image at runtime through the OS identity guard
// (ID=ubuntu, VERSION_ID=26.04, dpkg arch=amd64). The retired 24.04
// expectation is the release_surface red — the workflow is correct, the
// stale literal was not; the contract now follows the CURRENT image
// forward and rejects a regression to either side.
func TestLinuxRunnerContractAcceptsCurrentWorkflow(t *testing.T) {
	w := loadDesktopWorkflow(t)

	if err := ValidateLinuxRunnerContract(w); err != nil {
		t.Fatalf("current desktop workflow must satisfy the Linux runner contract: %v", err)
	}
}

// TestLinuxRunnerContractRejectsRetired2404Rollback: a workflow edited
// back to ubuntu-24.04 must FAIL — the retired image is never accepted,
// neither alone nor alongside 26.04 ("accept either version" is banned).
func TestLinuxRunnerContractRejectsRetired2404Rollback(t *testing.T) {
	w := loadDesktopWorkflow(t)

	rollback := strings.ReplaceAll(w, "runs-on: ubuntu-26.04", "runs-on: ubuntu-24.04")

	err := ValidateLinuxRunnerContract(rollback)
	if err == nil {
		t.Fatal("a workflow rolled back to ubuntu-24.04 must fail the Linux runner contract")
	}

	if !strings.Contains(err.Error(), LinuxRunnerLabel) {
		t.Fatalf("failure must name the pinned %s image, got: %v", LinuxRunnerLabel, err)
	}

	// A mixed spelling (one lane moved back) is equally rejected — by the
	// Linux x64 exact-match rule or the any-lane retired-image rule.
	mixed := strings.Replace(w, "runs-on: ubuntu-26.04", "runs-on: ubuntu-24.04", 1)

	if err := ValidateLinuxRunnerContract(mixed); err == nil {
		t.Fatal("a workflow with any ubuntu-24.04 lane must fail the Linux runner contract")
	}
}

// linuxRunnerFixture is a minimal self-contained workflow with the same
// structural shape the Linux runner validator reasons about: a jobs map,
// the "Linux x64" lane pinned to the singular ubuntu-26.04 runner, the
// runtime OS identity guard the contract demands, and a second lane so
// the any-lane rules are exercisable. It is EMBEDDED in this test source:
// the negative validator tests must never depend on reading the live
// production workflow file before they can execute.
//
// Root cause this fixture fixes (run 42405993776, Windows x64 red): the
// former RejectsExtraLabels precondition grepped the real build-desktop.yml
// with a multi-line LF literal ("name: Linux x64\n    runs-on: ...");
// windows-latest materializes the unpinned .yml as CRLF (no .gitattributes
// eol pin for *.yml), the LF literal never matched, and the test failed its
// precondition while the validator itself was correct — the same
// environment-materialization defect class the workflow_contract.go
// hand-rolled-scanner failure B already documented. The fixture lives in
// .go source (*.go is pinned text eol=lf), so every literal here is
// byte-stable on every platform; the PRODUCTION workflow is only ever
// matched through the YAML parser, never raw bytes.
const linuxRunnerFixture = `name: releasecontract Linux runner fixture
on:
  workflow_dispatch: {}

jobs:
  fixture-linux:
    name: Linux x64
    runs-on: ubuntu-26.04
    timeout-minutes: 45
    env:
      APP_VERSION: "1.11.1"
    steps:
      - name: Runner OS identity (Ubuntu 26.04 gate)
        run: |
          . /etc/os-release
          if [ "${ID}" != "ubuntu" ] || [ "${VERSION_ID}" != "26.04" ]; then
            echo "::error::runner identity mismatch"
            exit 1
          fi
          if [ "$(dpkg --print-architecture)" != "amd64" ]; then
            echo "::error::architecture mismatch"
            exit 1
          fi
      - name: Build (fixture)
        run: |
          go build -o "dist/linux/app/SHEYTAN-LA" ./cmd/sheytan-local-agent

  fixture-other:
    name: Fixture other lane
    runs-on: ubuntu-26.04
    steps:
      - name: no-op
        run: echo ok
`

// linuxRunnerOtherLaneBlock is the fixture fragment identifying the
// second lane's runs-on line (used for the any-lane retired-image case).
const linuxRunnerOtherLaneBlock = "name: Fixture other lane\n    runs-on: ubuntu-26.04\n"

// TestLinuxRunnerContractRejectsExtraLabels: the singular pinned label
// contract is a VALIDATOR property, exercised against the embedded
// fixture — a composite runner selector (26.04 plus an extra label, e.g.
// a self-hosted or non-x64 composite) violates the singular pinned label.
// Production conformance is asserted separately, through the YAML parser,
// by TestBuildDesktopWorkflowLinuxX64Conformance.
func TestLinuxRunnerContractRejectsExtraLabels(t *testing.T) {
	// The valid fixture must be ACCEPTED — both as committed (LF) and as
	// a Windows runner would materialize equivalent text (CRLF). This is
	// the line-ending regression case: the validator is decoding YAML, so
	// the two spellings must be indistinguishable to it.
	if err := ValidateLinuxRunnerContract(linuxRunnerFixture); err != nil {
		t.Fatalf("the valid fixture must satisfy the Linux runner contract: %v", err)
	}

	crlfFixture := strings.ReplaceAll(linuxRunnerFixture, "\n", "\r\n")
	if err := ValidateLinuxRunnerContract(crlfFixture); err != nil {
		t.Fatalf("the valid fixture must satisfy the Linux runner contract under CRLF materialization: %v", err)
	}

	// Mutations target the Linux lane's own runs-on line via the fixture's
	// LF-stable anchor (embedded .go source is eol=lf by .gitattributes).
	mutate := func(replacement string) string {
		return strings.Replace(
			linuxRunnerFixture,
			"name: Linux x64\n    runs-on: ubuntu-26.04\n",
			replacement,
			1,
		)
	}

	composite := mutate("name: Linux x64\n    runs-on: [ubuntu-26.04, self-hosted]\n")

	err := ValidateLinuxRunnerContract(composite)
	if err == nil {
		t.Fatal("a composite (multi-label) Linux x64 runner must fail the singular-label contract")
	}

	if !strings.Contains(err.Error(), LinuxRunnerLabel) {
		t.Fatalf("failure must name the pinned %s image, got: %v", LinuxRunnerLabel, err)
	}

	// Order-insensitive composite rejection and the remaining malformed
	// runner selectors — each must fail for the contract reason.
	cases := []struct {
		name string
		repl string
	}{
		{"reversed composite [self-hosted, ubuntu-26.04]", "name: Linux x64\n    runs-on: [self-hosted, ubuntu-26.04]\n"},
		{"duplicate labels [ubuntu-26.04, ubuntu-26.04]", "name: Linux x64\n    runs-on: [ubuntu-26.04, ubuntu-26.04]\n"},
		{"retired 24.04 image", "name: Linux x64\n    runs-on: ubuntu-24.04\n"},
		{"non-x64 arm image", "name: Linux x64\n    runs-on: ubuntu-26.04-arm\n"},
		{"missing runs-on", "name: Linux x64\n"},
		{"empty runs-on", "name: Linux x64\n    runs-on: \"\"\n"},
		{"mapping-form runs-on (unsupported node kind)", "name: Linux x64\n    runs-on:\n      group: x64\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLinuxRunnerContract(mutate(tc.repl)); err == nil {
				t.Fatalf("mutation %q must fail the Linux runner contract", tc.name)
			}
		})
	}

	// A retired label on ANY OTHER lane is equally rejected (the any-lane
	// rule), even while the Linux x64 job itself stays exactly correct.
	otherLane := strings.Replace(
		linuxRunnerFixture,
		linuxRunnerOtherLaneBlock,
		"name: Fixture other lane\n    runs-on: ubuntu-24.04\n",
		1,
	)
	if err := ValidateLinuxRunnerContract(otherLane); err == nil {
		t.Fatal("a retired ubuntu-24.04 label on any lane must fail the Linux runner contract")
	}

	// Losing the Linux x64 job display name removes the lane identity.
	missing := strings.ReplaceAll(linuxRunnerFixture, "name: Linux x64", "name: Linux lane")
	if err := ValidateLinuxRunnerContract(missing); err == nil {
		t.Fatal("a workflow without the Linux x64 job must fail the runner contract")
	}

	// Malformed YAML is rejected, never tolerated.
	malformed := "jobs:\n  fixture-linux:\n    name: [unclosed\n"
	if _, err := parseWorkflow(malformed); err == nil {
		t.Fatal("malformed workflow YAML must fail to parse")
	}
}

// TestLinuxRunnerContractRejectsMissingOSGuard: relabeling the YAML to
// 26.04 while dropping the runtime identity guard fails — the pin must
// be VERIFIED on the runner (VERSION_ID + dpkg arch), not just declared.
func TestLinuxRunnerContractRejectsMissingOSGuard(t *testing.T) {
	w := loadDesktopWorkflow(t)

	if !strings.Contains(w, `"${VERSION_ID}" != "26.04"`) {
		t.Fatalf("fixture precondition: the current workflow carries the VERSION_ID 26.04 guard")
	}

	// Removing the guard means removing the runtime ASSERTIONS (the
	// os-release identity check and the dpkg architecture check), not
	// renaming them.
	unguarded := strings.ReplaceAll(
		w,
		`if [ "${ID}" != "ubuntu" ] || [ "${VERSION_ID}" != "26.04" ]; then`,
		`if false; then`,
	)

	if err := ValidateLinuxRunnerContract(unguarded); err == nil {
		t.Fatal("a workflow without the runtime OS identity guard must fail the runner contract")
	}

	// Dropping ONLY the architecture assertion must also fail.
	noArch := strings.ReplaceAll(
		w,
		`if [ "$(dpkg --print-architecture)" != "amd64" ]; then`,
		`if false; then`,
	)

	if err := ValidateLinuxRunnerContract(noArch); err == nil {
		t.Fatal("a workflow without the amd64 architecture assertion must fail the runner contract")
	}
}

// TestLinuxRunnerContractRejectsMissingLinuxJob: a workflow that loses
// the "Linux x64" job cannot satisfy the lane identity at all.
func TestLinuxRunnerContractRejectsMissingLinuxJob(t *testing.T) {
	w := loadDesktopWorkflow(t)

	missing := strings.ReplaceAll(w, "name: Linux x64", "name: Linux lane")

	if err := ValidateLinuxRunnerContract(missing); err == nil {
		t.Fatal("a workflow without the Linux x64 job must fail the runner contract")
	}
}

// TestBuildDesktopWorkflowLinuxX64Conformance pins the PRODUCTION
// workflow (the real .github/workflows/build-desktop.yml) to the Linux
// runner contract through the YAML parser — never through raw bytes.
// The same file materializes as LF on Linux and CRLF on windows-latest
// (unpinned .yml plus the runner's autocrlf default), so byte-level
// matching here would re-introduce the run-42405993776 Windows red by
// construction.
//
// The Linux x64 lane is located by display name over the real jobs map;
// its YAML job key is DISCOVERED, never assumed. The lane must be the
// true Linux x64 package job — verified by what it DOES (build the
// desktop binary from the canonical cmd target into the dist/linux/app
// staging tree, produce the portable -linux-x64.zip archive, upload the
// linux-x64-package artifact from dist/), never by display name alone.
// Any runner or package drift in the production workflow fails here.
func TestBuildDesktopWorkflowLinuxX64Conformance(t *testing.T) {
	w := loadDesktopWorkflow(t)

	doc, err := parseWorkflow(w)
	if err != nil {
		t.Fatalf("parse %s: %v", workflowRelPath, err)
	}

	jobKeys := make([]string, 0, len(doc.Jobs))
	for key := range doc.Jobs {
		jobKeys = append(jobKeys, key)
	}
	sort.Strings(jobKeys)

	linuxKey := ""
	linuxJob := workflowJob{}

	for _, key := range jobKeys {
		if doc.Jobs[key].Name == LinuxJobName {
			linuxKey = key
			linuxJob = doc.Jobs[key]

			break
		}
	}

	if linuxKey == "" {
		t.Fatalf(
			"production workflow drift: no job carries display name %q — jobs present: %v",
			LinuxJobName, jobKeys,
		)
	}

	// Typed exact runner identity: exactly ONE decoded label and it must
	// be the pinned image. A runner array (any composite), the retired
	// 24.04 image, a non-x64 label, or a missing/empty runs-on all fail —
	// this is a typed structural assertion, not a substring match.
	if len(linuxJob.RunsOn) != 1 || linuxJob.RunsOn[0] != LinuxRunnerLabel {
		t.Fatalf(
			"production workflow drift: job %q (YAML key %q) runs-on is %v, want exactly [%s]",
			LinuxJobName, linuxKey, []string(linuxJob.RunsOn), LinuxRunnerLabel,
		)
	}

	// Package-job identity from the steps, never the display name.
	var buildsBinary, buildsPortableZip bool

	for _, step := range linuxJob.Steps {
		if strings.Contains(step.Run, "./cmd/sheytan-local-agent") &&
			strings.Contains(step.Run, "dist/linux/app") {
			buildsBinary = true
		}

		if strings.Contains(step.Run, "-linux-x64.zip") {
			buildsPortableZip = true
		}
	}

	if !buildsBinary {
		t.Fatalf(
			"production workflow drift: job %q (YAML key %q) does not build the desktop binary from ./cmd/sheytan-local-agent into dist/linux/app — the pinned runner identity would guard a job that no longer builds the Linux x64 package",
			LinuxJobName, linuxKey,
		)
	}

	if !buildsPortableZip {
		t.Fatalf(
			"production workflow drift: job %q (YAML key %q) does not produce the portable -linux-x64.zip archive",
			LinuxJobName, linuxKey,
		)
	}

	uploads, err := ArtifactSteps(w, "actions/upload-artifact")
	if err != nil {
		t.Fatalf("extract upload-artifact steps from %s: %v", workflowRelPath, err)
	}

	uploadsPackage := false

	for _, u := range uploads {
		if u.Job == linuxKey && u.Name == LinuxPackageArtifactName &&
			strings.Contains(u.Path, "dist/") && strings.Contains(u.Path, "-linux-x64.zip") {
			uploadsPackage = true

			break
		}
	}

	if !uploadsPackage {
		t.Fatalf(
			"production workflow drift: job %q (YAML key %q) does not upload the %q package artifact from dist/ — not the Linux x64 package job",
			LinuxJobName, linuxKey, LinuxPackageArtifactName,
		)
	}

	// The shared validator must ACCEPT the production workflow: the
	// runner pin AND the runtime OS identity guard, checked by the ONE
	// function the stress gate also runs (gate and tests cannot drift).
	if err := ValidateLinuxRunnerContract(w); err != nil {
		t.Fatalf("current desktop workflow must satisfy the Linux runner contract: %v", err)
	}
}
