package engine

import (
	"fmt"
)

// TinyFixtureArchitecture is "tiny-fixture-1": a deterministic,
// in-repo fixture architecture whose COMPLETE pipeline (tokenizer,
// config, weights, tensor mapping, graph, forward path, sampling) the
// engine executes end to end.
//
// What this fixture IS: a real execution path through the reference
// kernels — every output token is computed from real matmuls, attention
// and sampling over REAL (deterministically generated) safetensors
// weights. It proves the engine's vertical slice.
//
// What this fixture is NOT: a pretrained language model. Its weights
// are untrained and derived from a documented seed; it makes no
// language claim and no performance claim. Real checkpoint families
// (Llama-style et al.) are future architecture adapters over this same
// Registry/Graph/kernel path.
//
// Shape vocabulary:
//
//	vocab = 258 (256 byte tokens + BOS + EOS)
//	d     = hidden dim (16)
//	f     = feed dim (32)
//
// Forward pass (single block, single head):
//
//	x   = embedding(tok_emb, ids)              (t,d)
//	h   = rmsnorm(x, ln1)                      (t,d)
//	q   = h @ wq ; k = h @ wk ; v = h @ wv     (t,d)
//	a   = causal_attention(q,k,v)              (t,d)
//	x2  = x + a                                (t,d)
//	h2  = rmsnorm(x2, ln2)                     (t,d)
//	m1  = h2 @ wff1                            (t,f)
//	m2  = silu(m1)                             (t,f)
//	m3  = m2 @ wff2                            (t,d)
//	x3  = x2 + m3                              (t,d)
//	nf  = rmsnorm(x3, lnf)                     (t,d)
//	logits = nf @ lm_head                      (t,vocab)
//
// The graph node list above is exactly what BuildGraph compiles, and
// the KV state question is answered honestly: the fixture recomputes
// the FULL prefix attention each step (bounded, correct, deterministic)
// — a paged/prefix KV cache is the documented next slice, not a pretend
// one (work order section 5.7).
type TinyFixtureArchitecture struct{}

func (TinyFixtureArchitecture) Name() string { return "tiny-fixture-1" }

const (
	tinyFixtureD     = 16
	tinyFixtureF     = 32
	tinyFixtureVocab = byteTokenizerVocabSize
)

func (TinyFixtureArchitecture) ParseConfig(raw []byte) (ModelConfig, error) {
	var cfg ModelConfig
	if err := jsonUnmarshalStrict(raw, &cfg); err != nil {
		return ModelConfig{}, fmt.Errorf("%w: tiny-fixture-1 config: %v", ErrMalformedContainer, err)
	}
	if cfg.Architecture != "tiny-fixture-1" {
		return ModelConfig{}, fmt.Errorf("%w: config architecture %q is not tiny-fixture-1", ErrUnsupportedModel, cfg.Architecture)
	}
	if cfg.VocabSize != tinyFixtureVocab {
		return ModelConfig{}, fmt.Errorf("%w: config vocab_size %d, the fixture tokenizer defines %d", ErrShape, cfg.VocabSize, tinyFixtureVocab)
	}
	if cfg.HiddenDim != tinyFixtureD || cfg.FeedDim != tinyFixtureF {
		return ModelConfig{}, fmt.Errorf("%w: config dims (d=%d f=%d) do not match the fixture (%d,%d)", ErrShape, cfg.HiddenDim, cfg.FeedDim, tinyFixtureD, tinyFixtureF)
	}
	if cfg.ContextLength <= 0 {
		return ModelConfig{}, fmt.Errorf("%w: config context_length %d must be positive", ErrShape, cfg.ContextLength)
	}
	if cfg.RMSEps <= 0 {
		cfg.RMSEps = 1e-5
	}
	return cfg, nil
}

func (TinyFixtureArchitecture) RequiredTensors(cfg ModelConfig) map[string]Shape {
	d, f := cfg.HiddenDim, cfg.FeedDim
	return map[string]Shape{
		"tok_emb": {cfg.VocabSize, d},
		"wq":      {d, d},
		"wk":      {d, d},
		"wv":      {d, d},
		"wff1":    {d, f},
		"wff2":    {f, d},
		"lm_head": {d, cfg.VocabSize},
		"ln1":     {d},
		"ln2":     {d},
		"lnf":     {d},
	}
}

func (TinyFixtureArchitecture) BuildGraph(m *Model, tokens []int32) (*Graph, map[string]*Tensor, error) {
	if len(tokens) == 0 {
		return nil, nil, fmt.Errorf("%w: empty prompt", ErrTokenizer)
	}
	if len(tokens) > m.Config.ContextLength {
		return nil, nil, fmt.Errorf("%w: %d tokens exceed the context length %d", ErrContextLength, len(tokens), m.Config.ContextLength)
	}
	d, vocab := m.Config.HiddenDim, m.Config.VocabSize

	idsT, err := NewTensor(DTypeI32, Shape{len(tokens)})
	if err != nil {
		return nil, nil, err
	}
	copy(idsT.Bytes(), i32AsBytes(tokens))

	// Graph inputs: the token ids plus every weight tensor.
	inputs := []string{"ids"}
	for name := range m.Weights {
		inputs = append(inputs, name)
	}
	g := NewGraph(inputs, []string{"logits"})
	g.Append(Node{
		Op: "embedding", Inputs: map[string]string{"table": "tok_emb", "ids": "ids"},
		Output: "x", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "rmsnorm", Inputs: map[string]string{"x": "x", "weight": "ln1"}, Attrs: map[string]string{"eps": fmt.Sprintf("%g", m.Config.RMSEps)},
		Output: "h", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "h", "b": "wq"},
		Output: "q", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "h", "b": "wk"},
		Output: "k", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "h", "b": "wv"},
		Output: "v", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "attention", Inputs: map[string]string{"q": "q", "k": "k", "v": "v"},
		Output: "att", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "add", Inputs: map[string]string{"a": "x", "b": "att"},
		Output: "x2", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "rmsnorm", Inputs: map[string]string{"x": "x2", "weight": "ln2"}, Attrs: map[string]string{"eps": fmt.Sprintf("%g", m.Config.RMSEps)},
		Output: "h2", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "h2", "b": "wff1"},
		Output: "m1", DType: DTypeF32, Shape: Shape{len(tokens), m.Config.FeedDim},
	})
	g.Append(Node{
		Op: "silu", Inputs: map[string]string{"x": "m1"},
		Output: "m2", DType: DTypeF32, Shape: Shape{len(tokens), m.Config.FeedDim},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "m2", "b": "wff2"},
		Output: "m3", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "add", Inputs: map[string]string{"a": "x2", "b": "m3"},
		Output: "x3", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "rmsnorm", Inputs: map[string]string{"x": "x3", "weight": "lnf"}, Attrs: map[string]string{"eps": fmt.Sprintf("%g", m.Config.RMSEps)},
		Output: "nf", DType: DTypeF32, Shape: Shape{len(tokens), d},
	})
	g.Append(Node{
		Op: "matmul", Inputs: map[string]string{"a": "nf", "b": "lm_head"},
		Output: "logits", DType: DTypeF32, Shape: Shape{len(tokens), vocab},
	})

	tensors := map[string]*Tensor{"ids": idsT}
	for name, w := range m.Weights {
		tensors[name] = w
	}
	return g, tensors, nil
}
