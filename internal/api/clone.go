// clone.go — v1.3.0 first-class GitHub clone API.
//
// Endpoints (all under /api/workspace, mirroring the workspace trio):
//
//	POST /api/workspace/clone         start a clone (single slot)
//	GET  /api/workspace/clone/status  live progress / final result
//	POST /api/workspace/clone/cancel  abort the running clone
//
// The HTTP shape follows the v1.2.3 "Download Manager" contract (see
// update.go): POST returns immediately with the first status snapshot;
// the UI polls status at ~1.2 s; cancel aborts (the process tree dies
// with the context — internal/proc). Only ONE clone runs at a time.
//
// After a successful clone the workflow from the release contract runs
// automatically when the user asked for it (openAfter, default true):
//
//	clone verified → workspace switched → summary returned
//
// The switch reuses EXACTLY the validated sequence of
// /api/workspace/switch (tool rebind, project-intel observe, config
// persist) — extracted into switchWorkspaceRoot so both endpoints share
// one implementation. If a run became active while the clone was
// downloading, the switch is skipped with an honest reason and the user
// can switch manually — never a torn state.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/gitclone"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// cloneRequest is the POST /api/workspace/clone body.
type cloneRequest struct {
	URL         string `json:"url"`
	Destination string `json:"destination,omitempty"`
	Branch      string `json:"branch,omitempty"`
	OpenAfter   *bool  `json:"openAfter,omitempty"`
}

// cloneStatusEnvelope is what the status endpoint serves: the job
// snapshot plus the server-side post-clone actions.
type cloneStatusEnvelope struct {
	gitclone.Status

	// Switched is true when the cloned folder became the active
	// workspace after a successful clone.
	Switched bool `json:"switched,omitempty"`
	// SwitchNote explains why a successful clone did NOT switch (e.g. a
	// run was active) — actionable, never silent.
	SwitchNote string `json:"switchNote,omitempty"`
	// Summary is the post-switch workspace summary.
	Summary *workspaceSummary `json:"summary,omitempty"`
}

// startClone validates and launches one clone job. Called from the HTTP
// handler with the user's request.
func (s *Server) startClone(req cloneRequest) (*gitclone.Job, *cloneStatusEnvelope, *apiError) {
	if s.cloneRunning.Load() {
		return nil, nil, &apiError{
			Status:  http.StatusConflict,
			Kind:    "clone-running",
			Message: "a clone is already in progress — cancel it or wait for it to finish",
		}
	}

	// 1. URL validation BEFORE any process spawn.
	target, err := gitclone.ValidateGitHubURL(req.URL)
	if err != nil {
		return nil, nil, &apiError{
			Status:  http.StatusBadRequest,
			Kind:    string(gitclone.ClassifyError(err)),
			Message: err.Error(),
		}
	}

	// 2. Git availability (actionable error when missing).
	if availErr := gitclone.Available(); availErr != nil {
		return nil, nil, &apiError{
			Status:  http.StatusBadRequest,
			Kind:    string(gitclone.ClassifyError(availErr)),
			Message: availErr.Error(),
		}
	}

	cfg := s.src.Load()

	// 3. Destination resolution + validation. Default: the scratch
	// workspace under the canonical root (<root>\workspace\<repo>).
	dest := strings.TrimSpace(req.Destination)
	if dest == "" {
		dest = filepath.Join(cfg.WorkspaceDir(), target.Repo)
	}
	dest = filepath.Clean(dest)
	if !filepath.IsAbs(dest) {
		if abs, absErr := filepath.Abs(dest); absErr == nil {
			dest = filepath.Clean(abs)
		}
	}
	if err := gitclone.ValidateDestination(dest); err != nil {
		return nil, nil, &apiError{
			Status:  http.StatusConflict,
			Kind:    string(gitclone.ClassifyError(err)),
			Message: err.Error(),
		}
	}

	// 4. Refuse while a run is active — cloning into the workspace
	// mid-run would move the floor under the agent (same refusal class
	// as /api/workspace/switch).
	s.runsMu.Lock()
	running := len(s.runs) > 0
	s.runsMu.Unlock()
	if running {
		return nil, nil, &apiError{
			Status:  http.StatusConflict,
			Kind:    "run-active",
			Message: "an agent run is active — wait for it to finish or abort it before cloning",
		}
	}

	openAfter := true
	if req.OpenAfter != nil {
		openAfter = *req.OpenAfter
	}

	// 5. Launch (single slot).
	job := gitclone.Start(target, dest, strings.TrimSpace(req.Branch), gitclone.Options{})

	if !s.cloneRunning.CompareAndSwap(false, true) {
		job.Cancel()
		return nil, nil, &apiError{
			Status:  http.StatusConflict,
			Kind:    "clone-running",
			Message: "a clone is already in progress",
		}
	}

	envelope := &cloneStatusEnvelope{Status: job.Status()}
	s.cloneStatus.Store(envelope)
	s.cloneJob.Store(job)

	logging.Default().Info(
		"workspace",
		"clone started: %s into %s (branch=%q openAfter=%t)",
		target.Name, dest, req.Branch, openAfter,
	)

	// 6. Watcher: fold job snapshots into the envelope and perform the
	// post-success workspace switch.
	go s.watchClone(job, envelope, openAfter)

	return job, envelope, nil
}

// watchClone folds job progress into the served envelope and applies the
// post-clone workspace switch.
func (s *Server) watchClone(job *gitclone.Job, envelope *cloneStatusEnvelope, openAfter bool) {
	defer s.cloneRunning.Store(false)

	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-job.Done():
			s.finalizeClone(job, envelope, openAfter)
			return
		case <-ticker.C:
			next := job.Status()
			s.cloneStatus.Store(&cloneStatusEnvelope{Status: next})
		}
	}
}

// finalizeClone records the terminal state and (when requested and
// possible) switches the workspace to the cloned folder.
func (s *Server) finalizeClone(job *gitclone.Job, envelope *cloneStatusEnvelope, openAfter bool) {
	final := job.Status()

	out := &cloneStatusEnvelope{Status: final}

	switch final.State {
	case gitclone.StateSucceeded:
		logging.Default().Info("workspace", "clone completed: %s (head=%s)", final.RepoName, final.Head)

		// v1.7.0: a succeeded clone IS a genuine git_change event for the
		// automation scheduler (real application-generated event — the
		// subsystem that produced it knows it happened).
		if s.stack != nil {
			s.stack.NotifyGitChange()
		}
		if openAfter {
			// Re-check the run gate at switch time (a run may have
			// started while the download was running).
			s.runsMu.Lock()
			running := len(s.runs) > 0
			s.runsMu.Unlock()
			if running {
				out.SwitchNote = "workspace not switched automatically — an agent run is active; open it from the Workspace tab when the run finishes"
				logging.Default().Warn("workspace", "clone succeeded but the workspace switch was skipped: an agent run is active")
			} else {
				summary, err := s.switchWorkspaceRoot(final.Destination)
				if err != nil {
					out.SwitchNote = "clone succeeded but switching the workspace failed: " + err.Error()
					logging.Default().Warn("workspace", "clone succeeded but the workspace switch failed: %v", err)
				} else {
					out.Switched = true
					out.Summary = summary
				}
			}
		}
	case gitclone.StateCanceled:
		logging.Default().Info("workspace", "clone canceled: %s", final.RepoName)
	case gitclone.StateFailed:
		logging.Default().Warn(
			"workspace",
			"clone failed: %s (kind=%s exit=%d): %s",
			final.RepoName, final.ErrorKind, final.ExitCode, final.ErrorMessage,
		)
	}

	s.cloneStatus.Store(out)
	s.cloneJob.Store(nil)
}

// handleCloneStart is POST /api/workspace/clone.
func (s *Server) handleCloneStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	var req cloneRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	_, envelope, apiErr := s.startClone(req)
	if apiErr != nil {
		// Structured error: kind + actionable message (never raw noise).
		w.WriteHeader(apiErr.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":     apiErr.Message,
			"errorKind": apiErr.Kind,
		})
		return
	}

	writeJSONStatus(w, http.StatusAccepted, envelope)
}

// handleCloneStatus is GET /api/workspace/clone/status.
func (s *Server) handleCloneStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	if envelope := s.cloneStatus.Load(); envelope != nil {
		writeJSONStatus(w, http.StatusOK, envelope)
		return
	}

	writeJSONStatus(w, http.StatusOK, cloneStatusEnvelope{
		Status: gitclone.Status{State: "idle"},
	})
}

// handleCloneCancel is POST /api/workspace/clone/cancel.
func (s *Server) handleCloneCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	job := s.cloneJob.Load()
	if job == nil {
		writeErr(w, http.StatusConflict, errString("no clone is running"))
		return
	}

	job.Cancel()
	writeJSONStatus(w, http.StatusOK, map[string]any{"ok": true, "canceling": true})
}

// switchWorkspaceRoot applies the validated workspace-switch sequence
// (tool rebind → project intel observe → config persist) and returns the
// post-switch summary. Extracted from handleWorkspaceSwitch so the clone
// completion reuses the EXACT same implementation.
func (s *Server) switchWorkspaceRoot(abs string) (*workspaceSummary, error) {
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return nil, errString("not an accessible directory: " + abs)
	}

	// Single atomic rebind for every tool that resolves through BaseDir.
	tools.SetBaseDir(abs)

	// Re-observe the new project (bounded walk, cached card).
	if s.stack != nil && s.stack.Intel != nil {
		if _, err := s.stack.Intel.Observe(abs); err != nil {
			return nil, err
		}
	}

	// Re-index the new project (bounded incremental pass; the first
	// pass over a fresh root walks once within the update budget). No
	// request context is in scope here (the switch is also the clone
	// auto-switch path) — the store's own work budget bounds the pass.
	if s.stack != nil && s.stack.RepoIndex != nil {
		if _, err := s.stack.RepoIndex.Update(context.Background(), abs); err != nil {
			return nil, err
		}
	}

	// Persist: root + recent list. One copy-on-write mutation, one save.
	s.src.Update(func(c *config.Config) {
		c.WorkspaceRoot = abs
		c.PushRecentWorkspace(abs)
	})
	if cfg := s.src.Load(); cfg.ConfigPath() != "" {
		_ = config.Save(cfg.ConfigPath(), cfg)
	}

	summary := s.buildWorkspaceSummary()
	return summary, nil
}

// apiError is a structured API error with HTTP status.
type apiError struct {
	Status  int
	Kind    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

var _ = fmt.Sprintf // retained for future formatted errors in this file
