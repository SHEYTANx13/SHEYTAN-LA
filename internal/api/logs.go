package api

// logs.go — v1.1.7 in-app Log Viewer + connection diagnostics surface.
//
//   GET /api/logs      — the REDACTED, parsed recent app log (bounded by
//                        the logging ring; never loads unlimited history)
//   GET /api/netcheck  — one bounded connection diagnosis
//
// Both reuse existing infrastructure: the logging.Manager ring (no second
// logging system) and the netcheck package (no new network subsystem).

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
)

// logLinesCap bounds one /api/logs response. The server ring itself is
// capped at 512 entries; this cap protects the response even if the ring
// cap ever changes.
const logLinesCap = 512

// handleLogs serves GET /api/logs?limit=N (default 512).
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	limit := logLinesCap
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < logLinesCap {
			limit = n
		}
	}

	// Parsed + REDACTED view; secrets are redacted inside RecentParsed
	// BEFORE anything leaves the process. The ring is bounded server-side,
	// so memory usage stays capped no matter how the UI polls.
	writeJSON(w, map[string]any{
		"entries": logging.Default().RecentParsed(limit),
		"limit":   limit,
	})
}

// handleNetcheck serves GET /api/netcheck — ONE bounded diagnosis per call
// (no retries, no background probes; local-first, never a gate).
func (s *Server) handleNetcheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	writeJSON(w, netcheck.Diagnose())
}
