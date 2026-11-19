package releasecontract

// Product-identity spelling contract (master work order for run
// 44457547289, "Canonical product spelling"): the product identifier is
// the exact ASCII string SHEYTAN-LA. In every source file, UI label,
// workflow, artifact name, document, command, test, report and package
// name, no Cyrillic lookalike, inserted whitespace, alternate hyphen or
// stylized spelling may replace it. This file is the release-contract
// test that rejects:
//
//   1. Cyrillic homoglyphs (С Е У Т А Н and friends) anywhere in tracked
//      text — a homoglyph identifier renders identically and breaks
//      scripts, search and artifact naming silently;
//   2. invisible/ambiguous characters inside or adjacent to the
//      identifier (zero-width joiners/space, soft hyphen, BOM, word
//      joiner);
//   3. whitespace or hyphen variants of the identifier: SHEYTAN‑LA
//      (U+2011), SHEYTAN -LA, SHEYTAN- LA, SHEYTAN LA, SHEYTAN—LA and
//      every other non-ASCII-hyphen-42 spelling.

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode"
)

// cyrillicHomoglyphs is the Cyrillic block scan range: every code point
// in U+0400..U+04FF is a potential lookalike for this product's ASCII
// identifier and has no legitimate use in this repository's English/
// ASCII-only engineering surface.
const cyrillicRangeLo, cyrillicRangeHi = 0x0400, 0x04FF

// invisibleRunes are characters that may not appear adjacent to (or
// inside) the product identifier because they are invisible in review
// and can smuggle a spelling variant past a human diff read.
var invisibleRunes = map[rune]bool{
	'\u200B': true, // zero-width space
	'\u200C': true, // zero-width non-joiner
	'\u200D': true, // zero-width joiner
	'\uFEFF': true, // BOM / zero-width no-break space
	'\u00AD': true, // soft hyphen
	'\u2060': true, // word joiner
}

// notASCIITextExtensions are file types the scan covers. Binary assets
// (png/ico/zip) are excluded by name suffix.
var binarySuffixes = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".zip": true, ".gz": true, ".aar": true,
	".jar": true, ".bin": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".pdf": true, ".der": true,
	".pem": true, ".gitattributes": false,
}

func trackedTextFiles(t *testing.T) []string {
	t.Helper()

	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v (run from the repository root)", err)
	}

	var files []string

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		skip := false

		for suffix := range binarySuffixes {
			if strings.HasSuffix(line, suffix) && suffix != ".gitattributes" {
				skip = true
				break
			}
		}

		// The spelling-contract fixtures carry intentional violations as
		// test data; they are excluded from the whole-tree scan (their
		// detector logic is pinned by the unit tests above instead).
		if strings.HasSuffix(line, "spelling_contract_test.go") {
			skip = true
		}

		if !skip {
			files = append(files, line)
		}
	}

	if len(files) == 0 {
		t.Fatalf("git ls-files returned no text files")
	}

	return files
}

func TestProductIdentifierHasNoCyrillicHomoglyphs(t *testing.T) {
	for _, path := range trackedTextFiles(t) {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}

		scanner := bufio.NewScanner(f)
		lineNo := 0

		for scanner.Scan() {
			lineNo++

			for _, r := range scanner.Text() {
				if r >= cyrillicRangeLo && r <= cyrillicRangeHi {
					t.Errorf("%s:%d: Cyrillic code point %U (%q) found — the product identifier and the whole engineering surface are ASCII-only; use the exact spelling SHEYTAN-LA",
						path, lineNo, r, string(r))
					break
				}
			}
		}

		_ = f.Close()
	}
}

func TestProductIdentifierRejectsInvisibleAndWhitespaceVariants(t *testing.T) {
	// Every spelling below MUST be rejected by this contract wherever it
	// would stand in for the product identifier. The exact form
	// "SHEYTAN-LA" (ASCII hyphen-minus, no whitespace) is the only
	// accepted one.
	bad := []string{
		"SHEYTAN\u2011LA",   // non-breaking hyphen
		"SHEYTAN\u2013LA",   // en dash
		"SHEYTAN\u2014LA",   // em dash
		"SHEYTAN -LA",       // space before hyphen
		"SHEYTAN- LA",       // space after hyphen
		"SHEYTAN LA",        // hyphen removed
		"SHEYTAN\u00A0- LA", // nbsp + split hyphen
		"SHEYTAN\u200BLA",   // zero-width space instead of hyphen
		"\u0421HEYTAN-LA",   // leading Cyrillic Es (U+0421)
		"SHEYTAN-L\u0410",   // trailing Cyrillic A (U+0410)
	}

	for _, b := range bad {
		if spellingVariantViolation(b) == "" {
			t.Errorf("spellingVariantViolation(%q) = \"\" — the variant must be detected", b)
		}
	}

	// The canonical spelling must pass untouched.
	if v := spellingVariantViolation("SHEYTAN-LA"); v != "" {
		t.Errorf("the canonical spelling must not be flagged, got %q", v)
	}

	if v := spellingVariantViolation("SHEYTAN-LA-ALPHA-windows-x64-portable.zip"); v != "" {
		t.Errorf("canonical spelling inside an artifact name must not be flagged, got %q", v)
	}
}

func TestTrackedFilesHaveNoIdentifierSpellingVariants(t *testing.T) {
	for _, path := range trackedTextFiles(t) {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}

		scanner := bufio.NewScanner(f)
		lineNo := 0

		for scanner.Scan() {
			lineNo++

			line := scanner.Text()

			// Invisible characters anywhere in tracked text are a
			// contract violation: they are invisible in review and can
			// alter identifier spelling or scripts without detection.
			for _, r := range line {
				if invisibleRunes[r] {
					t.Errorf("%s:%d: invisible code point %U found — remove it (zero-width/soft-hyphen/BOM characters have no legitimate use in this tree)", path, lineNo, r)
					break
				}
			}

			// Identifier-shaped text with anything other than the exact
			// ASCII spelling is a variant: match SHEYTAN, optional junk,
			// LA as a token boundary — then require the byte-exact form.
			if v := spellingVariantViolation(line); v != "" {
				t.Errorf("%s:%d: %s", path, lineNo, v)
			}
		}

		_ = f.Close()
	}
}

// spellingVariantViolation scans one line for identifier-shaped tokens
// that are NOT the exact ASCII spelling. It returns "" when the line is
// clean. The detector is deliberately mechanical:
//
//   - find every occurrence of "SHEYTAN" (case-sensitive; the product
//     identifier is always uppercase in this tree);
//   - require the following rune to be the ASCII hyphen-minus and the
//     one after it to be 'L', then 'A', then a non-letter boundary;
//   - flag Cyrillic lookalike sequences that begin with a Cyrillic
//     letter from the identifier's homoglyph set.
func spellingVariantViolation(line string) string {
	const ident = "SHEYTAN"

	for i := 0; i+len(ident) <= len(line); i++ {
		if line[i:i+len(ident)] != ident {
			continue
		}

		rest := line[i+len(ident):]

		// Exact canonical spelling: hyphen-minus + LA + boundary.
		if strings.HasPrefix(rest, "-LA") {
			after := ""
			if len(rest) > 3 {
				after = rest[3:4]
			}
			if !isIdentifierBoundary(after) {
				return "identifier-shaped token 'SHEYTAN-LA' followed by a non-boundary character " + quoteRune(after) + " — use the exact product identifier"
			}
			continue
		}

		// Whitespace/hyphen variants: optional unicode whitespace around
		// any hyphen-like or zero-width separator, then LA.
		j := 0

		for j < len(rest) {
			r := []rune(rest[j:])[0]
			if r == ' ' || r == '\t' || (unicode.IsSpace(r) && r != '\n' && r != '\r') {
				j += len(string(r))
				continue
			}
			break
		}

		if j < len(rest) {
			r := []rune(rest[j:])[0]
			if isHyphenLike(r) || invisibleRunes[r] {
				k := j + len(string(r))

				for k < len(rest) {
					w := []rune(rest[k:])[0]
					if w == ' ' || w == '\t' || (unicode.IsSpace(w) && w != '\n' && w != '\r') {
						k += len(string(w))
						continue
					}
					break
				}

				if strings.HasPrefix(rest[k:], "LA") {
					afterLA := ""
					if len(rest) > k+2 {
						afterLA = rest[k+2 : k+3]
					}
					if isIdentifierBoundary(afterLA) {
						return "product identifier spelling variant detected (whitespace or non-ASCII hyphen around SHEYTAN-LA) — use the exact ASCII spelling SHEYTAN-LA"
					}
				}
			}
		}

		// Homoglyph tail: SHEYTAN-L followed by a non-ASCII letter
		// (e.g. the Cyrillic А U+0410 standing in for the Latin A).
		if strings.HasPrefix(rest, "-L") && len(rest) > 2 {
			r := []rune(rest[2:])[0]
			if r != 'A' && unicode.IsLetter(r) && (r > 0x7F || r >= cyrillicRangeLo && r <= cyrillicRangeHi) {
				return "product identifier homoglyph detected (non-ASCII letter standing in for the Latin A of SHEYTAN-LA) — use the exact ASCII spelling"
			}
		}

		// Missing hyphen entirely: "SHEYTAN LA".
		if strings.HasPrefix(rest, " LA") {
			after := ""
			if len(rest) > 3 {
				after = rest[3:4]
			}
			if isIdentifierBoundary(after) {
				return "product identifier spelled without its hyphen (SHEYTAN LA) — use the exact ASCII spelling SHEYTAN-LA"
			}
		}
	}

	// Cyrillic homoglyph sequences that start a lookalike identifier
	// (e.g. "\u0421HEYTAN"): flagged by the whole-tree Cyrillic scan,
	// but surface a precise message for identifier-shaped prefixes here.
	runes := []rune(line)

	for i, r := range runes {
		if r >= cyrillicRangeLo && r <= cyrillicRangeHi && i+6 < len(runes) {
			tail := string(runes[i+1 : i+7])
			if tail == "HEYTAN" {
				return "Cyrillic homoglyph at the start of an identifier-shaped token — use the exact ASCII spelling SHEYTAN-LA"
			}
		}
	}

	return ""
}

func isIdentifierBoundary(s string) bool {
	if s == "" {
		return true
	}

	r := []rune(s)[0]

	return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
}

func isHyphenLike(r rune) bool {
	switch r {
	case '-', // hyphen-minus (ASCII, the ONLY accepted separator when
		// directly between SHEYTAN and LA with no whitespace)
		'\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015',
		'\u2212', '\u02D7', '\u2796', '\uFE58', '\uFE63', '\uFF0D':
		return true
	}

	return false
}

func quoteRune(s string) string {
	if s == "" {
		return "end of line"
	}

	for _, r := range s {
		return string(r)
	}

	return s
}
