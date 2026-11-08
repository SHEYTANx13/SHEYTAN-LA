package api

// goals_log_injection_callsites_v1111_test.go — v1.11.1 E5 remediation
// proof: the settleDriveError logger callsites (CodeQL go/log-injection
// cluster #309–#317 in internal/api/goals.go) sit behind the ONE
// boundary encoder goalLogValue — every attacker-controlled argument
// (goal id, phase, status, category, error text) is escaped there before
// it reaches log.Printf. These tests drive the REAL callsites with
// hostile payloads (CRLF, ANSI CSI, Unicode line separators, tabs, NEL)
// and pin that:
//
//   - every emitted record is a single "[goal]"-prefixed line — a
//     payload can never forge an extra log line;
//   - the raw control bytes never appear in the output (the escaped
//     spellings do — the boundary encoded them, it did not drop them).

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

const hostileLogPayload = "boom\r\n[FORGED LINE]\x1b[31mred\u2028next\u0085more\tend"

// captureDefaultLogger swaps the standard logger for a buffer and
// restores the previous writer/flags afterwards.
func captureDefaultLogger(t *testing.T) (*bytes.Buffer, func()) {
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

// assertOnlyGoalRecords pins the two invariants above over the captured
// output: exactly wantRecords "[goal]" records and zero raw control
// bytes from the hostile payload.
func assertOnlyGoalRecords(t *testing.T, out string, wantRecords int) {
	t.Helper()

	for _, raw := range []string{"\r", "\x1b", "\u2028", "\u0085"} {
		if strings.Contains(out, raw) {
			t.Errorf("raw control byte %q reached the log output — the boundary encoder must escape it", raw)
		}
	}

	records := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue // the logger's trailing newline per record
		}
		records++
		if !strings.HasPrefix(line, "[goal]") {
			t.Errorf("forged log line %q — every record must carry the [goal] prefix", line)
		}
	}

	if records != wantRecords {
		t.Errorf("log records = %d, want exactly %d (one per exercised callsite):\n%s", records, wantRecords, out)
	}
}

func TestSettleDriveErrorLogCallsitesNeutralizeInjection(t *testing.T) {
	base := t.TempDir()
	store, err := goal.Open(base)
	if err != nil {
		t.Fatalf("open goal store: %v", err)
	}

	g, err := store.Create("objective with \x1b[31m hostile bytes\r\nin it", "sess-log-injection", "sys-1", 1, "")
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	s := &Server{stack: &runtime.Stack{Goals: store}}

	buf, restore := captureDefaultLogger(t)
	defer restore()

	// (a) vanished-goal callsite with a HOSTILE goal id (L299 cluster):
	// the id carries every record-breaking class (CRLF, ANSI CSI, the
	// three Unicode line separators, a tab) so the boundary's escaped
	// spellings are all observable on a logged value.
	s.settleDriveError("ghost\r\n[forged]\x1b[0m-id\u2028\u0085more\tend", context.Canceled)

	// (b) cancelled-drive callsite with a hostile wrapped error
	// (L316): errors.Is recognizes the Canceled category through the
	// wrapping, so the hostile payload flows to the boundary.
	s.settleDriveError(g.GoalID, fmt.Errorf("drive failed: %s: %w", hostileLogPayload, context.Canceled))

	// (c) settled-drive callsite with a hostile non-retryable error
	// (evidence append + SettleDriveFailure + the settled record, L322).
	// The detail redactor normalizes whitespace fields upstream, so the
	// tab/NEL expectations are pinned on the id path in (a).
	s.settleDriveError(g.GoalID, fmt.Errorf("drive boom %s", hostileLogPayload))

	assertOnlyGoalRecords(t, buf.String(), 3)

	// The escaped spellings must be present — the boundary encoded the
	// payloads, it did not silently drop them.
	out := buf.String()
	for _, escaped := range []string{`\r`, `\x1b`, `\x2028`, `\x85`, `\t`} {
		if !strings.Contains(out, escaped) {
			t.Errorf("escaped form %s missing from output — the boundary must encode, not discard:\n%s", escaped, out)
		}
	}
}

func TestSettleDriveErrorVanishedGoalIsSingleRecord(t *testing.T) {
	base := t.TempDir()
	store, err := goal.Open(base)
	if err != nil {
		t.Fatalf("open goal store: %v", err)
	}

	s := &Server{stack: &runtime.Stack{Goals: store}}

	buf, restore := captureDefaultLogger(t)
	defer restore()

	// A long hostile id on the vanished path stays ONE record with the
	// whole payload confined inside it.
	s.settleDriveError(strings.Repeat("id\x1b\r\n", 40), context.DeadlineExceeded)

	assertOnlyGoalRecords(t, buf.String(), 1)
}
