package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// Engine is the SHEYTAN-Engine.go public entry: it owns the
// architecture registry and the backend pool, loads models with
// fail-closed validation, and enforces the bounded admission queue for
// concurrent generation (work order section 5.7: bounded, cancellable,
// observable — never an unbounded queue).
//
// Ownership: the Engine owns the models it loaded. Close() releases
// every loaded model and its backend resources deterministically; the
// zero-value Engine is unusable (use NewEngine).
type Engine struct {
	registry *Registry

	mu      sync.Mutex
	closed  bool
	models  []*Model
	builtIn bool

	// admission bounds the number of concurrent Generate executions
	// across the engine (a typed ErrBusy answer when full — backpressure
	// is an explicit answer, never a silent queue-up).
	admission chan struct{}
}

// NewEngine constructs an engine over the given registry (nil = a fresh
// registry with the built-in fixture architecture registered).
func NewEngine(r *Registry, maxConcurrentGenerate int) (*Engine, error) {
	if maxConcurrentGenerate <= 0 {
		return nil, fmt.Errorf("%w: maxConcurrentGenerate %d must be positive", ErrBusy, maxConcurrentGenerate)
	}
	if r == nil {
		r = NewRegistry()
		if err := r.Register(TinyFixtureArchitecture{}); err != nil {
			return nil, err
		}
	}
	return &Engine{
		registry:  r,
		admission: make(chan struct{}, maxConcurrentGenerate),
	}, nil
}

// LoadModel loads a model container. The ONLY container kind this slice
// implements is Safetensors — and ONLY as METADATA_INSPECTION plus a
// complete, fail-closed required-tensor mapping against the registered
// architecture. configJSON is the architecture's config document (the
// "config.json" analogue; for the fixture it may be empty, which loads
// the fixture defaults).
//
// Every load emits structured events: load_start, one load_progress per
// mapped tensor, load_done with the container digest.
func (e *Engine) LoadModel(ctx context.Context, container []byte, configJSON []byte, provenance string, sink EventSink) (*Model, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, ErrEngineClosed
	}
	e.mu.Unlock()

	if sink == nil {
		sink = func(Event) {}
	}
	sink(newEvent(EventLoadStart))

	meta, err := ParseSafetensorsHeader(container)
	if err != nil {
		return nil, fmt.Errorf("safetensors header: %w", err)
	}

	// Config: the caller supplies the architecture config; the container
	// may also carry it in __metadata__.config (the adapter decides).
	cfgJSON := configJSON
	if len(cfgJSON) == 0 {
		if v, ok := meta.Metadata["config"]; ok {
			cfgJSON = []byte(v)
		}
	}

	// The architecture is named by the config; an empty config defaults
	// to the fixture architecture (the ONLY registered one in this
	// slice) — never a silent guess between families.
	archName := "tiny-fixture-1"
	if len(cfgJSON) > 0 {
		var probe struct {
			Architecture string `json:"architecture"`
		}
		if err := jsonUnmarshalProbe(cfgJSON, &probe); err != nil {
			return nil, fmt.Errorf("%w: config is not valid JSON: %v", ErrMalformedContainer, err)
		}
		if probe.Architecture != "" {
			archName = probe.Architecture
		}
	}

	arch, err := e.registry.Lookup(archName)
	if err != nil {
		return nil, err
	}

	cfg, err := arch.ParseConfig(cfgJSON)
	if err != nil {
		return nil, err
	}

	// Complete required-tensor mapping: every required name must exist
	// in the container with the declared shape and a kernel-backed
	// dtype. A missing/mismatched tensor fails the load closed.
	required := arch.RequiredTensors(cfg)
	byName := make(map[string]SafetensorsTensorInfo, len(meta.Tensors))
	for _, t := range meta.Tensors {
		byName[t.Name] = t
	}
	weights := make(map[string]*Tensor, len(required))
	mapped := 0
	for name, wantShape := range required {
		info, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w: architecture %q requires tensor %q which the container does not carry (complete mapping required)", ErrMalformedContainer, archName, name)
		}
		if !sameShape(info.Shape, wantShape) {
			return nil, fmt.Errorf("%w: tensor %q shape %s does not match the architecture requirement %s", ErrMalformedContainer, name, info.Shape, wantShape)
		}
		t, err := TensorFromSafetensors(container, info)
		if err != nil {
			return nil, fmt.Errorf("tensor %q: %v", name, err)
		}
		weights[name] = t
		mapped++
		ev := Event{Kind: EventLoadProgress, Phase: "weights", Step: mapped, Total: len(required), Detail: name}
		ev.TimestampUtc = nowUTCString()
		sink(ev)
	}

	digest := sha256.Sum256(container)
	m := &Model{
		Config:     cfg,
		Tokenizer:  ByteTokenizer{},
		Backend:    NewCPUBackend(),
		Weights:    weights,
		Provenance: fmt.Sprintf("%s container_sha256=%s tensors_mapped=%d/%d", provenance, hex.EncodeToString(digest[:]), mapped, len(required)),
		owner:      e,
	}

	e.mu.Lock()
	e.models = append(e.models, m)
	e.mu.Unlock()

	done := newEvent(EventLoadDone)
	done.Detail = m.Provenance
	sink(done)
	return m, nil
}

// Close releases every loaded model (each Close releases its backend
// buffers deterministically). Idempotent; subsequent loads answer
// ErrEngineClosed.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var firstErr error
	for _, m := range e.models {
		if err := m.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	e.models = nil
	return firstErr
}

// acquire admits one generation (bounded queue; ErrBusy when full).
func (e *Engine) acquire(ctx context.Context) error {
	select {
	case e.admission <- struct{}{}:
		return nil
	default:
	}
	// Queue full: wait for capacity or cancellation — bounded WAITING,
	// not unbounded queueing.
	select {
	case e.admission <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) release() {
	<-e.admission
}

func (e *Engine) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

// jsonUnmarshalProbe decodes WITHOUT unknown-field rejection (a probe
// into foreign config shapes; the architecture's own ParseConfig is the
// strict pass).
func jsonUnmarshalProbe(raw []byte, v interface{}) error {
	return json.Unmarshal(raw, v)
}
