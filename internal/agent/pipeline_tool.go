package agent

// pipeline_tool.go — Phase 7D: the `pipeline` tool. Lets the model declare
// a bounded deterministic tool sequence ONCE (inspect → build → test →
// repair → retest → verify) and let the runtime execute it without
// regenerating every intermediate call. The model authorizes the workflow;
// the runtime executes it under the same bounds and observation as direct
// tool calls.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/pipeline"
)

// PipelineTool implements agent.Tool for declared pipelines.
type PipelineTool struct {
	// orch is the registry backing the execution seam (set at wiring).
	orch *Orchestrator
	// src resolves the per-run config snapshot for enablement checks.
	src *config.Source
}

// NewPipelineTool wires the pipeline tool to the orchestrator registry.
func NewPipelineTool(orch *Orchestrator, src *config.Source) *PipelineTool {
	return &PipelineTool{orch: orch, src: src}
}

func (p *PipelineTool) Name() string { return "pipeline" }

func (p *PipelineTool) Description() string {
	return "Execute a declared sequence of tool stages deterministically (inspect → build → test → verify). Use when a bounded, predictable workflow is known: each stage names a registered tool and its arguments; stages run in order under one budget, results are reported per stage. Choose this over repeating individual tool calls when the sequence is predictable; the sequence still respects all tool policies."
}

// pipelineParams is the flat JSON schema exposed to the model.
type pipelineParams struct {
	Stages []struct {
		Tool       string          `json:"tool"`
		Args       json.RawMessage `json:"args"`
		Label      string          `json:"label"`
		Optional   bool            `json:"optional"`
		TimeoutSec int             `json:"timeoutSec"`
	} `json:"stages"`
}

func (p *PipelineTool) Parameters() any { return pipelineParams{} }

// orchestratorRunner adapts the orchestrator registry to pipeline.Runner.
type orchestratorRunner struct {
	o       *Orchestrator
	offered map[string]bool
}

func (r *orchestratorRunner) HasTool(name string) bool {
	return r.offered[name]
}

func (r *orchestratorRunner) RunTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	t, ok := r.o.tool(name)
	if !ok {
		return "", fmt.Errorf("tool %q not registered", name)
	}
	return t.Run(ctx, args)
}

func (p *PipelineTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var req pipelineParams
	if err := json.Unmarshal(args, &req); err != nil {
		return "", fmt.Errorf("invalid pipeline arguments: %v", err)
	}

	if len(req.Stages) == 0 {
		return "", fmt.Errorf("pipeline requires at least one stage")
	}

	cfg := p.src.Load()

	offered := map[string]bool{}
	for name := range p.orch.Tools() {
		if cfg.ToolEnabled(name) {
			offered[name] = true
		}
	}

	stages := make([]pipeline.Stage, 0, len(req.Stages))
	for _, s := range req.Stages {
		stages = append(stages, pipeline.Stage{
			Tool:       s.Tool,
			Args:       s.Args,
			Label:      s.Label,
			Optional:   s.Optional,
			TimeoutSec: s.TimeoutSec,
		})
	}

	runner := &orchestratorRunner{o: p.orch, offered: offered}

	// Stage-level events fold into the final report (the agent loop's
	// activity stream already shows the pipeline tool start/end; the
	// per-stage trail rides the report JSON below).
	report := pipeline.Run(ctx, runner, stages, nil)

	encoded, err := json.Marshal(report)
	if err != nil {
		return report.Summary(), nil
	}

	return string(encoded), nil
}
