// license_contract_test.go — v1.7.2: the EXACT-ONE license artifact
// contract (REWRITTEN for the single-license-file architecture).
//
// THE REJECTED MODEL: "one Markdown + three legal authorities" — a root
// LICENSE, LICENSE-APACHE, LICENSE-PROPRIETARY and LICENSE.md coexisting
// (plus the earlier LICENSE-MAP.md/NOTICE.md pair). Four-plus licensing
// artifacts in one repository is ambiguous, drifts, and forces every
// consumer to guess which file is binding.
//
// THE v1.7.2 MODEL: the ENTIRE repository carries EXACTLY ONE license
// artifact — root LICENSE.md — and that file is the COMPLETE license
// document: the mixed-license model, the component classification, the
// third-party attribution notices, the trademark notice, governance and
// contact, and the FULL legal texts of BOTH licenses in force (Apache
// License 2.0 and the Parsaetak Proprietary License v1.1). The
// generator (scripts/gen-license.go) writes ONLY LICENSE.md from
// internal/brand.LicenseText and actively removes any resurrected legacy
// artifact.
//
// This contract prevents regression in BOTH directions:
//
//   - no second license/notice/copying artifact may appear ANYWHERE in
//     the tree, under ANY spelling (LICENSE, LICENCE, NOTICE, COPYING,
//     with -, _ or . continuations, any case);
//   - LICENSE.md may never degrade back into an index: the complete
//     consolidated sections AND representative anchors from BOTH full
//     legal texts must be present in the document itself;
//   - no documentation, generator, or runtime code may point at the
//     deleted files as live authorities;
//   - source files ABOUT licensing (a license command, this test, the
//     generator) are documents about the topic, not license artifacts.
package releasecontract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
)

// repoRoot resolves the repository root from THIS package's location
// (internal/releasecontract) so the contract reads the real tree.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		t.Fatalf("repo root not found at %s", root)
	}
	return root
}

// excludedDirs are VCS/dependency/build/runtime trees intentionally
// absent from the source distribution (never packaged, never part of the
// source license surface).
var excludedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, ".npm": true,
	"bin": true, "testresults": true, ".cache": true, "build-out": true,
	"coverage": true, "tmp": true,
}

// codeExtensions are implementation files ABOUT licensing (a license
// command, a contract test, the generator) — documents, not licenses.
var codeExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true,
	".py": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
	".hpp": true, ".cc": true, ".java": true, ".sh": true, ".ps1": true,
	".nsi": true, ".yml": true, ".yaml": true, ".json": true,
	".mod": true, ".sum": true, ".html": true, ".css": true, ".svg": true,
}

// isLicenseArtifact reports whether a FILENAME marks a license/notice/
// copying artifact under any common spelling, case-insensitively:
// LICENSE/LICENCE/NOTICE/COPYING bare, or with -, _ or . continuations
// (LICENSE-APACHE, LICENSE-MAP.md, NOTICE.md, copying.lesser, …).
// Source files ABOUT licensing (code extensions) are NOT artifacts.
func isLicenseArtifact(name string) bool {
	base := strings.ToLower(name)
	if ext := filepath.Ext(base); codeExtensions[ext] {
		return false
	}
	prefixes := []string{"license", "licence", "notice", "copying"}
	for _, p := range prefixes {
		if base == p || strings.HasPrefix(base, p+"-") || strings.HasPrefix(base, p+"_") ||
			strings.HasPrefix(base, p+".") {
			return true
		}
	}
	return false
}

// walkLicenseArtifacts collects EVERY license-named artifact in the whole
// repository (recursive, case-insensitive), excluding only VCS/dependency/
// build/runtime trees that are intentionally absent from source
// distribution.
func walkLicenseArtifacts(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if isLicenseArtifact(d.Name()) {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				rel = path
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return found
}

// TestExactlyOneLicenseArtifactInTheWholeTree is THE core invariant: the
// entire repository contains EXACTLY ONE license artifact and it is root
// LICENSE.md. Every other spelling — LICENSE, LICENSE-APACHE,
// LICENSE-PROPRIETARY, LICENSE-MAP.md, NOTICE.md, LICENCE, COPYING, … —
// anywhere in the tree fails the release.
func TestExactlyOneLicenseArtifactInTheWholeTree(t *testing.T) {
	found := walkLicenseArtifacts(t)

	if len(found) == 0 {
		t.Fatal("no license artifact found — root LICENSE.md is required")
	}

	if len(found) > 1 {
		t.Fatalf("EXACTLY ONE license artifact may exist, found %d: %v", len(found), found)
	}

	if found[0] != "LICENSE.md" {
		t.Fatalf("the one license artifact must be root LICENSE.md, found %q", found[0])
	}

	root := repoRoot(t)
	if info, err := os.Stat(filepath.Join(root, "LICENSE.md")); err != nil || info.IsDir() {
		t.Fatalf("LICENSE.md must exist as a file at the repository root: %v", err)
	}

	// The specific rejected artifacts, spelled out so a failure names them.
	for _, banned := range []string{
		"LICENSE", "LICENSE-APACHE", "LICENSE-PROPRIETARY",
		"LICENSE-MAP.md", "NOTICE.md", "LICENCE", "LICENCE.md", "COPYING",
	} {
		if _, err := os.Stat(filepath.Join(root, banned)); err == nil {
			t.Errorf("banned license artifact %q exists at the repository root", banned)
		}
	}
}

// TestLicenseMDIsTheCompleteConsolidatedDocument: LICENSE.md must be the
// COMPLETE license package, never an index pointing at files that no
// longer exist. Representative anchors from BOTH full legal texts are
// checked — not merely the words "Apache" and "Proprietary".
func TestLicenseMDIsTheCompleteConsolidatedDocument(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "LICENSE.md"))
	if err != nil {
		t.Fatalf("LICENSE.md: %v", err)
	}
	content := string(data)
	low := strings.ToLower(content)

	// (a) The seven consolidated sections exist.
	for _, section := range []string{
		"licensing model",
		"component classification",
		"third-party software and attribution notices",
		"apache license 2.0",
		"parsaetak proprietary license v1.1",
		"trademarks",
		"governance",
		"contact",
	} {
		if !strings.Contains(low, section) {
			t.Errorf("LICENSE.md missing consolidated section: %q", section)
		}
	}

	// (b) Apache License 2.0 — representative anchors from the FULL legal
	// text (clause structure, definitions, grants, conditions, appendix).
	for _, anchor := range []string{
		"TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION",
		"\"Derivative Works\" shall mean any work",
		"Grant of Copyright License",
		"Grant of Patent License",
		"institute patent litigation against any entity",
		"You must give any other recipients of the Work or",
		"Submission of Contributions",
		"Disclaimer of Warranty",
		"Limitation of Liability",
		"Accepting Warranty or Additional Liability",
		"END OF TERMS AND CONDITIONS",
		"How to apply the Apache License to your work",
		"http://www.apache.org/licenses/LICENSE-2.0",
	} {
		if !strings.Contains(content, anchor) && !strings.Contains(low, strings.ToLower(anchor)) {
			t.Errorf("LICENSE.md missing Apache-2.0 legal text anchor: %q", anchor)
		}
	}

	// (c) Parsaetak Proprietary License v1.1 — representative anchors from
	// the full text (scope, every numbered clause, contact).
	for _, anchor := range []string{
		"PARSAETAK PROPRIETARY LICENSE",
		"Version 1.1",
		"SCOPE OF THIS LICENSE",
		"IMPORTANT — READ CAREFULLY",
		"GRANT OF LICENSE",
		"INTELLECTUAL PROPERTY",
		"TRADEMARK",
		"DISTRIBUTION",
		"You may NOT redistribute, sublicense, sell, rent, lease, or host",
		"DERIVATIVE WORKS",
		"LOCAL-FIRST PRIVACY",
		"ACCEPTABLE USE",
		"DISCLAIMER OF WARRANTY",
		"TERMINATION",
		"This license terminates automatically if you breach any term",
		"CHANGES",
	} {
		if !strings.Contains(content, anchor) {
			t.Errorf("LICENSE.md missing Proprietary License legal text anchor: %q", anchor)
		}
	}

	// (d) Classification + third-party notices (former LICENSE-MAP.md and
	// NOTICE.md content, merged in).
	for _, anchor := range []string{
		"Open components",
		"Proprietary components",
		"internal/humanize/",
		"SPDX-License-Identifier",
		"llama.cpp",
		"The ggml authors",
		"gorilla/websocket",
		"Qt 6 (Core/Gui/Quick/QML/QuickControls2/Test)",
		"react-markdown",
		"How to determine the license of a file",
	} {
		if !strings.Contains(content, anchor) && !strings.Contains(low, strings.ToLower(anchor)) {
			t.Errorf("LICENSE.md missing classification/attribution content: %q", anchor)
		}
	}

	// (e) The rejected routing: no live authority in deleted files. The
	// document must not name the removed artifacts as authorities (the
	// historical migration may be acknowledged, routing may not).
	for _, stale := range []string{
		"see LICENSE-APACHE", "governed by LICENSE-APACHE",
		"see LICENSE-PROPRIETARY", "governed by LICENSE-PROPRIETARY",
		"see LICENSE +", "authoritative legal texts", "](" + "LICENSE)",
		"](" + "LICENSE-APACHE)", "](" + "LICENSE-PROPRIETARY)",
		"](" + "LICENSE-MAP.md)", "](" + "NOTICE.md)",
	} {
		if strings.Contains(low, strings.ToLower(stale)) {
			t.Errorf("LICENSE.md still routes authority to a deleted file: %q", stale)
		}
	}
}

// normalizeLicenseText is the canonical EOL normalization for the
// synchronization contract: CRLF and bare CR both fold to LF, trailing
// newlines collapse to exactly one terminal LF. v1.7.3 — the previous
// comparison trimmed only "\n" before the suffix check, so a Windows
// checkout (CRLF) left a trailing "\r" and failed the contract on a
// perfectly synchronized document (Actions run 36311052375, Windows job
// 108597318493). The contract is now CROSS-PLATFORM: normalization
// first, then exact complete-document equality.
func normalizeLicenseText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	return strings.TrimRight(s, "\n") + "\n"
}

// TestLicenseMDSynchronizedWithBrand: the shipped LICENSE.md must equal
// brand.LicenseText EXACTLY after EOL normalization — the generator
// contract (scripts/gen-license.go writes the file from the in-code
// authority; drift is a defect). This is a full-document comparison
// against the compiled brand constant, not a suffix heuristic: any
// drift anywhere in the document — front, middle, end, whitespace or
// missing content — fails with the first differing line named.
func TestLicenseMDSynchronizedWithBrand(t *testing.T) {
	if testing.Short() {
		t.Skip("brand compile check skipped in -short mode")
	}

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "LICENSE.md"))
	if err != nil {
		t.Fatalf("LICENSE.md: %v", err)
	}

	shipped := normalizeLicenseText(string(data))
	authoritative := normalizeLicenseText(brand.LicenseText)

	if shipped != authoritative {
		shippedLines := strings.Split(shipped, "\n")
		authoritativeLines := strings.Split(authoritative, "\n")

		firstDiff := -1
		for i := 0; i < len(shippedLines) || i < len(authoritativeLines); i++ {
			var a, b string
			if i < len(shippedLines) {
				a = shippedLines[i]
			}
			if i < len(authoritativeLines) {
				b = authoritativeLines[i]
			}
			if a != b {
				firstDiff = i

				break
			}
		}

		t.Fatalf("LICENSE.md does not match brand.LicenseText exactly after EOL "+
			"normalization (regenerate with scripts/gen-license.go): "+
			"%d shipped lines vs %d authoritative lines, first differing line %d"+
			"%s",
			len(shippedLines), len(authoritativeLines), firstDiff+1,
			describeLicenseDiffLine(firstDiff, shippedLines, authoritativeLines))
	}

	// Redundant with the full-document equality above, but kept as
	// standalone diagnostics so a degraded document is named precisely
	// even if brand.LicenseText itself regresses in the same direction:
	// the document must start with the consolidated title and end with
	// the consolidated contact section.
	if !strings.HasPrefix(shipped, "# LICENSE.md — SHEYTAN-Local-Agent licensing") {
		t.Errorf("LICENSE.md does not start with the consolidated document title (regenerate with scripts/gen-license.go)")
	}
	if !strings.HasSuffix(shipped, "channel listed there).\n") {
		t.Errorf("LICENSE.md does not end with the consolidated contact section (regenerate with scripts/gen-license.go)")
	}

	// The brand source must carry the same document inside LicenseText.
	brandSrc, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
	if err != nil {
		t.Fatalf("brand.go: %v", err)
	}
	brandSrcStr := string(brandSrc)

	// Anchor: the title and both marker comments must appear in brand.go.
	for _, anchor := range []string{
		"const LicenseText = `# LICENSE.md — SHEYTAN-Local-Agent licensing",
		"apache-2.0-text-begin",
		"apache-2.0-text-end",
		"proprietary-text-begin",
		"proprietary-text-end",
	} {
		if !strings.Contains(brandSrcStr, anchor) {
			t.Errorf("brand.go LicenseText missing anchor %q — the const must carry the complete consolidated document", anchor)
		}
	}
}

// describeLicenseDiffLine renders the first divergent line pair for the
// failure message (empty when the line counts match and no divergence
// was found — defensive; the caller only invokes it on mismatch).
func describeLicenseDiffLine(idx int, shipped, authoritative []string) string {
	if idx <= 0 {
		return ""
	}

	var b strings.Builder

	i := idx - 1
	if i < len(shipped) {
		fmt.Fprintf(&b, "\n  shipped      line %d: %q", idx, shipped[i])
	}
	if i < len(authoritative) {
		fmt.Fprintf(&b, "\n  authoritative line %d: %q", idx, authoritative[i])
	}

	return b.String()
}

// TestLicenseSyncContractSurvivesWindowsLineEndings encodes the exact
// Windows CI failure class of run 36311052375: a LICENSE.md rendered
// with CRLF line endings (the historical checkout/round-trip mode) must
// still satisfy the synchronization contract. The contract is
// cross-platform by construction — normalization happens BEFORE the
// comparison, never inside the document.
func TestLicenseSyncContractSurvivesWindowsLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(brand.LicenseText, "\n", "\r\n")
	cr := strings.ReplaceAll(brand.LicenseText, "\n", "\r")

	if normalizeLicenseText(crlf) != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("a CRLF-rendered LICENSE.md must satisfy the synchronization contract (Windows portability)")
	}

	if normalizeLicenseText(cr) != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("a CR-rendered LICENSE.md must satisfy the synchronization contract (Windows portability)")
	}

	if normalizeLicenseText(brand.LicenseText+"\n\n\n") != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("trailing-newline variance must not break the synchronization contract")
	}
}

// TestNormalizeLicenseTextCanonicalizesToSingleTerminalLF: the helper
// itself must fold CRLF/CR to LF and collapse all trailing newlines to
// exactly one terminal LF — the canonical form the generator emits.
func TestNormalizeLicenseTextCanonicalizesToSingleTerminalLF(t *testing.T) {
	got := normalizeLicenseText("alpha\r\nbeta\rgamma\n\n\n")
	want := "alpha\nbeta\ngamma\n"

	if got != want {
		t.Fatalf("normalizeLicenseText mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestNoLiveReferencesToDeletedLicenseFiles: documentation, generator and
// runtime code must not point at the deleted artifacts as live
// authorities. Source files ABOUT licensing (this test, the generator's
// legacy-cleanup list) legitimately mention the names; the scan therefore
// ignores files whose mention is a historical or removal reference, and
// fails only on ACTIVE routing patterns (markdown links, "see X",
// "governed by X", "authoritative").
func TestNoLiveReferencesToDeletedLicenseFiles(t *testing.T) {
	root := repoRoot(t)

	// Files allowed to mention the artifacts without being "live
	// references": this contract test, the generator (its cleanup list),
	// and clearly historical changelog records.
	allowed := map[string]bool{
		"internal/releasecontract/license_contract_test.go": true,
		"scripts/gen-license.go":                            true,
		"UPDATE.md":                                         true, // changelog: history of the migration
		"worklog.md":                                        true, // engineering log: history
	}

	type violation struct {
		file, line string
	}

	var violations []violation

	// Active-authority patterns (case-insensitive).
	patterns := []string{
		"](LICENSE)", "](LICENSE-APACHE)", "](LICENSE-PROPRIETARY)",
		"](LICENSE-MAP.md)", "](NOTICE.md)",
		"see `LICENSE`", "see LICENSE-APACHE", "see LICENSE-PROPRIETARY",
		"see LICENSE +", "governed by LICENSE-APACHE",
		"governed by LICENSE-PROPRIETARY", "authorities (LICENSE",
		"LICENSE-APACHE / LICENSE-PROPRIETARY",
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if allowed[rel] {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !codeExtensions[ext] && ext != ".md" && ext != ".txt" {
			return nil
		}
		if strings.HasPrefix(rel, "web/static/") {
			return nil // generated frontend bundle, not source
		}

		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		low := strings.ToLower(string(data))

		for _, pat := range patterns {
			if strings.Contains(low, strings.ToLower(pat)) {
				violations = append(violations, violation{rel, pat})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	for _, v := range violations {
		t.Errorf("%s still references a deleted license file as a live authority (%q)", v.file, v.line)
	}
}

// TestGeneratorWritesOnlyLicenseMD: the generator source must write
// LICENSE.md and never LICENSE/LICENSE-APACHE/LICENSE-PROPRIETARY as
// output paths.
func TestGeneratorWritesOnlyLicenseMD(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "gen-license.go"))
	if err != nil {
		t.Fatalf("gen-license.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, `os.WriteFile("LICENSE.md"`) {
		t.Error("gen-license.go must write LICENSE.md (the one license artifact)")
	}

	// A WriteFile to a legacy path would recreate the rejected model.
	for _, banned := range []string{`os.WriteFile("LICENSE"`, `os.WriteFile("LICENSE-APACHE"`, `os.WriteFile("LICENSE-PROPRIETARY"`} {
		if strings.Contains(src, banned) {
			t.Errorf("gen-license.go writes banned artifact path %q", banned)
		}
	}

	// The generator must actively remove resurrected legacy artifacts.
	if !strings.Contains(src, "legacyArtifacts") || !strings.Contains(src, "os.Remove") {
		t.Error("gen-license.go must remove resurrected legacy license artifacts (the guard clause)")
	}
}

// TestLicenseCLIRemainsValid: the license CLI path renders the brand
// constants truthfully under the single-document architecture.
func TestLicenseCLIRemainsValid(t *testing.T) {
	root := repoRoot(t)

	// cmd/license.go prints brand.LicenseText — the full consolidated
	// document — so the CLI needs no routing to deleted files.
	cliSrc, err := os.ReadFile(filepath.Join(root, "cmd", "license.go"))
	if err != nil {
		t.Fatalf("cmd/license.go: %v", err)
	}
	cli := string(cliSrc)

	if !strings.Contains(cli, "brand.LicenseText") {
		t.Error("cmd/license.go must print brand.LicenseText (the complete consolidated document)")
	}
	for _, stale := range []string{"LICENSE-APACHE", "LICENSE-PROPRIETARY", "LICENSE-MAP.md", "NOTICE.md"} {
		if strings.Contains(cli, stale) {
			t.Errorf("cmd/license.go still references the deleted file %q", stale)
		}
	}

	// The brand constants reference only LICENSE.md.
	brandSrc, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
	if err != nil {
		t.Fatalf("brand.go: %v", err)
	}
	brandSrcStr := string(brandSrc)

	// brand.go contains the full LicenseText which legitimately names
	// LICENSE.md §2 routing INSIDE the document — but it must not route
	// to the deleted FILES. The LicenseText const body references
	// "LICENSE.md" (this same file) only.
	for _, stale := range []string{"LICENSE-APACHE", "LICENSE-PROPRIETARY", "LICENSE-MAP.md", "NOTICE.md"} {
		if strings.Contains(brandSrcStr, stale) {
			t.Errorf("internal/brand still references the deleted file %q — the consolidated document routes internally", stale)
		}
	}
}
