// installer_contract_test.go — run #122 (41239634246) deep-fix: the NSIS
// installer contracts that previously lived only as two loosely-coupled
// texts (the workflow's payload-uninstall generator and installer.nsi)
// are pinned here, mechanically, so the drift class that killed run #122
// fails the Go ladder instead of a hosted run.
//
// THE RUN #122 ROOT CAUSE (this file is its permanent guard): the
// workflow step "Generate payload-uninstall.nsh (uninstall symmetry)"
// emits `!define SHEYTAN_PAYLOAD_UNINSTALL` followed by
// `!macro SHEYTAN_PAYLOAD_UNINSTALL_FILES`, while installer.nsi executed
// `!insertmacro SHEYTAN_PAYLOAD_UNINSTALL` — the bare DEFINE name, not
// the macro name. NSIS keeps defines and macros in SEPARATE namespaces:
// the !ifdef guard passed on the generated define, then the insertmacro
// targeted a macro that does not exist, and makensis aborted
// (macro-not-found, exit 1). One convention now holds and is asserted
// below: define == `SHEYTAN_PAYLOAD_UNINSTALL`, macro ==
// `SHEYTAN_PAYLOAD_UNINSTALL_FILES`, and the nsi's !ifdef and
// !insertmacro must agree with the generator EXACTLY.
//
// The same file pins the other installer contracts this session touched:
//   - the makensis diagnosability contract (tee + marker enforcement);
//   - the license-page contract (single-source LICENSE.md, required
//     payload file, symmetric uninstall delete — no second license
//     artifact anywhere, enforced by license_contract_test.go);
//   - the brand-identity contract (installer PUBLISHER/AUMID/REGKEY must
//     equal the runtime identity in internal/config / internal/platform;
//     the Parsaetak copyright ATTRIBUTION line stays byte-identical).
//
// All checks are deterministic text contracts over files in the checkout
// (class local-static). makensis itself does not run here — the hosted
// run is the installer's compile gate.
package releasecontract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/platform"
)

const (
	workflowRelPath = ".github/workflows/build-desktop.yml"
	nsiRelPath      = "packaging/nsis/installer.nsi"

	// payloadDefineName / payloadMacroName are the ONE convention (see
	// the file comment). They are re-extracted from the generator line
	// in every test below; these constants are the failure-message
	// anchors, not a second source of truth.
	payloadDefineName = "SHEYTAN_PAYLOAD_UNINSTALL"
	payloadMacroName  = "SHEYTAN_PAYLOAD_UNINSTALL_FILES"

	// nsisIncludeMarker is emitted by installer.nsi (!echo) when the
	// generated payload-uninstall list is included; the workflow must
	// assert it in the teed makensis log after the build.
	nsisIncludeMarker = "sheytan: payload uninstall list included"

	// nsisLogPath is the tee target that makes the makensis transcript
	// retrievable from the windows-logs-part artifact (run #122 lesson:
	// the red was named but its compiler output was not retrievable).
	nsisLogPath = "ci-logs/win-nsis-build.log"
)

func readTreeFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract source %s unreadable: %v", rel, err)
	}
	return string(data)
}

// extractGeneratorNames pulls the define and macro names the workflow's
// payload-uninstall generator emits ($lines = @('!define X', '!macro Y')).
func extractGeneratorNames(t *testing.T, workflow string) (define, macro string) {
	t.Helper()
	re := regexp.MustCompile(`\$lines\s*=\s*@\('!define\s+(\w+)'\s*,\s*'!macro\s+(\w+)'\)`)
	m := re.FindStringSubmatch(workflow)
	if m == nil {
		t.Fatalf("workflow generator line not found: expected $lines = @('!define <NAME>', '!macro <NAME>') in %s", workflowRelPath)
	}
	return m[1], m[2]
}

// nsiUninstallSection returns the text of installer.nsi from the
// Uninstall section onward, so the !ifdef/!insertmacro extraction cannot
// match unrelated directives.
func nsiUninstallSection(t *testing.T, nsi string) string {
	t.Helper()
	idx := strings.Index(nsi, `Section "Uninstall"`)
	if idx < 0 {
		t.Fatalf("installer.nsi has no Uninstall section — uninstall symmetry contract unreadable")
	}
	return nsi[idx:]
}

// TestNSISPayloadUninstallMacroNameContract is THE run #122 guard: the
// generated macro name and the nsi's !insertmacro name must be equal,
// and the nsi's !ifdef define name must equal the generator's define.
func TestNSISPayloadUninstallMacroNameContract(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)
	nsi := readTreeFile(t, nsiRelPath)

	genDefine, genMacro := extractGeneratorNames(t, workflow)
	if genDefine != payloadDefineName {
		t.Errorf("generator define name drifted: got %q, want %q (update this contract DELIBERATELY, on both sides, in one batch)", genDefine, payloadDefineName)
	}
	if genMacro != payloadMacroName {
		t.Errorf("generator macro name drifted: got %q, want %q (update this contract DELIBERATELY, on both sides, in one batch)", genMacro, payloadMacroName)
	}

	section := nsiUninstallSection(t, nsi)

	ifdefRe := regexp.MustCompile(`!ifdef\s+(\w+)`)
	m := ifdefRe.FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("installer.nsi Uninstall section has no !ifdef payload guard")
	}
	if m[1] != genDefine {
		t.Errorf("installer.nsi !ifdef define %q != generator define %q — the uninstall include guard would never fire", m[1], genDefine)
	}

	insertRe := regexp.MustCompile(`!insertmacro\s+(SHEYTAN_PAYLOAD\w*)`)
	m = insertRe.FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("installer.nsi Uninstall section never inserts the generated payload macro — the uninstaller would remove nothing")
	}
	// The exact equality that failed run #122: the MACRO name, not the
	// define name, must be inserted.
	if m[1] != genMacro {
		t.Errorf("installer.nsi !insertmacro %q != generator macro %q — makensis aborts with macro-not-found (the run #122 failure class)", m[1], genMacro)
	}

	// The generated file must keep its own guard define: the generator
	// emits the define FIRST so the nsi's !ifdef can test it.
	if !strings.Contains(workflow, `'!define `+genDefine+`', '!macro `+genMacro+`'`) {
		t.Errorf("generator must emit !define %s before !macro %s (the nsi !ifdef tests the define)", genDefine, genMacro)
	}
}

// TestNSISMarkerEnforcedByWorkflow pins the makensis diagnosability
// contract: the build step tees the transcript into ci-logs/win-nsis-build.log
// (reaching the windows-logs-part artifact) and asserts the nsi include
// marker in that log with a titled ::error on absence.
func TestNSISMarkerEnforcedByWorkflow(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)
	nsi := readTreeFile(t, nsiRelPath)

	// The nsi must still emit the marker the workflow asserts.
	if !regexp.MustCompile(`!echo\s+"` + regexp.QuoteMeta(nsisIncludeMarker) + `"`).MatchString(nsi) {
		t.Fatalf("installer.nsi no longer echoes %q — the workflow marker contract has no source", nsisIncludeMarker)
	}

	step := "Build the NSIS installer"
	start := strings.Index(workflow, "- name: "+step)
	if start < 0 {
		t.Fatalf("workflow step %q not found", step)
	}
	// The step body ends at the next "- name:" (or end of file).
	end := strings.Index(workflow[start+1:], "- name: ")
	var body string
	if end < 0 {
		body = workflow[start:]
	} else {
		body = workflow[start : start+1+end]
	}

	if !strings.Contains(body, nsisLogPath) {
		t.Errorf("makensis step does not tee into %s — the transcript would again be unretrievable (the run #122 lesson)", nsisLogPath)
	}
	if !strings.Contains(body, "Tee-Object") {
		t.Errorf("makensis step does not Tee-Object its output into %s", nsisLogPath)
	}
	if !strings.Contains(body, nsisIncludeMarker) {
		t.Errorf("makensis step does not assert the %q marker after the build", nsisIncludeMarker)
	}
	if !strings.Contains(body, "::error::NSIS marker contract") {
		t.Errorf("marker absence must fail with a titled ::error::NSIS marker contract (never a bare exit)")
	}
}

// TestNSISLicensePageContract pins the installer license contract: the
// license page renders the STAGED single-source LICENSE.md, its presence
// is a compile-time assertion, the payload File is REQUIRED (not
// /nonfatal), and the uninstaller deletes it symmetrically. Exactly one
// license artifact exists in the whole tree (license_contract_test.go) —
// the installer embeds THAT file, it never creates a second one.
func TestNSISLicensePageContract(t *testing.T) {
	nsi := readTreeFile(t, nsiRelPath)

	// Page order: WELCOME < LICENSE < DIRECTORY (operator requirement).
	welcome := strings.Index(nsi, "!insertmacro MUI_PAGE_WELCOME")
	license := strings.Index(nsi, "!insertmacro MUI_PAGE_LICENSE ")
	directory := strings.Index(nsi, "!insertmacro MUI_PAGE_DIRECTORY")
	if welcome < 0 || license < 0 || directory < 0 {
		t.Fatalf("installer.nsi missing one of the MUI pages: welcome=%d license=%d directory=%d", welcome, license, directory)
	}
	if !(welcome < license && license < directory) {
		t.Errorf("MUI page order must be WELCOME -> LICENSE -> DIRECTORY, got welcome=%d license=%d directory=%d", welcome, license, directory)
	}

	licRe := regexp.MustCompile(`!insertmacro\s+MUI_PAGE_LICENSE\s+"([^"]+)"`)
	m := licRe.FindStringSubmatch(nsi)
	if m == nil {
		t.Fatalf("MUI_PAGE_LICENSE inserted without a LicenseData path")
	}
	if m[1] != `${BUILDDIR}\LICENSE.md` {
		t.Errorf("license page must render the staged single-source LICENSE.md (${BUILDDIR}\\LICENSE.md), got %q", m[1])
	}

	// Compile-time assertion: a missing staged license fails the
	// installer build with the exact path (same fail-fast class as the
	// staged EXE assertion).
	if !strings.Contains(nsi, `!if ! /FileExists "${BUILDDIR}\LICENSE.md"`) {
		t.Errorf("installer.nsi has no compile-time assertion for the staged LICENSE.md")
	}

	// The payload file is REQUIRED: /nonfatal would ship an installer
	// without its license on staging drift.
	if strings.Contains(nsi, `File /nonfatal "${BUILDDIR}\LICENSE.md"`) {
		t.Errorf(`payload File for LICENSE.md must be REQUIRED (no /nonfatal) — the license page and the uninstall symmetry depend on it`)
	}
	if !strings.Contains(nsi, `File "${BUILDDIR}\LICENSE.md"`) {
		t.Errorf(`installer payload must stage LICENSE.md (File "${BUILDDIR}\LICENSE.md")`)
	}

	// Uninstall symmetry: the installed LICENSE.md is removed again.
	uninstall := nsiUninstallSection(t, nsi)
	if !strings.Contains(uninstall, `Delete "$INSTDIR\LICENSE.md"`) {
		t.Errorf(`uninstaller must Delete "$INSTDIR\LICENSE.md" (uninstall symmetry with the required payload File)`)
	}
}

// TestNSISBrandIdentityContract pins the installer's brand identity to
// the runtime authorities: PUBLISHER/AUMID/REGKEY are the SHEYTANx13
// identity (derived from the code repo, matching internal/config and
// internal/platform — converted from Parsaetak in run #122), PRODUCT and
// DESCRIPTION match internal/config, and the Parsaetak copyright
// ATTRIBUTION line stays byte-identical. No Parsaetak identity string may
// re-enter any functional nsi directive: every remaining Parsaetak
// occurrence must be a comment or the LegalCopyright attribution.
func TestNSISBrandIdentityContract(t *testing.T) {
	nsi := readTreeFile(t, nsiRelPath)

	nsiDefine := func(name string) string {
		t.Helper()
		re := regexp.MustCompile(`!define\s+` + name + `\s+"([^"]*)"`)
		m := re.FindStringSubmatch(nsi)
		if m == nil {
			t.Fatalf("installer.nsi missing !define %s", name)
		}
		return m[1]
	}

	if got := nsiDefine("PRODUCT"); got != config.AppShortName {
		t.Errorf("PRODUCT %q != config.AppShortName %q (brand authority mismatch)", got, config.AppShortName)
	}
	if got := nsiDefine("DESCRIPTION"); got != config.AppDescription {
		t.Errorf("DESCRIPTION %q != config.AppDescription %q (brand authority mismatch)", got, config.AppDescription)
	}
	if got := nsiDefine("PUBLISHER"); got != config.AppPublisher {
		t.Errorf("PUBLISHER %q != config.AppPublisher %q (installer and runtime must publish the SAME identity)", got, config.AppPublisher)
	}
	if got := nsiDefine("AUMID"); got != config.AppUserModelID {
		t.Errorf("AUMID %q != config.AppUserModelID %q (installer and runtime must register the SAME AppUserModelID)", got, config.AppUserModelID)
	}
	if config.AppUserModelID != platform.AppUserModelID {
		t.Errorf("config.AppUserModelID %q != platform.AppUserModelID %q (runtime identity split)", config.AppUserModelID, platform.AppUserModelID)
	}

	owner := strings.SplitN(config.AppUserModelID, ".", 2)[0]
	wantRegKey := fmt.Sprintf(`Software\%s\SHEYTAN-LA`, owner)
	if got := nsiDefine("REGKEY"); got != wantRegKey {
		t.Errorf("REGKEY %q != %q (must derive from the code-repo owner %q, same <owner>\\<product> pattern)", got, wantRegKey, owner)
	}

	// ATTRIBUTION, byte-identical: the copyright line is Parsaetak's and
	// must never drift (identity conversion ≠ reattribution).
	const wantCopyright = `(c) 2024-2026 Parsaetak. All rights reserved.`
	crRe := regexp.MustCompile(`VIAddVersionKey\s+"LegalCopyright"\s+"([^"]*)"`)
	m := crRe.FindStringSubmatch(nsi)
	if m == nil {
		t.Fatalf("installer.nsi missing LegalCopyright VIAddVersionKey")
	}
	if m[1] != wantCopyright {
		t.Errorf("LegalCopyright drifted: got %q, want byte-identical %q (attribution is NOT identity — restore it)", m[1], wantCopyright)
	}

	// No Parsaetak IDENTITY string in any functional directive: every
	// remaining occurrence must live on a comment line or in the
	// LegalCopyright attribution above.
	for i, line := range strings.Split(nsi, "\n") {
		if !strings.Contains(line, "Parsaetak") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "VIAddVersionKey \"LegalCopyright\"") {
			continue
		}
		t.Errorf("installer.nsi line %d carries Parsaetak OUTSIDE attribution/comments: %s", i+1, strings.TrimSpace(line))
	}
}
