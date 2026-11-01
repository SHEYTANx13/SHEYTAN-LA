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
//   - the makensis define-quoting contract (run #123: a bare
//     -DVERSION=1.11.0 split in PowerShell's argument parser into
//     -DVERSION=1 plus a script path .11.0 — makensis exited 1 before
//     installer.nsi ever opened);
//   - the payload-uninstall macro-closure contract (run #124: the
//     generator emitted the non-NSIS `!endmacro` terminator, the
//     generated macro was never closed, and makensis aborted on the
//     include at installer.nsi line 163 — pinned closed by
//     TestNSISPayloadUninstallMacroClosedByMacroend);
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

// TestNSISDefinesQuotedByWorkflow is THE run #123 guard: every makensis
// invocation in the workflow must pass every -D<NAME>= define token as a
// double-quoted argument, no bare -D<NAME>= token may exist anywhere in
// the workflow file, and the VERSION define specifically must stay in
// its quoted form.
//
// THE RUN #123 ROOT CAUSE (this test is its permanent guard): GitHub
// Actions substitutes ${{ ... }} BEFORE pwsh parses the step, so the
// bare token -DVERSION=1.11.0 reached PowerShell's native-command
// argument parser without quotes. That parser splits a bare token after
// the leading numeric literal: makensis received -DVERSION=1 (defining
// VERSION as "1" — confirmed by the teed line 'Command line defined:
// "VERSION=1"') and a separate .11.0, which it took for the script path
// (confirmed by 'Can't open script ".11.0"') — exit 1 before
// installer.nsi ever opened. The quoted siblings on the same line
// (-DEXE, -DBUILDDIR, -DOUTFILE) survived intact, which is what isolated
// the defect to the one bare token. Note that the quoted-sibling control
// also proves the check below cannot be satisfied by quoting SOME
// defines: every define token must carry its own quotes.
//
// Standing rule made mechanical here: an external-tool argument that
// carries a substituted value is either quoted or contract-tested —
// never a bare token. Any future drift of exactly this class reds the
// local ladder instead of a hosted run.
func TestNSISDefinesQuotedByWorkflow(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)
	lines := strings.Split(workflow, "\n")

	// defineTokenRe matches a makensis define argument anywhere in a
	// line: -D<NAME>= with no space before the '='. A quoted define
	// still matches (the match sits inside the quotes); the bareness
	// decision looks at the character BEFORE the match: an opening
	// double quote means the token is quoted, anything else (space,
	// line start) means bare.
	defineTokenRe := regexp.MustCompile(`-D[A-Za-z_][A-Za-z0-9_]*=`)
	defineName := func(tok string) string {
		m := regexp.MustCompile(`-D([A-Za-z_][A-Za-z0-9_]*)=`).FindStringSubmatch(tok)
		if m == nil {
			return tok
		}
		return m[1]
	}
	unquoted := func(t *testing.T, lineNo int, line string, loc []int) {
		t.Helper()
		name := defineName(line[loc[0]:loc[1]])
		t.Errorf("workflow line %d passes the makensis define %s UNQUOTED — GitHub Actions substitutes ${{ }} before pwsh parses the line, and a bare token splits after the leading numeric literal (run #123: -DVERSION=1.11.0 reached makensis as -DVERSION=1 plus a script path .11.0, exit 1 before installer.nsi opened). Quote the WHOLE -D<NAME>=... token, exactly like its siblings.", lineNo, name)
	}

	// 1. Whole-file sweep, fail closed: EVERY -D<NAME>= token on every
	// non-comment line must be quoted. The rule is not scoped to the
	// one line that failed run #123 — a second invocation site or a
	// newly added define gets the same mechanical rule.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, loc := range defineTokenRe.FindAllStringIndex(line, -1) {
			if loc[0] > 0 && line[loc[0]-1] == '"' {
				continue // opening quote: quoted define token
			}
			unquoted(t, i+1, line, loc)
		}
	}

	// 2. The guard must have a subject: at least one real makensis
	// invocation (comments mentioning makensis do not count).
	invocations := 0
	var invocationLines []int
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "makensis.exe") && !strings.HasPrefix(trimmed, "#") {
			invocations++
			invocationLines = append(invocationLines, i+1)
		}
	}
	if invocations == 0 {
		t.Fatalf("no makensis invocation found in %s — the define-quoting contract has no subject (was the installer build step removed or renamed?)", workflowRelPath)
	}

	// 3. Pin the run #123 fix itself: the VERSION define must exist on
	// a makensis invocation line IN QUOTED FORM (opening AND closing
	// double quote around the whole token, substituted value inside).
	versionRe := regexp.MustCompile(`"-DVERSION=[^"]*"`)
	for _, lineNo := range invocationLines {
		if !versionRe.MatchString(lines[lineNo-1]) {
			t.Errorf("workflow line %d does not carry the quoted VERSION define \"-DVERSION=<value>\" — the run #123 quoting fix drifted", lineNo)
		}
	}
}

// TestNSISPayloadUninstallMacroClosedByMacroend is THE run #124 guard
// (41256606070): the payload-uninstall generator must close the macro it
// opens with the exact NSIS preprocessor directive `!macroend`.
//
// THE RUN #124 ROOT CAUSE (this test is its permanent guard): the
// generator emitted `$lines += '!endmacro'` — not an NSIS directive. NSIS
// closes macros with `!macroend` only, so the generated
// payload-uninstall.nsh opened `!macro SHEYTAN_PAYLOAD_UNINSTALL_FILES`
// and never closed it. The hosted Windows job died inside the include:
//
//	!macro "SHEYTAN_PAYLOAD_UNINSTALL_FILES": unterminated (no !macroend found in file)!
//	!include: error in script: "payload-uninstall.nsh" on line 13
//	Error in script "packaging\\nsis\\installer.nsi" on line 163 -- aborting creation process
//
// installer.nsi (and its line-163 include) was and is CORRECT — the
// defect was one emitted generator token. The fix is the `!macroend`
// emission below plus the workflow's own post-write validation, both
// pinned here. Mechanical rules (deterministic text contract over the
// checkout; makensis itself does not run locally — the hosted run stays
// the installer's compile gate):
//
//  1. the generator still opens guard-define-then-macro (names
//     re-extracted, same convention as the #122 guard);
//  2. the LAST `$lines +=` emission before Set-Content is exactly
//     '!macroend'; the token '!endmacro' is explicitly rejected AND
//     fail-closed nowhere in the generator body outside comments;
//  3. the terminator is emitted AFTER the generated Delete/RMDir body
//     (a terminator emitted before the body would close the macro over
//     an empty list and strand the Delete/RMDir lines outside it);
//  4. the step validates the WRITTEN file before makensis reads it —
//     exists/non-empty readback, expected opening directives, final
//     directive exactly '!macroend', macro balance, and Delete-content
//     checks — each failing with a titled ::error:: naming the
//     generated file.
func TestNSISPayloadUninstallMacroClosedByMacroend(t *testing.T) {
	workflow := readTreeFile(t, workflowRelPath)

	step := "Generate payload-uninstall.nsh (uninstall symmetry)"
	start := strings.Index(workflow, "- name: "+step)
	if start < 0 {
		t.Fatalf("workflow step %q not found — the uninstall-symmetry generator is gone", step)
	}
	end := strings.Index(workflow[start+1:], "- name: ")
	var body string
	if end < 0 {
		body = workflow[start:]
	} else {
		body = workflow[start : start+1+end]
	}

	// The contract's subject: the define/macro opening must still be
	// emitted (names re-extracted, not duplicated as literals here).
	genDefine, genMacro := extractGeneratorNames(t, workflow)

	// Non-comment lines only: a terminator mentioned in a comment is not
	// an emission, and a comment carrying the defect must not hide it.
	type genLine struct {
		no   int
		text string
	}
	var code []genLine
	for i, l := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		code = append(code, genLine{no: i + 1, text: l})
	}

	// Rule 2 (fail-closed): the non-NSIS '!endmacro' token appears
	// NOWHERE in the generator body outside comments.
	for _, l := range code {
		if strings.Contains(l.text, "!endmacro") {
			t.Errorf("generator step line %d carries the non-NSIS token '!endmacro' outside a comment: %s — NSIS closes macros with exactly '!macroend' (the run #124 failure mode: unterminated !macro)", l.no, strings.TrimSpace(l.text))
		}
	}

	// The step must write the generated include.
	setIdx := -1
	for i, l := range code {
		if strings.Contains(l.text, "Set-Content") && strings.Contains(l.text, "payload-uninstall.nsh") {
			setIdx = i
			break
		}
	}
	if setIdx < 0 {
		t.Fatalf("generator never writes payload-uninstall.nsh (Set-Content) — the uninstall include has no source")
	}

	// Rules 1+2: collect every `$lines +=` emission before Set-Content;
	// the macro terminator is the LAST one.
	emissionRe := regexp.MustCompile(`\$lines\s*\+=\s*'([^']*)'`)
	var terminator genLine
	terminatorSet := false
	for _, l := range code[:setIdx] {
		if emissionRe.MatchString(l.text) {
			terminator, terminatorSet = l, true
		}
	}
	if !terminatorSet {
		t.Fatalf("generator emits no `$lines += '...'` terminator before Set-Content — the macro would again be left unterminated")
	}
	m := emissionRe.FindStringSubmatch(terminator.text)
	if m == nil {
		t.Fatalf("final pre-Set-Content `$lines +=` line is not a single-quoted emission: %q", strings.TrimSpace(terminator.text))
	}
	if m[1] != "!macroend" {
		if m[1] == "!endmacro" {
			t.Errorf("generator emits the terminator %q (step-body line %d) — NOT an NSIS directive; NSIS closes macros with exactly '!macroend'. THE RUN #124 ROOT CAUSE: with this token the generated payload-uninstall.nsh opens !macro %s and never closes it, makensis reports 'unterminated (no !macroend found in file)', the include at installer.nsi line 163 aborts, and the hosted installer build dies. Restore the '!macroend' emission.", m[1], terminator.no, genMacro)
		} else {
			t.Errorf("generator macro terminator drifted: emitted %q, want exactly '!macroend' (the only NSIS macro-closing directive; a wrong terminator re-creates run #124's 'unterminated (no !macroend found in file)' hosted failure)", m[1])
		}
	}

	// Rule 3: the terminator comes AFTER the generated Delete/RMDir body.
	lastDelete, lastRMDir := -1, -1
	for _, l := range code[:setIdx] {
		if !strings.Contains(l.text, "$lines +=") {
			continue
		}
		if strings.Contains(l.text, "Delete") {
			lastDelete = l.no
		}
		if strings.Contains(l.text, "RMDir") {
			lastRMDir = l.no
		}
	}
	if lastDelete < 0 || lastRMDir < 0 {
		t.Fatalf("generator body lost its Delete/RMDir emission lines (delete=%d rmdir=%d) — the uninstall list would be empty or malformed", lastDelete, lastRMDir)
	}
	if terminator.no <= lastRMDir {
		t.Errorf("macro terminator emitted at step-body line %d is not after the last RMDir emission (line %d) — the terminator must close the macro AFTER the Delete/RMDir body, or the generated uninstall commands end up outside the macro", terminator.no, lastRMDir)
	}

	// Rule 4: the WRITTEN file is validated before makensis reads it.
	hasReadback, hasEmpty, hasOpen, hasFinal, hasDeleteCheck := false, false, false, false, false
	hasOpenCount, hasCloseCount, hasTitled, hasGenBinding := false, false, false, false
	for _, l := range code[setIdx:] {
		s := l.text
		if strings.Contains(s, "Test-Path -LiteralPath $gen") && strings.Contains(s, "Length -eq 0") {
			hasEmpty = true
		}
		if strings.Contains(s, "Get-Content -LiteralPath $gen") {
			hasReadback = true
		}
		if strings.Contains(s, "'!define "+genDefine+"'") && strings.Contains(s, "'!macro "+genMacro+"'") {
			hasOpen = true
		}
		if strings.Contains(s, "-ne '!macroend'") {
			hasFinal = true
		}
		if strings.Contains(s, "-like '!macro *'") {
			hasOpenCount = true
		}
		if strings.Contains(s, "-eq '!macroend'") {
			hasCloseCount = true
		}
		if strings.Contains(s, "-like 'Delete *'") {
			hasDeleteCheck = true
		}
		if strings.Contains(s, "::error::payload-uninstall validation") {
			hasTitled = true
		}
		if strings.Contains(s, `$gen = "packaging/nsis/payload-uninstall.nsh"`) {
			hasGenBinding = true
		}
	}
	if !hasReadback {
		t.Errorf("generator step does not read the written file back (Get-Content -LiteralPath $gen) — run #124 proved the failure is deterministic at generation time and must be caught there")
	}
	if !hasEmpty {
		t.Errorf("generator validation does not assert the generated file exists and is non-empty (Test-Path + Length) before makensis runs")
	}
	if !hasOpen {
		t.Errorf("generator validation does not assert the expected opening directives ('!define %s' then '!macro %s')", genDefine, genMacro)
	}
	if !hasFinal {
		t.Errorf("generator validation does not assert the final non-empty directive is exactly '!macroend'")
	}
	if !(hasOpenCount && hasCloseCount) {
		t.Errorf("generator validation does not assert macro open/close balance (no '!macro *' open count and '!macroend' close count pair)")
	}
	if !hasDeleteCheck {
		t.Errorf("generator validation does not assert the output contains generated Delete commands (the uninstall body)")
	}
	if !(hasTitled && hasGenBinding) {
		t.Errorf("generator validation failures are not titled ::error:: messages naming the generated file — the step must bind $gen = \"packaging/nsis/payload-uninstall.nsh\" and fail every invariant with a titled ::error::payload-uninstall validation line (one-look diagnosability, the run #122/#124 lesson)")
	}
}
