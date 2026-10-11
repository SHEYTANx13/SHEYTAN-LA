package engine

// phase5_bench_test.go — MEASURED native performance evidence (Phase 5
// requirement: measured evidence, never implied speed).
//
// Run explicitly:
//
//	go test -tags headless ./internal/native/engine/ -run TestPhase5Benchmark -count=1 -v
//
// Numbers reported (all measured; durations from the C++ engine's
// monotonic clock, wall time from Go):
//
//	model load time, prompt processing, TTFT, decode tok/s, KV capacity
//	and used bytes, generated token count, host RSS.
//
// The llama.cpp baseline comparison for the same fixture is recorded in
// worklog.md (measured with llama-cli on the same machine when the
// toolchain was available; skipped-with-reason otherwise).

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPhase5Benchmark measures the native engine on the real fixtures.
// Not a pass/fail performance gate: it prints the measured evidence and
// sanity-checks that the measurements exist.
func TestPhase5Benchmark(t *testing.T) {
	bin := realHostBinaryPath()
	if !fileExists(bin) {
		t.Skipf("C++ host binary not built (%s); build native/engine with CMake to enable", bin)
	}

	for _, fixture := range []string{"tiny-llama-f32.gguf", "tiny-llama-slow.gguf"} {
		t.Run(fixture, func(t *testing.T) {
			e := New(bin)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			if err := e.Start(ctx); err != nil {
				t.Fatalf("start: %v", err)
			}
			t.Cleanup(func() {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer stopCancel()
				_ = e.Stop(stopCtx)
			})

			// --- model load time (measured) --------------------------------
			loadStart := time.Now()
			loadRealFixture(t, e, fixture)
			loadDuration := time.Since(loadStart)

			// --- generation (measured by the C++ engine) -------------------
			const maxTokens = 32

			genStart := time.Now()
			var chunks int

			result, err := e.StreamGeneration(ctx, GenerationRequest{
				RequestID: "bench-1",
				Prompt:    "hello",
				MaxTokens: maxTokens,
				Seed:      42,
			}, func(chunk GenerationChunk) error {
				chunks++
				return nil
			})
			wall := time.Since(genStart)

			if err != nil {
				t.Fatalf("generation: %v", err)
			}

			// --- KV + metrics snapshots ------------------------------------
			kv, kvErr := e.KVCacheInfo(ctx)
			if kvErr != nil {
				t.Fatalf("kv info: %v", kvErr)
			}
			m, mErr := e.MetricsSnapshot(ctx)

			t.Logf("[%s] MEASURED (native engine):", fixture)
			t.Logf("  model load time:        %v", loadDuration)
			t.Logf("  prompt tokens:          %d", result.PromptTokens)
			t.Logf("  generated tokens:       %d", result.GeneratedTokens)
			t.Logf("  finish reason:          %s", result.FinishReason)
			t.Logf("  prompt processing:      %.3f s (%.1f tok/s prompt)",
				result.Metrics.PromptSeconds, result.Metrics.PromptTokensPerSecond)
			t.Logf("  TTFT (C++ measured):    %.4f s", result.Metrics.TTFTSeconds)
			t.Logf("  decode (C++ measured):  %.3f s (%.2f tok/s decode)",
				result.Metrics.DecodeSeconds, result.Metrics.TokensPerSecond)
			t.Logf("  total (C++ measured):   %.3f s", result.Metrics.TotalSeconds)
			t.Logf("  wall (Go measured):     %v (%d chunks)", wall, chunks)
			t.Logf("  KV capacity:            %d bytes", kv.CapacityBytes)
			t.Logf("  KV used:                %d bytes (%d positions)",
				kv.UsedBytes, kv.UsedPositions)
			if mErr == nil {
				t.Logf("  host RSS:               %d bytes", m.ProcessRSSBytes)
			}

			// Sanity on the MEASUREMENT (not performance thresholds).
			if result.GeneratedTokens == 0 {
				t.Fatalf("no tokens generated — measurement invalid")
			}
			if result.Metrics.TTFTSeconds <= 0 {
				t.Fatalf("TTFT not measured")
			}
			if result.Metrics.TokensPerSecond <= 0 {
				t.Fatalf("decode tok/s not measured")
			}
			if kv.CapacityBytes == 0 || kv.UsedBytes == 0 {
				t.Fatalf("KV accounting missing: %+v", kv)
			}
		})
	}
}

// TestPhase5BenchmarkHostProcessRSS measures the host process RSS via
// /proc (Linux) for the evidence table.
func TestPhase5BenchmarkHostProcessRSS(t *testing.T) {
	bin := realHostBinaryPath()
	if !fileExists(bin) {
		t.Skipf("C++ host binary not built (%s)", bin)
	}

	e := New(bin)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = e.Stop(stopCtx)
	})

	pid := e.Pid()
	if pid == 0 {
		t.Skip("no pid")
	}

	rssIdle := procRSSKB(t, pid)

	loadRealFixture(t, e, "tiny-llama-f32.gguf")

	_, err := e.StreamGeneration(ctx, GenerationRequest{
		RequestID: "rss-1",
		Prompt:    "hello",
		MaxTokens: 8,
	}, func(chunk GenerationChunk) error { return nil })
	if err != nil {
		t.Fatalf("generation: %v", err)
	}

	rssAfter := procRSSKB(t, pid)

	t.Logf("MEASURED host RSS: idle=%d KB, after load+generate=%d KB (delta %d KB)",
		rssIdle, rssAfter, rssAfter-rssIdle)
}

// procRSSKB reads VmRSS from /proc/<pid>/status (Linux).
func procRSSKB(t *testing.T, pid int) int {
	t.Helper()

	//nolint:gosec // /proc path built from a pid we own.
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Skipf("/proc not available: %v", err)
	}

	sc := bufio.NewScanner(newSR(string(data)))
	for sc.Scan() {
		line := sc.Text()
		var kb int
		if n, _ := fmt.Sscanf(line, "VmRSS: %d kB", &kb); n == 1 {
			return kb
		}
	}
	return 0
}

// newSR wraps strings.Reader (local alias to keep imports tidy).
func newSR(s string) *strings.Reader { return strings.NewReader(s) }
