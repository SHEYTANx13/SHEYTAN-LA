// presentation.go — the v1.11.0 Web Workspace control + publish surface.
//
// The Web Workspace is the SECOND rendering surface of the hybrid UI
// (Fyne remains the native shell). This file owns its ONE control plane
// on the core server:
//
//	GET  /api/presentation/state   — honest host lifecycle (no token)
//	POST /api/presentation/start   — bind the loopback host, return the ONE
//	                                 launch URL (token in the fragment; the
//	                                 token never reaches any log)
//	POST /api/presentation/stop    — stop the host (idempotent)
//	GET  /api/presentation/blocks  — the block list from the ONE registry
//	POST /api/presentation/publish — publish typed blocks (bounded, typed)
//
// The registry (internal/presentation.Registry) is the single block
// store: the loopback page, the IPC list endpoint and the Fyne shell all
// read the SAME authority — there is no second copy of published
// content. The workspace host is stopped at server Close() — the
// listener never outlives the core.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/presentation"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/webworkspace"
)

// publishBody is the typed publish request. Provenance is REQUIRED:
// an unnamed producer is refused (fail closed) — the model cannot
// publish anonymously, and neither can a tool.
type publishBody struct {
	Kind      string            `json:"kind"`
	Title     string            `json:"title,omitempty"`
	Payload   string            `json:"payload,omitempty"`
	Language  string            `json:"language,omitempty"`
	RunID     string            `json:"runId,omitempty"`
	SessionID string            `json:"sessionId,omitempty"`
	Source    string            `json:"source"`
	Meta      map[string]string `json:"meta,omitempty"`
	// StableID optionally pins the block identity (deduplicated by the
	// registry — a duplicate is a typed error, never a remap).
	StableID string `json:"stableId,omitempty"`
}

// Blocks implements webworkspace.Source over the ONE registry.
func (s *Server) Blocks(ctx context.Context, limit int) ([]presentation.Block, error) {
	return s.presentReg.List(limit), nil
}

// handlePresentationState reports the honest host lifecycle.
func (s *Server) handlePresentationState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if !s.src.Load().WebWorkspaceEnabled {
		writeJSON(w, map[string]any{
			"enabled": false,
			"running": false,
			"reason":  "the Web Workspace is disabled in Settings (webWorkspaceEnabled)",
		})
		return
	}
	if s.workspaceHost == nil {
		writeJSON(w, map[string]any{"enabled": true, "running": false})
		return
	}
	st := s.workspaceHost.State()
	writeJSON(w, map[string]any{
		"enabled":   true,
		"running":   st.Running,
		"url":       st.URL,
		"port":      st.Port,
		"startedAt": st.Started,
		"requests":  st.Requests,
		"stats":     s.presentReg.Stats(),
	})
}

// handlePresentationStart launches the loopback host and returns the
// ONE tokened launch URL. The URL travels in THIS response body only —
// never into any log line.
func (s *Server) handlePresentationStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if !s.src.Load().WebWorkspaceEnabled {
		writeErr(w, http.StatusForbidden, errors.New(
			"the Web Workspace is disabled in Settings — enable it under Settings → General (webWorkspaceEnabled)"))
		return
	}
	if s.workspaceHost == nil {
		s.workspaceHost = webworkspace.NewHost(s)
	}
	url, err := s.workspaceHost.Start()
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	st := s.workspaceHost.State()
	logging.Default().Info("workspace", "Web Workspace host started on %s (port %d)", st.URL, st.Port)
	writeJSON(w, map[string]any{
		"url":  url, // includes #t= fragment — the ONLY place the token travels
		"port": st.Port,
	})
}

// handlePresentationStop shuts the host down. Stopping a stopped host
// is a clean no-op (honest idempotence).
func (s *Server) handlePresentationStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	if s.workspaceHost == nil {
		writeJSON(w, map[string]any{"running": false})
		return
	}
	if err := s.workspaceHost.Stop(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	logging.Default().Info("workspace", "Web Workspace host stopped")
	writeJSON(w, map[string]any{"running": false})
}

// handlePresentationBlocks serves the block list (the same read the
// loopback page performs — one DTO, one registry).
func (s *Server) handlePresentationBlocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	blocks := s.presentReg.List(maxPublishBlocks)
	writeJSON(w, map[string]any{
		"version": presentation.EnvelopeVersion,
		"blocks":  blocks,
		"stats":   s.presentReg.Stats(),
	})
}

// maxPublishBlocks bounds the IPC list response.
const maxPublishBlocks = 100

// maxPublishBody bounds the publish request body (slightly above the
// per-block payload bound so a single-block publish always fits).
const maxPublishBody = presentation.MaxPayloadBytes + (16 << 10)

// handlePresentationPublish publishes typed blocks into the ONE
// registry. Deterministic degradation applies: unknown kinds arrive as
// raw-data artifact blocks, oversized payloads fail closed, and every
// stored block requires provenance. The response carries the stored
// blocks with their STABLE IDs — the identity both surfaces share.
func (s *Server) handlePresentationPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}
	var body publishBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPublishBody)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Source) == "" {
		writeErr(w, http.StatusBadRequest, errString(
			"publish requires a provenance source (the producer identity)"))
		return
	}
	blk := presentation.Block{
		Kind:      body.Kind,
		Title:     body.Title,
		Payload:   body.Payload,
		Language:  body.Language,
		RunID:     body.RunID,
		SessionID: body.SessionID,
		Source:    body.Source,
		Meta:      body.Meta,
	}
	var (
		stored presentation.Block
		err    error
	)
	if body.StableID != "" {
		stored, err = s.presentReg.PublishWithID(blk, body.StableID)
	} else {
		stored, err = s.presentReg.Publish(blk)
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, map[string]any{
		"block":        stored,
		"renderer":     presentation.Select(stored.Kind),
		"degraded":     stored.Meta["degraded"] != "",
		"originalKind": stored.Meta["originalKind"],
	})
}

// stopWorkspaceHost is the Close()-time cleanup: the loopback listener
// never outlives the core (same contract as the 3D Studio viewer).
func (s *Server) stopWorkspaceHost() {
	if s.workspaceHost != nil {
		if err := s.workspaceHost.Stop(); err != nil {
			logging.Default().Warn("workspace", "host stop during close: %v", err)
		}
	}
}

// PublishBlocks is the IN-PROCESS convenience used by the desktop
// client layer and tests: deterministic extraction of typed blocks
// from a finished answer, then registry publish. Returned blocks carry
// the stable IDs both renderers share.
func (s *Server) PublishBlocks(answer, runID, sessionID, source string) ([]presentation.Block, error) {
	extracted := presentation.ExtractBlocks(answer, runID, sessionID, source)
	out := make([]presentation.Block, 0, len(extracted))
	for _, b := range extracted {
		stored, err := s.presentReg.Publish(b)
		if err != nil {
			return out, err
		}
		out = append(out, stored)
	}
	return out, nil
}
