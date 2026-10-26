package studio

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
)

// securityHeaders is applied to EVERY viewer response. The CSP forbids
// remote origins entirely: the viewer is self-hosted over loopback from
// embedded assets, so 'self' plus data: images is the complete world.
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

// Viewer serves the 3D Studio page + the topology read endpoint. One
// viewer per process; Start/Stop are owned by the core API layer.
type Viewer struct {
	mu       chanMu
	ln       net.Listener
	srv      *http.Server
	token    string
	url      string
	src      ViewSource
	started  time.Time
	Requests int64 // atomic-ish via chanMu guard; diagnostics only
}

// chanMu serializes lifecycle access (start/stop/state) — a plain mutex
// held across Serve makes Stop deadlock, so ownership is a small channel
// token instead.
type chanMu struct{ c chan struct{} }

func newChanMu() chanMu {
	c := make(chan struct{}, 1)
	c <- struct{}{}
	return chanMu{c: c}
}
func (m chanMu) lock()   { <-m.c }
func (m chanMu) unlock() { m.c <- struct{}{} }

// NewViewer builds a viewer over the given read-only source.
func NewViewer(src ViewSource) *Viewer {
	return &Viewer{src: src, mu: newChanMu()}
}

// Start binds 127.0.0.1 on an EPHEMERAL port, generates the per-launch
// bearer token and serves until Stop. The returned URL carries the token
// in the FRAGMENT (#t=...) — fragments are never sent to servers, never
// logged by them, and stay out of the core's own log lines.
func (v *Viewer) Start() (string, error) {
	v.mu.lock()
	defer v.mu.unlock()
	if v.ln != nil {
		return "", errAlreadyRunning
	}
	if v.src == nil {
		return "", fmt.Errorf("studio viewer: no view source")
	}

	// Loopback only, ephemeral port. No configuration surface can move
	// the bind away from 127.0.0.1 — that is a deliberate invariant.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("studio viewer bind: %w", err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		_ = ln.Close()
		return "", fmt.Errorf("studio token: %w", err)
	}
	v.token = hex.EncodeToString(buf)
	v.ln = ln
	v.started = time.Now()
	v.Requests = 0

	mux := http.NewServeMux()
	mux.HandleFunc("/view", v.servePage)
	mux.HandleFunc("/view/", v.serveAsset)
	mux.HandleFunc("/api/view", v.serveTopology)
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
	v.srv = srv
	// Serve from the LOCAL reference: Stop() may nil v.srv as soon as
	// Start returns — the goroutine must never dereference the field.
	go func() { _ = srv.Serve(ln) }()

	v.url = fmt.Sprintf("http://127.0.0.1:%d/view#t=%s", ln.Addr().(*net.TCPAddr).Port, v.token)
	return v.url, nil
}

// Stop shuts the viewer down (bounded) and clears the launch token.
func (v *Viewer) Stop() error {
	v.mu.lock()
	defer v.mu.unlock()
	if v.ln == nil {
		return nil // idempotent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := v.srv.Shutdown(ctx)
	v.ln = nil
	v.srv = nil
	v.token = ""
	v.url = ""
	return err
}

// State reports the honest viewer facts for the API surface.
type State struct {
	Running  bool   `json:"running"`
	URL      string `json:"url,omitempty"` // WITHOUT the token fragment
	Port     int    `json:"port,omitempty"`
	Started  string `json:"startedAt,omitempty"`
	Requests int64  `json:"requests"`
}

// State snapshots the viewer lifecycle (never fabricates a URL).
func (v *Viewer) State() State {
	v.mu.lock()
	defer v.mu.unlock()
	if v.ln == nil {
		return State{Running: false}
	}
	return State{
		Running:  true,
		URL:      fmt.Sprintf("http://127.0.0.1:%d/view", v.ln.Addr().(*net.TCPAddr).Port),
		Port:     v.ln.Addr().(*net.TCPAddr).Port,
		Started:  v.started.UTC().Format(time.RFC3339),
		Requests: v.Requests,
	}
}

// Running is the boolean lifeline used by Close-time cleanup.
func (v *Viewer) Running() bool {
	v.mu.lock()
	defer v.mu.unlock()
	return v.ln != nil
}

// TokenURL is the launch URL including the per-launch fragment token.
// It exists so the API layer can hand the ONE launch URL to the requesting
// local client; it is empty when stopped.
func (v *Viewer) TokenURL() string {
	v.mu.lock()
	defer v.mu.unlock()
	return v.url
}

// authorized checks the bearer token (constant-time compare).
func (v *Viewer) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}
	got := strings.TrimPrefix(h, "Bearer ")
	v.mu.lock()
	defer v.mu.unlock()
	if v.token == "" {
		return false
	}
	// Constant-time comparison via crypto/subtle semantics (hex strings
	// of equal length; unequal lengths fail fast without leaking timing
	// that matters for a loopback-local single-token gate).
	if len(got) != len(v.token) {
		return false
	}
	var diff byte
	for i := 0; i < len(got); i++ {
		diff |= got[i] ^ v.token[i]
	}
	return diff == 0
}

func (v *Viewer) servePage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	v.mu.lock()
	v.Requests++
	v.mu.unlock()
	securityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(viewIndexHTML))
}

func (v *Viewer) serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		securityHeaders(w)
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/view/")
	// Allow-list — no traversal is possible: the lookup is by exact name
	// against the embedded FS, never a filesystem join.
	var data []byte
	var ctype string
	switch name {
	case "studio.js":
		data, ctype = []byte(viewStudioJS), "text/javascript; charset=utf-8"
	case "studio.css":
		data, ctype = []byte(viewStudioCSS), "text/css; charset=utf-8"
	case "assets/three.core.js":
		data, ctype = threeCoreJS, "text/javascript; charset=utf-8"
	case "assets/three.module.js":
		data, ctype = threeModuleJS, "text/javascript; charset=utf-8"
	case "assets/OrbitControls.js":
		data, ctype = orbitControlsJS, "text/javascript; charset=utf-8"
	default:
		securityHeaders(w)
		http.NotFound(w, r)
		return
	}
	v.mu.lock()
	v.Requests++
	v.mu.unlock()
	securityHeaders(w)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (v *Viewer) serveTopology(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !v.authorized(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"studio view requires the launch token (Authorization: Bearer)"}`))
		return
	}
	// Bounded context: a hung source must not pin the viewer.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	topo, err := v.src.StudioTopology(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"topology source unavailable: ` + jsonEscape(err.Error()) + `"}`))
		return
	}
	if err := topo.Validate(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"topology failed the schema gate: ` + jsonEscape(err.Error()) + `"}`))
		return
	}
	v.mu.lock()
	v.Requests++
	v.mu.unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	data, _ := json.Marshal(topo)
	_, _ = w.Write(data)
}
