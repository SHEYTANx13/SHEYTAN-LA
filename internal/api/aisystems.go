package api

// aisystems.go — v1.9.0 HTTP surface for the ONE AI System authority.
//
// Routes (registered in Handler()):
//
//      GET    /api/systems              list (systems + activeSystemId)
//      POST   /api/systems              create
//      POST   /api/systems/import       import an exported document
//      GET    /api/systems/active       the active system + frozen snapshot
//      GET    /api/systems/{id}         one system
//      PUT    /api/systems/{id}         update (revision increments)
//      DELETE /api/systems/{id}         delete (active falls back to default)
//      POST   /api/systems/{id}/activate  select/activate
//      POST   /api/systems/{id}/clone     clone
//      GET    /api/systems/{id}/export    export document
//
// Every handler is a thin adapter over internal/aisystem — the store is
// the only authority; nothing here duplicates validation or persistence.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/aisystem"
)

// systemsStore returns the runtime's AI System store (nil-safe).
func (s *Server) systemsStore() *aisystem.Store {
	if s.stack == nil {
		return nil
	}
	return s.stack.Systems
}

// handleAISystems serves the collection routes: /api/systems.
func (s *Server) handleAISystems(w http.ResponseWriter, r *http.Request) {
	store := s.systemsStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("ai systems unavailable"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		systems, err := store.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		activeID := aisystem.ReservedID
		if active, err := store.Active(); err == nil {
			activeID = active.SystemID
		}
		if systems == nil {
			systems = []aisystem.System{}
		}
		writeJSON(w, map[string]any{
			"systems":        systems,
			"activeSystemId": activeID,
		})

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 256*1024)

		var body aisystem.System
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		created, err := store.Create(body)
		if err != nil {
			writeAISystemErr(w, err)
			return
		}
		writeJSON(w, created)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// handleAISystemImport serves POST /api/systems/import.
func (s *Server) handleAISystemImport(w http.ResponseWriter, r *http.Request) {
	store := s.systemsStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("ai systems unavailable"))
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	data, readErr := readAllBody(r)
	if readErr != nil {
		writeErr(w, http.StatusBadRequest, readErr)
		return
	}
	imported, err := store.Import(data)
	if err != nil {
		writeAISystemErr(w, err)
		return
	}
	writeJSON(w, imported)
}

// handleAISystemActive serves GET /api/systems/active.
func (s *Server) handleAISystemActive(w http.ResponseWriter, r *http.Request) {
	store := s.systemsStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("ai systems unavailable"))
		return
	}
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	sys, err := store.Active()
	if err != nil {
		writeAISystemErr(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"system":   sys,
		"snapshot": sys.Snapshot(),
	})
}

// handleAISystemItem serves the per-item routes: {id}, {id}/activate,
// {id}/clone, {id}/export.
func (s *Server) handleAISystemItem(w http.ResponseWriter, r *http.Request) {
	store := s.systemsStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("ai systems unavailable"))
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/systems/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		s.handleAISystems(w, r)
		return
	}

	parts := strings.Split(rest, "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "" {
		switch r.Method {
		case http.MethodGet:
			sys, err := store.Get(id)
			if err != nil {
				writeAISystemErr(w, err)
				return
			}
			writeJSON(w, sys)

		case http.MethodPut:
			r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
			var body aisystem.System
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			updated, err := store.Update(id, func(cur *aisystem.System) error {
				// The request payload REPLACES the editable surface;
				// identity, revision and timestamps stay store-owned.
				cur.Name = body.Name
				cur.Instructions = body.Instructions
				cur.Model = body.Model
				cur.Reasoning = body.Reasoning
				cur.AllowedTools = body.AllowedTools
				cur.ApprovalPolicy = body.ApprovalPolicy
				cur.EnabledSkills = body.EnabledSkills
				cur.KnowledgeRefs = body.KnowledgeRefs
				cur.MemoryPolicy = body.MemoryPolicy
				cur.CompactionPolicy = body.CompactionPolicy
				cur.RuntimeProfile = body.RuntimeProfile
				cur.VerificationPolicy = body.VerificationPolicy
				return nil
			})
			if err != nil {
				writeAISystemErr(w, err)
				return
			}
			writeJSON(w, updated)

		case http.MethodDelete:
			if err := store.Delete(id); err != nil {
				writeAISystemErr(w, err)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "deleted": id})

		default:
			writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		}
		return
	}

	switch action {
	case "activate":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
			return
		}
		sys, err := store.Select(id)
		if err != nil {
			writeAISystemErr(w, err)
			return
		}
		// The NEXT run binds this snapshot; in-flight runs keep theirs.
		writeJSON(w, map[string]any{
			"ok":       true,
			"active":   sys.SystemID,
			"revision": sys.Revision,
			"snapshot": sys.Snapshot(),
		})

	case "clone":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
			return
		}
		clone, err := store.Clone(id)
		if err != nil {
			writeAISystemErr(w, err)
			return
		}
		writeJSON(w, clone)

	case "export":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
			return
		}
		data, err := store.Export(id)
		if err != nil {
			writeAISystemErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"ai-system-%s.json\"", id))
		_, _ = w.Write(data)

	default:
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown ai system action %q", action))
	}
}

// writeAISystemErr maps store errors to honest status codes.
func writeAISystemErr(w http.ResponseWriter, err error) {
	switch {
	case err == aisystem.ErrNotFound:
		writeErr(w, http.StatusNotFound, err)
	case err == aisystem.ErrReserved:
		writeErr(w, http.StatusConflict, err)
	case err == aisystem.ErrTooMany:
		writeErr(w, http.StatusInsufficientStorage, err)
	default:
		if isInvalidAISystemErr(err) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
	}
}

func isInvalidAISystemErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), aisystem.ErrInvalid.Error())
}

// readAllBody reads a bounded, already-MaxBytesReader-wrapped body.
func readAllBody(r *http.Request) ([]byte, error) {
	dec := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
