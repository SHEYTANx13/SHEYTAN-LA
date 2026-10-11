package netcheck

// netcheck_diag_test.go — v1.1.7: the plain-language diagnosis must map
// measured facts onto the five states deterministically.

import "testing"

func TestClassifyDiag(t *testing.T) {
	tests := []struct {
		name    string
		dns     bool
		https   bool
		l1, l2  float64
		want    DiagState
		wantMsg bool // is a first-failure reason expected?
	}{
		{"offline: both fail", false, false, 0, 0, DiagOffline, true},
		{"offline: dns ok, https blocked", true, false, 0, 0, DiagOffline, true},
		{"unstable: https ok, dns broken", false, true, 120, 130, DiagUnstable, true},
		{"slow: 1.6 s latency", true, true, 1600, 1650, DiagSlow, true},
		{"good: 800 ms latency", true, true, 800, 780, DiagGood, false},
		{"excellent: 90 ms latency", true, true, 90, 95, DiagExcellent, false},
		{"unstable: wild jitter", true, true, 60, 900, DiagUnstable, true},
		{"stable fast second sample", true, true, 700, 120, DiagExcellent, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, reason := classifyDiag(tc.dns, tc.https, tc.l1, tc.l2)
			if state != tc.want {
				t.Fatalf("state = %q, want %q (reason %q)", state, tc.want, reason)
			}
			if tc.wantMsg && reason == "" {
				t.Fatalf("expected a first-failure reason for %q", state)
			}
			if !tc.wantMsg && reason != "" {
				t.Fatalf("unexpected reason %q for %q", reason, state)
			}
		})
	}
}

func TestDiagStatesVocabulary(t *testing.T) {
	// The UI contract: exactly these five plain-language states.
	want := map[DiagState]bool{
		DiagExcellent: true,
		DiagGood:      true,
		DiagUnstable:  true,
		DiagSlow:      true,
		DiagOffline:   true,
	}
	if len(want) != 5 {
		t.Fatalf("state vocabulary drifted: %v", want)
	}
}

func TestFirstErrLineTruncates(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "x"
	}
	// The newline is stripped first, so the 100-char prefix survives whole.
	if got := firstErrLine(errString(long + "\nmore")); got != long {
		t.Fatalf("firstErrLine = %d chars, want the 100-char first line", len(got))
	}
	// A single line longer than the cap is truncated to 160.
	flat := ""
	for i := 0; i < 300; i++ {
		flat += "y"
	}
	if got := firstErrLine(errString(flat)); len(got) != 160 {
		t.Fatalf("firstErrLine length = %d, want 160", len(got))
	}
	if got2 := firstErrLine(errString("short\nnewline")); got2 != "short" {
		t.Fatalf("firstErrLine = %q, want %q", got2, "short")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
