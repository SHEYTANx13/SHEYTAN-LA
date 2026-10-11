// v1.2.0 — the recommendation endpoint: Detected → Calculated →
// Recommended. Applying is a separate, explicit act done through the
// EXISTING config PATCH (PUT /api/config) so every applied value keeps its
// audit trail in config.json; measuring the effect afterwards happens
// through /api/perf. Nothing in this file mutates configuration.
package api

import (
	"net/http"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// recommendationPayload wraps the engine output with the CURRENT config
// values so the UI can render Current → Recommended pairs honestly.
type recommendationPayload struct {
	Task        string                         `json:"task"`
	Current     recommendationCurrent          `json:"current"`
	Recommended *recommendation.Recommendation `json:"recommended,omitempty"`
	Available   []recommendation.TaskInfo      `json:"available"`
}

type recommendationCurrent struct {
	Model          string `json:"model,omitempty"`
	Context        int    `json:"context,omitempty"`
	Threads        int    `json:"threads,omitempty"`
	UBatchSize     int    `json:"ubatchSize,omitempty"`
	GPULayers      int    `json:"gpuLayers,omitempty"`
	GPUAutoOffload bool   `json:"gpuAutoOffload"`
	FlashAttention bool   `json:"flashAttention"`
	KVCacheQuant   string `json:"kvCacheQuant,omitempty"`
	MMProjOffload  string `json:"mmprojOffload,omitempty"`
	RuntimeProfile string `json:"runtimeProfile,omitempty"`
}

// buildRecommendationInput assembles the Input from measured sources.
func (s *Server) buildRecommendationInput(cfg *config.Config, modelName, task string) recommendation.Input {
	modelPath := ""
	if modelName == "" {
		modelName = cfg.Model
	}
	if modelName != "" {
		if p, err := llm.ResolveModelPath(cfg.ModelsDir, modelName); err == nil {
			modelPath = p
		}
	}

	in := recommendation.Input{
		HW:                 hardware.Collect(cfg),
		Task:               recommendation.Task(task),
		ConfiguredContext:  cfg.LLM.NumCtx,
		EngineContextLimit: s.llama.EngineContextLimit(),
		KVQuant:            cfg.EffectiveKVCacheQuant(),
	}
	if !recommendation.IsValidTask(task) {
		in.Task = recommendation.TaskChat
	}

	if caps := s.llama.Caps(); caps != nil {
		in.Engine = caps
	}

	if modelPath != "" {
		card, caps := modelCardFor(modelPath, cfg)
		in.Card = card
		in.Caps = caps
		if card != nil {
			in.Vision = vision.EvaluateModel(
				cfg.ModelsDir, modelPath, cfg.VisionMMProj, card.Arch,
			)
		}
	}

	return in
}

// handleRecommendation serves GET /api/recommendation?model=<name>&task=<id>.
func (s *Server) handleRecommendation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	cfg := s.src.Load()
	model := r.URL.Query().Get("model")
	task := r.URL.Query().Get("task")

	in := s.buildRecommendationInput(cfg, model, task)
	rec := recommendation.Recommend(in)

	writeJSON(w, recommendationPayload{
		Task: string(in.Task),
		Current: recommendationCurrent{
			Model:          cfg.Model,
			Context:        cfg.LLM.NumCtx,
			Threads:        cfg.LLM.NumThread,
			UBatchSize:     cfg.EffectiveUBatchSize(),
			GPULayers:      cfg.LLM.NumGPU,
			GPUAutoOffload: cfg.GPUAutoOffload,
			FlashAttention: cfg.FlashAttention,
			KVCacheQuant:   cfg.EffectiveKVCacheQuant(),
			MMProjOffload:  cfg.VisionMMProjOffload,
			RuntimeProfile: cfg.RuntimeProfile,
		},
		Recommended: &rec,
		Available:   recommendation.Tasks(),
	})
}
