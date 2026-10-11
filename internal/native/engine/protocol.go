package engine

// protocol.go — the SHEYTAN Native API wire protocol (v1.1.5 Phase 5).
//
// Frame layout (both directions, binary-safe):
//
//      [4 bytes little-endian payload length][length bytes of UTF-8 JSON]
//
// Request:
//
//      {"id": 1, "op": "health"}
//      {"id": 2, "op": "load_model", "payload": {"path": "...", "contextLength": 0}}
//      {"id": 3, "op": "generate", "payload": {"requestId": "...", "prompt": "...",
//            "maxTokens": 64, "temperature": 0.7, "topK": 40, "topP": 0.95,
//            "repetitionPenalty": 1.1, "repeatLastN": 64, "seed": 42}}
//
// Response (one or more frames per request id):
//
//      {"id": 1, "ok": true, "result": { ... }}                        (final)
//      {"id": 3, "ok": true, "event": "chunk", "result": { ... }}      (stream event)
//      {"id": 3, "ok": true, "result": { ... }}                        (final, same id)
//      {"id": 1, "ok": false, "error": "human-readable reason"}       (final, error)
//
// Operations (coarse-grained by design — no tiny high-frequency calls
// cross this boundary; generation streams BATCHED chunks, never one
// frame per token):
//
//      ping              handshake: protocol + ABI version negotiation
//      health            active engine health probe
//      hwinfo            hardware capability profile (detected values)
//      metrics           engine metrics snapshot (measured values)
//      cancel            cooperative cancellation of one generation request
//      load_model        validate + memory-map a GGUF model, extract metadata, plan memory
//      unload_model      release the loaded model (idempotent)
//      model_info        snapshot of the model concern (state + metadata + plan)
//      tokenizer_init    materialize the GGUF tokenizer (Phase 4)
//      tokenizer_info    tokenizer snapshot (initialized, vocab size, specials)
//      tokenizer_encode  UTF-8 text → token ids
//      tokenizer_decode  token ids → UTF-8 text
//      kv_cache_info     measured KV-cache snapshot (Phase 5: real population)
//      scheduler_info    measured scheduler snapshot (Phase 5: real execution)
//      generate          REAL native generation: streamed event frames + final
//      shutdown          graceful engine shutdown
//
// Unknown ops and malformed frames produce a bounded error response (or
// are rejected as a protocol violation on the Go side); they NEVER crash
// either side. Frame size is capped so a hostile or buggy peer cannot
// exhaust memory.
//
// Version history: v1 = Phase 1 (lifecycle/health/hardware/metrics);
// v2 = Phase 2 (model loading surface added); v3 = Phase 4
// (tokenizer/KV/scheduler surface added); v4 = Phase 5 (REAL generation:
// generate op with streamed event frames + real cancel semantics — the
// Phase 4 cancel stub answered "no generation requests exist"). Both
// sides are bumped together — a mismatch is a hard handshake failure
// (fail closed).

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ProtocolVersion is the wire protocol version implemented here. The C++
// host reports its own value in the ping result; a mismatch is a hard
// handshake failure (fail closed).
const ProtocolVersion = 4

// MaxFrameBytes bounds one protocol frame (1 MiB). Anything larger is a
// protocol violation, not a buffer to allocate.
const MaxFrameBytes = 1 << 20

// Op names crossing the boundary.
const (
	OpPing            = "ping"
	OpHealth          = "health"
	OpHardware        = "hwinfo"
	OpMetrics         = "metrics"
	OpCancel          = "cancel"
	OpLoadModel       = "load_model"
	OpUnloadModel     = "unload_model"
	OpModelInfo       = "model_info"
	OpTokenizerInit   = "tokenizer_init"
	OpTokenizerInfo   = "tokenizer_info"
	OpTokenizerEncode = "tokenizer_encode"
	OpTokenizerDecode = "tokenizer_decode"
	OpKVCacheInfo     = "kv_cache_info"
	OpSchedulerInfo   = "scheduler_info"
	OpGenerate        = "generate"
	OpShutdown        = "shutdown"
)

// ValidOps is the closed set of accepted operations (validation + tests).
var ValidOps = map[string]bool{
	OpPing:            true,
	OpHealth:          true,
	OpHardware:        true,
	OpMetrics:         true,
	OpCancel:          true,
	OpLoadModel:       true,
	OpUnloadModel:     true,
	OpModelInfo:       true,
	OpTokenizerInit:   true,
	OpTokenizerInfo:   true,
	OpTokenizerEncode: true,
	OpTokenizerDecode: true,
	OpKVCacheInfo:     true,
	OpSchedulerInfo:   true,
	OpGenerate:        true,
	OpShutdown:        true,
}

// ErrFrameTooLarge reports a frame exceeding MaxFrameBytes.
var ErrFrameTooLarge = errors.New("native engine protocol: frame exceeds 1 MiB cap")

// ErrFrameClosed reports the peer closed the stream.
var ErrFrameClosed = errors.New("native engine protocol: stream closed")

// ErrFrameTruncated reports a short read/write mid-frame.
var ErrFrameTruncated = errors.New("native engine protocol: truncated frame")

// Request is one outbound operation.
type Request struct {
	ID      int64           `json:"id"`
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is one inbound result. Streaming responses carry the same id
// as the request: every frame with a non-empty Event is an intermediate
// event (delivered to the stream callback); the frame WITHOUT an Event
// member is the final response that completes the call.
type Response struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Event  string          `json:"event,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// IsFinal reports whether this response completes its request (no event
// marker — event frames are intermediate by definition).
func (r *Response) IsFinal() bool { return r.Event == "" }

// PingResult is the handshake answer.
type PingResult struct {
	ProtocolVersion int    `json:"protocolVersion"`
	ABIVersion      uint32 `json:"abiVersion"`
	Engine          string `json:"engine"`
}

// CancelPayload identifies the request to cancel.
type CancelPayload struct {
	RequestID string `json:"requestId"`
}

// CancelResult reports what the engine did with a cancel request.
type CancelResult struct {
	Cancelled bool   `json:"cancelled"`
	Reason    string `json:"reason,omitempty"`
}

// LoadModelPayload is the load_model request payload. ContextLength 0
// plans with the model's own trained context length.
type LoadModelPayload struct {
	Path          string `json:"path"`
	ContextLength uint32 `json:"contextLength,omitempty"`
}

// NativeModelInfo is the GGUF metadata the C++ engine actually read or
// derived. Zero means "not present in the file" — nothing is guessed.
type NativeModelInfo struct {
	Path            string `json:"path"`
	Architecture    string `json:"architecture,omitempty"`
	Name            string `json:"name,omitempty"`
	Quantization    string `json:"quantization,omitempty"`
	State           string `json:"state,omitempty"`
	Error           string `json:"error,omitempty"`
	FileSizeBytes   uint64 `json:"fileSizeBytes,omitempty"`
	ParameterCount  uint64 `json:"parameterCount,omitempty"`
	ContextLength   uint64 `json:"contextLength,omitempty"`
	VocabularySize  uint64 `json:"vocabularySize,omitempty"`
	EmbeddingLength uint64 `json:"embeddingLength,omitempty"`
	LayerCount      uint64 `json:"layerCount,omitempty"`
	TensorCount     uint32 `json:"tensorCount,omitempty"`
	GGUFVersion     uint32 `json:"ggufVersion,omitempty"`
	FileType        uint32 `json:"fileType,omitempty"`
	HasFileType     bool   `json:"hasFileType,omitempty"`
	KVCacheBytes    uint64 `json:"kvCacheBytes,omitempty"`
	WorkspaceBytes  uint64 `json:"workspaceBytes,omitempty"`
	TotalPlanBytes  uint64 `json:"totalPlanBytes,omitempty"`

	// GenerationCapable (Phase 5): the load-time native-inference
	// verdict — the llama graph validated against real GGUF metadata
	// (every required tensor present with the right shape and a
	// supported type). False + GenerationReason is the explicit,
	// inspectable fallback signal the backend layer acts on.
	GenerationCapable bool   `json:"generationCapable"`
	GenerationReason  string `json:"generationReason,omitempty"`
}

// NativeMemoryPlan is the load-time memory budget computed by the C++
// engine from parsed metadata (nothing is allocated to produce it).
type NativeMemoryPlan struct {
	ModelFileBytes       uint64 `json:"modelFileBytes,omitempty"`
	MappedBytes          uint64 `json:"mappedBytes,omitempty"`
	WeightsBytes         uint64 `json:"weightsBytes,omitempty"`
	WorkspaceBytes       uint64 `json:"workspaceBytes,omitempty"`
	KVCacheBytes         uint64 `json:"kvCacheBytes,omitempty"`
	RuntimeOverheadBytes uint64 `json:"runtimeOverheadBytes,omitempty"`
	TotalBytes           uint64 `json:"totalBytes,omitempty"`
	AvailableRAMBytes    uint64 `json:"availableRamBytes,omitempty"`

	// FitsInRAM: 1 yes / 0 no / -1 unknown (no RAM probe).
	FitsInRAM int32 `json:"fitsInRam"`
}

// ModelOpResult is the shared result shape of the model ops:
// load_model / unload_model / model_info. Model and Memory are present
// only when the engine has something to report (a load attempt was
// made); a fresh engine reports just the unloaded state.
type ModelOpResult struct {
	Loaded bool   `json:"loaded"`
	State  string `json:"state"`

	Model  *NativeModelInfo  `json:"model,omitempty"`
	Memory *NativeMemoryPlan `json:"memory,omitempty"`
}

// --- Phase 5: generation wire types ---------------------------------------

// GeneratePayload is the generate op request payload. Prompt is the
// plain prompt text — the ENGINE tokenizes it with its own materialized
// GGUF tokenizer (the real native path: Go request → engine tokenizer →
// forward pass).
type GeneratePayload struct {
	RequestID         string  `json:"requestId"`
	Prompt            string  `json:"prompt"`
	MaxTokens         uint32  `json:"maxTokens"`
	Temperature       float64 `json:"temperature,omitempty"`
	TopK              int32   `json:"topK,omitempty"`
	TopP              float64 `json:"topP,omitempty"`
	RepetitionPenalty float64 `json:"repetitionPenalty,omitempty"`
	RepeatLastN       uint32  `json:"repeatLastN,omitempty"`
	Seed              uint64  `json:"seed,omitempty"`
}

// GenerationEventResult is one streamed "event":"chunk" frame's result.
type GenerationEventResult struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
	Token     uint32 `json:"token"`
	Final     bool   `json:"final,omitempty"`
}

// GenerationMetricsWire is the metrics object inside the generate op's
// final frame (the engine-level view lives in generation.go).
type GenerationMetricsWire struct {
	PromptSeconds         float64 `json:"promptSeconds"`
	TTFTSeconds           float64 `json:"ttftSeconds"`
	DecodeSeconds         float64 `json:"decodeSeconds"`
	TotalSeconds          float64 `json:"totalSeconds"`
	TokensPerSecond       float64 `json:"tokensPerSecond"`
	PromptTokensPerSecond float64 `json:"promptTokensPerSecond"`
	KVPositionsUsed       uint64  `json:"kvPositionsUsed"`
}

// GenerationFinalResult is the generate op's final frame result.
type GenerationFinalResult struct {
	RequestID       string                `json:"requestId"`
	FinishReason    string                `json:"finishReason"`
	PromptTokens    uint32                `json:"promptTokens"`
	GeneratedTokens uint32                `json:"generatedTokens"`
	Metrics         GenerationMetricsWire `json:"metrics"`
}

// Finish reasons (mirrors the C++ SHTN_FINISH_* values).
const (
	FinishEOS       = "eos"
	FinishLength    = "length"
	FinishCancelled = "cancelled"
)

// WriteFrame writes one length-prefixed JSON payload.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameBytes {
		return ErrFrameTooLarge
	}

	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}

	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}

	return nil
}

// ReadFrame reads one length-prefixed payload, enforcing the size cap and
// treating EOF as stream closure.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte

	n, err := io.ReadFull(r, header[:])
	if err != nil {
		if n == 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe)) {
			return nil, ErrFrameClosed
		}
		return nil, fmt.Errorf("read frame header: %w", err)
	}

	size := binary.LittleEndian.Uint32(header[:])

	if size == 0 {
		return nil, fmt.Errorf("native engine protocol: zero-length frame")
	}

	if size > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}

	buf := make([]byte, size)

	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, ErrFrameTruncated
	}

	return buf, nil
}

// EncodeRequest marshals and frames a request.
func EncodeRequest(w io.Writer, req *Request) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	return WriteFrame(w, payload)
}

// DecodeRequest parses and validates an inbound request (used by the fake
// host in tests; the C++ host performs the equivalent validation natively).
func DecodeRequest(payload []byte) (*Request, error) {
	var req Request

	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("malformed request: %w", err)
	}

	if req.Op == "" {
		return nil, fmt.Errorf("malformed request: missing op")
	}

	if !ValidOps[req.Op] {
		return nil, fmt.Errorf("unknown op %q", req.Op)
	}

	return &req, nil
}

// DecodeResponse parses an inbound response. Error responses to
// unparseable requests may carry id 0; request/response matching is the
// caller's job (the pending map never has id 0 registered).
func DecodeResponse(payload []byte) (*Response, error) {
	var resp Response

	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, fmt.Errorf("malformed response: %w", err)
	}

	return &resp, nil
}

// EncodeResponse frames a response.
func EncodeResponse(w io.Writer, resp *Response) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	return WriteFrame(w, payload)
}

// DecodeResult unmarshals a successful response's result into out.
func DecodeResult(resp *Response, out any) error {
	if resp == nil {
		return fmt.Errorf("nil response")
	}

	if !resp.OK {
		return fmt.Errorf("native engine error: %s", resp.Error)
	}

	if len(resp.Result) == 0 {
		return fmt.Errorf("native engine protocol: ok response without result")
	}

	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("malformed result payload: %w", err)
	}

	return nil
}
