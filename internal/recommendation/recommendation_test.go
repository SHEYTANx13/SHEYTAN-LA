package recommendation

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

func gb(n int) int64 { return int64(n) << 30 }

func baseInput() Input {
	return Input{
		HW: hardware.Profile{
			OS:   "windows",
			Arch: "amd64",
			CPU:  hardware.CPU{Name: "Test CPU", PhysicalCores: 8, LogicalCores: 16},
			RAM:  hardware.RAM{TotalBytes: gb(32), AvailableBytes: gb(24)},
			GPUs: []hardware.GPU{{Vendor: "Test", Name: "Test GPU", VRAMBytes: gb(4)}},
			Backend: hardware.Backend{
				Vulkan:    true,
				EngineTag: "b9999",
			},
		},
		Card: &llm.ModelCard{
			Arch:          "gemma3",
			ContextLength: 131072,
			SizeBytes:     gb(2), // small enough that model+projector fit 4 GB VRAM
		},
		Caps:   &llm.ModelCapabilities{Arch: "gemma3", ContextLength: 131072},
		Engine: &llm.EngineCaps{FlashAttnEnabled: true, CacheReuse: true, Tag: "b9999"},
		Vision: vision.Evaluation{
			State:     vision.StateFound,
			Projector: "mmproj-gemma3.gguf",
			Reason:    "paired",
		},
		Task:               TaskChat,
		EngineContextLimit: 131072,
		KVQuant:            "f16",
	}
}

func TestRecommend_EvidenceGrounded(t *testing.T) {
	rec := Recommend(baseInput())

	if rec.Task != TaskChat {
		t.Fatalf("task = %s", rec.Task)
	}
	if rec.Threads != 8 || rec.ThreadsBatch != 16 {
		t.Fatalf("threads = %d/%d, want 8/16 (measured cores)", rec.Threads, rec.ThreadsBatch)
	}
	if !rec.GPUAutoOffload || rec.GPULayers != 99 {
		t.Fatalf("gpu posture = %v/%d, want auto/99 with Vulkan+GPU", rec.GPUAutoOffload, rec.GPULayers)
	}
	if !rec.FlashAttention {
		t.Fatal("flash attention must be ON only because the engine profile verified it")
	}
	if rec.KVCacheQuant != "q8_0" {
		t.Fatalf("kv quant = %s, want q8_0 with flash-attn + GPU", rec.KVCacheQuant)
	}
	if rec.Context != taskContextBase(TaskChat) {
		t.Fatalf("context = %d, want task base %d (no clamp needed)", rec.Context, taskContextBase(TaskChat))
	}
	if rec.ProjectorDevice != "gpu" {
		t.Fatalf("projector device = %s, want gpu (fits VRAM)", rec.ProjectorDevice)
	}
	if len(rec.Reasons) == 0 {
		t.Fatal("every recommendation must carry reasons")
	}
	if rec.Predicted.Speed != "high" && rec.Predicted.Speed != "medium" {
		t.Fatalf("predicted speed = %s", rec.Predicted.Speed)
	}
}

func TestRecommend_ContextClampedByModelTrainingLimit(t *testing.T) {
	in := baseInput()
	in.Card.ContextLength = 8192
	in.Caps.ContextLength = 8192
	rec := Recommend(in)
	if rec.Context != 8192 {
		t.Fatalf("context = %d, want clamped to 8192", rec.Context)
	}
	found := false
	for _, r := range rec.Reasons {
		if strings.Contains(r, "training limit") {
			found = true
		}
	}
	if !found {
		t.Fatalf("clamp reason missing: %v", rec.Reasons)
	}
}

func TestRecommend_ContextStepsDownWhenUnsupported(t *testing.T) {
	in := baseInput()
	// A tiny machine: 2 GB RAM, no GPU. Huge-context footprint must step down.
	in.HW.GPUs = nil
	in.HW.Backend.Vulkan = false
	in.HW.RAM = hardware.RAM{TotalBytes: gb(2), AvailableBytes: gb(2)}
	in.Card.ContextLength = 131072
	rec := Recommend(in)
	if rec.Context > 16384 {
		t.Fatalf("context = %d, expected the resource ladder to step far down on a 2 GB machine", rec.Context)
	}
	// v1.8.4 (P0-B): the recommendation NEVER writes a derived
	// gpuAutoOffload=false — the pre-1.8.4 derived OFF was the stale state
	// that permanently blocked the AUTO Vulkan candidate. On a no-GPU
	// machine the posture stays AUTO/automatic (launch-time evidence
	// keeps the engine on CPU) with zero pinned layers, and the note
	// states the truth.
	if !rec.GPUAutoOffload {
		t.Fatal("no-GPU posture must stay AUTO/automatic (never a derived off)")
	}
	if rec.GPULayers != 0 {
		t.Fatalf("no-GPU layers = %d, want 0 (launch evidence decides)", rec.GPULayers)
	}
	if rec.Predicted.Speed == "high" || rec.Predicted.Speed == "medium" && rec.Threads < 8 {
		t.Fatalf("no-GPU predicted speed = %q must never claim GPU-class throughput", rec.Predicted.Speed)
	}
}

func TestRecommend_LowPowerProfile(t *testing.T) {
	in := baseInput()
	in.Task = TaskLowPower
	rec := Recommend(in)

	if rec.Threads != 4 {
		t.Fatalf("low-power threads = %d, want half of 8", rec.Threads)
	}
	if rec.UBatchSize != 256 {
		t.Fatalf("low-power ubatch = %d, want 256", rec.UBatchSize)
	}
	if rec.FlashAttention {
		t.Fatal("low-power keeps flash attention off")
	}
	if rec.MMProjOffload != "off" || rec.ProjectorDevice != "cpu" {
		t.Fatalf("low-power projector = %s/%s, want off/cpu", rec.MMProjOffload, rec.ProjectorDevice)
	}
	if rec.Context >= taskContextBase(TaskChat) {
		t.Fatalf("low-power context %d should be smaller than chat base", rec.Context)
	}
}

func TestRecommend_NoVisionProjectorIsHonest(t *testing.T) {
	in := baseInput()
	in.Vision = vision.Evaluation{State: vision.StateMissing}
	rec := Recommend(in)
	found := false
	for _, n := range rec.Notes {
		if strings.Contains(n, "mmproj") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-projector note absent: %v", rec.Notes)
	}
}

func TestRecommend_UnsupportedArchNoVisionClaims(t *testing.T) {
	in := baseInput()
	in.Vision = vision.Evaluation{State: vision.StateUnsupported}
	rec := Recommend(in)
	if rec.ProjectorDevice == "gpu" {
		t.Fatal("an unsupported-architecture model must never get a GPU projector")
	}
}

func TestRecommend_EngineWithoutFlashAttn(t *testing.T) {
	in := baseInput()
	in.Engine = &llm.EngineCaps{FlashAttnEnabled: false}
	rec := Recommend(in)
	if rec.FlashAttention {
		t.Fatal("unverified flash-attn support must stay off")
	}
}

func TestTasks_CompleteAndValid(t *testing.T) {
	seen := map[Task]bool{}
	for _, task := range Tasks() {
		if seen[task.ID] {
			t.Fatalf("duplicate task %s", task.ID)
		}
		seen[task.ID] = true
		if task.Label == "" || task.Description == "" {
			t.Fatalf("task %s missing label/description", task.ID)
		}
		if !IsValidTask(string(task.ID)) {
			t.Fatalf("task %s not valid", task.ID)
		}
	}
	if IsValidTask("turbo") {
		t.Fatal("unknown task must be invalid")
	}
}
