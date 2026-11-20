package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// jsonUnmarshalStrict decodes JSON with unknown-field rejection: a
// config carrying fields the architecture does not understand is a
// malformed answer, not a silent ignore (fail-closed provenance).
func jsonUnmarshalStrict(raw []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ModelConfig is the JSON model configuration (the Hugging Face
// config.json analogue). Real pretrained checkpoints map their config
// fields onto this structure inside an architecture adapter; the
// fixture architecture declares its fields directly.
type ModelConfig struct {
	Architecture  string  `json:"architecture"`   // registry key ("tiny-fixture-1")
	VocabSize     int     `json:"vocab_size"`     // token space upper bound
	HiddenDim     int     `json:"hidden_dim"`     // d
	FeedDim       int     `json:"feed_dim"`       // f (MLP width)
	ContextLength int     `json:"context_length"` // maximum sequence length
	RMSEps        float64 `json:"rms_eps"`
}

// Tokenizer turns text into token ids and back. Real pretrained models
// ship their own tokenizer + special-token metadata + chat template
// (work order section 5.1 layer 4); the ByteTokenizer is the VERIFIED
// fixture tokenizer: one token per byte plus BOS/EOS, deterministic and
// lossless for arbitrary bytes.
type Tokenizer interface {
	Encode(text string) ([]int32, error)
	Decode(ids []int32) (string, error)
	VocabSize() int
	BOS() int32
	EOS() int32
}

// ByteTokenizer is the fixture architecture's tokenizer: bytes 0..255
// are their own ids; BOS = 256; EOS = 257; vocab = 258. It never fails
// on input (every byte is representable) — ErrTokenizer is reserved for
// decode of out-of-range ids.
type ByteTokenizer struct{}

const (
	byteTokenizerVocabSize = 258
	byteTokenizerBOS       = int32(256)
	byteTokenizerEOS       = int32(257)
)

func (ByteTokenizer) VocabSize() int { return byteTokenizerVocabSize }
func (ByteTokenizer) BOS() int32     { return byteTokenizerBOS }
func (ByteTokenizer) EOS() int32     { return byteTokenizerEOS }

func (ByteTokenizer) Encode(text string) ([]int32, error) {
	out := make([]int32, 0, len(text)+1)
	out = append(out, byteTokenizerBOS)
	for i := 0; i < len(text); i++ {
		out = append(out, int32(text[i]))
	}
	return out, nil
}

func (ByteTokenizer) Decode(ids []int32) (string, error) {
	out := make([]byte, 0, len(ids))
	for _, id := range ids {
		if id < 0 || id >= byteTokenizerEOS {
			return "", fmt.Errorf("%w: id %d is not a byte token (valid: 0..%d)", ErrTokenizer, id, byteTokenizerEOS)
		}
		if id == byteTokenizerBOS || id == byteTokenizerEOS {
			continue
		}
		out = append(out, byte(id))
	}
	return string(out), nil
}

// Model is a loaded model: validated weights, config, tokenizer and the
// backend that executes it. The Model OWNS its weights and the loaded
// backend resources; Close releases them deterministically (never via
// finalizers).
type Model struct {
	Config    ModelConfig
	Tokenizer Tokenizer
	Backend   Backend
	// Weights maps the architecture's required tensor names to
	// validated tensors (the COMPLETE mapping — LoadModel fails closed
	// if any required tensor is missing, work order section 5.3).
	Weights map[string]*Tensor

	// Provenance: where the weights came from (for benchmark/evidence
	// records). Container kind + container digest + revision labels.
	Provenance string

	owner *Engine
}

// Close releases the model's backend resources. The model is unusable
// afterwards (every method returns ErrEngineClosed).
func (m *Model) Close() error {
	if m == nil {
		return nil
	}
	m.Weights = nil
	err := m.Backend.Close()
	return err
}

// Architecture is the model-semantics adapter (work order section 5.1
// layer 2): it validates a config, names the REQUIRED tensor mapping,
// and compiles the canonical graph for one forward pass. Every
// architecture family registers exactly one implementation; nothing
// else may hard-code model semantics.
type Architecture interface {
	Name() string
	ParseConfig(raw []byte) (ModelConfig, error)
	// RequiredTensors returns the complete tensor mapping (name ->
	// shape). LoadModel fails closed unless EVERY entry resolves.
	RequiredTensors(cfg ModelConfig) map[string]Shape
	// BuildGraph compiles one forward pass over the given token ids,
	// wiring the model's weights into the graph. The returned map must
	// contain the graph's input tensors (token ids + weights).
	BuildGraph(m *Model, tokens []int32) (*Graph, map[string]*Tensor, error)
}

// Registry is the architecture registry (typed miss = ErrUnsupportedModel).
type Registry struct {
	mu   sync.RWMutex
	arch map[string]Architecture
}

func NewRegistry() *Registry {
	return &Registry{arch: map[string]Architecture{}}
}

func (r *Registry) Register(a Architecture) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.arch[a.Name()]; exists {
		return fmt.Errorf("engine: architecture %q is already registered", a.Name())
	}
	r.arch[a.Name()] = a
	return nil
}

func (r *Registry) Lookup(name string) (Architecture, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.arch[name]
	if !ok {
		known := make([]string, 0, len(r.arch))
		for k := range r.arch {
			known = append(known, k)
		}
		sort.Strings(known)
		return nil, &UnsupportedError{Kind: "model", Name: name, Details: "registered architectures: " + fmt.Sprint(known)}
	}
	return a, nil
}
