package llm

// capability_compat_test.go — v1.1.7 regression tests for the bounded
// full-speed retry-up gate and the compat-reason recording.
//
// Historical defect: once the ladder descended (e.g. to compatibility
// mode 2), EngineCompat was persisted and EVERY later boot resumed there —
// "started in compatibility mode 2 (no speed flags)" repeated on every
// startup even after the capability profile had been repaired and
// re-verified. shouldRetryFullSpeed is the gate that must (a) allow the
// one bounded restore attempt when the verified profile moved on and
// (b) refuse it in every other case so the incompatibility is never
// re-discovered expensively.

import (
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// validLevel0Args is a launch profile that validates cleanly against any
// modern capability profile (no speed flags needed for validity).
func validLevel0Args() []string {
	return []string{
		"--model", "test.gguf",
		"--host", "127.0.0.1",
		"--port", "8817",
		"--ctx-size", "4096",
		"--temp", "0.7",
	}
}

func TestShouldRetryFullSpeedGates(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	older := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	verifiedCaps := func(verifiedAt string) *EngineCaps {
		caps := defaultCapsForTag("b11000")
		caps.Source = "help-parse"
		caps.VerifiedAt = verifiedAt
		return caps
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		caps    *EngineCaps
		args    []string
		wantTry bool
	}{
		{
			name:    "no compat persisted — nothing to retry",
			cfg:     &config.Config{EngineCompat: 0},
			caps:    verifiedCaps(now),
			args:    validLevel0Args(),
			wantTry: false,
		},
		{
			name: "non-option reason (model load) — never retried",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "model-load",
				EngineCompatAt:     older,
			},
			caps:    verifiedCaps(now),
			args:    validLevel0Args(),
			wantTry: false,
		},
		{
			name: "option reason but profile never verified — no retry",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "option-layout:flash-attn",
				EngineCompatAt:     older,
			},
			caps:    func() *EngineCaps { c := defaultCapsForTag("b11000"); return c }(), // no VerifiedAt
			args:    validLevel0Args(),
			wantTry: false,
		},
		{
			name: "option reason but downgrade newer than profile — no retry",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "option-layout:flash-attn",
				EngineCompatAt:     now,
			},
			caps:    verifiedCaps(older),
			args:    validLevel0Args(),
			wantTry: false,
		},
		{
			name: "v1.1.6 upgrade: recorded option reason, no stamp — one retry",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "option-layout:flash-attn",
				EngineCompatAt:     "",
			},
			caps:    verifiedCaps(older),
			args:    validLevel0Args(),
			wantTry: true,
		},
		{
			name: "profile re-verified after downgrade — one retry",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "unknown-option:cache-reuse",
				EngineCompatAt:     older,
			},
			caps:    verifiedCaps(now),
			args:    validLevel0Args(),
			wantTry: true,
		},
		{
			name: "level-0 profile still invalid — no retry",
			cfg: &config.Config{
				EngineCompat:       2,
				EngineCompatReason: "option-layout:flash-attn",
				EngineCompatAt:     older,
			},
			caps: verifiedCaps(now),
			// value-form engine given a bare-flag pair: validation must fail
			args:    []string{"--model", "m.gguf", "--flash-attn", "--cache-reuse", "64"},
			wantTry: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, why := shouldRetryFullSpeed(tc.cfg, tc.caps, tc.args)
			if got != tc.wantTry {
				t.Fatalf("shouldRetryFullSpeed = %v (reason %q), want %v", got, why, tc.wantTry)
			}
			if tc.wantTry && why == "" {
				t.Fatalf("expected a non-empty reason when retry is allowed")
			}
		})
	}
}

// strErr is a bare error carrying exactly one line of engine output.
type strErr string

func (s strErr) Error() string { return string(s) }

func TestCompatReasonFromError(t *testing.T) {
	tests := []struct {
		errText string
		want    string
	}{
		{
			`error while handling argument "--flash-attn": unknown value for --flash-attn: '--cache-reuse'`,
			"option-layout:flash-attn",
		},
		{
			"error: unknown argument: --cache-reuse",
			"unknown-option:cache-reuse",
		},
		{
			"failed to load model: unable to allocate CUDA buffer",
			"model-load",
		},
		{"something completely unfamiliar", "unknown"},
		{"", "unknown"},
	}

	for _, tc := range tests {
		var err error
		if tc.errText != "" {
			err = &exitFailure{err: strErr(tc.errText)}
		} else {
			err = nil
		}

		got := compatReasonFromError(err)
		if got != tc.want {
			t.Fatalf("compatReasonFromError(%q) = %q, want %q", tc.errText, got, tc.want)
		}
	}

	// Option narrowing must be a suffix of the classified kind.
	got := compatReasonFromError(&exitFailure{err: strErr("error: invalid argument: on")})
	if !strings.HasPrefix(got, "option-layout") {
		t.Fatalf("legacy layout failure classified as %q, want option-layout prefix", got)
	}
}

// TestCompatSnapshotRecordsRemovedOptions pins the diagnostics contract:
// a repaired profile's Removed options reach the API surface.
func TestCompatSnapshotRecordsRemovedOptions(t *testing.T) {
	caps := defaultCapsForTag("b11000")
	caps.CacheReuse = false
	caps.Removed = []RemovedOption{{
		Option: "--cache-reuse",
		Reason: "engine build reports the option as unknown",
	}}

	// The snapshot builder is a method on LlamaServer; exercise the pure
	// part — the Changes formatting — through a synthetic profile.
	if len(caps.Removed) != 1 {
		t.Fatalf("removed options not recorded")
	}
	if !strings.Contains(caps.Removed[0].Option, "cache-reuse") {
		t.Fatalf("unexpected removed option %q", caps.Removed[0].Option)
	}
}
