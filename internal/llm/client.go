package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// Message is an OpenAI-style chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`

	// Reasoning holds the model's separate thinking trace (v1.0.2
	// thinking mode): native reasoning_content from the endpoint, or
	// extracted <think>…</think> blocks. Persisted with the session but
	// never re-sent to the API (json:"-" on the wire would break some
	// strict servers, so it is stripped when building requests via
	// StripReasoning).
	Reasoning   string   `json:"reasoning,omitempty"`
	Attachments []string `json:"attachments,omitempty"` // display-only file names

	// Images (v1.0.6 vision): file paths of images attached to this
	// message. Persisted with the session so history keeps them; on the
	// WIRE they are converted into OpenAI content parts (image_url data
	// URLs) for the current turn and degraded to text notes for older
	// turns (see wireMessages — re-encoding every historical image every
	// iteration would stall the agent loop).
	Images []string `json:"images,omitempty"`

	// Feedback (v1.0.6): the user's 👍/👎 verdict on an assistant message.
	// 0 = none, +1 = like, -1 = dislike. Persisted with the session and
	// mirrored into the recall engine (liked answers rank higher in future
	// memory searches). Never sent to the API.
	Feedback int `json:"feedback,omitempty"`

	// At (v1.0.6): when the message was sent — rendered as the bubble's
	// timestamp. Zero = unknown (legacy sessions). Never sent to the API.
	At time.Time `json:"at,omitempty"`
}

// StripReasoning returns a copy of msgs with the reasoning and attachment
// display fields removed — the chat-completions wire format knows neither.
func StripReasoning(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].Reasoning = ""
		out[i].Attachments = nil
		out[i].Feedback = 0
		out[i].At = time.Time{}
	}
	return out
}

type ToolCall struct {
	// Index identifies the streaming slot this delta belongs to. OpenAI-
	// compatible servers stream tool-call arguments in fragments tagged
	// with index; assembling by index (not by arrival order) is required
	// when several tool calls are streamed interleaved.
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Client is the OpenAI-compatible chat client. It talks to either the
// bundled llama.cpp server (provider=local) or any remote OpenAI-compatible
// endpoint (provider=remote) using cfg.EffectiveBaseURL/APIKey.
type Client struct {
	// src is the live config source: sampling values are read per request
	// so Settings patches apply to the very next call (v1.1.4: previously
	// a shared mutable pointer — a data race against the config patcher).
	src  *config.Source
	http *http.Client

	// streamHTTP serves streaming requests. It carries NO overall timeout
	// (a 10-minute cap silently truncated long generations); the request
	// context plus the stream stall watchdog provide the bounds instead.
	streamHTTP *http.Client

	// v1.1.3: engine busy hook. Runtime wiring sets it to LlamaServer
	// MarkBusy so real inference traffic flips the authoritative engine
	// state ready↔busy for the UI. Nil = no reporting (tests).
	busyMu   sync.RWMutex
	busyHook func(busy bool)

	// v1.0.6: encoded-image cache. The agent loop rebuilds the request
	// every iteration; without the cache a 1 MB screenshot would be
	// base64-encoded dozens of times per turn. Keyed by path, invalidated
	// by mtime, bounded by entry count AND total bytes (v1.2.4).
	imgCacheMu    sync.Mutex
	imgCache      map[string]imageCacheEntry
	imgCacheOrder []string
	imgCacheBytes int64
}

// SetBusyHook wires the engine busy reporter. Safe under concurrency
// (v1.1.4: previously an unsynchronized write raced the streaming read
// path — benign only because wiring happened at boot, now guaranteed).
func (c *Client) SetBusyHook(fn func(busy bool)) {
	c.busyMu.Lock()
	c.busyHook = fn
	c.busyMu.Unlock()
}

// markBusy reports an inference window to the wired hook, if any.
func (c *Client) markBusy(busy bool) {
	c.busyMu.RLock()
	fn := c.busyHook
	c.busyMu.RUnlock()

	if fn != nil {
		fn(busy)
	}
}

type imageCacheEntry struct {
	modTime time.Time
	url     string
}

// v1.2.4: the image cache is bounded by TOTAL BYTES, not only entry count.
// Encoded data-URLs are up to ~6 MB each (vision.EncodedBudget), so eight
// entries could retain ~48 MB of base64 text. The cache now evicts
// oldest-first once the byte cap is hit; a wholesale reset stays as the
// fallback so the worst case remains one oversized entry.
const (
	imgCacheMaxEntries = 8
	imgCacheMaxBytes   = 24 << 20 // 24 MiB of retained base64
)

// newTunedTransport builds the shared HTTP transport (v1.0.4 Speed Pack):
// keep-alive connections are pooled per host so the health checks, the
// streaming chat calls, and parallel tool requests reuse one warm
// connection instead of paying a fresh TCP handshake every time.
func newTunedTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     120 * time.Second,
		DisableCompression:  false,
	}
}

func NewClient(src *config.Source) *Client {
	// v1.1.4: two clients. `http` keeps the 10-minute overall timeout for
	// non-streaming calls. `streamHTTP` has no overall timeout — long
	// generations are legal — but the transport bounds the response HEADER
	// wait and the SSE read loop is guarded by the stall watchdog in
	// streamOnce (5 minutes without a single byte = abort).
	streamTransport := newTunedTransport()
	streamTransport.ResponseHeaderTimeout = 2 * time.Minute

	return &Client{
		src:        src,
		http:       &http.Client{Timeout: 10 * time.Minute, Transport: newTunedTransport()},
		streamHTTP: &http.Client{Transport: streamTransport},
	}
}

// ChatRequest is the body sent to /v1/chat/completions.
type ChatRequest struct {
	Model            string     `json:"model"`
	Messages         []Message  `json:"-"` // marshaled via MarshalJSON (wire form)
	Temperature      float64    `json:"temperature,omitempty"`
	TopP             float64    `json:"top_p,omitempty"`
	TopK             int        `json:"top_k,omitempty"`
	MinP             float64    `json:"min_p,omitempty"` // llama.cpp sampler
	MaxTokens        int        `json:"max_tokens,omitempty"`
	Stop             []string   `json:"stop,omitempty"`
	Seed             int        `json:"seed,omitempty"`
	PresencePenalty  float64    `json:"presence_penalty,omitempty"`  // OpenAI standard
	FrequencyPenalty float64    `json:"frequency_penalty,omitempty"` // OpenAI standard
	RepeatLastN      int        `json:"repeat_last_n,omitempty"`     // llama.cpp sampler
	Stream           bool       `json:"stream,omitempty"`
	Tools            []ToolSpec `json:"tools,omitempty"`
	NumCtx           int        `json:"n_ctx,omitempty"`
	// ReasoningBudget (v1.8.5) is the REAL numeric thinking-token
	// budget for the request: 0 = end thinking immediately, > 0 = cap
	// thinking at that many tokens, nil = no client-side budget
	// (the engine default). It rides the llama.cpp OpenAI-compatible
	// endpoint as `reasoning_budget_tokens` — verified present in BOTH
	// managed engine builds (b10642 and b11205) at the REQUEST level
	// (the server only applies it when the model's chat template
	// exposes a thinking section; a non-thinking model ignores it —
	// never fabricated reasoning). A pointer keeps 0 (disable) on the
	// wire — omitempty drops only the unset (nil) case. Set by the
	// orchestrator from the composer's reasoning level; remote
	// providers never receive it.
	ReasoningBudget *int `json:"reasoning_budget_tokens,omitempty"`
	// CachePrompt asks llama.cpp to reuse the KV cache across turns
	// (v1.0.4). Together with --cache-reuse on the server this collapses
	// the repeated agent prefix (AI context + tool schemas) to a near-zero
	// prefill after the first turn. Harmless no-op on other servers.
	CachePrompt bool `json:"cache_prompt"`

	// wire (v1.0.6) is the precomputed multimodal wire form produced by
	// BuildChatRequest: messages with images become OpenAI content parts.
	// nil → MarshalJSON derives a plain string-content wire copy (the
	// pre-v1.0.6 behavior, used by direct constructions like the
	// multi-agent planner prompts).
	wire []wireMessage `json:"-"`
}

// wireMessage is the on-the-wire message: Content is either a string or a
// content-parts array (multimodal).
type wireMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"` // string | []contentPart
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"` // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// MarshalJSON projects the request into the OpenAI wire format: the
// precomputed multimodal wire form when present, otherwise the plain
// string-content form. Display-only fields (reasoning, attachments,
// feedback, image paths) never reach the wire.
func (r *ChatRequest) MarshalJSON() ([]byte, error) {
	msgs := r.wire
	if msgs == nil && r.Messages != nil {
		msgs = plainWireMessages(r.Messages)
	}
	return json.Marshal(struct {
		Model            string      `json:"model"`
		Messages         interface{} `json:"messages"`
		Temperature      float64     `json:"temperature,omitempty"`
		TopP             float64     `json:"top_p,omitempty"`
		TopK             int         `json:"top_k,omitempty"`
		MinP             float64     `json:"min_p,omitempty"`
		MaxTokens        int         `json:"max_tokens,omitempty"`
		Stop             []string    `json:"stop,omitempty"`
		Seed             int         `json:"seed,omitempty"`
		PresencePenalty  float64     `json:"presence_penalty,omitempty"`
		FrequencyPenalty float64     `json:"frequency_penalty,omitempty"`
		RepeatLastN      int         `json:"repeat_last_n,omitempty"`
		Stream           bool        `json:"stream,omitempty"`
		Tools            []ToolSpec  `json:"tools,omitempty"`
		NumCtx           int         `json:"n_ctx,omitempty"`
		// v1.8.5 reasoning budget: pointer keeps the explicit 0
		// (disable thinking) on the wire; omitempty drops only
		// the unset case.
		ReasoningBudget *int `json:"reasoning_budget_tokens,omitempty"`
		CachePrompt     bool `json:"cache_prompt"`
	}{
		Model:            r.Model,
		Messages:         msgs,
		Temperature:      r.Temperature,
		TopP:             r.TopP,
		TopK:             r.TopK,
		MinP:             r.MinP,
		MaxTokens:        r.MaxTokens,
		Stop:             r.Stop,
		Seed:             r.Seed,
		PresencePenalty:  r.PresencePenalty,
		FrequencyPenalty: r.FrequencyPenalty,
		RepeatLastN:      r.RepeatLastN,
		Stream:           r.Stream,
		Tools:            r.Tools,
		NumCtx:           r.NumCtx,
		ReasoningBudget:  r.ReasoningBudget,
		CachePrompt:      r.CachePrompt,
	})
}

// plainWireMessages is the pre-v1.0.6 projection: string content, display
// fields stripped, images degraded to text notes (callers that build a
// ChatRequest directly never carry images — this is a safety net).
func plainWireMessages(msgs []Message) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMessage{
			Role:       m.Role,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		content := m.Content
		if len(m.Images) > 0 {
			var names []string
			for _, p := range m.Images {
				names = append(names, filepath.Base(p))
			}
			note := "[image attached: " + strings.Join(names, ", ") + "]"
			if content == "" {
				content = note
			} else {
				content += "\n" + note
			}
		}
		if content != "" {
			wm.Content = content
		}
		out = append(out, wm)
	}
	return out
}

type ToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// ChatResponse is a non-streaming response.
type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// baseURL/apiKey resolve the active provider endpoints.
func (c *Client) baseURL() string { return c.src.Load().EffectiveBaseURL() }
func (c *Client) apiKey() string  { return c.src.Load().EffectiveAPIKey() }

// promptStats returns message count + total chars for logging.
func promptStats(msgs []Message) (int, int) {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return len(msgs), n
}

// offlineError is the fail-fast error returned when a remote provider is
// unreachable because the machine has no internet. The GUI surfaces its
// message directly so the user knows local mode keeps working.
func (c *Client) offlineError() error {
	return errors.New("remote LLM endpoint unreachable: no internet connection detected. " +
		"Switch to the local provider (Agent → LLM provider → local llama.cpp) to keep working offline — " +
		"all local tools continue to function")
}

// isLocalBaseURL reports whether an endpoint lives on this machine
// (127.0.0.1, localhost, ::1). Local servers — llama.cpp, Ollama, LM Studio
// — keep working while offline, so the offline fast-fail must not fire.
func isLocalBaseURL(base string) bool {
	b := strings.ToLower(base)
	return strings.Contains(b, "localhost") || strings.Contains(b, "127.0.0.1") ||
		strings.Contains(b, "[::1]") || strings.Contains(b, "0.0.0.0")
}

// offlineBlocked reports whether the active provider is a REMOTE endpoint
// that cannot be reached while offline.
func (c *Client) offlineBlocked() bool {
	cfg := c.src.Load()
	return cfg.IsRemote() && !isLocalBaseURL(c.baseURL()) && netcheck.IsOffline()
}

// Chat sends a non-streaming chat request with retry on transient errors.
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Offline fail-fast: skip the 4-attempt/19s backoff ladder when the
	// machine simply has no connectivity (local endpoints are exempt).
	if c.offlineBlocked() {
		err := c.offlineError()
		c.logCall(req, time.Now(), 0, 0, "", err)
		return nil, err
	}
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			logging.Default().Warn("llm", "retry %d after %v", attempt, lastErr)
			if err := sleepCtx(ctx, retryDelay(attempt)); err != nil {
				return nil, err
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			c.baseURL()+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey())

		start := time.Now()
		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("LLM call: %w", err)
			c.logCall(req, start, 0, 0, "", lastErr)
			if isTransient(err) {
				continue
			}
			return nil, lastErr
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			lastErr = fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, truncateStr(string(buf), 512))
			continue // retryable
		}
		if resp.StatusCode != 200 {
			buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			// v1.7.1: real context exhaustion is a typed condition —
			// detected once at this boundary, never re-grepped upstream.
			err := wrapContextExhaustion(fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, truncateStr(string(buf), 512)))
			c.logCall(req, start, 0, 0, "", err)
			return nil, err
		}
		var out ChatResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()

		if decodeErr != nil {
			c.logCall(req, start, 0, 0, "", decodeErr)
			return nil, decodeErr
		}

		if len(out.Choices) == 0 {
			err := errors.New(
				"LLM response contained no choices",
			)

			c.logCall(
				req,
				start,
				0,
				0,
				"",
				err,
			)

			return nil, err
		}

		finish := ""
		contentLen := 0
		toolCalls := 0
		if len(out.Choices) > 0 {
			finish = out.Choices[0].FinishReason
			contentLen = len(out.Choices[0].Message.Content)
			toolCalls = len(out.Choices[0].Message.ToolCalls)
		}
		c.logCall(req, start, contentLen, toolCalls, finish, nil)
		return &out, nil
	}
	// v1.1.4: retry exhaustion was previously invisible in llm.jsonl —
	// only individual transport errors were logged, never the final
	// give-up. This is the record that actually explains a dead turn.
	if lastErr != nil {
		lastErr = wrapContextExhaustion(lastErr)
		c.logCall(req, time.Now(), 0, 0, "", lastErr)
	}
	return nil, lastErr
}

// isTransient reports whether a transport error is worth retrying.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "broken pipe")
}

// retryDelay returns the backoff before retry attempt n (1-based):
// 2s, 5s, 12s — long enough to ride out API rate-limit windows.
func retryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 2 * time.Second
	case 2:
		return 5 * time.Second
	default:
		return 12 * time.Second
	}
}

// sleepCtx sleeps for d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StreamChat sends a streaming chat request. The callback is invoked for
// every chunk. A chunk may contain a delta of either content text or a
// tool_call. When `finishReason` is non-empty the stream is done.
//
// Robustness notes:
//   - tool-call argument fragments are assembled BY INDEX, so multiple
//     interleaved tool calls stream correctly
//   - if the server ignores `stream:true` and returns plain JSON, the
//     response is decoded and replayed as a single event
//   - both `data: ` and `data:` prefixes are accepted
type StreamEvent struct {
	Content      string
	Reasoning    string // native thinking trace delta (reasoning_content / reasoning)
	ToolCalls    []ToolCall
	FinishReason string
	Usage        *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}

	// TimingMark (v1.2.6 continuation) carries a TRANSPORT-level timing
	// observation from the HTTP streaming client to its consumer — the
	// moment itself, measured where it happens, never derived from
	// content deltas:
	//
	//      "request_sent"     — the request was handed to the transport
	//      "response_headers" — the response headers arrived
	//      "first_byte"       — the first body byte was READ off the wire
	//
	// A TimingMark event carries no content; consumers fold it into their
	// own clock. Emitted by streamOnce (llm client), consumed by the agent
	// RunClock (internal/agent/orchestrator.go).
	TimingMark string `json:"-"`
}

// Transport timing marks (StreamEvent.TimingMark values). The strings are
// the agent package's stage names — kept in sync deliberately so the mark
// maps 1:1 onto RunClock stages.
const (
	TimingMarkRequestSent    = "request_sent"
	TimingMarkResponseHeader = "response_headers"
	TimingMarkFirstByte      = "first_byte"
)

// PerfStats is the live speed telemetry of one streaming call (v1.0.4
// Speed Pack): time-to-first-token and tokens/sec, LM Studio-style — what
// local-LLM users expect to see.
type PerfStats struct {
	TTFTMs       int     // request start → first content delta
	Tokens       int     // completion tokens (usage when present, else deltas)
	TokensPerSec float64 // generation throughput
	PromptTokens int     // from usage when the server reports it
	WallMs       int     // total call duration
}

// String renders the compact HUD line: "41.2 tok/s · first token 0.8s".
func (p PerfStats) String() string {
	if p.TokensPerSec <= 0 || p.WallMs <= 0 {
		return ""
	}
	s := fmt.Sprintf("%.1f tok/s", p.TokensPerSec)
	if p.TTFTMs > 0 {
		s += fmt.Sprintf(" · first token %.1fs", float64(p.TTFTMs)/1000)
	}
	return s
}

// toolCallAssembler assembles streaming tool-call deltas by index.
type toolCallAssembler struct {
	order []int       // indices in first-seen order
	byIdx map[int]int // index -> position in calls
	calls map[int]*ToolCall
}

func newAssembler() *toolCallAssembler {
	return &toolCallAssembler{byIdx: map[int]int{}, calls: map[int]*ToolCall{}}
}

func (a *toolCallAssembler) add(tc ToolCall) {
	if _, ok := a.byIdx[tc.Index]; !ok {
		a.byIdx[tc.Index] = len(a.order)
		a.order = append(a.order, tc.Index)
	}
	pos := a.byIdx[tc.Index]
	if _, ok := a.calls[pos]; !ok {
		clone := tc
		a.calls[pos] = &clone
		return
	}
	existing := a.calls[pos]
	if tc.ID != "" {
		existing.ID = tc.ID
	}
	if tc.Type != "" {
		existing.Type = tc.Type
	}
	if tc.Function.Name != "" {
		existing.Function.Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		existing.Function.Arguments += tc.Function.Arguments
	}
}

func (a *toolCallAssembler) all() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		if c, ok := a.calls[a.byIdx[idx]]; ok {
			out = append(out, *c)
		}
	}
	return out
}

func (c *Client) StreamChat(ctx context.Context, req *ChatRequest, onEvent func(StreamEvent) error) error {
	_, err := c.StreamChatDetailed(ctx, req, onEvent)
	return err
}

// StreamChatDetailed streams like StreamChat and additionally returns the
// performance telemetry of the successful attempt (v1.0.4).
func (c *Client) StreamChatDetailed(ctx context.Context, req *ChatRequest, onEvent func(StreamEvent) error) (PerfStats, error) {
	// Offline fail-fast (remote provider): skip retries entirely.
	if c.offlineBlocked() {
		err := c.offlineError()
		c.logCall(req, time.Now(), 0, 0, "", err)
		return PerfStats{}, err
	}
	req.Stream = true
	// v1.0.4: ask the engine to reuse the cached prompt prefix across
	// turns (agent loop prefill collapse; no-op elsewhere).
	req.CachePrompt = true
	body, err := json.Marshal(req)
	if err != nil {
		return PerfStats{}, err
	}
	var lastErr error
	var perf PerfStats
	c.markBusy(true)
	defer c.markBusy(false)
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			logging.Default().Warn("llm", "stream retry %d after %v", attempt, lastErr)
			if err := sleepCtx(ctx, retryDelay(attempt)); err != nil {
				return perf, err
			}
		}
		var emitted bool
		// per-attempt telemetry
		tr := newTokenTimer()
		lastErr = c.streamOnce(ctx, req, body, func(ev StreamEvent) error {
			// v1.2.6 continuation: transport timing marks are
			// observations, not content — they never count as
			// "emitted" for the retry rule (retrying a stream
			// that only produced a request_sent mark is safe:
			// no content was seen).
			if ev.TimingMark == "" {
				emitted = true
				tr.observe(ev)
			}
			return onEvent(ev)
		})
		perf = tr.stats()
		// v1.1.7: remember what a REAL generation measured so the
		// Settings → Performance view shows honest tok/s and TTFT.
		if lastErr == nil && (perf.Tokens > 0 || perf.PromptTokens > 0) {
			recordPerfSample(PerfSample{
				At:           time.Now(),
				Model:        req.Model,
				PromptTokens: perf.PromptTokens,
				Tokens:       perf.Tokens,
				TokensPerSec: perf.TokensPerSec,
				TTFTMs:       int64(perf.TTFTMs),
				WallMs:       int64(perf.WallMs),
			})
		}
		// Retry only when nothing was emitted yet (retrying mid-stream
		// would duplicate content the caller already saw).
		if lastErr == nil || emitted {
			return perf, lastErr
		}
		if !isTransient(lastErr) && !isRetryableStatus(lastErr) {
			return perf, lastErr
		}
	}
	// v1.1.4: record the final give-up (mirrors the Chat path).
	if lastErr != nil {
		c.logCall(req, time.Now(), 0, 0, "", lastErr)
	}
	return perf, lastErr
}

// tokenTimer measures TTFT + throughput of one streaming attempt.
type tokenTimer struct {
	start    time.Time
	firstAt  time.Time
	deltas   int // content/reasoning deltas seen (token approximation)
	usageTok int // authoritative completion tokens when reported
	prompt   int
	done     time.Time
}

func newTokenTimer() *tokenTimer { return &tokenTimer{start: time.Now()} }

func (t *tokenTimer) observe(ev StreamEvent) {
	if ev.Content != "" || ev.Reasoning != "" {
		t.deltas++
		if t.firstAt.IsZero() {
			t.firstAt = time.Now()
		}
	}
	if ev.Usage != nil {
		if ev.Usage.CompletionTokens > 0 {
			t.usageTok = ev.Usage.CompletionTokens
		}
		if ev.Usage.PromptTokens > 0 {
			t.prompt = ev.Usage.PromptTokens
		}
	}
}

func (t *tokenTimer) stats() PerfStats {
	t.done = time.Now()
	p := PerfStats{WallMs: int(t.done.Sub(t.start).Milliseconds()), PromptTokens: t.prompt}
	if !t.firstAt.IsZero() {
		p.TTFTMs = int(t.firstAt.Sub(t.start).Milliseconds())
	}
	tokens := t.usageTok
	if tokens == 0 {
		tokens = t.deltas
	}
	p.Tokens = tokens
	genMs := t.done.Sub(t.firstAt).Milliseconds()
	if tokens > 0 && genMs > 0 {
		p.TokensPerSec = float64(tokens) / (float64(genMs) / 1000)
	}
	return p
}

// isRetryableStatus inspects an error produced by streamOnce for HTTP-level
// retry codes embedded in the message.
func isRetryableStatus(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 429") || strings.Contains(msg, "HTTP 5")
}

// SSE wire tokens (v1.0.9 byte-level pump).
var (
	sseDataPrefix = []byte("data:")
	sseDoneToken  = []byte("[DONE]")
)

// streamStallTimeout is how long a stream may deliver ZERO bytes before the
// watchdog aborts it. Slow-but-alive streams never trip this; only truly
// stalled connections do. (var: tests shrink the window.)
var streamStallTimeout = 5 * time.Minute

func (c *Client) streamOnce(ctx context.Context, req *ChatRequest, body []byte, onEvent func(StreamEvent) error) error {
	// v1.1.4: the request runs on a child context the stall watchdog can
	// cancel. A blocked SSE body read cannot be interrupted by any reader
	// wrapper — canceling the request context is the only reliable way to
	// unwind a hung connection.
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST",
		c.baseURL()+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey())
	httpReq.Header.Set("Accept", "text/event-stream")

	start := time.Now()

	// v1.2.6 continuation: MEASURED transport timing. request_sent is
	// the moment the request is handed to the transport — the caller's
	// clock learns the REAL send time (the previous code marked it
	// before the request was even built). Emitted through onEvent so
	// every routing seam (GenerationStream router, native fallback)
	// observes it without signature churn.
	if err := onEvent(StreamEvent{TimingMark: TimingMarkRequestSent}); err != nil {
		return err
	}

	// v1.1.4: streaming uses the timeout-free client — the overall
	// bound is the caller's context plus the stall watchdog below.
	resp, err := c.streamHTTP.Do(httpReq)
	if err != nil {
		err = fmt.Errorf("LLM stream: %w", err)
		c.logCall(req, start, 0, 0, "", err)
		return err
	}
	defer resp.Body.Close()

	// response_headers: the server answered — headers are on the wire.
	if err := onEvent(StreamEvent{TimingMark: TimingMarkResponseHeader}); err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		// v1.7.1: typed context-exhaustion detection at the backend boundary.
		err := wrapContextExhaustion(fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, truncateStr(string(buf), 512)))
		c.logCall(req, start, 0, 0, "", err)
		return err
	}

	// Non-SSE fallback: server returned plain JSON despite stream=true.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "text/event-stream") {
		// The body read starts here — the first bytes are on the wire.
		if err := onEvent(StreamEvent{TimingMark: TimingMarkFirstByte}); err != nil {
			return err
		}

		var out ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			c.logCall(req, start, 0, 0, "", err)
			return err
		}
		ev := StreamEvent{}
		if len(out.Choices) > 0 {
			ev.Content = out.Choices[0].Message.Content
			ev.Reasoning = out.Choices[0].Message.Reasoning
			ev.ToolCalls = out.Choices[0].Message.ToolCalls
			ev.FinishReason = out.Choices[0].FinishReason
			if ev.FinishReason == "" {
				ev.FinishReason = "stop"
			}
		}
		c.logCall(req, start, len(ev.Content), len(ev.ToolCalls), ev.FinishReason, nil)
		return onEvent(ev)
	}

	// v1.0.9 (TURBINE): the SSE pump scans BYTES — scanner.Bytes() with
	// prefix/trim handled at the byte level, so a data line only becomes a
	// string when it is actually a JSON chunk worth decoding. The old loop
	// allocated two strings per SSE line (scanner.Text() + TrimPrefix) even
	// for comment/keep-alive lines, which on a fast stream meant thousands
	// of short-lived allocations per reply (GC pressure = dropped frames).
	//
	// v1.1.4 stall watchdog: a stalled engine (deadlocked generation,
	// half-dead TCP connection) previously hung the stream until the
	// 10-minute client timeout — which in turn silently TRUNCATED every
	// longer generation. The watchdog aborts only when NO bytes arrive
	// for streamStallTimeout; active generations never trigger it.
	var lastData int64 // unix nano of the last received byte
	atomic.StoreInt64(&lastData, time.Now().UnixNano())

	// The watchdog cancels the REQUEST context: a blocked SSE body read
	// cannot be interrupted by any reader wrapper — reqCancel is the only
	// reliable way to unwind a hung connection. The tick adapts to the
	// stall window (3 checks per window, clamped to 200ms..15s).
	tick := streamStallTimeout / 3
	if tick < 200*time.Millisecond {
		tick = 200 * time.Millisecond
	}
	if tick > 15*time.Second {
		tick = 15 * time.Second
	}

	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-reqCtx.Done():
				return
			case <-ticker.C:
				last := atomic.LoadInt64(&lastData)
				if time.Since(time.Unix(0, last)) > streamStallTimeout {
					reqCancel()
					return
				}
			}
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	asm := newAssembler()
	var contentChars int
	var finishReason string
	var streamErr error

	// firstByteSeen latches the one-time first_byte mark: the first
	// scanner.Scan() returning a line IS the read of the first body
	// bytes off the network (a keep-alive comment line counts exactly
	// as much as a data line — both are network bytes).
	firstByteSeen := false

	markFirstByte := func() error {
		if firstByteSeen {
			return nil
		}
		firstByteSeen = true
		return onEvent(StreamEvent{TimingMark: TimingMarkFirstByte})
	}

	emit := func(ev StreamEvent) error {
		contentChars += len(ev.Content)
		if ev.FinishReason != "" {
			finishReason = ev.FinishReason
		}
		return onEvent(ev)
	}

	defer func() {
		c.logCall(req, start, contentChars, len(asm.all()), finishReason, streamErr)
	}()

	for scanner.Scan() {
		atomic.StoreInt64(&lastData, time.Now().UnixNano())

		if err := markFirstByte(); err != nil {
			streamErr = err
			return err
		}

		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue
		}
		data := bytes.TrimSpace(line[len(sseDataPrefix):])
		if len(data) == 0 {
			continue
		}
		if bytes.Equal(data, sseDoneToken) {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content    string     `json:"content"`
					Reasoning  string     `json:"reasoning_content"` // DeepSeek/GLM style
					Reasoning2 string     `json:"reasoning"`         // some vendors' alt field
					ToolCalls  []ToolCall `json:"tool_calls"`
					Role       string     `json:"role"`
				} `json:"delta"`
				Message struct {
					Content   string     `json:"content"`
					Reasoning string     `json:"reasoning_content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				} `json:"message"` // some servers send full messages mid-stream
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		ev := StreamEvent{Usage: chunk.Usage}
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			ev.Content = choice.Delta.Content
			ev.Reasoning = choice.Delta.Reasoning
			if ev.Reasoning == "" {
				ev.Reasoning = choice.Delta.Reasoning2
			}
			// Full-message variant (some servers emit it for the
			// first chunk or when replaying non-SSE JSON as SSE).
			if ev.Content == "" && choice.Message.Content != "" {
				ev.Content = choice.Message.Content
			}
			if ev.Reasoning == "" && choice.Message.Reasoning != "" {
				ev.Reasoning = choice.Message.Reasoning
			}
			if len(choice.Message.ToolCalls) > 0 && len(choice.Delta.ToolCalls) == 0 {
				ev.ToolCalls = choice.Message.ToolCalls
			}
			ev.FinishReason = choice.FinishReason
			if len(choice.Delta.ToolCalls) > 0 {
				for _, tc := range choice.Delta.ToolCalls {
					asm.add(tc)
				}
				ev.ToolCalls = asm.all()
			}
		}
		if err := emit(ev); err != nil {
			streamErr = err
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		// Distinguish a watchdog abort from a transport failure so the
		// visible error says what actually happened.
		if reqCtx.Err() != nil && ctx.Err() == nil {
			err = fmt.Errorf(
				"LLM stream stalled: no data for %v — the engine was generating nothing (aborted)",
				streamStallTimeout,
			)
		}
		streamErr = err
		return err
	}
	return nil
}

// logCall records one LLM call to the log catcher (llm.jsonl).
func (c *Client) logCall(req *ChatRequest, start time.Time, completionChars, toolCalls int, finish string, err error) {
	msgs, chars := promptStats(req.Messages)
	rec := logging.LLMCallRecord{
		TS:              start,
		Provider:        c.src.Load().ProviderKind(),
		Model:           req.Model,
		PromptMsgs:      msgs,
		PromptChars:     chars,
		CompletionChars: completionChars,
		ToolCalls:       toolCalls,
		FinishReason:    finish,
		DurationMs:      time.Since(start).Milliseconds(),
	}
	if err != nil {
		rec.Error = err.Error()
	}
	logging.Default().LLMCall(rec)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// imageDataURL returns the data-URL form of an image file, cached by path
// and mtime (the agent loop rebuilds the request every iteration — the cache
// turns dozens of base64 encodings into one). Bounded by entry count AND by
// total retained bytes (v1.2.4).
func (c *Client) imageDataURL(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	c.imgCacheMu.Lock()
	if e, ok := c.imgCache[path]; ok && e.modTime.Equal(fi.ModTime()) {
		c.imgCacheMu.Unlock()
		return e.url, nil
	}
	c.imgCacheMu.Unlock()

	url, err := vision.EncodeImage(path)
	if err != nil {
		return "", err
	}
	c.imgCacheMu.Lock()
	if c.imgCache == nil {
		c.imgCache = map[string]imageCacheEntry{}
		c.imgCacheOrder = c.imgCacheOrder[:0]
		c.imgCacheBytes = 0
	}
	if _, exists := c.imgCache[path]; !exists {
		c.imgCacheOrder = append(c.imgCacheOrder, path)
		c.imgCacheBytes += int64(len(url))
	}
	c.imgCache[path] = imageCacheEntry{modTime: fi.ModTime(), url: url}
	// LRU-style byte/count eviction: drop OLDEST entries until within bounds.
	for c.imgCacheBytes > imgCacheMaxBytes || len(c.imgCache) > imgCacheMaxEntries {
		if len(c.imgCacheOrder) == 0 {
			break
		}
		oldest := c.imgCacheOrder[0]
		c.imgCacheOrder = c.imgCacheOrder[1:]
		if e, ok := c.imgCache[oldest]; ok {
			c.imgCacheBytes -= int64(len(e.url))
			delete(c.imgCache, oldest)
		}
	}
	if c.imgCacheBytes > imgCacheMaxBytes {
		// Single oversized entry — reset wholesale (previous behaviour).
		c.imgCache = map[string]imageCacheEntry{}
		c.imgCacheOrder = c.imgCacheOrder[:0]
		c.imgCacheBytes = 0
	}
	c.imgCacheMu.Unlock()
	return url, nil
}

// TrimImageCache releases the entire encoded-image cache (v1.2.4
// coordinated cleanup hook). Returns the bytes retained before the drop.
// Pure cache — the next imageDataURL call simply re-encodes on demand.
func (c *Client) TrimImageCache() int64 {
	c.imgCacheMu.Lock()
	defer c.imgCacheMu.Unlock()
	freed := c.imgCacheBytes
	if c.imgCache != nil {
		c.imgCache = map[string]imageCacheEntry{}
	}
	c.imgCacheOrder = c.imgCacheOrder[:0]
	c.imgCacheBytes = 0
	return freed
}

// wireMessages projects messages into the multimodal wire form (v1.0.6).
//
// Images are LIVE (real image_url parts) only for the current turn's tail —
// the last user message and everything after it (tool results of this turn,
// e.g. screenshots the agent just took). Older images degrade to a text note:
// re-encoding every historical image on every iteration would stall the loop
// for minutes and most context windows cannot hold them anyway.
//
// Tool-role images are LOCAL-only: llama.cpp accepts image parts on tool
// messages, but the OpenAI wire spec (and most hosted endpoints) require
// string content there — remote providers get the degrading note instead.
func (c *Client) wireMessages(msgs []Message) []wireMessage {
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}
	remote := c.src.Load().IsRemote()
	out := make([]wireMessage, 0, len(msgs))
	for i, m := range msgs {
		wm := wireMessage{
			Role:       m.Role,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		if len(m.Images) == 0 {
			if m.Content != "" {
				wm.Content = m.Content
			}
			out = append(out, wm)
			continue
		}
		live := lastUser >= 0 && i >= lastUser
		if m.Role == "tool" && remote {
			live = false
		}
		if !live {
			var names []string
			for _, p := range m.Images {
				names = append(names, filepath.Base(p))
			}
			note := "[image attached earlier: " + strings.Join(names, ", ") + "]"
			content := m.Content
			if content == "" {
				content = note
			} else {
				content += "\n" + note
			}
			wm.Content = content
			out = append(out, wm)
			continue
		}
		// Live multimodal content: text part + up to MaxImagesPerMessage
		// image parts.
		var parts []contentPart
		if m.Content != "" {
			parts = append(parts, contentPart{Type: "text", Text: m.Content})
		}
		appended := 0
		for _, p := range m.Images {
			if appended >= vision.MaxImagesPerMessage {
				break
			}
			url, err := c.imageDataURL(p)
			if err != nil {
				parts = append(parts, contentPart{Type: "text",
					Text: fmt.Sprintf("[image %s could not be loaded: %v]", filepath.Base(p), err)})
				continue
			}
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: url}})
			appended++
		}
		wm.Content = parts
		out = append(out, wm)
	}
	return out
}

// ImageCacheLenForTest exposes the encoded-image cache size (v1.0.6 stress
// seam: proves the cache is bounded and actually hit).
func (c *Client) ImageCacheLenForTest() int {
	c.imgCacheMu.Lock()
	defer c.imgCacheMu.Unlock()
	return len(c.imgCache)
}

// BuildChatRequest converts raw messages + sampling options to a ChatRequest.
// Provider-aware: llama-only knobs (top_k, min_p, n_ctx, repeat_last_n) are
// sent to local llama.cpp but omitted for remote endpoints, which may reject
// them. Presence/frequency penalty are OpenAI-standard and sent to both.
// Reasoning/attachment display fields are stripped from the wire copy;
// v1.0.6: images are projected into OpenAI content parts.
//
// v1.1.4: the previously-ignored sampling settings (minP, repeatLastN,
// presence/frequency penalty) now actually reach the engine — they were
// editable in Settings but silently dropped from every request before.
func (c *Client) BuildChatRequest(model string, messages []Message, tools []ToolSpec) *ChatRequest {
	return c.BuildChatRequestWithOptions(model, messages, tools, 0)
}

// BuildChatRequestWithOptions is BuildChatRequest with an explicit
// context-window override for the wire payload (1.1.6). numCtx > 0
// replaces cfg.LLM.NumCtx in the request body for local endpoints so the
// request carries the SAME effective context the planner validated —
// never a second, larger number that would advertise an unsupported
// window to the engine.
func (c *Client) BuildChatRequestWithOptions(model string, messages []Message, tools []ToolSpec, numCtx int) *ChatRequest {
	cfg := c.src.Load()
	clean := StripReasoning(messages)
	req := &ChatRequest{
		Model:            model,
		Messages:         clean,
		wire:             c.wireMessages(clean),
		Tools:            tools,
		Temperature:      cfg.LLM.Temperature,
		TopP:             cfg.LLM.TopP,
		MaxTokens:        cfg.LLM.MaxTokens,
		Seed:             cfg.LLM.Seed,
		PresencePenalty:  cfg.LLM.PresencePenalty,
		FrequencyPenalty: cfg.LLM.FrequencyPenalty,
	}
	if !cfg.IsRemote() {
		req.TopK = cfg.LLM.TopK
		if numCtx > 0 {
			req.NumCtx = numCtx
		} else {
			req.NumCtx = cfg.LLM.NumCtx
		}
		req.MinP = cfg.LLM.MinP
		req.RepeatLastN = cfg.LLM.RepeatLastN
	}
	if cfg.LLM.Stop != "" {
		req.Stop = strings.Split(cfg.LLM.Stop, ",")
	}
	return req
}
