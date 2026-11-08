package api

// goals_log_injection_v1111_test.go — v1.11.1 (run 41933333132, work
// order §8.4): the goal engine's log sink normalizes every untrusted
// value. Forged "INFO" lines, bare CR/LF, tabs, NUL, ANSI escapes and
// invalid UTF-8 can no longer break one logical event out of one log
// record — each becomes a visible escaped literal on a single line.

import (
	"strings"
	"testing"
)

func TestGoalLogValueNeutralizesInjection(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		lines int // records the value occupies when embedded in one log line
	}{
		{"forged INFO line", "goal-1\nINFO forged record", `goal-1\nINFO forged record`, 1},
		{"bare CR", "goal-1\rforged", `goal-1\rforged`, 1},
		{"bare LF", "goal-1\nforged", `goal-1\nforged`, 1},
		{"CRLF", "goal-1\r\nINFO", `goal-1\r\nINFO`, 1},
		{"tab", "goal\tid", `goal\tid`, 1},
		{"NUL", "goal\x00id", `goal\x00id`, 1},
		{"ANSI escape", "\x1b[31mred\x1b[0m", `\x1b[31mred\x1b[0m`, 1},
		{"unicode line separator", "a b", `a\x2028b`, 1},
		{"NEL next line (U+0085)", "a\u0085b", `a\x85b`, 1},
		{"paragraph separator", "a\u2029b", `a\x2029b`, 1},
		{"invalid utf-8 replaced", "goal-\xff\xfe-id", "goal-\uFFFD-id", 1},
	}

	for _, tc := range cases {
		got := goalLogValue(tc.in)
		if got != tc.want {
			t.Errorf("%s: goalLogValue(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
		if n := strings.Count(got, "\x0a") + strings.Count(got, "\x0d"); n != 0 {
			t.Errorf("%s: the sanitized value still carries raw control bytes (%d)", tc.name, n)
		}
	}
}

func TestGoalLogValueKeepsPlainTextIntact(t *testing.T) {
	for _, s := range []string{"goal-abc-123", "b11223", "plain sentence with spaces", ""} {
		if got := goalLogValue(s); got != s {
			t.Errorf("goalLogValue(%q) = %q — plain text must pass through unchanged", s, got)
		}
	}
}
