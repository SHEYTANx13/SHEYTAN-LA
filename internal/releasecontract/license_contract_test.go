// license_contract_test.go — v1.7.2: the EXACT-ONE license artifact
// contract (REWRITTEN for the single-license-file architecture).
//
// Preferred authority is the short-form LICENSE.md restored by maintainer.
// brand.LicenseText and these tests are aligned to that document.
package releasecontract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/brand"
)

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

var excludedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, ".npm": true,
	"bin": true, "testresults": true, ".cache": true, "build-out": true,
	"coverage": true, "tmp": true,
}

var codeExtensions = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true,
	".py": true, ".rs": true, ".c": true, ".h": true, ".cpp": true,
	".hpp": true, ".cc": true, ".java": true, ".sh": true, ".ps1": true,
	".nsi": true, ".yml": true, ".yaml": true, ".json": true,
	".mod": true, ".sum": true, ".html": true, ".css": true, ".svg": true,
}

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
	for _, banned := range []string{
		"LICENSE", "LICENSE-APACHE", "LICENSE-PROPRIETARY",
		"LICENSE-MAP.md", "NOTICE.md", "LICENCE", "LICENCE.md", "COPYING",
	} {
		if _, err := os.Stat(filepath.Join(root, banned)); err == nil {
			t.Errorf("banned license artifact %q exists at the repository root", banned)
		}
	}
}

func TestLicenseMDIsTheCompleteConsolidatedDocument(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "LICENSE.md"))
	if err != nil {
		t.Fatalf("LICENSE.md: %v", err)
	}
	content := string(data)
	low := strings.ToLower(content)

	// Core consolidated sections (short-form LICENSE.md authority).
	for _, section := range []string{
		"licensing model",
		"component classification",
		"third-party software and attribution",
		"apache license 2.0",
		"proprietary license",
		"governance",
		"contact",
	} {
		if !strings.Contains(low, section) {
			t.Errorf("LICENSE.md missing consolidated section: %q", section)
		}
	}

	// Apache License 2.0 — short form references the standard license.
	for _, anchor := range []string{
		"Apache License 2.0",
		"http://www.apache.org/licenses/LICENSE-2.0",
	} {
		if !strings.Contains(content, anchor) && !strings.Contains(low, strings.ToLower(anchor)) {
			t.Errorf("LICENSE.md missing Apache-2.0 reference: %q", anchor)
		}
	}

	// Proprietary License — anchors present in the short-form document.
	for _, anchor := range []string{
		"Proprietary License",
		"Grant of License",
		"Trademark",
		"Ownership",
		"Disclaimer of Warranty",
		"Termination",
		"This license terminates automatically if you breach any term",
		"Changes",
	} {
		if !strings.Contains(content, anchor) {
			t.Errorf("LICENSE.md missing Proprietary License anchor: %q", anchor)
		}
	}

	// Classification + third-party notices.
	for _, anchor := range []string{
		"Open components",
		"Proprietary components",
		"internal/humanize/",
		"SPDX-License-Identifier",
		"llama.cpp",
		"fyne.io/fyne/v2",
		"Determining the license of a file",
	} {
		if !strings.Contains(content, anchor) && !strings.Contains(low, strings.ToLower(anchor)) {
			t.Errorf("LICENSE.md missing classification/attribution content: %q", anchor)
		}
	}

	// No live authority in deleted files.
	for _, stale := range []string{
		"see LICENSE-APACHE", "governed by LICENSE-APACHE",
		"see LICENSE-PROPRIETARY", "governed by LICENSE-PROPRIETARY",
		"see LICENSE +", "authoritative legal texts",
	} {
		if strings.Contains(low, strings.ToLower(stale)) {
			t.Errorf("LICENSE.md still routes authority to a deleted file: %q", stale)
		}
	}
}

func normalizeLicenseText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n") + "\n"
}

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
			"normalization: %d shipped lines vs %d authoritative lines, first differing line %d%s",
			len(shippedLines), len(authoritativeLines), firstDiff+1,
			describeLicenseDiffLine(firstDiff, shippedLines, authoritativeLines))
	}

	if !strings.HasPrefix(shipped, "# License") {
		t.Errorf("LICENSE.md does not start with the expected title")
	}
	if !strings.Contains(shipped, "https://github.com/SHEYTANx13") {
		t.Errorf("LICENSE.md does not end with the contact section")
	}

	brandSrc, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
	if err != nil {
		t.Fatalf("brand.go: %v", err)
	}
	brandSrcStr := string(brandSrc)

	for _, anchor := range []string{
		"const LicenseText = `# License",
		"Proprietary License",
		"Apache License 2.0",
		"internal/humanize/",
	} {
		if !strings.Contains(brandSrcStr, anchor) {
			t.Errorf("brand.go LicenseText missing anchor %q", anchor)
		}
	}
}

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

func TestLicenseSyncContractSurvivesWindowsLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(brand.LicenseText, "\n", "\r\n")
	cr := strings.ReplaceAll(brand.LicenseText, "\n", "\r")
	if normalizeLicenseText(crlf) != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("a CRLF-rendered LICENSE.md must satisfy the synchronization contract")
	}
	if normalizeLicenseText(cr) != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("a CR-rendered LICENSE.md must satisfy the synchronization contract")
	}
	if normalizeLicenseText(brand.LicenseText+"\n\n\n") != normalizeLicenseText(brand.LicenseText) {
		t.Fatal("trailing-newline variance must not break the synchronization contract")
	}
}

func TestNormalizeLicenseTextCanonicalizesToSingleTerminalLF(t *testing.T) {
	got := normalizeLicenseText("alpha\r\nbeta\rgamma\n\n\n")
	want := "alpha\nbeta\ngamma\n"
	if got != want {
		t.Fatalf("normalizeLicenseText mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestNoLiveReferencesToDeletedLicenseFiles(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{
		"internal/releasecontract/license_contract_test.go": true,
		"scripts/gen-license.go":                            true,
		"docs/UPDATE.md":                                    true,
		"docs/worklog.md":                                   true,
	}
	type violation struct {
		file, line string
	}
	var violations []violation
	patterns := []string{
		"](LICENSE)", "](LICENSE-APACHE)", "](LICENSE-PROPRIETARY)",
		"](LICENSE-MAP.md)", "](NOTICE.md)",
		"see `LICENSE`", "see LICENSE-APACHE", "see LICENSE-PROPRIETARY",
		"see LICENSE +", "governed by LICENSE-APACHE",
		"governed by LICENSE-PROPRIETARY", "authorities (LICENSE",
		"LICENSE-APACHE / LICENSE-PROPRIETARY",
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		ext := strings.ToLower(filepath.Ext(path))
		if !codeExtensions[ext] && ext != ".md" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		low := strings.ToLower(string(data))
		for _, p := range patterns {
			if strings.Contains(low, strings.ToLower(p)) {
				violations = append(violations, violation{file: rel, line: p})
			}
		}
		return nil
	})
	if len(violations) > 0 {
		for _, v := range violations {
			t.Errorf("live reference to deleted license artifact in %s: %q", v.file, v.line)
		}
	}
}
