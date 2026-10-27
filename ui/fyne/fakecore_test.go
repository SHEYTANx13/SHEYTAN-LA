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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

	// wsEnabled flips the /api/presentation/state contract for the
	// Web Workspace view tests.
	wsEnabled atomic.Bool
	// failWSState forces a 503 on /api/presentation/state.
	failWSState bool
	// wsBlocks stores published blocks (typed publish contract).
	wsBlocks []map[string]any

	lastSelectModel string
	configPatched   bool

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
	mux.HandleFunc("/api/models/select", f.handleSelect)
	mux.HandleFunc("/api/studio/state", f.handleStudioState)
	mux.HandleFunc("/api/presentation/state", f.handleWSState)
	mux.HandleFunc("/api/presentation/start", f.handleWSStart)
	mux.HandleFunc("/api/presentation/stop", f.handleWSStop)
	mux.HandleFunc("/api/presentation/blocks", f.handleWSBlocks)
	mux.HandleFunc("/api/presentation/publish", f.handleWSPublish)
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
		"coreVersion": "1.11.0", "api": "v1", "authenticated": true,
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
		writeJSON(w, 200, map[string]any{"phase": "ready"})
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
	f.lastSelectModel = body.Model
	writeJSON(w, 200, map[string]any{"phase": "ready", "model": body.Model, "applied": true})
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

// ---------------------------------------------------------------------------
// v1.11.0 Web Workspace fake surface — mirrors the core contract:
// enabled/stopped by default, typed publish, honest blocks list.
// ---------------------------------------------------------------------------

func (f *fakeCore) handleWSState(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	if f.failWSState {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "core presentation surface unavailable",
		})
		return
	}
	if !f.wsEnabled.Load() {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"running": false,
			"reason":  "the Web Workspace is disabled in Settings (webWorkspaceEnabled)",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"running": false,
		"stats":   map[string]any{"published": len(f.wsBlocks), "live": len(f.wsBlocks), "evicted": 0},
	})
}

func (f *fakeCore) handleWSStart(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	if !f.wsEnabled.Load() {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "the Web Workspace is disabled in Settings",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":  "http://127.0.0.1:9/workspace#t=fake",
		"port": 9,
	})
}

func (f *fakeCore) handleWSStop(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": false})
}

func (f *fakeCore) handleWSBlocks(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": 1,
		"blocks":  f.wsBlocks,
	})
}

func (f *fakeCore) handleWSPublish(w http.ResponseWriter, r *http.Request) {
	if !f.authed(w, r) {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if src, _ := in["source"].(string); strings.TrimSpace(src) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "publish requires a provenance source"})
		return
	}
	stored := map[string]any{
		"id":      fmt.Sprintf("b-%03d", len(f.wsBlocks)+1),
		"version": 1,
		"kind":    in["kind"],
		"payload": in["payload"],
		"source":  in["source"],
	}
	if v, ok := in["title"]; ok {
		stored["title"] = v
	}
	f.wsBlocks = append(f.wsBlocks, stored)
	writeJSON(w, http.StatusOK, map[string]any{"block": stored})
}
