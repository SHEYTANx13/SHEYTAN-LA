package api

// Attachment API (v1.1.3): real staged file uploads backing the Agent
// conversation. Flow: select → validate → safe staging → inspect → type
// detection → extract → chunk → cache → associate with the session →
// retrieve relevant content at run time.
//
// Endpoints:
//
//	POST   /api/attachments            multipart upload (sessionId, files[])
//	GET    /api/attachments            list staged attachments
//	GET    /api/attachments/{id}       inspect one attachment (metadata + chunks)
//	DELETE /api/attachments/{id}       remove one attachment
//
// Security: multipart bodies are hard-capped by MaxBytesReader; filenames
// are sanitized to display names only; content is content-addressed and
// written without execute bits; nothing uploaded is ever executed.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/attachments"
)

// maxUploadBodyBytes caps the whole multipart body per upload request
// (per-file limits are enforced by the attachments manager itself).
const maxUploadBodyBytes = 160 << 20

// handleAttachments dispatches the attachment endpoints.
func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request) {
	if s.stack == nil || s.stack.Attachments == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("attachment store unavailable"))
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/attachments")
	path = strings.Trim(path, "/")

	switch {
	case path == "" && r.Method == http.MethodPost:
		s.handleAttachmentUpload(w, r)

	case path == "" && r.Method == http.MethodGet:
		s.handleAttachmentList(w, r)

	case path != "" && r.Method == http.MethodGet:
		s.handleAttachmentGet(w, r, path)

	case path != "" && r.Method == http.MethodDelete:
		s.handleAttachmentDelete(w, r, path)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// handleAttachmentUpload stages one or more files.
func (s *Server) handleAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodyBytes)

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("upload too large or malformed: %v", err))
		return
	}

	defer func() {
		_ = r.MultipartForm.RemoveAll()
	}()

	sessionID := r.URL.Query().Get("sessionId")

	form := r.MultipartForm

	if form == nil || len(form.File["files"]) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no files uploaded (field name must be \"files\")"))
		return
	}

	files := form.File["files"]

	limits := s.stack.Attachments.Limits()

	if len(files) > limits.MaxFilesPerAttach {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"too many files: %d uploaded, %d allowed per request",
			len(files), limits.MaxFilesPerAttach,
		))
		return
	}

	staged := make([]*attachments.Attachment, 0, len(files))
	failed := make([]map[string]string, 0)

	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			failed = append(failed, map[string]string{
				"name":  fh.Filename,
				"error": fmt.Sprintf("open failed: %v", err),
			})

			continue
		}

		att, err := s.stack.Attachments.Add(r.Context(), sessionID, fh.Filename, src)
		_ = src.Close()

		if err != nil {
			failed = append(failed, map[string]string{
				"name":  fh.Filename,
				"error": err.Error(),
			})

			continue
		}

		if sessionID != "" {
			s.stack.Attachments.Associate(att.ID, sessionID)
		}

		staged = append(staged, att)
	}

	writeJSON(w, map[string]any{
		"ok":           true,
		"attachments":  staged,
		"failed":       failed,
		"cacheWarning": nil,
	})
}

// handleAttachmentList returns every staged attachment (newest first).
func (s *Server) handleAttachmentList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"attachments": s.stack.Attachments.List(),
		"limits":      s.stack.Attachments.Limits(),
	})
}

// handleAttachmentGet inspects one attachment.
func (s *Server) handleAttachmentGet(w http.ResponseWriter, r *http.Request, id string) {
	att, ok := s.stack.Attachments.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("attachment %s not found", id))
		return
	}

	// Include the staged object path so the UI can show provenance and the
	// agent-side tools can reach the file.
	writeJSON(w, map[string]any{
		"attachment": att,
		"stagedPath": s.stack.Attachments.StagePath(id),
	})
}

// handleAttachmentDelete removes one attachment.
func (s *Server) handleAttachmentDelete(w http.ResponseWriter, r *http.Request, id string) {
	ok := s.stack.Attachments.Delete(id)

	writeJSON(w, map[string]any{
		"ok":      ok,
		"deleted": id,
	})
}

// attachmentIDsForSession returns the attachment ids associated with a
// session context (persisted), filtered to still-existing attachments.
func (s *Server) attachmentIDsForSession(ids []string) []string {
	if s.stack == nil || s.stack.Attachments == nil || len(ids) == 0 {
		return nil
	}

	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if _, ok := s.stack.Attachments.Get(id); ok {
			out = append(out, id)
		}
	}

	return out
}

// marshalAttachmentsSafe is a tiny helper for tests and debug output.
func marshalAttachmentsSafe(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(data)
}
