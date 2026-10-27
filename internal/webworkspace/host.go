// Package webworkspace — the v1.11.0 on-demand Web Workspace host.
//
// The Workspace is the SECOND rendering surface of the hybrid UI: a
// read-only, loopback-hosted page that renders typed presentation
// blocks (markdown, code, diffs, tables, chart specs, artifact raw
// data) with browser-grade fidelity. It is NOT a second core: it owns
// no state, executes no commands, and serves ONLY what the core's
// presentation Registry publishes — the exact same read model the
// Fyne shell reads.
//
// Security boundary (mirrors the audited 3D Studio contract):
//   - bind 127.0.0.1:0 ONLY (loopback, ephemeral port; no configuration
//     surface can move the bind);
//   - one 256-bit crypto-random token per launch, carried in the URL
//     FRAGMENT (#t=…) — fragments are never sent to servers and never
//     reach logs; data endpoints require Authorization: Bearer <token>;
//   - strict CSP on every response ('self' only, no remote origins, no
//     inline script, no eval) plus nosniff / no-referrer / DENY framing;
//   - allow-listed embedded assets only — no filesystem joins, no
//     directory listing, unknown paths 404;
//   - GET-only data/static surface (405 otherwise); there is NO write,
//     command, approval or configuration endpoint anywhere in this
//     package — browser-originating actions do not exist by design, so
//     nothing can bypass the core's risk/approval policy;
//   - the page builds all DOM text via createTextNode/textContent (no
//     innerHTML, no document.write), so a hostile payload cannot inject
//     markup even in principle.
package webworkspace

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

// Source is the read-only view over the ONE presentation registry.
type Source interface {
	// Blocks returns up to limit newest blocks (newest first).
	Blocks(ctx context.Context, limit int) ([]presentation.Block, error)
}

// Host serves the Workspace page and its one data endpoint. One host
// per process; Start/Stop are owned by the core API layer (like the
// studio viewer, the listener never outlives the server Close()).
type Host struct {
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
// deadlocks Stop — same channel-token ownership as the studio viewer).
type chanMu struct{ c chan struct{} }

func newChanMu() chanMu {
	c := make(chan struct{}, 1)
	c <- struct{}{}
	return chanMu{c: c}
}
func (m chanMu) lock()   { <-m.c }
func (m chanMu) unlock() { m.c <- struct{}{} }

// NewHost builds a host over the given read-only source.
func NewHost(src Source) *Host {
	return &Host{src: src, mu: newChanMu()}
}

// Start binds loopback on an ephemeral port and serves until Stop.
// The returned URL carries the launch token in the fragment.
func (h *Host) Start() (string, error) {
	h.mu.lock()
	defer h.mu.unlock()
	if h.ln != nil {
		return "", errAlreadyRunning
	}
	if h.src == nil {
		return "", fmt.Errorf("web workspace: no block source")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("web workspace bind: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		_ = ln.Close()
		return "", fmt.Errorf("web workspace token: %w", err)
	}
	h.token = hex.EncodeToString(buf)
	h.ln = ln
	h.started = time.Now()
	h.reqs = 0

	mux := http.NewServeMux()
	mux.HandleFunc("/workspace", h.servePage)
	mux.HandleFunc("/workspace/", h.serveAsset)
	mux.HandleFunc("/api/blocks", h.serveBlocks)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Unknown paths: a named 404, never a listing.
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
	h.srv = srv
	// Serve from the LOCAL reference: Stop may nil h.srv as soon as
	// Start returns.
	go func() { _ = srv.Serve(ln) }()

	h.url = fmt.Sprintf("http://127.0.0.1:%d/workspace#t=%s", ln.Addr().(*net.TCPAddr).Port, h.token)
	return h.url, nil
}

// Stop shuts the host down (bounded) and clears the launch token.
// Stopping a stopped host is a clean no-op.
func (h *Host) Stop() error {
	h.mu.lock()
	defer h.mu.unlock()
	if h.ln == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := h.srv.Shutdown(ctx)
	h.ln = nil
	h.srv = nil
	h.token = ""
	h.url = ""
	return err
}

// State is the honest host lifecycle for the API surface. URL never
// contains the token.
type State struct {
	Running  bool   `json:"running"`
	URL      string `json:"url,omitempty"`
	Port     int    `json:"port,omitempty"`
	Started  string `json:"startedAt,omitempty"`
	Requests int64  `json:"requests"`
}

func (h *Host) State() State {
	h.mu.lock()
	defer h.mu.unlock()
	if h.ln == nil {
		return State{Running: false}
	}
	return State{
		Running:  true,
		URL:      fmt.Sprintf("http://127.0.0.1:%d/workspace", h.ln.Addr().(*net.TCPAddr).Port),
		Port:     h.ln.Addr().(*net.TCPAddr).Port,
		Started:  h.started.UTC().Format(time.RFC3339),
		Requests: h.reqs,
	}
}

func (h *Host) Running() bool {
	h.mu.lock()
	defer h.mu.unlock()
	return h.ln != nil
}

// TokenURL is the launch URL including the per-launch fragment token.
// Empty when stopped. It exists so the API layer can hand the ONE
// launch URL to the requesting local client.
func (h *Host) TokenURL() string {
	h.mu.lock()
	defer h.mu.unlock()
	return h.url
}

func (h *Host) authorized(r *http.Request) bool {
	hdr := r.Header.Get("Authorization")
	if !strings.HasPrefix(hdr, "Bearer ") {
		return false
	}
	got := strings.TrimPrefix(hdr, "Bearer ")
	h.mu.lock()
	defer h.mu.unlock()
	if h.token == "" || len(got) != len(h.token) {
		return false
	}
	var diff byte
	for i := 0; i < len(got); i++ {
		diff |= got[i] ^ h.token[i]
	}
	return diff == 0
}

func securityHeaders(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; "+
			"connect-src 'self'; img-src 'self' data:; font-src 'self'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("X-Frame-Options", "DENY")
}

func (h *Host) count() {
	h.mu.lock()
	h.reqs++
	h.mu.unlock()
}

func (h *Host) servePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	h.count()
	securityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}

// serveAsset answers ONLY the exact allow-listed asset names. The
// lookup is by exact string against embedded bytes — no filesystem
// join, no traversal, no listing.
func (h *Host) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/workspace/")
	var data []byte
	var ctype string
	switch name {
	case "app.js":
		data, ctype = []byte(appJS), "text/javascript; charset=utf-8"
	case "app.css":
		data, ctype = []byte(appCSS), "text/css; charset=utf-8"
	default:
		securityHeaders(w)
		http.NotFound(w, r)
		return
	}
	h.count()
	securityHeaders(w)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// blocksEnvelope is the versioned wire shape of the data endpoint.
type blocksEnvelope struct {
	Version int                  `json:"version"`
	Blocks  []presentation.Block `json:"blocks"`
	Stats   presentation.Stats   `json:"stats"`
}

// maxListBlocks bounds one data response.
const maxListBlocks = 100

func (h *Host) serveBlocks(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !h.authorized(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"the workspace requires the launch token (Authorization: Bearer)"}`))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	blocks, err := h.src.Blocks(ctx, maxListBlocks)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"block source unavailable"}`))
		return
	}
	h.count()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(blocksEnvelope{Version: presentation.EnvelopeVersion, Blocks: blocks})
}
