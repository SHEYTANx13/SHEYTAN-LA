package logging

import (
	"strings"
	"testing"
)

// TestRedactIdentityTokens pins the v1.8.2 human-facing redaction rules:
// opaque run/session tokens disappear, everything diagnostic survives.
func TestRedactIdentityTokens(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "runId token removed",
			in:   "run settled error runId=abc123 in 57ms",
			want: "run settled error in 57ms",
		},
		{
			name: "runID token removed",
			in:   "runID=deadbeef paused: checkpoint kept",
			want: "paused: checkpoint kept",
		},
		{
			name: "session token removed",
			in:   "runId=abc paused: checkpoint kept, session=s123",
			want: "paused: checkpoint kept",
		},
		{
			name: "sessionId token removed",
			in:   "attached sessionId=abc-123 ready",
			want: "attached ready",
		},
		{
			name: "sessionID token removed",
			in:   "sessionID=xyz stream bound",
			want: "stream bound",
		},
		{
			name: "prose containing the word session survives",
			in:   "session context window narrowed to 8192 tokens",
			want: "session context window narrowed to 8192 tokens",
		},
		{
			name: "mid-word key matches never fire",
			in:   "checkpoint mysession=abc survived intact",
			want: "checkpoint mysession=abc survived intact",
		},
		{
			name: "URL path survives; only the identity token goes",
			in:   "dial ws://127.0.0.1:9000/ws/activity?sessionId=abc&v=2 failed",
			want: "dial ws://127.0.0.1:9000/ws/activity?&v=2 failed",
		},
		{
			name: "plain path with session= prefix value is redacted",
			in:   "stored under session=abc123 done",
			want: "stored under done",
		},
		{
			name: "internal storage filename survives (no token boundary)",
			in:   "wrote sessions/abc123.json checkpoint",
			want: "wrote sessions/abc123.json checkpoint",
		},
		{
			name: "durations and causes survive",
			in:   "runId=e1 classified kind=chat complexity=8 in 12ms",
			want: "classified kind=chat complexity=8 in 12ms",
		},
		{
			name: "token at string start removed",
			in:   "session=abc only",
			want: "only",
		},
		{
			name: "parenthesised token removed cleanly",
			in:   "run aborted (runId=abc123) by user",
			want: "run aborted by user",
		},
		{
			name: "multiple tokens removed",
			in:   "runId=a session=b both gone",
			want: "both gone",
		},
		{
			name: "non-identity keys survive",
			in:   "model=gemma-4-E2B-it-Q4_K_M.gguf ctx=8192",
			want: "model=gemma-4-E2B-it-Q4_K_M.gguf ctx=8192",
		},
		{
			name: "empty string safe",
			in:   "",
			want: "",
		},
		{
			name: "no equals sign short-circuits",
			in:   "plain message without tokens",
			want: "plain message without tokens",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactIdentityTokens(tc.in)

			if got != tc.want {
				t.Fatalf("redact(%q) = %q, want %q", tc.in, got, tc.want)
			}

			// Idempotence: redacting twice must not change the result.
			if again := RedactIdentityTokens(got); again != got {
				t.Fatalf("redact not idempotent: %q -> %q", got, again)
			}
		})
	}
}

// TestRedactPreservesDiagnosticFields is the negative-space proof: after
// redaction the line still carries timestamps-equivalents, severity-
// bearing words, subsystems, durations and error causes.
func TestRedactPreservesDiagnosticFields(t *testing.T) {
	in := "runId=abc123 engine staged b11273 in 1.2s: probe failed (timeout)"
	got := RedactIdentityTokens(in)

	for _, want := range []string{"engine staged b11273", "1.2s", "probe failed", "(timeout)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted line lost %q: %q", want, got)
		}
	}

	if strings.Contains(got, "runId=") {
		t.Fatalf("redacted line still carries runId=: %q", got)
	}
}
