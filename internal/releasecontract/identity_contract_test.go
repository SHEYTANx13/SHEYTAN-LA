// Package releasecontract — identity_contract_test.go
//
// Run 44306343338 work order §14: the public product identity is exactly
// SHEYTAN-LA (ASCII) with stage ALPHA. An automated scan rejects the
// known wrong spellings and Unicode homoglyphs across the tracked,
// user-facing surface: docs, workflows, packaging metadata, scripts and
// Go sources (repo owner/module path, historical commit IDs and
// third-party names are NOT rewritten by this contract).
package releasecontract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bannedIdentityPatterns are the wrong spellings the work order S14
// forbids: a spaced variant of the name and any non-ASCII lookalike
// character occupying the T position (the U+0422 defect found in
// REQUIREMENTS-TRACEABILITY.md during this repair cycle). The banned
// spellings are never spelled out literally here — the scan would
// flag its own source — they are described and matched structurally.
var bannedIdentityPatterns = []*regexp.Regexp{
	regexp.MustCompile(`SHEY\s+TAN`),             // a spaced variant of the name
	regexp.MustCompile(`SHEY[-_]{2,}TAN`),        // doubled separators
	regexp.MustCompile(`(?i)SHEY[^\x00-\x7F]AN`), // non-ASCII homoglyph in the T position (Cyrillic/Greek/CJK lookalikes)
}

// retiredArtifactNames must never reappear in any tracked file (the
// verbose all-logs artifact and the duplicate generic package names are
// retired with the topology they belonged to).
var retiredArtifactNames = []string{
	"SHEYTAN-LA-all-logs.txt",
	"windows-x64-package",
	"linux-x64-package",
}

func TestTrackedTreeUsesExactASCIIIdentity(t *testing.T) {
	root := findRepoRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "dist" || name == "ci-logs" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".md", ".yml", ".yaml", ".json", ".ps1", ".sh", ".bat", ".txt", ".mod", ".toml", ".plist":
		default:
			return nil // binary assets and icons are not name-bearing surfaces
		}
		if d.Name() == "FILE_CATALOG.json" || d.Name() == "FILE_CATALOG.md" {
			return nil // generated catalog mirrors tracked file PATHS, not prose
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		relSlash := filepath.ToSlash(rel)
		text := string(data)

		// Enforcement sites OWN the retirement vocabulary: the
		// releasecontract package (constants + negative fixtures) and the
		// collector fixture suite (its negative assertions quote the
		// retired names to ban them). The retired-name scan exempts them.
		inReleasecontract := strings.HasPrefix(relSlash, "internal/releasecontract/") ||
			relSlash == "scripts/ci/consolidate-run-log-fixtures.sh" ||
			relSlash == "docs/changelog.md" ||
			relSlash == "docs/retired.md"

		for _, re := range bannedIdentityPatterns {
			if loc := re.FindStringIndex(text); loc != nil {
				line := 1 + strings.Count(text[:loc[0]], "\n")
				t.Errorf("%s:%d: banned identity spelling %q — the product name is exactly SHEYTAN-LA (ASCII, no homoglyphs, work order §14)", rel, line, re.FindString(text))
			}
		}

		if inReleasecontract {
			return nil
		}

		for _, retired := range retiredArtifactNames {
			if strings.Contains(text, retired) {
				t.Errorf("%s: retired artifact name %q must not reappear (run 44306343338 §6.3/§7 consolidation)", rel, retired)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("identity scan walk failed: %v", err)
	}
}

func TestIdentityScanDetectsTheKnownDefect(t *testing.T) {
	// The detector must catch the exact homoglyph defect found in this
	// repair cycle (REQUIREMENTS-TRACEABILITY.md used U+0422 in the name).
	defected := "report for SHEY\u0422AN-LA-Stability"
	for _, re := range bannedIdentityPatterns {
		if re.MatchString(defected) {
			return
		}
	}
	t.Fatal("the identity detector no longer matches the known U+0422 homoglyph defect")
}
