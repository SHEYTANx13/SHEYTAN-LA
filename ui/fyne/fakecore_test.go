//go:build !headless

// fakecore_test.go — a deterministic in-process fake of the core API for
// UI-contract tests. It speaks the REAL wire contracts (bare session
// array, {systems,activeSystemId} envelope, {error} bodies, config
// deep-merge) over a loopback HTTP server; the CoreClient is pointed at
// it with the endpoint dialer swapped to TCP (test seam: NewCoreClient
// dials unix sockets in production; the fake server url is passed the
// same way through a tiny adapter).
package fyneui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SHEYTANx13/SHEYTAN-LA/internal/config"
)

// contextBackground is a test-local alias (keeps call sites tidy).
func contextBackground() context.Context { return context.Background() }

// fakeCore models the endpoints the contract tests exercise.
type fakeCore struct {
	t        *testing.T
	srv      *httptest.Server
	endpoint string
	key      string

	sessions     []Session
	failSessions bool

	// studioEnabled flips the /api/studio/state contract for the view tests.
	studioEnabled atomic.Bool
	// failStudioState forces a 503 on /api/studio/state (typed-error path).
	failStudioState bool

	lastSelectModel string
	configPatched   bool

	// v1.11.1 models/selection test surface: a controllable selection
	// state machine for the non-destructive model-selection flow.
	selectMux   sync.Mutex
	selectPhase string        // GET-side live phase while a flight is active
	selectBlock chan struct{} // non-nil: POST blocks until closed
	selectFail  string        // non-empty: POST fails with this message
	selectPosts int           // number of POST /api/models/select handled

	// v1.11.1 engine update-check test surface: the controllable official
	// llama.cpp release-check state the Models panel renders.
	engineUpdMux       sync.Mutex
	engineUpdStatus    string         // check status class (default current)
	engineUpdInstalled string         // installed tag (default b12031)
	engineUpdLatest    string         // latest resolved tag (default b12032 when update-available)
	engineUpdAsset     string         // exact asset name override
	engineUpdProgress  map[string]any // live progress envelope (nil = none)
	engineUpdFail      bool           // GET check fails with 500
	engineUpdPosts     int            // number of POST /api/engine/update handled
	engineUpdBlock     chan struct{}  // non-nil: POST blocks until closed

	// v1.11.1 Model Studio test surface: the AI Systems store mirror the
	// binding card edits (PUT /api/systems/<id> round-trips the full
	// editable surface; systemsPuts counts the store writes).
	systemsMux    sync.Mutex
	systems       []AISystem
	systemsActive string
	systemsPuts   int
	systemsFail   bool // GET /api/systems fails with 503 (degradation path)

	cfg map[string]any
}

func newFakeCore(t *testing.T) *fakeCore {
	f := &fakeCore{
		t:   t,
		key: "test-key",
		cfg: map[string]any{
			"model":        "seed-model",
			"llm":          map[string]any{"temperature": 0.7, "numCtx": 16384},
			"enabledTools": []any{"fs_read"},
		},
		sessions: []Session{
			{ID: "seed-1", Title: "Seeded", Mode: ModeChat, MsgCount: intPtr(2), Messages: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			}},
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ipc/handshake", f.handleHandshake)
	mux.HandleFunc("/api/sessions", f.handleSessions)
	mux.HandleFunc("/api/sessions/", f.handleSession)
	mux.HandleFunc("/api/run", f.handleRun)
	mux.HandleFunc("/api/config", f.handleConfig)
	mux.HandleFunc("/api/models", f.handleModels)
	mux.HandleFunc("/api/models/select", f.handleSelect)
	mux.HandleFunc("/api/engine/update/check", f.handleEngineUpdateCheck)
	mux.HandleFunc("/api/engine/update", f.handleEngineUpdate)
	mux.HandleFunc("/api/systems", f.handleSystems)
	mux.HandleFunc("/api/systems/", f.handleSystemItem)
	mux.HandleFunc("/api/studio/state", f.handleStudioState)
	f.srv = httptest.NewServer(mux)
	// Point the unix-socket dialer at the TCP test server (test seam).
	host := strings.TrimPrefix(f.srv.URL, "http://")
	f.endpoint = host
	return f
}

// NewCoreClientForTest builds a client wired to a TCP endpoint (test
// seam; production dials the unix socket path).
func NewCoreClientForTest(endpoint, key string) *CoreClient {
	c := NewCoreClient(endpoint, key)
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", endpoint)
	}
	c.httpc.Transport = &http.Transport{DialContext: dial}
	c.wsdial.NetDialContext = dial
	return c
}

func (f *fakeCore) Close() { f.srv.Close() }

func (f *fakeCore) client() *CoreClient { return NewCoreClientForTest(f.endpoint, f.key) }

func intPtr(i int) *int { return &i }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeCore) authed(w http.ResponseWriter, r *http.Request) bool {
	// The handshake key header travels on every request for parity; the
	// fake enforces it like the real endpoint enforces it on handshake.
	if got := r.Header.Get("X-SHEYTAN-IPC-Key"); got != f.key {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing handshake key"})
		return false
	}
	return true
}

func (f *fakeCore) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	writeJSON(w, 200, map[string]any{
		"protocol": "sheytan-ipc", "protocolVersion": 1,
		// The fake mirrors the REAL core's handshake, which reports
		// internal/config.AppVersion — the fake must track the same
		// authority so the UI's version display can never pass
		// against a stale hand-written literal.
		"coreVersion": config.AppVersion, "api": "v1", "authenticated": true,
	})
}

func (f *fakeCore) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	if f.failSessions {
		writeJSON(w, 503, map[string]string{"error": "sessions store unavailable"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		// BARE ARRAY wire contract (pinned by the field-contract test).
		writeJSON(w, 200, f.sessions)
	case http.MethodPost:
		mode := ModeAgent
		var body struct {
			Mode string `json:"mode"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Mode != "" {
			mode = NormalizeMode(body.Mode)
		}
		s := Session{
			ID:        fmt.Sprintf("s-%d", time.Now().UnixNano()),
			Title:     "",
			Mode:      mode,
			CreatedAt: nowPtr(),
			UpdatedAt: nowPtr(),
			MsgCount:  intPtr(0),
		}
		f.sessions = append(f.sessions, s)
		writeJSON(w, 200, s)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (f *fakeCore) handleSession(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	switch r.Method {
	case http.MethodGet:
		for _, s := range f.sessions {
			if s.ID == id {
				writeJSON(w, 200, s)
				return
			}
		}
		writeJSON(w, 404, map[string]string{"error": "unknown session"})
	case http.MethodPut:
		var body struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for i, s := range f.sessions {
			if s.ID == id {
				f.sessions[i].Title = body.Title
				writeJSON(w, 200, f.sessions[i])
				return
			}
		}
		writeJSON(w, 404, map[string]string{"error": "unknown session"})
	case http.MethodDelete:
		kept := f.sessions[:0]
		for _, s := range f.sessions {
			if s.ID != id {
				kept = append(kept, s)
			}
		}
		f.sessions = kept
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (f *fakeCore) handleRun(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	var body RunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "sessionId and message required"})
		return
	}
	writeJSON(w, 200, RunResponse{OK: true, SessionID: body.SessionID, RunID: "run-1", State: "registered"})
}

func (f *fakeCore) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, f.cfg)
	case http.MethodPut, http.MethodPost:
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad patch"})
			return
		}
		deepMerge(f.cfg, patch)
		f.configPatched = true
		writeJSON(w, 200, f.cfg)
	default:
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (f *fakeCore) handleSelect(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		// GET: the AUTHORITATIVE live selection state. While a POST
		// flight is blocked (test-controlled), the phase the test set is
		// reported; otherwise the settled "ready" state.
		f.selectMux.Lock()
		phase := f.selectPhase
		inFlight := f.selectBlock != nil
		model := f.lastSelectModel
		f.selectMux.Unlock()
		if inFlight && phase == "" {
			phase = "select"
		}
		if phase == "" {
			phase = "ready"
		}
		writeJSON(w, 200, map[string]any{"phase": phase, "model": model})
		return
	}
	var body struct {
		Model string `json:"model"`
		Task  string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		writeJSON(w, 400, map[string]string{"error": "model required"})
		return
	}
	f.selectMux.Lock()
	f.selectPosts++
	fail := f.selectFail
	block := f.selectBlock
	f.selectMux.Unlock()
	if block != nil {
		<-block
	}
	if fail != "" {
		writeJSON(w, 500, map[string]string{"error": fail})
		return
	}
	f.lastSelectModel = body.Model
	writeJSON(w, 200, map[string]any{"phase": "ready", "model": body.Model, "applied": true})
}

// setSelectPhase drives the fake's GET-side live phase (tests).
func (f *fakeCore) setSelectPhase(ph string) {
	f.selectMux.Lock()
	f.selectPhase = ph
	f.selectMux.Unlock()
}

// selectPOSTs reports how many selection POSTs were handled (tests).
func (f *fakeCore) selectPOSTs() int {
	f.selectMux.Lock()
	defer f.selectMux.Unlock()
	return f.selectPosts
}

// setSystemsFail toggles the AI Systems store degradation (typed 503).
func (f *fakeCore) setSystemsFail(fail bool) {
	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()
	f.systemsFail = fail
}

// handleSystems serves the fake AI Systems store (v1.11.1 Model Studio
// test surface): GET returns the mirror + active id; PUT /api/systems/<id>
// replaces the editable surface and echoes the updated document.
func (f *fakeCore) handleSystems(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}

	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()

	switch r.Method {
	case http.MethodGet:
		if f.systemsFail {
			writeJSON(w, 503, map[string]string{"error": "ai systems store unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"systems":        f.systems,
			"activeSystemId": f.systemsActive,
		})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (f *fakeCore) handleSystemItem(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/systems/")
	if id == "" {
		f.handleSystems(w, r)
		return
	}

	f.systemsMux.Lock()
	defer f.systemsMux.Unlock()

	if f.systemsFail {
		// Degradation covers the whole store surface (GET and PUT).
		writeJSON(w, 503, map[string]string{"error": "ai systems store unavailable"})
		return
	}

	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var updated AISystem
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for i := range f.systems {
		if f.systems[i].SystemID == id {
			updated.SystemID = id
			updated.Revision = f.systems[i].Revision + 1
			f.systems[i] = updated
			f.systemsPuts++
			writeJSON(w, 200, updated)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "ai system not found"})
}

// handleModels serves the fake /api/models payload (two local models; the
// second is already serving — enough surface for the selection-flow
// tests: markers, detail, single-flight, phase polling).
func (f *fakeCore) handleModels(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	writeJSON(w, 200, map[string]any{
		"backend": "llama",
		"local": []map[string]any{
			{
				"id": "model-a.gguf", "name": "Model A", "path": "/models/model-a.gguf",
				"sizeBytes": 4294967296, "architecture": "llama", "quantization": "Q4_K_M",
				"contextLength": 32768, "parameterInfo": "8B", "recommendedContext": 8192,
				"visionState": "none", "serving": false,
			},
			{
				"id": "model-b.gguf", "name": "Model B", "serving": true,
			},
		},
	})
}

// handleEngineUpdateCheck serves the fake GET /api/engine/update/check —
// the controllable official-release check state.
func (f *fakeCore) handleEngineUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	f.engineUpdMux.Lock()
	defer f.engineUpdMux.Unlock()
	if f.engineUpdFail {
		writeJSON(w, 500, map[string]string{"error": "release list unreachable"})
		return
	}
	status := f.engineUpdStatus
	if status == "" {
		status = "current"
	}
	installed := f.engineUpdInstalled
	if installed == "" {
		installed = "b12031"
	}
	latest := f.engineUpdLatest
	if latest == "" && status == "update-available" {
		latest = "b12032"
	}
	out := map[string]any{
		"installedTag":     installed,
		"installedVariant": "cpu",
		"assetVariant":     "cpu",
		"platform":         "windows/amd64",
		"source":           "ggml-org/llama.cpp",
		"status":           status,
		"checkedAt":        "2026-11-02T00:00:00Z",
	}
	if latest != "" {
		out["latestTag"] = latest
		asset := f.engineUpdAsset
		if asset == "" {
			asset = "llama-" + latest + "-bin-win-cpu-x64.zip"
		}
		out["assetName"] = asset
	}
	switch status {
	case "update-available":
		out["message"] = "engine update available: " + installed + " to " + latest
	case "current":
		out["message"] = "engine is current"
	}
	if f.engineUpdProgress != nil {
		out["progress"] = f.engineUpdProgress
	}
	writeJSON(w, 200, out)
}

// handleEngineUpdate serves the fake POST /api/engine/update — the
// user-triggered engine update start (blocks while the test holds the
// block channel; the check surface then reports the driven status).
func (f *fakeCore) handleEngineUpdate(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	f.engineUpdMux.Lock()
	f.engineUpdPosts++
	block := f.engineUpdBlock
	f.engineUpdMux.Unlock()
	if block != nil {
		<-block
	}
	writeJSON(w, 200, map[string]any{
		"status":  "installing",
		"message": "engine update transaction started",
	})
}

// setEngineUpdateCheck drives the fake check state (tests).
func (f *fakeCore) setEngineUpdateCheck(status, installed, latest string, progress map[string]any) {
	f.engineUpdMux.Lock()
	f.engineUpdStatus = status
	f.engineUpdInstalled = installed
	f.engineUpdLatest = latest
	f.engineUpdProgress = progress
	f.engineUpdMux.Unlock()
}

// engineUpdatePOSTs reports how many engine-update POSTs were handled.
func (f *fakeCore) engineUpdatePOSTs() int {
	f.engineUpdMux.Lock()
	defer f.engineUpdMux.Unlock()
	return f.engineUpdPosts
}

// deepMerge mirrors the core's patch semantics for the fields the fake
// publishes (maps merge recursively, everything else replaces).
func deepMerge(dst, patch map[string]any) {
	for k, v := range patch {
		if mv, ok := v.(map[string]any); ok {
			if dmv, ok := dst[k].(map[string]any); ok {
				deepMerge(dmv, mv)
				continue
			}
		}
		dst[k] = v
	}
}

func nowPtr() *time.Time {
	t := time.Now()
	return &t
}

// handleStudioState serves the studio lifecycle contract: enabled and
// stopped by default (the viewer itself is core-owned and NOT started in
// the fake — the view must render that state honestly). Tests flip
// studioEnabled via the atomic flag.
func (f *fakeCore) handleStudioState(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	if f.failStudioState {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "core studio surface unavailable",
		})
		return
	}
	if !f.studioEnabled.Load() {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"running": false,
			"reason":  "the 3D Studio is disabled in Settings (studioEnabled)",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"running": false,
		"version": "0.182.0",
		"license": "MIT",
	})
}
