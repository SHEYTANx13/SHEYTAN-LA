package engine

// generation.go — native engine generation concern (v1.1.5 Phase 5:
// REAL native generation through the supervised host).
//
// The full path this file drives:
//
//      Go request → IPC "generate" op → shtn-engine-host generation lane
//      → C++ engine (tokenizer → transformer forward pass → sampler →
//      KV-cache updates → repeated decode) → streamed event frames
//      (coarse chunks, never per-token) → Go stream callback.
//
// Cancellation is real: the Go context abort triggers an IPC "cancel" op
// which the host propagates to the native generation loop (observed per
// token). The stream then finishes with a "cancelled" final frame.
//
// All metrics are MEASURED by the C++ engine with a monotonic clock;
// this layer only converts and reports them.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recovery"
)

// GenerationRequest is one native generation request (the wire payload
// is built from it; the ENGINE tokenizes the prompt itself).
type GenerationRequest struct {
	// RequestID identifies the request for cancel round-trips. Empty →
	// a deterministic id is generated.
	RequestID string

	// Prompt is the plain prompt text (required).
	Prompt string

	// MaxTokens caps generation (required, > 0). The engine REJECTS
	// prompt+max_tokens beyond the model context window (explicit error,
	// no silent truncation).
	MaxTokens uint32

	// Sampling controls (mirror the llm request contract).
	Temperature       float64
	TopK              int32
	TopP              float64
	RepetitionPenalty float64
	RepeatLastN       uint32
	Seed              uint64
}

// GenerationChunk is one streamed chunk from the native engine.
type GenerationChunk struct {
	RequestID string
	Text      string
	TokenID   uint32
	Final     bool
}

// GenerationMetrics is the measured metrics of a completed request.
type GenerationMetrics struct {
	PromptSeconds         float64
	TTFTSeconds           float64
	DecodeSeconds         float64
	TotalSeconds          float64
	TokensPerSecond       float64
	PromptTokensPerSecond float64
	KVPositionsUsed       uint64
}

// GenerationResult is the final outcome of one generation.
type GenerationResult struct {
	RequestID       string
	FinishReason    string // eos | length | cancelled | stop
	PromptTokens    uint32
	GeneratedTokens uint32
	Metrics         GenerationMetrics
}

// ContextExhaustedError reports a request that cannot fit the model's
// context window (rejected by the engine, no silent truncation).
//
// v1.7.1: the structured native rejection unwraps into the ONE shared
// condition (recovery.ErrContextExhausted), so the runtime detects
// exhaustion identically for both backends via
// recovery.IsContextExhausted / errors.Is — while this concrete type
// stays the native boundary's own evidence carrier.
type ContextExhaustedError struct {
	Detail string
}

func (e *ContextExhaustedError) Error() string {
	return e.Detail
}

// Unwrap maps the structured native context-bound rejection into the
// shared runtime condition contract.
func (e *ContextExhaustedError) Unwrap() error {
	return recovery.ErrContextExhausted
}

// IsContextExhausted reports whether err is a context-window rejection
// (the concrete native type, or anything unwrapping into the shared
// recovery condition).
func IsContextExhausted(err error) bool {
	if _, ok := err.(*ContextExhaustedError); ok {
		return true
	}
	return recovery.IsContextExhausted(err)
}

// generateStallTimeout mirrors the llama.cpp stream contract: no overall
// client timeout, but a zero-progress watchdog (5 minutes without a
// single frame) aborts the stream.
const generateStallTimeout = 5 * time.Minute

var generationSeq atomic.Int64

// newRequestID returns a deterministic unique request id.
func newRequestID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, generationSeq.Add(1))
}

// StreamGeneration performs one native generation request through the
// supervised host, invoking onChunk for every streamed event chunk and
// returning the final result. The context bounds the CALLER's patience;
// cancellation propagates to the native generation loop through the
// cancel op (real cooperative cancellation — the loop observes it at
// every token).
func (e *Engine) StreamGeneration(ctx context.Context, req GenerationRequest,
	onChunk func(GenerationChunk) error) (GenerationResult, error) {
	if req.Prompt == "" {
		return GenerationResult{}, fmt.Errorf("native generation: prompt is empty")
	}
	if req.MaxTokens == 0 {
		return GenerationResult{}, fmt.Errorf("native generation: max tokens must be > 0")
	}

	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return GenerationResult{}, fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	requestID := req.RequestID
	if requestID == "" {
		requestID = newRequestID("go")
	}

	payload, err := json.Marshal(GeneratePayload{
		RequestID:         requestID,
		Prompt:            req.Prompt,
		MaxTokens:         req.MaxTokens,
		Temperature:       req.Temperature,
		TopK:              req.TopK,
		TopP:              req.TopP,
		RepetitionPenalty: req.RepetitionPenalty,
		RepeatLastN:       req.RepeatLastN,
		Seed:              req.Seed,
	})
	if err != nil {
		return GenerationResult{}, err
	}

	// Generation window: engine state ready → busy → ready (the EXISTING
	// vocabulary — no new states).
	e.setState(llm.StateBusy)

	// Zero-progress watchdog: every frame (event or final) resets it.
	stallCtx, stallCancel := context.WithCancel(context.Background())
	defer stallCancel()
	var stallTimer *time.Timer
	var stallMu chan struct{} // serialized timer resets via a tiny mutex
	_ = stallMu
	stallTimer = time.AfterFunc(generateStallTimeout, stallCancel)
	resetStall := func() {
		if stallTimer != nil {
			stallTimer.Reset(generateStallTimeout)
		}
	}

	// Combine the caller's patience with the stall watchdog.
	genCtx, genCancel := context.WithCancel(ctx)
	defer genCancel()
	go func() {
		select {
		case <-stallCtx.Done():
			genCancel()
		case <-genCtx.Done():
		}
	}()

	var finalResp *Response
	var streamErr error

	resp, err := ipc.streamCall(genCtx, OpGenerate, payload, func(ev *Response) error {
		resetStall()

		var chunk GenerationEventResult
		if err := json.Unmarshal(ev.Result, &chunk); err != nil {
			return fmt.Errorf("malformed generation event: %w", err)
		}

		if onChunk != nil {
			if err := onChunk(GenerationChunk{
				RequestID: chunk.RequestID,
				Text:      chunk.Text,
				TokenID:   chunk.Token,
				Final:     chunk.Final,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	resetStall()

	if resp != nil {
		finalResp = resp
	} else {
		streamErr = err
	}

	stallTimer.Stop()

	// The engine is reusable regardless of outcome.
	defer e.setState(llm.StateReady)

	if streamErr != nil {
		// Map the explicit engine rejections onto inspectable errors.
		if strings.Contains(streamErr.Error(), "exceeds the model context window") {
			return GenerationResult{RequestID: requestID, FinishReason: "error"},
				&ContextExhaustedError{Detail: streamErr.Error()}
		}
		// v1.7.1: normalize every other native failure into the shared
		// typed categories (§5.5) — raw host text stays embedded as detail.
		return GenerationResult{RequestID: requestID, FinishReason: "error"},
			NormalizeError(fmt.Errorf("native generation: %w", streamErr))
	}

	if !finalResp.OK {
		// Map the explicit engine rejections to inspectable errors.
		if ctxErr := context.Cause(genCtx); ctxErr != nil && finalResp.Error == "" {
			// Stall abort: surface it honestly — and v1.7.5: keep the
			// context sentinel in the chain, so a PAUSE-driven cancel is
			// still classifiable by the orchestrator's errors.Is(err,
			// context.Canceled) check.
			return GenerationResult{RequestID: requestID, FinishReason: "error"},
				fmt.Errorf("native generation stalled (no frames for %v): %w", generateStallTimeout, ctxErr)
		}
		if isContextOverflowMessage(finalResp.Error) {
			return GenerationResult{RequestID: requestID, FinishReason: "error"},
				&ContextExhaustedError{Detail: finalResp.Error}
		}
		// v1.7.1: typed native failure normalization (§5.5).
		return GenerationResult{RequestID: requestID, FinishReason: "error"},
			NormalizeError(fmt.Errorf("native generation: %s", finalResp.Error))
	}

	var final GenerationFinalResult
	if err := json.Unmarshal(finalResp.Result, &final); err != nil {
		return GenerationResult{RequestID: requestID, FinishReason: "error"},
			fmt.Errorf("malformed generation final result: %w", err)
	}

	return GenerationResult{
		RequestID:       final.RequestID,
		FinishReason:    final.FinishReason,
		PromptTokens:    final.PromptTokens,
		GeneratedTokens: final.GeneratedTokens,
		Metrics: GenerationMetrics{
			PromptSeconds:         final.Metrics.PromptSeconds,
			TTFTSeconds:           final.Metrics.TTFTSeconds,
			DecodeSeconds:         final.Metrics.DecodeSeconds,
			TotalSeconds:          final.Metrics.TotalSeconds,
			TokensPerSecond:       final.Metrics.TokensPerSecond,
			PromptTokensPerSecond: final.Metrics.PromptTokensPerSecond,
			KVPositionsUsed:       final.Metrics.KVPositionsUsed,
		},
	}, nil
}

// isContextOverflowMessage detects the engine's context-bound rejection
// text (explicit reject policy).
func isContextOverflowMessage(msg string) bool {
	return msg != "" && strings.Contains(msg, "exceeds the model context window")
}

// CancelGeneration cancels the in-flight generation with this id
// (cooperative — the native loop observes it per token).
func (e *Engine) CancelGeneration(ctx context.Context, requestID string) error {
	e.mu.Lock()
	ipc := e.ipc
	e.mu.Unlock()

	if ipc == nil {
		return fmt.Errorf("native engine is not running (state %s)", e.State())
	}

	payload, err := json.Marshal(CancelPayload{RequestID: requestID})
	if err != nil {
		return err
	}

	probeCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	resp, err := ipc.call(probeCtx, OpCancel, payload)
	if err != nil {
		return fmt.Errorf("native engine cancel: %w", err)
	}

	var result CancelResult
	if err := DecodeResult(resp, &result); err != nil {
		return err
	}

	if !result.Cancelled {
		return fmt.Errorf("native engine did not cancel: %s", result.Reason)
	}

	return nil
}

// GenerationStats is the generation snapshot reported through metrics.
// All timing fields are zero until the engine actually serves generation
// (measured values only — never a guess).
type GenerationStats struct {
	// ActiveRequests: in-flight generation requests (measured).
	ActiveRequests int `json:"activeRequests,omitempty"`

	// TTFTSeconds: time-to-first-token of the most recent completed
	// request (measured; absent until generation exists).
	TTFTSeconds float64 `json:"ttftSeconds,omitempty"`

	// PromptTokensPerSecond: prompt processing speed of the most recent
	// completed request (measured; absent until generation exists).
	PromptTokensPerSecond float64 `json:"promptTokensPerSecond,omitempty"`

	// DecodeTokensPerSecond: decode speed of the most recent completed
	// request (measured; absent until generation exists).
	DecodeTokensPerSecond float64 `json:"decodeTokensPerSecond,omitempty"`
}
