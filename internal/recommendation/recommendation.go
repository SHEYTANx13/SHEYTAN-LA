// Package recommendation is SHEYTAN's evidence-based runtime
// recommendation engine (v1.2.0).
//
// Pipeline contract — every recommendation must be traceable:
//
//	Detected → Calculated → Recommended → Applied → Measured →
//	Compared → Verified
//
// This package owns "Detected/Calculated/Recommended": it reads the
// hardware profile, the model's GGUF capabilities and the engine's
// VERIFIED capability profile, and produces a configuration with explicit
// reasons. It never invents measurements: predicted effects are labelled
// predicted, and only /api/perf telemetry speaks in measured numbers.
package recommendation

import (
	"fmt"
	"sort"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// Task is a task-aware runtime profile id.
type Task string

const (
	TaskChat     Task = "chat"
	TaskCoding   Task = "coding"
	TaskResearch Task = "research"
	TaskVision   Task = "vision"
	TaskAgent    Task = "agent"
	TaskLowPower Task = "low-power"
	TaskMaximum  Task = "maximum"
)

// TaskInfo describes one profile for the UI picker.
type TaskInfo struct {
	ID          Task   `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Tasks returns the supported runtime profiles in display order.
func Tasks() []TaskInfo {
	return []TaskInfo{
		{TaskChat, "Chat", "Moderate context, latency-first sampling posture."},
		{TaskCoding, "Coding", "Higher context and stronger verification headroom for tools and diffs."},
		{TaskResearch, "Research", "Larger context for long sources and multi-step retrieval."},
		{TaskVision, "Vision", "Projector-aware posture: GPU projector when it fits, image-ready context."},
		{TaskAgent, "Agent", "Tool-heavy runs: higher context, bounded iteration budget."},
		{TaskLowPower, "Low power", "Quiet and cool: smaller context, batch, and offload posture."},
		{TaskMaximum, "Maximum", "Everything the machine has verified it can run."},
	}
}

// IsValidTask reports whether id names a supported profile.
func IsValidTask(id string) bool {
	for _, t := range Tasks() {
		if string(t.ID) == id {
			return true
		}
	}
	return false
}

// taskContextBase is the per-task context PREFERENCE (tokens). Every value
// is later clamped to the model's training limit, the configured engine
// window and the machine's measured resources.
func taskContextBase(t Task) int {
	switch t {
	case TaskCoding, TaskResearch, TaskAgent, TaskMaximum:
		return 32768
	case TaskChat, TaskVision:
		return 16384
	case TaskLowPower:
		return 8192
	default:
		return 16384
	}
}

// Input is everything the engine needs. All pointers are optional; nil
// facts simply narrow the recommendation and surface as notes instead of
// guesses.
type Input struct {
	HW     hardware.Profile       // measured hardware
	Card   *llm.ModelCard         // GGUF header (weights, layers, ctx limit)
	Caps   *llm.ModelCapabilities // resolved model capabilities
	Engine *llm.EngineCaps        // VERIFIED engine capability profile
	Vision vision.Evaluation      // pre-boot vision evidence

	Task               Task   // requested profile (default chat)
	ConfiguredContext  int    // current effective context setting
	EngineContextLimit int    // verified engine window (0 = unknown)
	KVQuant            string // current KV quant ("", f16, q8_0, ...)
}

// Recommendation is the output contract consumed by /api/recommendation.
type Recommendation struct {
	Task Task `json:"task"`

	Context         int    `json:"context"`
	Threads         int    `json:"threads"`
	ThreadsBatch    int    `json:"threadsBatch"`
	GPULayers       int    `json:"gpuLayers"` // 99 = auto/offload-all, 0 = CPU
	UBatchSize      int    `json:"ubatchSize"`
	FlashAttention  bool   `json:"flashAttention"`
	KVCacheQuant    string `json:"kvCacheQuant"`
	GPUAutoOffload  bool   `json:"gpuAutoOffload"`
	MMProjOffload   string `json:"mmprojOffload"`   // auto | on | off
	ProjectorDevice string `json:"projectorDevice"` // gpu | cpu | none
	CacheReuse      int    `json:"cacheReuse,omitempty"`

	// Predicted effects — explicitly PREDICTED, never presented as
	// measured. Verification happens after apply through /api/perf.
	Predicted Predicted `json:"predicted"`

	// Reasons is the EXPLAIN-WHY list: every entry cites a real detected
	// input or a real calculation over one.
	Reasons []string `json:"reasons"`

	// Notes are honest limitations (unknown VRAM, unsupported flash
	// attention, missing projector...).
	Notes []string `json:"notes,omitempty"`
}

// Predicted is the labelled prediction block.
type Predicted struct {
	// TotalBytes is the assessed footprint for the recommended context
	// (weights + KV + runtime overhead) — from llm.AssessContextResource.
	TotalBytes int64 `json:"totalBytes,omitempty"`
	// KVBytes is the KV-cache share at the recommended context.
	KVBytes int64 `json:"kvBytes,omitempty"`
	// Class is the resource verdict: safe | caution | unsupported.
	Class string `json:"class,omitempty"`
	// Speed is qualitative (low/medium/high) — never a tok/s claim.
	Speed string `json:"speed,omitempty"`
	// VRAMBytes is the predicted VRAM share with the recommended offload.
	VRAMBytes int64 `json:"vramBytes,omitempty"`
}

// Recommend produces the profile-driven configuration. It never mutates
// config — the UI applies values explicitly and records the outcome.
func Recommend(in Input) Recommendation {
	task := in.Task
	if !IsValidTask(string(task)) {
		task = TaskChat
	}

	rec := Recommendation{Task: task}
	hw := in.HW

	// --- Threads: physical cores generate, logical cores prefill. -----
	if gen, batch := sysinfoThreads(hw); gen > 0 {
		if task == TaskLowPower && gen > 2 {
			rec.Threads = gen / 2
			rec.ThreadsBatch = batch
			rec.Reasons = append(rec.Reasons,
				fmt.Sprintf("low-power profile: %d of %d physical cores for generation", rec.Threads, gen))
		} else {
			rec.Threads = gen
			rec.ThreadsBatch = batch
			rec.Reasons = append(rec.Reasons,
				fmt.Sprintf("%d physical cores detected — generation uses physical, prefill uses logical cores", gen))
		}
	} else {
		rec.Notes = append(rec.Notes, "CPU core count unavailable — keeping current thread settings.")
	}

	// --- GPU posture: only from measured adapters + backend evidence. --
	// v1.8.4 (P0-B) POSTURE SEMANTICS: the recommendation never writes
	// gpuAutoOffload=false as a derived state. The pre-1.8.4 behavior
	// ("GPU present but no Vulkan engine backend" → false/0) persisted
	// a CPU posture that then looked like an explicit OFF to the AUTO
	// candidate gate — the exact circularity that kept AUTO CPU-only
	// forever (Vulkan never provisioned because the posture said off;
	// the posture said off because Vulkan was never provisioned). The
	// AUTO posture (true) is safe in every case here: the launch-time
	// evidence gate (llm.autoGPUOffload) keeps the engine on CPU when
	// no usable device is proven, which is exactly the CPU-only
	// behavior the old derived false was approximating — without the
	// sticky state.
	gpuVRAM := hw.VRAMBytes()
	vulkan := hw.Backend.Vulkan
	switch {
	case hw.HasGPU() && vulkan:
		rec.GPUAutoOffload = true
		rec.GPULayers = 99
		rec.Reasons = append(rec.Reasons,
			fmt.Sprintf("GPU detected (%s) with a Vulkan engine backend — automatic layer offload", gpuLabel(hw)))
		if gpuVRAM > 0 {
			rec.Reasons = append(rec.Reasons,
				fmt.Sprintf("%s of VRAM measured on the primary adapter", humanBytes(gpuVRAM)))
		} else {
			rec.Notes = append(rec.Notes,
				"VRAM size not reported by the driver — offload posture stays automatic; watch /api/perf after applying.")
		}
	case hw.HasGPU() && !vulkan:
		rec.GPUAutoOffload = true
		rec.GPULayers = 0
		rec.Notes = append(rec.Notes,
			"GPU present but no Vulkan engine backend detected — the launch-time evidence gate keeps the engine on CPU until a Vulkan engine build is provisioned (AUTO may provision one; the posture stays automatic, never a pinned off).")
	default:
		rec.GPUAutoOffload = true
		rec.GPULayers = 0
		rec.Notes = append(rec.Notes,
			"No dedicated GPU detected — the launch-time evidence gate keeps the engine on CPU (the posture stays automatic, never a pinned off).")
	}
	if task == TaskLowPower && hw.HasGPU() && vulkan {
		// Low-power keeps the offload (VRAM does not burn wall power the
		// way CPU generation does) but is not the place for maximum ctx.
		rec.Reasons = append(rec.Reasons, "low-power profile: GPU offload retained — VRAM work is cooler than CPU generation")
	}

	// --- Flash attention: only when the engine VERIFIED support. -------
	if in.Engine != nil && in.Engine.FlashAttnEnabled {
		if task == TaskLowPower {
			rec.FlashAttention = false
			rec.Reasons = append(rec.Reasons, "low-power profile: flash attention off (smaller compute buffers)")
		} else {
			rec.FlashAttention = true
			if in.Engine.Tag != "" {
				rec.Reasons = append(rec.Reasons,
					fmt.Sprintf("flash attention verified supported by engine build %s", in.Engine.Tag))
			} else {
				rec.Reasons = append(rec.Reasons, "flash attention verified supported by the engine capability profile")
			}
		}
	} else {
		rec.FlashAttention = false
		rec.Notes = append(rec.Notes,
			"This engine build has not verified --flash-attn — keeping it off.")
	}

	// --- KV quant: shrink KV only when flash attention is on. ----------
	rec.KVCacheQuant = in.KVQuant
	if rec.KVCacheQuant == "" {
		rec.KVCacheQuant = "f16"
	}
	if rec.FlashAttention {
		if task == TaskLowPower || task == TaskMaximum || gpuVRAM > 0 {
			rec.KVCacheQuant = "q8_0"
			if task == TaskLowPower {
				rec.Reasons = append(rec.Reasons, "low-power profile: q8_0 KV cache halves KV memory for free capacity")
			} else {
				rec.Reasons = append(rec.Reasons, "q8_0 KV cache recommended — halves KV memory at negligible quality cost")
			}
		}
	}

	// --- UBatch: task-sized. -------------------------------------------
	rec.UBatchSize = 512
	if task == TaskLowPower {
		rec.UBatchSize = 256
		rec.Reasons = append(rec.Reasons, "low-power profile: ubatch 256 reduces peak compute buffers")
	}

	// --- Context: task preference → clamps → resource verdict. ---------
	base := taskContextBase(task)
	if in.Caps != nil && in.Caps.RecommendedCtx > 0 && in.Caps.RecommendedCtx > base {
		// The model class recommends MORE than the task needs — that is
		// fine; the task wins. Note it so the picker stays explainable.
		rec.Reasons = append(rec.Reasons,
			fmt.Sprintf("%s profile targets %d-token context", task, base))
	}
	if in.Card != nil && in.Card.ContextLength > 0 && base > in.Card.ContextLength {
		base = in.Card.ContextLength
		rec.Reasons = append(rec.Reasons,
			fmt.Sprintf("context clamped to the model's training limit (%d)", in.Card.ContextLength))
	}
	if in.EngineContextLimit > 0 && base > in.EngineContextLimit {
		base = in.EngineContextLimit
		rec.Reasons = append(rec.Reasons,
			fmt.Sprintf("context clamped to the verified engine window (%d)", in.EngineContextLimit))
	}

	// Resource verdict ladder: step down until the assessment is not
	// unsupported on this machine (or reach the floor).
	for {
		if in.Card == nil {
			rec.Context = base
			rec.Notes = append(rec.Notes, "GGUF metadata unreadable — footprint assessment unavailable.")
			break
		}
		assess := llm.AssessContextResource(
			in.Card, in.Card.SizeBytes, base, rec.KVCacheQuant,
			llm.SystemMemory{
				TotalRAMBytes:     hw.RAM.TotalBytes,
				AvailableRAMBytes: hw.RAM.AvailableBytes,
				TotalVRAMBytes:    gpuVRAM,
			},
		)
		if assess.Class != llm.ResourceUnsupported || base <= 4096 {
			rec.Context = base
			rec.Predicted.TotalBytes = assess.EstimatedTotalBytes
			rec.Predicted.KVBytes = assess.KVCacheBytes
			rec.Predicted.Class = string(assess.Class)
			if assess.Class == llm.ResourceCaution {
				rec.Notes = append(rec.Notes,
					"Footprint lands in the caution band on this machine — verify with live metrics after applying.")
			}
			if assess.Reason != "" {
				rec.Reasons = append(rec.Reasons, assess.Reason)
			}
			break
		}
		base /= 2
		rec.Reasons = append(rec.Reasons,
			fmt.Sprintf("context halved to %d — %d-token footprint does not fit measured memory", base*2, base*2))
	}
	if rec.Context == 0 {
		rec.Context = base
	}

	// --- Vision posture: real projector evidence only. -----------------
	rec.MMProjOffload = "auto"
	rec.ProjectorDevice = "cpu"
	switch {
	case in.Vision.State.Healthy() || in.Vision.State == vision.StateFound:
		// v1.8.4: GPU projector placement requires a USABLE GPU backend
		// (measured Vulkan engine backend), never the posture flag
		// alone — with a CPU-only engine the honest placement is CPU
		// even though the posture is now always AUTO/automatic.
		if rec.GPUAutoOffload && vulkan {
			fits := gpuVRAM == 0 ||
				rec.Predicted.TotalBytes == 0 ||
				rec.Predicted.TotalBytes-rec.Predicted.KVBytes < gpuVRAM
			if task == TaskLowPower {
				rec.MMProjOffload = "off"
				rec.ProjectorDevice = "cpu"
				rec.Reasons = append(rec.Reasons, "low-power profile: projector stays on CPU (mmproj offload off)")
			} else if fits {
				rec.ProjectorDevice = "gpu"
				rec.Reasons = append(rec.Reasons, "vision projector fits VRAM alongside the model — GPU projector placement")
			} else {
				rec.MMProjOffload = "off"
				rec.Notes = append(rec.Notes,
					"Predicted model footprint already fills VRAM — projector pinned to CPU to avoid swapping layers out.")
			}
		} else {
			rec.ProjectorDevice = "cpu"
			rec.Notes = append(rec.Notes, "Projector runs on CPU while the engine has no GPU backend.")
		}
	case in.Vision.State == vision.StateMissing || in.Vision.State == vision.StateSupported:
		rec.Notes = append(rec.Notes, "No compatible projector on disk — download the model family's mmproj to enable vision.")
	case in.Vision.State == vision.StateFailed:
		rec.Notes = append(rec.Notes, "Vision override unresolved — check the projector path in Vision settings.")
	default:
		// unsupported: nothing vision-specific to tune.
	}

	// --- Cache reuse: keep only when the engine verified it. -----------
	if in.Engine != nil && in.Engine.CacheReuse {
		rec.CacheReuse = 256
	}

	// --- Predicted speed: qualitative, honest. -------------------------
	rec.Predicted.Speed = predictedSpeed(rec, gpuVRAM, hw.HasGPU() && vulkan)

	sortStrings(rec.Reasons)
	return rec
}

// sysinfoThreads reuses the measured core counts from the hardware profile
// (physical generation / logical batch), 0 when unknown.
func sysinfoThreads(hw hardware.Profile) (gen, batch int) {
	if hw.CPU.PhysicalCores > 0 {
		gen = hw.CPU.PhysicalCores
	}
	if hw.CPU.LogicalCores > 0 {
		batch = hw.CPU.LogicalCores
	}
	return gen, batch
}

// predictedSpeed is a qualitative band from the USABLE GPU evidence and
// the footprint class — explicitly NOT a tok/s measurement. v1.8.4: the
// band keys on the measured Vulkan engine backend (the posture flag is
// always AUTO/automatic now), so a machine whose engine would launch CPU
// is never promised GPU-class speed.
func predictedSpeed(rec Recommendation, gpuVRAM int64, gpuUsable bool) string {
	switch {
	case gpuUsable && gpuVRAM > 0 && rec.Predicted.Class == "safe":
		return "high"
	case gpuUsable && rec.Predicted.Class == "caution":
		return "medium"
	case gpuUsable:
		return "medium"
	case rec.Threads >= 8:
		return "medium"
	default:
		return "low"
	}
}

func gpuLabel(hw hardware.Profile) string {
	if g := hw.PrimaryGPU(); g != nil && g.Name != "" {
		return g.Name
	}
	return "unknown adapter"
}

func humanBytes(n int64) string {
	const gb = 1 << 30
	const mb = 1 << 20
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%d MB", n/mb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func sortStrings(s []string) {
	sort.Strings(s)
}
