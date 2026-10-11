package api

// repoindex.go — v1.3.4 (ROADMAP v1.4 slice 1) repository-intelligence
// API surface.
//
// Three endpoints over the persistent repository index:
//
//      GET  /api/repo/index          — cheap, walk-free index status
//      POST /api/repo/index/refresh  — bounded incremental re-index
//      POST /api/repo/search         — bounded hybrid search
//
// The status endpoint NEVER walks the tree (the UI can poll it freely);
// refresh and search run the bounded incremental update so results are
// always against a fresh-ish index without ever rescanning the whole
// repository per request.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/repoindex"
)

// repoIndexRefreshTimeout bounds the refresh endpoint.
const repoIndexRefreshTimeout = 4 * time.Second

// handleRepoIndex serves GET /api/repo/index.
func (s *Server) handleRepoIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	if s.stack == nil || s.stack.RepoIndex == nil {
		writeJSON(w, repoindex.Status{Root: s.workspaceRoot(), State: "empty"})
		return
	}

	writeJSON(w, s.stack.RepoIndex.Status(s.workspaceRoot()))
}

// handleRepoIndexRefresh serves POST /api/repo/index/refresh: one
// bounded incremental update pass, then the fresh status.
func (s *Server) handleRepoIndexRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	if s.stack == nil || s.stack.RepoIndex == nil {
		writeErr(w, http.StatusServiceUnavailable, errString("repository index is not available"))
		return
	}

	root := s.workspaceRoot()

	ctx, cancel := context.WithTimeout(r.Context(), repoIndexRefreshTimeout)
	defer cancel()

	report, err := s.stack.RepoIndex.Update(ctx, root)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, map[string]any{
		"report": report,
		"status": s.stack.RepoIndex.Status(root),
	})
}

// handleRepoSearch serves POST /api/repo/search with a repoindex.Query
// body, returning the bounded, evidence-carrying result page.
func (s *Server) handleRepoSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	if s.stack == nil || s.stack.RepoIndex == nil {
		writeErr(w, http.StatusServiceUnavailable, errString("repository index is not available"))
		return
	}

	var q repoindex.Query
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, errString("invalid search body: "+err.Error()))
		return
	}

	report, err := s.stack.RepoIndex.Search(r.Context(), s.workspaceRoot(), q)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, report)
}

// workspaceRoot resolves the current effective workspace root.
func (s *Server) workspaceRoot() string {
	return s.src.Load().EffectiveWorkspaceRoot()
}
