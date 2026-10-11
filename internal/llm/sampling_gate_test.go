package llm

// sampling_gate_test.go — v1.6.1 P0: the deterministic configuration gate.
//
// THE DEFECT (runtime evidence, v1.6.0): a repeatPenalty of 0 (or any
// non-finite / non-positive value) reached buildArgsWithCaps, was emitted
// as "--repeat-penalty 0", and the engine rejected it with
//
//      error while handling argument "--repeat-penalty":
//      repeat-penalty must be finite and greater than 0
//
// The exit was classified FailOptionLayout ("repairable") and re-fed
// through the compatibility ladder: 2 passes x 4 levels, doubled again by
// the vision-projector retry — a RETRY STORM of engine spawns for a value
// that can never work at ANY compatibility level.
//
// THE CONTRACT PROVED HERE (v1.6.1):
//
//   1. an invalid sampling value NEVER spawns the engine process
//      (launch-count evidence file stays empty);
//   2. Start() refuses with ONE classified, actionable error naming the
//      field, the rule and the fix — no ladder, no repair loop;
//   3. the failure is a deterministic CONFIGURATION failure, never
//      recorded as an accelerator/compatibility downgrade;
//   4. valid-but-unusual user settings still boot normally (no
//      over-validation — the audit only rejects parser-invalid values).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// countLaunches reads the fake engine's launch-count evidence.
func countLaunches(t *testing.T, countFile string) int {
	t.Helper()

	data, err := os.ReadFile(countFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read launch count: %v", err)
	}

	return strings.Count(string(data), "launch\n")
}

// TestEngineStartRefusesInvalidRepeatPenalty proves the P0 contract: the
// engine process is never spawned for a parser-invalid repeat penalty.
func TestEngineStartRefusesInvalidRepeatPenalty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{"zero", 0},
		{"negative", -1.1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := fakeEngineConfig(t, "invalid-arg")
			cfg.LLM.RepeatPenalty = tc.value

			countFile := filepath.Join(t.TempDir(), "launches.txt")
			t.Setenv("SHEYTAN_FAKE_LAUNCH_COUNT", countFile)

			srv := NewLlamaServer(config.NewSource(cfg))
			defer func() { _ = srv.Stop() }()

			err := srv.Start()
			if err == nil {
				t.Fatalf("Start must refuse repeatPenalty=%v", tc.value)
			}

			if got := srv.State(); got != StateFailed {
				t.Fatalf("refused boot must report failed, got %s", got)
			}

			// THE storm proof: zero engine processes spawned.
			if n := countLaunches(t, countFile); n != 0 {
				t.Fatalf("invalid repeatPenalty must NEVER spawn the engine — spawned %d times", n)
			}

			// Actionable diagnostic: names the field, the rule and the fix.
			msg := err.Error()
			for _, want := range []string{"repeatPenalty", "repeat-penalty", "greater than 0"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("error must mention %q for the user to act on, got: %s", want, msg)
				}
			}

			// Deterministic CONFIG classification — not an accelerator or
			// compatibility verdict.
			if strings.Contains(strings.ToLower(msg), "compat") ||
				strings.Contains(strings.ToLower(msg), "accelerator") {
				t.Fatalf("config failure misclassified as compat/accelerator: %s", msg)
			}

			var cfgErr *InvalidSamplingConfigError
			if !asInvalidSamplingConfig(err, &cfgErr) {
				t.Fatalf("error must be *InvalidSamplingConfigError, got %T: %v", err, err)
			}
		})
	}
}

// asInvalidSamplingConfig is the local errors.As helper (kept tiny so the
// test file reads linearly).
func asInvalidSamplingConfig(err error, target **InvalidSamplingConfigError) bool {
	for err != nil {
		if e, ok := err.(*InvalidSamplingConfigError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestEngineStartAcceptsValidSamplingValues pins the other half of the
// contract: values the engine's parser accepts must keep booting — the
// gate exists to reject parser-invalid input, never to reshape valid
// user settings.
func TestEngineStartAcceptsValidSamplingValues(t *testing.T) {
	for _, tc := range []struct {
		name          string
		mutate        func(*config.Config)
		expectBooting bool
	}{
		{"default-penalty", func(c *config.Config) {}, true},
		{"high-penalty", func(c *config.Config) { c.LLM.RepeatPenalty = 2.0 }, true},
		{"sub-1-penalty", func(c *config.Config) { c.LLM.RepeatPenalty = 0.55 }, true},
		{"zero-temp", func(c *config.Config) { c.LLM.Temperature = 0 }, true},
		{"zero-minp-unset", func(c *config.Config) { c.LLM.MinP = 0 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := fakeEngineConfig(t, "invalid-arg")
			tc.mutate(cfg)

			srv := NewLlamaServer(config.NewSource(cfg))
			defer func() { _ = srv.Stop() }()

			if err := srv.Start(); err != nil {
				t.Fatalf("valid sampling values must still boot: %v", err)
			}

			if got := srv.State(); got != StateReady {
				t.Fatalf("engine must reach ready, got %s", got)
			}
		})
	}
}

// TestLaunchArgsRangeValidation covers the second entry point: sampling
// values that arrive through the argument vector itself (user extra args),
// validated by argProblems before any spawn.
func TestLaunchArgsRangeValidation(t *testing.T) {
	caps := defaultCapsForTag("")

	for _, tc := range []struct {
		name    string
		args    []string
		problem string
	}{
		{
			"repeat-penalty zero",
			[]string{"--repeat-penalty", "0"},
			"--repeat-penalty",
		},
		{
			"repeat-penalty negative",
			[]string{"--repeat-penalty", "-1.5"},
			"--repeat-penalty",
		},
		{
			"repeat-penalty NaN",
			[]string{"--repeat-penalty", "NaN"},
			"--repeat-penalty",
		},
		{
			"repeat-penalty Inf",
			[]string{"--repeat-penalty", "Inf"},
			"--repeat-penalty",
		},
		{
			"temp negative",
			[]string{"--temp", "-0.5"},
			"--temp",
		},
		{
			"top-p out of range",
			[]string{"--top-p", "1.5"},
			"--top-p",
		},
		{
			"min-p out of range",
			[]string{"--min-p", "2"},
			"--min-p",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := argProblems(tc.args, caps)
			if len(problems) == 0 {
				t.Fatalf("argProblems must reject %v", tc.args)
			}
			if !strings.Contains(strings.Join(problems, "; "), tc.problem) {
				t.Fatalf("problem must name %q, got %v", tc.problem, problems)
			}
		})
	}

	// Valid ranges must NOT be rejected.
	for _, args := range [][]string{
		{"--repeat-penalty", "1.1"},
		{"--repeat-penalty", "0.55"},
		{"--temp", "0"},
		{"--temp", "2.5"},
		{"--top-p", "1"},
		{"--min-p", "0.05"},
		{"--presence-penalty", "-1.5"},
		{"--frequency-penalty", "1.5"},
	} {
		if problems := argProblems(args, caps); len(problems) > 0 {
			t.Fatalf("argProblems must accept %v, got %v", args, problems)
		}
	}
}
