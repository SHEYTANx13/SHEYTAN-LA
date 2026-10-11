package llm

// perftracker_test.go — v1.1.7: the live performance surface must report
// only REAL measurements, stay bounded, and never average in empty samples.

import (
	"testing"
	"time"
)

func TestPerfTrackerEmptyReportsNoSample(t *testing.T) {
	// Reset the package tracker for a deterministic test.
	enginePerf.mu.Lock()
	enginePerf.samples = nil
	enginePerf.mu.Unlock()

	snap := SnapshotEnginePerf()
	if snap.HasSample {
		t.Fatalf("empty tracker reported HasSample=true — the UI would show a fabricated metric")
	}
	if snap.SamplesAvailable != 0 {
		t.Fatalf("SamplesAvailable = %d, want 0", snap.SamplesAvailable)
	}
}

func TestPerfTrackerAggregatesLastSample(t *testing.T) {
	enginePerf.mu.Lock()
	enginePerf.samples = nil
	enginePerf.mu.Unlock()

	recordPerfSample(PerfSample{
		At: time.Now(), Model: "qwen.gguf",
		PromptTokens: 1000, Tokens: 200,
		TokensPerSec: 25.0, TTFTMs: 630, WallMs: 40000,
	})
	recordPerfSample(PerfSample{
		At: time.Now(), Model: "qwen.gguf",
		PromptTokens: 2000, Tokens: 100,
		TokensPerSec: 20.0, TTFTMs: 500, WallMs: 50000,
	})

	snap := SnapshotEnginePerf()
	if !snap.HasSample {
		t.Fatalf("HasSample=false after real samples")
	}
	if snap.SamplesAvailable != 2 {
		t.Fatalf("SamplesAvailable = %d, want 2", snap.SamplesAvailable)
	}
	// The LAST sample is "current speed".
	if snap.GenTokPerSec != 20.0 {
		t.Fatalf("GenTokPerSec = %v, want 20.0 (last sample)", snap.GenTokPerSec)
	}
	if snap.TTFTSeconds != 0.5 {
		t.Fatalf("TTFTSeconds = %v, want 0.5", snap.TTFTSeconds)
	}
	if snap.PromptTokPerSec != 2000/50.0 {
		t.Fatalf("PromptTokPerSec = %v, want %v", snap.PromptTokPerSec, 2000/50.0)
	}
}

func TestPerfTrackerIgnoresEmptySamples(t *testing.T) {
	enginePerf.mu.Lock()
	enginePerf.samples = nil
	enginePerf.mu.Unlock()

	recordPerfSample(PerfSample{At: time.Now()}) // aborted call: nothing measured
	if s := SnapshotEnginePerf(); s.HasSample {
		t.Fatalf("empty sample recorded — zero-speed fabrications must be dropped")
	}
}

func TestPerfTrackerBounded(t *testing.T) {
	enginePerf.mu.Lock()
	enginePerf.samples = nil
	enginePerf.mu.Unlock()

	for i := 0; i < maxPerfSamples+10; i++ {
		recordPerfSample(PerfSample{
			At: time.Now(), Tokens: 10, TokensPerSec: float64(i),
		})
	}

	enginePerf.mu.Lock()
	n := len(enginePerf.samples)
	enginePerf.mu.Unlock()

	if n != maxPerfSamples {
		t.Fatalf("ring grew to %d samples, want bounded %d", n, maxPerfSamples)
	}
}
