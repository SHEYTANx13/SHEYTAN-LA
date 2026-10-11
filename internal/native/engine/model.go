package engine

// model.go — native engine model concern (v1.1.5 Phase 2: REAL).
//
// The C++ engine loads GGUF models (validate → memory-map → metadata →
// memory plan); this file owns the Go-side model state machine and the
// IPC surface: Engine.LoadModel / Engine.UnloadModel / Engine.ModelInfo.
//
// Model states use ONE dedicated vocabulary (unloaded / loading / loaded
// / failed) — deliberately separate from the ENGINE states (llm.State*)
// which keep describing the host subprocess lifecycle. A host restart
// resets the model concern: a fresh host process has nothing mapped, so
// cached model state must never survive it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// loadModelTimeout bounds one load_model round-trip. A load is
// metadata-only (the tensor data is memory-mapped lazily), but cold
// starts on slow disks still deserve a generous, bounded window.
const loadModelTimeout = 30 * time.Second

// Model lifecycle states (wire vocabulary, mirrored by the C++ engine).
const (
	ModelStateUnloaded = "unloaded"
	ModelStateLoading  = "loading"
	ModelStateLoaded   = "loaded"
	ModelStateFailed   = "failed"
)

// ModelSpec describes a model the native engine should serve.
type ModelSpec struct {
	// Path is the model file (GGUF-class) path.
	Path string `json:"path"`

	// SizeBytes is the file size (measured at validation time).
	SizeBytes int64 `json:"sizeBytes,omitempty"`

	// Quantization, Architecture and ContextLength are model facts the
	// loader parses from the file header; the spec carries none of
	// them — they are never guessed by the caller.
	Quantization  string `json:"quantization,omitempty"`
	Architecture  string `json:"architecture,omitempty"`
	ContextLength int    `json:"contextLength,omitempty"`

	// ContextLengthOverride, when > 0, plans the KV-cache and
	// workspace with this context instead of the trained one
	// (planning only — Phase 2 allocates no context buffers).
	ContextLengthOverride uint32 `json:"contextLengthOverride,omitempty"`
}

// ModelState is the lifecycle state of a model inside the native engine.
// It is a pure snapshot of the model concern: Loaded mirrors
// State == ModelStateLoaded.
type ModelState struct {
	// Loaded reports whether a model is resident.
	Loaded bool `json:"loaded"`

	// Path of the resident / attempted model (empty when none).
	Path string `json:"path,omitempty"`

	// State is one of: "unloaded", "loading", "loaded", "failed".
	State string `json:"state,omitempty"`

	// Detail carries the failure reason or load notes.
	Detail string `json:"detail,omitempty"`
}

// ValidateModelSpec checks a model spec against the filesystem WITHOUT
// loading anything: the path must be absolute-or-resolvable, must exist,
// must be a regular file, and must sit inside the given models root (path
// jail discipline — the same class of rule every file tool enforces).
func ValidateModelSpec(modelsRoot, path string) (ModelSpec, error) {
	spec := ModelSpec{Path: path}

	if strings.TrimSpace(path) == "" {
		return spec, fmt.Errorf("model path is empty")
	}

	root, err := filepath.Abs(modelsRoot)
	if err != nil {
		return spec, fmt.Errorf("resolve models root: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return spec, fmt.Errorf("resolve model path: %w", err)
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return spec, fmt.Errorf("validate model path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return spec, fmt.Errorf("model path %q escapes the models directory", path)
	}

	fi, err := os.Stat(abs)
	if err != nil {
		return spec, fmt.Errorf("model file: %w", err)
	}

	if fi.IsDir() {
		return spec, fmt.Errorf("model path %q is a directory", path)
	}

	spec.SizeBytes = fi.Size()

	return spec, nil
}

// --- Engine-level model management ------------------------------------------

// LoadModel validates the spec, then drives the native load through the
// supervised host: validate → map → metadata → plan. The Go-side model
// state walks unloaded → loading → loaded | failed around the IPC call.
//
// Semantics (mirrored by the C++ engine):
//   - empty path / missing file: rejected before any state transition
//     (missing file walks to failed — a real attempt was made);
//   - load while loaded: replace semantics (the engine unloads first);
//   - a failed load leaves NOTHING loaded;
//   - concurrent Load/Unload calls are serialized by the engine.
func (e *Engine) LoadModel(ctx context.Context, spec ModelSpec) error {
	if strings.TrimSpace(spec.Path) == "" {
		return fmt.Errorf("model spec path is empty")
	}

	e.modelMu.Lock()
	defer e.modelMu.Unlock()

	e.mu.Lock()
	e.modelState = ModelStateUnloaded
	e.modelDetail = ""
	e.modelInfo = nil
	e.modelPlan = nil
	e.mu.Unlock()

	// Local file validation (a directory, a missing file or an empty
	// file is a caller-visible error before any IPC happens; a
	// missing file still counts as a failed attempt).
	fi, err := os.Stat(spec.Path)
	if err != nil {
		e.setModelFailedLocked(spec.Path, fmt.Sprintf("model file: %v", err))
		return fmt.Errorf("model file: %w", err)
	}
	if fi.IsDir() {
		e.setModelFailedLocked(spec.Path, fmt.Sprintf("model path %q is a directory", spec.Path))
		return fmt.Errorf("model path %q is a directory", spec.Path)
	}

	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		e.setModelFailedLocked(spec.Path, fmt.Sprintf("native engine is not running (state %s)", e.State()))
		return fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	e.mu.Lock()
	e.modelState = ModelStateLoading
	e.modelDetail = spec.Path
	e.mu.Unlock()

	payload, err := json.Marshal(LoadModelPayload{
		Path:          spec.Path,
		ContextLength: spec.ContextLengthOverride,
	})
	if err != nil {
		e.setModelFailedLocked(spec.Path, fmt.Sprintf("marshal load payload: %v", err))
		return err
	}

	loadCtx, cancel := context.WithTimeout(ctx, loadModelTimeout)
	defer cancel()

	resp, err := ipc.call(loadCtx, OpLoadModel, payload)
	if err != nil {
		detail := fmt.Sprintf("native engine load: %v", err)
		e.setModelFailedLocked(spec.Path, detail)
		// v1.7.1: typed native failure normalization (5.5).
		return NormalizeError(fmt.Errorf("%s", detail))
	}

	var result ModelOpResult
	if err := DecodeResult(resp, &result); err != nil {
		detail := fmt.Sprintf("native engine load result: %v", err)
		e.setModelFailedLocked(spec.Path, detail)
		// v1.7.1: typed native failure normalization (5.5).
		return NormalizeError(fmt.Errorf("%s", detail))
	}

	if result.State != ModelStateLoaded || result.Model == nil {
		detail := "native engine load: unexpected result state " + result.State
		e.setModelFailedLocked(spec.Path, detail)
		return fmt.Errorf("%s", detail)
	}

	e.mu.Lock()
	e.modelState = ModelStateLoaded
	e.modelDetail = ""
	e.modelInfo = result.Model
	e.modelPlan = result.Memory
	e.mu.Unlock()

	return nil
}

// UnloadModel releases the loaded model on the engine. Idempotent:
// unloading an engine with no model succeeds (the C++ side treats it the
// same way), so "Unload, Unload again" is always safe.
func (e *Engine) UnloadModel(ctx context.Context) error {
	e.modelMu.Lock()
	defer e.modelMu.Unlock()

	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		// No process → nothing can be mapped. Honest no-op that
		// still normalizes the local snapshot.
		e.setModelUnloadedLocked()
		return nil
	}

	unloadCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(unloadCtx, OpUnloadModel, nil)
	if err != nil {
		return fmt.Errorf("native engine unload: %w", err)
	}

	var result ModelOpResult
	if err := DecodeResult(resp, &result); err != nil {
		return err
	}

	if result.State != ModelStateUnloaded {
		return fmt.Errorf("native engine unload: unexpected result state %s", result.State)
	}

	e.setModelUnloadedLocked()
	return nil
}

// ModelInfo reports the model concern snapshot: the Go-cached state when
// the engine is down, a live round-trip when it is up. Never fails for a
// running engine — a fresh engine reports the unloaded state.
func (e *Engine) ModelInfo(ctx context.Context) (ModelOpResult, error) {
	e.mu.Lock()
	ipc := e.ipc
	state := e.modelState
	info := e.modelInfo
	plan := e.modelPlan
	e.mu.Unlock()

	if ipc == nil {
		// Engine down: whatever was cached cannot be trusted as
		// resident — report the honest local view.
		return ModelOpResult{
			Loaded: state == ModelStateLoaded,
			State:  state,
			Model:  info,
			Memory: plan,
		}, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpModelInfo, nil)
	if err != nil {
		return ModelOpResult{}, fmt.Errorf("native engine model info: %w", err)
	}

	var result ModelOpResult
	if err := DecodeResult(resp, &result); err != nil {
		return ModelOpResult{}, err
	}

	// Cache the authoritative answer.
	e.mu.Lock()
	e.modelState = result.State
	e.modelInfo = result.Model
	e.modelPlan = result.Memory
	e.mu.Unlock()

	return result, nil
}

// NativeModelState returns the current Go-side model state string.
func (e *Engine) NativeModelState() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelState
}

// NativeGenerationCapable reports whether the CURRENTLY loaded model
// validated as natively executable at load time (the C++ engine's
// llama-graph verdict, cached from the load result). False when no
// model is loaded, the model failed to load, or the loaded model is not
// natively executable — the llama.cpp fallback signal the backend layer
// acts on.
func (e *Engine) NativeGenerationCapable() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelState == ModelStateLoaded && e.modelInfo != nil &&
		e.modelInfo.GenerationCapable
}

// NativeGenerationReason returns the inspectable reason a loaded model
// is NOT natively executable (empty when capable or nothing is loaded).
func (e *Engine) NativeGenerationReason() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modelInfo == nil {
		return ""
	}
	return e.modelInfo.GenerationReason
}

// NativeModelPath returns the path of the currently loaded (or last
// attempted) model — a pure local read for status snapshots, so the
// /api/engine poll path never needs an IPC round-trip.
func (e *Engine) NativeModelPath() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modelInfo == nil {
		return ""
	}
	return e.modelInfo.Path
}

// setModelFailedLocked records a failed load (callers hold modelMu; the
// engine mu guards the fields themselves).
func (e *Engine) setModelFailedLocked(path, detail string) {
	e.mu.Lock()
	e.modelState = ModelStateFailed
	e.modelDetail = detail
	e.modelInfo = &NativeModelInfo{
		Path:  path,
		State: ModelStateFailed,
		Error: detail,
	}
	e.modelPlan = nil
	e.mu.Unlock()
}

// setModelUnloadedLocked normalizes the local model snapshot.
func (e *Engine) setModelUnloadedLocked() {
	e.mu.Lock()
	e.modelState = ModelStateUnloaded
	e.modelDetail = ""
	e.modelInfo = nil
	e.modelPlan = nil
	e.mu.Unlock()
}

// resetModelForHostCycle clears the model concern whenever the host
// process is (re)started or stopped: a fresh host maps nothing, so stale
// Go-side state must never survive a lifecycle boundary.
func (e *Engine) resetModelForHostCycle() {
	e.modelMu.Lock()
	e.mu.Lock()
	e.modelState = ModelStateUnloaded
	e.modelDetail = ""
	e.modelInfo = nil
	e.modelPlan = nil
	e.mu.Unlock()
	e.modelMu.Unlock()
}

// ModelInfoCached returns the cached model metadata of the currently
// loaded model (nil when nothing is loaded). v1.7.1 capability contract
// accessor — no IPC round-trip, a pure state read.
func (e *Engine) ModelInfoCached() *NativeModelInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelInfo
}

// MemoryPlanCached returns the cached memory plan of the currently
// loaded model (nil when unknown). v1.7.1 capability contract accessor.
func (e *Engine) MemoryPlanCached() *NativeMemoryPlan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modelPlan
}
