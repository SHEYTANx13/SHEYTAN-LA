package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadPlatformBuildWorkflow reads the real consolidated platform-build
// workflow for the contract tests (same repo-relative identity the
// installer contract pins). The failure message carries the resolved
// ABSOLUTE path and the repo root so a broken checkout or loader is
// diagnosable on any runner instead of surfacing as an opaque
// precondition red.
func loadPlatformBuildWorkflow(t *testing.T) string {
	t.Helper()

	root := findRepoRoot(t)
	path := filepath.Join(root, PlatformBuildWorkflowPath)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read platform-build workflow: file %s (repo root %s, relative %s): %v", path, root, PlatformBuildWorkflowPath, err)
	}

	return string(data)
}

// TestLinuxRunnerContractAcceptsCurrentWorkflow pins the CURRENT Linux
// build lane identity (v1.11.1, run 42127523923; carried into the CI
// consolidation): the linux-x64 MATRIX TARGET of the platform-build job
// runs on exactly the pinned ubuntu-26.04 x64 image AND the workflow
// verifies that image at runtime through the OS identity guard
// (ID=ubuntu, VERSION_ID=26.04, dpkg arch=amd64). The retired 24.04
// expectation is the release_surface red — the workflow is correct, the
// stale literal was not; the contract now follows the CURRENT image
// forward and rejects a regression to either side.
func TestLinuxRunnerContractAcceptsCurrentWorkflow(t *testing.T) {
	w := loadPlatformBuildWorkflow(t)

	if err := ValidateLinuxRunnerContract(w); err != nil {
		t.Fatalf("current platform-build workflow must satisfy the Linux runner contract: %v", err)
	}
}

// TestLinuxRunnerContractRejectsRetired2404Rollback: a workflow edited
// back to ubuntu-24.04 must FAIL — the retired image is never accepted,
// neither alone nor alongside 26.04 ("accept either version" is banned).
func TestLinuxRunnerContractRejectsRetired2404Rollback(t *testing.T) {
	w := loadPlatformBuildWorkflow(t)

	rollback := strings.ReplaceAll(w, "os: ubuntu-26.04", "os: ubuntu-24.04")

	err := ValidateLinuxRunnerContract(rollback)
	if err == nil {
		t.Fatal("a workflow rolled back to ubuntu-24.04 must fail the Linux runner contract")
	}

	if !strings.Contains(err.Error(), LinuxRunnerLabel) {
		t.Fatalf("failure must name the pinned %s image, got: %v", LinuxRunnerLabel, err)
	}

	// A mixed spelling (one matrix target moved back) is equally
	// rejected — by the linux-x64 exact-match rule or the any-target
	// retired-image rule.
	mixed := strings.Replace(w, "os: ubuntu-26.04", "os: ubuntu-24.04", 1)

	if err := ValidateLinuxRunnerContract(mixed); err == nil {
		t.Fatal("a workflow with any ubuntu-24.04 matrix target must fail the Linux runner contract")
	}
}

// linuxRunnerFixture is a minimal self-contained workflow with the same
// structural shape the Linux runner validator reasons about: a jobs map,
// the platform-build job whose matrix carries the linux-x64 target on
// the singular ubuntu-26.04 runner, the runtime OS identity guard the
// contract demands, and a second matrix target so the any-target rules
// are exercisable. It is EMBEDDED in this test source: the negative
// validator tests must never depend on reading the live production
// workflow file before they can execute.
//
// Root cause this fixture shape fixes (run 42405993776, Windows x64
// red): the former precondition grepped the real workflow with a
// multi-line LF literal; windows-latest materializes the unpinned .yml
// as CRLF (no .gitattributes eol pin for *.yml), the LF literal never
// matched, and the test failed its precondition while the validator
// itself was correct. The fixture lives in .go source (*.go is pinned
// text eol=lf), so every literal here is byte-stable on every platform;
// the PRODUCTION workflow is only ever matched through the YAML parser,
// never raw bytes.
const linuxRunnerFixture = `name: releasecontract Linux runner fixture
on:
  workflow_dispatch: {}

jobs:
  platform-build:
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - target: linux-x64
            os: ubuntu-26.04
            artifact: SHEYTAN-LA-ALPHA-linux-x64-portable
            kind: targz
          - target: other-x64
            os: ubuntu-26.04
            artifact: SHEYTAN-LA-ALPHA-other-x64-portable
            kind: zip
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
`

// linuxRunnerOtherTargetBlock is the fixture fragment identifying the
// second target's os line (used for the any-target retired-image case).
const linuxRunnerOtherTargetBlock = "- target: other-x64\n            os: ubuntu-26.04\n"

// TestLinuxRunnerContractRejectsBadRunnerShapes: the singular pinned
// image contract is a VALIDATOR property, exercised against the embedded
// fixture — the linux-x64 matrix target's os must be EXACTLY the pinned
// image (the retired 24.04 image, a non-x64 arm image, a missing or
// empty os all fail). Production conformance is asserted separately,
// through the YAML parser, by
// TestPlatformBuildWorkflowLinuxX64Conformance.
func TestLinuxRunnerContractRejectsBadRunnerShapes(t *testing.T) {
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

	// Mutations target the linux-x64 target's own os line via the
	// fixture's LF-stable anchor (embedded .go source is eol=lf by
	// .gitattributes).
	mutate := func(replacement string) string {
		return strings.Replace(
			linuxRunnerFixture,
			"- target: linux-x64\n            os: ubuntu-26.04\n",
			replacement,
			1,
		)
	}

	cases := []struct {
		name string
		repl string
	}{
		{"retired 24.04 image", "- target: linux-x64\n            os: ubuntu-24.04\n"},
		{"non-x64 arm image", "- target: linux-x64\n            os: ubuntu-26.04-arm\n"},
		{"missing os", "- target: linux-x64\n"},
		{"empty os", "- target: linux-x64\n            os: \"\"\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateLinuxRunnerContract(mutate(tc.repl)); err == nil {
				t.Fatalf("mutation %q must fail the Linux runner contract", tc.name)
			}
		})
	}

	// A retired label on ANY OTHER matrix target is equally rejected (the
	// any-target rule), even while the linux-x64 target itself stays
	// exactly correct.
	otherTarget := strings.Replace(
		linuxRunnerFixture,
		linuxRunnerOtherTargetBlock,
		"- target: other-x64\n            os: ubuntu-24.04\n",
		1,
	)
	if err := ValidateLinuxRunnerContract(otherTarget); err == nil {
		t.Fatal("a retired ubuntu-24.04 label on any matrix target must fail the Linux runner contract")
	}

	// Losing the linux-x64 target removes the lane identity.
	missing := strings.ReplaceAll(linuxRunnerFixture, "target: linux-x64", "target: linux-lane")
	if err := ValidateLinuxRunnerContract(missing); err == nil {
		t.Fatal("a workflow without the linux-x64 matrix target must fail the runner contract")
	}

	// Malformed YAML is rejected, never tolerated.
	malformed := "jobs:\n  platform-build:\n    strategy: [unclosed\n"
	if _, err := parseWorkflow(malformed); err == nil {
		t.Fatal("malformed workflow YAML must fail to parse")
	}
}

// TestLinuxRunnerContractRejectsMissingOSGuard: relabeling the YAML to
// 26.04 while dropping the runtime identity guard fails — the pin must
// be VERIFIED on the runner (VERSION_ID + dpkg arch), not just declared.
func TestLinuxRunnerContractRejectsMissingOSGuard(t *testing.T) {
	w := loadPlatformBuildWorkflow(t)

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

// TestLinuxRunnerContractRejectsMissingLinuxTarget: a workflow that
// loses the linux-x64 matrix target cannot satisfy the lane identity at
// all.
func TestLinuxRunnerContractRejectsMissingLinuxTarget(t *testing.T) {
	w := loadPlatformBuildWorkflow(t)

	missing := strings.ReplaceAll(w, "target: linux-x64", "target: linux-lane")

	if err := ValidateLinuxRunnerContract(missing); err == nil {
		t.Fatal("a workflow without the linux-x64 matrix target must fail the runner contract")
	}
}

// TestPlatformBuildWorkflowLinuxX64Conformance pins the PRODUCTION
// workflow (the real .github/workflows/build.yml) to the Linux runner
// contract through the YAML parser — never through raw bytes. The same
// file materializes as LF on Linux and CRLF on windows-latest (unpinned
// .yml plus the runner's autocrlf default), so byte-level matching here
// would re-introduce the run-42405993776 Windows red by construction.
//
// The linux-x64 lane is located through the parsed MATRIX; its runner
// image must be the singular pinned ubuntu-26.04. The lane must be the
// true Linux x64 package lane — verified by what it DOES (build the
// desktop binary from the canonical cmd target into the dist/linux/app
// staging tree via scripts/build/linux.sh, produce and verify the
// portable .tar.gz archive), never by a label alone. Any runner or
// package drift in the production workflow fails here.
func TestPlatformBuildWorkflowLinuxX64Conformance(t *testing.T) {
	w := loadPlatformBuildWorkflow(t)

	doc, err := parseWorkflow(w)
	if err != nil {
		t.Fatalf("parse %s: %v", PlatformBuildWorkflowPath, err)
	}

	job, ok := doc.Jobs[PlatformBuildJobKey]
	if !ok {
		t.Fatalf("production workflow drift: no %q job — jobs present: %v", PlatformBuildJobKey, doc.Jobs)
	}

	if job.Strategy == nil || job.Strategy.Matrix == nil {
		t.Fatalf("production workflow drift: job %q carries no matrix", PlatformBuildJobKey)
	}

	entry, found := matrixEntryByTarget(job.Strategy.Matrix.Include, LinuxTargetName)
	if !found {
		t.Fatalf("production workflow drift: no linux-x64 matrix target — include: %+v", job.Strategy.Matrix.Include)
	}

	// Typed exact runner identity: the pinned image (a composite label
	// list is impossible at the matrix level, but the retired 24.04
	// image, a non-x64 arm label, or a missing/empty os all fail — this
	// is a typed structural assertion, not a substring match).
	if entry.OS != LinuxRunnerLabel {
		t.Fatalf(
			"production workflow drift: matrix target %q runs on %q, want exactly %s",
			LinuxTargetName, entry.OS, LinuxRunnerLabel,
		)
	}

	// Package-lane identity from the dispatched script content, never
	// from a label. scripts/build/linux.sh must build the desktop binary
	// from the canonical cmd target into the dist/linux/app staging tree
	// and produce the portable .tar.gz archive.
	linuxScript := readTreeFile(t, linuxLaneRelPath)

	if !strings.Contains(linuxScript, "./cmd/sheytan-local-agent") ||
		!strings.Contains(linuxScript, "dist/linux/app") {
		t.Fatalf(
			"production workflow drift: the Linux lane script %s does not build the desktop binary from ./cmd/sheytan-local-agent into dist/linux/app — the pinned runner identity would guard a lane that no longer builds the Linux x64 package",
			linuxLaneRelPath,
		)
	}

	if !strings.Contains(linuxScript, "SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz") {
		t.Fatalf(
			"production workflow drift: the Linux lane script does not produce the SHEYTAN-LA-ALPHA-linux-x64-portable.tar.gz archive",
		)
	}

	uploads, err := ArtifactSteps(w, "actions/upload-artifact")
	if err != nil {
		t.Fatalf("extract upload-artifact steps from %s: %v", PlatformBuildWorkflowPath, err)
	}

	uploadsPackage := false

	for _, u := range uploads {
		if u.Job == PlatformBuildJobKey && u.Name == MatrixPackageArtifactSlot &&
			strings.Contains(u.Path, "dist/") && strings.Contains(u.Path, "SHEYTAN-LA-ALPHA-") {
			uploadsPackage = true

			break
		}
	}

	if !uploadsPackage {
		t.Fatalf(
			"production workflow drift: job %q does not upload the matrix package artifact from dist/ — not the platform package upload",
			PlatformBuildJobKey,
		)
	}

	// The shared validator must ACCEPT the production workflow: the
	// runner pin AND the runtime OS identity guard, checked by the ONE
	// function the stress gate also runs (gate and tests cannot drift).
	if err := ValidateLinuxRunnerContract(w); err != nil {
		t.Fatalf("current platform-build workflow must satisfy the Linux runner contract: %v", err)
	}
}
