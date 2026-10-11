// Resource protection for large contexts (1.1.6).
//
// A context window can be technically valid for the model and still
// predictably destabilise the machine: the KV-cache for 128K tokens on a
// 7B model is gigabytes before the weights are even counted. This file
// classifies candidate context sizes into
//
//	safe / caution / unsupported
//
// from measurable inputs — model card facts (layers, embedding width),
// the model file size, the KV-cache quantization setting and the machine's
// RAM/VRAM — so the UI can explain WHY a value is unavailable instead of
// letting the engine die mid-load. The estimates are deliberately
// conservative: this is a pre-flight guard, not a benchmark.
package llm

import "fmt"

// ResourceClass is the pre-flight classification of one context size.
type ResourceClass string

const (
	// ResourceSafe: estimated footprint fits comfortably.
	ResourceSafe ResourceClass = "safe"
	// ResourceCaution: likely fits, but tight — watch for swap/slowdown.
	ResourceCaution ResourceClass = "caution"
	// ResourceUnsupported: predictably exceeds the machine's memory —
	// must not be offered or silently activated.
	ResourceUnsupported ResourceClass = "unsupported"
)

// SystemMemory is the machine memory snapshot the assessment runs
// against. Bytes of 0 mean "unknown" (the classifier then errs toward
// caution rather than inventing a limit).
type SystemMemory struct {
	TotalRAMBytes     int64 `json:"totalRamBytes,omitempty"`
	AvailableRAMBytes int64 `json:"availableRamBytes,omitempty"`
	TotalVRAMBytes    int64 `json:"totalVramBytes,omitempty"` // 0 = unknown/no GPU
}

// ResourceAssessment is the pre-flight verdict for one context size.
type ResourceAssessment struct {
	ContextTokens        int           `json:"contextTokens"`
	ModelMemoryBytes     int64         `json:"modelMemoryBytes,omitempty"`
	KVCacheBytes         int64         `json:"kvCacheBytes,omitempty"`
	RuntimeOverheadBytes int64         `json:"runtimeOverheadBytes,omitempty"`
	EstimatedTotalBytes  int64         `json:"estimatedTotalBytes"`
	SystemRAMBytes       int64         `json:"systemRamBytes,omitempty"`
	AvailableRAMBytes    int64         `json:"availableRamBytes,omitempty"`
	TotalVRAMBytes       int64         `json:"totalVramBytes,omitempty"`
	Class                ResourceClass `json:"classification"`
	Reason               string        `json:"reason,omitempty"`
}

// runtimeOverheadBytes covers llama.cpp process + allocator + compute
// buffers beyond weights and KV-cache. Conservative constant: measured
// footprints for small models land well under this.
const runtimeOverheadBytes = 768 << 20

// KVCacheBytes estimates the KV-cache for one model at one context size:
//
//	2 (K and V) × layers × ctx × embeddingWidth × bytesPerElement
//
// bytesPerElement follows the configured KV quantization: f16 = 2,
// q8_0 ≈ 1, q4_0/q4_1 ≈ 0.5. Unknown card facts degrade to a
// width-conservative default (12B per token: roughly a 7B-class model at
// f16 KV) so the guard still means something when the GGUF header is
// unreadable.
func KVCacheBytes(card *ModelCard, ctxTokens int, kvQuant string) int64 {
	if ctxTokens <= 0 {
		return 0
	}

	perToken := int64(12) // fallback: ~7B-class model, f16 KV, per token
	if card != nil && card.Layers > 0 && card.EmbeddingLen > 0 {
		perToken = int64(2*card.Layers*card.EmbeddingLen) * kvBytesPerElement(kvQuant)
	}

	return perToken * int64(ctxTokens)
}

func kvBytesPerElement(kvQuant string) int64 {
	switch kvQuant {
	case "q8_0", "q8", "q8k":
		return 1
	case "q4_0", "q4_1", "q4k", "q4":
		return 1 // half of 0.5 rounded up per element pair — keep conservative
	default:
		return 2 // f16 (also the default when unset)
	}
}

// AssessContextResource classifies one context size against the machine.
//
// Classification rules (total = weights + KV-cache + runtime overhead):
//   - total > available RAM (+VRAM when known)  → unsupported
//   - total > 60 % of total RAM                 → caution
//   - otherwise                                 → safe
//
// With unknown RAM the verdict is caution for anything above 32K tokens
// and safe below it — never unsupported, because there is no measurement
// to prove it. The reason string is user-facing.
func AssessContextResource(
	card *ModelCard,
	modelSizeBytes int64,
	ctxTokens int,
	kvQuant string,
	sys SystemMemory,
) ResourceAssessment {
	assessment := ResourceAssessment{
		ContextTokens:     ctxTokens,
		SystemRAMBytes:    sys.TotalRAMBytes,
		AvailableRAMBytes: sys.AvailableRAMBytes,
		TotalVRAMBytes:    sys.TotalVRAMBytes,
		Class:             ResourceSafe,
	}

	if modelSizeBytes > 0 {
		assessment.ModelMemoryBytes = modelSizeBytes
	}
	assessment.KVCacheBytes = KVCacheBytes(card, ctxTokens, kvQuant)
	assessment.RuntimeOverheadBytes = runtimeOverheadBytes
	assessment.EstimatedTotalBytes =
		assessment.ModelMemoryBytes + assessment.KVCacheBytes + assessment.RuntimeOverheadBytes

	// The machine's usable pool: RAM plus VRAM when VRAM is actually
	// known. Available RAM is the honest denominator when known — the
	// OS and other processes already own the rest.
	pool := sys.AvailableRAMBytes
	poolKnown := pool > 0
	if !poolKnown {
		pool = sys.TotalRAMBytes
		poolKnown = pool > 0
	}
	if sys.TotalVRAMBytes > 0 {
		pool += sys.TotalVRAMBytes
	}

	if poolKnown {
		if assessment.EstimatedTotalBytes > pool {
			assessment.Class = ResourceUnsupported
			assessment.Reason = fmt.Sprintf(
				"estimated %s (weights %s + KV-cache %s + runtime %s) exceeds the machine's usable memory %s",
				humanBytes(assessment.EstimatedTotalBytes),
				humanBytes(assessment.ModelMemoryBytes),
				humanBytes(assessment.KVCacheBytes),
				humanBytes(assessment.RuntimeOverheadBytes),
				humanBytes(pool))
			return assessment
		}
		if sys.TotalRAMBytes > 0 && assessment.EstimatedTotalBytes > (sys.TotalRAMBytes*6)/10 {
			assessment.Class = ResourceCaution
			assessment.Reason = fmt.Sprintf(
				"estimated %s is over 60%% of system RAM %s — expect pressure",
				humanBytes(assessment.EstimatedTotalBytes),
				humanBytes(sys.TotalRAMBytes))
			return assessment
		}
		assessment.Reason = fmt.Sprintf("estimated %s fits within %s usable memory",
			humanBytes(assessment.EstimatedTotalBytes), humanBytes(pool))
		return assessment
	}

	// No memory measurement available: conservative, never absolute.
	if ctxTokens > 32768 {
		assessment.Class = ResourceCaution
		assessment.Reason = "system memory could not be measured — large windows need caution"
		return assessment
	}
	assessment.Reason = "system memory could not be measured — size judged by model class only"
	return assessment
}

// ContextOptionSizes is the fixed selector ladder the UI offers
// (1.1.6 §4). Values are tokens.
var ContextOptionSizes = []int{4096, 8192, 16384, 32768, 65536, 131072}

// ContextOption is one selector entry with its verdict.
type ContextOption struct {
	Tokens      int           `json:"tokens"`
	Label       string        `json:"label"` // "4K", "8K", …
	Class       ResourceClass `json:"classification"`
	Available   bool          `json:"available"`
	Reason      string        `json:"reason,omitempty"`
	ModelClamp  bool          `json:"modelClamped,omitempty"`  // beyond the model's GGUF limit
	EngineClamp bool          `json:"engineClamped,omitempty"` // beyond the running engine's window
}

// ContextLabel renders the compact UI label for a token count.
func ContextLabel(tokens int) string {
	switch {
	case tokens%1024 == 0 && tokens >= 1024:
		return fmt.Sprintf("%dK", tokens/1024)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

// ContextOptions builds the selector ladder for the current model and
// machine: every size is classified and marked available/unavailable
// with an honest reason. Values beyond the model's GGUF limit or the
// running engine's verified window are listed but unavailable.
func ContextOptions(
	card *ModelCard,
	modelSizeBytes int64,
	kvQuant string,
	sys SystemMemory,
	modelMax, engineWindow int,
) []ContextOption {
	out := make([]ContextOption, 0, len(ContextOptionSizes))

	for _, size := range ContextOptionSizes {
		assessed := AssessContextResource(card, modelSizeBytes, size, kvQuant, sys)
		opt := ContextOption{
			Tokens:    size,
			Label:     ContextLabel(size),
			Class:     assessed.Class,
			Reason:    assessed.Reason,
			Available: assessed.Class != ResourceUnsupported,
		}

		if modelMax > 0 && size > modelMax {
			opt.ModelClamp = true
			opt.Available = false
			opt.Reason = fmt.Sprintf(
				"beyond the model's context limit %s — the model was not trained for it",
				ContextLabel(modelMax))
		}
		if opt.Available && engineWindow > 0 && size > engineWindow {
			opt.EngineClamp = true
			// Selectable — planning clamps to the engine window and says
			// so — but flagged so the UI can explain the real window.
			opt.Reason = fmt.Sprintf(
				"the running engine serves %s; requests are clamped to it until the engine restarts with a larger window",
				ContextLabel(engineWindow))
		}

		out = append(out, opt)
	}

	return out
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
