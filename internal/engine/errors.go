// Package engine is SHEYTAN-Engine.go — the genuine Go inference engine
// foundation (master work order section 5): one shared execution core with
// explicitly separated model semantics, graph execution, kernels, device
// backends, memory, scheduling, sampling and benchmark evidence.
//
// Delivered scope in this slice (evidence labels, never marketing):
//
//   - typed Engine/Model/Session/Backend interfaces with explicit
//     Close() ownership and context-aware load/generate (VERIFIED);
//   - tensor dtype/shape/stride/bounds validation with checked integer
//     arithmetic (VERIFIED);
//   - a scalar CPU reference backend and kernel set, the correctness
//     reference for every future optimized or device kernel (VERIFIED);
//   - a canonical graph IR with validation and a deterministic CPU
//     executor (VERIFIED);
//   - Safetensors strict header parsing — METADATA_INSPECTION_ONLY (the
//     container header is parsed and validated; weight-byte access for a
//     real pretrained model is NOT yet verified);
//   - a deterministic in-repo fixture architecture ("tiny-fixture-1")
//     with byte tokenizer, real weights, complete tensor mapping, graph
//     forward path and seeded sampling — executed END TO END (VERIFIED:
//     the numbers come from the kernels, not from canned text);
//   - structured load/generation progress events (VERIFIED);
//   - a benchmark harness recording the machine-readable fields of
//     section 5.8 (VERIFIED as scaffolding; no comparative performance
//     claims are made — llama.cpp, vLLM, SGLang, TensorRT-LLM, MLX, MLC
//     and LiteRT-LM comparisons require reproducible evidence that does
//     not exist yet).
//
// NOT in this slice (typed seams, exact prerequisites documented in
// docs/architecture/sheytan-engine-architecture.md):
//
//   - GGUF execution (typed unsupported; version-aware container
//     inspection is scheduled behind the same strict-parser pattern);
//   - LiteRT-LM adapter (adapter seam marked unavailable: the official
//     pinned runtime is not vendored, built or tested here);
//   - GPU/vendor backends (the backend contract exists; only the CPU
//     reference backend is registered);
//   - real pretrained checkpoint execution (Safetensors is
//     METADATA_INSPECTION_ONLY until a complete architecture adapter,
//     tokenizer and tensor mapping run on real weights).
package engine

import (
	"errors"
	"fmt"
)

// Sentinel errors — every failure path in the engine is one of these
// (wrapped with %w by the producers so errors.Is answers honestly).
var (
	// ErrUnsupportedModel: the model architecture is not registered.
	ErrUnsupportedModel = errors.New("engine: unsupported model architecture")

	// ErrUnsupportedOperator: the operation is not implemented by the
	// selected backend (kernel registry miss).
	ErrUnsupportedOperator = errors.New("engine: unsupported operator")

	// ErrUnsupportedDType: the tensor dtype is not implemented for the
	// requested operation/backend.
	ErrUnsupportedDType = errors.New("engine: unsupported dtype")

	// ErrUnsupportedQuantization: the quantization format has no
	// implemented and tested decoder. Unknown quantization types are
	// NEVER silently reinterpreted (work order section 5.4).
	ErrUnsupportedQuantization = errors.New("engine: unsupported quantization")

	// ErrUnsupportedBackend: no registered backend satisfies the request.
	ErrUnsupportedBackend = errors.New("engine: unsupported backend")

	// ErrMemory: an allocation exceeded a declared bound (tensor size,
	// KV cache budget, admission limits).
	ErrMemory = errors.New("engine: memory limit exceeded")

	// ErrShape: tensor shapes do not agree for the requested operation.
	ErrShape = errors.New("engine: shape mismatch")

	// ErrMalformedContainer: a model container (Safetensors/GGUF) failed
	// strict structural validation.
	ErrMalformedContainer = errors.New("engine: malformed model container")

	// ErrSessionClosed: the session was closed and no longer owns state.
	ErrSessionClosed = errors.New("engine: session closed")

	// ErrEngineClosed: the engine was closed; resources are released.
	ErrEngineClosed = errors.New("engine: engine closed")

	// ErrBusy: the bounded admission queue is full — a backpressure
	// signal, never a silent queue-up.
	ErrBusy = errors.New("engine: admission queue full")

	// ErrTokenizer: the tokenizer cannot represent the input.
	ErrTokenizer = errors.New("engine: tokenizer failure")

	// ErrContextLength: the request exceeds the model context length.
	ErrContextLength = errors.New("engine: context length exceeded")
)

// ShapeError reports a precise shape disagreement (operation, expected,
// actual) so a kernel mismatch is debuggable, not a mystery.
type ShapeError struct {
	Op       string
	Expected string
	Actual   string
}

func (e *ShapeError) Error() string {
	return fmt.Sprintf("%s: op %q expected %s, got %s", ErrShape.Error(), e.Op, e.Expected, e.Actual)
}

func (e *ShapeError) Unwrap() error { return ErrShape }

// UnsupportedError is the generic typed unsupported answer carrying WHAT
// is unsupported and WHY the engine refuses (never a silent fallback).
type UnsupportedError struct {
	Kind    string // "model" | "operator" | "dtype" | "quantization" | "backend"
	Name    string // the unsupported identifier
	Details string // the exact prerequisite that is missing
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("engine: unsupported %s %q: %s", e.Kind, e.Name, e.Details)
}

// Unwrap maps the unsupported KIND back to its sentinel so callers can
// classify with errors.Is (an unsupported operator IS
// ErrUnsupportedOperator, an unsupported dtype IS ErrUnsupportedDType,
// ...).
func (e *UnsupportedError) Unwrap() error {
	switch e.Kind {
	case "model":
		return ErrUnsupportedModel
	case "operator":
		return ErrUnsupportedOperator
	case "dtype":
		return ErrUnsupportedDType
	case "quantization":
		return ErrUnsupportedQuantization
	case "backend":
		return ErrUnsupportedBackend
	default:
		return nil
	}
}
