// Session context policy API (1.1.6 §3/§4/§8/§16).
//
// The backend is the single source of truth for context decisions:
// this endpoint resolves the per-session policy against the global
// configuration, the model's GGUF limit, the engine's verified window
// and the machine's memory budget, and returns EVERY figure separately
// so the UI never has to calculate a different truth:
//
//	GET /api/sessions/{id}/context   — the full resolved status + selector options
//	PUT  /api/sessions/{id}/context  — set/clear the per-session policy
//
// A policy that is predictably destabilising (resource classification
// "unsupported") is REJECTED with an explanation rather than accepted
// and silently misbehaving later.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// contextStatus is the resolved context decision for one session.
type contextStatus struct {
	SessionID string `json:"sessionId"`

	// The decision chain, every figure exposed separately (1.1.6 §3).
	Requested     int `json:"requested"`               // what this chat asked for (policy or global)
	Configured    int `json:"configured"`              // global cfg.LLM.NumCtx
	SessionPolicy int `json:"sessionPolicy,omitempty"` // raw per-session policy (0 = inherit)
	Effective     int `json:"effective"`               // the window that will actually be used
	ModelMax      int `json:"modelMax,omitempty"`      // GGUF training limit (0 = unknown)
	EngineMax     int `json:"engineMax,omitempty"`     // engine-verified window (0 = unknown)
	UsableInput   int `json:"usableInput"`             // effective − output reserve
	OutputReserve int `json:"outputReserve"`           // tokens held for the reply
	SafetyReserve int `json:"safetyReserve"`           // estimator-error headroom

	// Live usage against the effective window (backend-measured).
	Used      int     `json:"used"`
	Remaining int     `json:"remaining"`
	Pressure  float64 `json:"pressure"`

	// Resource verdict for the effective size (1.1.6 §8).
	Classification string `json:"classification"`
	ResourceReason string `json:"resourceReason,omitempty"`

	// Why the decision is what it is, in order.
	Reasons []string `json:"reasons,omitempty"`

	// Selector ladder for the UI (1.1.6 §4): 4K…128K with per-size
	// verdicts. Only values valid for the current model are available.
	Options []llm.ContextOption `json:"options"`
}

// handleSessionContext serves GET/PUT /api/sessions/{id}/context.
// The route mux delivers paths of the form "{id}/context" here.
func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		status, err := s.sessionContextStatus(sessionID)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, status)

	case http.MethodPut, http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

		var body struct {
			ContextTokens *int `json:"contextTokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if body.ContextTokens == nil || *body.ContextTokens < 0 {
			writeErr(w, http.StatusBadRequest, errString("contextTokens must be a non-negative integer (0 = inherit global)"))
			return
		}

		tokens := *body.ContextTokens

		// Resource gate: never accept a value that predictably
		// destabilises the machine. Explain WHY instead.
		if tokens > 0 {
			status, err := s.sessionContextStatus(sessionID)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			for _, opt := range status.Options {
				if opt.Tokens == tokens && opt.Class == llm.ResourceUnsupported {
					writeErr(w, http.StatusConflict, errString(
						"context "+llm.ContextLabel(tokens)+" is not supported on this machine: "+opt.Reason))
					return
				}
			}
		}

		// v1.2.8.1: the token policy is applied through the store's
		// atomic context mutation — the previous Get-copy/Update-
		// whole-context shape could silently revert a concurrent
		// context update (history refs attach, attachment associate)
		// that landed between the read and the write.
		if err := s.store.UpdateContextFunc(sessionID, func(c *sessions.Context) (bool, error) {
			changed := c.ContextTokens != tokens
			c.ContextTokens = tokens
			return changed, nil
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		// The choice lives with THIS session only — no global mutation.
		status, err := s.sessionContextStatus(sessionID)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, status)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// engineWindow returns the currently verified engine window across both
// backends (0 = unknown / engine not alive).
func (s *Server) engineWindow() int {
	limit := 0
	if s.llama != nil {
		if v := s.llama.EngineContextLimit(); v > 0 {
			limit = v
		}
	}
	if s.native != nil {
		if res, err := s.native.ModelInfo(context.Background()); err == nil &&
			res.Loaded && res.Model != nil && res.Model.ContextLength > 0 {
			n := int(res.Model.ContextLength)
			if limit == 0 || n < limit {
				limit = n
			}
		}
	}
	return limit
}

// sessionContextStatus resolves the full context decision for one session.
func (s *Server) sessionContextStatus(sessionID string) (*contextStatus, error) {
	sess, err := s.store.Get(sessionID)
	if err != nil {
		return nil, err
	}

	cfg := s.src.Load()

	// Model facts + the authoritative decision — the same function the
	// run path uses, with the same nil-card fallback (card unavailable →
	// nil caps → the decision matches the orchestrator's).
	var card *llm.ModelCard
	modelSize := int64(0)
	var caps *llm.ModelCapabilities
	if !cfg.IsRemote() {
		if modelPath, perr := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model); perr == nil {
			// v1.1.8: one cached read serves both the card and the
			// capability card (was: two independent GGUF parses).
			card, caps = modelCardFor(modelPath, cfg)
			if fi, serr := os.Stat(modelPath); serr == nil {
				modelSize = fi.Size()
			}
		}
	}

	engineMax := s.engineWindow()

	effCtx := llm.ResolveSessionContext(cfg, caps, engineMax, sess.Context.ContextTokens)

	budget := contextplan.NewBudget(effCtx.Effective, cfg.LLM.MaxTokens)
	safety := agent.ContextSafetyMargin(effCtx.Effective)

	// Live usage measured by the backend over the session's real history.
	usage := continuum.EstimateUsage(sess.Messages, budget.Usable)
	used := usage.EstTokens
	remaining := budget.Usable - used
	if remaining < 0 {
		remaining = 0
	}
	pressure := 0.0
	if budget.Usable > 0 {
		pressure = float64(used) / float64(budget.Usable)
	}

	// Resource verdict for the effective size.
	kvQuant := cfg.EffectiveKVCacheQuant()
	mem := s.systemMemory()
	assessed := llm.AssessContextResource(card, modelSize, effCtx.Effective, kvQuant, mem)

	requested := effCtx.Requested
	if requested <= 0 {
		requested = effCtx.Configured
	}

	return &contextStatus{
		SessionID:      sessionID,
		Requested:      requested,
		Configured:     effCtx.Configured,
		SessionPolicy:  effCtx.SessionPolicy,
		Effective:      effCtx.Effective,
		ModelMax:       effCtx.ModelMax,
		EngineMax:      effCtx.EngineLimit,
		UsableInput:    budget.Usable,
		OutputReserve:  budget.ReserveOutput,
		SafetyReserve:  safety,
		Used:           used,
		Remaining:      remaining,
		Pressure:       pressure,
		Classification: string(assessed.Class),
		ResourceReason: assessed.Reason,
		Reasons:        effCtx.Reasons,
		Options: llm.ContextOptions(
			card, modelSize, kvQuant, mem,
			effCtx.ModelMax, effCtx.EngineLimit,
		),
	}, nil
}

// systemMemory converts the host probe into the assessment input.
//
// v1.2.6 continuation: this reads sysinfo.ProbeFast() LIVE instead of the
// Server's construction-time snapshot. The snapshot used to be frozen at
// New() time (fast facts only — VRAM arrives with the deep probe), so a
// session-context assessment during the first minutes of a cold start saw
// ZERO VRAM even on GPU machines. ProbeFast merges the measured deep facts
// the moment the background probe lands and never blocks on it.
func (s *Server) systemMemory() llm.SystemMemory {
	mem := llm.SystemMemory{}

	info := sysinfo.ProbeFast()
	if info == nil {
		info = s.sys
	}
	if info != nil {
		mem.TotalRAMBytes = int64(info.RAM.TotalBytes)
		mem.AvailableRAMBytes = int64(info.RAM.Available)
		for _, gpu := range info.GPU {
			if gpu.VRAMBytes > 0 {
				mem.TotalVRAMBytes += int64(gpu.VRAMBytes)
			}
		}
	}

	return mem
}

// errString is a tiny helper for static error payloads.
type staticError string

func (e staticError) Error() string { return string(e) }

func errString(s string) error { return staticError(s) }

// handleModelsFolder opens the models directory in the OS file manager
// (1.1.6 §10 first-use path: [Open models folder]). Desktop use only —
// best effort, errors surface honestly.
func (s *Server) handleModelsFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()
	dir := cfg.ModelsDir
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	var openErr error
	switch runtime.GOOS {
	case "windows":
		openErr = exec.Command("explorer", dir).Start()
	case "darwin":
		openErr = exec.Command("open", dir).Start()
	default:
		openErr = exec.Command("xdg-open", dir).Start()
	}
	if openErr != nil {
		writeErr(w, http.StatusInternalServerError, errString(
			"could not open the models folder ("+dir+"): "+openErr.Error()))
		return
	}

	writeJSON(w, map[string]any{
		"ok":   true,
		"path": filepath.Clean(dir),
	})
}

// sessionSubresource routes "/{id}/{rest}" pairs for the session endpoint.
func splitSessionPath(rest string) (id, sub string) {
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}
