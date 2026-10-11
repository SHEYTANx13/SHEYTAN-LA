package llm

// extraargs_gate_v162_test.go — v1.6.2: the sampling-contract audit
// closure for the RAW USER EXTRA ARGUMENTS (llamaExtraArgs).
//
// The v1.6.1 gate closed the first-class fields; the audit of every
// argument actually emitted by buildArgsWithCaps found the remaining
// passthrough: strings.Fields(cfg.LlamaExtraArgs) reached the engine
// verbatim, so a malformed numeric argument ("--temp abc") spawned the
// engine, failed deterministically in the ENGINE's parser, and fed the
// compatibility ladder / repair machinery — including accelerator
// downgrades for what is a config typo.
//
// Contract under test:
//   - a malformed numeric extra argument refuses the launch BEFORE any
//     engine process exists (zero spawns — the same storm proof);
//   - the failure is classified InvalidSamplingConfigError (deterministic
//     config failure — never compat/accelerator);
//   - VALID user values (including unusual-but-valid flags and unknown
//     flags) pass through unchanged.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func TestEngineStartRefusesMalformedNumericExtraArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args string
		want []string
	}{
		{"non-numeric temp", "--temp abc", []string{"--temp", "finite number"}},
		{"nan temp", "--temp nan", []string{"--temp", "finite"}},
		{"inf top-p", "--top-p inf", []string{"top-p"}},
		{"out-of-range top-p", "--top-p 1.5", []string{"top-p", "between 0 and 1"}},
		{"zero repeat-penalty", "--repeat-penalty 0", []string{"repeat-penalty", "greater than 0"}},
		{"negative temp", "--temp -0.5", []string{"temperature"}},
		{"non-integer top-k", "--top-k 4.5", []string{"--top-k", "integer"}},
		{"non-integer threads", "--threads many", []string{"--threads", "integer"}},
		{"equals-form malformed", "--temp=abc", []string{"--temp", "finite"}},
		{"equals-form out-of-range", "--min-p=2", []string{"min-p", "between 0 and 1"}},
		{"missing value at end", "--ctx-size", []string{"--ctx-size", "missing its value"}},
		{"non-integer seed", "--seed xyz", []string{"--seed", "integer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := fakeEngineConfig(t, "invalid-arg")
			cfg.LlamaExtraArgs = tc.args

			countFile := filepath.Join(t.TempDir(), "launches.txt")
			t.Setenv("SHEYTAN_FAKE_LAUNCH_COUNT", countFile)

			srv := NewLlamaServer(config.NewSource(cfg))
			defer func() { _ = srv.Stop() }()

			err := srv.Start()
			if err == nil {
				t.Fatalf("Start must refuse extra args %q", tc.args)
			}

			// THE storm proof: zero engine processes spawned.
			if n := countLaunches(t, countFile); n != 0 {
				t.Fatalf("malformed extra args must NEVER spawn the engine — spawned %d times", n)
			}

			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("error must mention %q, got: %s", want, msg)
				}
			}

			// Deterministic CONFIG classification — no compat, no accelerator.
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "compat") || strings.Contains(lower, "accelerator") {
				t.Fatalf("config failure misclassified as compat/accelerator: %s", msg)
			}

			var cfgErr *InvalidSamplingConfigError
			if !asInvalidSamplingConfig(err, &cfgErr) {
				t.Fatalf("error must be *InvalidSamplingConfigError, got %T: %v", err, err)
			}
		})
	}
}

// Valid values — including unusual-but-valid ones and flags SHEYTAN does
// not manage — must pass through UNCHANGED (the gate never reshapes
// valid user input; the engine's parser stays the authority for unknown
// flags).
func TestValidateExtraArgsAcceptsValidValues(t *testing.T) {
	for _, args := range []string{
		"",
		"   ",
		"--temp 0.7",
		"--temp 2.5",           // unusual but valid (finite, >= 0)
		"--temp=0.9",           // equals form
		"--top-k 40",
		"--top-k 0",
		"--repeat-penalty 1.1",
		"--presence-penalty -0.5", // negative penalties are valid
		"--frequency-penalty 0.5",
		"--min-p 0.05",
		"--mirostat 2 --mirostat-tau 5.0 --mirostat-eta 0.1",
		"--threads 8 --threads-batch 16",
		"--n-gpu-layers 99",
		"--seed 42",
		"--cache-reuse 512",
		"--n-predict -1",
		"--draft-max 16",
		"--ctx-size 8192 --batch-size 512",
		"--some-unknown-flag value",            // engine authority, not ours
		"--another-unknown=whatever",
		"--mlock --jinja --no-webui",           // non-numeric flags untouched
		"--verbose",                            // bare unknown flag
	} {
		if problems := config.ValidateExtraArgs(args); len(problems) > 0 {
			t.Errorf("ValidateExtraArgs(%q) must accept, got %v", args, problems)
		}
	}
}

func TestValidateExtraArgsRejectsKnownBadValues(t *testing.T) {
	for _, tc := range []struct {
		args string
		want string
	}{
		{"--temp abc", "--temp"},
		{"--temp nan", "finite"},
		{"--temp inf", "finite"},
		{"--temp -1", "at least 0"},
		{"--top-p 1.5", "between 0 and 1"},
		{"--min-p -0.1", "between 0 and 1"},
		{"--repeat-penalty 0", "greater than 0"},
		{"--repeat-penalty -1", "greater than 0"},
		{"--top-k abc", "integer"},
		{"--top-k 1.5", "integer"},
		{"--threads x", "integer"},
		{"--ctx-size", "missing"},
		{"--temp", "missing"},
		{"--temp=nan", "finite"},
		{"--mirostat-tau -1", "at least 0"},
	} {
		problems := config.ValidateExtraArgs(tc.args)
		if len(problems) == 0 {
			t.Errorf("ValidateExtraArgs(%q) must reject", tc.args)
			continue
		}
		if tc.want != "" && !strings.Contains(problems[0].String(), tc.want) {
			t.Errorf("ValidateExtraArgs(%q) problem must mention %q, got %v", tc.args, tc.want, problems[0])
		}
	}
}

// The launch arguments themselves must carry the extra args verbatim
// when they are valid (no silent rewriting of user input).
func TestBuildArgsCarriesValidExtraArgsVerbatim(t *testing.T) {
	cfg, _ := fakeEngineConfig(t, "")
	cfg.LlamaExtraArgs = "--temp 1.7 --some-future-flag on"

	srv := NewLlamaServer(config.NewSource(cfg))

	args := srv.buildArgs(cfg, "model.gguf", 2)

	found := false
	for i, a := range args {
		if a == "--temp" && i+1 < len(args) && args[i+1] == "1.7" {
			found = true
		}
	}
	if !found {
		t.Fatalf("valid extra args must be carried verbatim: %v", args)
	}

	hasFuture := false
	for _, a := range args {
		if a == "--some-future-flag" {
			hasFuture = true
		}
	}
	if !hasFuture {
		t.Fatalf("unknown extra flags must pass through untouched: %v", args)
	}
}
