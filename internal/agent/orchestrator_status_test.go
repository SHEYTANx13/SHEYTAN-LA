package agent

import "testing"

// TestContextPlanStatusContract pins the v1.2.5 status contract (repair
// brief §11B): a plan record can NEVER claim "compressed" when no
// compression happened. The v1.2.4 defect flipped the status to
// "compressed" whenever any automatic adjustment existed — even at
// compressed=0 — so the log contradicted its own counters.
func TestContextPlanStatusContract(t *testing.T) {
	cases := []struct {
		name          string
		compressed    int
		elided        int
		recalled      int
		adjustments   int
		sessionPolicy int
		want          string
	}{
		{"plain turn is raw", 0, 0, 0, 0, 0, "raw"},
		{"real compression wins", 120, 0, 0, 0, 0, "compressed"},
		{"compression beats elision", 120, 300, 0, 0, 0, "compressed"},
		{"elided without compression", 0, 300, 0, 0, 0, "elided"},
		{"recalled without reduction", 0, 0, 4, 0, 0, "recalled"},
		{"adjustment is NOT compression", 0, 0, 0, 1, 0, "adjusted"},
		{"toolset reduction with compressed=0 must be adjusted", 0, 0, 0, 2, 0, "adjusted"},
		{"session policy alone", 0, 0, 0, 0, 8192, "session-policy"},
		{"real reduction beats session policy", 50, 0, 0, 0, 8192, "compressed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contextPlanStatus(
				tc.compressed,
				tc.elided,
				tc.recalled,
				tc.adjustments,
				tc.sessionPolicy,
			)

			if got != tc.want {
				t.Fatalf("contextPlanStatus(%d,%d,%d,%d,%d) = %q, want %q",
					tc.compressed, tc.elided, tc.recalled,
					tc.adjustments, tc.sessionPolicy, got, tc.want)
			}
		})
	}
}
