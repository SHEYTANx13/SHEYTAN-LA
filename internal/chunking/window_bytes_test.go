package chunking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowHeadTailBytesMatchesStringPath pins the v1.2.4 byte-windowing
// semantics against the original string implementation: identical output on
// realistic and adversarial inputs (empty, single line, no trailing
// newline, unicode, oversized single line).
func TestWindowHeadTailBytesMatchesStringPath(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"short":            "one\ntwo\nthree\n",
		"no-newline":       strings.Repeat("x", 1000),
		"unicode":          strings.Repeat("héllo wörld 你好世界\n", 80),
		"one-huge-line":    strings.Repeat("a", 5000),
		"lines":            strings.Repeat("2026-09-16 INFO handled request\n", 200),
		"blank-line-heavy": strings.Repeat("para\n\n\n", 120),
	}
	for name, text := range cases {
		got := WindowHeadTailBytes([]byte(text), 2048)
		want := WindowHeadTail(text, 2048)
		if got != want {
			t.Fatalf("%s: byte path diverges from string path\n got: %q\nwant: %q", name, got, want)
		}
	}
}

// TestWindowHeadTailBytesNeverAliasesInput guards the safety property: the
// returned string must be a fresh allocation bounded by the budget, never a
// view of the input slice that would keep the whole file alive in memory.
func TestWindowHeadTailBytesNeverAliasesInput(t *testing.T) {
	big := []byte(strings.Repeat("line of a fairly large log file\n", 40000)) // ~1.2 MB
	out := WindowHeadTailBytes(big, DefaultAttachmentBudgetBytes)
	if len(out) > DefaultAttachmentBudgetBytes+4096 {
		t.Fatalf("windowed output %d bytes exceeds budget bound", len(out))
	}
	// Mutate the input; the output must be unaffected (no shared backing).
	for i := range big {
		big[i] = 'X'
	}
	if strings.Contains(out, "XXXX") && strings.HasPrefix(out, strings.Repeat("X", 16)) {
		t.Fatal("output aliases input backing array")
	}
}

// TestFormatFileAttachmentLargeFile is the large-file processing check:
// a multi-MB text attachment composes into a budgeted block with the
// elision marker present.
func TestFormatFileAttachmentLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	line := "2026-09-16T00:00:00Z INFO request handled status=200\n"
	var sb strings.Builder
	for sb.Len() < 3<<20 {
		sb.WriteString(line)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := FormatFileAttachment(path, DefaultAttachmentBudgetBytes)
	if len(out) > DefaultAttachmentBudgetBytes*2 {
		t.Fatalf("composed block %d bytes — not budgeted", len(out))
	}
	if !strings.Contains(out, "elided") {
		t.Fatal("elision marker missing for oversized file")
	}
	if !strings.Contains(out, "big.log") {
		t.Fatal("file name missing from block")
	}
}
