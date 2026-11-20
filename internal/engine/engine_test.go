package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// buildFixtureContainer assembles the REAL tiny-fixture-1 model: a
// safetensors container with deterministic (seeded) f32 weights plus the
// architecture config. Every weight element is derived from rand with a
// FIXED seed — the fixture is reproducible byte-for-byte, and the
// generated tokens come from the kernels over these weights.
func buildFixtureContainer(t *testing.T, cfg ModelConfig, mutate string) ([]byte, []byte) {
	t.Helper()
	rng := rand.New(rand.NewSource(20261121)) // the fixture seed, documented
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
			t.Fatal(err)
		}
		vals := make([]float32, numel)
		for i := range vals {
			vals[i] = float32(rng.NormFloat64()) * 0.05
		}
		if mutate != "" && name == mutate {
			vals[0] += 0.5 // the sensitivity probe
		}
		header[name] = rec{DType: "F32", Shape: []int(shape), DataOffsets: [2]int{offset, offset + numel*4}}
		data = append(data, f32AsBytes(vals)...)
		offset += numel * 4
	}
	hj, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 8)
	binary.LittleEndian.PutUint64(out, uint64(len(hj)))
	out = append(out, hj...)
	out = append(out, data...)

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return out, cfgJSON
}

func sortedNames(m map[string]Shape) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

func fixtureConfig() ModelConfig {
	return ModelConfig{
		Architecture:  "tiny-fixture-1",
		VocabSize:     tinyFixtureVocab,
		HiddenDim:     tinyFixtureD,
		FeedDim:       tinyFixtureF,
		ContextLength: 64,
		RMSEps:        1e-5,
	}
}

func loadFixture(t *testing.T, mutate string) (*Engine, *Model) {
	t.Helper()
	container, cfgJSON := buildFixtureContainer(t, fixtureConfig(), mutate)
	eng, err := NewEngine(nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	model, err := eng.LoadModel(context.Background(), container, cfgJSON, "test-fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	return eng, model
}

// TestEngineEndToEndFixtureExecution is THE vertical-slice proof: the
// real container parse, the complete tensor mapping, the graph
// execution over the reference kernels and the seeded sampling run in
// one pipeline, and the output tokens are a function of the weights and
// the seed — not canned text.
func TestEngineEndToEndFixtureExecution(t *testing.T) {
	eng, model := loadFixture(t, "")
	defer eng.Close()

	sess, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	text, err := sess.Generate(ctx, "sheytan", nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(sess.GeneratedTokens()) != 8 {
		t.Fatalf("want 8 generated tokens, got %d", len(sess.GeneratedTokens()))
	}
	if strings.Contains(text, "<decode error") {
		t.Fatalf("decode error in generated text: %q", text)
	}

	// Determinism: the same seed reproduces the SAME token stream.
	sess2, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess2.Close()
	text2, err := sess2.Generate(ctx, "sheytan", nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != text2 {
		t.Fatalf("same-seed generation diverged:\n%q\nvs\n%q", text, text2)
	}
}

// TestEngineWeightSensitivity proves the forward pass really executes
// the weights: a perturbed lm_head produces DIFFERENT logits for the
// same prompt (a canned generator would be weight-insensitive). The
// comparison is at the graph-executor level — fully deterministic.
func TestEngineWeightSensitivity(t *testing.T) {
	engA, modelA := loadFixture(t, "")
	defer engA.Close()
	engB, modelB := loadFixture(t, "lm_head")
	defer engB.Close()

	prompt := "sensitivity"
	arch := TinyFixtureArchitecture{}

	logitsFor := func(m *Model) []float32 {
		t.Helper()
		toks, err := m.Tokenizer.Encode(prompt)
		if err != nil {
			t.Fatal(err)
		}
		graph, tensors, err := arch.BuildGraph(m, toks)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := m.Backend.Compile(graph)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Backend.Submit(plan, tensors); err != nil {
			t.Fatal(err)
		}
		if err := m.Backend.Synchronize(); err != nil {
			t.Fatal(err)
		}
		lf, err := tensors["logits"].Floats()
		if err != nil {
			t.Fatal(err)
		}
		return lf
	}

	la := logitsFor(modelA)
	lb := logitsFor(modelB)
	if len(la) != len(lb) {
		t.Fatalf("logit lengths diverge: %d vs %d", len(la), len(lb))
	}
	diffs := 0
	for i := range la {
		if la[i] != lb[i] {
			diffs++
		}
	}
	if diffs == 0 {
		t.Fatal("perturbing lm_head must change the logits — the model is not executing real weights")
	}
	t.Logf("logits differ at %d/%d positions under a single-weight perturbation", diffs, len(la))

	// Session-level determinism pin over the UNPERTURBED model.
	ctx := context.Background()
	sessA, _ := NewSession(modelA, SamplingConfig{Seed: 7, MaxNewTokens: 8}, nil)
	defer sessA.Close()
	ta, err := sessA.Generate(ctx, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessA2, _ := NewSession(modelA, SamplingConfig{Seed: 7, MaxNewTokens: 8}, nil)
	defer sessA2.Close()
	tb, err := sessA2.Generate(ctx, prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ta != tb {
		t.Fatal("same-seed session generation diverged")
	}
}

// TestEngineLoadFailures pins the fail-closed container contract.
func TestEngineLoadFailures(t *testing.T) {
	container, cfgJSON := buildFixtureContainer(t, fixtureConfig(), "")

	eng, err := NewEngine(nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// Corrupted container (header length destroyed).
	corrupt := append([]byte(nil), container...)
	binary.LittleEndian.PutUint64(corrupt, uint64(1)<<40)
	if _, err := eng.LoadModel(context.Background(), corrupt, cfgJSON, "x", nil); !errors.Is(err, ErrMalformedContainer) {
		t.Fatalf("corrupt container: want ErrMalformedContainer, got %v", err)
	}

	// Missing required tensor: drop lm_head's record by loading with a
	// container missing it (rebuild without that name is not easy with
	// the builder; instead corrupt the name in the header).
	hj := []byte(`{"tok_emb":{"dtype":"F32","shape":[258,16],"data_offsets":[0,16512]}}`)
	partial := make([]byte, 8)
	binary.LittleEndian.PutUint64(partial, uint64(len(hj)))
	partial = append(partial, hj...)
	partial = append(partial, make([]byte, 16512)...)
	if _, err := eng.LoadModel(context.Background(), partial, cfgJSON, "x", nil); err == nil {
		t.Fatal("incomplete tensor mapping must fail the load (complete mapping required)")
	}

	// Unknown architecture is a typed unsupported answer.
	cfg := fixtureConfig()
	cfg.Architecture = "llama-70b-fixture"
	_, cfgJSON2 := buildFixtureContainer(t, cfg, "")
	if _, err := eng.LoadModel(context.Background(), container, cfgJSON2, "x", nil); !errors.Is(err, ErrUnsupportedModel) {
		var ue *UnsupportedError
		if !errors.As(err, &ue) {
			t.Fatalf("unknown architecture: want ErrUnsupportedModel, got %v", err)
		}
	}
}

// TestEngineContextLength pins the bounded-context contract.
func TestEngineContextLength(t *testing.T) {
	eng, model := loadFixture(t, "")
	defer eng.Close()
	sess, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	long := strings.Repeat("a", 128) // > context 64
	if _, err := sess.Generate(context.Background(), long, nil); !errors.Is(err, ErrContextLength) {
		t.Fatalf("want ErrContextLength, got %v", err)
	}
	// The session history is untouched by the rejected request.
	if len(sess.History()) != 0 {
		t.Fatalf("rejected request must not mutate the session, history=%d", len(sess.History()))
	}
}

// TestEngineAdmissionQueue pins the bounded queue: the admission
// primitive is exercised DIRECTLY (deterministic, no timing race) — a
// full queue answers ErrBusy under cancellation pressure, then the slot
// frees and Generate proceeds.
func TestEngineAdmissionQueue(t *testing.T) {
	container, cfgJSON := buildFixtureContainer(t, fixtureConfig(), "")
	eng, err := NewEngine(nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	model, err := eng.LoadModel(context.Background(), container, cfgJSON, "test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Take the ONE admission slot directly.
	ctx := context.Background()
	if err := eng.acquire(ctx); err != nil {
		t.Fatalf("acquire the only slot: %v", err)
	}

	sess, _ := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 2}, nil)
	defer sess.Close()

	// A Generate now waits on the FULL queue; a cancelling context turns
	// that wait into the typed ErrBusy answer (never a silent queue-up).
	busyCtx, cancelBusy := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancelBusy()
	if _, err := sess.Generate(busyCtx, "busy", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue: want ErrBusy, got %v", err)
	}
	// The rejected request did not mutate the session.
	if len(sess.History()) != 0 {
		t.Fatalf("rejected request must not mutate the session, history=%d", len(sess.History()))
	}

	// Release the slot: Generate proceeds normally.
	eng.release()
	if _, err := sess.Generate(ctx, "ok", nil); err != nil {
		t.Fatalf("generate after release: %v", err)
	}
}

// TestEngineCancellation pins the cancellation contract.
func TestEngineCancellation(t *testing.T) {
	eng, model := loadFixture(t, "")
	defer eng.Close()
	sess, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 30}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	// Pre-cancelled context: deterministic. The step loop checks
	// ctx.Err() before every forward pass, so a cancellation that lands
	// at ANY point answers with the cancellation error (a mid-generation
	// cancel is the same code path — the fixture is simply too fast to
	// race it reliably).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sess.Generate(ctx, "cancel me", nil)
	if err == nil {
		t.Fatal("cancelled generation must report an error")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("want context.Canceled/ErrEngineClosed, got %v", err)
	}
	// The session stays usable after cancellation (Reset drops the
	// request-local state — the documented rollback discipline).
	sess.Reset()
	if _, err := sess.Generate(context.Background(), "ag", nil); err != nil {
		t.Fatalf("session must remain usable after cancellation: %v", err)
	}
}

// TestEngineProgressEvents pins the structured progress events.
func TestEngineProgressEvents(t *testing.T) {
	container, cfgJSON := buildFixtureContainer(t, fixtureConfig(), "")
	eng, err := NewEngine(nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	var kinds []EventKind
	sink := func(ev Event) { kinds = append(kinds, ev.Kind) }

	model, err := eng.LoadModel(context.Background(), container, cfgJSON, "events", sink)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 3}, sink)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if _, err := sess.Generate(context.Background(), "ev", nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []EventKind{EventLoadStart, EventLoadProgress, EventLoadDone, EventGenerateStart, EventGenerateToken, EventGenerateDone} {
		found := false
		for _, k := range kinds {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("progress event %s missing from %v", want, kinds)
		}
	}
}

// TestEngineCloseOwnership pins the ownership contract: Close releases
// everything; later calls answer the typed closed errors.
func TestEngineCloseOwnership(t *testing.T) {
	eng, model := loadFixture(t, "")
	sess, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("engine Close must be idempotent, got %v", err)
	}
	if _, err := eng.LoadModel(context.Background(), nil, nil, "x", nil); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("load after Close: want ErrEngineClosed, got %v", err)
	}
	if _, err := sess.Generate(context.Background(), "x", nil); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("generate after Close: want ErrEngineClosed, got %v", err)
	}
	if _, err := NewSession(model, SamplingConfig{Seed: 1, MaxNewTokens: 2}, nil); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("session after Close: want ErrEngineClosed, got %v", err)
	}
}
