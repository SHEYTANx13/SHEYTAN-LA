// Package pipeline implements Phase 7D: programmatic tool pipelines.
//
// For a predictable sequence like inspect → modify → build → test →
// inspect failure → repair → retest → verify, forcing the model to
// regenerate every intermediate tool call wastes context and tokens — the
// scarcest resources on a small local model. A pipeline lets the model
// CHOOSE and AUTHORIZE a bounded deterministic stage plan once; the runtime
// then executes the stages without further model round-trips.
//
// Every stage is observable (activities), cancellable (context), bounded
// (per-stage timeout, output cap, stage count cap) and evidence-producing
// (results are recorded for the verification layer). The pipeline never
// invents actions: it only replays tool calls the declaring model specified,
// through the SAME policy guards a direct tool call would pass.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxStages bounds one pipeline declaration.
const MaxStages = 12

// MaxOutputBytes bounds one stage's captured output.
const MaxOutputBytes = 64 << 10

// Stage is one declared tool invocation inside a pipeline.
type Stage struct {
	// Name of the registered agent tool to invoke.
	Tool string `json:"tool"`
	// Args is the raw JSON argument object passed to the tool.
	Args json.RawMessage `json:"args"`
	// Label optionally names the stage for the activity trail.
	Label string `json:"label,omitempty"`
	// Optional stages may fail without aborting the pipeline (the result
	// still records the failure).
	Optional bool `json:"optional,omitempty"`
	// TimeoutSec bounds one stage (default 120, max 600).
	TimeoutSec int `json:"timeoutSec,omitempty"`
}

// Runner is the tool-execution seam: the orchestrator's registry behind a
// narrow interface (keeps the pipeline package decoupled).
type Runner interface {
	// RunTool executes one registered tool with policy guards and returns
	// its output (or an error).
	RunTool(ctx context.Context, name string, args json.RawMessage) (string, error)
	// HasTool reports whether the tool is registered and offered.
	HasTool(name string) bool
}

// StageResult is the observable outcome of one stage.
type StageResult struct {
	Stage      string        `json:"stage"`
	Tool       string        `json:"tool"`
	OK         bool          `json:"ok"`
	DurationMs int64         `json:"durationMs"`
	Output     string        `json:"output"`
	Err        string        `json:"err,omitempty"`
	Skipped    bool          `json:"skipped,omitempty"`
	Duration   time.Duration `json:"-"`
}

// Report is the full pipeline outcome (evidence-grade).
type Report struct {
	OK          bool          `json:"ok"`
	Stages      []StageResult `json:"stages"`
	FailedStage int           `json:"failedStage,omitempty"`
	DurationMs  int64         `json:"durationMs"`
	Canceled    bool          `json:"canceled,omitempty"`
}

// Summary renders the one-line evidence summary.
func (r *Report) Summary() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "pipeline %s (%d stages, %dms)",
		map[bool]string{true: "OK", false: "FAILED"}[r.OK],
		len(r.Stages), r.DurationMs)
	for _, s := range r.Stages {
		status := "ok"
		if s.Skipped {
			status = "skipped"
		} else if !s.OK {
			status = "FAILED: " + firstLine(s.Err)
		}
		fmt.Fprintf(&b, "\n  %s → %s", s.Tool, status)
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// OnEvent receives one activity line per stage (start/end); nil is fine.
type OnEvent func(stage int, msg string)

// Run executes a declared stage plan deterministically.
//
// Guarantees:
//   - stage count bounded (MaxStages);
//   - every stage runs under its own timeout and the caller's context
//     (cancellation propagates between stages);
//   - a failed stage aborts unless declared optional;
//   - every stage result is captured for evidence, output bounded.
func Run(ctx context.Context, r Runner, stages []Stage, onEvent OnEvent) *Report {
	report := &Report{OK: true, FailedStage: -1}
	started := time.Now()

	if len(stages) > MaxStages {
		stages = stages[:MaxStages]
		if onEvent != nil {
			onEvent(-1, fmt.Sprintf("pipeline truncated to %d stages (bound)", MaxStages))
		}
	}

	if len(stages) == 0 {
		report.OK = false
		report.DurationMs = time.Since(started).Milliseconds()
		return report
	}

	for i, st := range stages {
		if err := ctx.Err(); err != nil {
			report.Canceled = true
			report.OK = false
			// Mark remaining stages skipped.
			for j := i; j < len(stages); j++ {
				report.Stages = append(report.Stages, StageResult{
					Tool: stages[j].Tool, Skipped: true,
				})
			}
			break
		}

		if onEvent != nil {
			onEvent(i, fmt.Sprintf("stage %d/%d: %s %s",
				i+1, len(stages), st.Tool, st.Label))
		}

		res := StageResult{Stage: st.Label, Tool: st.Tool}

		if !r.HasTool(st.Tool) {
			res.Err = fmt.Sprintf("tool %q not registered", st.Tool)
			res.OK = false
			res.DurationMs = 0
			report.Stages = append(report.Stages, res)
			if finalizeStage(report, i, st, res, onEvent) {
				abortRemaining(report, stages, i+1)
				break
			}
			continue
		}

		timeout := time.Duration(st.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}

		// v1.2.9: stage duration anchors at THIS stage's actual start —
		// the pipeline's own `started` (which includes every earlier
		// stage's runtime) stays reserved for Report.DurationMs. The
		// previous `time.Since(started)` made every stage's
		// Duration/DurationMs CUMULATIVE (stage 3 reporting the elapsed
		// time of stages 1+2+3), which silently poisoned per-stage
		// evidence and the `stage N done (Xms)` activity lines.
		stageStart := time.Now()

		stageCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := r.RunTool(stageCtx, st.Tool, st.Args)
		cancel()

		res.Duration = time.Since(stageStart)
		res.DurationMs = res.Duration.Milliseconds()

		if err != nil {
			res.OK = false
			res.Err = err.Error()
			if out != "" {
				res.Output = bound(out)
			}
		} else {
			res.OK = true
			res.Output = bound(out)
		}

		report.Stages = append(report.Stages, res)
		if finalizeStage(report, i, st, res, onEvent) {
			abortRemaining(report, stages, i+1)
			break
		}
	}

	report.DurationMs = time.Since(started).Milliseconds()
	return report
}

// abortRemaining marks every stage from index start onward as skipped
// (required-failure and cancellation paths).
func abortRemaining(report *Report, stages []Stage, start int) {
	for j := start; j < len(stages); j++ {
		report.Stages = append(report.Stages, StageResult{
			Tool: stages[j].Tool, Skipped: true,
		})
	}
}

// finalizeStage records a stage outcome and reports whether the plan must
// abort (required failure).
func finalizeStage(report *Report, i int, st Stage, res StageResult, onEvent OnEvent) bool {
	if res.OK || res.Skipped {
		if onEvent != nil && !res.Skipped {
			onEvent(i, fmt.Sprintf("stage %d done (%dms)", i+1, res.DurationMs))
		}
		return false
	}

	if report.FailedStage < 0 {
		report.FailedStage = i
	}

	if st.Optional {
		if onEvent != nil {
			onEvent(i, fmt.Sprintf("stage %d failed (optional): %s", i+1, firstLine(res.Err)))
		}
		return false
	}

	report.OK = false
	if onEvent != nil {
		onEvent(i, fmt.Sprintf("stage %d FAILED: %s", i+1, firstLine(res.Err)))
	}
	return true
}

// bound caps captured output to MaxOutputBytes with an explicit marker.
func bound(s string) string {
	if len(s) <= MaxOutputBytes {
		return s
	}
	return s[:MaxOutputBytes] + fmt.Sprintf("\n[output bounded at %d bytes]", MaxOutputBytes)
}
