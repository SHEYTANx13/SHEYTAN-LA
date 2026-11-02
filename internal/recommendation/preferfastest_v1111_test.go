package recommendation

// preferfastest_v1111_test.go — v1.11.1: the speed-priority preference
// (Input.PreferFastest, config key preferFastest, default ON) inside the
// ONE recommendation authority. The contract under test:
//
//   - ON: the flash-attention profile takes the speed-oriented KV choice
//     (q8_0 — halved KV memory, more offload headroom) on ANY task, with
//     an explainable reason line;
//   - OFF: the balanced policy is unchanged (chat/research on a GPU-less
//     machine keep f16; maximum/low-power/GPU-present keep q8_0 as before);
//   - the speed preference NEVER forces settings past the measured
//     limits: the resource verdict ladder still clamps the context, the
//     GPU posture stays evidence-gated, flash attention still requires a
//     VERIFIED engine, and MANUAL mode is not this engine's concern (the
//     caller's apply step only runs under AUTO — pinned in the api tests).
import (
	"strings"
	"testing"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/hardware"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/llm"
	"github.com/SHEYTANx13/SHEYTAN-LA/internal/vision"
)

func cpuOnlyInput() Input {
	in := baseInput()
	in.HW = hardware.Profile{
		OS:   "windows",
		Arch: "amd64",
		CPU:  hardware.CPU{Name: "Test CPU", PhysicalCores: 8, LogicalCores: 16},
		RAM:  hardware.RAM{TotalBytes: gb(16), AvailableBytes: gb(12)},
	}
	in.Vision = vision.Evaluation{State: vision.StateMissing}
	return in
}

func TestPreferFastestTakesQ8KVOnAnyTask(t *testing.T) {
	in := cpuOnlyInput() // chat + CPU-only + flash-attn verified: the balanced policy would keep f16
	in.PreferFastest = true

	rec := Recommend(in)
	if rec.KVCacheQuant != "q8_0" {
		t.Fatalf("PreferFastest=ON must take the speed-oriented q8_0 KV cache on the chat profile (CPU-only machine), got %q", rec.KVCacheQuant)
	}
	found := false
	for _, r := range rec.Reasons {
		if strings.Contains(r, "fastest-safe preference") {
			found = true
		}
	}
	if !found {
		t.Fatal("the speed-priority decision must be explainable (reason line missing)")
	}
}

func TestPreferFastestOffKeepsBalancedPolicy(t *testing.T) {
	in := cpuOnlyInput()
	in.PreferFastest = false

	rec := Recommend(in)
	if rec.KVCacheQuant != "f16" {
		t.Fatalf("PreferFastest=OFF on the chat profile (CPU-only) must keep the balanced f16 KV cache, got %q", rec.KVCacheQuant)
	}
	for _, r := range rec.Reasons {
		if strings.Contains(r, "fastest-safe preference") {
			t.Fatal("the speed-priority reason must not appear when the preference is off")
		}
	}
}

func TestPreferFastestKeepsVerifiedEvidenceGates(t *testing.T) {
	// Flash attention UNVERIFIED: the speed preference must NOT turn it on.
	in := cpuOnlyInput()
	in.PreferFastest = true
	in.Engine = &llm.EngineCaps{FlashAttnEnabled: false, Tag: "b9999"}

	rec := Recommend(in)
	if rec.FlashAttention {
		t.Fatal("PreferFastest must never enable flash attention without the engine's VERIFIED support")
	}
	if rec.KVCacheQuant == "q8_0" {
		t.Fatal("KV quantization shrink still requires flash attention — the speed preference cannot bypass the evidence gate")
	}

	// GPU posture stays evidence-gated: no GPU + no Vulkan backend → the
	// launch-time gate posture, never a claimed offload.
	in2 := cpuOnlyInput()
	in2.PreferFastest = true
	rec2 := Recommend(in2)
	if rec2.GPULayers != 0 {
		t.Fatalf("CPU-only machine must keep GPULayers=0 regardless of the speed preference, got %d", rec2.GPULayers)
	}
	if !rec2.GPUAutoOffload {
		t.Fatal("the AUTO posture flag must stay automatic (never a pinned off)")
	}
}

func TestPreferFastestStillClampedByMeasuredMemory(t *testing.T) {
	// A 12 GB model on a 16 GB machine with a 32k task context: the
	// resource ladder must clamp the context even with the preference ON.
	in := cpuOnlyInput()
	in.PreferFastest = true
	in.Task = TaskResearch // 32768-token preference
	in.Card = &llm.ModelCard{Arch: "llama", ContextLength: 131072, SizeBytes: gb(12)}

	rec := Recommend(in)
	if rec.Context > 16384 {
		t.Fatalf("the resource verdict ladder must still clamp the context under the speed preference — got %d", rec.Context)
	}
}

func TestPreferFastestDefaultOffInZeroInputIsExplicit(t *testing.T) {
	// The Input zero value (PreferFastest=false) is the BALANCED policy —
	// the ON default lives in config.Default(), never in this package.
	in := cpuOnlyInput()
	if in.PreferFastest {
		t.Fatal("zero Input must carry the explicit balanced policy (the ON default is config's concern)")
	}
	rec := Recommend(in)
	if rec.KVCacheQuant != "f16" {
		t.Fatalf("zero Input (balanced) on the chat profile must keep f16, got %q", rec.KVCacheQuant)
	}
}
