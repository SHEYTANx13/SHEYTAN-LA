//go:build !headless

// Package fyneui is the ONE product UI: a native Go + Fyne application
// replacing the retired Qt/QML host and the retired React/TypeScript web
// frontend (v1.11.0 one-Go-UI migration).
//
// Authority contract: this package is a CLIENT. Durable state, permissions,
// policy, run lifecycle, tool execution, approvals, model/backend truth and
// persistence remain in the existing Go core (internal/*) exposed through
// the secured local IPC endpoint (internal/localipc). The UI mirrors no
// state that the core does not publish, and every visible action calls a
// real core operation.
//
// Transport: HTTP/1.1 + WebSocket over the per-user local endpoint (Unix
// domain socket / Windows named pipe) with the ephemeral handshake key, the
// exact contract the Qt host consumed (ui/qt/src/coreclient.cpp, retired).
// No Origin header is sent (native client); the key header travels on every
// request for parity with the previous client.
package fyneui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/localipc"
)

// CoreClient is the typed client to the ONE core authority. It is safe for
// concurrent use; individual methods carry their own bounded timeouts.
type CoreClient struct {
	httpc   *http.Client
	wsdial  websocket.Dialer
	base    string // endpoint string for net.Dial
	key     string
	version string
}

// NewCoreClient builds a client for the localipc endpoint. The endpoint is
// dialed directly (no TCP), preserving the transport identity boundary:
// uid peer check on Unix, per-user DACL on Windows.
func NewCoreClient(endpoint, key string) *CoreClient {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", endpoint)
	}
	return &CoreClient{
		httpc: &http.Client{
			Transport: &http.Transport{DialContext: dial},
			// Bounded per-request timeout; streaming reads use the raw
			// response body with their own lifecycle.
			Timeout: 30 * time.Second,
		},
		wsdial: websocket.Dialer{
			NetDialContext:   dial,
			HandshakeTimeout: 10 * time.Second,
		},
		base: endpoint,
		key:  key,
	}
}

// Version reports the core version announced by the handshake.
func (c *CoreClient) Version() string { return c.version }

// apiError is the typed transport failure the UI renders honestly — never
// swallowed, never silently turned into an empty panel.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("core request failed (%d): %s", e.Status, e.Body)
	}
	return fmt.Sprintf("core request failed (%d)", e.Status)
}

// statusErr extracts the typed 4xx body ("error" field) when present.
func statusErr(status int, body []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		return &apiError{Status: status, Body: payload.Error}
	}
	return &apiError{Status: status, Body: strings.TrimSpace(string(body))}
}

func (c *CoreClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://core"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set(localipc.HeaderKey, c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return statusErr(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

// Handshake performs the typed, versioned first request. A protocol or
// version mismatch is a visible refusal, never a fallback.
func (c *CoreClient) Handshake(ctx context.Context) error {
	var hs struct {
		Protocol        string `json:"protocol"`
		ProtocolVersion int    `json:"protocolVersion"`
		CoreVersion     string `json:"coreVersion"`
		Authenticated   bool   `json:"authenticated"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/ipc/handshake", nil, &hs); err != nil {
		return err
	}
	if hs.Protocol != localipc.ProtocolName || hs.ProtocolVersion != localipc.ProtocolVersion {
		return fmt.Errorf("core handshake mismatch: protocol %q v%d (want %q v%d)",
			hs.Protocol, hs.ProtocolVersion, localipc.ProtocolName, localipc.ProtocolVersion)
	}
	if !hs.Authenticated {
		return errors.New("core handshake refused authentication")
	}
	c.version = hs.CoreVersion
	return nil
}

// --- Wire types (mirror of the core's published shapes) ---

// Message mirrors llm.Message.
type Message struct {
	Role        string          `json:"role"`
	Content     string          `json:"content,omitempty"`
	Reasoning   string          `json:"reasoning,omitempty"`
	ToolCalls   json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
}

// Session mirrors sessions.Session.
type Session struct {
	ID        string     `json:"id"`
	Title     string     `json:"title,omitempty"`
	Model     string     `json:"model,omitempty"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	Mode      string     `json:"mode,omitempty"`
	Context   *int       `json:"context,omitempty"`
	Messages  []Message  `json:"messages,omitempty"`
	MsgCount  *int       `json:"msgCount,omitempty"`
}

// Config mirrors the redacted config JSON published by GET /api/config.
// Only user-facing settings the UI reads or patches are declared here; the
// patch path sends nested objects and the core deep-merges.
type Config struct {
	DataDir            string   `json:"dataDir,omitempty"`
	ModelsDir          string   `json:"modelsDir,omitempty"`
	Host               string   `json:"host,omitempty"`
	Port               int      `json:"port,omitempty"`
	LLMBaseURL         string   `json:"llmBaseUrl,omitempty"`
	Model              string   `json:"model,omitempty"`
	Provider           string   `json:"provider,omitempty"`
	RemoteBaseURL      string   `json:"remoteBaseUrl,omitempty"`
	RemoteAPIKey       string   `json:"remoteApiKey,omitempty"`
	RemoteModel        string   `json:"remoteModel,omitempty"`
	LlamaBinPath       string   `json:"llamaBinPath,omitempty"`
	LlamaHost          string   `json:"llamaHost,omitempty"`
	LlamaPort          int      `json:"llamaPort,omitempty"`
	LlamaAutoStart     bool     `json:"llamaAutoStart,omitempty"`
	LlamaExtraArgs     string   `json:"llamaExtraArgs,omitempty"`
	EngineBackend      string   `json:"engineBackend,omitempty"`
	MaxIterations      int      `json:"maxIterations,omitempty"`
	ParallelTools      bool     `json:"parallelTools,omitempty"`
	RunTimeoutMin      int      `json:"runTimeoutMinutes,omitempty"`
	SandboxEnabled     bool     `json:"sandboxEnabled,omitempty"`
	SandboxMemory      string   `json:"sandboxMemory,omitempty"`
	SandboxCPU         int      `json:"sandboxCPU,omitempty"`
	LLM                LLMCfg   `json:"llm,omitempty"`
	BrowserExecPath    string   `json:"browserExecutablePath,omitempty"`
	BrowserHeadless    bool     `json:"browserHeadless,omitempty"`
	BrowserSlowMoMs    int      `json:"browserSlowMoMs,omitempty"`
	UpdateSchedule     string   `json:"updateSchedule,omitempty"`
	ThinkingMode       bool     `json:"thinkingMode,omitempty"`
	EnabledTools       []string `json:"enabledTools,omitempty"`
	AttachBudgetKB     int      `json:"attachmentsBudgetKb,omitempty"`
	HistoryWindowPct   int      `json:"historyWindowPct,omitempty"`
	RecallEnabled      bool     `json:"recallEnabled,omitempty"`
	RecallTopK         int      `json:"recallTopK,omitempty"`
	GPUAutoOffload     bool     `json:"gpuAutoOffload,omitempty"`
	EngineCompat       int      `json:"engineCompat,omitempty"`
	FlashAttention     bool     `json:"flashAttention,omitempty"`
	CacheReuse         int      `json:"cacheReuse,omitempty"`
	UBatchSize         int      `json:"ubatchSize,omitempty"`
	ThreadsBatch       int      `json:"threadsBatch,omitempty"`
	KVCacheQuant       string   `json:"kvCacheQuant,omitempty"`
	Mlock              bool     `json:"mlock,omitempty"`
	DraftModel         string   `json:"draftModel,omitempty"`
	ShowPerfHUD        bool     `json:"showPerfHud,omitempty"`
	VisionEnabled      bool     `json:"visionEnabled,omitempty"`
	VisionMmproj       string   `json:"visionMmproj,omitempty"`
	VisionOffload      string   `json:"visionMmprojOffload,omitempty"`
	RuntimeProfile     string   `json:"runtimeProfile,omitempty"`
	PerformanceMode    string   `json:"performanceMode,omitempty"`
	Accelerator        string   `json:"accelerator,omitempty"`
	WorkspaceRoot      string   `json:"workspaceRoot,omitempty"`
	MaxWorkspaceMB     int      `json:"maxWorkspaceMb,omitempty"`
	MaxSessionsKept    int      `json:"maxSessionsKept,omitempty"`
	MaxLogMB           int      `json:"maxLogMb,omitempty"`
	MultiAgentDepth    int      `json:"multiAgentDepth,omitempty"`
	ContinuumEnabled   bool     `json:"continuumEnabled,omitempty"`
	ContinuumThrPct    int      `json:"continuumThresholdPct,omitempty"`
	ContinuumCarry     int      `json:"continuumCarryMessages,omitempty"`
	ContinuumTokens    int      `json:"continuumFrameworkTokens,omitempty"`
	SmoothStream       bool     `json:"smoothStream,omitempty"`
	TargetFPS          int      `json:"targetFps,omitempty"`
	LabEnabled         bool     `json:"labEnabled,omitempty"`
	LabWorkspaceRoot   string   `json:"labWorkspaceRoot,omitempty"`
	LabCmdTimeoutSec   int      `json:"labCommandTimeoutSec,omitempty"`
	LabMaxIter         int      `json:"labMaxIterations,omitempty"`
	LabKeepWs          bool     `json:"labKeepWorkspaces,omitempty"`
	LabAllowNetwork    bool     `json:"labAllowNetwork,omitempty"`
	ResearchEnabled    bool     `json:"researchEnabled,omitempty"`
	ResearchBackend    string   `json:"researchBackend,omitempty"`
	ResearchSearxURL   string   `json:"researchSearxngUrl,omitempty"`
	ResearchMaxRes     int      `json:"researchMaxResults,omitempty"`
	ResearchTimeoutS   int      `json:"researchTimeoutSec,omitempty"`
	ResearchCacheMin   int      `json:"researchCacheTtlMin,omitempty"`
	ResearchGitHub     bool     `json:"researchGitHub,omitempty"`
	ResearchReddit     bool     `json:"researchReddit,omitempty"`
	ResearchWeb        bool     `json:"researchWeb,omitempty"`
	ResearchUserAgent_ string   `json:"researchUserAgent,omitempty"`
}

// LLMCfg mirrors the nested llm sampling object (deep-merged per field).
type LLMCfg struct {
	Temperature   float64 `json:"temperature,omitempty"`
	TopP          float64 `json:"topP,omitempty"`
	TopK          int     `json:"topK,omitempty"`
	MinP          float64 `json:"minP,omitempty"`
	MaxTokens     int     `json:"maxTokens,omitempty"`
	Stop          string  `json:"stop,omitempty"`
	Seed          int     `json:"seed,omitempty"`
	RepeatPenalty float64 `json:"repeatPenalty,omitempty"`
	RepeatLastN   int     `json:"repeatLastN,omitempty"`
	PresencePen   float64 `json:"presencePenalty,omitempty"`
	FrequencyPen  float64 `json:"frequencyPenalty,omitempty"`
	Mirostat      int     `json:"mirostat,omitempty"`
	MirostatTau   float64 `json:"mirostatTau,omitempty"`
	MirostatEta   float64 `json:"mirostatEta,omitempty"`
	NumCtx        int     `json:"numCtx,omitempty"`
	NumBatch      int     `json:"numBatch,omitempty"`
	NumGPU        int     `json:"numGpu,omitempty"`
	NumThread     int     `json:"numThread,omitempty"`
	Stream        *bool   `json:"stream,omitempty"`
	Preset        string  `json:"preset,omitempty"`
}

// ModelInfo mirrors the api modelInfo shape.
type ModelInfo struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	Path          string `json:"path,omitempty"`
	SizeBytes     int64  `json:"sizeBytes,omitempty"`
	Architecture  string `json:"architecture,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int64  `json:"contextLength,omitempty"`
	ParameterInfo string `json:"parameterInfo,omitempty"`
	EstMemBytes   int64  `json:"estimatedMemoryBytes,omitempty"`
	RecContext    int64  `json:"recommendedContext,omitempty"`
	Multimodal    bool   `json:"multimodal,omitempty"`
	VisionState   string `json:"visionState,omitempty"`
	VisionReason  string `json:"visionReason,omitempty"`
	Serving       bool   `json:"serving,omitempty"`
}

// ModelsPayload mirrors GET /api/models.
type ModelsPayload struct {
	Local        []ModelInfo `json:"local"`
	Loaded       []ModelInfo `json:"loaded"`
	LlamaRunning bool        `json:"llamaRunning"`
	Backend      string      `json:"backend"`
}

// SelectionState mirrors GET/POST /api/models/select.
type SelectionState struct {
	Phase           string          `json:"phase"`
	Model           string          `json:"model,omitempty"`
	PreviousModel   string          `json:"previousModel,omitempty"`
	PerformanceMode string          `json:"performanceMode,omitempty"`
	Applied         bool            `json:"applied,omitempty"`
	Evidence        json.RawMessage `json:"evidence,omitempty"`
	Error           string          `json:"error,omitempty"`
	UpdatedAt       string          `json:"updatedAt,omitempty"`
}

// RunResponse mirrors POST /api/run.
type RunResponse struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	State     string `json:"state"`
}

// PausedRun mirrors GET /api/run/paused entries.
type PausedRun struct {
	RunID          string    `json:"runId"`
	SessionID      string    `json:"sessionId"`
	Revision       int       `json:"revision"`
	UserMessage    string    `json:"userMessage,omitempty"`
	AssistantDraft string    `json:"assistantDraft,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
}

// Goal mirrors the goals store shape.
type Goal struct {
	ID         string          `json:"id,omitempty"`
	Goal       string          `json:"goal,omitempty"`
	Status     string          `json:"status,omitempty"`
	CreatedAt  string          `json:"createdAt,omitempty"`
	Plan       []GoalStep      `json:"plan,omitempty"`
	ApprovalID string          `json:"approvalId,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// GoalStep is one plan step.
type GoalStep struct {
	Title  string `json:"title,omitempty"`
	Status string `json:"status,omitempty"`
}

// Tool mirrors GET /api/tools entries.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Source      string `json:"source,omitempty"`
}

// CustomTool mirrors /api/custom-tools entries.
type CustomTool struct {
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Short       string          `json:"shortDescription,omitempty"`
	Description string          `json:"description,omitempty"`
	Permission  string          `json:"permission,omitempty"`
	Enabled     bool            `json:"enabled,omitempty"`
	Registered  bool            `json:"registered,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// AISystem mirrors /api/systems entries (editable surface subset).
type AISystem struct {
	SystemID       string `json:"systemId,omitempty"`
	Name           string `json:"name,omitempty"`
	Revision       int    `json:"revision,omitempty"`
	Instructions   string `json:"instructions,omitempty"`
	ApprovalPolicy string `json:"approvalPolicy,omitempty"`
	Active         bool   `json:"-"`
}

// WorkspaceSummary mirrors GET /api/workspace.
type WorkspaceSummary struct {
	Root          string          `json:"root,omitempty"`
	IsDefault     bool            `json:"isDefault,omitempty"`
	Exists        bool            `json:"exists,omitempty"`
	ProjectCard   json.RawMessage `json:"projectCard,omitempty"`
	Runtime       json.RawMessage `json:"runtime,omitempty"`
	ActiveSession json.RawMessage `json:"activeSession,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

// LogEntry mirrors /api/logs entries.
type LogEntry struct {
	TS    string `json:"ts,omitempty"`
	Level string `json:"level,omitempty"`
	Sub   string `json:"sub,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

// EngineSnapshot mirrors GET /api/engine (subset the UI renders).
type EngineSnapshot struct {
	State             string          `json:"state,omitempty"`
	Detail            string          `json:"detail,omitempty"`
	Model             string          `json:"model,omitempty"`
	Backend           string          `json:"backend,omitempty"`
	Provider          string          `json:"provider,omitempty"`
	SelectionRequired bool            `json:"selectionRequired,omitempty"`
	Verified          bool            `json:"verified,omitempty"`
	VerifiedModel     string          `json:"verifiedModel,omitempty"`
	Download          json.RawMessage `json:"download,omitempty"`
	Selection         json.RawMessage `json:"selection,omitempty"`
	Raw               json.RawMessage `json:"-"`
}

// --- Typed operations (every visible UI action maps to one) ---

// Sessions lists sessions (BARE JSON ARRAY wire contract, pinned by the
// field-contract test). mode filters: "", "chat" or "agent".
func (c *CoreClient) Sessions(ctx context.Context, mode string) ([]Session, error) {
	path := "/api/sessions"
	if mode != "" {
		path += "?mode=" + url.QueryEscape(mode)
	}
	var out []Session
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateSession creates a session; mode "" uses the core default.
func (c *CoreClient) CreateSession(ctx context.Context, mode string) (Session, error) {
	var body any
	if mode != "" {
		body = map[string]string{"mode": mode}
	}
	var out Session
	err := c.do(ctx, http.MethodPost, "/api/sessions", body, &out)
	return out, err
}

// OpenSession fetches one session (with messages).
func (c *CoreClient) OpenSession(ctx context.Context, id string) (Session, error) {
	var out Session
	err := c.do(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(id), nil, &out)
	return out, err
}

// DeleteSession removes a session.
func (c *CoreClient) DeleteSession(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/sessions/"+url.PathEscape(id), nil, nil)
}

// RenameSession updates the title (PUT /api/sessions/{id}).
func (c *CoreClient) RenameSession(ctx context.Context, id, title string) (Session, error) {
	var out Session
	err := c.do(ctx, http.MethodPut, "/api/sessions/"+url.PathEscape(id), map[string]string{"title": title}, &out)
	return out, err
}

// SearchHistory hits /api/history/search; q + mode ("" both modes).
func (c *CoreClient) SearchHistory(ctx context.Context, q, mode string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 20
	}
	path := fmt.Sprintf("/api/history/search?limit=%d&q=%s", limit, url.QueryEscape(q))
	if mode != "" {
		path += "&mode=" + url.QueryEscape(mode)
	}
	var out []Session
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RunRequest is the POST /api/run body.
type RunRequest struct {
	SessionID  string `json:"sessionId"`
	Message    string `json:"message,omitempty"`
	NetSearch  bool   `json:"netSearch,omitempty"`
	Thinking   string `json:"thinking,omitempty"`
	Regenerate bool   `json:"regenerate,omitempty"`
	ToolMode   string `json:"toolMode,omitempty"`
}

// Run posts a user turn. The response is immediate; content streams over
// the activity WebSocket.
func (c *CoreClient) Run(ctx context.Context, r RunRequest) (RunResponse, error) {
	var out RunResponse
	err := c.do(ctx, http.MethodPost, "/api/run", r, &out)
	return out, err
}

// Abort stops the session's active run.
func (c *CoreClient) Abort(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodPost, "/api/abort", map[string]string{"sessionId": sessionID}, nil)
}

// PauseRun pauses the active run at the next safe boundary.
func (c *CoreClient) PauseRun(ctx context.Context, sessionID, runID string) (revision int, err error) {
	var out struct {
		OK       bool `json:"ok"`
		Revision int  `json:"revision"`
	}
	body := map[string]any{"sessionId": sessionID}
	if runID != "" {
		body["runId"] = runID
	}
	err = c.do(ctx, http.MethodPost, "/api/run/pause", body, &out)
	return out.Revision, err
}

// ResumeRun resumes a paused run (CAS on revision).
func (c *CoreClient) ResumeRun(ctx context.Context, sessionID, runID string, revision int) error {
	return c.do(ctx, http.MethodPost, "/api/run/resume", map[string]any{
		"sessionId": sessionID, "runId": runID, "revision": revision,
	}, nil)
}

// EditRun edits message/draft of a paused run (CAS on revision).
func (c *CoreClient) EditRun(ctx context.Context, sessionID, runID string, revision int, message, draft *string) error {
	body := map[string]any{"sessionId": sessionID, "runId": runID, "revision": revision}
	if message != nil {
		body["message"] = *message
	}
	if draft != nil {
		body["draft"] = *draft
	}
	return c.do(ctx, http.MethodPost, "/api/run/edit", body, nil)
}

// PausedRuns lists paused runs for a session.
func (c *CoreClient) PausedRuns(ctx context.Context, sessionID string) ([]PausedRun, error) {
	var out []PausedRun
	err := c.do(ctx, http.MethodGet, "/api/run/paused?sessionId="+url.QueryEscape(sessionID), nil, &out)
	return out, err
}

// Config GETs the redacted config.
func (c *CoreClient) Config(ctx context.Context) (Config, error) {
	var out Config
	err := c.do(ctx, http.MethodGet, "/api/config", nil, &out)
	return out, err
}

// PatchConfig PUTs a partial patch (nested objects deep-merge in the core)
// and then RE-READS the authoritative redacted view so the UI can never
// drift from the persisted state.
func (c *CoreClient) PatchConfig(ctx context.Context, patch map[string]any) (Config, error) {
	if err := c.do(ctx, http.MethodPut, "/api/config", patch, nil); err != nil {
		return Config{}, err
	}
	return c.Config(ctx)
}

// Models lists local + loaded models.
func (c *CoreClient) Models(ctx context.Context) (ModelsPayload, error) {
	var out ModelsPayload
	err := c.do(ctx, http.MethodGet, "/api/models", nil, &out)
	return out, err
}

// SelectionState fetches the selection state machine.
func (c *CoreClient) Selection(ctx context.Context) (SelectionState, error) {
	var out SelectionState
	err := c.do(ctx, http.MethodGet, "/api/models/select", nil, &out)
	return out, err
}

// SelectModel runs the REAL selection path (POST /api/models/select) —
// never a config patch shortcut; the state machine persists, boots the
// engine and verifies.
func (c *CoreClient) SelectModel(ctx context.Context, model, task string) (SelectionState, error) {
	body := map[string]string{"model": model}
	if task != "" {
		body["task"] = task
	}
	var out SelectionState
	err := c.do(ctx, http.MethodPost, "/api/models/select", body, &out)
	return out, err
}

// ImportModel stages a model from an absolute path.
func (c *CoreClient) ImportModel(ctx context.Context, path string) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, "/api/models/import", map[string]string{"path": path}, &out)
	return out, err
}

// OpenModelsFolder reveals the models directory via the core.
func (c *CoreClient) OpenModelsFolder(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/models/open-folder", nil, nil)
}

// Engine fetches the engine snapshot.
func (c *CoreClient) Engine(ctx context.Context) (EngineSnapshot, error) {
	var out EngineSnapshot
	err := c.do(ctx, http.MethodGet, "/api/engine", nil, &out)
	return out, err
}

// CancelEngineDownload cancels an in-flight engine/model download.
func (c *CoreClient) CancelEngineDownload(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/llama", map[string]string{"action": "cancel-download"}, nil)
}

// SysInfo fetches the fast hardware snapshot (raw for rendering).
func (c *CoreClient) SysInfo(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/sysinfo", nil, &out)
	return out, err
}

// Governor fetches the policy envelope (raw).
func (c *CoreClient) Governor(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/governor", nil, &out)
	return out, err
}

// Maintenance fetches the maintenance phase status (raw).
func (c *CoreClient) Maintenance(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/maintenance", nil, &out)
	return out, err
}

// Systems lists AI systems + the active id.
func (c *CoreClient) Systems(ctx context.Context) ([]AISystem, string, error) {
	var out struct {
		Systems        []AISystem `json:"systems"`
		ActiveSystemID string     `json:"activeSystemId"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/systems", nil, &out); err != nil {
		return nil, "", err
	}
	for i := range out.Systems {
		out.Systems[i].Active = out.Systems[i].SystemID == out.ActiveSystemID
	}
	return out.Systems, out.ActiveSystemID, nil
}

// ActivateSystem activates an AI system.
func (c *CoreClient) ActivateSystem(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/systems/"+url.PathEscape(id)+"/activate", nil, nil)
}

// Goals lists goals.
func (c *CoreClient) Goals(ctx context.Context) ([]Goal, error) {
	var out struct {
		Goals []Goal `json:"goals"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/goals", nil, &out); err != nil {
		return nil, err
	}
	return out.Goals, nil
}

// CreateGoal creates (and optionally starts) a goal.
func (c *CoreClient) CreateGoal(ctx context.Context, text, sessionID string, start bool) (Goal, error) {
	body := map[string]any{"goal": text, "start": start}
	if sessionID != "" {
		body["sessionId"] = sessionID
	}
	var out Goal
	err := c.do(ctx, http.MethodPost, "/api/goals", body, &out)
	return out, err
}

// GoalAction runs start|pause|resume|cancel on a goal.
func (c *CoreClient) GoalAction(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, "/api/goals/"+url.PathEscape(id)+"/"+url.PathEscape(action), nil, nil)
}

// GoalDecision approves/rejects a goal approval.
func (c *CoreClient) GoalDecision(ctx context.Context, id, decision, approvalID string) error {
	return c.do(ctx, http.MethodPost, "/api/goals/"+url.PathEscape(id)+"/"+url.PathEscape(decision),
		map[string]string{"approvalId": approvalID}, nil)
}

// Workspace fetches the workspace summary (raw).
func (c *CoreClient) Workspace(ctx context.Context) (WorkspaceSummary, error) {
	var out WorkspaceSummary
	err := c.do(ctx, http.MethodGet, "/api/workspace", nil, &out)
	return out, err
}

// SwitchWorkspace switches the workspace root.
func (c *CoreClient) SwitchWorkspace(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPost, "/api/workspace/switch", map[string]string{"path": path}, nil)
}

// RevealWorkspace reveals files/terminal in the desktop environment.
func (c *CoreClient) RevealWorkspace(ctx context.Context, target string) error {
	return c.do(ctx, http.MethodPost, "/api/workspace/reveal", map[string]string{"target": target}, nil)
}

// CloneRepository starts a clone; returns the raw status envelope.
func (c *CoreClient) CloneRepository(ctx context.Context, url_, branch string, openAfter bool) (json.RawMessage, error) {
	body := map[string]any{"url": url_, "openAfter": openAfter}
	if branch != "" {
		body["branch"] = branch
	}
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/workspace/clone", body, &out)
	return out, err
}

// CloneStatus polls the clone workflow.
func (c *CoreClient) CloneStatus(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/workspace/clone/status", nil, &out)
	return out, err
}

// CancelClone cancels an in-flight clone.
func (c *CoreClient) CancelClone(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/workspace/clone/cancel", nil, nil)
}

// RepoIndex fetches index status (raw); RefreshRepoIndex re-indexes.
func (c *CoreClient) RepoIndex(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/repo/index", nil, &out)
	return out, err
}

func (c *CoreClient) RefreshRepoIndex(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/repo/index/refresh", nil, &out)
	return out, err
}

// RepoSearch searches the repository index (bounded server-side).
func (c *CoreClient) RepoSearch(ctx context.Context, query map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/repo/search", query, &out)
	return out, err
}

// Lab fetches lab task snapshots (raw).
func (c *CoreClient) Lab(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/lab", nil, &out)
	return out, err
}

// LabRun posts a lab action payload.
func (c *CoreClient) LabRun(ctx context.Context, payload map[string]any) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/lab", payload, &out)
	return out, err
}

// Automation fetches tasks + artifacts (raw envelopes).
func (c *CoreClient) Automation(ctx context.Context) (tasks, artifacts json.RawMessage, err error) {
	if err = c.do(ctx, http.MethodGet, "/api/automation/tasks", nil, &tasks); err != nil {
		return nil, nil, err
	}
	err = c.do(ctx, http.MethodGet, "/api/automation/artifacts", nil, &artifacts)
	return tasks, artifacts, err
}

// AutomationTaskAction runs run|pause|resume|cancel on a task.
func (c *CoreClient) AutomationTaskAction(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, "/api/automation/tasks/"+url.PathEscape(id)+"/"+url.PathEscape(action), nil, nil)
}

// Logs fetches the redacted log ring.
func (c *CoreClient) Logs(ctx context.Context, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	var out struct {
		Entries []LogEntry `json:"entries"`
	}
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/logs?limit=%d", limit), nil, &out)
	return out.Entries, err
}

// Tools fetches the available-tool catalog.
func (c *CoreClient) Tools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	err := c.do(ctx, http.MethodGet, "/api/tools", nil, &out)
	return out, err
}

// CustomTools fetches the custom tool store.
func (c *CoreClient) CustomTools(ctx context.Context) ([]CustomTool, error) {
	var out []CustomTool
	err := c.do(ctx, http.MethodGet, "/api/custom-tools", nil, &out)
	return out, err
}

// Perf fetches the performance payload (raw; the UI renders only real
// measured fields).
func (c *CoreClient) Perf(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/perf", nil, &out)
	return out, err
}

// Recommendation fetches the read-only performance recommendation.
func (c *CoreClient) Recommendation(ctx context.Context, model, task string) (json.RawMessage, error) {
	path := "/api/recommendation?"
	if model != "" {
		path += "model=" + url.QueryEscape(model) + "&"
	}
	path += "task=" + url.QueryEscape(task)
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// NetSearch runs a research query.
func (c *CoreClient) NetSearch(ctx context.Context, query string, maxResults int) (json.RawMessage, error) {
	body := map[string]any{"query": query}
	if maxResults > 0 {
		body["maxResults"] = maxResults
	}
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/net-search", body, &out)
	return out, err
}

// UpdateStatus/Check/Cancel/Download mirror the update surface.
func (c *CoreClient) UpdateStatus(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/api/update/status", nil, &out)
	return out, err
}

func (c *CoreClient) UpdateCheck(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/update/check", nil, &out)
	return out, err
}

func (c *CoreClient) UpdateCancel(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/update/cancel", nil, nil)
}

func (c *CoreClient) UpdateDownload(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/api/update/download", nil, &out)
	return out, err
}

// Attachments uploads files; returns the raw response.
func (c *CoreClient) UploadAttachments(ctx context.Context, sessionID string, files []AttachmentInput) (json.RawMessage, error) {
	pr, pw := io.Pipe()
	w := newMultipartWriter(pw, files, sessionID)
	go func() {
		err := w.Write()
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://core/api/attachments?sessionId="+url.QueryEscape(sessionID), pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set(localipc.HeaderKey, c.key)
	req.Header.Set("Content-Type", w.ContentType())
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, statusErr(resp.StatusCode, raw)
	}
	return raw, nil
}

// AttachmentInput is one staged file.
type AttachmentInput struct {
	Name string
	Data []byte
}

// StudioState reports the honest 3D Studio viewer lifecycle from the ONE
// core authority (enabled / running / URL / port).
type StudioState struct {
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	Reason   string `json:"reason,omitempty"`
	URL      string `json:"url,omitempty"` // WITHOUT the token
	Port     int    `json:"port,omitempty"`
	Started  string `json:"startedAt,omitempty"`
	Requests int64  `json:"requests"`
	Version  string `json:"version,omitempty"`
	License  string `json:"license,omitempty"`
}

// StudioStart launches the core-owned loopback viewer. The returned URL
// includes the per-launch #t= fragment token; the UI must open it (or copy
// it) verbatim and MUST NOT log it.
type StudioStart struct {
	URL     string `json:"url"`
	Port    int    `json:"port"`
	Version string `json:"version"`
}

// StudioState fetches the viewer lifecycle.
func (c *CoreClient) StudioState(ctx context.Context) (StudioState, error) {
	var out StudioState
	err := c.do(ctx, http.MethodGet, "/api/studio/state", nil, &out)
	return out, err
}

// StudioStart starts the viewer.
func (c *CoreClient) StudioStart(ctx context.Context) (StudioStart, error) {
	var out StudioStart
	err := c.do(ctx, http.MethodPost, "/api/studio/start", map[string]any{}, &out)
	return out, err
}

// StudioStop stops the viewer (idempotent at the core).
func (c *CoreClient) StudioStop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/studio/stop", map[string]any{}, nil)
}
