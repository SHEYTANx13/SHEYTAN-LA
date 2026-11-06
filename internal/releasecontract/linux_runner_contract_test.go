package releasecontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadDesktopWorkflow reads the real desktop workflow for the contract
// tests (same repo-relative identity the installer contract pins).
func loadDesktopWorkflow(t *testing.T) string {
	t.Helper()

	root := findRepoRoot(t)

	data, err := os.ReadFile(filepath.Join(root, workflowRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", workflowRelPath, err)
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

// TestLinuxRunnerContractRejectsExtraLabels: a composite runner selector
// (26.04 plus an extra label — e.g. a self-hosted or non-x64 composite)
// violates the singular pinned label.
func TestLinuxRunnerContractRejectsExtraLabels(t *testing.T) {
	w := loadDesktopWorkflow(t)

	// Target the Linux x64 job's own runs-on line (the job block is the
	// only place the name line is immediately followed by runs-on).
	linuxJobBlock := "name: Linux x64\n    runs-on: ubuntu-26.04"

	if !strings.Contains(w, linuxJobBlock) {
		t.Fatalf("fixture precondition: the workflow must carry the Linux x64 job block")
	}

	composite := strings.Replace(
		w,
		linuxJobBlock,
		"name: Linux x64\n    runs-on: [ubuntu-26.04, self-hosted]",
		1,
	)

	if err := ValidateLinuxRunnerContract(composite); err == nil {
		t.Fatal("a composite (multi-label) Linux x64 runner must fail the singular-label contract")
	}

	wrongImage := strings.Replace(
		w,
		linuxJobBlock,
		"name: Linux x64\n    runs-on: ubuntu-26.04-arm",
		1,
	)

	if err := ValidateLinuxRunnerContract(wrongImage); err == nil {
		t.Fatal("a non-x64 Linux runner label must fail the runner contract")
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
