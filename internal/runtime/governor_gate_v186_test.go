package runtime

// governor_gate_v186_test.go — v1.8.6 Phase 2: the resource-aware run
// gate + inference-footprint wiring on the Stack (§6/§7).
//
// THE CONTRACT:
//
//   - the Stack wires the inference footprint source into the Governor
//     (model-card file facts + planned KV at the serving window);
//   - an unknown footprint (no model / unreadable card) stays unknown;
//   - the run gate consults the Governor ONLY on a measured state:
//     pressure defers the model load with the honest reason, a healthy
//     envelope admits it, and an UNMEASURED Governor falls through to
//     the preflight gate exactly as before (no behavior change for
//     unwired/early-boot paths).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/preflight"
)

// writeMinimalGGUF writes a plausible minimal GGUF (the same shape the
// llm import tests use — magic + version, zero tensors/kv).
func writeMinimalGGUF(t *testing.T, path string, size int) {
	t.Helper()

	buf := make([]byte, size)
	copy(buf, "GGUF")
	buf[4] = 3 // version
	buf[8] = 0 // tensor count
	buf[12] = 0 // kv count

	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gateStack(t *testing.T, modelSize int) *Stack {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	if err := os.MkdirAll(cfg.ModelsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if modelSize > 0 {
		writeMinimalGGUF(t, filepath.Join(cfg.ModelsDir, "qwen2-1.5b-q4.gguf"), modelSize)
		cfg.Model = "qwen2-1.5b-q4.gguf"
	}

	stack := NewStack(cfg)
	stack.StartGovernor()

	t.Cleanup(stack.Close)

	return stack
}

func TestStackWiresInferenceFootprintSource(t *testing.T) {
	stack := gateStack(t, 64<<20)

	g := stack.Governor()
	if g == nil {
		t.Fatal("the Governor must be wired")
	}

	// The footprint source is wired: a readable card + file size produces
	// a MEASURED footprint (file-fact weights + planned KV).
	inf := stack.inferenceFootprint()
	if !inf.Known {
		t.Fatalf("the footprint must be measurable for a real GGUF, got %+v", inf)
	}

	if inf.ModelBytes != int64(64<<20) {
		t.Fatalf("model bytes = %d, want the file size %d", inf.ModelBytes, 64<<20)
	}

	if inf.KVBytes < 0 {
		t.Fatalf("KV bytes must never be negative, got %d", inf.KVBytes)
	}
}

func TestInferenceFootprintUnknownWithoutModel(t *testing.T) {
	stack := gateStack(t, 0) // no model selected

	if inf := stack.inferenceFootprint(); inf.Known {
		t.Fatalf("no model — the footprint must stay unknown, got %+v", inf)
	}
}

func TestGovernorAdmitsModelLoadHealthyEnvelope(t *testing.T) {
	stack := gateStack(t, 64<<20)

	// A generous measured state: 32 GiB, 90% available, OK pressure.
	stack.Governor().Observe(preflight.Sample{
		RAMTotalBytes:     32 << 30,
		RAMAvailableBytes: 29 << 30,
		ProcRSSBytes:      1 << 30,
		Level:             preflight.PressureOK,
	})

	if err := stack.GovernorAdmitsModelLoad(); err != nil {
		t.Fatalf("a healthy envelope must admit the model load: %v", err)
	}
}

func TestGovernorDefersModelLoadUnderSustainedPressure(t *testing.T) {
	stack := gateStack(t, 64<<20)

	// High pressure: the envelope refuses heavyweight admission. The
	// Governor's own sustained window needs 60s before the hard refusal —
	// fold the sample twice with the clock advanced through the state
	// itself (Sustained grows on repeated Observe calls).
	for i := 0; i < 2; i++ {
		stack.Governor().Observe(preflight.Sample{
			RAMTotalBytes:     32 << 30,
			RAMAvailableBytes: 1 << 30,
			ProcRSSBytes:      1 << 30,
			Level:             preflight.PressureHigh,
		})
	}

	err := stack.GovernorAdmitsModelLoad()
	if err == nil {
		t.Fatal("sustained high pressure must defer the model load")
	}

	if !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("the deferral must be truthful and explainable: %v", err)
	}
}

func TestGovernorGateFallsThroughWhenUnmeasured(t *testing.T) {
	stack := gateStack(t, 64<<20)

	// NO monitor samples folded: the Governor state is unmeasured — the
	// gate falls through (the preflight gate remains the authority).
	if err := stack.GovernorAdmitsModelLoad(); err != nil {
		t.Fatalf("an unmeasured Governor state must not block the run gate: %v", err)
	}
}

func TestGovernorGateFallsThroughWithoutPlan(t *testing.T) {
	stack := gateStack(t, 0) // no model → no computable plan

	stack.Governor().Observe(preflight.Sample{
		RAMTotalBytes:     32 << 30,
		RAMAvailableBytes: 29 << 30,
		Level:             preflight.PressureOK,
	})

	// No model selected means the run gate fails LATER with the honest
	// ErrNoModelSelected from the model-first gate — the governor gate
	// itself does not invent a refusal for an unknowable plan.
	if err := stack.GovernorAdmitsModelLoad(); err != nil {
		t.Fatalf("no computable plan must fall through (nil), got: %v", err)
	}
}
