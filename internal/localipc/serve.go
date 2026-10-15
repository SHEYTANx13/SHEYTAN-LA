package localipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/api"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// Server is the local IPC endpoint owned by the desktop launcher. It wraps
// ONE api.Server — every business request is served by the existing
// handlers through the same mux used by the headless/API mode; no session,
// run, tool, approval or policy behavior is duplicated here.
type Server struct {
	endpoint string
	key      string
	listener net.Listener
	http     *http.Server
	served   chan struct{}
}

// maxHandshakeBody caps the handshake request read (the endpoint is GET
// only; the cap is defense against a broken client sending a body).
const maxHandshakeBody = 1 << 10

// Serve binds the per-user endpoint, verifies transport identity on every
// connection, and serves the existing core handler plus the typed
// handshake. It returns after the listener is bound; call Stop to shut it
// down cleanly.
func Serve(core *api.Server, cfg *config.Config, key string) (*Server, error) {
	endpoint, _, err := Endpoint()
	if err != nil {
		return nil, err
	}

	ln, err := listenEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	s := &Server{
		endpoint: endpoint,
		key:      key,
		listener: ln,
		served:   make(chan struct{}),
	}

	handler := http.NewServeMux()
	handler.HandleFunc("/api/ipc/handshake", s.handleHandshake)
	handler.Handle("/", core.Handler())

	s.http = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		defer close(s.served)
		serveErr := s.http.Serve(ln)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			fmt.Println("localipc: serve:", serveErr)
		}
	}()

	return s, nil
}

// Endpoint reports the bound endpoint string.
func (s *Server) Endpoint() string { return s.endpoint }

// handleHandshake is the typed, versioned first request. The ephemeral key
// is checked against the header; the key itself is never echoed back and
// never logged.
func (s *Server) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeIpcError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.key != "" {
		got := r.Header.Get("X-SHEYTAN-IPC-Key")
		if got == "" {
			writeIpcError(w, http.StatusUnauthorized, "missing handshake key")
			return
		}
		if got != s.key {
			writeIpcError(w, http.StatusForbidden, "handshake key mismatch")
			return
		}
	}

	writeJSONIpc(w, map[string]any{
		"protocol":        ProtocolName,
		"protocolVersion": ProtocolVersion,
		"coreVersion":     config.AppVersion,
		"api":             "v1",
		"authenticated":   true,
	})
}

// Stop performs the clean two-phase shutdown: stop accepting + drain
// in-flight requests, then close the listener. The endpoint file is
// unlinked by the Unix listener (SetUnlinkOnClose) or vanishes with the
// last pipe handle on Windows.
func (s *Server) Stop() {
	if s == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = s.http.Shutdown(ctx)
	_ = s.listener.Close()

	select {
	case <-s.served:
	case <-time.After(6 * time.Second):
		fmt.Println("localipc: bounded wait: serve loop did not exit during shutdown")
	}
}
