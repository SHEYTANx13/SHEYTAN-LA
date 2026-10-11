// Custom tools API (v1.6.0, spec §8/§9/§18).
//
// Endpoints (all validated, bounded, deterministic in shape):
//
//	GET    /api/custom-tools            — list definitions
//	POST   /api/custom-tools            — create (validated, atomic save, registered when enabled)
//	GET    /api/custom-tools/{id}       — one definition
//	PUT    /api/custom-tools/{id}       — update (re-validate, atomic, re-register)
//	DELETE /api/custom-tools/{id}       — delete + unregister
//	POST   /api/custom-tools/{id}/test  — execute with caller-supplied sample args
//
// The registry integration goes through the EXISTING agent tool registry
// (orch.Register/Unregister) — custom tools appear in /api/tools and in
// the dynamic toolset selection exactly like built-ins, with
// source="custom" metadata (spec §17).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/customtools"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
)

// customToolView is the deterministic API shape for one definition.
type customToolView struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	ShortDesc   string                 `json:"shortDescription"`
	Description string                 `json:"description"`
	Params      []customtools.Param    `json:"inputParameters"`
	ExecType    string                 `json:"executionType"`
	HTTP        *customtools.HTTPExec  `json:"http,omitempty"`
	Command     *customtools.CommandExec `json:"command,omitempty"`
	Permission  string                 `json:"permission"`
	TimeoutSec  int                    `json:"timeoutSeconds"`
	OutputLimit int                    `json:"outputLimitBytes"`
	Enabled     bool                   `json:"enabled"`
	Registered  bool                   `json:"registered"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

func viewOf(d *customtools.Definition, registered bool) customToolView {
	return customToolView{
		ID:          d.ID,
		Name:        d.Name,
		ShortDesc:   d.ShortDesc,
		Description: d.Description,
		Params:      d.Params,
		ExecType:    d.ExecType,
		HTTP:        d.HTTP,
		Command:     d.Command,
		Permission:  d.Permission,
		TimeoutSec:  d.TimeoutSec,
		OutputLimit: d.OutputLimit,
		Enabled:     d.Enabled,
		Registered:  registered,
		CreatedAt:   d.CreatedAt,
		UpdatedAt:   d.UpdatedAt,
	}
}

// registerCustomTools syncs the ENABLED definitions into the ONE tool
// registry. A disabled tool is unregistered (it cannot be offered to
// the model); an enabled one is (re)registered as a first-class Tool.
func (s *Server) registerCustomTools() {
	if s.ctStore == nil || s.orch == nil {
		return
	}

	enabled := map[string]bool{}

	for _, d := range s.ctStore.List() {
		if !d.Enabled {
			continue
		}

		if vr := d.Validate(); !vr.OK {
			// Never register an invalid definition (a hand-edited file).
			logging.Default().Warn("custom-tools",
				"skipping invalid tool definition %q: %s (%s)", d.Name, vr.Error, vr.Owner)
			continue
		}

		enabled[d.Name] = true
		s.orch.Register(customtools.NewTool(d))
	}

	// Unregister tools whose definition vanished or became disabled.
	for name := range s.orch.Tools() {
		if !enabled[name] {
			// Only unregister OUR custom tools — never a built-in.
			if s.isCustomToolName(name) {
				s.orch.Unregister(name)
			}
		}
	}
}

// isCustomToolName reports whether a registry entry is one of OUR
// custom tools (by matching against the store's definitions).
func (s *Server) isCustomToolName(name string) bool {
	if s.ctStore == nil {
		return false
	}

	for _, d := range s.ctStore.List() {
		if d.Name == name {
			return true
		}
	}
	return false
}

// handleCustomTools serves the collection routes.
func (s *Server) handleCustomTools(w http.ResponseWriter, r *http.Request) {
	if s.ctStore == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("custom tools unavailable"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		registered := s.registeredCustomToolNames()

		out := make([]customToolView, 0)
		for _, d := range s.ctStore.List() {
			out = append(out, viewOf(d, registered[d.Name]))
		}

		writeJSON(w, out)

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, customtools.MaxDefinitionBytes)

		var def customtools.Definition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		// Creation is ALWAYS disabled-by-default (spec §8 security):
		// strip an incoming Enabled flag — enabling is a separate,
		// explicit action.
		def.Enabled = false

		if def.ID != "" {
			if _, exists := s.ctStore.Get(def.ID); exists {
				writeErr(w, http.StatusConflict, fmt.Errorf("tool %q already exists", def.ID))
				return
			}
		}

		if err := s.ctStore.Save(&def); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		s.registerCustomTools()

		logging.Default().Info("custom-tools", "tool %q created (disabled by default)", def.Name)

		registered := s.registeredCustomToolNames()
		view := viewOf(&def, registered[def.Name])
		writeJSON(w, view)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// handleCustomTool serves the per-item routes: {id}, {id}/test.
func (s *Server) handleCustomTool(w http.ResponseWriter, r *http.Request) {
	if s.ctStore == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("custom tools unavailable"))
		return
	}

	// Path shape after the prefix: "{id}" or "{id}/test".
	rest := strings.TrimPrefix(r.URL.Path, "/api/custom-tools/")
	rest = strings.Trim(rest, "/")

	if rest == "" {
		s.handleCustomTools(w, r)
		return
	}

	parts := strings.Split(rest, "/")
	id := parts[0]

	def, ok := s.ctStore.Get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("custom tool %q not found", id))
		return
	}

	if len(parts) == 2 && parts[1] == "test" {
		s.handleCustomToolTest(w, r, def)
		return
	}

	if len(parts) != 1 {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown custom tool route"))
		return
	}

	switch r.Method {
	case http.MethodGet:
		registered := s.registeredCustomToolNames()
		writeJSON(w, viewOf(def, registered[def.Name]))

	case http.MethodPut, http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, customtools.MaxDefinitionBytes)

		var next customtools.Definition
		if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		// Identity: the update keeps the existing id and creation time.
		next.ID = def.ID
		next.CreatedAt = def.CreatedAt

		// Enabled state changes through the dedicated toggle route OR an
		// explicit enabled field in the update (the UI toggle sends the
		// whole definition).
		if err := s.ctStore.Save(&next); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}

		s.registerCustomTools()

		updated, _ := s.ctStore.Get(id)
		registered := s.registeredCustomToolNames()
		writeJSON(w, viewOf(updated, registered[updated.Name]))

	case http.MethodDelete:
		if s.ctStore.Delete(id) {
			s.registerCustomTools()
			logging.Default().Info("custom-tools", "tool %q deleted", def.Name)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeErr(w, http.StatusNotFound, fmt.Errorf("custom tool %q not found", id))

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// handleCustomToolTest executes a tool with caller-supplied sample
// arguments through the SAME executor the agent loop uses — a real
// end-to-end exercise, bounded by the tool's own timeout/output cap.
func (s *Server) handleCustomToolTest(w http.ResponseWriter, r *http.Request, def *customtools.Definition) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	// Allow testing a tool that is not yet enabled (that is the point of
	// a test) — but ONLY through this explicit user action; the agent
	// registry still refuses disabled tools.
	runDef := *def
	runDef.Enabled = true

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

	var body struct {
		Args json.RawMessage `json:"args"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	tool := customtools.NewTool(&runDef)

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(runDef.TimeoutSec)*time.Second)
	defer cancel()

	started := time.Now()

	out, err := tool.Run(ctx, body.Args)

	result := map[string]any{
		"toolId":   runDef.ID,
		"toolName": runDef.Name,
		"duration": time.Since(started).Milliseconds(),
	}

	if err != nil {
		result["ok"] = false
		result["error"] = err.Error()

		// A DISABLED rejection can only happen if the definition changed
		// concurrently — report honestly.
		if errors.Is(err, customtools.ErrDisabled) {
			result["error"] = "tool is disabled — enable it first"
		}

		writeJSON(w, result)
		return
	}

	result["ok"] = true
	result["output"] = out

	writeJSON(w, result)
}

// registeredCustomToolNames snapshots which custom tool names are
// currently live in the registry.
func (s *Server) registeredCustomToolNames() map[string]bool {
	out := map[string]bool{}

	if s.orch == nil || s.ctStore == nil {
		return out
	}

	known := map[string]bool{}
	for _, d := range s.ctStore.List() {
		known[d.Name] = true
	}

	for name, t := range s.orch.Tools() {
		if known[name] {
			if _, isCustom := t.(*customtools.Tool); isCustom {
				out[name] = true
			}
		}
	}

	return out
}
