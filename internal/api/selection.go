// selection.go — v1.5.0 MODEL-FIRST startup: the backend-authoritative
// model-selection state machine.
//
// The engine-start contract (spec §6):
//
//	discover model → select model → resolve engine → inspect
//	capabilities → calculate settings → apply config atomically →
//	start engine → verify readiness → verify selected model → Ready
//
// POST /api/models/select drives that whole chain for one explicit
// selection. The phases are backend-authoritative and surfaced through
// /api/engine (and the WS engine frames) so the UI can never invent a
// state the engine did not report:
//
//	select → analyzing → configuring → loading → ready | failed
//	  (→ calibrating → ready, when a bounded real calibration runs)
//
// AUTO vs MANUAL (spec §5): in AUTO the evidence-based profile from the
// ONE recommendation engine (internal/recommendation — never a second
// tuning engine) is applied atomically with the selection; in MANUAL
// only the model changes and every explicit user value is preserved
// verbatim. Changing the model in AUTO recalculates the profile.
//
// No stop/start cycles are wasted: when the selected model already
// matches the running, verified engine configuration the loading step
// is skipped entirely.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/calibration"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// selectionPhase is the backend-authoritative state of the model
// selection flow.
type selectionPhase string

const (
	// phaseSelect: a selection request was accepted.
	phaseSelect selectionPhase = "select"
	// phaseAnalyzing: GGUF facts + measured hardware + engine capability.
	phaseAnalyzing selectionPhase = "analyzing"
	// phaseConfiguring: profile calculated; applying config atomically.
	phaseConfiguring selectionPhase = "configuring"
	// phaseLoading: engine start / model load in progress.
	phaseLoading selectionPhase = "loading"
	// phaseCalibrating: bounded real calibration in progress (AUTO).
	phaseCalibrating selectionPhase = "calibrating"
	// phaseReady: verified — engine ready and the SELECTED model serving.
	phaseReady selectionPhase = "ready"
	// phaseFailed: honest failure with the reason.
	phaseFailed selectionPhase = "failed"
)

// appliedProfile is the profile summary surfaced in the selection state
// (what AUTO applied — MANUAL applies nothing beyond the model).
type appliedProfile struct {
	Context        int    `json:"context,omitempty"`
	Threads        int    `json:"threads,omitempty"`
	ThreadsBatch   int    `json:"threadsBatch,omitempty"`
	GPULayers      int    `json:"gpuLayers,omitempty"`
	GPUAutoOffload bool   `json:"gpuAutoOffload"`
	FlashAttention bool   `json:"flashAttention"`
	KVCacheQuant   string `json:"kvCacheQuant,omitempty"`
	UBatchSize     int    `json:"ubatchSize,omitempty"`
	RuntimeProfile string `json:"runtimeProfile,omitempty"`
	PredictedClass string `json:"predictedClass,omitempty"`
	PredictedVRAM  int64  `json:"predictedVramBytes,omitempty"`
}

// selectionState is the single authoritative selection record.
type selectionState struct {
	Phase selectionPhase `json:"phase"`

	// Model is the selected model NAME (config form).
	Model string `json:"model,omitempty"`
	// ModelPath is the resolved absolute path.
	ModelPath string `json:"modelPath,omitempty"`
	// PreviousModel is the model this selection replaced (the selector's
	// "previous selection" marker).
	PreviousModel string `json:"previousModel,omitempty"`

	// PerformanceMode active during this selection (auto | manual).
	PerformanceMode string `json:"performanceMode"`
	// Applied is the profile AUTO applied (nil in MANUAL).
	Applied *appliedProfile `json:"applied,omitempty"`
	// Evidence cites the measured inputs behind the applied profile.
	Evidence []string `json:"evidence,omitempty"`

	// Calibrated is true only after a real measured calibration (or a
	// retained one) — never for a mere evidence-based profile.
	Calibrated bool `json:"calibrated"`
	// Calibration summarizes the bounded pass (nil when none ran).
	Calibration *calibration.Summary `json:"calibration,omitempty"`
	// CalibrationNote is the honest label ("not benchmarked" etc).
	CalibrationNote string `json:"calibrationNote,omitempty"`

	// Error is the failure reason when Phase == failed.
	Error string `json:"error,omitempty"`

	// UpdatedAt is when this state was written.
	UpdatedAt time.Time `json:"updatedAt"`
}

// handleModelsSelect routes GET (current state) and POST (select) for
// /api/models/select.
func (s *Server) handleModelsSelect(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.selectionMu.Lock()
		st := s.selectionState()
		s.selectionMu.Unlock()
		writeJSON(w, st)

	case http.MethodPost:
		s.selectModel(w, r)

	default:
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
	}
}

// selectionState returns the current selection state (initialized from
// config so a fresh install reports "model selection required" through
// the same surface). Caller holds selectionMu.
func (s *Server) selectionState() *selectionState {
	if s.selection != nil {
		return s.selection
	}

	cfg := s.src.Load()

	st := &selectionState{
		Phase:           phaseReady,
		PerformanceMode: cfg.EffectivePerformanceMode(),
		UpdatedAt:       time.Now().UTC(),
	}

	// A fresh install with no selection reports the pre-selection state:
	// nothing failed, the Model Selector simply owns the next step.
	if !cfg.IsRemote() && cfg.Model == "" {
		st.Phase = phaseSelect
		st.Model = ""
		st.Error = ""
	}

	return st
}

// setSelection writes the selection state under the mutex.
func (s *Server) setSelection(st *selectionState) {
	st.UpdatedAt = time.Now().UTC()

	s.selectionMu.Lock()
	s.selection = st
	s.selectionMu.Unlock()
}

// updateSelection mutates the current state under the mutex.
func (s *Server) updateSelection(fn func(*selectionState)) {
	s.selectionMu.Lock()
	defer s.selectionMu.Unlock()

	if s.selection == nil {
		cfg := s.src.Load()
		s.selection = &selectionState{
			Phase:           phaseSelect,
			PerformanceMode: cfg.EffectivePerformanceMode(),
		}
	}

	fn(s.selection)
	s.selection.UpdatedAt = time.Now().UTC()
}

// anyRunActive reports whether an agent/chat run is currently live —
// model changes must be coordinated with active runs, never raced.
func (s *Server) anyRunActive() bool {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()
	return len(s.runs) > 0
}

// BeginEngineMutation / EndEngineMutation implement the llm.EngineAdmission
// seam (v1.9.5, P1): the ONE coordination point that closes the
// engine-transaction / run-admission TOCTOU.
//
// The claim and the run-registry read happen inside ONE runsMu critical
// section, and run admission reads the same runsMu-guarded flag inside
// ITS registration critical section. The two operations are therefore
// linearized: a run either registers before the claim (the claim's live
// verdict sees it and the transaction refuses), or after (its admission
// observes the claim and is refused). No accepted run can lose its
// serving engine across a candidate/update/rollback transaction.
//
// Lock discipline: runsMu is a leaf lock here — no runtime call, no
// engine call, no blocking work happens under it, so the transaction
// (holding switchMu + the engine lease) cannot deadlock with admission.
func (s *Server) BeginEngineMutation() bool {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	live := len(s.runs) > 0

	if !live {
		s.engineMutation = true
	}

	return live
}

// EndEngineMutation releases the admission claim taken by
// BeginEngineMutation.
func (s *Server) EndEngineMutation() {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	s.engineMutation = false
}

// engineMutationActive reports the runsMu-guarded claim flag for the
// fast, user-friendly pre-checks (the airtight check lives inside the
// admission critical sections).
func (s *Server) engineMutationActive() bool {
	s.runsMu.Lock()
	defer s.runsMu.Unlock()

	return s.engineMutation
}

// errEngineMutationInProgress is the deterministic user-visible refusal
// while an engine variant transaction is in flight.
var errEngineMutationInProgress = fmt.Errorf(
	"engine maintenance in progress — new runs are deferred until the engine transaction settles; retry shortly",
)

// selectModelRequestBody is the POST /api/models/select body.
type selectModelRequestBody struct {
	// Model is the model name or absolute path (required).
	Model string `json:"model"`
	// Task optionally names the runtime profile (default chat).
	Task string `json:"task"`
}

// selectModel implements the engine-start contract for one selection.
func (s *Server) selectModel(w http.ResponseWriter, r *http.Request) {
	var body selectModelRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decode select body: %w", err))
		return
	}

	cfg := s.src.Load()

	if cfg.IsRemote() {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"remote provider active — model selection is a local-engine concept",
		))
		return
	}

	model := filepath.Base(filepath.Clean(body.Model))
	if body.Model == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"model is required (pick one from GET /api/models)",
		))
		return
	}

	// Resolve FIRST (discover/verify) — an unknown model is a 404 with
	// the honest reason, before ANY config change. The failed attempt
	// is still recorded in the selection state (observability), it just
	// never mutates configuration.
	resolved, rerr := llm.ResolveModelPath(cfg.ModelsDir, body.Model)
	if rerr != nil {
		if errors.Is(rerr, llm.ErrNoModelSelected) {
			writeErr(w, http.StatusBadRequest, rerr)
			return
		}
		s.setSelection(&selectionState{
			Phase:           phaseFailed,
			Model:           model,
			PerformanceMode: cfg.EffectivePerformanceMode(),
			Error:           rerr.Error(),
		})
		writeErr(w, http.StatusNotFound, rerr)
		return
	}

	// Changing the model while a run is active must be blocked — an
	// engine swap under a live generation would abort it mid-flight.
	if s.anyRunActive() {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"a run is active — model changes are locked while a generation is in flight",
		))
		return
	}

	// One selection/calibration flow at a time.
	if s.calibrating.Load() {
		writeErr(w, http.StatusConflict, fmt.Errorf(
			"an automatic calibration is in progress — retry when it settles",
		))
		return
	}

	task := body.Task
	if !recommendation.IsValidTask(task) {
		task = string(recommendation.TaskChat)
	}

	// ---- phase: select -------------------------------------------------
	st := &selectionState{
		Phase:           phaseSelect,
		Model:           model,
		ModelPath:       resolved,
		PreviousModel:   previousModelName(cfg),
		PerformanceMode: cfg.EffectivePerformanceMode(),
		Evidence:        []string{},
	}
	s.setSelection(st)

	// ---- phase: analyzing (GGUF facts + measured hw + engine caps) -----
	s.updateSelection(func(x *selectionState) { x.Phase = phaseAnalyzing })

	in := s.buildRecommendationInput(cfg, model, task)
	rec := recommendation.Recommend(in)

	// ---- phase: configuring (ONE atomic config apply) ------------------
	s.updateSelection(func(x *selectionState) { x.Phase = phaseConfiguring })

	auto := cfg.EffectivePerformanceMode() == config.ModeAuto

	// Retained calibration (same model fingerprint + same machine +
	// same engine identity + same task profile): the previously
	// MEASURED verified profile wins over a fresh evidence-only
	// calculation — reuse, don't re-benchmark. Any identity mismatch
	// (or a corrupt record) is a miss: the evidence-based profile is
	// used instead (spec §6 — retained calibration is invalidated
	// safely, failing closed).
	var retained calibration.Record
	haveRetained := false
	if auto {
		if mk, err := calibration.ModelKey(resolved); err == nil {
			hw := in.HW
			if rk, ok := calibration.Retained(cfg.DataDir, mk, calibration.HWKey(hw), task); ok {
				retained = rk
				haveRetained = true
				rec = retained.Profile
			}
		}
	}

	next, cerr := s.applySelectionConfig(cfg, resolved, model, auto, rec, task)
	if cerr != nil {
		s.failSelection(cerr)
		writeErr(w, http.StatusInternalServerError, cerr)
		return
	}

	if auto {
		applied := profileSummary(rec)
		s.updateSelection(func(x *selectionState) {
			x.Applied = applied
			x.Evidence = rec.Reasons
			if haveRetained {
				x.Calibrated = true
				x.CalibrationNote = fmt.Sprintf(
					"retained verified profile (measured %.1f tok/s, first token %.2fs, %s)",
					retained.GenTokensPerSec, retained.TTFTSeconds,
					retained.MeasuredAt.Format("2006-01-02"),
				)
			}
		})
	} else {
		s.updateSelection(func(x *selectionState) {
			x.Applied = nil
			x.Evidence = []string{
				"MANUAL mode: explicit user settings preserved verbatim; only the model changed",
			}
		})
	}

	// ---- phase: loading (engine start / model load) ---------------------
	s.updateSelection(func(x *selectionState) { x.Phase = phaseLoading })

	skipRestart := s.selectedModelAlreadyServing(resolved)
	booted := false

	if !skipRestart {
		var err error
		booted, err = s.startEngineForSelection(r.Context(), resolved)
		if err != nil {
			s.failSelection(err)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	// ---- verify: readiness + the SELECTED model ------------------------
	// Only a flow that actually booted (or found the model already
	// serving) claims verified readiness; with auto-start OFF the
	// selection is applied and the engine boots on first use — reported
	// honestly as ready-with-note, never as a fake verification.
	if booted || skipRestart {
		if err := s.verifySelection(resolved); err != nil {
			s.failSelection(err)
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		s.updateSelection(func(x *selectionState) {
			// Preserve an honest note set earlier in the flow (a
			// retained verified profile, a calibration reason) — the
			// auto-start hint only fills an empty note.
			if x.CalibrationNote == "" {
				x.CalibrationNote = "selection applied — the engine starts on first use (auto-start is off)"
			}
		})
	}

	s.updateSelection(func(x *selectionState) {
		x.Phase = phaseReady
		if !x.Calibrated && x.CalibrationNote == "" {
			x.CalibrationNote = "evidence-based automatic profile — not benchmarked"
		}
	})

	// ---- bounded real calibration (AUTO, llama backend, engine live) ---
	if auto && !haveRetained && s.calibrationEligible() {
		s.startCalibration(next, resolved, rec)
	}

	s.selectionMu.Lock()
	resp := s.selectionState()
	s.selectionMu.Unlock()

	writeJSON(w, resp)
}

// previousModelName renders the outgoing model for the "previous
// selection" marker (empty when this is the first selection).
func previousModelName(cfg *config.Config) string {
	if cfg.Model == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(cfg.Model))
}

// applySelectionConfig performs the ONE atomic config write of the
// selection flow: Model (+ PreviousModel) always; the recommendation
// profile only in AUTO. The returned config is the new live config.
func (s *Server) applySelectionConfig(
	cfg *config.Config,
	resolved, name string,
	auto bool,
	rec recommendation.Recommendation,
	task string,
) (*config.Config, error) {
	next := *cfg
	next.Model = name
	if old := cfg.Model; old != "" && old != name {
		next.PreviousModel = old
	}

	if auto {
		ApplyRecommendationToConfig(&next, rec)
		next.RuntimeProfile = task
	}

	if err := config.Save(next.ConfigPath(), &next); err != nil {
		return nil, fmt.Errorf("persist selection: %w", err)
	}

	s.src.Store(&next)

	return &next, nil
}

// ApplyRecommendationToConfig maps the recommendation engine's output
// onto the config fields that drive engine launch args. It is exported
// so the calibration flow reuses the EXACT same mapping (one seam, no
// drift between the initial apply and a calibration winner re-apply).
func ApplyRecommendationToConfig(cfg *config.Config, rec recommendation.Recommendation) {
	if rec.Context > 0 {
		cfg.LLM.NumCtx = rec.Context
	}
	if rec.Threads > 0 {
		cfg.LLM.NumThread = rec.Threads
	}
	if rec.ThreadsBatch > 0 {
		cfg.ThreadsBatch = rec.ThreadsBatch
	}
	cfg.LLM.NumGPU = rec.GPULayers
	cfg.GPUAutoOffload = rec.GPUAutoOffload
	cfg.FlashAttention = rec.FlashAttention
	if rec.KVCacheQuant != "" {
		cfg.KVCacheQuant = rec.KVCacheQuant
	}
	if rec.UBatchSize > 0 {
		cfg.UBatchSize = rec.UBatchSize
	}
	if rec.MMProjOffload != "" {
		cfg.VisionMMProjOffload = rec.MMProjOffload
	}
	if rec.CacheReuse > 0 {
		cfg.CacheReuse = rec.CacheReuse
	}
}

// profileSummary renders the applied-profile block for the state.
func profileSummary(rec recommendation.Recommendation) *appliedProfile {
	return &appliedProfile{
		Context:        rec.Context,
		Threads:        rec.Threads,
		ThreadsBatch:   rec.ThreadsBatch,
		GPULayers:      rec.GPULayers,
		GPUAutoOffload: rec.GPUAutoOffload,
		FlashAttention: rec.FlashAttention,
		KVCacheQuant:   rec.KVCacheQuant,
		UBatchSize:     rec.UBatchSize,
		RuntimeProfile: string(rec.Task),
		PredictedClass: rec.Predicted.Class,
		PredictedVRAM:  rec.Predicted.VRAMBytes,
	}
}

// failSelection records the honest failure.
func (s *Server) failSelection(err error) {
	s.updateSelection(func(x *selectionState) {
		x.Phase = phaseFailed
		x.Error = err.Error()
	})
}

// selectedModelAlreadyServing reports whether the engine is verified
// ready AND already serving exactly the selected model — the
// avoid-unnecessary-restart shortcut (spec §6).
func (s *Server) selectedModelAlreadyServing(resolved string) bool {
	if s.native != nil && s.src.Load().NativeBackendEnabled() {
		return s.native.IsAlive() && s.native.NativeModelPath() == resolved
	}

	return s.llama.VerifiedReady() && s.llama.LoadedModel() == resolved
}

// startEngineForSelection boots (or reboots) the engine for the freshly
// applied selection. The engine-backend order mirrors the /api/llama
// start contract: native first when enabled, then llama.
func (s *Server) startEngineForSelection(ctx context.Context, resolved string) (bool, error) {
	if s.native != nil && s.src.Load().NativeBackendEnabled() {
		nativeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		err := s.stack.EnsureNativeReady(nativeCtx)
		cancel()
		if err != nil {
			return false, fmt.Errorf("native engine: %w", err)
		}
	}

	cfg := s.src.Load()

	// The llama engine only needs to run when it is (or becomes) the
	// serving backend: a verified-capable native engine makes llama a
	// fallback that may legitimately stay down (the Phase 5 contract).
	nativeServes := false
	if s.native != nil && cfg.NativeBackendEnabled() {
		if gc, ok := s.stack.NativeBackend().(llm.GenerationCapable); ok && gc.GenerationCapable() {
			nativeServes = true
		}
	}

	if nativeServes && !s.llama.IsRunning() {
		return true, nil
	}

	switch {
	case s.llama.IsRunning():
		// A different model is loaded — the restart swaps it (the
		// canonical persist+restart seam, one cycle).
		if err := s.llama.Restart(); err != nil {
			return false, fmt.Errorf("engine restart for the selected model: %w", err)
		}
	case cfg.LlamaAutoStart:
		if err := s.llama.Start(); err != nil {
			return false, fmt.Errorf("engine start for the selected model: %w", err)
		}
	default:
		// Auto-start is off: the selection is applied; the engine boots
		// on the first use (EnsureLLM) — reported honestly, not as an
		// error.
		return false, nil
	}

	return true, nil
}

// verifySelection proves readiness AND that the SELECTED model is what
// is actually serving (spec §6: "verify selected model → expose Ready").
func (s *Server) verifySelection(resolved string) error {
	cfg := s.src.Load()

	if s.native != nil && cfg.NativeBackendEnabled() {
		if !s.native.IsAlive() {
			return fmt.Errorf("native engine is not alive after selection load")
		}
		if s.native.NativeModelPath() != resolved {
			return fmt.Errorf(
				"native engine serves %q, not the selected model",
				s.native.NativeModelPath(),
			)
		}
		// The native engine is the SERVING backend for this selection —
		// llama.cpp is a fallback (the Phase 5 single-selection
		// contract) and has NO verification obligation here unless it is
		// actually running alongside.
		if !s.llama.IsRunning() {
			return nil
		}
	}

	if !s.llama.VerifiedReady() {
		return fmt.Errorf("engine did not reach verified ready (state %s)", s.llama.State())
	}

	loaded := s.llama.LoadedModel()
	if s.llama.VerifiedModel() != "" {
		loaded = s.llama.VerifiedModel()
	}
	if loaded != resolved {
		return fmt.Errorf("engine serves %q, not the selected model %q", loaded, resolved)
	}

	return nil
}

// calibrationEligible: calibration drives the LLAMA engine's launch
// args; the native engine has no such profile surface, and a stopped
// engine (auto-start off) must not be booted for benchmarking.
func (s *Server) calibrationEligible() bool {
	cfg := s.src.Load()

	if cfg.EngineBackend == config.BackendNative {
		return false
	}

	if !s.llama.IsRunning() || !s.llama.VerifiedReady() {
		return false
	}

	// A missing binary means the running engine is an adopted instance
	// — the restart cycles of calibration cannot be driven locally.
	if _, err := os.Stat(llm.ExpectedEngineBinPath(cfg)); err != nil {
		return false
	}

	return true
}

// calibrationRunner implements calibration.Runner over the server/stack.
type calibrationRunner struct {
	s *Server
	// restartsAtStart snapshots the watchdog counter at pass start so
	// EngineStable can prove no auto-restart happened during measurement.
	restartsAtStart int
}

// llamaServingModel is the model the llama engine is ACTUALLY serving
// right now (the verified id when available, the spawned path otherwise).
func llamaServingModel(l *llm.LlamaServer) string {
	if v := l.VerifiedModel(); v != "" {
		return v
	}
	return l.LoadedModel()
}

// ApplyProfile: run-abort check FIRST (a candidate must never displace a
// user run), then the atomic config apply + verified restart, then the
// model-serving verification (spec §2.2/§2.3/§2.13).
func (cr *calibrationRunner) ApplyProfile(ctx context.Context, modelPath string, rec recommendation.Recommendation) error {
	s := cr.s

	// Abort the calibration when a run arrives while we are between
	// candidates — the user's work wins over the benchmark. Checked
	// BEFORE any config write: an aborted candidate must not leave
	// its profile persisted (spec §2.7).
	if s.anyRunActive() {
		return fmt.Errorf("%w", calibration.ErrCalibrationAborted)
	}

	cfg := s.src.Load()

	next := *cfg
	ApplyRecommendationToConfig(&next, rec)
	if err := config.Save(next.ConfigPath(), &next); err != nil {
		return fmt.Errorf("persist calibration profile: %w", err)
	}
	s.src.Store(&next)

	if err := s.llama.Restart(); err != nil {
		return fmt.Errorf("restart with candidate profile: %w", err)
	}

	if !s.llama.VerifiedReady() {
		return fmt.Errorf("candidate did not reach verified ready (state %s)", s.llama.State())
	}

	// The candidate is only good when the engine is serving the
	// EXPECTED model after the swap — a profile that boots a
	// different model is a failed boot (spec §2.13, §8).
	if serving := llamaServingModel(s.llama); serving != modelPath {
		return fmt.Errorf("engine serves %q after the candidate swap, not the expected model", serving)
	}

	return nil
}

// MeasureGeneration: ONE short REAL generation through the live engine
// client (the same serving path a user message takes). Never a fabricated
// number: the sample is whatever the real engine reported.
func (cr *calibrationRunner) MeasureGeneration(ctx context.Context, prompt string, maxTokens int) (llm.PerfStats, error) {
	req := &llm.ChatRequest{
		MaxTokens: maxTokens,
		Stream:    true,
		Messages: []llm.Message{
			{Role: "user", Content: prompt},
		},
	}

	return cr.s.stack.Client.StreamChatDetailed(ctx, req, func(llm.StreamEvent) error {
		return nil
	})
}

// EngineStable: verified ready, the EXPECTED model still the one being
// served, and no watchdog restart since the pass started (spec §2.13:
// stability includes model-serving verification).
func (cr *calibrationRunner) EngineStable(expectedModel string) bool {
	s := cr.s
	if !s.llama.VerifiedReady() {
		return false
	}
	if llamaServingModel(s.llama) != expectedModel {
		return false
	}
	return s.llama.Restarts() == cr.restartsAtStart
}

// startCalibration arms the calibration/model-change exclusion gate and
// starts the bounded pass in a background worker. The calibrating phase
// is published SYNCHRONOUSLY — the selection response and the engine
// snapshot never expose a phantom ready while the pass is about to
// change the profile (spec §7: calibration state is honest and distinct
// from the final ready state).
func (s *Server) startCalibration(cfg *config.Config, modelPath string, base recommendation.Recommendation) {
	if !s.calibrating.CompareAndSwap(false, true) {
		return
	}

	// A run that is already active makes the whole pass moot — the
	// user's work wins over the benchmark (spec §2.3). The gate is
	// released before returning.
	if s.anyRunActive() {
		s.calibrating.Store(false)
		s.updateSelection(func(x *selectionState) {
			x.CalibrationNote = "calibration skipped — a run was active"
		})
		return
	}

	s.updateSelection(func(x *selectionState) {
		x.Phase = phaseCalibrating
		x.CalibrationNote = "measuring a small candidate set (bounded)"
	})

	go func() {
		defer s.calibrating.Store(false)
		s.runCalibration(cfg, modelPath, base)
	}()
}

// runCalibration executes the bounded pass and settles the selection
// state on the verified outcome: the winner (with ITS OWN measured
// metrics), the baseline restore, or the supervised failure.
func (s *Server) runCalibration(cfg *config.Config, modelPath string, base recommendation.Recommendation) {
	hw := hardware.Collect(cfg)
	cands := calibration.Candidates(base, hw, s.llama.Caps())

	runner := &calibrationRunner{s: s}
	runner.restartsAtStart = s.llama.Restarts()

	ctx, cancel := context.WithTimeout(context.Background(), calibration.DefaultTotalBudget+calibration.DefaultBootBudget)
	defer cancel()

	sum := calibration.Calibrate(ctx, runner, modelPath, base, cands, calibration.DefaultTotalBudget)

	s.updateSelection(func(x *selectionState) {
		x.Calibration = &sum

		// A supervised failure (the rollback itself failed) is NEVER
		// ready or verified — the real reason is surfaced (spec §2.11).
		if sum.Failed {
			x.Phase = phaseFailed
			x.Error = sum.Reason
			x.Calibrated = false
			x.CalibrationNote = ""
			return
		}

		if sum.Ran {
			x.Calibrated = true

			// The winner metrics come from the measurement whose
			// candidate EQUALS sum.Winner — never Measurements[0]
			// unless that IS the winner (spec §7).
			if wm := sum.WinnerMeasurement; wm != nil {
				x.CalibrationNote = fmt.Sprintf(
					"verified winner %q (%.1f tok/s, first token %.2fs) from measured runs",
					sum.Winner, wm.GenTokensPerSec, wm.TTFTSeconds,
				)
			} else {
				x.CalibrationNote = fmt.Sprintf(
					"verified winner %q from measured runs", sum.Winner,
				)
			}

			// The applied-profile block now describes the VERIFIED FINAL
			// profile — the profile actually left running (spec §2.9).
			if sum.WinnerProfile != nil {
				x.Applied = profileSummary(*sum.WinnerProfile)
			}
		} else {
			x.Calibrated = false
			x.CalibrationNote = "evidence-based automatic profile — not benchmarked"
		}
		x.Phase = phaseReady
	})

	if !sum.Ran || sum.Failed {
		return
	}

	// Retain the verified profile for this model on this machine, scoped
	// to the task profile it was measured with (spec §6).
	if mk, err := calibration.ModelKey(modelPath); err == nil {
		winner := sum.Winner
		profile := base
		if sum.WinnerProfile != nil {
			profile = *sum.WinnerProfile
		}

		var tokPerSec, ttft float64
		if wm := sum.WinnerMeasurement; wm != nil {
			tokPerSec = wm.GenTokensPerSec
			ttft = wm.TTFTSeconds
		}

		rec := calibration.Record{
			ModelPath:       modelPath,
			Winner:          winner,
			Profile:         profile,
			GenTokensPerSec: tokPerSec,
			TTFTSeconds:     ttft,
			MeasuredAt:      time.Now().UTC(),
		}

		if err := calibration.Retain(cfg.DataDir, mk, calibration.HWKey(hw), rec); err != nil {
			logging.Default().Warn(
				"calibration",
				"retained profile could not be persisted (it stays active for this session): %v",
				err,
			)
		}
	}
}

// modelRecommendation is the per-model evidence summary for the picker's
// "Recommended for this machine" chip — computed by the ONE
// recommendation engine over measured hardware + engine capability,
// never by filename/size heuristics.
type modelRecommendation struct {
	// Class is the resource verdict (safe | caution | unsupported).
	Class string `json:"class,omitempty"`
	// Speed is the qualitative prediction (never a tok/s claim).
	Speed string `json:"speed,omitempty"`
	// Context is the recommended context.
	Context int `json:"context,omitempty"`
	// GPULayers / GPUAutoOffload summarize the GPU posture.
	GPULayers      int  `json:"gpuLayers,omitempty"`
	GPUAutoOffload bool `json:"gpuAutoOffload"`
	// FitsVRAM reports a predicted VRAM share (0 = unknown).
	PredictedVRAM int64 `json:"predictedVramBytes,omitempty"`
	// Reasons cite the measured inputs (the chip's evidence).
	Reasons []string `json:"reasons,omitempty"`
	// Notes carry the honest limitations.
	Notes []string `json:"notes,omitempty"`
	// HardwareMeasured is true when the recommendation was computed
	// over a real measured hardware profile (the chip requires it).
	HardwareMeasured bool `json:"hardwareMeasured"`
}

// handleModelsRecommendations serves the per-model recommendation
// summaries for every local model (the picker fetches this ONCE when it
// opens — one hardware probe shared across models, cached model cards).
func (s *Server) handleModelsRecommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()

	task := r.URL.Query().Get("task")
	if !recommendation.IsValidTask(task) {
		task = string(recommendation.TaskChat)
	}

	hw := hardware.Collect(cfg)
	hwMeasured := hw.CPU.LogicalCores > 0 && hw.RAM.TotalBytes > 0

	out := map[string]modelRecommendation{}

	for _, name := range llm.ListLocalModels(cfg.ModelsDir) {
		in := s.buildRecommendationInput(cfg, name, task)
		// The batch shares ONE measured hardware profile (the same
		// source /api/recommendation uses for a single model).
		in.HW = hw

		rec := recommendation.Recommend(in)

		out[name] = modelRecommendation{
			Class:            rec.Predicted.Class,
			Speed:            rec.Predicted.Speed,
			Context:          rec.Context,
			GPULayers:        rec.GPULayers,
			GPUAutoOffload:   rec.GPUAutoOffload,
			PredictedVRAM:    rec.Predicted.VRAMBytes,
			Reasons:          rec.Reasons,
			Notes:            rec.Notes,
			HardwareMeasured: hwMeasured,
		}
	}

	writeJSON(w, map[string]any{
		"task":             task,
		"hardwareMeasured": hwMeasured,
		"recommendations":  out,
	})
}
