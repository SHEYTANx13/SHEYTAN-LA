package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"
)

// randSource / binaryPutUint64: the fixture builder's tiny helpers
// (deterministic seed; LE u64 field writer).
func randSource() *rand.Rand { return rand.New(rand.NewSource(20261121)) }

func binaryPutUint64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }

// Benchmark harness (work order section 5.8): the engine benchmark
// record captures the full evidence vocabulary — model identity,
// quantization/dtype, tokenizer, token counts, timings, memory,
// device/backend, OS/arch, Go version, sampling settings and the raw
// output. Comparative claims (llama.cpp, vLLM, SGLang, TensorRT-LLM,
// MLX, MLC, LiteRT-LM) are NOT made and NOT implied: this harness
// measures THIS engine's reference path only, on an untrained fixture.
type BenchRecord struct {
	Model           string  `json:"model"`
	ModelProvenance string  `json:"model_provenance"`
	Quantization    string  `json:"quantization"`
	DType           string  `json:"dtype"`
	Tokenizer       string  `json:"tokenizer"`
	PromptTokens    int     `json:"prompt_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	TTFTUs          float64 `json:"ttft_us"`
	TotalMs         int64   `json:"total_ms"`
	TPOTUs          float64 `json:"tpot_us"` // time per output token (decode)
	PrefillTokPerS  float64 `json:"prefill_tokens_per_sec"`
	DecodeTokPerS   float64 `json:"decode_tokens_per_sec"`
	PeakHeapBytes   uint64  `json:"peak_heap_bytes"`
	Backend         string  `json:"backend"`
	Driver          string  `json:"driver"`
	OS              string  `json:"os"`
	Arch            string  `json:"arch"`
	GoVersion       string  `json:"go_version"`
	Seed            int64   `json:"seed"`
	Temperature     float32 `json:"temperature"`
	RawOutput       string  `json:"raw_output"`
	Note            string  `json:"note"`
}

// BenchmarkFixtureGenerate is the harness: it runs the fixture
// generation and emits the machine-readable record as JSON
// (SHEYTAN_ENGINE_BENCH=1 turns it into a file-writing run).
func BenchmarkFixtureGenerate(b *testing.B) {
	container, cfgJSON := buildFixtureContainerB(b, fixtureConfig())
	eng, err := NewEngine(nil, 1)
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	model, err := eng.LoadModel(context.Background(), container, cfgJSON, "bench-fixture", nil)
	if err != nil {
		b.Fatal(err)
	}

	var before, after runtime.MemStats
	b.ResetTimer()
	var lastText string
	for i := 0; i < b.N; i++ {
		sess, err := NewSession(model, SamplingConfig{Seed: int64(i), Temperature: 0.9, TopK: 40, MaxNewTokens: 16}, nil)
		if err != nil {
			b.Fatal(err)
		}
		runtime.ReadMemStats(&before)
		start := time.Now()
		var ttft time.Duration
		firstSeen := false
		stream := func(_ string, _ int32, done bool) {
			if !done && !firstSeen {
				firstSeen = true
				ttft = time.Since(start) // first REAL token callback
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		text, err := sess.Generate(ctx, "bench", stream)
		cancel()
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if err != nil {
			b.Fatal(err)
		}
		lastText = text

		rec := BenchRecord{
			Model:           model.Config.Architecture,
			ModelProvenance: model.Provenance,
			Quantization:    "none (f32 weights)",
			DType:           "f32",
			Tokenizer:       "byte-258",
			PromptTokens:    6,
			OutputTokens:    16,
			TTFTUs:          float64(ttft.Microseconds()),
			TotalMs:         elapsed.Milliseconds(),
			TPOTUs:          float64(elapsed.Microseconds()) / float64(16),
			DecodeTokPerS:   float64(16) / elapsed.Seconds(),
			PeakHeapBytes:   after.TotalAlloc - before.TotalAlloc,
			Backend:         model.Backend.Info().Name,
			Driver:          model.Backend.Info().Driver,
			OS:              runtime.GOOS,
			Arch:            runtime.GOARCH,
			GoVersion:       runtime.Version(),
			Seed:            int64(i),
			Temperature:     0.9,
			RawOutput:       text,
			Note:            "reference CPU path over an untrained fixture; NO comparative performance claim",
		}
		if i == 0 {
			// One machine-readable record per bench invocation is the
			// evidence contract; the benchmark loop itself is for Go's
			// timing statistics.
			b.StopTimer()
			emitBenchRecord(b, rec)
			b.StartTimer()
		}
	}
	_ = lastText
}

func emitBenchRecord(b *testing.B, rec BenchRecord) {
	b.Helper()
	line, err := json.Marshal(rec)
	if err != nil {
		b.Fatalf("bench record marshal: %v", err)
	}
	b.Logf("BENCH_RECORD %s", line)
	if os.Getenv("SHEYTAN_ENGINE_BENCH_JSON") == "" {
		return
	}
	path := os.Getenv("SHEYTAN_ENGINE_BENCH_JSON")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		b.Fatalf("bench record file: %v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\n", line)
}

// buildFixtureContainerB mirrors buildFixtureContainer without *testing.T
// (benchmarks take *testing.B).
func buildFixtureContainerB(b *testing.B, cfg ModelConfig) ([]byte, []byte) {
	b.Helper()
	rng := randSource()
	arch := TinyFixtureArchitecture{}
	required := arch.RequiredTensors(cfg)

	type rec struct {
		DType       string `json:"dtype"`
		Shape       []int  `json:"shape"`
		DataOffsets [2]int `json:"data_offsets"`
	}
	header := map[string]interface{}{}
	data := make([]byte, 0, 4096)
	offset := 0
	for _, name := range sortedNames(required) {
		shape := required[name]
		numel, err := shape.Numel()
		if err != nil {
			b.Fatal(err)
		}
		vals := make([]float32, numel)
		for i := range vals {
			vals[i] = float32(rng.NormFloat64()) * 0.05
		}
		header[name] = rec{DType: "F32", Shape: []int(shape), DataOffsets: [2]int{offset, offset + numel*4}}
		data = append(data, f32AsBytes(vals)...)
		offset += numel * 4
	}
	hj, err := json.Marshal(header)
	if err != nil {
		b.Fatal(err)
	}
	out := make([]byte, 8)
	binaryPutUint64(out, uint64(len(hj)))
	out = append(out, hj...)
	out = append(out, data...)

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		b.Fatal(err)
	}
	return out, cfgJSON
}
