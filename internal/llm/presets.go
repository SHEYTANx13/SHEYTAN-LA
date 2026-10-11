// Package llm provides the GGUF inference engine: a llama.cpp subprocess
// manager + an OpenAI-compatible HTTP client + sampling presets.
package llm

import (
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// Preset is a named bundle of sampling/runtime knobs.
type Preset struct {
	Name          string  `json:"name"`
	Label         string  `json:"label"`
	Description   string  `json:"description"`
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
	TopK          int     `json:"top_k"`
	MaxTokens     int     `json:"max_tokens"`
	RepeatPenalty float64 `json:"repeat_penalty"`
	Mirostat      int     `json:"mirostat"`
	NumCtx        int     `json:"num_ctx"`
}

// Presets returns the canonical preset matrix (LM Studio-style).
func Presets() []Preset {
	return []Preset{
		{Name: "precise", Label: "Precise", Description: "Low temperature, deterministic answers (good for Q&A, math, facts)",
			Temperature: 0.1, TopP: 0.5, TopK: 8, MaxTokens: 1024, RepeatPenalty: 1.1, Mirostat: 0, NumCtx: 4096},
		{Name: "balanced", Label: "Balanced", Description: "Default — good general purpose balance",
			Temperature: 0.7, TopP: 0.95, TopK: 40, MaxTokens: 1024, RepeatPenalty: 1.1, Mirostat: 0, NumCtx: 8192},
		{Name: "creative", Label: "Creative", Description: "High temperature for brainstorming, fiction, dialogue",
			Temperature: 1.1, TopP: 0.97, TopK: 100, MaxTokens: 2048, RepeatPenalty: 1.05, Mirostat: 2, NumCtx: 8192},
		{Name: "coding", Label: "Coding", Description: "Tight, repetitive-penalty heavy for code generation",
			Temperature: 0.2, TopP: 0.85, TopK: 20, MaxTokens: 2048, RepeatPenalty: 1.18, Mirostat: 0, NumCtx: 16384},
		{Name: "verbose", Label: "Verbose", Description: "More tokens, richer responses",
			Temperature: 0.8, TopP: 0.97, TopK: 50, MaxTokens: 4096, RepeatPenalty: 1.07, Mirostat: 0, NumCtx: 16384},
		{Name: "locked", Label: "Locked", Description: "Reproducible — seed=42, temperature=0",
			Temperature: 0.0, TopP: 1.0, TopK: 1, MaxTokens: 1024, RepeatPenalty: 1.1, Mirostat: 0, NumCtx: 4096},
	}
}

// ApplyPreset overrides the matching fields on the given LLMOptions.
func ApplyPreset(cfg *config.LLMOptions, name string) bool {
	for _, p := range Presets() {
		if p.Name == name {
			cfg.Temperature = p.Temperature
			cfg.TopP = p.TopP
			cfg.TopK = p.TopK
			cfg.MaxTokens = p.MaxTokens
			cfg.RepeatPenalty = p.RepeatPenalty
			cfg.Mirostat = p.Mirostat
			cfg.NumCtx = p.NumCtx
			cfg.Preset = p.Name
			return true
		}
	}
	return false
}
