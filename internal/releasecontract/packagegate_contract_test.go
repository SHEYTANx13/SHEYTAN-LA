package releasecontract

import (
	"regexp"
	"strings"
	"testing"
)

// Package-gate and Windows-package-pipeline contract tests (v1.11.1, run
// 42502043636 work order §3.3 items 6 and 9).
//
// The reviewed run captured the Windows lane printing "package gate:
// reached" with BOTH distribution packages absent (consolidated-log line
// 3313): the retired inline guard only rejected "ZIP missing AND installer
// exists". The gate is now the fail-closed scripts/ci/PackageGate.ps1, its
// six-state truth table is pinned hosted by PackageGate.Tests.ps1 (run by
// the "Package gate fixture contract" step), and the tests below pin the
// WORKFLOW side: the gate must call the script with both contracted paths,
// the producer steps must precede the validation/upload steps, and the
// upload must be success-gated with if-no-files-found: error and carry BOTH
// package paths — so an upload can never run with an incomplete package set.

// workflowStepBody returns the text of the named "- name:" step up to the
// next step boundary (a line opening with six spaces + "- name:") or the
// next top-level job boundary (a line of exactly two spaces, a job key and
// a colon) — whichever comes first.
var jobBoundaryRe = regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:[ \t]*$`)

func workflowStepBody(t *testing.T, workflow, step string) string {
	t.Helper()
	idx := strings.Index(workflow, "- name: "+step)
	if idx < 0 {
		t.Fatalf("workflow step %q not found", step)
	}
	rest := workflow[idx:]
	end := len(rest)
	if i := strings.Index(rest[1:], "\n      - name: "); i >= 0 && i+1 < end {
		end = i + 1
	}
	if loc := jobBoundaryRe.FindStringIndex(rest[1:]); loc != nil && loc[0]+1 < end {
		end = loc[0] + 1
	}
	return rest[:end]
}

func workflowStepIndex(workflow, step string) int {
	re := regexp.MustCompile(`-\s*name:\s*"?` + regexp.QuoteMeta(step) + `"?`)
	loc := re.FindStringIndex(workflow)
	if loc == nil {
		return -1
	}
	return loc[0]
}

// TestWindowsPackageGateIsFailClosed pins the gate replacement: the workflow
// step must invoke the fail-closed gate script with BOTH contracted package
// paths, must keep the always() root-cause-reporter placement, and must
// fail the job on any non-zero gate exit. The flawed inline predicate
// ( Test-Path $zip) -and (Test-Path $inst) must never return.
func TestWindowsPackageGateIsFailClosed(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	body := workflowStepBody(t, workflow, "Package gate (root-cause reporter)")
	for _, need := range []struct {
		fragment string
		why      string
	}{
		{"scripts/ci/PackageGate.ps1", "the gate must be the fail-closed script, not an inline predicate"},
		{`-Zip "dist/${env:WIN_PKG_ROOT}-v${env:APP_VERSION}-windows-x64.zip"`, "the gate must independently verify the contracted portable ZIP path"},
		{`-Installer "dist/${env:WIN_PKG_ROOT}-v${env:APP_VERSION}-windows-x64-installer.exe"`, "the gate must independently verify the contracted installer path"},
		{"$LASTEXITCODE -ne 0", "a non-zero gate exit must fail the job"},
		{"ci-logs/win-package-gate.log", "the gate transcript must land in the consolidated run-log artifact"},
	} {
		if !strings.Contains(body, need.fragment) {
			t.Errorf("package gate step lost a required mechanism: %q — %s", need.fragment, need.why)
		}
	}
	if strings.Contains(body, `-and (Test-Path $inst)`) {
		t.Errorf("package gate step still contains the flawed 'ZIP missing AND installer exists' predicate that printed 'reached' with both packages absent")
	}
	if !strings.Contains(body, "if: always()") {
		t.Errorf("package gate step must keep if: always() so a red upstream build still yields the named root cause")
	}
}

// TestWindowsPackageGateTruthTableFixtureRunsHosted pins that the six-state
// truth table of the gate is exercised by a deterministic fixture suite on
// the Windows runner before the real gate runs.
func TestWindowsPackageGateTruthTableFixtureRunsHosted(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	body := workflowStepBody(t, workflow, "Package gate fixture contract")
	if !strings.Contains(body, "./scripts/ci/PackageGate.Tests.ps1") {
		t.Fatalf("the 'Package gate fixture contract' step must run scripts/ci/PackageGate.Tests.ps1 on the runner")
	}
	if !strings.Contains(body, "$LASTEXITCODE -ne 0") {
		t.Fatalf("the fixture step must fail the job when the truth-table fixtures report violations")
	}
}

// TestWindowsPackageProducerOrdering pins the package pipeline order
// (work order §3.3 item 9): producers precede validators, validators
// precede the gate, and the gate precedes the upload — so an upload can
// never run with an incomplete or unvalidated package set.
func TestWindowsPackageProducerOrdering(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	ordered := []string{
		"Headless core tests (windows/amd64)",
		"Build SHEYTAN-LA.exe (windowsgui subsystem)",
		"Verify the PE (x86-64, windowsgui)",
		"Create Windows ZIP",
		"Verify Windows ZIP",
		"Install NSIS",
		"Build the NSIS installer",
		"Verify the installer exists",
		"NSIS install/uninstall smoke (payload symmetry + data preservation)",
		"Package gate (root-cause reporter)",
		"Upload Windows package",
	}
	prev := -1
	for _, step := range ordered {
		idx := workflowStepIndex(workflow, step)
		if idx < 0 {
			t.Fatalf("required Windows package step %q is missing from the workflow", step)
		}
		if idx <= prev {
			t.Errorf("Windows package step %q is out of order (index %d after %d) — producers must precede validators, the gate must precede the upload", step, idx, prev)
		}
		prev = idx
	}
}

// TestWindowsPackageUploadIsSuccessGatedAndComplete pins the upload
// contract: success-gated (never always()), if-no-files-found: error, and
// BOTH distribution files listed — a partial package set can never upload.
func TestWindowsPackageUploadIsSuccessGatedAndComplete(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	body := workflowStepBody(t, workflow, "Upload Windows package")
	if strings.Contains(body, "if: always()") {
		t.Errorf("the Windows package upload must be success-gated — an always() upload can bundle an incomplete package set after an upstream failure")
	}
	for _, need := range []string{
		`dist/${{ env.WIN_PKG_ROOT }}-v${{ env.APP_VERSION }}-windows-x64.zip`,
		`dist/${{ env.WIN_PKG_ROOT }}-v${{ env.APP_VERSION }}-windows-x64-installer.exe`,
		"if-no-files-found: error",
	} {
		if !strings.Contains(body, need) {
			t.Errorf("Windows package upload is missing the required slot %q", need)
		}
	}
}

// TestWindowsZipPurityBansQtRuntime pins the Qt/QML retirement contract in
// the ZIP verifier (work order §3.3 items 3-4): the portable payload must
// reject Qt runtime DLL entries (Qt5*/Qt6*/libQt* shapes) — a windeployqt-
// style payload or a Qt plugin tree fails the build instead of shipping.
func TestWindowsZipPurityBansQtRuntime(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	body := workflowStepBody(t, workflow, "Verify Windows ZIP")
	for _, need := range []string{
		"Qt5*.dll",
		"Qt6*.dll",
		"libQt*.dll",
		"Qt runtime DLL",
	} {
		if !strings.Contains(body, need) {
			t.Errorf("Verify Windows ZIP lost the Qt runtime purity ban fragment %q", need)
		}
	}
}

// TestWindowsInstallerPEIdentityValidated pins the installer PE validation
// (work order §3.3 item 6): the installer's PE identity is checked, never
// inferred from the .exe suffix. NSIS 3.x stubs are i386 PEs even when the
// payload is x64-only, so both i386 and x86-64 machine types are accepted.
func TestWindowsInstallerPEIdentityValidated(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	body := workflowStepBody(t, workflow, "Verify the installer exists")
	for _, need := range []string{
		"0x4D",   // MZ
		"0x50",   // PE signature
		"0x014C", // i386 machine type (NSIS 3.x stub)
		"0x8664", // x86-64 machine type
		"installer is not a PE binary (missing MZ)",
	} {
		if !strings.Contains(body, need) {
			t.Errorf("Verify the installer exists lost the PE identity check fragment %q", need)
		}
	}
}
