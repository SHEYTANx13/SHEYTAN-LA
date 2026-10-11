package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/lab"
)

type labActionResponse struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// handleLab exposes the read-only Lab task list and the controlled Lab action
// interface.
//
// GET /api/lab
//
//	returns every currently active Lab task.
//
// POST /api/lab
//
//	delegates one lifecycle action to the existing Coding Lab tool.
//
// The handler never implements Lab semantics itself. All execution remains
// inside internal/lab.
func (s *Server) handleLab(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.stack == nil || s.stack.Lab == nil {
		writeErr(
			w,
			http.StatusServiceUnavailable,
			errors.New("coding lab is unavailable"),
		)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"tasks": s.stack.Lab.Snapshots(),
		})
		return

	case http.MethodPost:
		var raw json.RawMessage

		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if len(raw) == 0 ||
			strings.TrimSpace(string(raw)) == "" ||
			strings.TrimSpace(string(raw)) == "null" {
			writeErr(
				w,
				http.StatusBadRequest,
				errors.New("Lab request must be a JSON object"),
			)
			return
		}

		var request struct {
			Action string `json:"action"`
		}

		if err := json.Unmarshal(raw, &request); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		if strings.TrimSpace(request.Action) == "" {
			writeErr(
				w,
				http.StatusBadRequest,
				errors.New("Lab action is required"),
			)
			return
		}

		output, err := s.stack.Lab.Run(
			r.Context(),
			raw,
		)

		var decoded any

		if strings.TrimSpace(output) != "" {
			if decodeErr := json.Unmarshal(
				[]byte(output),
				&decoded,
			); decodeErr != nil {
				decoded = output
			}
		}

		response := labActionResponse{
			OK:     err == nil,
			Result: decoded,
		}

		if err != nil {
			response.Error = err.Error()
		}

		if err != nil {
			status := http.StatusBadRequest

			switch {
			case errors.Is(err, lab.ErrInvalidSource),
				errors.Is(err, lab.ErrInvalidWorkspace),
				errors.Is(err, lab.ErrTaskInvalid),
				errors.Is(err, lab.ErrTaskNotRunnable),
				errors.Is(err, lab.ErrTaskNotVerified),
				errors.Is(err, lab.ErrTaskVerificationStale):
				status = http.StatusBadRequest
			}

			writeJSONStatus(w, status, response)
			return
		}

		writeJSON(w, response)
		return

	default:
		writeErr(
			w,
			http.StatusMethodNotAllowed,
			fmt.Errorf("method %s not allowed", r.Method),
		)
	}
}

// handleLabTask returns one active task snapshot.
//
// GET /api/lab/:taskId
func (s *Server) handleLabTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(
			w,
			http.StatusMethodNotAllowed,
			fmt.Errorf("method %s not allowed", r.Method),
		)
		return
	}

	if s == nil || s.stack == nil || s.stack.Lab == nil {
		writeErr(
			w,
			http.StatusServiceUnavailable,
			errors.New("coding lab is unavailable"),
		)
		return
	}

	taskID := strings.TrimPrefix(
		r.URL.Path,
		"/api/lab/",
	)

	taskID = strings.TrimSpace(taskID)

	if taskID == "" {
		writeErr(
			w,
			http.StatusBadRequest,
			lab.ErrTaskSnapshotNotFound,
		)
		return
	}

	snapshot, err := s.stack.Lab.Snapshot(taskID)
	if err != nil {
		status := http.StatusNotFound

		if errors.Is(err, lab.ErrTaskSnapshotNotFound) ||
			errors.Is(err, lab.ErrSessionNotFound) {
			status = http.StatusNotFound
		}

		writeErr(w, status, err)
		return
	}

	writeJSON(w, snapshot)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
