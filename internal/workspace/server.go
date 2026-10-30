// Package workspace is the on-demand Web Workspace: the browser-hosted
// rich-content surface of the v1.11.0 hybrid shell. It renders blocks
// issued through internal/presentation by the Go runtime.
//
// It is NOT a second core, agent, session store, tool registry or
// permission system: it is a read-only viewer over one Go-owned block
// source, exactly like the 3D Studio is a read-only viewer over the
// topology source. Security boundary (same family as internal/studio):
//
//   - Bind 127.0.0.1 on an EPHEMERAL port only; no configuration can
//     move the bind away from loopback.
//   - Per-launch 256-bit random bearer token; the launch URL carries it
//     in the FRAGMENT (#t=…), which browsers never send to servers — the
//     token cannot land in server logs or in the page HTML.
//   - Strict CSP on every response (no remote origins, no inline script,
//     no framing); nosniff; no-referrer; DENY framing.
//   - Allow-listed embedded assets only — asset lookup is by exact name
//     against the embedded FS, never a filesystem join.
//   - Zero command/write endpoints: GET-only routes; there is no action
//     surface for browser JS in this slice. Any future action route must
//     be typed, Go-authorized and security-reviewed before it exists.
//   - The page builds all DOM text via textContent (never innerHTML), so
//     model-generated payload bytes can never inject markup.
package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/presentation"
)

// securityHeaders is applied to EVERY workspace response.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; "+
			"connect-src 'self'; img-src 'self' data:; font-src 'self'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
}

// Source is the read-only block source the server serves. The Go runtime
// owns the implementing store; the workspace can only read snapshots.
type Source interface {
	// WorkspaceBlocks returns the bounded metadata snapshot.
	WorkspaceBlocks() []presentation.Meta
	// WorkspaceBlock returns one canonical block by ID.
	WorkspaceBlock(id string) (presentation.Block, bool)
}

// Server serves the Workspace page + the two read-only block endpoints.
// One server per process; Start/Stop are owned by the UI layer.
type Server struct {
	mu      chanMu
	ln      net.Listener
	srv     *http.Server
	token   string
	url     string
	src     Source
	started time.Time
	reqs    int64
}

// chanMu serializes lifecycle access (a plain mutex held across Serve
// deadlocks Stop — the channel token is the studio-proven ownership).
type chanMu struct{ c chan struct{} }

func newChanMu() chanMu {
	c := make(chan struct{}, 1)
	c <- struct{}{}
	return chanMu{c: c}
}
func (m chanMu) lock()   { <-m.c }
func (m chanMu) unlock() { m.c <- struct{}{} }

// NewServer builds a workspace server over the given read-only source.
func NewServer(src Source) *Server {
	return &Server{src: src, mu: newChanMu()}
}

// Start binds 127.0.0.1 on an EPHEMERAL port, generates the per-launch
// bearer token and serves until Stop. The returned URL carries the token
// in the FRAGMENT (#t=...) — never sent to servers, never logged.
func (s *Server) Start() (string, error) {
	s.mu.lock()
	defer s.mu.unlock()
	if s.ln != nil {
		return "", errAlreadyRunning
	}
	if s.src == nil {
		return "", fmt.Errorf("workspace: no block source")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("workspace bind: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		_ = ln.Close()
		return "", fmt.Errorf("workspace token: %w", err)
	}
	s.token = hex.EncodeToString(buf)
	s.ln = ln
	s.started = time.Now()
	s.reqs = 0

	mux := http.NewServeMux()
	mux.HandleFunc("/workspace", s.servePage)
	mux.HandleFunc("/workspace.js", s.serveJS)
	mux.HandleFunc("/workspace.css", s.serveCSS)
	mux.HandleFunc("/api/workspace/blocks", s.serveBlocks)
	mux.HandleFunc("/api/workspace/block", s.serveBlock)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Unknown paths: a named 404, never a directory listing.
		securityHeaders(w)
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	s.srv = srv
	// Serve from the LOCAL reference: Stop may nil s.srv as soon as Start
	// returns — the goroutine must never dereference the field.
	go func() { _ = srv.Serve(ln) }()

	s.url = fmt.Sprintf("http://127.0.0.1:%d/workspace#t=%s", ln.Addr().(*net.TCPAddr).Port, s.token)
	return s.url, nil
}

// Stop shuts the server down (bounded) and clears the launch token.
func (s *Server) Stop() error {
	s.mu.lock()
	defer s.mu.unlock()
	if s.ln == nil {
		return nil // idempotent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	s.ln = nil
	s.srv = nil
	s.token = ""
	s.url = ""
	return err
}

// State is the honest lifecycle snapshot for the API/UI surfaces.
type State struct {
	Running  bool   `json:"running"`
	URL      string `json:"url,omitempty"` // WITHOUT the token fragment
	Port     int    `json:"port,omitempty"`
	Started  string `json:"startedAt,omitempty"`
	Requests int64  `json:"requests"`
}

// State reports the lifecycle (never fabricates a URL).
func (s *Server) State() State {
	s.mu.lock()
	defer s.mu.unlock()
	if s.ln == nil {
		return State{Running: false}
	}
	return State{
		Running:  true,
		URL:      fmt.Sprintf("http://127.0.0.1:%d/workspace", s.ln.Addr().(*net.TCPAddr).Port),
		Port:     s.ln.Addr().(*net.TCPAddr).Port,
		Started:  s.started.UTC().Format(time.RFC3339),
		Requests: s.reqs,
	}
}

// Running is the boolean lifeline used by close-time cleanup.
func (s *Server) Running() bool {
	s.mu.lock()
	defer s.mu.unlock()
	return s.ln != nil
}

// TokenURL is the launch URL including the per-launch fragment token.
// Empty when stopped. Only the local UI layer receives it; it is never
// logged and never stored.
func (s *Server) TokenURL() string {
	s.mu.lock()
	defer s.mu.unlock()
	return s.url
}

// authorized checks the bearer token (constant-time compare, same
// discipline as the studio viewer).
func (s *Server) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	got := strings.TrimPrefix(h, "Bearer ")
	s.mu.lock()
	defer s.mu.unlock()
	if s.token == "" {
		return false
	}
	if len(got) != len(s.token) {
		return false
	}
	var diff byte
	for i := 0; i < len(got); i++ {
		diff |= got[i] ^ s.token[i]
	}
	return diff == 0
}

func (s *Server) countReq() {
	s.mu.lock()
	s.reqs++
	s.mu.unlock()
}

func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.countReq()
	securityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}

func (s *Server) serveJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.countReq()
	securityHeaders(w)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(pageJS))
}

func (s *Server) serveCSS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.countReq()
	securityHeaders(w)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(pageCSS))
}

func (s *Server) serveBlocks(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		s.writeJSONError(w, http.StatusUnauthorized,
			"workspace requires the launch token (Authorization: Bearer)")
		return
	}
	s.countReq()
	metas := s.src.WorkspaceBlocks()
	if metas == nil {
		metas = []presentation.Meta{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"v": presentation.Version, "blocks": metas})
}

func (s *Server) serveBlock(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		s.writeJSONError(w, http.StatusUnauthorized,
			"workspace requires the launch token (Authorization: Bearer)")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > presentation.MaxIDLen || strings.ContainsAny(id, "/\\") {
		s.writeJSONError(w, http.StatusBadRequest, "invalid block id")
		return
	}
	b, ok := s.src.WorkspaceBlock(id)
	if !ok {
		s.writeJSONError(w, http.StatusNotFound, "no such block")
		return
	}
	s.countReq()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	_ = enc.Encode(b)
}

func (s *Server) writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
