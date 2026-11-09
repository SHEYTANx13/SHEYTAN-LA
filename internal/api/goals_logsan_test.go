package api

// goals_logsan_test.go — task 12-loginject (security work order §4.4)
// verification for the goal log boundary. Three proofs:
//
//  1. the boundary encoder goalLogValue neutralizes every
//     record-breaking class (CR, LF, CRLF, NUL, ANSI ESC sequences,
//     the FULL C1 range, U+2028/U+2029, invalid UTF-8) while
//     legitimate text — including HTML-sensitive characters, which
//     cannot break a plain-text log record — survives usefully;
//  2. one hostile value can never forge a second log line: no raw
//     \r or \n byte (or other C0/C1/line-separator codepoint) reaches
//     the formatted log output;
//  3. driving the REAL settleDriveError flow with hostile inputs, the
//     log stream stays one sanitized record per event while the
//     STORED/business values (the durable objective and evidence
//     detail) keep the original raw bytes — logging sanitizes only
//     the log representation, never storage.

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/goal"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/runtime"
)

// lsanCaptureLogger swaps the standard logger for a buffer and
// restores the previous writer/flags afterwards. (Uniquely named so
// this file stays self-contained next to the other goal log tests.)
func lsanCaptureLogger(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()

	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)

	return &buf, func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	}
}

// lsanAssertNoRawControlBytes fails if ANY raw C0 byte, C1 byte, DEL,
// or Unicode line separator survives in the sanitized value. This is
// the whole point of the boundary: these codepoints break log records
// and terminals; their ESCAPED spellings (e.g. the four bytes
// `\x1b`) are plain printable text and are allowed.
func lsanAssertNoRawControlBytes(t *testing.T, name, got string) {
	t.Helper()

	for r := rune(0x00); r <= 0x1f; r++ { // C0 including NUL, CR, LF, ESC
		if strings.ContainsRune(got, r) {
			t.Errorf("%s: raw C0 control byte U+%04X reached the output: %q", name, r, got)
		}
	}
	for r := rune(0x7f); r <= 0x9f; r++ { // DEL + full C1 (NEL, CSI, OSC...)
		if strings.ContainsRune(got, r) {
			t.Errorf("%s: raw C1 control byte U+%04X reached the output: %q", name, r, got)
		}
	}
	for _, r := range []rune{0x2028, 0x2029} { // Unicode line/paragraph separators
		if strings.ContainsRune(got, r) {
			t.Errorf("%s: raw Unicode line separator U+%04X reached the output: %q", name, r, got)
		}
	}
}

func TestGoalLogSanitizerTable(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"bare CR", "a\rb", `a\rb`},
		{"bare LF", "a\nb", `a\nb`},
		{"CRLF", "a\r\nb", `a\r\nb`},
		{"NUL", "a\x00b", `a\x00b`},
		{"tab", "a\tb", `a\tb`},
		{"ANSI color escape", "\x1b[31mred\x1b[0m", `\x1b[31mred\x1b[0m`},
		{"ANSI terminal wipe", "\x1b[2J\x1b[H", `\x1b[2J\x1b[H`},
		{"C1 CSI single-byte (U+009B)", "a\u009b1mb", `a\x9b1mb`},
		{"C1 OSC single-byte (U+009D)", "x\u009d0;t\u009c", `x\x9d0;t\x9c`},
		{"raw C1 byte is invalid UTF-8 → U+FFFD", "a\x9bb", "a\uFFFDb"},
		{"DEL", "a\x7fb", `a\x7fb`},
		{"NEL (U+0085)", "a\u0085b", `a\x85b`},
		{"line separator (U+2028)", "a\u2028b", `a\x2028b`},
		{"paragraph separator (U+2029)", "a\u2029b", `a\x2029b`},
		{"invalid UTF-8 replaced", "goal-\xff\xfe-id", "goal-\uFFFD-id"},
		{"error value", fmt.Errorf("boom\r\n[goal] FORGED"), `boom\r\n[goal] FORGED`},
		// HTML-sensitive characters cannot break a plain-text log
		// record; the boundary must keep them readable (useful), not
		// mangle them.
		{"HTML-sensitive passthrough", `<script>alert("xss")</script> & <b>`, `<script>alert("xss")</script> & <b>`},
		{"legitimate text passthrough", "goal-b11223 plain objective text", "goal-b11223 plain objective text"},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		got := goalLogValue(tc.in)
		if got != tc.want {
			t.Errorf("%s: goalLogValue(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
		lsanAssertNoRawControlBytes(t, tc.name, got)
	}
}

func TestGoalLogSanitizerOneValueOneRecord(t *testing.T) {
	hostile := []string{
		"a\r\n[goal] FORGED RECORD",
		"a\n[goal] FORGED RECORD",
		"a\r[goal] FORGED RECORD",
		"a\x00[goal] FORGED\x1b[2J",
		"a\u2028[goal] FORGED",
		"a\u2029[goal] FORGED",
		"a\u0085[goal] FORGED",
		"<script>\r\n[goal] FORGED",
	}

	for _, payload := range hostile {
		out := fmt.Sprintf("[goal] %s: settled", goalLogValue(payload))

		if n := strings.Count(out, "\n"); n != 0 {
			t.Errorf("payload %q produced %d raw LF bytes in the record — one value must stay ONE record: %q", payload, n, out)
		}
		// One hostile value stays ONE record: the forged "[goal]"
		// text survives only as inert escaped payload — with no raw
		// \r/\n byte it can never become a second line prefix.
		lines := strings.Split(out, "\n")
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "[goal]") {
			t.Errorf("payload %q broke the record structure — a forged line was injected: %q", payload, out)
		}
		lsanAssertNoRawControlBytes(t, "record", out)
	}
}

func TestGoalLogSanitizerRealFlowKeepsStorageRaw(t *testing.T) {
	store, err := goal.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open goal store: %v", err)
	}

	// Interior bytes only: Create TrimSpaces the edges, the hostile
	// interior must survive storage byte-for-byte.
	objective := "ship the thing\r\n[goal] FORGED\x1b[31m \x00<html>&amp;\u2029done"
	g, err := store.Create(objective, "sess-logsan", "sys-1", 1, "")
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	s := &Server{stack: &runtime.Stack{Goals: store}}

	buf, restore := lsanCaptureLogger(t)
	defer restore()

	// (1) vanished-goal flow with a HOSTILE goal id — the settle
	// callsites log the id through the boundary.
	s.settleDriveError("ghost\r\n[goal] FORGED\x1b[0m \u0085\x00\u2028\u2029end", context.Canceled)

	// (2) cancelled-settlement flow on the REAL goal with a hostile
	// wrapped error — the cancelled path appends the raw detail as
	// durable evidence and logs id/phase/status through the boundary.
	s.settleDriveError(g.GoalID, fmt.Errorf("drive boom\r\n[goal] FORGED\x1b[31m \x00end\u2028: %w", context.Canceled))

	out := buf.String()

	// Exactly TWO records; every emitted line carries the [goal]
	// prefix — nothing was forged out of the hostile payloads.
	records := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue // the logger's per-record trailing newline
		}
		records++
		if !strings.HasPrefix(line, "[goal]") {
			t.Errorf("forged log line %q — every record must carry the [goal] prefix", line)
		}
	}
	if records != 2 {
		t.Errorf("log records = %d, want exactly 2 (one per settleDriveError call):\n%s", records, out)
	}

	// No raw record-breaking byte reached the log stream. (Raw LF is
	// covered structurally by the per-line [goal] prefix check above.)
	lsanAssertNoRawControlBytes(t, "flow", strings.ReplaceAll(out, "\n", ""))

	// The boundary ENCODED the payloads — the escaped spellings must be
	// visible, not silently dropped.
	for _, esc := range []string{`\r`, `\n`, `\x00`, `\x1b`, `\x85`, `\x2028`, `\x2029`} {
		if !strings.Contains(out, esc) {
			t.Errorf("escaped form %s missing from log output — the boundary must encode, not discard:\n%s", esc, out)
		}
	}

	// The STORED/business value is unchanged by logging: the durable
	// objective keeps every raw hostile byte, and the evidence the
	// cancelled path appended keeps the raw non-whitespace controls
	// (CR/LF are collapsed by the pre-existing business-layer
	// redactor, NOT by the log boundary — that collapse is storage
	// behavior, out of scope here).
	after, err := store.Get(g.GoalID)
	if err != nil {
		t.Fatalf("re-read goal: %v", err)
	}
	if after.OriginalGoal != objective {
		t.Errorf("logging mutated the stored objective:\n got %q\nwant %q", after.OriginalGoal, objective)
	}
	if len(after.Evidence) == 0 {
		t.Fatalf("the cancelled path appended no evidence:\n%s", out)
	}
	last := after.Evidence[len(after.Evidence)-1]
	for _, raw := range []string{"\x1b", "\x00"} {
		if !strings.Contains(last.Detail, raw) {
			t.Errorf("stored evidence detail lost the raw byte %q — storage must keep the original business value: %q", raw, last.Detail)
		}
	}
}
