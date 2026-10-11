package engine

// tokenizer.go — native engine tokenizer concern (v1.1.5 Phase 4: REAL).
//
// The C++ engine materializes the GGUF tokenizer arrays (tokens,
// token_type, scores, merges, special-token ids) and serves encode/
// decode through the IPC protocol. This file owns the Go-side surface:
// Engine.InitTokenizer / Engine.TokenizerInfo / Engine.TokenizerEncode /
// Engine.TokenizerDecode.
//
// What this is:
//   - REAL tokenizer init that drives the C++ materialization;
//   - REAL encode that returns token ids (deterministic, bounded);
//   - REAL decode that returns UTF-8 text (bounded);
//   - honest reporting: an unsupported tokenizer model kind returns
//     ErrUnsupportedTokenizer and the llama.cpp fallback remains the
//     generation backend.
//
// What this is NOT:
//   - this is NOT a model forward pass;
//   - this is NOT generation (no logits → no sampling → no token output);
//   - the tokenizer can be used standalone to count tokens for context
//     budgeting, but it cannot produce generated text.

import (
	"context"
	"encoding/json"
	"fmt"
)

// TokenizerInfo is the JSON shape of the C++ "tokenizer_info" op result.
// Every field is filled by the C++ engine from real GGUF metadata; an
// uninitialized tokenizer leaves Initialized=false.
type TokenizerInfo struct {
	Initialized bool   `json:"initialized"`
	Model       string `json:"model"`               // "bpe", "unigram", "wpm"
	ModelName   string `json:"modelName,omitempty"` // raw tokenizer.ggml.model
	VocabSize   uint32 `json:"vocabSize"`
	MergeCount  uint32 `json:"mergeCount"`
	HasBOS      bool   `json:"hasBos"`
	HasEOS      bool   `json:"hasEos"`
	HasUnknown  bool   `json:"hasUnknown"`
	BOSID       uint32 `json:"bosId"`
	EOSID       uint32 `json:"eosId"`
	UnknownID   uint32 `json:"unknownId"`
	Error       string `json:"error,omitempty"`
}

// TokenizerInitResult is the JSON shape of the C++ "tokenizer_init" op
// result. It wraps the info struct plus an explicit Initialized flag and
// an error string for the unsupported case.
type TokenizerInitResult struct {
	Initialized bool          `json:"initialized"`
	Info        TokenizerInfo `json:"info"`
	Error       string        `json:"error,omitempty"`
}

// EncodeOptions configures one encode call.
type EncodeOptions struct {
	AddBOS    bool   `json:"addBos"`
	AddEOS    bool   `json:"addEos"`
	MaxTokens uint32 `json:"maxTokens"`
}

// EncodeResult holds the encode outcome.
type EncodeResult struct {
	IDs       []uint32 `json:"ids"`
	Count     uint32   `json:"count"`
	Truncated bool     `json:"truncated"`
}

// EncodePayload is the wire shape of the encode request payload.
type EncodePayload struct {
	Text      string `json:"text"`
	AddBOS    bool   `json:"addBos"`
	AddEOS    bool   `json:"addEos"`
	MaxTokens uint32 `json:"maxTokens"`
}

// DecodeOptions configures one decode call.
type DecodeOptions struct {
	SkipSpecial bool   `json:"skipSpecial"`
	MaxBytes    uint32 `json:"maxBytes"`
}

// DecodeResult holds the decode outcome.
// (Named TokenizerDecodeResult in this file to avoid colliding with the
// wire-level DecodeResult helper in protocol.go.)
type TokenizerDecodeResult struct {
	Text      string `json:"text"`
	Count     uint32 `json:"count"`
	Truncated bool   `json:"truncated"`
}

// DecodePayload is the wire shape of the decode request payload.
type DecodePayload struct {
	IDs         []uint32 `json:"ids"`
	SkipSpecial bool     `json:"skipSpecial"`
	MaxBytes    uint32   `json:"maxBytes"`
}

// ErrUnsupportedTokenizer signals that the loaded model's tokenizer model
// kind is not implemented in this engine build. The llama.cpp fallback
// remains the generation backend; the tokenizer cannot be initialized.
var ErrUnsupportedTokenizer = fmt.Errorf("native engine: tokenizer model not supported (llama.cpp fallback remains the generation backend)")

// InitTokenizer materializes the GGUF tokenizer for the currently loaded
// model. Idempotent: a second call returns the existing vocab. Returns
// ErrUnsupportedTokenizer for an unimplemented tokenizer model kind.
func (e *Engine) InitTokenizer(ctx context.Context) (TokenizerInitResult, error) {
	e.modelMu.Lock()
	defer e.modelMu.Unlock()

	var result TokenizerInitResult

	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return result, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	initCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(initCtx, OpTokenizerInit, nil)
	if err != nil {
		return result, fmt.Errorf("native engine tokenizer init: %w", err)
	}

	if err := DecodeResult(resp, &result); err != nil {
		return result, fmt.Errorf("native engine tokenizer init result: %w", err)
	}

	if !result.Initialized && result.Error != "" {
		// The C++ host reports "unsupported" honestly here.
		return result, ErrUnsupportedTokenizer
	}

	return result, nil
}

// TokenizerInfo reports the tokenizer snapshot without initializing it
// (a fresh engine reports Initialized=false).
func (e *Engine) TokenizerInfo(ctx context.Context) (TokenizerInfo, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return TokenizerInfo{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpTokenizerInfo, nil)
	if err != nil {
		return TokenizerInfo{}, fmt.Errorf("native engine tokenizer info: %w", err)
	}

	var info TokenizerInfo
	if err := DecodeResult(resp, &info); err != nil {
		return TokenizerInfo{}, err
	}

	return info, nil
}

// TokenizerEncode converts UTF-8 text to token ids deterministically.
// The caller can request BOS/EOS insertion; MaxTokens caps the output.
func (e *Engine) TokenizerEncode(ctx context.Context, text string, opts EncodeOptions) (EncodeResult, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return EncodeResult{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	if opts.MaxTokens == 0 {
		opts.MaxTokens = 256
	}

	payload, err := json.Marshal(EncodePayload{
		Text:      text,
		AddBOS:    opts.AddBOS,
		AddEOS:    opts.AddEOS,
		MaxTokens: opts.MaxTokens,
	})
	if err != nil {
		return EncodeResult{}, fmt.Errorf("marshal encode payload: %w", err)
	}

	encodeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(encodeCtx, OpTokenizerEncode, payload)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("native engine tokenizer encode: %w", err)
	}

	var result EncodeResult
	if err := DecodeResult(resp, &result); err != nil {
		return EncodeResult{}, err
	}

	return result, nil
}

// TokenizerDecode converts token ids back to UTF-8 text.
func (e *Engine) TokenizerDecode(ctx context.Context, ids []uint32, opts DecodeOptions) (TokenizerDecodeResult, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return TokenizerDecodeResult{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	if opts.MaxBytes == 0 {
		opts.MaxBytes = 1 << 20
	}

	payload, err := json.Marshal(DecodePayload{
		IDs:         ids,
		SkipSpecial: opts.SkipSpecial,
		MaxBytes:    opts.MaxBytes,
	})
	if err != nil {
		return TokenizerDecodeResult{}, fmt.Errorf("marshal decode payload: %w", err)
	}

	decodeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(decodeCtx, OpTokenizerDecode, payload)
	if err != nil {
		return TokenizerDecodeResult{}, fmt.Errorf("native engine tokenizer decode: %w", err)
	}

	var result TokenizerDecodeResult
	if err := DecodeResult(resp, &result); err != nil {
		return TokenizerDecodeResult{}, err
	}

	return result, nil
}

// KVCacheInfo is the JSON shape of the C++ "kv_cache_info" op result.
// Every byte count is the REAL allocation; UsedPositions is 0 until a
// forward pass exists (Phase 4 reports the honest 0).
type KVCacheInfo struct {
	Allocated         bool   `json:"allocated"`
	Quantization      string `json:"quantization,omitempty"`
	CapacityBytes     uint64 `json:"capacityBytes"`
	UsedBytes         uint64 `json:"usedBytes"`
	CapacityPositions uint64 `json:"capacityPositions"`
	UsedPositions     uint64 `json:"usedPositions"`
	LayerCount        uint32 `json:"layerCount"`
	KVDim             uint32 `json:"kvDim"`
}

// KVCacheInfo reports the measured KV-cache snapshot. In Phase 4 the
// cache is NOT auto-allocated — this op reports the honest zero-state
// (Allocated=false) until a future op explicitly allocates it.
func (e *Engine) KVCacheInfo(ctx context.Context) (KVCacheInfo, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return KVCacheInfo{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpKVCacheInfo, nil)
	if err != nil {
		return KVCacheInfo{}, fmt.Errorf("native engine kv cache info: %w", err)
	}

	var info KVCacheInfo
	if err := DecodeResult(resp, &info); err != nil {
		return KVCacheInfo{}, err
	}

	return info, nil
}

// SchedulerInfo is the JSON shape of the C++ "scheduler_info" op result.
type SchedulerInfo struct {
	ActiveRequests  uint32 `json:"activeRequests"`
	QueuedRequests  uint32 `json:"queuedRequests"`
	MaxConcurrent   uint32 `json:"maxConcurrent"`
	QueueDepthLimit uint32 `json:"queueDepthLimit"`
	TotalSubmitted  uint64 `json:"totalSubmitted"`
	TotalCompleted  uint64 `json:"totalCompleted"`
	TotalCancelled  uint64 `json:"totalCancelled"`
	TotalFailed     uint64 `json:"totalFailed"`
	ShuttingDown    bool   `json:"shuttingDown"`
}

// SchedulerInfo reports the measured scheduler snapshot. Counts are real
// (queued requests, totals since create); active is 0 in Phase 4.
func (e *Engine) SchedulerInfo(ctx context.Context) (SchedulerInfo, error) {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return SchedulerInfo{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpSchedulerInfo, nil)
	if err != nil {
		return SchedulerInfo{}, fmt.Errorf("native engine scheduler info: %w", err)
	}

	var info SchedulerInfo
	if err := DecodeResult(resp, &info); err != nil {
		return SchedulerInfo{}, err
	}

	return info, nil
}
