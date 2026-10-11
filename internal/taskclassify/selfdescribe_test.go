package taskclassify

import "testing"

// v1.8.2 CAPABILITY-INTENT DETECTION: the deterministic self-describe
// signal must fire on the real phrasings a user asks, and must NOT fire
// on ordinary requests that merely contain related words.
func TestClassifySelfDescribeIntent(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"what tools do you have?", true},
		{"what tools do you have and what capabilities do you have?", true},
		{"what capabilities do you have", true},
		{"what can you do?", true},
		{"what can you access", true},
		{"what model are you running?", true},
		{"which model are you", true},
		{"list your tools", true},
		{"what hardware are you running on", true},
		{"what engine are you", true},
		// Negative space: ordinary requests never carry the signal.
		{"hi", false},
		{"fix the build error in main.go", false},
		{"search the web for llama.cpp news", false},
		{"remember that my name is Parsa", false},
		{"analyze the data in this csv", false},
		{"what is the capital of France?", false},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			p := Classify(tc.query, ClassifyOptions{})

			if p.Signals.SelfDescribe != tc.want {
				t.Fatalf("Classify(%q).Signals.SelfDescribe = %v, want %v (kind=%s reason=%s)",
					tc.query, p.Signals.SelfDescribe, tc.want, p.Kind, p.Reason)
			}
		})
	}
}

// A capability question stays CHEAP: the signal adds no complexity, so a
// one-line capability question classifies like the casual chat it is.
func TestSelfDescribeAddsNoComplexity(t *testing.T) {
	with := Classify("what tools do you have?", ClassifyOptions{})
	plain := Classify("hi", ClassifyOptions{})

	if with.Complexity > plain.Complexity+10 {
		t.Fatalf("capability question complexity %d vs plain %d — the signal must not deepen the tier",
			with.Complexity, plain.Complexity)
	}
}
